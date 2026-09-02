package anthropic

import (
	"encoding/json"
	"fmt"
	"io"

	"agent_gateway/internal/protocol"
)

// ---- 响应（非流式）----

type wireUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

type wireResponse struct {
	ID         string      `json:"id"`
	Type       string      `json:"type"`
	Role       string      `json:"role"`
	Model      string      `json:"model"`
	Content    []wireBlock `json:"content"`
	StopReason string      `json:"stop_reason"`
	Usage      wireUsage   `json:"usage"`
}

// stopFromAnthropic 把 anthropic stop_reason 映射为 IR。
func stopFromAnthropic(s string) protocol.StopReason {
	switch s {
	case "end_turn", "stop_sequence":
		return protocol.StopEndTurn
	case "tool_use":
		return protocol.StopToolUse
	case "max_tokens":
		return protocol.StopMaxTokens
	default:
		return protocol.StopOther
	}
}

// stopToAnthropic 把 IR 映射回 anthropic stop_reason。
func stopToAnthropic(s protocol.StopReason) string {
	switch s {
	case protocol.StopToolUse:
		return "tool_use"
	case protocol.StopMaxTokens:
		return "max_tokens"
	default:
		return "end_turn"
	}
}

// ParseResponse 解析 2xx 响应体。
func ParseResponse(status int, body []byte) (protocol.Response, error) {
	var w wireResponse
	if err := json.Unmarshal(body, &w); err != nil {
		return protocol.Response{}, fmt.Errorf("解析 anthropic 响应: %w", err)
	}
	resp := protocol.Response{
		Model:      w.Model,
		StopReason: stopFromAnthropic(w.StopReason),
		Usage: protocol.Usage{
			Input:      w.Usage.InputTokens,
			Output:     w.Usage.OutputTokens,
			CacheRead:  w.Usage.CacheReadInputTokens,
			CacheWrite: w.Usage.CacheCreationInputTokens,
		},
	}
	for _, b := range w.Content {
		switch b.Type {
		case "text":
			resp.Parts = append(resp.Parts, protocol.Text(b.Text))
		case "tool_use":
			input := string(b.Input)
			if input == "" || input == "null" {
				input = "{}"
			}
			resp.Parts = append(resp.Parts, protocol.ToolUse(b.ID, b.Name, input))
		case "thinking":
			resp.Parts = append(resp.Parts, protocol.Thinking(b.Thinking))
		}
	}
	return resp, nil
}

// BuildResponse 构建发给客户端的响应体。
func BuildResponse(resp protocol.Response) (int, []byte) {
	w := wireResponse{
		ID:         protocol.NewID("msg"),
		Type:       "message",
		Role:       "assistant",
		Model:      resp.Model,
		StopReason: stopToAnthropic(resp.StopReason),
		Usage: wireUsage{
			InputTokens:              resp.Usage.Input,
			OutputTokens:             resp.Usage.Output,
			CacheReadInputTokens:     resp.Usage.CacheRead,
			CacheCreationInputTokens: resp.Usage.CacheWrite,
		},
	}
	if w.Model == "" {
		w.Model = "agw"
	}
	for _, p := range resp.Parts {
		switch p.Kind {
		case protocol.KindText:
			w.Content = append(w.Content, wireBlock{Type: "text", Text: p.Text})
		case protocol.KindToolUse:
			input := json.RawMessage(p.ToolInputJSON)
			if len(input) == 0 {
				input = json.RawMessage("{}")
			}
			w.Content = append(w.Content, wireBlock{Type: "tool_use", ID: p.ToolCallID, Name: p.ToolName, Input: input})
		case protocol.KindThinking:
			w.Content = append(w.Content, wireBlock{Type: "thinking", Thinking: p.Text})
		}
	}
	body, _ := json.Marshal(w)
	return 200, body
}

// ---- 流式解码：anthropic SSE → IR 事件 ----

type streamDecoder struct {
	r          *protocol.SSEReader
	startUsage protocol.Usage
	endInfo    *protocol.Event
}

// NewStreamDecoder 构造解码器。
func NewStreamDecoder(r io.Reader) protocol.StreamDecoder {
	return &streamDecoder{r: protocol.NewSSEReader(r)}
}

func (d *streamDecoder) Next() (protocol.Event, error) {
	for {
		frame, err := d.r.Next()
		if err != nil {
			return protocol.Event{}, err
		}
		if frame.Data == "" || frame.Data == "[DONE]" {
			continue
		}
		switch frame.Name {
		case "message_start":
			var e struct {
				Message struct {
					Model string    `json:"model"`
					Usage wireUsage `json:"usage"`
				} `json:"message"`
			}
			if json.Unmarshal([]byte(frame.Data), &e) != nil {
				continue
			}
			d.startUsage = protocol.Usage{
				Input:      e.Message.Usage.InputTokens,
				CacheRead:  e.Message.Usage.CacheReadInputTokens,
				CacheWrite: e.Message.Usage.CacheCreationInputTokens,
			}
			return protocol.Event{Kind: protocol.EvStreamStart, Model: e.Message.Model, Usage: d.startUsage}, nil
		case "content_block_start":
			var e struct {
				Index        int       `json:"index"`
				ContentBlock wireBlock `json:"content_block"`
			}
			if json.Unmarshal([]byte(frame.Data), &e) != nil {
				continue
			}
			var block protocol.Part
			switch e.ContentBlock.Type {
			case "text":
				block = protocol.Part{Kind: protocol.KindText}
			case "thinking":
				block = protocol.Part{Kind: protocol.KindThinking}
			case "tool_use":
				block = protocol.Part{Kind: protocol.KindToolUse, ToolCallID: e.ContentBlock.ID, ToolName: e.ContentBlock.Name}
			default:
				continue
			}
			return protocol.Event{Kind: protocol.EvBlockStart, Index: e.Index, Block: block}, nil
		case "content_block_delta":
			var e struct {
				Index int `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					Thinking    string `json:"thinking"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if json.Unmarshal([]byte(frame.Data), &e) != nil {
				continue
			}
			switch e.Delta.Type {
			case "text_delta":
				return protocol.Event{Kind: protocol.EvTextDelta, Index: e.Index, Text: e.Delta.Text}, nil
			case "thinking_delta":
				return protocol.Event{Kind: protocol.EvThinkingDelta, Index: e.Index, Text: e.Delta.Thinking}, nil
			case "input_json_delta":
				return protocol.Event{Kind: protocol.EvToolCallDelta, Index: e.Index, ToolDelta: e.Delta.PartialJSON}, nil
			}
		case "content_block_stop":
			var e struct {
				Index int `json:"index"`
			}
			if json.Unmarshal([]byte(frame.Data), &e) != nil {
				continue
			}
			return protocol.Event{Kind: protocol.EvBlockStop, Index: e.Index}, nil
		case "message_delta":
			var e struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal([]byte(frame.Data), &e) != nil {
				continue
			}
			d.endInfo = &protocol.Event{
				Kind:       protocol.EvStreamEnd,
				StopReason: stopFromAnthropic(e.Delta.StopReason),
				Usage: protocol.Usage{
					Input: d.startUsage.Input, Output: e.Usage.OutputTokens,
					CacheRead: d.startUsage.CacheRead, CacheWrite: d.startUsage.CacheWrite,
				},
			}
		case "message_stop":
			if d.endInfo != nil {
				ev := *d.endInfo
				d.endInfo = nil
				return ev, nil
			}
			return protocol.Event{Kind: protocol.EvStreamEnd, Usage: d.startUsage}, nil
		case "error":
			var e struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			_ = json.Unmarshal([]byte(frame.Data), &e)
			return protocol.Event{Kind: protocol.EvStreamError, ErrMessage: e.Error.Message}, nil
		case "ping":
			// 心跳忽略
		}
	}
}

// ---- 流式编码：IR 事件 → anthropic SSE ----

type streamEncoder struct {
	w     *protocol.SSEWriter
	ended bool
	msgID string
	model string
}

// NewStreamEncoder 构造编码器。
func NewStreamEncoder(w io.Writer) protocol.StreamEncoder {
	return &streamEncoder{w: protocol.NewSSEWriter(w, nil)}
}

func (e *streamEncoder) Encode(ev protocol.Event) error {
	switch ev.Kind {
	case protocol.EvStreamStart:
		e.model = ev.Model
		e.msgID = protocol.NewID("msg")
		payload := map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id": e.msgID, "type": "message", "role": "assistant", "model": orDefault(ev.Model, "agw"),
				"content": []any{},
				"usage": map[string]any{
					"input_tokens": ev.Usage.Input, "output_tokens": 1,
					"cache_read_input_tokens": ev.Usage.CacheRead, "cache_creation_input_tokens": ev.Usage.CacheWrite,
				},
			},
		}
		return e.sendJSON("message_start", payload)
	case protocol.EvBlockStart:
		var block map[string]any
		switch ev.Block.Kind {
		case protocol.KindText:
			block = map[string]any{"type": "text", "text": ""}
		case protocol.KindThinking:
			block = map[string]any{"type": "thinking", "thinking": ""}
		case protocol.KindToolUse:
			block = map[string]any{"type": "tool_use", "id": ev.Block.ToolCallID, "name": ev.Block.ToolName, "input": map[string]any{}}
		default:
			block = map[string]any{"type": "text", "text": ""}
		}
		return e.sendJSON("content_block_start", map[string]any{"type": "content_block_start", "index": ev.Index, "content_block": block})
	case protocol.EvTextDelta:
		return e.sendJSON("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": ev.Index,
			"delta": map[string]any{"type": "text_delta", "text": ev.Text},
		})
	case protocol.EvThinkingDelta:
		return e.sendJSON("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": ev.Index,
			"delta": map[string]any{"type": "thinking_delta", "thinking": ev.Text},
		})
	case protocol.EvToolCallDelta:
		return e.sendJSON("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": ev.Index,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": ev.ToolDelta},
		})
	case protocol.EvBlockStop:
		return e.sendJSON("content_block_stop", map[string]any{"type": "content_block_stop", "index": ev.Index})
	case protocol.EvStreamEnd:
		if err := e.sendJSON("message_delta", map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": stopToAnthropic(ev.StopReason), "stop_sequence": nil},
			"usage": map[string]any{"output_tokens": ev.Usage.Output},
		}); err != nil {
			return err
		}
		if err := e.sendJSON("message_stop", map[string]any{"type": "message_stop"}); err != nil {
			return err
		}
		e.ended = true
		return nil
	case protocol.EvStreamError:
		return e.sendJSON("error", map[string]any{
			"type":  "error",
			"error": map[string]any{"type": "api_error", "message": orDefault(ev.ErrMessage, "上游流错误")},
		})
	}
	return nil
}

// Finish 在未收到正常结束事件时合成终止帧，保证客户端不死等。
func (e *streamEncoder) Finish() error {
	if e.ended {
		return nil
	}
	return e.Encode(protocol.Event{Kind: protocol.EvStreamEnd, StopReason: protocol.StopOther})
}

func (e *streamEncoder) sendJSON(name string, payload any) error {
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
