package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"agent_gateway/internal/config"
)

func doRequest(s *Server, token, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestFailoverOnRetryableStatus(t *testing.T) {
	var aCount, bCount atomic.Int64
	upA := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		aCount.Add(1)
		w.WriteHeader(529)
		io.WriteString(w, `{"type":"error","error":{"type":"overloaded_error","message":"过载"}}`)
	})
	upB := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		bCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		io.WriteString(w, `{"ok":"from-b"}`)
	})
	cfg, token := testConfig(t,
		anthropicProvider("a", upA.server.URL, 1),
		anthropicProvider("b", upB.server.URL, 2),
	)
	s := New(cfg, nil, nil)
	rec := doRequest(s, token, "/v1/messages", `{"model":"m","max_tokens":1}`)
	if rec.Code != 200 || rec.Body.String() != `{"ok":"from-b"}` {
		t.Fatalf("客户端应无感拿到 B 的响应: %d %s", rec.Code, rec.Body.String())
	}
	if aCount.Load() != 1 || bCount.Load() != 1 {
		t.Errorf("a=%d b=%d，应各一次", aCount.Load(), bCount.Load())
	}
}

func TestAllFailReturnsLastErrorWithRetryAfter(t *testing.T) {
	upA := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(429)
		io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"A 限流"}}`)
	})
	// B：连接拒绝（直接关掉的服务器）
	deadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := deadServer.URL
	deadServer.Close()

	cfg, token := testConfig(t,
		anthropicProvider("a", upA.server.URL, 1),
		anthropicProvider("b", deadURL, 2),
	)
	s := New(cfg, nil, nil)
	rec := doRequest(s, token, "/v1/messages", `{"model":"m"}`)
	if rec.Code != 429 {
		t.Fatalf("应返回最后一次上游错误 429, got %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") != "30" {
		t.Errorf("Retry-After 丢失")
	}
	if !strings.Contains(rec.Body.String(), "A 限流") {
		t.Errorf("应保留 A 的错误体（B 是传输失败）: %s", rec.Body.String())
	}
}

func TestOpenBreakerSkipsProvider(t *testing.T) {
	var bCount atomic.Int64
	upB := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		bCount.Add(1)
		w.WriteHeader(200)
		io.WriteString(w, `{"ok":true}`)
	})
	cfg, token := testConfig(t,
		anthropicProvider("a", "http://127.0.0.1:1", 1), // 连接拒绝
		anthropicProvider("b", upB.server.URL, 2),
	)
	s := New(cfg, nil, nil)
	// 连续 3 次请求把 a 打开熔断
	for i := 0; i < 3; i++ {
		rec := doRequest(s, token, "/v1/messages", `{"model":"m"}`)
		if rec.Code != 200 {
			t.Fatalf("第 %d 次应经 b 成功: %d", i+1, rec.Code)
		}
	}
	br, _ := s.registry.Breaker("a")
	if br == nil || br.State() != "open" {
		t.Fatalf("a 应处于 open: %+v", br)
	}
	// 第 4 次请求：a 熔断被跳过（零连接），直接 b
	rec := doRequest(s, token, "/v1/messages", `{"model":"m"}`)
	if rec.Code != 200 {
		t.Fatalf("第 4 次应成功: %d", rec.Code)
	}
	snap := s.registry.Snapshot()
	for _, e := range snap {
		if e.Name == "a" && e.Requests != 3 {
			t.Errorf("a 请求数 = %d，应为 3（熔断后零连接）", e.Requests)
		}
	}
	if bCount.Load() != 4 {
		t.Errorf("b 次数 = %d", bCount.Load())
	}
}

func TestStickyPreferredBeatsPriority(t *testing.T) {
	var bCount atomic.Int64
	upA := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("优先级更高但不该被首选")
		w.WriteHeader(200)
	})
	upB := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		bCount.Add(1)
		w.WriteHeader(200)
		io.WriteString(w, `{"ok":"b"}`)
	})
	pb := anthropicProvider("b", upB.server.URL, 9)
	pb.Preferred = true
	cfg, token := testConfig(t,
		anthropicProvider("a", upA.server.URL, 1),
		pb,
	)
	s := New(cfg, nil, nil)
	rec := doRequest(s, token, "/v1/messages", `{"model":"m"}`)
	if rec.Code != 200 || bCount.Load() != 1 {
		t.Fatalf("粘性首选应走 b: %d bCount=%d", rec.Code, bCount.Load())
	}
}

func TestBodyOverLimit413(t *testing.T) {
	var hits atomic.Int64
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) { hits.Add(1) })
	cfg, token := testConfig(t, anthropicProvider("a", up.server.URL, 1))
	s := New(cfg, nil, nil)

	old := maxBody
	maxBody = 16
	defer func() { maxBody = old }()

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m","pad":"0123456789abcdefghijk"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 413 {
		t.Fatalf("超限应 413: %d %s", rec.Code, rec.Body.String())
	}
	if hits.Load() != 0 {
		t.Error("超限请求不应触达上游")
	}
}

func TestCountTokensForwardAndEstimate(t *testing.T) {
	t.Run("anthropic上游转发", func(t *testing.T) {
		up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/messages/count_tokens" {
				t.Errorf("路径 = %s", r.URL.Path)
			}
			w.WriteHeader(200)
			io.WriteString(w, `{"input_tokens":42}`)
		})
		cfg, token := testConfig(t, anthropicProvider("a", up.server.URL, 1))
		s := New(cfg, nil, nil)
		rec := doRequest(s, token, "/v1/messages/count_tokens", `{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "42") {
			t.Fatalf("应转发上游计数: %d %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("仅chat上游本地估算", func(t *testing.T) {
		up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
			t.Error("count_tokens 不应转发 chat 上游")
		})
		cfg, token := testConfig(t, config.Provider{
			Name: "c", Protocol: config.ProtocolOpenAIChat, BaseURL: up.server.URL, APIKey: "k", Priority: 1, Enabled: true,
		})
		s := New(cfg, nil, nil)
		body := `{"model":"m","messages":[{"role":"user","content":"你好世界"}]}`
		rec := doRequest(s, token, "/v1/messages/count_tokens", body)
		if rec.Code != 200 {
			t.Fatalf("估算应 200: %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"input_tokens":`) {
			t.Fatalf("缺少 input_tokens: %s", rec.Body.String())
		}
	})
	t.Run("上游404兜底估算", func(t *testing.T) {
		up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(404)
		})
		cfg, token := testConfig(t, anthropicProvider("a", up.server.URL, 1))
		s := New(cfg, nil, nil)
		rec := doRequest(s, token, "/v1/messages/count_tokens", `{"model":"m","messages":[]}`)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"input_tokens":`) {
			t.Fatalf("404 后应本地估算: %d %s", rec.Code, rec.Body.String())
		}
	})
}
