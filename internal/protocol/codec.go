package protocol

import (
	"io"
	"net/http"
)

// StreamDecoder 把上游 SSE 流解码为 IR 事件序列；结束返回 io.EOF。
type StreamDecoder interface {
	Next() (Event, error)
}

// StreamEncoder 把 IR 事件编码为客户端协议的 SSE 写出。
// Encode 处理除终止帧外的事件；Finish 在流正常收尾时调用（合成终止帧）。
type StreamEncoder interface {
	Encode(ev Event) error
	Finish() error
}

// ErrUnmappedBlock 表示请求中含有该方向无法映射的内容块（如非 data URL 的远程图片）。
type ErrUnmappedBlock struct {
	Detail string
}

func (e *ErrUnmappedBlock) Error() string { return "无法映射的内容块: " + e.Detail }

// Codec 是一种线上协议的六件套编解码器：
// 请求解析/构建、非流式响应解析/构建、流式解码/编码，外加错误体映射。
// 认证头由网关注入，Codec 不处理密钥。
type Codec interface {
	// Name 返回协议名（与 config.Protocol 对应）。
	Name() string

	// ParseRequest 把客户端请求体解析为 IR。
	ParseRequest(body []byte) (Request, error)

	// BuildRequest 把 IR 构建为目标协议请求（路径、非认证默认头、请求体）。
	BuildRequest(req Request) (path string, header http.Header, body []byte, err error)

	// ParseResponse 把上游 2xx 响应体解析为 IR。
	ParseResponse(status int, body []byte) (Response, error)

	// BuildResponse 把 IR 构建为客户端协议的非流式响应体。
	BuildResponse(resp Response) (status int, body []byte)

	// NewStreamDecoder 解码上游 SSE（假定 2xx 响应体）。
	NewStreamDecoder(r io.Reader) StreamDecoder

	// NewStreamEncoder 编码输出给客户端的 SSE。
	NewStreamEncoder(w io.Writer) StreamEncoder

	// ParseError 从上游错误体提取可读错误信息（保留原状态码语义）。
	ParseError(status int, body []byte) string

	// BuildError 按本协议格式构造错误体（发给客户端）。
	BuildError(status int, msg string) []byte
}
