package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCalendarRotatingWriterRotatesByDateAndRemovesExpiredLogs(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.Local)

	writeTestFile(t, filepath.Join(dir, "uddns-2026-05-19.log"), "old\n")
	writeTestFile(t, filepath.Join(dir, "uddns-2026-05-20.log"), "keep\n")
	writeTestFile(t, filepath.Join(dir, "other-2026-05-19.log"), "ignore\n")

	writer, err := newCalendarRotatingWriterWithClock(dir, logFilePrefix, 2, func() time.Time {
		return now
	})
	if err != nil {
		t.Fatalf("newCalendarRotatingWriterWithClock returned error: %v", err)
	}
	defer writer.Close()

	if _, err := writer.Write([]byte("today\n")); err != nil {
		t.Fatalf("write current log: %v", err)
	}

	assertMissing(t, filepath.Join(dir, "uddns-2026-05-19.log"))
	assertExists(t, filepath.Join(dir, "uddns-2026-05-20.log"))
	assertExists(t, filepath.Join(dir, "uddns-2026-05-21.log"))
	assertExists(t, filepath.Join(dir, "other-2026-05-19.log"))

	now = time.Date(2026, 5, 22, 1, 0, 0, 0, time.Local)
	if _, err := writer.Write([]byte("tomorrow\n")); err != nil {
		t.Fatalf("write rotated log: %v", err)
	}

	assertMissing(t, filepath.Join(dir, "uddns-2026-05-20.log"))
	assertExists(t, filepath.Join(dir, "uddns-2026-05-21.log"))
	assertExists(t, filepath.Join(dir, "uddns-2026-05-22.log"))

	content, err := os.ReadFile(filepath.Join(dir, "uddns-2026-05-22.log"))
	if err != nil {
		t.Fatalf("read rotated log: %v", err)
	}
	if !bytes.Contains(content, []byte("tomorrow\n")) {
		t.Fatalf("expected rotated log to contain written content, got %q", string(content))
	}
}

func TestCalendarRotatingWriterCreatesPrivateDirectoryAndLogFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.Local)

	writer, err := newCalendarRotatingWriterWithClock(dir, logFilePrefix, 2, func() time.Time {
		return now
	})
	if err != nil {
		t.Fatalf("newCalendarRotatingWriterWithClock returned error: %v", err)
	}
	defer writer.Close()

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat log directory: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != logDirMode {
		t.Fatalf("expected log directory permissions %04o, got %04o", logDirMode, got)
	}

	logPath := filepath.Join(dir, "uddns-2026-05-21.log")
	fileInfo, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("stat log file: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != logFileMode {
		t.Fatalf("expected log file permissions %04o, got %04o", logFileMode, got)
	}
}

func TestCalendarRotatingWriterRejectsCurrentLogSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target.log")
	writeTestFile(t, target, "unchanged\n")
	logPath := filepath.Join(dir, "uddns-2026-05-21.log")
	if err := os.Symlink(target, logPath); err != nil {
		t.Skipf("create log symlink: %v", err)
	}

	_, err := newCalendarRotatingWriterWithClock(dir, logFilePrefix, 2, func() time.Time {
		return time.Date(2026, 5, 21, 10, 0, 0, 0, time.Local)
	})
	if err == nil {
		t.Fatal("expected current log symlink to be rejected")
	}

	content, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("read symlink target: %v", readErr)
	}
	if string(content) != "unchanged\n" {
		t.Fatalf("expected symlink target to remain unchanged, got %q", content)
	}
}

func TestCalendarRotatingWriterCleanupRemainsInOpenedRoot(t *testing.T) {
	baseDir := t.TempDir()
	dir := filepath.Join(baseDir, "logs")
	if err := os.Mkdir(dir, logDirMode); err != nil {
		t.Fatalf("create log directory: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "uddns-2026-05-20.log"), "original\n")

	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.Local)
	writer, err := newCalendarRotatingWriterWithClock(dir, logFilePrefix, 2, func() time.Time {
		return now
	})
	if err != nil {
		t.Fatalf("newCalendarRotatingWriterWithClock returned error: %v", err)
	}
	defer writer.Close()

	movedDir := filepath.Join(baseDir, "moved-logs")
	if err := os.Rename(dir, movedDir); err != nil {
		t.Skipf("rename open log directory: %v", err)
	}
	if err := os.Mkdir(dir, logDirMode); err != nil {
		t.Fatalf("create replacement log directory: %v", err)
	}
	replacementLog := filepath.Join(dir, "uddns-2026-05-20.log")
	writeTestFile(t, replacementLog, "replacement\n")

	now = time.Date(2026, 5, 22, 1, 0, 0, 0, time.Local)
	if _, err := writer.Write([]byte("tomorrow\n")); err != nil {
		t.Fatalf("write rotated log: %v", err)
	}

	assertMissing(t, filepath.Join(movedDir, "uddns-2026-05-20.log"))
	assertExists(t, replacementLog)
	assertExists(t, filepath.Join(movedDir, "uddns-2026-05-22.log"))
}

func TestParseRotatedLogDate(t *testing.T) {
	date, ok := parseRotatedLogDate("uddns-2026-05-21.log", logFilePrefix)
	if !ok {
		t.Fatal("expected valid rotated log name")
	}
	if date.Format(logDateLayout) != "2026-05-21" {
		t.Fatalf("expected date 2026-05-21, got %s", date.Format(logDateLayout))
	}

	invalidNames := []string{
		"uddns.log",
		"uddns-2026-05-21.txt",
		"other-2026-05-21.log",
		"uddns-not-a-date.log",
	}
	for _, name := range invalidNames {
		t.Run(name, func(t *testing.T) {
			if _, ok := parseRotatedLogDate(name, logFilePrefix); ok {
				t.Fatalf("expected %q to be ignored", name)
			}
		})
	}
}

func TestLogProcessAttrsIncludesVersionAndPID(t *testing.T) {
	oldVersion := version
	version = "v-test"
	t.Cleanup(func() {
		version = oldVersion
	})

	attrs := logProcessAttrs()
	values := map[string]any{}
	for i := 0; i < len(attrs); i += 2 {
		key, ok := attrs[i].(string)
		if !ok {
			t.Fatalf("expected attr key at index %d to be string, got %T", i, attrs[i])
		}
		values[key] = attrs[i+1]
	}

	if values["version"] != "v-test" {
		t.Fatalf("expected version attr v-test, got %#v", values["version"])
	}
	if values["pid"] != os.Getpid() {
		t.Fatalf("expected pid attr %d, got %#v", os.Getpid(), values["pid"])
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write test file %s: %v", path, err)
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be missing, stat err=%v", path, err)
	}
}
