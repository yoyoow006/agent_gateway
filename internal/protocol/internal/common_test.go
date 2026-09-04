package internal

import "testing"

func TestOrDefault(t *testing.T) {
	if got := OrDefault("", "fallback"); got != "fallback" {
		t.Errorf("空应返回 fallback，实际 %q", got)
	}
	if got := OrDefault("hello", "fallback"); got != "hello" {
		t.Errorf("非空应返回原值，实际 %q", got)
	}
}

func TestParseErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"anthropic 嵌套", `{"type":"error","error":{"type":"api_error","message":"boom"}}`, "boom"},
		{"chat 简洁", `{"error":{"message":"upstream error","type":"server_error"}}`, "upstream error"},
		{"responses 简洁", `{"error":{"code":"server_error","message":"x"}}`, "x"},
		{"顶层 message", `{"message":"flat"}`, "flat"},
		{"空 body", ``, ""},
		{"非 JSON", `not json`, ""},
		{"message 为空", `{"error":{"message":""}}`, ""},
		{"error 缺失", `{"foo":"bar"}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParseErrorMessage([]byte(c.body)); got != c.want {
				t.Errorf("want %q got %q", c.want, got)
			}
		})
	}
}

func TestFormatErrorBody(t *testing.T) {
	body := FormatErrorBody(map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": "missing field",
		},
	})
	got := string(body)
	want := `{"error":{"message":"missing field","type":"invalid_request_error"},"type":"error"}`
	if got != want {
		t.Errorf("\nwant %s\n got %s", want, got)
	}
}

func TestErrorTypeOpenAIChat(t *testing.T) {
	cases := map[int]string{429: "rate_limit_error", 400: "invalid_request_error", 404: "invalid_request_error", 500: "server_error", 529: "server_error", 200: "server_error"}
	for s, want := range cases {
		if got := ErrorTypeOpenAIChat(s); got != want {
			t.Errorf("chat %d = %q, want %q", s, got, want)
		}
	}
}

func TestErrorTypeAnthropic(t *testing.T) {
	cases := map[int]string{400: "invalid_request_error", 413: "invalid_request_error", 422: "invalid_request_error", 401: "authentication_error", 403: "authentication_error", 404: "not_found_error", 429: "rate_limit_error", 529: "overloaded_error", 500: "api_error", 502: "api_error", 302: "api_error"}
	for s, want := range cases {
		if got := ErrorTypeAnthropic(s); got != want {
			t.Errorf("anthropic %d = %q, want %q", s, got, want)
		}
	}
}

func TestErrorCodeResponses(t *testing.T) {
	cases := map[int]string{429: "rate_limit_exceeded", 400: "invalid_request_error", 500: "server_error", 529: "server_error"}
	for s, want := range cases {
		if got := ErrorCodeResponses(s); got != want {
			t.Errorf("responses %d = %q, want %q", s, got, want)
		}
	}
}
