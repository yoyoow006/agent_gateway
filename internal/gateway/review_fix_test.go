package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agent_gateway/internal/config"
)

// SEC-01 回归：重载到 AdminToken 为空的配置后，管理端点仍拒绝无认证请求。
func TestAdminAuthSurvivesEmptyTokenReload(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.AdminToken = "secret"
	cfg.RebuildTokenIndex()
	s := New(cfg, func() (*config.Config, error) {
		broken := *cfg
		broken.AdminToken = ""
		return &broken, nil
	}, nil)
	h := s.Handler()

	req := httptest.NewRequest("POST", "/__agw/reload", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("初始无认证应 401: %d", rec.Code)
	}
	// 带正确令牌触发重载（换成空 admin 配置）
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("合法重载应 200: %d %s", rec.Code, rec.Body.String())
	}
	// 重载后空 token 不得旁路
	req = httptest.NewRequest("GET", "/__agw/metrics", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("空 AdminToken 时必须拒绝: %d", rec.Code)
	}
}

// BRK-01 回归：热重载不得复位正在冷却的熔断器。
func TestReloadPreservesOpenBreaker(t *testing.T) {
	var hits int
	upB := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(200)
		io.WriteString(w, `{"ok":true}`)
	})
	cfg, token := testConfig(t,
		anthropicProvider("dead", "http://127.0.0.1:1", 1),
		anthropicProvider("b", upB.server.URL, 2),
	)
	s := New(cfg, func() (*config.Config, error) { return config.Load(cfg.RepoRoot) }, nil)
	for i := 0; i < 3; i++ {
		rec := doRequest(s, token, "/v1/messages", `{"model":"m"}`)
		if rec.Code != 200 {
			t.Fatalf("第 %d 次应经 b 成功: %d", i+1, rec.Code)
		}
	}
	br, _ := s.registry.Breaker("dead")
	if br.State() != "open" {
		t.Fatalf("dead 应 open: %v", br.State())
	}
	// 重建同配置的 cfg 并热重载（等价 provider add/switch/SIGHUP）
	cfg2, _ := testConfig(t,
		anthropicProvider("dead", "http://127.0.0.1:1", 1),
		anthropicProvider("b", upB.server.URL, 2),
	)
	_ = cfg2
	s.mu.Lock()
	s.cfg = cfg2
	s.mu.Unlock()
	s.syncBreakers()
	br2, _ := s.registry.Breaker("dead")
	if br2.State() != "open" {
		t.Fatalf("热重载后熔断被复位: %v", br2.State())
	}
}

// BRK-02 回归：GET 跨协议跳过不得消耗半开探针。
func TestGetSkipDoesNotConsumeProbe(t *testing.T) {
	now := time.Unix(0, 0)
	upChat := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("GET 不应转发 chat 供应商")
	})
	upResp := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, `{"id":"resp_x","output":[],"status":"completed"}`)
	})
	cfg, token := testConfig(t,
		config.Provider{Name: "chat", Protocol: config.ProtocolOpenAIChat, BaseURL: upChat.server.URL, APIKey: "k", Priority: 1, Enabled: true},
		config.Provider{Name: "resp", Protocol: config.ProtocolOpenAIResponses, BaseURL: upResp.server.URL, APIKey: "k", Priority: 2, Enabled: true},
	)
	s := New(cfg, nil, nil)
	s.now = func() time.Time { return now }

	// 打开 chat 熔断
	for i := 0; i < 3; i++ {
		s.registry.RecordRequest("chat")
		s.registry.RecordFailure("chat", "boom")
	}
	br, _ := s.registry.Breaker("chat")
	if br.State() != "open" {
		t.Fatalf("chat 应 open: %v", br.State())
	}
	// 冷却到期
	now = now.Add(61 * time.Second)

	// GET /v1/responses：跳过 chat（不消耗探针），由 resp 服务
	req := httptest.NewRequest("GET", "/v1/responses", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == 404 || rec.Code == 401 {
		t.Fatalf("GET 应可达转发器: %d", rec.Code)
	}
	// chat 的半开探针名额未被消耗：Allow 应仍可用（POST 场景能探测恢复）
	if !br.Allow() {
		t.Fatal("GET 跳过不应消耗半开探针名额")
	}
}

// TOK-01 回归：count_tokens 请求体超限 413 且不触上游。
func TestCountTokensBodyOverLimit413(t *testing.T) {
	var hits int
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) { hits++ })
	cfg, token := testConfig(t, anthropicProvider("a", up.server.URL, 1))
	s := New(cfg, nil, nil)
	old := maxBody
	maxBody = 16
	defer func() { maxBody = old }()
	req := httptest.NewRequest("POST", "/v1/messages/count_tokens", strings.NewReader(`{"model":"m","pad":"0123456789abcdefghijk"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 413 || hits != 0 {
		t.Fatalf("应 413 且零上游连接: %d hits=%d", rec.Code, hits)
	}
}

// MIN-01 回归：项目档案配置错误返回 503 配置错误而非 401。
func TestProfileConfigErrorIs503(t *testing.T) {
	cfg, _ := testConfig(t, anthropicProvider("a", "http://127.0.0.1:1", 1))
	cfg.Projects["bad"] = config.ProjectProfile{Providers: []string{"no-such-provider"}, Token: "agw-bad"}
	cfg.RebuildTokenIndex()
	s := New(cfg, nil, nil)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("Authorization", "Bearer agw-bad")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "配置错误") {
		t.Fatalf("应 503 配置错误: %d %s", rec.Code, rec.Body.String())
	}
}

// STR-01 回归：上游流截断时客户端收到协议错误帧而非正常收尾。
func TestTranslatedStreamTruncationEmitsError(t *testing.T) {
	// chat 上游输出一半就断开（无 [DONE]、无 finish_reason）
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"部分\"},\"finish_reason\":null}]}\n\n")
		// 直接返回：连接关闭，流截断
	})
	prov := config.Provider{Name: "c", Protocol: config.ProtocolOpenAIChat, BaseURL: up.server.URL, APIKey: "k", Priority: 1, Enabled: true}
	out := streamViaGateway(t, prov, "/v1/messages", `{"model":"m","max_tokens":8,"stream":true}`)
	if !strings.Contains(out, "event: error") {
		t.Fatalf("截断流应输出 anthropic error 事件而非合成 message_stop:\n%s", out)
	}
	if strings.Contains(out, "event: message_stop") {
		t.Fatalf("截断流不得合成正常收尾:\n%s", out)
	}
}

// MIN-04：熔断冷却到期后原上游恢复接流（注入时钟的 e2e）。
func TestE2EBreakerCooldownRecovery(t *testing.T) {
	now := time.Unix(0, 0)
	var aPhase atomic.Int32 // 0=故障期 1=恢复期
	var aHits atomic.Int64
	upA := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if aHits.Add(1) <= 3 || aPhase.Load() == 0 {
			w.WriteHeader(500)
			io.WriteString(w, `{"type":"error","error":{"message":"boom"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		io.WriteString(w, `{"ok":"a"}`)
	})
	upB := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		io.WriteString(w, `{"ok":"b"}`)
	})
	cfg, token := testConfig(t,
		anthropicProvider("a", upA.server.URL, 1),
		anthropicProvider("b", upB.server.URL, 2),
	)
	s := New(cfg, nil, nil)
	s.now = func() time.Time { return now }

	// 3 次失败 → a 熔断打开
	for i := 0; i < 3; i++ {
		rec := doRequest(s, token, "/v1/messages", `{"model":"m","max_tokens":1}`)
		if !strings.Contains(rec.Body.String(), `"ok":"b"`) {
			t.Fatalf("故障期应由 b 服务: %s", rec.Body.String())
		}
	}
	br, _ := s.registry.Breaker("a")
	if br.State() != "open" {
		t.Fatalf("a 应 open: %v", br.State())
	}
	// 冷却期内：a 被跳过，b 继续服务且 a 零连接
	before := aHits.Load()
	rec := doRequest(s, token, "/v1/messages", `{"model":"m","max_tokens":1}`)
	if !strings.Contains(rec.Body.String(), `"ok":"b"`) || aHits.Load() != before {
		t.Fatalf("冷却期 a 应零连接: hits %d→%d", before, aHits.Load())
	}
	// 冷却到期 + a 恢复 → 探针成功回切
	now = now.Add(61 * time.Second)
	aPhase.Store(1)
	rec = doRequest(s, token, "/v1/messages", `{"model":"m","max_tokens":1}`)
	if !strings.Contains(rec.Body.String(), `"ok":"a"`) {
		t.Fatalf("冷却到期应由 a 接流: %s", rec.Body.String())
	}
	if br.State() != "closed" {
		t.Fatalf("探针成功应关闭熔断: %v", br.State())
	}
}
