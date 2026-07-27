package selfupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChecksumForAssetUsesExactFilename(t *testing.T) {
	t.Parallel()

	targetName := "uddns_1.9.0_linux_amd64.tar.gz"
	targetHash := sha256.Sum256([]byte("target"))
	otherHash := sha256.Sum256([]byte("other"))
	content := fmt.Sprintf(
		"%x  %s.sig\n%x  %s\n%x  other.zip\n",
		otherHash,
		targetName,
		targetHash,
		targetName,
		otherHash,
	)
	path := filepath.Join(t.TempDir(), "checksums.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write checksums fixture: %v", err)
	}

	got, err := checksumForAsset(path, targetName)
	if err != nil {
		t.Fatalf("checksumForAsset returned error: %v", err)
	}
	if !bytes.Equal(got, targetHash[:]) {
		t.Fatalf("checksumForAsset = %x, want %x", got, targetHash)
	}
}

func TestChecksumForAssetRejectsMissingEntry(t *testing.T) {
	t.Parallel()

	hash := sha256.Sum256([]byte("other"))
	path := filepath.Join(t.TempDir(), "checksums.txt")
	writeDownloadTestFile(t, path, fmt.Sprintf("%x  other.tar.gz\n", hash))

	if _, err := checksumForAsset(path, "release.tar.gz"); err == nil {
		t.Fatal("checksumForAsset succeeded, want missing-entry error")
	}
}

func TestChecksumForAssetRejectsDuplicateEntry(t *testing.T) {
	t.Parallel()

	const assetName = "release.tar.gz"
	first := sha256.Sum256([]byte("first"))
	second := sha256.Sum256([]byte("second"))
	path := filepath.Join(t.TempDir(), "checksums.txt")
	writeDownloadTestFile(
		t,
		path,
		fmt.Sprintf("%x  %s\n%x  %s\n", first, assetName, second, assetName),
	)

	if _, err := checksumForAsset(path, assetName); err == nil {
		t.Fatal("checksumForAsset succeeded, want duplicate-entry error")
	}
}

func TestChecksumForAssetRejectsMalformedContent(t *testing.T) {
	t.Parallel()

	validHash := sha256.Sum256([]byte("content"))
	tests := []struct {
		name    string
		content string
	}{
		{name: "missing filename", content: hex.EncodeToString(validHash[:]) + "\n"},
		{name: "extra field", content: fmt.Sprintf("%x  release.tar.gz extra\n", validHash)},
		{name: "non hexadecimal hash", content: strings.Repeat("z", 64) + "  release.tar.gz\n"},
		{name: "short hash", content: strings.Repeat("0", 62) + "  release.tar.gz\n"},
		{
			name: "malformed unrelated entry",
			content: fmt.Sprintf(
				"%x  release.tar.gz\nnot-a-hash  unrelated.zip\n",
				validHash,
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "checksums.txt")
			writeDownloadTestFile(t, path, tt.content)
			if _, err := checksumForAsset(path, "release.tar.gz"); err == nil {
				t.Fatal("checksumForAsset succeeded, want malformed-content error")
			}
		})
	}
}

func TestVerifyFileChecksum(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "release.tar.gz")
	content := []byte("release archive")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write release fixture: %v", err)
	}
	checksum := sha256.Sum256(content)

	if err := verifyFileChecksum(path, checksum[:]); err != nil {
		t.Fatalf("verifyFileChecksum returned error for matching checksum: %v", err)
	}

	wrong := sha256.Sum256([]byte("different archive"))
	if err := verifyFileChecksum(path, wrong[:]); err == nil {
		t.Fatal("verifyFileChecksum succeeded for mismatched checksum")
	}
}

func TestDownloadFileWritesBoundedResponse(t *testing.T) {
	t.Parallel()

	content := []byte("candidate")
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		if got, want := request.Header.Get("Accept"), "application/octet-stream"; got != want {
			t.Errorf("Accept header = %q, want %q", got, want)
		}
		if got, want := request.Header.Get("User-Agent"), "uddns/v1.8.0"; got != want {
			t.Errorf("User-Agent header = %q, want %q", got, want)
		}
		_, _ = writer.Write(content)
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "download")
	updater := downloadTestUpdater(server.Client())
	if err := updater.downloadFile(
		context.Background(),
		server.URL+"/asset",
		destination,
		int64(len(content)),
	); err != nil {
		t.Fatalf("downloadFile returned error: %v", err)
	}

	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("downloaded content = %q, want %q", got, content)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatalf("stat downloaded file: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Errorf("downloaded mode = %o, want %o", got, want)
	}
}

func TestDownloadFileRejectsHTTPStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		http.Error(writer, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "download")
	err := downloadTestUpdater(server.Client()).downloadFile(
		context.Background(),
		server.URL+"/missing",
		destination,
		32,
	)
	if err == nil {
		t.Fatal("downloadFile succeeded, want status error")
	}
	assertDownloadTestFileAbsent(t, destination)
}

func TestDownloadFileRejectsOversizedContentLength(t *testing.T) {
	t.Parallel()

	const maximumSize = int64(8)
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Content-Length", "9")
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, "123456789")
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "download")
	err := downloadTestUpdater(server.Client()).downloadFile(
		context.Background(),
		server.URL+"/oversized",
		destination,
		maximumSize,
	)
	if err == nil {
		t.Fatal("downloadFile succeeded, want Content-Length error")
	}
	assertDownloadTestFileAbsent(t, destination)
}

func TestDownloadFileRejectsStreamingByteBeyondLimit(t *testing.T) {
	t.Parallel()

	const maximumSize = int64(8)
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.WriteHeader(http.StatusOK)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = writer.Write(bytes.Repeat([]byte{'x'}, int(maximumSize+1)))
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "download")
	err := downloadTestUpdater(server.Client()).downloadFile(
		context.Background(),
		server.URL+"/stream",
		destination,
		maximumSize,
	)
	if err == nil {
		t.Fatal("downloadFile succeeded, want streaming size-limit error")
	}
	assertDownloadTestFileAbsent(t, destination)
}

func downloadTestUpdater(client *http.Client) *Updater {
	return &Updater{
		currentVersion: "v1.8.0",
		httpClient:     client,
		testEndpoint:   true,
	}
}

func writeDownloadTestFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write download fixture: %v", err)
	}
}

func assertDownloadTestFileAbsent(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("failed download left destination behind: %v", err)
	}
}
