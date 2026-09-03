package cli

import "regexp"

var (
	// tokenLike 匹配 agw-/sk- 风格令牌与长 hex 串
	tokenLike = regexp.MustCompile(`(agw-|sk-)[A-Za-z0-9_-]{8,}`)
	// bearerLike 匹配 Authorization 头值
	bearerLike = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]+`)
	// apiKeyField 匹配 json/toml 里的密钥字段值
	apiKeyField = regexp.MustCompile(`(?i)(api_key|token)\s*[=:]\s*"?[A-Za-z0-9._-]{8,}"?`)
)

// Redact 把日志/错误文本中的密钥与令牌替换为 前6位+***。
func Redact(s string) string {
	s = bearerLike.ReplaceAllStringFunc(s, func(m string) string {
		parts := regexp.MustCompile(`\s+`).Split(m, 2)
		if len(parts) == 2 {
			return parts[0] + " " + maskToken(parts[1])
		}
		return m
	})
	s = tokenLike.ReplaceAllStringFunc(s, maskToken)
	s = apiKeyField.ReplaceAllStringFunc(s, func(m string) string {
		// 保留 "key = " 前缀，值打码
		idx := -1
		for _, sep := range []int{indexAny(m, ":"), indexAny(m, "=")} {
			if sep > idx {
				idx = sep
			}
		}
		if idx < 0 {
			return "***"
		}
		return m[:idx+1] + " ***"
	})
	return s
}

func maskToken(tok string) string {
	if len(tok) <= 6 {
		return "***"
	}
	return tok[:6] + "***"
}

func indexAny(s, chars string) int {
	for i := 0; i < len(s); i++ {
		for j := 0; j < len(chars); j++ {
			if s[i] == chars[j] {
				return i
			}
		}
	}
	return -1
}
