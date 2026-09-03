package openairesponses

import (
	"io"
	"net/http"

	"agent_gateway/internal/protocol"
)

// Codec 把包函数装配为 protocol.Codec。
type Codec struct{}

// DefaultCodec 是无状态单例。
var DefaultCodec protocol.Codec = Codec{}

// Name 返回协议名。
func (Codec) Name() string { return "openai-responses" }

// ParseRequest 见包级函数。
func (Codec) ParseRequest(body []byte) (protocol.Request, error) { return ParseRequest(body) }

// BuildRequest 见包级函数。
func (Codec) BuildRequest(req protocol.Request) (string, http.Header, []byte, error) {
	return BuildRequest(req)
}

// ParseResponse 见包级函数。
func (Codec) ParseResponse(status int, body []byte) (protocol.Response, error) {
	return ParseResponse(status, body)
}

// BuildResponse 见包级函数。
func (Codec) BuildResponse(resp protocol.Response) (int, []byte) { return BuildResponse(resp) }

// NewStreamDecoder 见包级函数。
func (Codec) NewStreamDecoder(r io.Reader) protocol.StreamDecoder { return NewStreamDecoder(r) }

// NewStreamEncoder 见包级函数。
func (Codec) NewStreamEncoder(w io.Writer) protocol.StreamEncoder { return NewStreamEncoder(w) }

// ParseError 见包级函数。
func (Codec) ParseError(status int, body []byte) string { return ParseError(status, body) }

// BuildError 见包级函数。
func (Codec) BuildError(status int, msg string) []byte { return BuildError(status, msg) }
