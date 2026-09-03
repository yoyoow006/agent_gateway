package openairesponses

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"agent_gateway/internal/protocol"
)

const goldenRequest = `{
  "model": "gpt-5.2-codex",
  "instructions": "你是编码助手",
  "input": [
    {"type": "message", "role": "user", "content": [
      {"type": "input_text", "text": "查下天气"},
      {"type": "input_image", "image_url": "data:image/png;base64,aWNvbg=="}
    ]},
    {"type": "function_call", "call_id": "call_01", "name": "get_weather", "arguments": "{\"city\":\"北京\"}"},
    {"type": "function_call_output", "call_id": "call_01", "output": "晴"},
    {"type": "message", "role": "user", "content": "继续"}
  ],
  "tools": [
    {"type": "function", "name": "get_weather", "description": "查天气", "parameters": {"type": "object", "properties": {"city": {"type": "string"}}}, "strict": false}
  ],
  "tool_choice": "auto",
  "max_output_tokens": 1024,
  "temperature": 0.2,
  "stream": true,
  "store": false
}`

func TestParseRequest(t *testing.T) {
	req, err := ParseRequest([]byte(goldenRequest))
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "gpt-5.2-codex" || len(req.System) != 1 || req.System[0] != "你是编码助手" {
		t.Errorf("基础字段 = %+v", req)
	}
	if len(req.Turns) != 3 {
		t.Fatalf("turns = %d: %+v", len(req.Turns), req.Turns)
	}
	u := req.Turns[0]
	if u.Role != "user" || u.Parts[0].Text != "查下天气" ||
		u.Parts[1].Kind != protocol.KindImage || u.Parts[1].ImageMediaType != "image/png" || u.Parts[1].ImageData != "aWNvbg==" {
		t.Errorf("turn0 = %+v", u.Parts)
	}
	fc := req.Turns[1].Parts[0]
	if req.Turns[1].Role != "assistant" || fc.Kind != protocol.KindToolUse || fc.ToolCallID != "call_01" || fc.ToolName != "get_weather" {
		t.Errorf("turn1 = %+v", req.Turns[1])
	}
	fo := req.Turns[2].Parts[0]
	if req.Turns[2].Role != "user" || fo.Kind != protocol.KindToolResult || fo.ToolCallID != "call_01" || fo.ToolResult != "晴" {
		t.Errorf("turn2 = %+v", req.Turns[2])
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "get_weather" {
		t.Errorf("tools = %+v", req.Tools)
	}
	if req.ToolChoice.Mode != protocol.ToolChoiceAuto || req.MaxTokens != 1024 || !req.Stream {
		t.Errorf("参数 = %+v/%d/%v", req.ToolChoice, req.MaxTokens, req.Stream)
	}
	// function_call_output 与后续 user 消息合并为单轮：tool_result 块 + 文本块
	if len(req.Turns[2].Parts) != 2 || req.Turns[2].Parts[1].Text != "继续" {
		t.Errorf("合并轮 = %+v", req.Turns[2].Parts)
	}
}

func TestBuildRequestRoundTrip(t *testing.T) {
	req, err := ParseRequest([]byte(goldenRequest))
	if err != nil {
		t.Fatal(err)
	}
	path, _, body, err := BuildRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/responses" {
		t.Errorf("path = %q", path)
	}
	var raw map[string]any
	json.Unmarshal(body, &raw)
	if v, _ := raw["store"].(bool); v {
		t.Error("store 必须为 false（无状态可切换前提）")
	}
	if raw["instructions"] != "你是编码助手" {
		t.Errorf("instructions = %v", raw["instructions"])
	}
	req2, err := ParseRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if req2.Model != req.Model || len(req2.Turns) != 3 || len(req2.Tools) != 1 || req2.MaxTokens != 1024 {
		t.Errorf("round trip drift: %+v", req2)
	}
	var in map[string]any
	json.Unmarshal([]byte(req2.Turns[1].Parts[0].ToolInputJSON), &in)
	if in["city"] != "北京" {
		t.Errorf("tool input = %v", in)
	}
	if req2.Turns[2].Parts[0].ToolResult != "晴" {
		t.Errorf("tool result = %v", req2.Turns[2].Parts[0].ToolResult)
	}
}

func TestParseResponseAndBuild(t *testing.T) {
	body := `{"id":"resp_1","object":"response","status":"completed","model":"gpt-5.2-codex","output":[
    {"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"想一想"}]},
    {"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"你好"}]},
    {"type":"function_call","id":"fc_1","call_id":"call_02","name":"search","arguments":"{\"q\":\"x\"}"}
  ],"usage":{"input_tokens":11,"output_tokens":7,"output_tokens_details":{"reasoning_tokens":3}}}`
	resp, err := ParseResponse(200, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != protocol.StopToolUse || resp.Usage.Input != 11 || resp.Usage.Output != 7 {
		t.Errorf("resp = %+v", resp)
	}
	if len(resp.Parts) != 3 || resp.Parts[0].Kind != protocol.KindThinking ||
		resp.Parts[1].Text != "你好" || resp.Parts[2].ToolName != "search" {
		t.Errorf("parts = %+v", resp.Parts)
	}
	status, out := BuildResponse(resp)
	var raw map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatal(err)
	}
	usage, _ := raw["usage"].(map[string]any)
	// Codex 0.149+ 将 total_tokens 视为必填字段，缺失即断流
	if usage["total_tokens"] != float64(18) || usage["input_tokens"] != float64(11) || usage["output_tokens"] != float64(7) {
		t.Errorf("非流式 usage = %v，缺 total_tokens 或值错误（应 11+7=18）", usage)
	}
	resp2, err := ParseResponse(status, out)
	if err != nil {
		t.Fatal(err)
	}
	if resp2.StopReason != protocol.StopToolUse || len(resp2.Parts) != 3 {
		t.Errorf("build/parse drift: %+v", resp2)
	}
}

const goldenSSE = `event: response.created
data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5.2-codex","status":"in_progress"}}

event: output_item.added
data: {"type":"output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}

event: content_part.added
data: {"type":"content_part.added","item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}

event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"你"}

event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"好"}

event: output_item.done
data: {"type":"output_item.done","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"你好"}]}}

event: output_item.added
data: {"type":"output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_02","name":"get_weather","arguments":""}}

event: response.function_call_arguments.delta
data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":1,"delta":"{\"city\":"}

event: response.function_call_arguments.delta
data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":1,"delta":"\"北京\"}"}

event: output_item.done
data: {"type":"output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_02","name":"get_weather","arguments":"{\"city\":\"北京\"}","status":"completed"}}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":12,"output_tokens":8}}}`

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
	want := "stream_start,block_start,text_delta,text_delta,block_stop,block_start,tool_call_delta,tool_call_delta,block_stop,stream_end,"
	if kinds != want {
		t.Fatalf("kinds = %s\nwant %s", kinds, want)
	}
	if evs[0].Model != "gpt-5.2-codex" {
		t.Errorf("start model = %q", evs[0].Model)
	}
	toolStart := evs[5]
	if toolStart.Index != 1 || toolStart.Block.Kind != protocol.KindToolUse || toolStart.Block.ToolCallID != "call_02" || toolStart.Block.ToolName != "get_weather" {
		t.Errorf("tool start = %+v", toolStart)
	}
	end := evs[len(evs)-1]
	if end.StopReason != protocol.StopToolUse || end.Usage.Input != 12 || end.Usage.Output != 8 {
		t.Errorf("end = %+v", end)
	}
}

func TestStreamDecoderPrefixedNames(t *testing.T) {
	// 原生上游（OpenAI/智谱）事件 type 带 response. 前缀，解码器须同样识别
	sse := `event: response.output_item.added
data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}

event: response.content_part.added
data: {"type":"response.content_part.added","item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}

event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"好"}

event: response.output_item.done
data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1","status":"completed","content":[{"type":"output_text","text":"好"}]}}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`
	evs := collect(t, NewStreamDecoder(strings.NewReader(sse)))
	kinds := ""
	for _, e := range evs {
		kinds += string(e.Kind) + ","
	}
	if kinds != "block_start,text_delta,block_stop,stream_end," {
		t.Fatalf("prefixed kinds = %s", kinds)
	}
	end := evs[len(evs)-1]
	if end.Usage.Input != 1 || end.Usage.Output != 1 {
		t.Errorf("prefixed end = %+v", end)
	}
}

func TestStreamEncoderRoundTrip(t *testing.T) {
	irEvents := []protocol.Event{
		{Kind: protocol.EvStreamStart, Model: "gpt-5.2-codex"},
		{Kind: protocol.EvBlockStart, Index: 0, Block: protocol.Part{Kind: protocol.KindText}},
		{Kind: protocol.EvTextDelta, Index: 0, Text: "你好"},
		{Kind: protocol.EvBlockStop, Index: 0},
		{Kind: protocol.EvStreamEnd, StopReason: protocol.StopEndTurn, Usage: protocol.Usage{Input: 12, Output: 8}},
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
		"event: response.created", "event: response.output_item.added",
		`"type":"response.output_item.added"`,
		"event: response.output_text.delta", `"delta":"你好"`,
		"event: response.output_item.done", `"type":"response.output_item.done"`,
		"event: response.completed",
		`"input_tokens":12`, `"total_tokens":20`, `"status":"completed"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q\n%s", want, out)
		}
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

func TestErrorMapping(t *testing.T) {
	msg := ParseError(429, []byte(`{"error":{"message":"Quota"}}`))
	if !strings.Contains(msg, "Quota") {
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
