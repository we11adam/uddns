package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

const releaseTestAssetName = "uddns_1.9.0_linux_amd64.tar.gz"

type releaseTestRoundTripFunc func(*http.Request) (*http.Response, error)

func (f releaseTestRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestFetchReleaseEndpointAndHeaders(t *testing.T) {
	tests := []struct {
		name             string
		requestedVersion string
		wantPath         string
	}{
		{
			name:     "latest",
			wantPath: "/repos/we11adam/uddns/releases/latest",
		},
		{
			name:             "latest keyword",
			requestedVersion: "latest",
			wantPath:         "/repos/we11adam/uddns/releases/latest",
		},
		{
			name:             "specified tag",
			requestedVersion: "1.9.0",
			wantPath:         "/repos/we11adam/uddns/releases/tags/v1.9.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotRequest *http.Request
			body := releaseTestMarshalMetadata(t, releaseTestMetadata())
			updater := releaseTestUpdater(t, "1.8.0", releaseTestRoundTripFunc(
				func(request *http.Request) (*http.Response, error) {
					gotRequest = request.Clone(request.Context())
					return releaseTestResponse(http.StatusOK, body), nil
				},
			))

			if _, err := updater.Check(context.Background(), tt.requestedVersion); err != nil {
				t.Fatalf("Check returned error: %v", err)
			}
			if gotRequest == nil {
				t.Fatal("Check did not issue an HTTP request")
			}
			if gotRequest.Method != http.MethodGet {
				t.Errorf("request method = %q, want GET", gotRequest.Method)
			}
			if gotRequest.URL.Scheme != "https" || gotRequest.URL.Host != "api.github.com" {
				t.Errorf("request endpoint = %q, want official GitHub API", gotRequest.URL)
			}
			if gotRequest.URL.EscapedPath() != tt.wantPath {
				t.Errorf(
					"request path = %q, want %q",
					gotRequest.URL.EscapedPath(),
					tt.wantPath,
				)
			}
			if gotRequest.URL.RawQuery != "" {
				t.Errorf("request query = %q, want empty", gotRequest.URL.RawQuery)
			}
			if got := gotRequest.Header.Get("Accept"); got != "application/vnd.github+json" {
				t.Errorf("Accept header = %q", got)
			}
			if got := gotRequest.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
				t.Errorf("X-GitHub-Api-Version header = %q", got)
			}
			if got := gotRequest.Header.Get("User-Agent"); got != "uddns/1.8.0" {
				t.Errorf("User-Agent header = %q, want %q", got, "uddns/1.8.0")
			}
		})
	}
}

func TestFetchReleaseRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name            string
		statusCode      int
		body            string
		contentLength   int64
		wantErrContains string
	}{
		{
			name:            "non OK status",
			statusCode:      http.StatusServiceUnavailable,
			body:            `{"message":"temporarily unavailable"}`,
			contentLength:   -2,
			wantErrContains: "503 Service Unavailable",
		},
		{
			name:            "malformed JSON",
			statusCode:      http.StatusOK,
			body:            `{"tag_name":`,
			contentLength:   -2,
			wantErrContains: "decode release metadata",
		},
		{
			name:            "multiple JSON values",
			statusCode:      http.StatusOK,
			body:            `{"tag_name":"v1.9.0"} {}`,
			contentLength:   -2,
			wantErrContains: "multiple JSON values",
		},
		{
			name:            "missing tag",
			statusCode:      http.StatusOK,
			body:            `{}`,
			contentLength:   -2,
			wantErrContains: "missing tag_name",
		},
		{
			name:            "declared oversized metadata",
			statusCode:      http.StatusOK,
			body:            `{"tag_name":"v1.9.0"}`,
			contentLength:   maxReleaseMetadataSize + 1,
			wantErrContains: "size limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updater := releaseTestUpdater(t, "1.8.0", releaseTestRoundTripFunc(
				func(*http.Request) (*http.Response, error) {
					response := releaseTestResponse(tt.statusCode, tt.body)
					if tt.contentLength != -2 {
						response.ContentLength = tt.contentLength
					}
					return response, nil
				},
			))

			_, err := updater.fetchRelease(context.Background(), nil)
			if err == nil {
				t.Fatal("fetchRelease returned nil error")
			}
			if !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Fatalf("fetchRelease error = %q, want substring %q", err, tt.wantErrContains)
			}
		})
	}
}

func TestFetchReleaseRejectsUndeclaredOversizedMetadata(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "oversized JSON field",
			body: `{"tag_name":"v1.9.0","padding":"` +
				strings.Repeat("x", maxReleaseMetadataSize) + `"}`,
		},
		{
			name: "oversized trailing whitespace",
			body: `{"tag_name":"v1.9.0"}` +
				strings.Repeat(" ", maxReleaseMetadataSize),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updater := releaseTestUpdater(t, "1.8.0", releaseTestRoundTripFunc(
				func(*http.Request) (*http.Response, error) {
					response := releaseTestResponse(http.StatusOK, tt.body)
					response.ContentLength = -1
					return response, nil
				},
			))

			if _, err := updater.fetchRelease(context.Background(), nil); err == nil {
				t.Fatal("fetchRelease accepted metadata larger than maxReleaseMetadataSize")
			}
		})
	}
}

func TestValidateGitHubURLAllowlist(t *testing.T) {
	allowed := []string{
		"https://api.github.com/repos/we11adam/uddns/releases/latest",
		"https://github.com/we11adam/uddns/releases/download/v1.9.0/" + releaseTestAssetName,
		"https://objects.githubusercontent.com/github-production-release-asset/file",
		"https://release-assets.githubusercontent.com/github-production-release-asset/file?sp=r",
		"https://GITHUB.COM/we11adam/uddns/releases/download/v1.9.0/checksums.txt",
	}
	for _, rawURL := range allowed {
		t.Run("allow "+rawURL, func(t *testing.T) {
			parsed, err := url.Parse(rawURL)
			if err != nil {
				t.Fatalf("url.Parse returned error: %v", err)
			}
			if err := validateGitHubURL(parsed); err != nil {
				t.Fatalf("validateGitHubURL(%q) returned error: %v", rawURL, err)
			}
		})
	}

	rejected := []struct {
		name            string
		rawURL          string
		wantErrContains string
	}{
		{
			name:            "plain HTTP",
			rawURL:          "http://github.com/we11adam/uddns/releases/download/v1.9.0/file",
			wantErrContains: "HTTPS",
		},
		{
			name:            "untrusted host",
			rawURL:          "https://example.com/file",
			wantErrContains: "not trusted",
		},
		{
			name:            "lookalike suffix",
			rawURL:          "https://github.com.example.com/file",
			wantErrContains: "not trusted",
		},
		{
			name:            "untrusted subdomain",
			rawURL:          "https://uploads.github.com/file",
			wantErrContains: "not trusted",
		},
		{
			name:            "userinfo",
			rawURL:          "https://user@github.com/file",
			wantErrContains: "user information",
		},
		{
			name:            "nonstandard port",
			rawURL:          "https://github.com:444/file",
			wantErrContains: "default HTTPS port",
		},
		{
			name:            "parent path",
			rawURL:          "https://github.com/we11adam/../file",
			wantErrContains: "non-canonical path",
		},
		{
			name:            "duplicate separator",
			rawURL:          "https://github.com/we11adam//file",
			wantErrContains: "non-canonical path",
		},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := url.Parse(tt.rawURL)
			if err != nil {
				t.Fatalf("url.Parse returned error: %v", err)
			}
			err = validateGitHubURL(parsed)
			if err == nil {
				t.Fatalf("validateGitHubURL(%q) returned nil error", tt.rawURL)
			}
			if !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Fatalf("error = %q, want substring %q", err, tt.wantErrContains)
			}
		})
	}
}

func TestUpdaterCheckUsesProductionDownloadURLAllowlist(t *testing.T) {
	tests := []struct {
		name            string
		mutate          func(*releaseMetadata)
		wantErrContains string
	}{
		{
			name: "untrusted release asset URL",
			mutate: func(release *releaseMetadata) {
				release.Assets[0].BrowserDownloadURL = "https://example.com/uddns.tar.gz"
			},
			wantErrContains: "invalid release asset URL",
		},
		{
			name: "insecure checksum URL",
			mutate: func(release *releaseMetadata) {
				release.Assets[1].BrowserDownloadURL = "http://github.com/checksums.txt"
			},
			wantErrContains: "invalid checksum URL",
		},
		{
			name: "untrusted checksum signature URL",
			mutate: func(release *releaseMetadata) {
				release.Assets[2].BrowserDownloadURL = "https://example.com/checksums.txt.minisig"
			},
			wantErrContains: "invalid checksum signature URL",
		},
		{
			name: "different repository",
			mutate: func(release *releaseMetadata) {
				release.Assets[0].BrowserDownloadURL =
					"https://github.com/other/uddns/releases/download/v1.9.0/" +
						releaseTestAssetName
			},
			wantErrContains: "selected repository release",
		},
		{
			name: "different release tag",
			mutate: func(release *releaseMetadata) {
				release.Assets[0].BrowserDownloadURL =
					"https://github.com/we11adam/uddns/releases/download/v1.8.0/" +
						releaseTestAssetName
			},
			wantErrContains: "selected repository release",
		},
		{
			name: "asset URL query",
			mutate: func(release *releaseMetadata) {
				release.Assets[0].BrowserDownloadURL += "?download=1"
			},
			wantErrContains: "selected repository release",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := releaseTestMetadata()
			tt.mutate(&release)
			body := releaseTestMarshalMetadata(t, release)
			updater := releaseTestUpdater(t, "1.8.0", releaseTestRoundTripFunc(
				func(*http.Request) (*http.Response, error) {
					return releaseTestResponse(http.StatusOK, body), nil
				},
			))

			_, err := updater.Check(context.Background(), "")
			if err == nil {
				t.Fatal("Check returned nil error")
			}
			if !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Fatalf("Check error = %q, want substring %q", err, tt.wantErrContains)
			}
		})
	}
}

func releaseTestMetadata() releaseMetadata {
	const releaseBaseURL = "https://github.com/we11adam/uddns/releases/download/v1.9.0/"
	return releaseMetadata{
		TagName: "v1.9.0",
		Assets: []releaseAsset{
			{
				Name:               releaseTestAssetName,
				BrowserDownloadURL: releaseBaseURL + releaseTestAssetName,
			},
			{
				Name:               "checksums.txt",
				BrowserDownloadURL: releaseBaseURL + "checksums.txt",
			},
			{
				Name:               checksumSignatureName,
				BrowserDownloadURL: releaseBaseURL + checksumSignatureName,
			},
		},
	}
}

func releaseTestUpdater(
	t *testing.T,
	currentVersion string,
	transport http.RoundTripper,
) *Updater {
	t.Helper()

	updater, err := New(Config{
		CurrentVersion: currentVersion,
		ExecutablePath: filepath.Join(t.TempDir(), "uddns"),
		GOOS:           "linux",
		GOARCH:         "amd64",
		HTTPClient: &http.Client{
			Transport: transport,
		},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	return updater
}

func releaseTestMarshalMetadata(t *testing.T, release releaseMetadata) string {
	t.Helper()

	data, err := json.Marshal(release)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	return string(data)
}

func releaseTestResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode:    statusCode,
		Status:        fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}
