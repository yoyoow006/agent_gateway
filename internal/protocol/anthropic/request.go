// Package anthropic 实现 Anthropic Messages API 的 IR 编解码器。
package anthropic

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"agent_gateway/internal/protocol"
)

// APIVersion 是转发时携带的 anthropic-version。
const APIVersion = "2023-06-01"

// DefaultMaxTokens 是上游要求必填 max_tokens 而请求未携带时的默认值。
const DefaultMaxTokens = 8192

// Path 是 Messages 端点路径。
const Path = "/v1/messages"

type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type wireBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// image
	Source *imageSource `json:"source,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	// thinking
	Thinking string `json:"thinking,omitempty"`
	// cache_control 仅解析（跨协议翻译时丢弃并告警），构建时不回写
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
}

type wireTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

type wireToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type wireRequest struct {
	Model         string          `json:"model"`
	MaxTokens     int             `json:"max_tokens"`
	System        json.RawMessage `json:"system,omitempty"`
	TopK          json.RawMessage `json:"top_k,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
	Messages      []wireMessage   `json:"messages"`
	Temperature   *float64        `json:"temperature,omitempty"`
	TopP          *float64        `json:"top_p,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	Tools         []wireTool      `json:"tools,omitempty"`
	ToolChoice    *wireToolChoice `json:"tool_choice,omitempty"`
}

type wireMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// ParseRequest 解析 Messages API 请求体为 IR。
func ParseRequest(body []byte) (protocol.Request, error) {
	var w wireRequest
	if err := json.Unmarshal(body, &w); err != nil {
		return protocol.Request{}, fmt.Errorf("解析 anthropic 请求: %w", err)
	}
	req := protocol.Request{
		Model:       w.Model,
		MaxTokens:   w.MaxTokens,
		Temperature: w.Temperature,
		TopP:        w.TopP,
		Stop:        w.StopSequences,
		Stream:      w.Stream,
	}
	// system：字符串或文本块数组；cache_control 在此被丢弃（跨协议无对应）。
	cacheDrops := 0
	if len(w.System) > 0 {
		if w.System[0] == '"' {
			var s string
			if err := json.Unmarshal(w.System, &s); err == nil && s != "" {
				req.System = []string{s}
			}
		} else {
			var blocks []struct {
				Type         string          `json:"type"`
				Text         string          `json:"text"`
				CacheControl json.RawMessage `json:"cache_control"`
			}
			if err := json.Unmarshal(w.System, &blocks); err == nil {
				for _, b := range blocks {
					if b.Text != "" {
						req.System = append(req.System, b.Text)
					}
					if len(b.CacheControl) > 0 {
						cacheDrops++
					}
				}
			}
		}
	}
	for _, m := range w.Messages {
		turn := protocol.Turn{Role: m.Role}
		blocks, drops, err := parseBlocks(m.Content)
		if err != nil {
			return protocol.Request{}, err
		}
		cacheDrops += drops
		turn.Parts = blocks
		req.Turns = append(req.Turns, turn)
	}
	if cacheDrops > 0 {
		protocol.NotifyDrop(fmt.Sprintf("cache_control ×%d 仅 anthropic 协议支持，跨协议转发时将被丢弃", cacheDrops))
	}
	if len(w.TopK) > 0 {
		protocol.NotifyDrop("top_k 仅 anthropic 协议支持，跨协议转发时将被丢弃")
	}
	if len(w.Metadata) > 0 {
		protocol.NotifyDrop("metadata 仅 anthropic 协议支持，跨协议转发时将被丢弃")
	}
	for _, t := range w.Tools {
		req.Tools = append(req.Tools, protocol.ToolDef{
			Name: t.Name, Description: t.Description, Schema: t.InputSchema,
		})
	}
	if w.ToolChoice != nil {
		switch w.ToolChoice.Type {
		case "auto":
			req.ToolChoice = protocol.ToolChoice{Mode: protocol.ToolChoiceAuto}
		case "any", "required":
			req.ToolChoice = protocol.ToolChoice{Mode: protocol.ToolChoiceRequired}
		case "none":
			req.ToolChoice = protocol.ToolChoice{Mode: protocol.ToolChoiceNone}
		case "tool":
			req.ToolChoice = protocol.ToolChoice{Mode: protocol.ToolChoiceNamed, Name: w.ToolChoice.Name}
		}
	}
	return req, nil
}

// parseBlocks 解析消息 content：字符串或块数组；返回块列表与 cache_control 出现次数。
func parseBlocks(raw json.RawMessage) ([]protocol.Part, int, error) {
	if len(raw) == 0 {
		return nil, 0, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, 0, fmt.Errorf("解析 content 字符串: %w", err)
		}
		if s == "" {
			return nil, 0, nil
		}
		return []protocol.Part{protocol.Text(s)}, 0, nil
	}
	var blocks []wireBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, 0, fmt.Errorf("解析 content 块: %w", err)
	}
	parts := make([]protocol.Part, 0, len(blocks))
	drops := 0
	for _, b := range blocks {
		if len(b.CacheControl) > 0 {
			drops++
		}
		switch b.Type {
		case "text":
			parts = append(parts, protocol.Text(b.Text))
		case "image":
			if b.Source != nil && b.Source.Type == "base64" {
				parts = append(parts, protocol.Image(b.Source.MediaType, b.Source.Data))
			}
		case "tool_use":
			input := string(b.Input)
			if input == "" || input == "null" {
				input = "{}"
			}
			parts = append(parts, protocol.ToolUse(b.ID, b.Name, input))
		case "tool_result":
			content := parseResultContent(b.Content)
			parts = append(parts, protocol.ToolResult(b.ToolUseID, content, b.IsError))
		case "thinking":
			parts = append(parts, protocol.Thinking(b.Thinking))
		default:
			// 未知块（redacted_thinking 等）跳过，不失败。
		}
	}
	return parts, drops, nil
}

// parseResultContent 解析 tool_result.content（字符串或文本块数组）。
func parseResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
		return ""
	}
	var blocks []wireBlock
	if json.Unmarshal(raw, &blocks) == nil {
		texts := make([]string, 0, len(blocks))
		for _, b := range blocks {
			if b.Text != "" {
				texts = append(texts, b.Text)
			}
		}
		return strings.Join(texts, "\n")
	}
	return ""
}

// BuildRequest 把 IR 构建为 Messages API 请求。
func BuildRequest(req protocol.Request) (string, http.Header, []byte, error) {
	w := wireRequest{
		Model:         req.Model,
		MaxTokens:     req.MaxTokens,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		StopSequences: req.Stop,
		Stream:        req.Stream,
	}
	if w.MaxTokens <= 0 {
		w.MaxTokens = DefaultMaxTokens
	}
	if len(req.System) > 0 {
		blocks := make([]wireBlock, len(req.System))
		for i, s := range req.System {
			blocks[i] = wireBlock{Type: "text", Text: s}
		}
		raw, err := json.Marshal(blocks)
		if err != nil {
			return "", nil, nil, err
		}
		w.System = raw
	}
	for _, t := range req.Tools {
		schema := t.Schema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		w.Tools = append(w.Tools, wireTool{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	switch req.ToolChoice.Mode {
	case protocol.ToolChoiceAuto:
		w.ToolChoice = &wireToolChoice{Type: "auto"}
	case protocol.ToolChoiceNone:
		w.ToolChoice = &wireToolChoice{Type: "none"}
	case protocol.ToolChoiceRequired:
		w.ToolChoice = &wireToolChoice{Type: "any"}
	case protocol.ToolChoiceNamed:
		w.ToolChoice = &wireToolChoice{Type: "tool", Name: req.ToolChoice.Name}
	}

	turns := protocol.NormalizeTurns(req.Turns)
	for _, turn := range turns {
		blocks := make([]wireBlock, 0, len(turn.Parts))
		for _, p := range turn.Parts {
			switch p.Kind {
			case protocol.KindText:
				blocks = append(blocks, wireBlock{Type: "text", Text: p.Text})
			case protocol.KindImage:
				blocks = append(blocks, wireBlock{Type: "image", Source: &imageSource{Type: "base64", MediaType: p.ImageMediaType, Data: p.ImageData}})
			case protocol.KindToolUse:
				input := json.RawMessage(p.ToolInputJSON)
				if len(input) == 0 {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, wireBlock{Type: "tool_use", ID: p.ToolCallID, Name: p.ToolName, Input: input})
			case protocol.KindToolResult:
				content, _ := json.Marshal(p.ToolResult)
				blocks = append(blocks, wireBlock{Type: "tool_result", ToolUseID: p.ToolCallID, Content: content, IsError: p.ToolResultIsErr})
			case protocol.KindThinking:
				// 思考历史无 signature，回传会被扩展思考模型拒绝——与 responses 侧策略一致不回传
				protocol.NotifyDrop("thinking 历史块未回传上游（无 signature）")
			}
		}
		if len(blocks) == 0 {
			// 整轮都被丢弃（如仅 thinking 的 assistant 轮）：跳过该消息，
			// 避免产出空 text 块被上游 400 拒绝
			continue
		}
		raw, err := json.Marshal(blocks)
		if err != nil {
			return "", nil, nil, err
		}
		role := turn.Role
		if role != "user" && role != "assistant" {
			role = "user"
		}
		w.Messages = append(w.Messages, wireMessage{Role: role, Content: raw})
	}
	body, err := json.Marshal(w)
	if err != nil {
		return "", nil, nil, err
	}
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("Anthropic-Version", APIVersion)
	return Path, h, body, nil
}
