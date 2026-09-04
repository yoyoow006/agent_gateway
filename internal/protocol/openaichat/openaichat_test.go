package openaichat

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"agent_gateway/internal/protocol"
)

const goldenRequest = `{
  "model": "gpt-5.2",
  "messages": [
    {"role": "system", "content": "你是助手"},
    {"role": "user", "content": [
      {"type": "text", "text": "看图"},
      {"type": "image_url", "image_url": {"url": "data:image/png;base64,aWNvbg=="}}
    ]},
    {"role": "assistant", "content": "调用工具", "tool_calls": [
      {"id": "call_01", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"北京\"}"}}
    ]},
    {"role": "tool", "tool_call_id": "call_01", "content": "晴"},
    {"role": "user", "content": "继续"}
  ],
  "tools": [
    {"type": "function", "function": {"name": "get_weather", "description": "查天气", "parameters": {"type": "object", "properties": {"city": {"type": "string"}}}}}
  ],
  "tool_choice": "required",
  "max_tokens": 512,
  "temperature": 0.3,
  "stop": ["END"],
  "stream": true
}`

func TestParseRequest(t *testing.T) {
	req, err := ParseRequest([]byte(goldenRequest))
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "gpt-5.2" || req.MaxTokens != 512 || len(req.System) != 1 || req.System[0] != "你是助手" {
		t.Errorf("基础字段 = %+v", req)
	}
	if len(req.Turns) != 4 {
		t.Fatalf("turns = %d: %+v", len(req.Turns), req.Turns)
	}
	user0 := req.Turns[0]
	if user0.Role != "user" || user0.Parts[1].Kind != protocol.KindImage ||
		user0.Parts[1].ImageMediaType != "image/png" || user0.Parts[1].ImageData != "aWNvbg==" {
		t.Errorf("turn0 = %+v", user0.Parts)
	}
	asst := req.Turns[1]
	if asst.Role != "assistant" || asst.Parts[0].Text != "调用工具" ||
		asst.Parts[1].Kind != protocol.KindToolUse || asst.Parts[1].ToolCallID != "call_01" ||
		asst.Parts[1].ToolName != "get_weather" {
		t.Errorf("turn1 = %+v", asst.Parts)
	}
	var in map[string]any
	json.Unmarshal([]byte(asst.Parts[1].ToolInputJSON), &in)
	if in["city"] != "北京" {
		t.Errorf("tool input = %v", in)
	}
	tr := req.Turns[2].Parts[0]
	if req.Turns[2].Role != "user" || tr.Kind != protocol.KindToolResult || tr.ToolCallID != "call_01" || tr.ToolResult != "晴" {
		t.Errorf("turn2 = %+v", req.Turns[2])
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "get_weather" {
		t.Errorf("tools = %+v", req.Tools)
	}
	if req.ToolChoice.Mode != protocol.ToolChoiceRequired || req.Temperature == nil || *req.Temperature != 0.3 {
		t.Errorf("tool_choice/temp = %+v/%v", req.ToolChoice, req.Temperature)
	}
	if len(req.Stop) != 1 || req.Stop[0] != "END" || !req.Stream {
		t.Errorf("stop/stream = %+v/%v", req.Stop, req.Stream)
	}
}

func TestBuildRequestRoundTrip(t *testing.T) {
	req, err := ParseRequest([]byte(goldenRequest))
	if err != nil {
		t.Fatal(err)
	}
	path, hdr, body, err := BuildRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/chat/completions" {
		t.Errorf("path = %q", path)
	}
	if hdr.Get("Content-Type") != "application/json" {
		t.Errorf("content-type = %q", hdr.Get("Content-Type"))
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["stream_options"]; !ok {
		t.Error("流式请求应带 stream_options.include_usage")
	}
	req2, err := ParseRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if req2.Model != req.Model || len(req2.Turns) != 4 || len(req2.Tools) != 1 ||
		req2.MaxTokens != 512 || req2.ToolChoice.Mode != protocol.ToolChoiceRequired {
		t.Errorf("round trip drift: %+v", req2)
	}
	if req2.Turns[2].Parts[0].ToolResult != "晴" || req2.Turns[2].Parts[0].ToolCallID != "call_01" {
		t.Errorf("tool result drift: %+v", req2.Turns[2])
	}
}

func TestParseResponseAndBuild(t *testing.T) {
	body := `{"id":"c1","model":"gpt-5.2","choices":[{"index":0,"message":{
    "content":"你好",
    "tool_calls":[{"id":"call_02","type":"function","function":{"name":"search","arguments":"{\"q\":\"x\"}"}}]
  },"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":2}}}`
	resp, err := ParseResponse(200, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != protocol.StopToolUse || resp.Usage.Input != 7 || resp.Usage.Output != 3 || resp.Usage.CacheRead != 2 {
		t.Errorf("resp = %+v", resp)
	}
	if len(resp.Parts) != 2 || resp.Parts[0].Kind != protocol.KindText || resp.Parts[1].ToolName != "search" {
		t.Errorf("parts = %+v", resp.Parts)
	}
	status, out := BuildResponse(resp)
	resp2, err := ParseResponse(status, out)
	if err != nil {
		t.Fatal(err)
	}
	if resp2.StopReason != protocol.StopToolUse || len(resp2.Parts) != 2 {
		t.Errorf("build/parse drift: %+v", resp2)
	}
}

const goldenSSE = `data: {"id":"c1","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"c1","choices":[{"index":0,"delta":{"content":"你"},"finish_reason":null}]}

data: {"id":"c1","choices":[{"index":0,"delta":{"content":"好"},"finish_reason":null}]}

data: {"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_03","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}

data: {"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]},"finish_reason":null}]}

data: {"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"北京\"}"}}]},"finish_reason":null}]}

data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":null}],"usage":{"prompt_tokens":9,"completion_tokens":6}}

data: [DONE]
`

func collect(t *testing.T, dec protocol.StreamDecoder) []protocol.Event {
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

func TestStreamDecoder(t *testing.T) {
	evs := collect(t, NewStreamDecoder(strings.NewReader(goldenSSE)))
	kinds := ""
	for _, e := range evs {
		kinds += string(e.Kind) + ","
	}
	// 首个 chunk 合成起始事件 → 文本块懒开启 → 增量×2 → 工具块前关闭文本块 → 工具增量×2 → 收尾关闭 → 结束
	want := "stream_start,block_start,text_delta,text_delta,block_stop,block_start,tool_call_delta,tool_call_delta,block_stop,stream_end,"
	if kinds != want {
		t.Fatalf("kinds = %s\nwant %s", kinds, want)
	}
	toolStart := evs[5]
	if toolStart.Block.Kind != protocol.KindToolUse || toolStart.Block.ToolCallID != "call_03" || toolStart.Block.ToolName != "get_weather" {
		t.Errorf("tool start = %+v", toolStart)
	}
	end := evs[len(evs)-1]
	if end.StopReason != protocol.StopToolUse || end.Usage.Input != 9 || end.Usage.Output != 6 {
		t.Errorf("end = %+v", end)
	}
}

// TestStreamDecoderToolCallMissingIndexDistinctIDs 验证上游不发送 index 但 ID 唯一时，
// 解码器按 cc-switch 风格启发式分配独立 IR 块而非默认坍缩冲突。
const goldenSSEMissingIndexDistinctIDs = `data: {"id":"c1","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_a","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}

data: {"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_a","function":{"arguments":"{\"city\":\"北京\"}"}}]},"finish_reason":null}]}

data: {"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_b","type":"function","function":{"name":"get_time","arguments":""}}]},"finish_reason":null}]}

data: {"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_b","function":{"arguments":"{\"tz\":\"Asia\"}"}}]},"finish_reason":null}]}

data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]
`

func TestStreamDecoderToolCallMissingIndexDistinctIDs(t *testing.T) {
	evs := collect(t, NewStreamDecoder(strings.NewReader(goldenSSEMissingIndexDistinctIDs)))
	// 应合成 2 个独立 IR 工具块：每个块有 start + delta + stop
	var toolStarts []protocol.Event
	var toolStops []int
	for _, e := range evs {
		if e.Kind == protocol.EvBlockStart && e.Block.Kind == protocol.KindToolUse {
			toolStarts = append(toolStarts, e)
		}
		if e.Kind == protocol.EvBlockStop {
			toolStops = append(toolStops, e.Index)
		}
	}
	if len(toolStarts) != 2 {
		t.Fatalf("应合成 2 个独立 IR 工具块，实际 %d:\n%v", len(toolStarts), toolStarts)
	}
	if toolStarts[0].Block.ToolCallID != "call_a" || toolStarts[0].Block.ToolName != "get_weather" {
		t.Errorf("首 tool 应为 call_a/get_weather，实际 %+v", toolStarts[0])
	}
	if toolStarts[1].Block.ToolCallID != "call_b" || toolStarts[1].Block.ToolName != "get_time" {
		t.Errorf("次 tool 应为 call_b/get_time，实际 %+v", toolStarts[1])
	}
	if toolStarts[0].Index == toolStarts[1].Index {
		t.Errorf("两 tool 应分配不同 IR 块索引，实际均为 %d", toolStarts[0].Index)
	}
	if len(toolStops) < 2 || toolStops[0] != toolStarts[0].Index || toolStops[1] != toolStarts[1].Index {
		t.Errorf("每个 tool 应有独立 block_stop，实际 stops=%v", toolStops)
	}
}

// TestStreamDecoderToolCallMissingIndexCollapsesToLast 验证缺 index 且 ID 为空的 delta
// 归属到"最后已知 IR 工具块"而非默认坍缩到 chatIdx=0（首个工具块）——
// 旧实现 chatIdx 默认 0 会错误归属到 call_a；新启发式应归属到最近一个 tool（call_b）。
const goldenSSEMissingIndexCollapse = `data: {"id":"c1","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"f1","arguments":""}}]},"finish_reason":null}]}

data: {"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"f2","arguments":""}}]},"finish_reason":null}]}

data: {"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"function":{"arguments":"{\"x\":1}"}}]},"finish_reason":null}]}

data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]
`

func TestStreamDecoderToolCallMissingIndexCollapsesToLast(t *testing.T) {
	evs := collect(t, NewStreamDecoder(strings.NewReader(goldenSSEMissingIndexCollapse)))
	var toolStarts []protocol.Event
	var deltas []protocol.Event
	for _, e := range evs {
		if e.Kind == protocol.EvBlockStart && e.Block.Kind == protocol.KindToolUse {
			toolStarts = append(toolStarts, e)
		}
		if e.Kind == protocol.EvToolCallDelta {
			deltas = append(deltas, e)
		}
	}
	if len(toolStarts) != 2 {
		t.Fatalf("应合成 2 个独立 IR 工具块（call_a + call_b），实际 %d", len(toolStarts))
	}
	if toolStarts[0].Block.ToolCallID != "call_a" {
		t.Errorf("首 IR 工具块应为 call_a，实际 %+v", toolStarts[0])
	}
	if toolStarts[1].Block.ToolCallID != "call_b" {
		t.Errorf("次 IR 工具块应为 call_b，实际 %+v", toolStarts[1])
	}
	if len(deltas) != 1 {
		t.Fatalf("应 1 个 tool delta（缺 index/ID 续接），实际 %d", len(deltas))
	}
	if deltas[0].Index != toolStarts[1].Index {
		t.Errorf("续接 delta 应归属最后 IR 块（call_b index=%d），实际归属 index=%d",
			toolStarts[1].Index, deltas[0].Index)
	}
}

func TestStreamEncoderRoundTrip(t *testing.T) {
	irEvents := []protocol.Event{
		{Kind: protocol.EvStreamStart, Model: "gpt-5.2"},
		{Kind: protocol.EvBlockStart, Index: 0, Block: protocol.Part{Kind: protocol.KindText}},
		{Kind: protocol.EvTextDelta, Index: 0, Text: "你好"},
		{Kind: protocol.EvBlockStop, Index: 0},
		{Kind: protocol.EvStreamEnd, StopReason: protocol.StopEndTurn, Usage: protocol.Usage{Input: 9, Output: 6}},
	}
	var sb strings.Builder
	enc := NewStreamEncoder(&sb)
	for _, ev := range irEvents {
		if err := enc.Encode(ev); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Finish(); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	if !strings.Contains(out, `"role":"assistant"`) || !strings.Contains(out, `"content":"你好"`) ||
		!strings.Contains(out, `"finish_reason":"stop"`) || !strings.Contains(out, `"prompt_tokens":9`) ||
		!strings.Contains(out, "[DONE]") {
		t.Errorf("encoder output missing pieces:\n%s", out)
	}
	// 回环
	got := collect(t, NewStreamDecoder(strings.NewReader(out)))
	var text strings.Builder
	for _, ev := range got {
		if ev.Kind == protocol.EvTextDelta {
			text.WriteString(ev.Text)
		}
	}
	if text.String() != "你好" {
		t.Errorf("round trip text = %q", text.String())
	}
}

func TestRemoteImageURLError(t *testing.T) {
	_, err := ParseRequest([]byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://x/y.png"}}]}]}`))
	var unmapped *protocol.ErrUnmappedBlock
	if err == nil {
		t.Fatal("远程图片 URL 应返回 ErrUnmappedBlock")
	}
	if !errorsAs(err, &unmapped) {
		t.Fatalf("err = %v, want ErrUnmappedBlock", err)
	}
}

func errorsAs(err error, target *(*protocol.ErrUnmappedBlock)) bool {
	for err != nil {
		if e, ok := err.(*protocol.ErrUnmappedBlock); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestErrorMapping(t *testing.T) {
	msg := ParseError(429, []byte(`{"error":{"message":"Rate limited","type":"rate_limit"}}`))
	if !strings.Contains(msg, "Rate limited") {
		t.Errorf("ParseError = %q", msg)
	}
	body := BuildError(429, "限流")
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Error.Message != "限流" {
		t.Errorf("BuildError = %s err=%v", body, err)
	}
}

// TestCustomToolDefBuild 锁定 custom 型 ToolDef 合成 {code} schema 的构建契约。
func TestCustomToolDefBuild(t *testing.T) {
	req := protocol.Request{
		Model: "m",
		Turns: []protocol.Turn{{Role: "user", Parts: []protocol.Part{protocol.Text("hi")}}},
		Tools: []protocol.ToolDef{
			{Name: "functions.exec", Description: "Run JavaScript code", Custom: true},
			{Name: "functions.wait", Description: "Wait", Schema: []byte(`{"type":"object","properties":{"seconds":{"type":"number"}}}`)},
		},
	}
	_, _, body, err := BuildRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	var w struct {
		Tools []struct {
			Function struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &w); err != nil {
		t.Fatal(err)
	}
	if len(w.Tools) != 2 {
		t.Fatalf("tools = %d", len(w.Tools))
	}
	for _, tt := range w.Tools {
		if tt.Function.Name == "functions.exec" {
			if !strings.Contains(string(tt.Function.Parameters), `"code"`) || !strings.Contains(string(tt.Function.Parameters), "required") {
				t.Errorf("exec 应合成 code schema: %s", tt.Function.Parameters)
			}
			if tt.Function.Description != "Run JavaScript code" {
				t.Errorf("exec description 应透传: %q", tt.Function.Description)
			}
		}
		if tt.Function.Name == "functions.wait" && !strings.Contains(string(tt.Function.Parameters), "seconds") {
			t.Errorf("wait schema 应透传: %s", tt.Function.Parameters)
		}
	}
}
