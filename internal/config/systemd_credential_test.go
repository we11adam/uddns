package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestHasSystemdCredentialMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mode os.FileMode
		want bool
	}{
		{name: "systemd ACL projection", mode: 0440, want: true},
		{name: "ordinary private config", mode: 0600},
		{name: "group writable", mode: 0460},
		{name: "other readable", mode: 0444},
		{name: "setuid", mode: os.ModeSetuid | 0440},
		{name: "directory", mode: os.ModeDir | 0440},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hasSystemdCredentialMode(tt.mode); got != tt.want {
				t.Fatalf("hasSystemdCredentialMode(%v) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}

func TestIsDirectSystemdCredentialPath(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("systemd credential paths are Linux paths")
	}

	tests := []struct {
		name           string
		path           string
		credentialsDir string
		want           bool
	}{
		{
			name:           "direct credential",
			path:           "/run/credentials/uddns.service/uddns.yaml",
			credentialsDir: "/run/credentials/uddns.service",
			want:           true,
		},
		{
			name:           "alternate absolute credential directory",
			path:           "/srv/chroot/run/credentials/uddns.service/uddns.yaml",
			credentialsDir: "/srv/chroot/run/credentials/uddns.service",
			want:           true,
		},
		{
			name:           "relative path",
			path:           "run/credentials/uddns.service/uddns.yaml",
			credentialsDir: "/run/credentials/uddns.service",
		},
		{
			name:           "nested credential",
			path:           "/run/credentials/uddns.service/nested/uddns.yaml",
			credentialsDir: "/run/credentials/uddns.service",
		},
		{
			name:           "traversal",
			path:           "/run/credentials/uddns.service/../other.service/uddns.yaml",
			credentialsDir: "/run/credentials/uddns.service",
		},
		{
			name:           "non-clean directory",
			path:           "/run/credentials/uddns.service/uddns.yaml",
			credentialsDir: "/run/credentials/./uddns.service",
		},
		{
			name:           "root prefix trick",
			path:           "/run/credentials-evil/uddns.service/uddns.yaml",
			credentialsDir: "/run/credentials/uddns.service",
		},
		{
			name:           "sibling directory prefix",
			path:           "/run/credentials/uddns.service-evil/uddns.yaml",
			credentialsDir: "/run/credentials/uddns.service",
		},
		{
			name:           "filesystem root is not a credential directory",
			path:           "/uddns.yaml",
			credentialsDir: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isDirectSystemdCredentialPath(tt.path, tt.credentialsDir); got != tt.want {
				t.Fatalf("isDirectSystemdCredentialPath(%q, %q) = %v, want %v",
					tt.path, tt.credentialsDir, got, tt.want)
			}
		})
	}
}

func TestIsSameRegularFileRejectsSymlinkAndReplacement(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	original := filepath.Join(dir, "original.yaml")
	if err := os.WriteFile(original, []byte("providers: {}\n"), 0600); err != nil {
		t.Fatalf("write original: %v", err)
	}

	file, err := os.Open(original)
	if err != nil {
		t.Fatalf("open original: %v", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatalf("stat original: %v", err)
	}

	if !isSameRegularFile(original, info) {
		t.Fatal("expected the opened regular file to match its path")
	}

	symlink := filepath.Join(dir, "credential.yaml")
	if err := os.Symlink(original, symlink); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if isSameRegularFile(symlink, info) {
		t.Fatal("expected a symlink to be rejected")
	}

	replacement := filepath.Join(dir, "replacement.yaml")
	if err := os.WriteFile(replacement, []byte("providers: {}\n"), 0600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if err := os.Rename(replacement, original); err != nil {
		t.Fatalf("replace original: %v", err)
	}
	if isSameRegularFile(original, info) {
		t.Fatal("expected a path replaced after open to be rejected")
	}
}

func TestLstatDirectoryRejectsSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, ok := lstatDirectory(dir); !ok {
		t.Fatal("expected a real directory to be accepted")
	}

	link := filepath.Join(t.TempDir(), "credentials")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatalf("create directory symlink: %v", err)
	}
	if _, ok := lstatDirectory(link); ok {
		t.Fatal("expected a directory symlink to be rejected")
	}
}
