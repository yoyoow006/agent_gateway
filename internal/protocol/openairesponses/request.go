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
	Type    string          `json:"type"` // message | function_call | function_call_output | reasoning | additional_tools | custom_tool_call | custom_tool_call_output
	Role    string          `json:"role,omitempty"`
	Content json.RawMessage `json:"content,omitempty"` // string 或 parts
	// function_call / custom_tool_call
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	// custom_tool_call：调用输入为原始文本（非 JSON）
	Input string `json:"input,omitempty"`
	// function_call_output / custom_tool_call_output
	Output json.RawMessage `json:"output,omitempty"` // string 或 parts
	// reasoning
	Summary []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"summary,omitempty"`
	// additional_tools：Codex ≥0.149 工具编排（namespace 树内嵌 function/custom）
	Tools []wireAddTool `json:"tools,omitempty"`
}

// wireAddTool 是 additional_tools 的内嵌工具定义。
type wireAddTool struct {
	Type        string          `json:"type"` // namespace | function | custom
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"` // function
	Tools       []wireAddTool   `json:"tools,omitempty"`      // namespace 内嵌
}

type wireTool struct {
	Type        string          `json:"type"` // function
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type wireRequest struct {
	Model              string          `json:"model"`
	Instructions       string          `json:"instructions,omitempty"`
	Input              json.RawMessage `json:"input"` // string 或 items
	PreviousResponseID string          `json:"previous_response_id,omitempty"`
	// 以下顶层字段无跨协议对应，解析仅为告警
	Reasoning         json.RawMessage `json:"reasoning,omitempty"`
	Include           json.RawMessage `json:"include,omitempty"`
	Text              json.RawMessage `json:"text,omitempty"`
	ParallelToolCalls json.RawMessage `json:"parallel_tool_calls,omitempty"`
	PromptCacheKey    json.RawMessage `json:"prompt_cache_key,omitempty"`
	Tools             []wireTool      `json:"tools,omitempty"`
	ToolChoice        json.RawMessage `json:"tool_choice,omitempty"`
	MaxOutputTokens   int             `json:"max_output_tokens,omitempty"`
	Temperature       *float64        `json:"temperature,omitempty"`
	TopP              *float64        `json:"top_p,omitempty"`
	Stream            bool            `json:"stream,omitempty"`
	Store             *bool           `json:"store,omitempty"`
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
	if w.PreviousResponseID != "" {
		protocol.NotifyDrop("previous_response_id 被丢弃（agw 无状态路由，请求必须自包含上下文；请确认客户端已禁用响应存储）")
	}
	for name, raw := range map[string]json.RawMessage{
		"reasoning": w.Reasoning, "include": w.Include, "text": w.Text,
		"parallel_tool_calls": w.ParallelToolCalls, "prompt_cache_key": w.PromptCacheKey,
	} {
		if len(raw) > 0 {
			protocol.NotifyDrop("responses 顶层字段 " + name + " 无跨协议对应，将被丢弃")
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
		case "additional_tools":
			// Codex ≥0.149 工具编排：namespace 树展开为 <ns>.<name> 点连名；
			// function 直取 schema，custom 标记后由目标协议合成 code 参数。
			flattenAdditionalTools("", it.Tools, &req.Tools)
		case "custom_tool_call":
			req.Turns = append(req.Turns, protocol.Turn{Role: "assistant", Parts: []protocol.Part{
				protocol.ToolUse(it.CallID, it.Name, wrapCustomInput(it.Input)),
			}})
		case "custom_tool_call_output":
			req.Turns = append(req.Turns, protocol.Turn{Role: "user", Parts: []protocol.Part{
				protocol.ToolResult(it.CallID, parseOutput(it.Output), false),
			}})
		default:
			protocol.NotifyDrop("responses input 未知条目类型 " + it.Type + " 被跳过")
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
	// TotalTokens 必填：严格客户端（Codex 0.149+）缺失该字段即解析断流。
	TotalTokens int `json:"total_tokens"`
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
		Usage: &wireUsage{InputTokens: resp.Usage.Input, OutputTokens: resp.Usage.Output, TotalTokens: resp.Usage.Input + resp.Usage.Output},
	}
	if w.Model == "" {
		w.Model = "agw"
	}
	for _, p := range resp.Parts {
		switch p.Kind {
		case protocol.KindText:
			w.Output = append(w.Output, inputItem{Type: "message", Role: "assistant", Content: mustJSON([]inputPart{{Type: "output_text", Text: p.Text}})})
		case protocol.KindToolUse:
			if p.CustomTool {
				w.Output = append(w.Output, inputItem{Type: "custom_tool_call", CallID: p.ToolCallID, Name: p.ToolName, Input: protocol.UnwrapCustomInput(p.ToolInputJSON)})
				continue
			}
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

// flattenAdditionalTools 把 additional_tools 的 namespace 树展开为点连名扁平 ToolDef。
// 未知内嵌类型仅丢弃该工具并告警（不整体失败），与"未知事件忽略不失败"原则一致。
func flattenAdditionalTools(prefix string, tools []wireAddTool, out *[]protocol.ToolDef) {
	for _, t := range tools {
		name := t.Name
		if prefix != "" {
			name = prefix + "." + t.Name
		}
		switch t.Type {
		case "namespace":
			flattenAdditionalTools(name, t.Tools, out)
		case "function":
			*out = append(*out, protocol.ToolDef{Name: name, Description: t.Description, Schema: t.Parameters})
		case "custom":
			*out = append(*out, protocol.ToolDef{Name: name, Description: t.Description, Custom: true})
		default:
			protocol.NotifyDrop("additional_tools 内未知工具类型 " + t.Type + " 已跳过: " + name)
		}
	}
}

// wrapCustomInput 把 custom 调用的原始文本输入包装为 {"code": ...} JSON。
func wrapCustomInput(input string) string {
	b, err := json.Marshal(map[string]string{"code": input})
	if err != nil {
		return "{}"
	}
	return string(b)
}

// ExtractCustomTools 从客户端原始请求体提取 custom 型工具名单（点连名）。
// 网关在响应翻译路径用它把命中名单的 tool_use 标记为 custom 调用，随请求无状态。
func ExtractCustomTools(body []byte) map[string]bool {
	var w struct {
		Input []struct {
			Type  string `json:"type"`
			Tools []wireAddTool `json:"tools"`
		} `json:"input"`
	}
	if err := json.Unmarshal(body, &w); err != nil {
		return nil
	}
	var set map[string]bool
	for _, it := range w.Input {
		if it.Type != "additional_tools" {
			continue
		}
		var defs []protocol.ToolDef
		flattenAdditionalTools("", it.Tools, &defs)
		for _, d := range defs {
			if d.Custom {
				if set == nil {
					set = map[string]bool{}
				}
				set[d.Name] = true
			}
		}
	}
	return set
}
