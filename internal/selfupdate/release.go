package selfupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	maxReleaseMetadataSize = 2 << 20
	maxRedirects           = 5
	requestTimeout         = 5 * time.Minute
)

type releaseMetadata struct {
	TagName    string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func hardenedHTTPClient(client *http.Client, testEndpoint bool) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	hardened := *client
	if hardened.Timeout <= 0 || hardened.Timeout > requestTimeout {
		hardened.Timeout = requestTimeout
	}
	originalRedirectCheck := hardened.CheckRedirect
	hardened.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return errors.New("too many redirects")
		}
		if !testEndpoint {
			if err := validateGitHubURL(request.URL); err != nil {
				return err
			}
		}
		if originalRedirectCheck != nil {
			return originalRedirectCheck(request, via)
		}
		return nil
	}
	return &hardened
}

func (u *Updater) fetchRelease(
	ctx context.Context,
	requested *semanticVersion,
) (releaseMetadata, error) {
	endpoint := u.apiBaseURL + "/repos/" + defaultOwner + "/" + defaultRepository + "/releases/latest"
	if requested != nil {
		endpoint = u.apiBaseURL + "/repos/" + defaultOwner + "/" + defaultRepository +
			"/releases/tags/" + url.PathEscape(requested.canonical())
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return releaseMetadata{}, fmt.Errorf("create release request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "uddns/"+u.currentVersion)

	response, err := u.httpClient.Do(request)
	if err != nil {
		return releaseMetadata{}, fmt.Errorf("fetch release metadata: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return releaseMetadata{}, fmt.Errorf(
			"fetch release metadata: GitHub returned %s",
			response.Status,
		)
	}
	if response.ContentLength > maxReleaseMetadataSize {
		return releaseMetadata{}, errors.New("release metadata exceeds the size limit")
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxReleaseMetadataSize+1))
	if err != nil {
		return releaseMetadata{}, fmt.Errorf("read release metadata: %w", err)
	}
	if len(body) > maxReleaseMetadataSize {
		return releaseMetadata{}, fmt.Errorf("release metadata exceeds the size limit")
	}

	var release releaseMetadata
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&release); err != nil {
		return releaseMetadata{}, fmt.Errorf("decode release metadata: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return releaseMetadata{}, err
	}
	if release.TagName == "" {
		return releaseMetadata{}, fmt.Errorf("release metadata is missing tag_name")
	}
	return release, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	switch {
	case err == io.EOF:
		return nil
	case err != nil:
		return fmt.Errorf("decode release metadata: %w", err)
	default:
		return fmt.Errorf("release metadata contains multiple JSON values")
	}
}

func uniqueAssetURL(assets []releaseAsset, name string) (string, error) {
	var found string
	for _, asset := range assets {
		if asset.Name != name {
			continue
		}
		if found != "" {
			return "", fmt.Errorf("release contains duplicate %s assets", name)
		}
		if asset.BrowserDownloadURL == "" {
			return "", fmt.Errorf("release asset %s is missing its download URL", name)
		}
		found = asset.BrowserDownloadURL
	}
	if found == "" {
		return "", fmt.Errorf("release does not contain %s", name)
	}
	return found, nil
}

func (u *Updater) validateDownloadURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("malformed URL")
	}
	if u.testEndpoint {
		return nil
	}
	return validateGitHubURL(parsed)
}

func (u *Updater) validateReleaseAssetURL(rawURL, tagName, assetName string) error {
	if err := u.validateDownloadURL(rawURL); err != nil {
		return err
	}
	if u.testEndpoint {
		return nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("malformed URL")
	}
	expectedPath := "/we11adam/uddns/releases/download/" +
		url.PathEscape(tagName) + "/" + url.PathEscape(assetName)
	if strings.ToLower(parsed.Hostname()) != "github.com" ||
		parsed.EscapedPath() != expectedPath ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return fmt.Errorf("download URL does not match the selected repository release")
	}
	return nil
}

func validateGitHubURL(parsed *url.URL) error {
	if parsed.Scheme != "https" {
		return fmt.Errorf("download URL must use HTTPS")
	}
	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "api.github.com",
		"github.com",
		"objects.githubusercontent.com",
		"release-assets.githubusercontent.com":
	default:
		return fmt.Errorf("download host %q is not trusted", host)
	}
	if parsed.User != nil {
		return fmt.Errorf("download URL must not contain user information")
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return fmt.Errorf("download URL must use the default HTTPS port")
	}
	if path.Clean(parsed.Path) != parsed.Path {
		return fmt.Errorf("download URL contains a non-canonical path")
	}
	return nil
}
