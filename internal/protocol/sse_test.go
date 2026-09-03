package protocol

import (
	"io"
	"strings"
	"testing"
)

func TestSSEReaderFrames(t *testing.T) {
	in := "event: message_start\ndata: {\"a\":1}\n\ndata: line1\ndata: line2\n\n: comment\rid: 7\nretry: 100\nevent: ping\ndata: {}\r\n\r\n"
	r := NewSSEReader(strings.NewReader(in))

	ev, err := r.Next()
	if err != nil || ev.Name != "message_start" || ev.Data != `{"a":1}` {
		t.Fatalf("1st = %+v,%v", ev, err)
	}
	ev, err = r.Next()
	if err != nil || ev.Name != "" || ev.Data != "line1\nline2" {
		t.Fatalf("2nd = %+v,%v", ev, err)
	}
	ev, err = r.Next()
	if err != nil || ev.Name != "ping" || ev.Data != "{}" {
		t.Fatalf("3rd = %+v,%v", ev, err)
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}
}

func TestSSEReaderPartialAtEOF(t *testing.T) {
	r := NewSSEReader(strings.NewReader("event: x\ndata: half"))
	ev, err := r.Next()
	if err != nil || ev.Name != "x" || ev.Data != "half" {
		t.Fatalf("partial = %+v,%v", ev, err)
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatalf("want EOF after partial, got %v", err)
	}
}

func TestSSEReaderDoneSentinel(t *testing.T) {
	r := NewSSEReader(strings.NewReader("data: [DONE]\n\n"))
	ev, err := r.Next()
	if err != nil || ev.Data != "[DONE]" {
		t.Fatalf("done = %+v,%v", ev, err)
	}
}

func TestSSEWriter(t *testing.T) {
	var sb strings.Builder
	w := NewSSEWriter(&sb, nil)
	if err := w.Send("message_start", `{"x":1}`); err != nil {
		t.Fatal(err)
	}
	if err := w.SendData(`{"y":2}`); err != nil {
		t.Fatal(err)
	}
	want := "event: message_start\ndata: {\"x\":1}\n\ndata: {\"y\":2}\n\n"
	if sb.String() != want {
		t.Fatalf("out = %q, want %q", sb.String(), want)
	}
}
