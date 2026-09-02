package gateway

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent_gateway/internal/config"
)

// upstream 模拟一个供应商。
type upstream struct {
	server *httptest.Server
}

type capturedReq struct {
	method, path, query, host string
	body                      []byte
	header                    http.Header
}

func newUpstream(t *testing.T, handler http.HandlerFunc) *upstream {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &upstream{server: srv}
}

func testConfig(t *testing.T, provs ...config.Provider) (*config.Config, string) {
	t.Helper()
	cfg := &config.Config{
		Gateway:    config.GatewayCfg{Listen: "127.0.0.1:0", DefaultToken: "agw-test-global"},
		AdminToken: "agw-test-admin",
		Providers:  provs,
		Projects:   map[string]config.ProjectProfile{},
	}
	cfg.RebuildTokenIndex()
	return cfg, "agw-test-global"
}

func anthropicProvider(name, base string, priority int) config.Provider {
	return config.Provider{Name: name, Protocol: config.ProtocolAnthropic, BaseURL: base, APIKey: "sk-upstream", Priority: priority, Enabled: true}
}

func TestUnknownToken401PerProtocol(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("未授权请求不应触达上游")
	})
	cfg, _ := testConfig(t, anthropicProvider("a", up.server.URL, 1))
	s := New(cfg, nil, nil)

	// anthropic 端点
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`)))
	rec.Header().Set("Authorization", "Bearer nope")
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("Authorization", "Bearer nope")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), `"type":"error"`) {
		t.Errorf("anthropic 401 = %d %s", rec.Code, rec.Body.String())
	}
	// openai 端点
	req = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer nope")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("openai 401 = %d %s", rec.Code, rec.Body.String())
	}
}

func TestModelsFromConfig(t *testing.T) {
	cfg, _ := testConfig(t, config.Provider{
		Name: "relay", Protocol: config.ProtocolOpenAIChat, BaseURL: "http://127.0.0.1:1", APIKey: "k", Enabled: true,
		ModelMap: map[string]string{"claude-x": "gpt-x", "claude-y": "gpt-y"},
	})
	s := New(cfg, nil, nil)
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer agw-test-global")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "claude-x") || !strings.Contains(rec.Body.String(), "claude-y") {
		t.Errorf("models = %d %s", rec.Code, rec.Body.String())
	}
}

func TestAdminEndpoints(t *testing.T) {
	cfg, _ := testConfig(t)
	s := New(cfg, nil, nil)
	h := s.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/__agw/healthz", nil))
	if rec.Code != 200 {
		t.Errorf("healthz = %d", rec.Code)
	}

	req := httptest.NewRequest("GET", "/__agw/metrics", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Errorf("metrics 无令牌 = %d", rec.Code)
	}
	req.Header.Set("Authorization", "Bearer agw-test-admin")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "providers") {
		t.Errorf("metrics = %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest("POST", "/__agw/reload", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Errorf("reload 错误令牌 = %d", rec.Code)
	}
}

func TestPassthroughByteFidelity(t *testing.T) {
	var captured capturedReq
	done := make(chan struct{})
	clientBody := `{"model":"claude-sonnet-5","max_tokens":100,"stream":false,"system":"保持原样","messages":[{"role":"user","content":"你好"}],"metadata":{"session_id":"abc"},"weird_field":[1,2,{"nested":true}]}`
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = capturedReq{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, host: r.Host, body: body, header: r.Header.Clone()}
		close(done)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		io.WriteString(w, `{"ok":true}`)
	})
	cfg, _ := testConfig(t, anthropicProvider("a", up.server.URL, 1))
	s := New(cfg, nil, nil)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(clientBody))
	req.Header.Set("Authorization", "Bearer agw-test-global")
	req.Header.Set("X-Api-Key", "agw-test-global")
	req.Header.Set("Anthropic-Version", "2023-06-01")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("上游未被调用: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Code != 200 || rec.Body.String() != `{"ok":true}` {
		t.Fatalf("响应 = %d %s", rec.Code, rec.Body.String())
	}
	if captured.path != "/v1/messages" {
		t.Errorf("上游路径 = %s", captured.path)
	}
	if !bytes.Equal(captured.body, []byte(clientBody)) {
		t.Errorf("透传字节不一致:\n got %s\nwant %s", captured.body, clientBody)
	}
	// 认证替换：x-api-key 为上游密钥，客户端令牌不外泄
	if captured.header.Get("X-Api-Key") != "sk-upstream" {
		t.Errorf("x-api-key = %q", captured.header.Get("X-Api-Key"))
	}
	if auth := captured.header.Get("Authorization"); auth != "" {
		t.Errorf("Authorization 应被移除: %q", auth)
	}
	if captured.header.Get("Anthropic-Version") != "2023-06-01" {
		t.Errorf("anthropic-version 丢失")
	}
	if !strings.HasSuffix(captured.host, strings.TrimPrefix(up.server.URL, "http://")) {
		t.Errorf("Host = %q", captured.host)
	}
}

func TestPassthroughModelMapRewritesOnlyModel(t *testing.T) {
	var captured capturedReq
	done := make(chan struct{})
	clientBody := `{"model":"claude-x","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}`
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = capturedReq{body: body}
		close(done)
		w.WriteHeader(200)
		io.WriteString(w, `{"ok":1}`)
	})
	prov := anthropicProvider("a", up.server.URL, 1)
	prov.ModelMap = map[string]string{"claude-x": "claude-x-relay"}
	cfg, _ := testConfig(t, prov)
	s := New(cfg, nil, nil)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(clientBody))
	req.Header.Set("Authorization", "Bearer agw-test-global")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("上游未被调用: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(string(captured.body), `"model":"claude-x-relay"`) {
		t.Errorf("model 未重写: %s", captured.body)
	}
	if !strings.Contains(string(captured.body), `"max_tokens":50`) || !strings.Contains(string(captured.body), `"content":"hi"`) {
		t.Errorf("其他字段被改动: %s", captured.body)
	}
}

func TestPassthroughSSEStreamFlushed(t *testing.T) {
	chunk1 := "event: content_block_delta\ndata: {\"delta\":{\"text\":\"你\"}}\n\n"
	chunk2 := "event: content_block_delta\ndata: {\"delta\":{\"text\":\"好\"}}\n\n"
	released := make(chan struct{})
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		io.WriteString(w, chunk1)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-released
		io.WriteString(w, chunk2)
	})
	cfg, token := testConfig(t, anthropicProvider("a", up.server.URL, 1))
	gw := httptest.NewServer(New(cfg, nil, nil).Handler())
	t.Cleanup(gw.Close)

	req, _ := http.NewRequest("POST", gw.URL+"/v1/messages", strings.NewReader(`{"model":"m","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	first := string(buf[:n])
	if !strings.Contains(first, "你") {
		t.Fatalf("首块应在上游放行前到达（证明逐写即冲刷）: %q", first)
	}
	close(released)
	rest, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(rest), "好") {
		t.Fatalf("后续块缺失: %q", rest)
	}
}
