//go:build linux

package config

import (
	"os"
	"path/filepath"
	"syscall"
)

// isSystemdCredential recognizes the read-only ACL representation used by
// systemd for credentials. systemd owns the file as root:root, sets an ACL for
// the service user, and the ACL mask is exposed through stat(2) as mode 0440.
func isSystemdCredential(path string, info os.FileInfo) bool {
	if !hasSystemdCredentialMode(info.Mode()) {
		return false
	}

	credentialsDir := os.Getenv("CREDENTIALS_DIRECTORY")
	if !isDirectSystemdCredentialPath(path, credentialsDir) {
		return false
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 {
		return false
	}

	// Lstat rejects a symlink at the credential filename. SameFile ties this
	// path check to the already-open descriptor that will be parsed.
	if !isSameRegularFile(path, info) {
		return false
	}

	return isTrustedCredentialDirectory(credentialsDir)
}

func isTrustedCredentialDirectory(path string) bool {
	for dir := path; ; dir = filepath.Dir(dir) {
		info, ok := lstatDirectory(dir)
		if !ok {
			return false
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || stat.Gid != 0 || info.Mode().Perm()&0022 != 0 {
			return false
		}
		if filepath.Dir(dir) == dir {
			return true
		}
	}
}
