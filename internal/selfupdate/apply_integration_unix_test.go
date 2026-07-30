//go:build darwin || freebsd || linux

package selfupdate

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"aead.dev/minisign"
)

func TestUpdaterApplyEndToEnd(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "uddns")
	oldBinary := []byte("old binary")
	newBinary := []byte("new binary")
	if err := os.WriteFile(targetPath, oldBinary, 0o755); err != nil {
		t.Fatalf("write installed executable: %v", err)
	}

	targetVersion := mustParseSemanticVersion(t, "1.9.0")
	name, err := assetName(
		defaultRepository,
		targetVersion.artifactVersion(),
		runtime.GOOS,
		runtime.GOARCH,
	)
	if err != nil {
		t.Fatalf("assetName returned error: %v", err)
	}
	archive := buildIntegrationArchive(t, name, newBinary)
	checksum := sha256.Sum256(archive)
	checksums := []byte(fmt.Sprintf("%x  %s\n", checksum, name))
	publicKey, privateKey, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	signature := minisign.Sign(privateKey, checksums)
	releaseBaseURL := "https://github.com/we11adam/uddns/releases/download/v1.9.0/"
	release := releaseMetadata{
		TagName: "v1.9.0",
		Assets: []releaseAsset{
			{Name: name, BrowserDownloadURL: releaseBaseURL + name},
			{Name: "checksums.txt", BrowserDownloadURL: releaseBaseURL + "checksums.txt"},
			{Name: checksumSignatureName, BrowserDownloadURL: releaseBaseURL + checksumSignatureName},
		},
	}
	releaseJSON, err := json.Marshal(release)
	if err != nil {
		t.Fatalf("marshal release metadata: %v", err)
	}
	transport := releaseTestRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body []byte
		switch request.URL.String() {
		case defaultAPIBaseURL + "/repos/we11adam/uddns/releases/latest":
			body = releaseJSON
		case releaseBaseURL + "checksums.txt":
			body = checksums
		case releaseBaseURL + checksumSignatureName:
			body = signature
		case releaseBaseURL + name:
			body = archive
		default:
			return nil, fmt.Errorf("unexpected URL %s", request.URL)
		}
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        make(http.Header),
			Body:          io.NopCloser(bytes.NewReader(body)),
			ContentLength: int64(len(body)),
		}, nil
	})

	updater, err := New(Config{
		CurrentVersion:    "1.8.0",
		ExecutablePath:    targetPath,
		GOOS:              runtime.GOOS,
		GOARCH:            runtime.GOARCH,
		HTTPClient:        &http.Client{Transport: transport},
		TrustedPublicKeys: []string{publicKey.String()},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	updater.verifyBinary = func(_ context.Context, path string) (string, error) {
		content, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		switch {
		case bytes.Equal(content, oldBinary):
			return "1.8.0", nil
		case bytes.Equal(content, newBinary):
			return "1.9.0", nil
		default:
			return "", fmt.Errorf("unknown executable content")
		}
	}
	updater.verifyPlatform = func(
		context.Context,
		string,
		string,
		string,
	) error {
		return nil
	}

	plan, err := updater.Check(context.Background(), "")
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	result, err := updater.Apply(context.Background(), plan, ApplyOptions{})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if !result.Changed || result.ToVersion != "v1.9.0" {
		t.Fatalf("unexpected result: %#v", result)
	}
	assertIntegrationFileContent(t, targetPath, newBinary)
	assertIntegrationFileContent(t, backupPath(targetPath), oldBinary)
}

func buildIntegrationArchive(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "release")
	member := archiveTestMember{
		name:     archiveExecutableName(defaultRepository, runtime.GOOS),
		body:     body,
		mode:     0o755,
		typeflag: 0,
	}
	switch {
	case filepath.Ext(name) == ".zip":
		writeZipTestArchive(t, path, []archiveTestMember{member})
	default:
		writeTarGzipTestArchive(t, path, []archiveTestMember{member})
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	return content
}

func assertIntegrationFileContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s content = %q, want %q", path, got, want)
	}
}
