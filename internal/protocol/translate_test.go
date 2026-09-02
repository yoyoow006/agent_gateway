package protocol_test

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"agent_gateway/internal/protocol"
	"agent_gateway/internal/protocol/anthropic"
	"agent_gateway/internal/protocol/openaichat"
	"agent_gateway/internal/protocol/openairesponses"
)

// 四组客户端×供应商组合的黄金样例：请求→IR→目标协议请求体，
// 以及上游响应/SSE→IR→客户端协议格式的保真断言。

var (
	anthropicCodec = anthropic.DefaultCodec
	chatCodec      = openaichat.DefaultCodec
	responsesCodec = openairesponses.DefaultCodec

	anthropicReqBody = []byte(`{
  "model": "claude-sonnet-5", "max_tokens": 512, "stream": true,
  "system": [{"type":"text","text":"系统提示","cache_control":{"type":"ephemeral"}}],
  "messages": [
    {"role":"user","content":[{"type":"text","text":"查天气"}]},
    {"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"北京"}}]},
    {"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"晴"}]},
    {"role":"user","content":"谢谢，配张图","cache_control":{}}
  ],
  "tools":[{"name":"get_weather","description":"查","input_schema":{"type":"object"}}]
}`)

	responsesReqBody = []byte(`{
  "model": "gpt-5.2-codex", "stream": true, "max_output_tokens": 512,
  "instructions": "系统提示",
  "input": [
    {"type":"message","role":"user","content":"查天气"},
    {"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"北京\"}"},
    {"type":"function_call_output","call_id":"call_1","output":"晴"}
  ],
  "tools":[{"type":"function","name":"get_weather","description":"查","parameters":{"type":"object"}}]
}`)

	// chat 上游的非流式响应（含工具调用与 usage）
	chatRespBody = []byte(`{"id":"c1","model":"gpt-5.2","choices":[{"index":0,"message":{"content":"答案","tool_calls":[{"id":"call_2","type":"function","function":{"name":"search","arguments":"{\"q\":\"x\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":4}}`)

	// anthropic 上游的流式 SSE
	anthropicSSE = `event: message_start
data: {"type":"message_start","message":{"model":"claude-sonnet-5","usage":{"input_tokens":20,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"回复"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":6}}

event: message_stop
data: {"type":"message_stop"}`

	// chat 上游的流式 SSE
	chatSSE = `data: {"choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{"content":"回复"},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":null}],"usage":{"prompt_tokens":20,"completion_tokens":6}}

data: [DONE]
`
)

type pair struct {
	name         string
	client, prov protocol.Codec
	clientBody   []byte
}

var pairs = []pair{
	{"anthropic→chat", anthropicCodec, chatCodec, anthropicReqBody},
	{"anthropic→responses", anthropicCodec, responsesCodec, anthropicReqBody},
	{"responses→chat", responsesCodec, chatCodec, responsesReqBody},
	{"responses→anthropic", responsesCodec, anthropicCodec, responsesReqBody},
}

func TestTranslateRequestPairs(t *testing.T) {
	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			ir, err := p.client.ParseRequest(p.clientBody)
			if err != nil {
				t.Fatal(err)
			}
			if ir.Model == "" || len(ir.System) != 1 || len(ir.Tools) != 1 {
				t.Fatalf("IR 基础字段异常: %+v", ir)
			}
			// 工具往返保留
			toolTurn := -1
			for i, turn := range ir.Turns {
				for _, part := range turn.Parts {
					if part.Kind == protocol.KindToolUse {
						toolTurn = i
					}
				}
			}
			if toolTurn < 0 {
				t.Fatal("IR 丢失工具调用")
			}
			_, _, outBody, err := p.prov.BuildRequest(ir)
			if err != nil {
				t.Fatal(err)
			}
			ir2, err := p.prov.ParseRequest(outBody)
			if err != nil {
				t.Fatalf("目标协议请求体解析失败: %v\n%s", err, outBody)
			}
			if ir2.Model != ir.Model || len(ir2.System) != 1 || len(ir2.Tools) != 1 || ir2.MaxTokens != ir.MaxTokens {
				t.Errorf("翻译后语义漂移: %+v", ir2)
			}
			// 工具调用与结果文本保留
			var sawToolUse, sawToolResult bool
			for _, turn := range ir2.Turns {
				for _, part := range turn.Parts {
					if part.Kind == protocol.KindToolUse {
						sawToolUse = true
						var in map[string]any
						if json.Unmarshal([]byte(part.ToolInputJSON), &in) != nil || in["city"] != "北京" {
							t.Errorf("工具参数漂移: %v", in)
						}
					}
					if part.Kind == protocol.KindToolResult && part.ToolResult == "晴" {
						sawToolResult = true
					}
				}
			}
			if !sawToolUse || !sawToolResult {
				t.Errorf("工具调用/结果丢失: use=%v result=%v turns=%+v", sawToolUse, sawToolResult, ir2.Turns)
			}
		})
	}
}

func TestTranslateResponseChatToBothClients(t *testing.T) {
	ir, err := chatCodec.ParseResponse(200, chatRespBody)
	if err != nil {
		t.Fatal(err)
	}
	if ir.StopReason != protocol.StopToolUse || ir.Usage.Input != 10 || ir.Usage.Output != 4 {
		t.Fatalf("IR = %+v", ir)
	}
	for _, client := range []protocol.Codec{anthropicCodec, responsesCodec} {
		status, body := client.BuildResponse(ir)
		if status != 200 {
			t.Errorf("%s 状态 = %d", client.Name(), status)
		}
		ir2, err := client.ParseResponse(status, body)
		if err != nil {
			t.Fatalf("%s: %v", client.Name(), err)
		}
		if ir2.StopReason != protocol.StopToolUse || len(ir2.Parts) != 2 || ir2.Usage.Input != 10 || ir2.Usage.Output != 4 {
			t.Errorf("%s 响应翻译漂移: %+v", client.Name(), ir2)
		}
		if ir2.Parts[0].Kind != protocol.KindText || ir2.Parts[0].Text != "答案" {
			t.Errorf("%s 文本漂移: %+v", client.Name(), ir2.Parts[0])
		}
		if ir2.Parts[1].Kind != protocol.KindToolUse || ir2.Parts[1].ToolName != "search" {
			t.Errorf("%s 工具漂移: %+v", client.Name(), ir2.Parts[1])
		}
	}
}

func drainEvents(t *testing.T, dec protocol.StreamDecoder) []protocol.Event {
	t.Helper()
	var evs []protocol.Event
	for {
		ev, err := dec.Next()
		if err == io.EOF {
			return evs
		}
		if err != nil {
			t.Fatal(err)
		}
		evs = append(evs, ev)
	}
}

func kindsOf(evs []protocol.Event) string {
	var sb strings.Builder
	for _, e := range evs {
		sb.WriteString(string(e.Kind))
		sb.WriteString(",")
	}
	return sb.String()
}

// 上游 SSE → IR → 客户端 SSE → 客户端再解码，事件序列等价。
func TestTranslateStreamPairs(t *testing.T) {
	cases := []struct {
		name      string
		provDec   func(io.Reader) protocol.StreamDecoder
		clientEnc func(io.Writer) protocol.StreamEncoder
		clientDec func(io.Reader) protocol.StreamDecoder
		upstream  string
		wantKinds string
	}{
		{
			name:      "anthropic上游→chat客户端",
			provDec:   anthropic.NewStreamDecoder,
			clientEnc: openaichat.NewStreamEncoder,
			clientDec: openaichat.NewStreamDecoder,
			upstream:  anthropicSSE,
			wantKinds: "stream_start,block_start,text_delta,block_stop,stream_end,",
		},
		{
			name:      "anthropic上游→responses客户端",
			provDec:   anthropic.NewStreamDecoder,
			clientEnc: openairesponses.NewStreamEncoder,
			clientDec: openairesponses.NewStreamDecoder,
			upstream:  anthropicSSE,
			wantKinds: "stream_start,block_start,text_delta,block_stop,stream_end,",
		},
		{
			name:      "chat上游→anthropic客户端",
			provDec:   openaichat.NewStreamDecoder,
			clientEnc: anthropic.NewStreamEncoder,
			clientDec: anthropic.NewStreamDecoder,
			upstream:  chatSSE,
			wantKinds: "stream_start,block_start,text_delta,block_stop,stream_end,",
		},
		{
			name:      "chat上游→responses客户端",
			provDec:   openaichat.NewStreamDecoder,
			clientEnc: openairesponses.NewStreamEncoder,
			clientDec: openairesponses.NewStreamDecoder,
			upstream:  chatSSE,
			wantKinds: "stream_start,block_start,text_delta,block_stop,stream_end,",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evs := drainEvents(t, tc.provDec(strings.NewReader(tc.upstream)))
			var sb strings.Builder
			enc := tc.clientEnc(&sb)
			for _, ev := range evs {
				if err := enc.Encode(ev); err != nil {
					t.Fatal(err)
				}
			}
			if err := enc.Finish(); err != nil {
				t.Fatal(err)
			}
			got := drainEvents(t, tc.clientDec(strings.NewReader(sb.String())))
			if k := kindsOf(got); k != tc.wantKinds {
				t.Fatalf("回环事件 = %s want %s\n输出:\n%s", k, tc.wantKinds, sb.String())
			}
			// 文本与 usage 端到端保留
			var text strings.Builder
			for _, ev := range got {
				if ev.Kind == protocol.EvTextDelta {
					text.WriteString(ev.Text)
				}
			}
			if text.String() != "回复" {
				t.Errorf("文本 = %q", text.String())
			}
			end := got[len(got)-1]
			if end.Kind != protocol.EvStreamEnd || end.Usage.Output != 6 {
				t.Errorf("结束事件 = %+v", end)
			}
		})
	}
}

func TestCacheControlDropWarning(t *testing.T) {
	var warnings []string
	prev := protocol.DropHook
	protocol.DropHook = func(detail string) { warnings = append(warnings, detail) }
	defer func() { protocol.DropHook = prev }()

	_, err := anthropicCodec.ParseRequest(anthropicReqBody)
	if err != nil {
		t.Fatal(err)
	}
	// system 一处 + 消息一处 = 至少一次告警（合并计数）
	if len(warnings) == 0 || !strings.Contains(warnings[0], "cache_control") {
		t.Errorf("期望 cache_control 丢弃告警，得到 %v", warnings)
	}
}

// MIN-05 回归：previous_response_id / 未知 input 条目触发降级告警。
func TestDropNotificationsForResponsesFields(t *testing.T) {
	var warnings []string
	prev := protocol.DropHook
	protocol.DropHook = func(detail string) { warnings = append(warnings, detail) }
	defer func() { protocol.DropHook = prev }()

	body := []byte(`{"model":"m","previous_response_id":"resp_old","input":[{"type":"message","role":"user","content":"hi"},{"type":"web_search_call","id":"ws_1"}]}`)
	if _, err := responsesCodec.ParseRequest(body); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "previous_response_id") {
		t.Errorf("previous_response_id 丢弃应有告警: %v", warnings)
	}
	if !strings.Contains(joined, "web_search_call") {
		t.Errorf("未知条目类型丢弃应有告警: %v", warnings)
	}
}

// MIN-06 回归：anthropic 构建请求时 thinking 历史块不回传（无 signature）。
func TestAnthropicBuildDropsThinkingHistory(t *testing.T) {
	req := protocol.Request{
		Model: "m", MaxTokens: 10,
		Turns: []protocol.Turn{
			{Role: "assistant", Parts: []protocol.Part{protocol.Thinking("思考中"), protocol.ToolUse("t1", "f", "{}")}},
			{Role: "user", Parts: []protocol.Part{protocol.ToolResult("t1", "ok", false)}},
		},
	}
	_, _, body, err := anthropicCodec.BuildRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"thinking"`) {
		t.Errorf("thinking 历史不应回传上游: %s", body)
	}
	if !strings.Contains(string(body), `"tool_use"`) || !strings.Contains(string(body), `"tool_result"`) {
		t.Errorf("工具块应保留: %s", body)
	}
}

// NEW-02 回归：仅 thinking 的 assistant 轮不产出空 text 块（会被上游 400）。
func TestAnthropicSkipsThinkingOnlyTurn(t *testing.T) {
	req := protocol.Request{
		Model: "m", MaxTokens: 10,
		Turns: []protocol.Turn{
			{Role: "user", Parts: []protocol.Part{protocol.Text("hi")}},
			{Role: "assistant", Parts: []protocol.Part{protocol.Thinking("只想未说")}},
			{Role: "user", Parts: []protocol.Part{protocol.Text("go")}},
		},
	}
	_, _, body, err := anthropicCodec.BuildRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"text":""`) {
		t.Errorf("不应产出空 text 块: %s", body)
	}
	var parsed struct {
		Messages []json.RawMessage `json:"messages"`
	}
	json.Unmarshal(body, &parsed)
	if len(parsed.Messages) != 2 {
		t.Errorf("thinking-only 轮应被跳过, messages=%d: %s", len(parsed.Messages), body)
	}
}

// NEW-04 回归：responses 顶层未映射字段触发降级告警。
func TestResponsesTopLevelUnknownFieldWarns(t *testing.T) {
	var warnings []string
	prev := protocol.DropHook
	protocol.DropHook = func(detail string) { warnings = append(warnings, detail) }
	defer func() { protocol.DropHook = prev }()

	body := []byte(`{"model":"m","input":"hi","reasoning":{"effort":"high"},"prompt_cache_key":"k1"}`)
	if _, err := responsesCodec.ParseRequest(body); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "reasoning") || !strings.Contains(joined, "prompt_cache_key") {
		t.Errorf("顶层字段丢弃应有告警: %v", warnings)
	}
}
