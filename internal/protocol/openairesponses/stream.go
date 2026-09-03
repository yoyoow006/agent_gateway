package openairesponses

import (
	"encoding/json"
	"io"
	"strings"

	"agent_gateway/internal/protocol"
)

// ---- 流式解码：Responses SSE → IR 事件 ----

type streamDecoder struct {
	r       *protocol.SSEReader
	pending []protocol.Event
	sawTool bool
	sawEnd  bool
}

// NewStreamDecoder 构造解码器。
func NewStreamDecoder(r io.Reader) protocol.StreamDecoder {
	return &streamDecoder{r: protocol.NewSSEReader(r)}
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
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(frame.Data), &head) != nil {
			continue
		}
		switch head.Type {
		case "response.created", "response.in_progress":
			var e struct {
				Response struct {
					Model string `json:"model"`
				} `json:"response"`
			}
			if json.Unmarshal([]byte(frame.Data), &e) == nil && head.Type == "response.created" {
				return protocol.Event{Kind: protocol.EvStreamStart, Model: e.Response.Model}, nil
			}
		// 原生上游 type 带 response. 前缀（OpenAI/智谱），历史样例无前缀，双名兼容
		case "output_item.added", "response.output_item.added":
			var e struct {
				OutputIndex int       `json:"output_index"`
				Item        inputItem `json:"item"`
			}
			if json.Unmarshal([]byte(frame.Data), &e) != nil {
				continue
			}
			switch e.Item.Type {
			case "message":
				d.pending = append(d.pending, protocol.Event{
					Kind: protocol.EvBlockStart, Index: e.OutputIndex, Block: protocol.Part{Kind: protocol.KindText},
				})
			case "function_call":
				d.sawTool = true
				d.pending = append(d.pending, protocol.Event{
					Kind:  protocol.EvBlockStart,
					Index: e.OutputIndex,
					Block: protocol.Part{Kind: protocol.KindToolUse, ToolCallID: e.Item.CallID, ToolName: e.Item.Name},
				})
			case "reasoning":
				d.pending = append(d.pending, protocol.Event{
					Kind: protocol.EvBlockStart, Index: e.OutputIndex, Block: protocol.Part{Kind: protocol.KindThinking},
				})
			}
		case "response.output_text.delta":
			var e struct {
				OutputIndex int    `json:"output_index"`
				Delta       string `json:"delta"`
			}
			if json.Unmarshal([]byte(frame.Data), &e) != nil {
				continue
			}
			d.pending = append(d.pending, protocol.Event{Kind: protocol.EvTextDelta, Index: e.OutputIndex, Text: e.Delta})
		case "response.reasoning_summary_text.delta":
			var e struct {
				OutputIndex int    `json:"output_index"`
				Delta       string `json:"delta"`
			}
			if json.Unmarshal([]byte(frame.Data), &e) != nil {
				continue
			}
			d.pending = append(d.pending, protocol.Event{Kind: protocol.EvThinkingDelta, Index: e.OutputIndex, Text: e.Delta})
		case "response.function_call_arguments.delta":
			var e struct {
				OutputIndex int    `json:"output_index"`
				Delta       string `json:"delta"`
			}
			if json.Unmarshal([]byte(frame.Data), &e) != nil {
				continue
			}
			d.pending = append(d.pending, protocol.Event{Kind: protocol.EvToolCallDelta, Index: e.OutputIndex, ToolDelta: e.Delta})
		case "output_item.done", "response.output_item.done":
			var e struct {
				OutputIndex int `json:"output_index"`
			}
			if json.Unmarshal([]byte(frame.Data), &e) != nil {
				continue
			}
			d.pending = append(d.pending, protocol.Event{Kind: protocol.EvBlockStop, Index: e.OutputIndex})
		case "response.completed", "response.incomplete":
			var e struct {
				Response struct {
					Usage *wireUsage `json:"usage"`
				} `json:"response"`
			}
			_ = json.Unmarshal([]byte(frame.Data), &e)
			usage := protocol.Usage{}
			if e.Response.Usage != nil {
				usage = protocol.Usage{Input: e.Response.Usage.InputTokens, Output: e.Response.Usage.OutputTokens}
			}
			stop := protocol.StopEndTurn
			if d.sawTool {
				stop = protocol.StopToolUse
			}
			d.pending = append(d.pending, protocol.Event{Kind: protocol.EvStreamEnd, StopReason: stop, Usage: usage})
			d.sawEnd = true
		case "response.failed", "error":
			var e struct {
				Response struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				} `json:"response"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			_ = json.Unmarshal([]byte(frame.Data), &e)
			msg := e.Response.Error.Message
			if msg == "" && e.Error != nil {
				msg = e.Error.Message
			}
			d.pending = append(d.pending, protocol.Event{Kind: protocol.EvStreamError, ErrMessage: msg})
		}
		if len(d.pending) > 0 {
			ev := d.pending[0]
			d.pending = d.pending[1:]
			return ev, nil
		}
	}
}

// ---- 流式编码：IR 事件 → Responses SSE ----

type encBlockState struct {
	kind       protocol.PartKind
	itemID     string
	toolCallID string
	toolName   string
	text       strings.Builder
	args       strings.Builder
	partAdded  bool
}

type streamEncoder struct {
	w       *protocol.SSEWriter
	respID  string
	model   string
	started bool
	ended   bool
	blocks  map[int]*encBlockState
	order   []int
}

// NewStreamEncoder 构造编码器。
func NewStreamEncoder(w io.Writer) protocol.StreamEncoder {
	return &streamEncoder{w: protocol.NewSSEWriter(w, nil), respID: protocol.NewID("resp"), blocks: map[int]*encBlockState{}}
}

func (e *streamEncoder) Encode(ev protocol.Event) error {
	switch ev.Kind {
	case protocol.EvStreamStart:
		e.model = ev.Model
		return e.send("response.created", map[string]any{
			"type": "response.created",
			"response": map[string]any{
				"id": e.respID, "object": "response", "status": "in_progress",
				"model": orDefault(ev.Model, "agw"), "output": []any{},
			},
		})
	case protocol.EvBlockStart:
		b := &encBlockState{kind: ev.Block.Kind}
		switch ev.Block.Kind {
		case protocol.KindText:
			b.itemID = protocol.NewID("msg")
		case protocol.KindToolUse:
			b.itemID = protocol.NewID("fc")
			b.toolCallID = ev.Block.ToolCallID
			b.toolName = ev.Block.ToolName
		case protocol.KindThinking:
			b.itemID = protocol.NewID("rs")
		default:
			b.itemID = protocol.NewID("item")
		}
		e.blocks[ev.Index] = b
		e.order = append(e.order, ev.Index)
		item := map[string]any{"id": b.itemID, "status": "in_progress"}
		switch b.kind {
		case protocol.KindText:
			item["type"] = "message"
			item["role"] = "assistant"
			item["content"] = []any{}
		case protocol.KindToolUse:
			item["type"] = "function_call"
			item["call_id"] = b.toolCallID
			item["name"] = b.toolName
			item["arguments"] = ""
		case protocol.KindThinking:
			item["type"] = "reasoning"
			item["summary"] = []any{}
		}
		if err := e.send("response.output_item.added", map[string]any{
			"type": "response.output_item.added", "output_index": ev.Index, "item": item,
		}); err != nil {
			return err
		}
		if b.kind == protocol.KindText {
			return e.send("response.content_part.added", map[string]any{
				"type": "response.content_part.added", "item_id": b.itemID, "output_index": ev.Index,
				"content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
			})
		}
		return nil
	case protocol.EvTextDelta:
		b := e.blocks[ev.Index]
		if b == nil {
			return nil
		}
		b.text.WriteString(ev.Text)
		return e.send("response.output_text.delta", map[string]any{
			"type": "response.output_text.delta", "item_id": b.itemID,
			"output_index": ev.Index, "content_index": 0, "delta": ev.Text,
		})
	case protocol.EvThinkingDelta:
		b := e.blocks[ev.Index]
		if b == nil {
			return nil
		}
		b.text.WriteString(ev.Text)
		return e.send("response.reasoning_summary_text.delta", map[string]any{
			"type": "response.reasoning_summary_text.delta", "item_id": b.itemID,
			"output_index": ev.Index, "summary_index": 0, "delta": ev.Text,
		})
	case protocol.EvToolCallDelta:
		b := e.blocks[ev.Index]
		if b == nil {
			return nil
		}
		b.args.WriteString(ev.ToolDelta)
		return e.send("response.function_call_arguments.delta", map[string]any{
			"type": "response.function_call_arguments.delta", "item_id": b.itemID,
			"output_index": ev.Index, "delta": ev.ToolDelta,
		})
	case protocol.EvBlockStop:
		b := e.blocks[ev.Index]
		if b == nil {
			return nil
		}
		switch b.kind {
		case protocol.KindText:
			if err := e.send("response.content_part.done", map[string]any{
				"type": "response.content_part.done", "item_id": b.itemID, "output_index": ev.Index,
				"content_index": 0, "part": map[string]any{"type": "output_text", "text": b.text.String(), "annotations": []any{}},
			}); err != nil {
				return err
			}
			return e.send("response.output_item.done", map[string]any{
				"type": "response.output_item.done", "output_index": ev.Index,
				"item": map[string]any{
					"id": b.itemID, "type": "message", "role": "assistant", "status": "completed",
					"content": []any{map[string]any{"type": "output_text", "text": b.text.String(), "annotations": []any{}}},
				},
			})
		case protocol.KindToolUse:
			if err := e.send("response.function_call_arguments.done", map[string]any{
				"type": "response.function_call_arguments.done", "item_id": b.itemID,
				"output_index": ev.Index, "arguments": orDefault(b.args.String(), "{}"),
			}); err != nil {
				return err
			}
			return e.send("response.output_item.done", map[string]any{
				"type": "response.output_item.done", "output_index": ev.Index,
				"item": map[string]any{
					"id": b.itemID, "type": "function_call", "status": "completed",
					"call_id": b.toolCallID, "name": b.toolName,
					"arguments": orDefault(b.args.String(), "{}"),
				},
			})
		default:
			return e.send("response.output_item.done", map[string]any{
				"type": "response.output_item.done", "output_index": ev.Index,
				"item": map[string]any{"id": b.itemID, "type": "reasoning", "status": "completed", "summary": []any{}},
			})
		}
	case protocol.EvStreamEnd:
		output := make([]any, 0, len(e.order))
		for _, idx := range e.order {
			b := e.blocks[idx]
			if b == nil {
				continue
			}
			switch b.kind {
			case protocol.KindText:
				output = append(output, map[string]any{
					"id": b.itemID, "type": "message", "role": "assistant", "status": "completed",
					"content": []any{map[string]any{"type": "output_text", "text": b.text.String(), "annotations": []any{}}},
				})
			case protocol.KindToolUse:
				output = append(output, map[string]any{
					"id": b.itemID, "type": "function_call", "status": "completed",
					"call_id": b.toolCallID, "name": b.toolName,
					"arguments": orDefault(b.args.String(), "{}"),
				})
			case protocol.KindThinking:
				output = append(output, map[string]any{
					"id": b.itemID, "type": "reasoning", "status": "completed",
					"summary": []any{map[string]any{"type": "summary_text", "text": b.text.String()}},
				})
			}
		}
		e.ended = true
		return e.send("response.completed", map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"id": e.respID, "object": "response", "status": "completed",
				"model": orDefault(e.model, "agw"), "output": output,
				"usage": map[string]any{"input_tokens": ev.Usage.Input, "output_tokens": ev.Usage.Output, "total_tokens": ev.Usage.Input + ev.Usage.Output},
			},
		})
	case protocol.EvStreamError:
		return e.send("response.failed", map[string]any{
			"type": "response.failed",
			"response": map[string]any{
				"id": e.respID, "status": "failed",
				"error": map[string]any{"code": "agw_upstream_error", "message": orDefault(ev.ErrMessage, "上游流错误")},
			},
		})
	}
	return nil
}

// Finish 未收到结束事件时合成 completed，避免客户端死等。
func (e *streamEncoder) Finish() error {
	if e.ended {
		return nil
	}
	return e.Encode(protocol.Event{Kind: protocol.EvStreamEnd, StopReason: protocol.StopOther})
}

func (e *streamEncoder) send(name string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return e.w.Send(name, string(data))
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
