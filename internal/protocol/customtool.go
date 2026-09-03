package protocol

import "encoding/json"

// customToolSchemaRaw 是 custom 型工具（Responses 私有，输入为原始文本）翻译到
// function 型协议时的合成参数 schema：单 code 文本参数，description 由 ToolDef 自带。
const customToolSchemaRaw = `{"type":"object","properties":{"code":{"type":"string","description":"Tool input as raw text"}},"required":["code"],"additionalProperties":false}`

// CustomToolSchema 返回 custom 型工具的合成参数 schema。
func CustomToolSchema() json.RawMessage {
	return json.RawMessage(customToolSchemaRaw)
}

// UnwrapCustomInput 从 tool_use 输出参数 JSON 中提取 code 原文；
// 模型未按合成 schema 返回时宽容回退为整段参数文本（由客户端沙箱自行报错）。
func UnwrapCustomInput(inputJSON string) string {
	var m struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(inputJSON), &m); err == nil && m.Code != "" {
		return m.Code
	}
	return inputJSON
}
