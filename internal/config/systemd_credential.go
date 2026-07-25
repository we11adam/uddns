package config

import (
	"os"
	"path/filepath"
)

func hasSystemdCredentialMode(mode os.FileMode) bool {
	return mode == 0440
}

func isDirectSystemdCredentialPath(path, credentialsDir string) bool {
	if path == "" || credentialsDir == "" {
		return false
	}
	if !filepath.IsAbs(path) || !filepath.IsAbs(credentialsDir) {
		return false
	}
	if filepath.Clean(path) != path ||
		filepath.Clean(credentialsDir) != credentialsDir {
		return false
	}
	if filepath.Dir(credentialsDir) == credentialsDir {
		return false
	}
	return filepath.Dir(path) == credentialsDir
}

func isSameRegularFile(path string, openedInfo os.FileInfo) bool {
	pathInfo, err := os.Lstat(path)
	return err == nil &&
		pathInfo.Mode().IsRegular() &&
		os.SameFile(openedInfo, pathInfo)
}

func lstatDirectory(path string) (os.FileInfo, bool) {
	info, err := os.Lstat(path)
	return info, err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}
