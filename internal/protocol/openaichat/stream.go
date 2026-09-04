package openaichat

import (
	"encoding/json"
	"fmt"
	"io"

	"agent_gateway/internal/protocol"
)

// ---- 非流式响应 ----

type wireUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

type wireResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int            `json:"index"`
		Message      wireMessageOut `json:"message"`
		FinishReason string         `json:"finish_reason"`
	} `json:"choices"`
	Usage *wireUsage `json:"usage,omitempty"`
}

type wireMessageOut struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []toolCall `json:"tool_calls,omitempty"`
}

// stopFromChat 映射 finish_reason。
func stopFromChat(s string) protocol.StopReason {
	switch s {
	case "stop":
		return protocol.StopEndTurn
	case "tool_calls", "function_call":
		return protocol.StopToolUse
	case "length":
		return protocol.StopMaxTokens
	default:
		return protocol.StopOther
	}
}

// stopToChat 反向映射。
func stopToChat(s protocol.StopReason) string {
	switch s {
	case protocol.StopToolUse:
		return "tool_calls"
	case protocol.StopMaxTokens:
		return "length"
	default:
		return "stop"
	}
}

// ParseResponse 解析 2xx 响应。
func ParseResponse(status int, body []byte) (protocol.Response, error) {
	var w wireResponse
	if err := json.Unmarshal(body, &w); err != nil {
		return protocol.Response{}, fmt.Errorf("解析 chat 响应: %w", err)
	}
	resp := protocol.Response{Model: w.Model}
	if len(w.Choices) > 0 {
		c := w.Choices[0]
		resp.StopReason = stopFromChat(c.FinishReason)
		if c.Message.ReasoningContent != "" {
			resp.Parts = append(resp.Parts, protocol.Thinking(c.Message.ReasoningContent))
		}
		if c.Message.Content != "" {
			resp.Parts = append(resp.Parts, protocol.Text(c.Message.Content))
		}
		for _, tc := range c.Message.ToolCalls {
			args := tc.Function.Arguments
			if args == "" {
				args = "{}"
			}
			resp.Parts = append(resp.Parts, protocol.ToolUse(tc.ID, tc.Function.Name, args))
		}
	}
	if w.Usage != nil {
		resp.Usage = protocol.Usage{
			Input:  w.Usage.PromptTokens,
			Output: w.Usage.CompletionTokens,
		}
		if w.Usage.PromptTokensDetails != nil {
			resp.Usage.CacheRead = w.Usage.PromptTokensDetails.CachedTokens
		}
	}
	return resp, nil
}

// BuildResponse 构建响应体。
func BuildResponse(resp protocol.Response) (int, []byte) {
	out := wireMessageOut{Role: "assistant"}
	for _, p := range resp.Parts {
		switch p.Kind {
		case protocol.KindText:
			out.Content += p.Text
		case protocol.KindThinking:
			out.ReasoningContent += p.Text
		case protocol.KindToolUse:
			args := p.ToolInputJSON
			if args == "" {
				args = "{}"
			}
			out.ToolCalls = append(out.ToolCalls, toolCall{
				ID: p.ToolCallID, Type: "function",
				Function: functionCall{Name: p.ToolName, Arguments: args},
			})
		}
	}
	w := wireResponse{
		ID: protocol.NewID("chatcmpl"), Object: "chat.completion", Model: resp.Model,
		Choices: []struct {
			Index        int            `json:"index"`
			Message      wireMessageOut `json:"message"`
			FinishReason string         `json:"finish_reason"`
		}{{Index: 0, Message: out, FinishReason: stopToChat(resp.StopReason)}},
		Usage: &wireUsage{PromptTokens: resp.Usage.Input, CompletionTokens: resp.Usage.Output},
	}
	if w.Model == "" {
		w.Model = "agw"
	}
	body, _ := json.Marshal(w)
	return 200, body
}

// ---- 流式解码：chat SSE → IR 事件 ----

type streamChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role             string     `json:"role"`
			Content          string     `json:"content"`
			ReasoningContent string     `json:"reasoning_content"`
			ToolCalls        []toolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *wireUsage `json:"usage"`
}

type streamDecoder struct {
	r        *protocol.SSEReader
	pending  []protocol.Event
	started  bool
	nextIdx  int
	curIdx   int // 当前打开的块索引，-1 表示无
	curKind  protocol.PartKind
	toolIR   map[int]int    // chat tool_calls.index → IR 块索引
	idToChat map[string]int // tool id → chat 键，用于缺 index 时反查
	lastChat int            // 最近一次成功处理的 chat 键；缺 index+空 ID 时坍缩至此
	finish   *string
	usage    *wireUsage
}

// NewStreamDecoder 构造解码器。
func NewStreamDecoder(r io.Reader) protocol.StreamDecoder {
	return &streamDecoder{
		r:        protocol.NewSSEReader(r),
		curIdx:   -1,
		toolIR:   map[int]int{},
		idToChat: map[string]int{},
		lastChat: -1,
	}
}

func (d *streamDecoder) Next() (protocol.Event, error) {
	if len(d.pending) > 0 {
		ev := d.pending[0]
		d.pending = d.pending[1:]
		return ev, nil
	}
	for {
		frame, err := d.r.Next()
		if err != nil {
			return protocol.Event{}, err
		}
		if frame.Data == "" {
			continue
		}
		if frame.Data == "[DONE]" {
			d.closeBlock()
			usage := protocol.Usage{}
			if d.usage != nil {
				usage = protocol.Usage{Input: d.usage.PromptTokens, Output: d.usage.CompletionTokens}
			}
			stop := protocol.StopEndTurn
			if d.finish != nil {
				stop = stopFromChat(*d.finish)
			}
			d.pending = append(d.pending, protocol.Event{Kind: protocol.EvStreamEnd, StopReason: stop, Usage: usage})
			if len(d.pending) > 0 {
				ev := d.pending[0]
				d.pending = d.pending[1:]
				return ev, nil
			}
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(frame.Data), &chunk); err != nil {
			continue // 容忍中转站私有帧
		}
		// chat 协议无起始事件：在首个 chunk 合成 EvStreamStart（携带 model）
		if !d.started {
			d.started = true
			d.pending = append(d.pending, protocol.Event{Kind: protocol.EvStreamStart, Model: chunk.Model})
		}
		if chunk.Usage != nil {
			d.usage = chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		c := chunk.Choices[0]
		if c.FinishReason != nil && *c.FinishReason != "" {
			d.finish = c.FinishReason
		}
		if c.Delta.Content != "" {
			d.ensureBlock(protocol.KindText)
			d.pending = append(d.pending, protocol.Event{Kind: protocol.EvTextDelta, Index: d.curIdx, Text: c.Delta.Content})
		}
		if c.Delta.ReasoningContent != "" {
			d.ensureBlock(protocol.KindThinking)
			d.pending = append(d.pending, protocol.Event{Kind: protocol.EvThinkingDelta, Index: d.curIdx, Text: c.Delta.ReasoningContent})
		}
		for _, tc := range c.Delta.ToolCalls {
			chatIdx, irIdx, isNew := d.resolveToolKey(tc)
			_ = chatIdx
			if isNew {
				d.closeBlock()
				d.curIdx = irIdx
				d.curKind = protocol.KindToolUse
				d.pending = append(d.pending, protocol.Event{
					Kind:  protocol.EvBlockStart,
					Index: irIdx,
					Block: protocol.Part{Kind: protocol.KindToolUse, ToolCallID: tc.ID, ToolName: tc.Function.Name},
				})
				if tc.Function.Arguments != "" {
					d.pending = append(d.pending, protocol.Event{Kind: protocol.EvToolCallDelta, Index: irIdx, ToolDelta: tc.Function.Arguments})
				}
				continue
			}
			if tc.Function.Arguments != "" {
				d.curIdx = irIdx
				d.curKind = protocol.KindToolUse
				d.pending = append(d.pending, protocol.Event{Kind: protocol.EvToolCallDelta, Index: irIdx, ToolDelta: tc.Function.Arguments})
			}
		}
		if len(d.pending) > 0 {
			ev := d.pending[0]
			d.pending = d.pending[1:]
			return ev, nil
		}
	}
}

// ensureBlock 保证当前打开的块是 kind，不是则先关再开。
func (d *streamDecoder) ensureBlock(kind protocol.PartKind) {
	if d.curIdx >= 0 && d.curKind == kind {
		return
	}
	d.closeBlock()
	idx := d.nextIdx
	d.nextIdx++
	d.curIdx = idx
	d.curKind = kind
	d.pending = append(d.pending, protocol.Event{Kind: protocol.EvBlockStart, Index: idx, Block: protocol.Part{Kind: kind}})
}

// closeBlock 关闭当前块（若有）。
func (d *streamDecoder) closeBlock() {
	if d.curIdx >= 0 {
		d.pending = append(d.pending, protocol.Event{Kind: protocol.EvBlockStop, Index: d.curIdx})
		d.curIdx = -1
		d.curKind = ""
	}
}

// resolveToolKey 把 chat tool_calls 项解析为 (chat 键, IR 块索引, 是否新块)。
// 借鉴 cc-switch resolve_tool_key_without_index 启发式处理缺 index 情形：
//   - tc.Index != nil → 用 *tc.Index（主流路径，OpenAI 官方行为）
//   - tc.Index == nil && tc.ID 已记录 → 复用旧 chat 键与同一 IR 块
//   - tc.Index == nil && tc.ID 新 → 分配新 chat 键 + 新 IR 块
//   - tc.Index == nil && tc.ID 空 → 坍缩到最后已知 chat 键 + 同一 IR 块
//
// 同一调用只递增一次 d.nextIdx（chat 键与 IR 块复用同一计数器，避免双递增）。
func (d *streamDecoder) resolveToolKey(tc toolCall) (chatIdx, irIdx int, isNew bool) {
	switch {
	case tc.Index != nil:
		chatIdx = *tc.Index
	case tc.ID != "":
		if k, ok := d.idToChat[tc.ID]; ok {
			chatIdx = k
		} else {
			chatIdx = d.nextIdx
			d.idToChat[tc.ID] = chatIdx
		}
	case d.lastChat >= 0:
		chatIdx = d.lastChat
	default:
		chatIdx = d.nextIdx
	}

	if existing, ok := d.toolIR[chatIdx]; ok {
		return chatIdx, existing, false
	}
	irIdx = d.nextIdx
	d.nextIdx++
	d.toolIR[chatIdx] = irIdx
	d.lastChat = chatIdx
	return chatIdx, irIdx, true
}

// ---- 流式编码：IR 事件 → chat SSE ----

type streamEncoder struct {
	w       *protocol.SSEWriter
	id      string
	started bool
	ended   bool
	blocks  map[int]*encBlock
}

type encBlock struct {
	kind       protocol.PartKind
	idSent     bool
	toolCallID string
	toolName   string
}

// NewStreamEncoder 构造编码器。
func NewStreamEncoder(w io.Writer) protocol.StreamEncoder {
	return &streamEncoder{w: protocol.NewSSEWriter(w, nil), id: protocol.NewID("chatcmpl"), blocks: map[int]*encBlock{}}
}

func (e *streamEncoder) Encode(ev protocol.Event) error {
	switch ev.Kind {
	case protocol.EvStreamStart, protocol.EvBlockStart:
		if ev.Kind == protocol.EvBlockStart {
			e.blocks[ev.Index] = &encBlock{kind: ev.Block.Kind, toolCallID: ev.Block.ToolCallID, toolName: ev.Block.ToolName}
		}
		return e.ensureStart()
	case protocol.EvTextDelta:
		if err := e.ensureStart(); err != nil {
			return err
		}
		return e.chunk(map[string]any{"content": ev.Text}, nil)
	case protocol.EvThinkingDelta:
		if err := e.ensureStart(); err != nil {
			return err
		}
		return e.chunk(map[string]any{"reasoning_content": ev.Text}, nil)
	case protocol.EvToolCallDelta:
		if err := e.ensureStart(); err != nil {
			return err
		}
		fn := map[string]any{"arguments": ev.ToolDelta}
		call := map[string]any{"index": ev.Index, "type": "function", "function": fn}
		if b := e.blocks[ev.Index]; b != nil && !b.idSent {
			b.idSent = true
			call["id"] = b.toolCallID
			fn["name"] = b.toolName
		}
		return e.chunk(map[string]any{"tool_calls": []any{call}}, nil)
	case protocol.EvBlockStop:
		return nil
	case protocol.EvStreamEnd:
		if err := e.ensureStart(); err != nil {
			return err
		}
		fin := stopToChat(ev.StopReason)
		if err := e.chunk(map[string]any{}, &fin); err != nil {
			return err
		}
		if ev.Usage.Input > 0 || ev.Usage.Output > 0 {
			payload := map[string]any{
				"id": e.id, "object": "chat.completion.chunk",
				"choices": []any{},
				"usage":   map[string]any{"prompt_tokens": ev.Usage.Input, "completion_tokens": ev.Usage.Output},
			}
			data, _ := json.Marshal(payload)
			if err := e.w.SendData(string(data)); err != nil {
				return err
			}
		}
		e.ended = true
		return e.w.SendData("[DONE]")
	case protocol.EvStreamError:
		payload, _ := json.Marshal(map[string]any{"error": map[string]any{"message": orDefault(ev.ErrMessage, "上游流错误")}})
		return e.w.SendData(string(payload))
	}
	return nil
}

// Finish 未收到结束事件时补终止帧。
func (e *streamEncoder) Finish() error {
	if e.ended {
		return nil
	}
	return e.Encode(protocol.Event{Kind: protocol.EvStreamEnd, StopReason: protocol.StopOther})
}

func (e *streamEncoder) ensureStart() error {
	if e.started {
		return nil
	}
	e.started = true
	return e.chunk(map[string]any{"role": "assistant"}, nil)
}

func (e *streamEncoder) chunk(delta map[string]any, finish *string) error {
	choice := map[string]any{"index": 0, "delta": delta, "finish_reason": nil}
	if finish != nil {
		choice["finish_reason"] = *finish
	}
	payload := map[string]any{"id": e.id, "object": "chat.completion.chunk", "choices": []any{choice}}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return e.w.SendData(string(data))
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// ---- 错误映射 ----

// ParseError 提取上游错误信息。
func ParseError(status int, body []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	return fmt.Sprintf("上游返回 %d", status)
}

// BuildError 构造 chat 格式错误体。
func BuildError(status int, msg string) []byte {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{"message": msg, "type": errorType(status)},
	})
	return body
}

func errorType(status int) string {
	switch {
	case status == 429:
		return "rate_limit_error"
	case status >= 400 && status < 500:
		return "invalid_request_error"
	default:
		return "server_error"
	}
}
