//go:build !linux

package config

import "os"

func isSystemdCredential(_ string, _ os.FileInfo) bool {
	return false
}
