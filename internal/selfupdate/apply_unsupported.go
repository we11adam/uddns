//go:build !darwin && !freebsd && !linux

package selfupdate

import (
	"context"
	"fmt"
	"runtime"
)

func canApplyOnCurrentPlatform() bool {
	return false
}

func applyCandidate(
	context.Context,
	string,
	string,
	string,
	semanticVersion,
	binaryVerifier,
) error {
	return fmt.Errorf("self-update is not supported on %s", runtime.GOOS)
}

func rollbackExecutable(
	context.Context,
	string,
	binaryVerifier,
) (string, string, error) {
	return "", "", fmt.Errorf("self-update is not supported on %s", runtime.GOOS)
}
