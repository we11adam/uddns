package httpbody

import (
	"io"
	"strings"
	"testing"
)

type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}

func TestLimitBoundsReadsAndForwardsClose(t *testing.T) {
	body := &trackingReadCloser{Reader: strings.NewReader("123456789")}
	limited := Limit(body, 4)

	content, err := io.ReadAll(limited)
	if err != nil {
		t.Fatalf("read limited body: %v", err)
	}
	if string(content) != "1234" {
		t.Fatalf("limited content = %q, want %q", content, "1234")
	}
	if err := limited.Close(); err != nil {
		t.Fatalf("close limited body: %v", err)
	}
	if !body.closed {
		t.Fatal("expected the original body to be closed")
	}
}
