// Package openairesponses 实现 OpenAI Responses API 的 IR 编解码器。
package openairesponses

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"agent_gateway/internal/protocol"
)

// Path 是 Responses 端点路径。
const Path = "/v1/responses"

// ---- 请求 ----

type inputPart struct {
	Type     string `json:"type"` // input_text | output_text | input_image
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"` // data URL
}

type inputItem struct {
	Type    string          `json:"type"` // message | function_call | function_call_output | reasoning
	Role    string          `json:"role,omitempty"`
	Content json.RawMessage `json:"content,omitempty"` // string 或 parts
	// function_call
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	// function_call_output
	Output json.RawMessage `json:"output,omitempty"` // string 或 parts
	// reasoning
	Summary []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"summary,omitempty"`
}

type wireTool struct {
	Type        string          `json:"type"` // function
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type wireRequest struct {
	Model           string          `json:"model"`
	Instructions    string          `json:"instructions,omitempty"`
	Input           json.RawMessage `json:"input"` // string 或 items
	Tools           []wireTool      `json:"tools,omitempty"`
	ToolChoice      json.RawMessage `json:"tool_choice,omitempty"`
	MaxOutputTokens int             `json:"max_output_tokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	TopP            *float64        `json:"top_p,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
	Store           *bool           `json:"store,omitempty"`
}

// ParseRequest 解析 Responses 请求为 IR。
func ParseRequest(body []byte) (protocol.Request, error) {
	var w wireRequest
	if err := json.Unmarshal(body, &w); err != nil {
		return protocol.Request{}, fmt.Errorf("解析 responses 请求: %w", err)
	}
	req := protocol.Request{
		Model:       w.Model,
		MaxTokens:   w.MaxOutputTokens,
		Temperature: w.Temperature,
		TopP:        w.TopP,
		Stream:      w.Stream,
	}
	if w.Instructions != "" {
		req.System = append(req.System, w.Instructions)
	}
	var items []inputItem
	if len(w.Input) > 0 && w.Input[0] == '"' {
		var s string
		if err := json.Unmarshal(w.Input, &s); err == nil && s != "" {
			items = []inputItem{{Type: "message", Role: "user", Content: json.RawMessage(`"` + jsonEscape(s) + `"`)}}
		}
	} else if len(w.Input) > 0 {
		if err := json.Unmarshal(w.Input, &items); err != nil {
			return protocol.Request{}, fmt.Errorf("解析 responses input: %w", err)
		}
	}
	for _, it := range items {
		switch it.Type {
		case "message":
			parts := parseContentParts(it.Content)
			if it.Role == "system" || it.Role == "developer" {
				if s := joinText(parts); s != "" {
					req.System = append(req.System, s)
				}
				continue
			}
			role := it.Role
			if role != "assistant" {
				role = "user"
			}
			req.Turns = append(req.Turns, protocol.Turn{Role: role, Parts: parts})
		case "function_call":
			args := it.Arguments
			if args == "" {
				args = "{}"
			}
			req.Turns = append(req.Turns, protocol.Turn{Role: "assistant", Parts: []protocol.Part{
				protocol.ToolUse(it.CallID, it.Name, args),
			}})
		case "function_call_output":
			req.Turns = append(req.Turns, protocol.Turn{Role: "user", Parts: []protocol.Part{
				protocol.ToolResult(it.CallID, parseOutput(it.Output), false),
			}})
		case "reasoning":
			var sb strings.Builder
			for _, s := range it.Summary {
				if s.Text != "" {
					if sb.Len() > 0 {
						sb.WriteString("\n")
					}
					sb.WriteString(s.Text)
				}
			}
			if sb.Len() > 0 {
				req.Turns = append(req.Turns, protocol.Turn{Role: "assistant", Parts: []protocol.Part{
					protocol.Thinking(sb.String()),
				}})
			}
		}
	}
	// 相邻同角色 input 条目（如 function_call_output + user 消息）合并为单轮。
	req.Turns = protocol.NormalizeTurns(req.Turns)
	for _, t := range w.Tools {
		if t.Type != "" && t.Type != "function" {
			continue
		}
		req.Tools = append(req.Tools, protocol.ToolDef{
			Name: t.Name, Description: t.Description, Schema: t.Parameters,
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
				Type string `json:"type"`
				Name string `json:"name"`
			}
			if json.Unmarshal(w.ToolChoice, &obj) == nil && obj.Type == "function" {
				req.ToolChoice = protocol.ToolChoice{Mode: protocol.ToolChoiceNamed, Name: obj.Name}
			}
		}
	}
	return req, nil
}

// parseContentParts 解析 message.content（string 或 parts 数组）。
func parseContentParts(raw json.RawMessage) []protocol.Part {
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		if s == "" {
			return nil
		}
		return []protocol.Part{protocol.Text(s)}
	}
	var parts []inputPart
	if json.Unmarshal(raw, &parts) != nil {
		return nil
	}
	out := make([]protocol.Part, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case "input_text", "output_text", "text":
			if p.Text != "" {
				out = append(out, protocol.Text(p.Text))
			}
		case "input_image":
			if u := p.ImageURL; strings.HasPrefix(u, "data:") {
				if media, data, ok := splitDataURL(u); ok {
					out = append(out, protocol.Image(media, data))
				}
			}
		}
	}
	return out
}

// parseOutput 解析 function_call_output.output。
func parseOutput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	return joinText(parseContentParts(raw))
}

func joinText(parts []protocol.Part) string {
	texts := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.Kind == protocol.KindText {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}

func splitDataURL(u string) (media, data string, ok bool) {
	rest := strings.TrimPrefix(u, "data:")
	semi := strings.Index(rest, ";base64,")
	if semi < 0 {
		return "", "", false
	}
	return rest[:semi], rest[semi+len(";base64,"):], true
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return strings.Trim(string(b), `"`)
}

// BuildRequest 把 IR 构建为 Responses 请求；store 恒为 false（无状态可切换前提）。
func BuildRequest(req protocol.Request) (string, http.Header, []byte, error) {
	w := wireRequest{
		Model:           req.Model,
		MaxOutputTokens: req.MaxTokens,
		Temperature:     req.Temperature,
		TopP:            req.TopP,
		Stream:          req.Stream,
		Store:           boolPtr(false),
	}
	if len(req.System) > 0 {
		w.Instructions = strings.Join(req.System, "\n\n")
	}
	for _, t := range req.Tools {
		wt := wireTool{Type: "function", Name: t.Name, Description: t.Description, Parameters: t.Schema}
		if len(wt.Parameters) == 0 {
			wt.Parameters = json.RawMessage(`{"type":"object"}`)
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
		raw, _ := json.Marshal(map[string]string{"type": "function", "name": req.ToolChoice.Name})
		w.ToolChoice = raw
	}

	var items []inputItem
	for _, turn := range protocol.NormalizeTurns(req.Turns) {
		var textParts, imageParts []protocol.Part
		var toolParts, resultParts []protocol.Part
		for _, p := range turn.Parts {
			switch p.Kind {
			case protocol.KindText:
				textParts = append(textParts, p)
			case protocol.KindImage:
				imageParts = append(imageParts, p)
			case protocol.KindToolUse:
				toolParts = append(toolParts, p)
			case protocol.KindToolResult:
				resultParts = append(resultParts, p)
			case protocol.KindThinking:
				// 思考块不回传上游（部分上游对 reasoning 输入校验严格）
			}
		}
		if turn.Role == "user" {
			for _, p := range resultParts {
				items = append(items, inputItem{Type: "function_call_output", CallID: p.ToolCallID, Output: json.RawMessage(jsonString(p.ToolResult))})
			}
			var parts []inputPart
			for _, p := range textParts {
				parts = append(parts, inputPart{Type: "input_text", Text: p.Text})
			}
			for _, p := range imageParts {
				parts = append(parts, inputPart{Type: "input_image", ImageURL: "data:" + p.ImageMediaType + ";base64," + p.ImageData})
			}
			if len(parts) > 0 {
				items = append(items, inputItem{Type: "message", Role: "user", Content: mustJSON(parts)})
			}
			continue
		}
		// assistant
		for _, p := range toolParts {
			args := p.ToolInputJSON
			if args == "" {
				args = "{}"
			}
			items = append(items, inputItem{Type: "function_call", CallID: p.ToolCallID, Name: p.ToolName, Arguments: args})
		}
		var parts []inputPart
		for _, p := range textParts {
			parts = append(parts, inputPart{Type: "output_text", Text: p.Text})
		}
		if len(parts) > 0 {
			items = append(items, inputItem{Type: "message", Role: "assistant", Content: mustJSON(parts)})
		}
	}
	w.Input = mustJSON(items)
	body, err := json.Marshal(w)
	if err != nil {
		return "", nil, nil, err
	}
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	return Path, h, body, nil
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func boolPtr(b bool) *bool { return &b }

// ---- 非流式响应 ----

type wireUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type wireResponse struct {
	ID                string      `json:"id"`
	Object            string      `json:"object"`
	Status            string      `json:"status"`
	Model             string      `json:"model"`
	Output            []inputItem `json:"output"`
	Usage             *wireUsage  `json:"usage,omitempty"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details,omitempty"`
}

// ParseResponse 解析 2xx 响应。
func ParseResponse(status int, body []byte) (protocol.Response, error) {
	var w wireResponse
	if err := json.Unmarshal(body, &w); err != nil {
		return protocol.Response{}, fmt.Errorf("解析 responses 响应: %w", err)
	}
	resp := protocol.Response{Model: w.Model, StopReason: protocol.StopEndTurn}
	sawTool := false
	for _, it := range w.Output {
		switch it.Type {
		case "message":
			resp.Parts = append(resp.Parts, parseContentParts(it.Content)...)
		case "function_call":
			sawTool = true
			args := it.Arguments
			if args == "" {
				args = "{}"
			}
			resp.Parts = append(resp.Parts, protocol.ToolUse(it.CallID, it.Name, args))
		case "reasoning":
			var sb strings.Builder
			for _, s := range it.Summary {
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(s.Text)
			}
			resp.Parts = append(resp.Parts, protocol.Thinking(sb.String()))
		}
	}
	if sawTool {
		resp.StopReason = protocol.StopToolUse
	} else if w.Status == "incomplete" && w.IncompleteDetails != nil && w.IncompleteDetails.Reason == "max_output_tokens" {
		resp.StopReason = protocol.StopMaxTokens
	}
	if w.Usage != nil {
		resp.Usage = protocol.Usage{Input: w.Usage.InputTokens, Output: w.Usage.OutputTokens}
	}
	return resp, nil
}

// BuildResponse 构建响应体。
func BuildResponse(resp protocol.Response) (int, []byte) {
	w := wireResponse{
		ID: protocol.NewID("resp"), Object: "response", Status: "completed",
		Model: resp.Model,
		Usage: &wireUsage{InputTokens: resp.Usage.Input, OutputTokens: resp.Usage.Output},
	}
	if w.Model == "" {
		w.Model = "agw"
	}
	for _, p := range resp.Parts {
		switch p.Kind {
		case protocol.KindText:
			w.Output = append(w.Output, inputItem{Type: "message", Role: "assistant", Content: mustJSON([]inputPart{{Type: "output_text", Text: p.Text}})})
		case protocol.KindToolUse:
			args := p.ToolInputJSON
			if args == "" {
				args = "{}"
			}
			w.Output = append(w.Output, inputItem{Type: "function_call", CallID: p.ToolCallID, Name: p.ToolName, Arguments: args})
		case protocol.KindThinking:
			w.Output = append(w.Output, inputItem{Type: "reasoning", Summary: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{{Type: "summary_text", Text: p.Text}}})
		}
	}
	if resp.StopReason == protocol.StopToolUse {
		w.Status = "completed"
	}
	body, _ := json.Marshal(w)
	return 200, body
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

// BuildError 构造 responses 格式错误体。
func BuildError(status int, msg string) []byte {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"code":    errorCode(status),
			"message": msg,
		},
	})
	return body
}

func errorCode(status int) string {
	switch {
	case status == 429:
		return "rate_limit_exceeded"
	case status >= 400 && status < 500:
		return "invalid_request_error"
	default:
		return "server_error"
	}
}
