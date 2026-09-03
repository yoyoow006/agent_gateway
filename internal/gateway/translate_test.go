package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent_gateway/internal/config"
)

// anthropic 客户端请求（流式，含工具）
const anthropicStreamReq = `{"model":"claude-x","max_tokens":100,"stream":true,"system":"S","messages":[{"role":"user","content":"查天气"}],"tools":[{"name":"get_weather","description":"d","input_schema":{"type":"object"}}]}`

// chat 上游的流式脚本：文本 + 工具调用 + usage
const chatUpstreamSSE = `data: {"choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{"content":"答案"},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_9","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":\"BJ\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":null}],"usage":{"prompt_tokens":11,"completion_tokens":5}}

data: [DONE]
`

// responses 上游的流式脚本
const responsesUpstreamSSE = `event: response.created
data: {"type":"response.created","response":{"id":"resp_9","model":"gpt-5.2","status":"in_progress"}}

event: output_item.added
data: {"type":"output_item.added","output_index":0,"item":{"type":"message","id":"msg_9","role":"assistant","content":[]}}

event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_9","output_index":0,"content_index":0,"delta":"回复内容"}

event: output_item.done
data: {"type":"output_item.done","output_index":0,"item":{"type":"message","status":"completed"}}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_9","status":"completed","usage":{"input_tokens":7,"output_tokens":3}}}`

// anthropic 上游的流式脚本（供 responses 客户端用）
const anthropicUpstreamSSE = `event: message_start
data: {"type":"message_start","message":{"model":"claude-x","usage":{"input_tokens":9,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"文本"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}

event: message_stop
data: {"type":"message_stop"}`

// responses 客户端请求（含工具）
const responsesStreamReq = `{"model":"gpt-5.2-codex","stream":true,"max_output_tokens":256,"instructions":"S","input":[{"type":"message","role":"user","content":"查天气"}],"tools":[{"type":"function","name":"get_weather","parameters":{"type":"object"}}]}`

func chatUpstream(t *testing.T, sse string) *upstream {
	return newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("chat 上游路径 = %s", r.URL.Path)
		}
		if !strings.Contains(r.Header.Get("Authorization"), "Bearer sk-upstream") {
			t.Error("chat 上游应使用 Bearer 认证")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		io.WriteString(w, sse)
	})
}

func anthropicUpstreamServe(t *testing.T, sse string) *upstream {
	return newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("anthropic 上游路径 = %s", r.URL.Path)
		}
		if r.Header.Get("X-Api-Key") != "sk-upstream" {
			t.Error("anthropic 上游应使用 x-api-key")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		io.WriteString(w, sse)
	})
}

func responsesUpstream(t *testing.T, sse string) *upstream {
	return newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("responses 上游路径 = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		io.WriteString(w, sse)
	})
}

func streamViaGateway(t *testing.T, prov config.Provider, path, body string) string {
	t.Helper()
	cfg, token := testConfig(t, prov)
	gw := httptest.NewServer(New(cfg, nil, nil).Handler())
	t.Cleanup(gw.Close)
	req, _ := http.NewRequest("POST", gw.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("客户端应收到 SSE, content-type=%q", ct)
	}
	out, _ := io.ReadAll(resp.Body)
	return string(out)
}

func TestTranslateAnthropicClientToChatUpstream(t *testing.T) {
	up := chatUpstream(t, chatUpstreamSSE)
	prov := config.Provider{Name: "c", Protocol: config.ProtocolOpenAIChat, BaseURL: up.server.URL, APIKey: "sk-upstream", Priority: 1, Enabled: true}
	out := streamViaGateway(t, prov, "/v1/messages", anthropicStreamReq)
	for _, want := range []string{
		"event: message_start", `"model":"`, "content_block_start",
		`"type":"tool_use"`, `"name":"get_weather"`, `"partial_json":"{\"city\":\"BJ\"}"`,
		`"stop_reason":"tool_use"`, `"output_tokens":5`, "event: message_stop",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("客户端 SSE 缺少 %q\n%s", want, out)
		}
	}
}

func TestTranslateAnthropicClientToResponsesUpstream(t *testing.T) {
	up := responsesUpstream(t, responsesUpstreamSSE)
	prov := config.Provider{Name: "r", Protocol: config.ProtocolOpenAIResponses, BaseURL: up.server.URL, APIKey: "sk-upstream", Priority: 1, Enabled: true}
	out := streamViaGateway(t, prov, "/v1/messages", anthropicStreamReq)
	for _, want := range []string{
		"event: message_start", "text_delta", `"text":"回复内容"`,
		`"input_tokens":7`, `"output_tokens":3`, "event: message_stop",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("客户端 SSE 缺少 %q\n%s", want, out)
		}
	}
}

func TestTranslateResponsesClientToChatUpstream(t *testing.T) {
	up := chatUpstream(t, chatUpstreamSSE)
	prov := config.Provider{Name: "c", Protocol: config.ProtocolOpenAIChat, BaseURL: up.server.URL, APIKey: "sk-upstream", Priority: 1, Enabled: true}
	out := streamViaGateway(t, prov, "/v1/responses", responsesStreamReq)
	for _, want := range []string{
		"event: response.created", "event: response.output_text.delta", `"delta":"答案"`,
		`"type":"function_call"`, `"call_id":"call_9"`,
		"response.function_call_arguments.delta", `"input_tokens":11`,
		"event: response.completed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("客户端 SSE 缺少 %q\n%s", want, out)
		}
	}
}

func TestTranslateResponsesClientToAnthropicUpstream(t *testing.T) {
	up := anthropicUpstreamServe(t, anthropicUpstreamSSE)
	prov := anthropicProvider("a", up.server.URL, 1)
	out := streamViaGateway(t, prov, "/v1/responses", responsesStreamReq)
	for _, want := range []string{
		"event: response.created", "event: response.output_text.delta", `"delta":"文本"`,
		`"input_tokens":9`, `"output_tokens":4`, "event: response.completed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("客户端 SSE 缺少 %q\n%s", want, out)
		}
	}
}

func TestTranslateUpstream401ToClientProtocol(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, `{"error":{"message":"bad key"}}`)
	})
	cfg, token := testConfig(t, config.Provider{Name: "c", Protocol: config.ProtocolOpenAIChat, BaseURL: up.server.URL, APIKey: "sk-upstream", Priority: 1, Enabled: true})
	s := New(cfg, nil, nil)
	rec := doRequest(s, token, "/v1/messages", `{"model":"m","max_tokens":1}`)
	if rec.Code != 401 {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	// anthropic 客户端应收到 anthropic 格式错误体，而不是 chat 格式
	if !strings.Contains(rec.Body.String(), `"type":"error"`) || !strings.Contains(rec.Body.String(), "bad key") {
		t.Fatalf("错误体应翻译为 anthropic 格式: %s", rec.Body.String())
	}
}

// responses 客户端请求（含 Codex additional_tools 工具编排）
const responsesAdditionalToolsReq = `{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"additional_tools","role":"developer","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec","description":"Run JavaScript code"}]}]},{"type":"message","role":"user","content":"干活"}]}`

// chat 上游流式脚本：模型调用 custom 工具 functions.exec
const chatCustomToolSSE = `data: {"choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_c1","type":"function","function":{"name":"functions.exec","arguments":""}}]},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"code\":\"return 1;\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":null}],"usage":{"prompt_tokens":9,"completion_tokens":4}}

data: [DONE]
`

// TestTranslateCustomToolCallResponsesClientToChatUpstream 锁定网关 custom 名单
// 打标接线：additional_tools 里的 custom 工具经 chat 上游回叫后还原为 custom_tool_call。
func TestTranslateCustomToolCallResponsesClientToChatUpstream(t *testing.T) {
	up := chatUpstream(t, chatCustomToolSSE)
	prov := config.Provider{Name: "c", Protocol: config.ProtocolOpenAIChat, BaseURL: up.server.URL, APIKey: "sk-upstream", Priority: 1, Enabled: true}
	out := streamViaGateway(t, prov, "/v1/responses", responsesAdditionalToolsReq)
	for _, want := range []string{
		`"type":"custom_tool_call"`, `"name":"functions.exec"`, `"call_id":"call_c1"`,
		`"input":"return 1;"`, "event: response.completed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("客户端 SSE 缺少 %q\n%s", want, out)
		}
	}
	if strings.Contains(out, `"type":"function_call"`) {
		t.Errorf("custom 工具不应编码为 function_call:\n%s", out)
	}
}
