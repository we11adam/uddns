package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxArchiveSize  = 128 << 20
	maxChecksumSize = 1 << 20
)

func (u *Updater) downloadCandidate(
	ctx context.Context,
	plan Plan,
	stagingDirectory string,
) (string, error) {
	checksumPath := filepath.Join(stagingDirectory, "checksums.txt")
	if err := u.downloadFile(ctx, plan.checksumURL, checksumPath, maxChecksumSize); err != nil {
		return "", fmt.Errorf("download checksums: %w", err)
	}

	expectedChecksum, err := checksumForAsset(checksumPath, plan.AssetName)
	if err != nil {
		return "", err
	}

	archivePath := filepath.Join(stagingDirectory, "release.archive")
	if err := u.downloadFile(ctx, plan.assetURL, archivePath, maxArchiveSize); err != nil {
		return "", fmt.Errorf("download release archive: %w", err)
	}
	if err := verifyFileChecksum(archivePath, expectedChecksum); err != nil {
		return "", fmt.Errorf("verify %s: %w", plan.AssetName, err)
	}

	candidatePath := filepath.Join(stagingDirectory, "uddns.new")
	if err := extractExecutable(
		archivePath,
		plan.AssetName,
		archiveExecutableName(defaultRepository, u.goos),
		candidatePath,
	); err != nil {
		return "", fmt.Errorf("extract %s: %w", plan.AssetName, err)
	}
	return candidatePath, nil
}

func (u *Updater) downloadFile(
	ctx context.Context,
	rawURL string,
	destination string,
	maximumSize int64,
) error {
	if err := u.validateDownloadURL(rawURL); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "uddns/"+u.currentVersion)

	response, err := u.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return fmt.Errorf("server returned %s", response.Status)
	}
	if response.ContentLength > maximumSize {
		return fmt.Errorf("response exceeds the %d-byte size limit", maximumSize)
	}

	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(destination)
		}
	}()

	written, err := io.Copy(file, io.LimitReader(response.Body, maximumSize+1))
	if err != nil {
		return err
	}
	if written > maximumSize {
		return fmt.Errorf("response exceeds the %d-byte size limit", maximumSize)
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	success = true
	return nil
}

func checksumForAsset(checksumPath, assetName string) ([]byte, error) {
	content, err := os.ReadFile(checksumPath)
	if err != nil {
		return nil, fmt.Errorf("read checksums: %w", err)
	}
	if len(content) > maxChecksumSize {
		return nil, fmt.Errorf("checksums exceed the size limit")
	}

	var found []byte
	for lineNumber, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("checksums line %d is malformed", lineNumber+1)
		}
		hash, err := hex.DecodeString(fields[0])
		if err != nil || len(hash) != sha256.Size {
			return nil, fmt.Errorf("checksums line %d has an invalid SHA-256 value", lineNumber+1)
		}
		if fields[1] != assetName {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("checksums contain duplicate entries for %s", assetName)
		}
		found = hash
	}
	if found == nil {
		return nil, fmt.Errorf("checksums do not contain %s", assetName)
	}
	return found, nil
}

func verifyFileChecksum(path string, expected []byte) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	actual := hash.Sum(nil)
	if !equalBytes(actual, expected) {
		return fmt.Errorf("checksum mismatch")
	}
	return nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var difference byte
	for i := range a {
		difference |= a[i] ^ b[i]
	}
	return difference == 0
}
