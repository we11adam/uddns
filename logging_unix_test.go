//go:build unix

package main

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestCalendarRotatingWriterRejectsCurrentLogFIFO(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "uddns-2026-05-21.log")
	if err := syscall.Mkfifo(logPath, 0600); err != nil {
		t.Skipf("create log FIFO: %v", err)
	}

	_, err := newCalendarRotatingWriterWithClock(dir, logFilePrefix, 2, func() time.Time {
		return time.Date(2026, 5, 21, 10, 0, 0, 0, time.Local)
	})
	if err == nil {
		t.Fatal("expected current log FIFO to be rejected")
	}
}
