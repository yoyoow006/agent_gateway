package anthropic

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"agent_gateway/internal/protocol"
)

const goldenRequest = `{
  "model": "claude-sonnet-5",
  "max_tokens": 1024,
  "system": [
    {"type": "text", "text": "你是助手"},
    {"type": "text", "text": "第二段", "cache_control": {"type": "ephemeral"}}
  ],
  "messages": [
    {"role": "user", "content": [
      {"type": "text", "text": "看这张图"},
      {"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "aWNvbg=="}}
    ]},
    {"role": "assistant", "content": [
      {"type": "text", "text": "调用工具"},
      {"type": "tool_use", "id": "toolu_01", "name": "get_weather", "input": {"city": "北京"}}
    ]},
    {"role": "user", "content": [
      {"type": "tool_result", "tool_use_id": "toolu_01", "content": "晴", "is_error": false}
    ]}
  ],
  "tools": [
    {"name": "get_weather", "description": "查天气", "input_schema": {"type": "object", "properties": {"city": {"type": "string"}}}}
  ],
  "tool_choice": {"type": "auto"},
  "temperature": 0.7,
  "stream": true
}`

func TestParseRequest(t *testing.T) {
	req, err := ParseRequest([]byte(goldenRequest))
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "claude-sonnet-5" || req.MaxTokens != 1024 {
		t.Errorf("model/max_tokens = %s/%d", req.Model, req.MaxTokens)
	}
	if len(req.System) != 2 || req.System[1] != "第二段" {
		t.Errorf("system = %+v", req.System)
	}
	if len(req.Turns) != 3 {
		t.Fatalf("turns = %d", len(req.Turns))
	}
	user0 := req.Turns[0]
	if user0.Role != "user" || len(user0.Parts) != 2 {
		t.Fatalf("turn0 = %+v", user0)
	}
	if user0.Parts[0].Kind != protocol.KindText || user0.Parts[1].Kind != protocol.KindImage ||
		user0.Parts[1].ImageMediaType != "image/png" || user0.Parts[1].ImageData != "aWNvbg==" {
		t.Errorf("turn0 parts = %+v", user0.Parts)
	}
	asst := req.Turns[1]
	if asst.Parts[0].Kind != protocol.KindText || asst.Parts[1].Kind != protocol.KindToolUse ||
		asst.Parts[1].ToolCallID != "toolu_01" || asst.Parts[1].ToolName != "get_weather" {
		t.Errorf("turn1 = %+v", asst.Parts)
	}
	var cityIn map[string]any
	if err := json.Unmarshal([]byte(asst.Parts[1].ToolInputJSON), &cityIn); err != nil || cityIn["city"] != "北京" {
		t.Errorf("tool input = %q err=%v", asst.Parts[1].ToolInputJSON, err)
	}
	tr := req.Turns[2].Parts[0]
	if tr.Kind != protocol.KindToolResult || tr.ToolCallID != "toolu_01" || tr.ToolResult != "晴" {
		t.Errorf("turn2 = %+v", tr)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "get_weather" {
		t.Errorf("tools = %+v", req.Tools)
	}
	if req.ToolChoice.Mode != protocol.ToolChoiceAuto || req.Temperature == nil || *req.Temperature != 0.7 {
		t.Errorf("tool_choice/temp = %+v/%v", req.ToolChoice, req.Temperature)
	}
	if !req.Stream {
		t.Error("stream lost")
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
	if path != "/v1/messages" {
		t.Errorf("path = %q", path)
	}
	if hdr.Get("anthropic-version") == "" {
		t.Error("缺 anthropic-version 头")
	}
	// 再解析回来，语义字段一致。
	req2, err := ParseRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if req2.Model != req.Model || len(req2.System) != 2 || len(req2.Turns) != 3 ||
		len(req2.Tools) != 1 || req2.MaxTokens != 1024 || req2.Stream != true {
		t.Errorf("round trip drift: %+v", req2)
	}
	// 工具输入 JSON 语义等价（键序无关）。
	var in1, in2 map[string]any
	json.Unmarshal([]byte(req.Turns[1].Parts[1].ToolInputJSON), &in1)
	json.Unmarshal([]byte(req2.Turns[1].Parts[1].ToolInputJSON), &in2)
	if in1["city"] != in2["city"] {
		t.Errorf("tool input drift: %v vs %v", in1, in2)
	}
}

func TestParseResponse(t *testing.T) {
	body := `{"id":"msg_1","model":"claude-sonnet-5","role":"assistant","content":[
    {"type":"text","text":"你好"},
    {"type":"tool_use","id":"toolu_02","name":"search","input":{"q":"x"}}
  ],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":3,"cache_creation_input_tokens":2}}`
	resp, err := ParseResponse(200, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != protocol.StopToolUse || resp.Usage.Input != 10 || resp.Usage.Output != 5 ||
		resp.Usage.CacheRead != 3 || resp.Usage.CacheWrite != 2 {
		t.Errorf("resp = %+v", resp)
	}
	if len(resp.Parts) != 2 || resp.Parts[1].ToolName != "search" {
		t.Errorf("parts = %+v", resp.Parts)
	}
	// 反向构建再解析。
	status, out := BuildResponse(resp)
	resp2, err := ParseResponse(status, out)
	if err != nil {
		t.Fatal(err)
	}
	if resp2.StopReason != protocol.StopToolUse || len(resp2.Parts) != 2 {
		t.Errorf("build/parse drift: %+v", resp2)
	}
}

const goldenSSE = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-5","role":"assistant","content":[],"usage":{"input_tokens":25,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"好"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_03","name":"get_weather","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"city\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"北京\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":15}}

event: message_stop
data: {"type":"message_stop"}`

func collectEvents(t *testing.T, dec protocol.StreamDecoder) []protocol.Event {
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
	evs := collectEvents(t, NewStreamDecoder(strings.NewReader(goldenSSE)))
	kinds := ""
	for _, e := range evs {
		kinds += string(e.Kind) + ","
	}
	want := "stream_start,block_start,text_delta,text_delta,block_stop,block_start,tool_call_delta,tool_call_delta,block_stop,stream_end,"
	if kinds != want {
		t.Fatalf("kinds = %s want %s", kinds, want)
	}
	if evs[0].Usage.Input != 25 {
		t.Errorf("start usage = %+v", evs[0].Usage)
	}
	toolStart := evs[5]
	if toolStart.Block.Kind != protocol.KindToolUse || toolStart.Block.ToolName != "get_weather" || toolStart.Index != 1 {
		t.Errorf("tool block start = %+v", toolStart)
	}
	end := evs[len(evs)-1]
	if end.StopReason != protocol.StopToolUse || end.Usage.Output != 15 {
		t.Errorf("end = %+v", end)
	}
}

func TestStreamEncoderProducesValidSequence(t *testing.T) {
	// IR 事件 → anthropic SSE → 再解码 → 事件等价。
	irEvents := []protocol.Event{
		{Kind: protocol.EvStreamStart, Model: "claude-sonnet-5", Usage: protocol.Usage{Input: 25}},
		{Kind: protocol.EvBlockStart, Index: 0, Block: protocol.Part{Kind: protocol.KindText}},
		{Kind: protocol.EvTextDelta, Index: 0, Text: "你"},
		{Kind: protocol.EvTextDelta, Index: 0, Text: "好"},
		{Kind: protocol.EvBlockStop, Index: 0},
		{Kind: protocol.EvBlockStart, Index: 1, Block: protocol.Part{Kind: protocol.KindToolUse, ToolCallID: "toolu_03", ToolName: "get_weather"}},
		{Kind: protocol.EvToolCallDelta, Index: 1, ToolDelta: `{"city":`},
		{Kind: protocol.EvToolCallDelta, Index: 1, ToolDelta: `"北京"}`},
		{Kind: protocol.EvBlockStop, Index: 1},
		{Kind: protocol.EvStreamEnd, StopReason: protocol.StopToolUse, Usage: protocol.Usage{Input: 25, Output: 15}},
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
	for _, want := range []string{
		"event: message_start", `"type":"message_start"`,
		"event: content_block_start", `"type":"tool_use"`,
		"event: content_block_delta", `"partial_json":"{\"city\":"`,
		"event: content_block_stop", `"index":1`,
		"event: message_delta", `"stop_reason":"tool_use"`,
		"event: message_stop",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q\n%s", want, out)
		}
	}
	// 回环：解码器读编码器输出应得到等价事件序列。
	dec := NewStreamDecoder(strings.NewReader(out))
	got := collectEvents(t, dec)
	if len(got) != len(irEvents) {
		t.Fatalf("回环事件数 = %d want %d", len(got), len(irEvents))
	}
	for i := range got {
		if got[i].Kind != irEvents[i].Kind || got[i].Index != irEvents[i].Index {
			t.Errorf("事件 %d: %+v vs %+v", i, got[i], irEvents[i])
		}
		if got[i].Kind == protocol.EvToolCallDelta && got[i].ToolDelta != irEvents[i].ToolDelta {
			t.Errorf("事件 %d tool delta: %q vs %q", i, got[i].ToolDelta, irEvents[i].ToolDelta)
		}
	}
}

func TestErrorMapping(t *testing.T) {
	msg := ParseError(429, []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"太频繁"}}`))
	if !strings.Contains(msg, "太频繁") {
		t.Errorf("ParseError = %q", msg)
	}
	body := BuildError(429, "太频繁")
	var parsed struct {
		Type  string `json:"type"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Type != "error" || parsed.Error.Message != "太频繁" {
		t.Errorf("BuildError = %s", body)
	}
}
