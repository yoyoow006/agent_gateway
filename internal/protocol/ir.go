package protocol

// 中立中间表示（IR）：三个线上协议（anthropic / openai-chat / openai-responses）
// 经各自的 Codec 汇入 IR，再从 IR 构建为任意目标协议，覆盖全部客户端×供应商组合。

// PartKind 是内容块种类。
type PartKind string

const (
	KindText       PartKind = "text"
	KindImage      PartKind = "image"
	KindToolUse    PartKind = "tool_use"
	KindToolResult PartKind = "tool_result"
	KindThinking   PartKind = "thinking"
)

// Part 是一个内容块。只携带各协议共同可表达的字段。
type Part struct {
	Kind            PartKind
	Text            string // text / thinking 正文
	ImageMediaType  string // image
	ImageData       string // image，base64（不含 data: 前缀）
	ToolCallID      string // tool_use.id / tool_result.tool_use_id / call_id
	ToolName        string // tool_use
	ToolInputJSON   string // tool_use 输入参数（JSON 字符串，空视为 "{}"）
	ToolResult      string // tool_result 文本内容
	ToolResultIsErr bool   // tool_result 标记错误
	// CustomTool 标记该 tool_use 源自 Responses custom 型工具（输入为原始文本）；
	// 网关响应翻译按请求携带的 custom 名单打标，responses 编码器据此还原 custom_tool_call。
	CustomTool bool
}

// 构造器。
func Text(s string) Part     { return Part{Kind: KindText, Text: s} }
func Thinking(s string) Part { return Part{Kind: KindThinking, Text: s} }
func Image(media, b64 string) Part {
	return Part{Kind: KindImage, ImageMediaType: media, ImageData: b64}
}
func ToolUse(id, name, inputJSON string) Part {
	return Part{Kind: KindToolUse, ToolCallID: id, ToolName: name, ToolInputJSON: inputJSON}
}
func ToolResult(id, content string, isErr bool) Part {
	return Part{Kind: KindToolResult, ToolCallID: id, ToolResult: content, ToolResultIsErr: isErr}
}

// Turn 是一轮对话；tool_result 属于 user 轮，工具调用属于 assistant 轮。
type Turn struct {
	Role  string // "user" | "assistant"
	Parts []Part
}

// ToolDef 是工具定义（JSON Schema 参数）。
type ToolDef struct {
	Name        string
	Description string
	Schema      []byte
	// Custom 标记 Responses 的 custom 型工具（无 JSON schema，调用输入为原始文本）；
	// 目标协议构建时合成 {code: string} 单参数 schema，响应侧据此还原 custom 调用形态。
	Custom bool
}

// ToolChoiceMode 是工具选择模式。
type ToolChoiceMode string

const (
	ToolChoiceNone     ToolChoiceMode = "none"
	ToolChoiceAuto     ToolChoiceMode = "auto"
	ToolChoiceRequired ToolChoiceMode = "required"
	ToolChoiceNamed    ToolChoiceMode = "tool"
)

// ToolChoice 为零值（Mode==""）时表示请求未携带该字段。
type ToolChoice struct {
	Mode ToolChoiceMode
	Name string
}

// Request 是翻译后的补全请求。
type Request struct {
	Model       string
	System      []string
	Turns       []Turn
	Tools       []ToolDef
	ToolChoice  ToolChoice
	MaxTokens   int // 0 = 未指定
	Temperature *float64
	TopP        *float64
	Stop        []string
	Stream      bool
}

// StopReason 是统一结束原因。
type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
	StopOther     StopReason = "other"
)

// Usage 是 token 用量。
type Usage struct {
	Input      int
	Output     int
	CacheRead  int
	CacheWrite int
}

// Response 是非流式补全结果。
type Response struct {
	Model      string
	Parts      []Part
	StopReason StopReason
	Usage      Usage
}

// EventKind 是流式事件种类。
type EventKind string

const (
	EvStreamStart   EventKind = "stream_start"
	EvBlockStart    EventKind = "block_start"
	EvTextDelta     EventKind = "text_delta"
	EvThinkingDelta EventKind = "thinking_delta"
	EvToolCallDelta EventKind = "tool_call_delta"
	EvBlockStop     EventKind = "block_stop"
	EvStreamEnd     EventKind = "stream_end"
	EvStreamError   EventKind = "stream_error"
)

// Event 是一个流式事件。字段按 Kind 取用。
type Event struct {
	Kind       EventKind
	Index      int        // 块索引
	Block      Part       // EvBlockStart 的块骨架（Kind/ToolName/ToolCallID/图片元数据）
	Text       string     // text/thinking 增量
	ToolDelta  string     // 工具参数 JSON 增量
	Model      string     // EvStreamStart
	StopReason StopReason // EvStreamEnd
	Usage      Usage      // EvStreamStart（input 侧）/ EvStreamEnd（全量）
	ErrMessage string
}

// NormalizeTurns 合并相邻同角色轮次（部分协议要求 user/assistant 交替）。
func NormalizeTurns(turns []Turn) []Turn {
	var out []Turn
	for _, t := range turns {
		if t.Role == "" {
			t.Role = "user"
		}
		if n := len(out); n > 0 && out[n-1].Role == t.Role {
			out[n-1].Parts = append(out[n-1].Parts, t.Parts...)
			continue
		}
		out = append(out, t)
	}
	return out
}
