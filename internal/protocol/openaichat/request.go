// Package openaichat 实现 OpenAI Chat Completions 协议的 IR 编解码器。
package openaichat

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"agent_gateway/internal/protocol"
)

// Path 是 Chat Completions 端点路径。
const Path = "/v1/chat/completions"

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type toolCall struct {
	Index    *int         `json:"index,omitempty"` // 仅流式增量携带
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function functionCall `json:"function"`
}

type imageURL struct {
	URL string `json:"url"`
}

type contentPart struct {
	Type     string    `json:"type"` // text | image_url
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type wireMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"` // string 或 parts 数组
	ToolCalls  []toolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type wireTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

type wireRequest struct {
	Model               string          `json:"model"`
	Messages            []wireMessage   `json:"messages"`
	Tools               []wireTool      `json:"tools,omitempty"`
	ToolChoice          json.RawMessage `json:"tool_choice,omitempty"`
	MaxTokens           int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	Temperature         *float64        `json:"temperature,omitempty"`
	TopP                *float64        `json:"top_p,omitempty"`
	Stop                json.RawMessage `json:"stop,omitempty"` // string 或 []string
	Stream              bool            `json:"stream,omitempty"`
	StreamOptions       *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
}

// ParseRequest 解析 Chat Completions 请求为 IR。
func ParseRequest(body []byte) (protocol.Request, error) {
	var w wireRequest
	if err := json.Unmarshal(body, &w); err != nil {
		return protocol.Request{}, fmt.Errorf("解析 chat 请求: %w", err)
	}
	req := protocol.Request{
		Model:       w.Model,
		Temperature: w.Temperature,
		TopP:        w.TopP,
		Stream:      w.Stream,
		MaxTokens:   w.MaxTokens,
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = w.MaxCompletionTokens
	}
	if len(w.Stop) > 0 {
		if w.Stop[0] == '"' {
			var s string
			if json.Unmarshal(w.Stop, &s) == nil && s != "" {
				req.Stop = []string{s}
			}
		} else {
			_ = json.Unmarshal(w.Stop, &req.Stop)
		}
	}
	for _, m := range w.Messages {
		switch m.Role {
		case "system", "developer":
			if s := parseTextContent(m.Content); s != "" {
				req.System = append(req.System, s)
			}
		case "tool":
			req.Turns = append(req.Turns, protocol.Turn{Role: "user", Parts: []protocol.Part{
				protocol.ToolResult(m.ToolCallID, parseTextContent(m.Content), false),
			}})
		case "assistant":
			turn := protocol.Turn{Role: "assistant"}
			if s := parseTextContent(m.Content); s != "" {
				turn.Parts = append(turn.Parts, protocol.Text(s))
			}
			for _, tc := range m.ToolCalls {
				args := tc.Function.Arguments
				if args == "" {
					args = "{}"
				}
				turn.Parts = append(turn.Parts, protocol.ToolUse(tc.ID, tc.Function.Name, args))
			}
			req.Turns = append(req.Turns, turn)
		default: // user
			parts, err := parseContentParts(m.Content)
			if err != nil {
				return protocol.Request{}, err
			}
			req.Turns = append(req.Turns, protocol.Turn{Role: "user", Parts: parts})
		}
	}
	for _, t := range w.Tools {
		if t.Type != "" && t.Type != "function" {
			continue
		}
		req.Tools = append(req.Tools, protocol.ToolDef{
			Name: t.Function.Name, Description: t.Function.Description, Schema: t.Function.Parameters,
		})
	}
	if len(w.ToolChoice) > 0 {
		if w.ToolChoice[0] == '"' {
			var s string
			_ = json.Unmarshal(w.ToolChoice, &s)
			switch s {
			case "auto":
				req.ToolChoice = protocol.ToolChoice{Mode: protocol.ToolChoiceAuto}
			case "none":
				req.ToolChoice = protocol.ToolChoice{Mode: protocol.ToolChoiceNone}
			case "required":
				req.ToolChoice = protocol.ToolChoice{Mode: protocol.ToolChoiceRequired}
			}
		} else {
			var obj struct {
				Type     string `json:"type"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			}
			if json.Unmarshal(w.ToolChoice, &obj) == nil && obj.Type == "function" {
				req.ToolChoice = protocol.ToolChoice{Mode: protocol.ToolChoiceNamed, Name: obj.Function.Name}
			}
		}
	}
	return req, nil
}

// parseTextContent 把 content（string 或 parts）压成纯文本。
func parseTextContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	parts, err := parseContentParts(raw)
	if err != nil {
		return ""
	}
	texts := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.Kind == protocol.KindText {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// parseContentParts 解析 content parts；远程图片 URL 无法映射为 base64，报错。
func parseContentParts(raw json.RawMessage) ([]protocol.Part, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("解析 content: %w", err)
		}
		if s == "" {
			return nil, nil
		}
		return []protocol.Part{protocol.Text(s)}, nil
	}
	var parts []contentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("解析 content parts: %w", err)
	}
	out := make([]protocol.Part, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case "text":
			out = append(out, protocol.Text(p.Text))
		case "image_url":
			if p.ImageURL == nil {
				continue
			}
			media, data, ok := splitDataURL(p.ImageURL.URL)
			if !ok {
				return nil, &protocol.ErrUnmappedBlock{Detail: "图片仅支持 data: URL（远程 URL 无法跨协议映射）: " + p.ImageURL.URL}
			}
			out = append(out, protocol.Image(media, data))
		}
	}
	return out, nil
}

// splitDataURL 拆解 data:<media>;base64,<data>。
func splitDataURL(u string) (media, data string, ok bool) {
	if !strings.HasPrefix(u, "data:") {
		return "", "", false
	}
	rest := u[len("data:"):]
	semi := strings.Index(rest, ";base64,")
	if semi < 0 {
		return "", "", false
	}
	return rest[:semi], rest[semi+len(";base64,"):], true
}

// BuildRequest 把 IR 构建为 Chat Completions 请求。
func BuildRequest(req protocol.Request) (string, http.Header, []byte, error) {
	w := wireRequest{
		Model:       req.Model,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
		MaxTokens:   req.MaxTokens,
	}
	if w.MaxTokens > 0 {
		w.MaxTokens = req.MaxTokens
		w.MaxCompletionTokens = 0
	}
	if len(req.Stop) > 0 {
		raw, _ := json.Marshal(req.Stop)
		w.Stop = raw
	}
	if w.Stream {
		w.StreamOptions = &struct {
			IncludeUsage bool `json:"include_usage"`
		}{IncludeUsage: true}
	}
	if len(req.System) > 0 {
		raw, _ := json.Marshal(strings.Join(req.System, "\n\n"))
		w.Messages = append(w.Messages, wireMessage{Role: "system", Content: raw})
	}
	for _, t := range req.Tools {
		var wt wireTool
		wt.Type = "function"
		wt.Function.Name = t.Name
		wt.Function.Description = t.Description
		wt.Function.Parameters = t.Schema
		if t.Custom {
			// Responses custom 型工具：合成 {code} 单参数 schema，响应侧解包还原
			wt.Function.Parameters = protocol.CustomToolSchema()
		} else if len(wt.Function.Parameters) == 0 {
			wt.Function.Parameters = json.RawMessage(`{"type":"object"}`)
		}
		w.Tools = append(w.Tools, wt)
	}
	switch req.ToolChoice.Mode {
	case protocol.ToolChoiceAuto:
		w.ToolChoice = json.RawMessage(`"auto"`)
	case protocol.ToolChoiceNone:
		w.ToolChoice = json.RawMessage(`"none"`)
	case protocol.ToolChoiceRequired:
		w.ToolChoice = json.RawMessage(`"required"`)
	case protocol.ToolChoiceNamed:
		raw, _ := json.Marshal(map[string]any{"type": "function", "function": map[string]any{"name": req.ToolChoice.Name}})
		w.ToolChoice = raw
	}

	for _, turn := range protocol.NormalizeTurns(req.Turns) {
		if turn.Role == "user" {
			// tool_result 先行独立成 tool 消息，再发用户文本。
			var imageOrText []protocol.Part
			for _, p := range turn.Parts {
				if p.Kind == protocol.KindToolResult {
					raw, _ := json.Marshal(p.ToolResult)
					w.Messages = append(w.Messages, wireMessage{Role: "tool", ToolCallID: p.ToolCallID, Content: raw})
					continue
				}
				imageOrText = append(imageOrText, p)
			}
			if len(imageOrText) > 0 {
				raw, err := buildContent(imageOrText)
				if err != nil {
					return "", nil, nil, err
				}
				w.Messages = append(w.Messages, wireMessage{Role: "user", Content: raw})
			}
			continue
		}
		// assistant
		am := wireMessage{Role: "assistant"}
		var texts []string
		for _, p := range turn.Parts {
			switch p.Kind {
			case protocol.KindText:
				texts = append(texts, p.Text)
			case protocol.KindToolUse:
				args := p.ToolInputJSON
				if args == "" {
					args = "{}"
				}
				am.ToolCalls = append(am.ToolCalls, toolCall{
					ID: p.ToolCallID, Type: "function",
					Function: functionCall{Name: p.ToolName, Arguments: args},
				})
			case protocol.KindThinking:
				// chat 无对应，丢弃
			}
		}
		if len(texts) > 0 {
			raw, _ := json.Marshal(strings.Join(texts, "\n"))
			am.Content = raw
		}
		w.Messages = append(w.Messages, am)
	}
	body, err := json.Marshal(w)
	if err != nil {
		return "", nil, nil, err
	}
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	return Path, h, body, nil
}

// buildContent 构造 user content：单文本用字符串，否则 parts 数组。
func buildContent(parts []protocol.Part) (json.RawMessage, error) {
	if len(parts) == 1 && parts[0].Kind == protocol.KindText {
		raw, _ := json.Marshal(parts[0].Text)
		return raw, nil
	}
	out := make([]contentPart, 0, len(parts))
	for _, p := range parts {
		switch p.Kind {
		case protocol.KindText:
			out = append(out, contentPart{Type: "text", Text: p.Text})
		case protocol.KindImage:
			url := "data:" + p.ImageMediaType + ";base64," + p.ImageData
			out = append(out, contentPart{Type: "image_url", ImageURL: &imageURL{URL: url}})
		default:
			return nil, &protocol.ErrUnmappedBlock{Detail: "该内容块无法映射到 chat user 消息: " + string(p.Kind)}
		}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return raw, nil
}
