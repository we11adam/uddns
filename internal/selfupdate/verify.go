package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"time"
)

const (
	maxVersionOutput = 64 << 10
	verifyTimeout    = 10 * time.Second
)

type binaryVerifier func(context.Context, string) (string, error)

type binaryPlatformVerifier func(context.Context, string, string, string) error

type binaryInfo struct {
	Version string `json:"version"`
	GOOS    string `json:"goos"`
	GOARCH  string `json:"goarch"`
}

func inspectBinaryVersion(ctx context.Context, path string) (string, error) {
	info, err := inspectBinaryInfo(ctx, path)
	if err != nil {
		return "", err
	}
	if info.Version == "" {
		return "", fmt.Errorf("staged binary did not report a version")
	}
	return info.Version, nil
}

func inspectBinaryPlatform(
	ctx context.Context,
	path string,
	expectedGOOS string,
	expectedGOARCH string,
) error {
	info, err := inspectBinaryInfo(ctx, path)
	if err != nil {
		return err
	}
	if info.GOOS == "" || info.GOARCH == "" {
		return fmt.Errorf("staged binary did not report its runtime target")
	}
	if info.GOOS != expectedGOOS || info.GOARCH != expectedGOARCH {
		return fmt.Errorf(
			"staged binary reported %s/%s, expected %s/%s",
			info.GOOS,
			info.GOARCH,
			expectedGOOS,
			expectedGOARCH,
		)
	}
	return nil
}

func inspectBinaryInfo(ctx context.Context, path string) (binaryInfo, error) {
	verifyContext, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()

	command := exec.CommandContext(verifyContext, path, "version", "--json")
	stdout, err := command.StdoutPipe()
	if err != nil {
		return binaryInfo{}, fmt.Errorf("capture staged binary version: %w", err)
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return binaryInfo{}, fmt.Errorf("execute staged binary: %w", err)
	}

	output, readErr := io.ReadAll(io.LimitReader(stdout, maxVersionOutput+1))
	if readErr != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return binaryInfo{}, fmt.Errorf("read staged binary version: %w", readErr)
	}
	if len(output) > maxVersionOutput {
		_ = command.Process.Kill()
		_ = command.Wait()
		return binaryInfo{}, fmt.Errorf("staged binary version output exceeds the size limit")
	}
	if err := command.Wait(); err != nil {
		return binaryInfo{}, fmt.Errorf("execute staged binary: %w", err)
	}

	var info binaryInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return binaryInfo{}, fmt.Errorf("decode staged binary version: %w", err)
	}
	return info, nil
}

func verifyExpectedVersion(
	ctx context.Context,
	path string,
	expected semanticVersion,
	verifier binaryVerifier,
) error {
	return verifyExpectedVersionValue(
		ctx,
		path,
		expected.canonical(),
		verifier,
	)
}

func verifyExpectedVersionValue(
	ctx context.Context,
	path string,
	expectedValue string,
	verifier binaryVerifier,
) error {
	actualValue, err := verifier(ctx, path)
	if err != nil {
		return err
	}
	if expectedValue == "dev" {
		if actualValue != expectedValue {
			return fmt.Errorf(
				"staged binary reported version %s, expected %s",
				actualValue,
				expectedValue,
			)
		}
		return nil
	}

	expected, err := parseSemanticVersion(expectedValue)
	if err != nil {
		return fmt.Errorf("expected binary version %q is invalid: %w", expectedValue, err)
	}
	actual, err := parseSemanticVersion(actualValue)
	if err != nil {
		return fmt.Errorf("staged binary reported invalid version %q: %w", actualValue, err)
	}
	if actual.canonical() != expected.canonical() {
		return fmt.Errorf(
			"staged binary reported version %s, expected %s",
			actual.canonical(),
			expected.canonical(),
		)
	}
	return nil
}
