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

// E2E：完整网关 + 双假上游。primary（chat 协议）前 2 次 529 后恢复；
// anthropic 客户端与 responses 客户端各自走通 failover 与翻译。

const e2eAnthropicReq = `{"model":"claude-x","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hi"}]}`

const e2eChatSSE = `data: {"model":"gpt-5.2","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{"content":"OK"},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":null}],"usage":{"prompt_tokens":3,"completion_tokens":2}}

data: [DONE]
`

const e2eAnthropicSSE = `event: message_start
data: {"type":"message_start","message":{"model":"claude-x","usage":{"input_tokens":5,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"GOOD"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}

event: message_stop
data: {"type":"message_stop"}`

func TestE2EFailoverBothClients(t *testing.T) {
	var primaryHits, backupHits atomic.Int64
	primary := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		n := primaryHits.Add(1)
		if n <= 2 { // 前 2 次 529
			w.WriteHeader(529)
			io.WriteString(w, `{"error":{"message":"overloaded"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		io.WriteString(w, e2eChatSSE)
	})
	backup := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		backupHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		io.WriteString(w, e2eAnthropicSSE)
	})
	cfg, token := testConfig(t,
		config.Provider{Name: "primary-chat", Protocol: config.ProtocolOpenAIChat, BaseURL: primary.server.URL, APIKey: "k1", Priority: 1, Enabled: true},
		anthropicProvider("backup", backup.server.URL, 2),
	)
	gw := httptest.NewServer(New(cfg, nil, nil).Handler())
	t.Cleanup(gw.Close)

	post := func(path, body string) (int, string) {
		req, _ := http.NewRequest("POST", gw.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(out)
	}

	// 第 1 次：primary 529 → backup（anthropic 协议）翻译为 anthropic SSE 给客户端
	code, body := post("/v1/messages", e2eAnthropicReq)
	if code != 200 || !strings.Contains(body, "text_delta") || !strings.Contains(body, "GOOD") {
		t.Fatalf("anthropic 客户端 failover 失败: %d %s", code, body)
	}
	if backupHits.Load() != 1 {
		t.Fatalf("backup 应接 1 次, got %d", backupHits.Load())
	}

	// 第 2 次：responses 客户端 → primary 529 → backup 翻译为 responses SSE
	code, body = post("/v1/responses", `{"model":"gpt-5.2-codex","stream":true,"input":"hi"}`)
	if code != 200 || !strings.Contains(body, "response.created") || !strings.Contains(body, `"delta":"GOOD"`) {
		t.Fatalf("responses 客户端 failover 失败: %d %s", code, body)
	}
	if primaryHits.Load() != 2 || backupHits.Load() != 2 {
		t.Fatalf("计数 primary=%d backup=%d", primaryHits.Load(), backupHits.Load())
	}

	// 第 3 次：primary 恢复（chat 协议），anthropic 客户端直接翻译 chat SSE
	code, body = post("/v1/messages", e2eAnthropicReq)
	if code != 200 || !strings.Contains(body, `"text":"OK"`) || !strings.Contains(body, "message_stop") {
		t.Fatalf("primary 恢复后应走 primary 并翻译: %d %s", code, body)
	}
	if primaryHits.Load() != 3 {
		t.Fatalf("primary 应接第 3 次, got %d", primaryHits.Load())
	}
}

// E2E：SSE 首字节低延迟（上游分两块输出，第一块应在第二块放行前到达）。
func TestE2EStreamNoBuffering(t *testing.T) {
	release := make(chan struct{})
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		io.WriteString(w, "event: message_start\ndata: {}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-release
		io.WriteString(w, "event: message_stop\ndata: {}\n\n")
	})
	cfg, token := testConfig(t, anthropicProvider("a", up.server.URL, 1))
	gw := httptest.NewServer(New(cfg, nil, nil).Handler())
	t.Cleanup(gw.Close)

	req, _ := http.NewRequest("POST", gw.URL+"/v1/messages", strings.NewReader(e2eAnthropicReq))
	req.Header.Set("Authorization", "Bearer "+token)
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 256)
	n, _ := resp.Body.Read(buf)
	first := string(buf[:n])
	elapsed := time.Since(start)
	close(release)
	if !strings.Contains(first, "message_start") {
		t.Fatalf("首块未先于后续到达: %q", first)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("首块延迟 %v（存在聚合缓冲）", elapsed)
	}
	io.Copy(io.Discard, resp.Body)
}
