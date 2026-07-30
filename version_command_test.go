package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVersionCommandPrintsText(t *testing.T) {
	oldVersion := version
	version = "1.9.0"
	t.Cleanup(func() {
		version = oldVersion
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runWithIO(
		[]string{"version"},
		&stdout,
		&stderr,
		commandDependencies{},
	)
	if code != 0 {
		t.Fatalf("expected success, got %d: %s", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "uddns 1.9.0 (") {
		t.Fatalf("unexpected version output: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunVersionCommandPrintsJSON(t *testing.T) {
	oldVersion := version
	version = "1.9.0"
	t.Cleanup(func() {
		version = oldVersion
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runWithIO(
		[]string{"version", "--json"},
		&stdout,
		&stderr,
		commandDependencies{},
	)
	if code != 0 {
		t.Fatalf("expected success, got %d: %s", code, stderr.String())
	}

	var info versionInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		t.Fatalf("decode version JSON: %v", err)
	}
	if info.Version != "1.9.0" || info.GOOS == "" || info.GOARCH == "" {
		t.Fatalf("unexpected version info: %#v", info)
	}
}

func TestRunVersionJSONIgnoresLoggingEnvironment(t *testing.T) {
	t.Setenv("UDDNS_LOG_LEVEL", "invalid")
	t.Setenv("UDDNS_LOG_DIR", filepath.Join(t.TempDir(), "logs"))
	t.Setenv("UDDNS_LOG_RETENTION_DAYS", "invalid")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runWithIO(
		[]string{"version", "--json"},
		&stdout,
		&stderr,
		commandDependencies{},
	)
	if code != 0 {
		t.Fatalf("expected success, got %d: %s", code, stderr.String())
	}

	var info versionInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		t.Fatalf("version output is not pure JSON: %q: %v", stdout.String(), err)
	}
	if strings.Count(strings.TrimSpace(stdout.String()), "\n") != 0 {
		t.Fatalf("version output contains extra lines: %q", stdout.String())
	}
	if _, err := os.Stat(os.Getenv("UDDNS_LOG_DIR")); !os.IsNotExist(err) {
		t.Fatalf("version command unexpectedly initialized file logging: %v", err)
	}
}

func TestRunVersionCommandRejectsInvalidArguments(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"version", "--unknown"},
		{"version", "extra"},
	} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if code := runWithIO(args, &stdout, &stderr, commandDependencies{}); code != 2 {
			t.Fatalf("expected usage error for %v, got %d", args, code)
		}
		if stderr.Len() == 0 {
			t.Fatalf("expected usage error output for %v", args)
		}
	}
}

func TestRunVersionCommandHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runVersionCommand([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected help success, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Usage of uddns version") {
		t.Fatalf("unexpected help output: %q", stderr.String())
	}
}

func TestRunVersionCommandReportsWriteErrors(t *testing.T) {
	var stderr bytes.Buffer
	code := runVersionCommand(nil, failingWriter{}, &stderr)
	if code != 1 {
		t.Fatalf("expected operational error, got %d", code)
	}
	if !strings.Contains(stderr.String(), "write failed") {
		t.Fatalf("unexpected error output: %q", stderr.String())
	}

	stderr.Reset()
	code = runVersionCommand([]string{"--json"}, failingWriter{}, &stderr)
	if code != 1 {
		t.Fatalf("expected JSON write error, got %d", code)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
