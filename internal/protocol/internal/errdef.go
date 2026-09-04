// Package internal 汇集三 codec 共用的错误辅助；映射表按协议各自等价保留，
// 不做统一合并（统一表会改变 529/413 等状态在各协议错误体中的取值）。
package internal

// ErrorTypeOpenAIChat 把 HTTP 状态映射为 openai-chat/openai 风格错误 type。
func ErrorTypeOpenAIChat(status int) string {
	switch {
	case status == 429:
		return "rate_limit_error"
	case status >= 400 && status < 500:
		return "invalid_request_error"
	default:
		return "server_error"
	}
}

// ErrorTypeAnthropic 把 HTTP 状态映射为 anthropic 错误 type。
func ErrorTypeAnthropic(status int) string {
	switch {
	case status == 400 || status == 413 || status == 422:
		return "invalid_request_error"
	case status == 401 || status == 403:
		return "authentication_error"
	case status == 404:
		return "not_found_error"
	case status == 429:
		return "rate_limit_error"
	case status == 529:
		return "overloaded_error"
	default:
		return "api_error"
	}
}

// ErrorCodeResponses 把 HTTP 状态映射为 responses 错误 code。
func ErrorCodeResponses(status int) string {
	switch {
	case status == 429:
		return "rate_limit_exceeded"
	case status >= 400 && status < 500:
		return "invalid_request_error"
	default:
		return "server_error"
	}
}
