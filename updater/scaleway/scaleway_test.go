package scaleway

import (
	"testing"
)

func TestNewRejectsNilConfig(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("expected nil config to be rejected")
	}
}
