//go:build darwin || freebsd || linux

package selfupdate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectBinaryVersion(t *testing.T) {
	tests := []struct {
		name            string
		script          string
		wantVersion     string
		wantErrContains string
	}{
		{
			name:        "valid JSON",
			script:      `printf '%s\n' '{"version":"1.9.0","goos":"linux","goarch":"amd64"}'`,
			wantVersion: "1.9.0",
		},
		{
			name:            "invalid JSON",
			script:          `printf '%s\n' 'not-json'`,
			wantErrContains: "decode staged binary version",
		},
		{
			name:            "missing version",
			script:          `printf '%s\n' '{}'`,
			wantErrContains: "did not report a version",
		},
		{
			name:            "nonzero exit",
			script:          `exit 7`,
			wantErrContains: "execute staged binary",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeVersionScript(t, test.script)
			version, err := inspectBinaryVersion(context.Background(), path)
			if test.wantErrContains == "" {
				if err != nil {
					t.Fatalf("inspectBinaryVersion returned error: %v", err)
				}
				if version != test.wantVersion {
					t.Fatalf("got version %q, want %q", version, test.wantVersion)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErrContains) {
				t.Fatalf("got error %v, want substring %q", err, test.wantErrContains)
			}
		})
	}
}

func TestInspectBinaryPlatform(t *testing.T) {
	path := writeVersionScript(
		t,
		`printf '%s\n' '{"version":"1.9.0","goos":"darwin","goarch":"amd64"}'`,
	)
	if err := inspectBinaryPlatform(
		context.Background(),
		path,
		"darwin",
		"amd64",
	); err != nil {
		t.Fatalf("inspectBinaryPlatform returned error: %v", err)
	}
	err := inspectBinaryPlatform(
		context.Background(),
		path,
		"darwin",
		"arm64",
	)
	if err == nil || !strings.Contains(err.Error(), "reported darwin/amd64") {
		t.Fatalf("got error %v, want platform mismatch", err)
	}
}

func TestInspectBinaryPlatformRequiresTargetFields(t *testing.T) {
	path := writeVersionScript(
		t,
		`printf '%s\n' '{"version":"1.9.0"}'`,
	)
	err := inspectBinaryPlatform(
		context.Background(),
		path,
		"linux",
		"amd64",
	)
	if err == nil || !strings.Contains(err.Error(), "did not report its runtime target") {
		t.Fatalf("got error %v, want missing target error", err)
	}
}

func TestInspectBinaryVersionBoundsOutput(t *testing.T) {
	path := writeVersionScript(
		t,
		`while :; do printf 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx'; done`,
	)
	_, err := inspectBinaryVersion(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("got error %v, want output size error", err)
	}
}

func writeVersionScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "candidate")
	content := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("write version script: %v", err)
	}
	return path
}
