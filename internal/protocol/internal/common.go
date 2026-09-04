// Package internal 提供三 codec（anthropic / openaichat / openairesponses）
// 共享的协议无关小工具，避免在三处镜像实现。
package internal

import "encoding/json"

// OrDefault 返回 s；若为空则返回 def。
func OrDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// ParseErrorMessage 从上游错误体提取 message 字段。
// 适配三种协议的常见形态：{"error":{"message":"..."}} 与
// {"type":"error","error":{"message":"..."}}（anthropic 多一层 type 包裹）。
// 解析失败或字段为空返回 ""，由调用方兜底填状态码描述。
func ParseErrorMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	// 一次解析同时支持两种 JSON 形态：嵌套 error 块 / 含外层 type 的 error 块。
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	// 兼容 `{"message":"..."}`（部分上游把 message 提到顶层）
	var flat struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &flat); err == nil && flat.Message != "" {
		return flat.Message
	}
	return ""
}

// FormatErrorBody 把任意 map 编为 JSON 字节。marshal 错误被忽略（错误体字段都是
// map[string]any 的基础类型，编码失败为程序员 bug 而非常规分支；与历史
// `body, _ := json.Marshal(...)` 行为等价）。
func FormatErrorBody(body map[string]any) []byte {
	b, _ := json.Marshal(body)
	return b
}
