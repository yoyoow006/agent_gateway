package protocol

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// SSEEvent 是一帧服务端推送事件；Name 为空表示未写 event: 字段。
type SSEEvent struct {
	Name string
	Data string
}

// SSEReader 按帧读取 SSE 流：以空行分帧，容忍 \r\n，多行 data 以 \n 拼接，
// 忽略注释/id/retry；EOF 时若有未终止的半帧也尽力吐出。
type SSEReader struct {
	br *bufio.Reader
}

// NewSSEReader 构造读取器。
func NewSSEReader(r io.Reader) *SSEReader {
	return &SSEReader{br: bufio.NewReaderSize(r, 64*1024)}
}

// Next 返回下一帧；流结束返回 io.EOF。
func (r *SSEReader) Next() (SSEEvent, error) {
	var name string
	var datas []string
	saw := false
	for {
		line, err := r.br.ReadString('\n')
		stripped := strings.TrimRight(line, "\r\n")
		if stripped != "" {
			saw = true
			switch {
			case strings.HasPrefix(stripped, ":"):
				// 注释，忽略
			case strings.HasPrefix(stripped, "event:"):
				name = strings.TrimSpace(stripped[len("event:"):])
			case strings.HasPrefix(stripped, "data:"):
				datas = append(datas, strings.TrimSpace(stripped[len("data:"):]))
			default:
				// id:/retry: 等忽略
			}
			continue
		}
		// 空行：帧结束
		if saw {
			return SSEEvent{Name: name, Data: strings.Join(datas, "\n")}, nil
		}
		if err == io.EOF {
			return SSEEvent{}, io.EOF
		}
		if err != nil {
			return SSEEvent{}, err
		}
		// 连续空行继续等
	}
}

// SSEWriter 写出 SSE 帧；flush 非空时每帧后立即冲刷（流式低延迟）。
type SSEWriter struct {
	w     io.Writer
	flush func()
}

// NewSSEWriter 构造写入器。
func NewSSEWriter(w io.Writer, flush func()) *SSEWriter {
	return &SSEWriter{w: w, flush: flush}
}

// Send 写一帧带 event 名的事件。
func (w *SSEWriter) Send(name, data string) error {
	if _, err := fmt.Fprintf(w.w, "event: %s\ndata: %s\n\n", name, data); err != nil {
		return err
	}
	w.doFlush()
	return nil
}

// SendData 写一帧无 event 名的事件（chat completions 风格）。
func (w *SSEWriter) SendData(data string) error {
	if _, err := fmt.Fprintf(w.w, "data: %s\n\n", data); err != nil {
		return err
	}
	w.doFlush()
	return nil
}

func (w *SSEWriter) doFlush() {
	if w.flush != nil {
		w.flush()
	}
}
