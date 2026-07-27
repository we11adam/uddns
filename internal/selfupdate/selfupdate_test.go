package selfupdate

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"strings"
	"testing"
)

func TestUpdaterCheckBuildsCompletePlan(t *testing.T) {
	t.Parallel()
	release := releaseTestMetadata()
	body := releaseTestMarshalMetadata(t, release)
	updater := releaseTestUpdater(t, "1.8.0", releaseTestRoundTripFunc(
		func(*http.Request) (*http.Response, error) {
			return releaseTestResponse(http.StatusOK, body), nil
		},
	))

	plan, err := updater.Check(context.Background(), "")
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if plan.CurrentVersion != "1.8.0" {
		t.Errorf("CurrentVersion = %q, want %q", plan.CurrentVersion, "1.8.0")
	}
	if plan.TargetVersion != "v1.9.0" {
		t.Errorf("TargetVersion = %q, want %q", plan.TargetVersion, "v1.9.0")
	}
	if plan.Status != StatusUpdateAvailable {
		t.Errorf("Status = %q, want %q", plan.Status, StatusUpdateAvailable)
	}
	if plan.AssetName != releaseTestAssetName {
		t.Errorf("AssetName = %q, want %q", plan.AssetName, releaseTestAssetName)
	}
	if plan.assetURL != release.Assets[0].BrowserDownloadURL {
		t.Errorf("assetURL = %q, want %q", plan.assetURL, release.Assets[0].BrowserDownloadURL)
	}
	if plan.checksumURL != release.Assets[1].BrowserDownloadURL {
		t.Errorf(
			"checksumURL = %q, want %q",
			plan.checksumURL,
			release.Assets[1].BrowserDownloadURL,
		)
	}
	if plan.signatureURL != release.Assets[2].BrowserDownloadURL {
		t.Errorf(
			"signatureURL = %q, want %q",
			plan.signatureURL,
			release.Assets[2].BrowserDownloadURL,
		)
	}
	if got := plan.target.canonical(); got != "v1.9.0" {
		t.Errorf("target canonical version = %q, want %q", got, "v1.9.0")
	}
}

func TestUpdaterCheckVersionStatuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		currentVersion string
		wantStatus     Status
	}{
		{
			name:           "development",
			currentVersion: "dev",
			wantStatus:     StatusDevelopment,
		},
		{
			name:           "equal",
			currentVersion: "v1.9.0",
			wantStatus:     StatusUpToDate,
		},
		{
			name:           "equal ignoring build metadata",
			currentVersion: "1.9.0+local",
			wantStatus:     StatusUpToDate,
		},
		{
			name:           "newer than target",
			currentVersion: "1.10.0",
			wantStatus:     StatusNewerThanTarget,
		},
		{
			name:           "update available",
			currentVersion: "1.8.9",
			wantStatus:     StatusUpdateAvailable,
		},
	}

	body := releaseTestMarshalMetadata(t, releaseTestMetadata())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			updater := releaseTestUpdater(t, tt.currentVersion, releaseTestRoundTripFunc(
				func(*http.Request) (*http.Response, error) {
					return releaseTestResponse(http.StatusOK, body), nil
				},
			))

			plan, err := updater.Check(context.Background(), "")
			if err != nil {
				t.Fatalf("Check returned error: %v", err)
			}
			if plan.Status != tt.wantStatus {
				t.Fatalf("Status = %q, want %q", plan.Status, tt.wantStatus)
			}
		})
	}
}

func TestUpdateStatusRejectsInvalidCurrentVersion(t *testing.T) {
	t.Parallel()
	target := mustParseSemanticVersion(t, "1.9.0")
	invalid := []string{
		"",
		"vdev",
		"1.9",
		"1.09.0",
		" 1.8.0",
		"1.8.0 ",
	}

	for _, current := range invalid {
		t.Run(current, func(t *testing.T) {
			t.Parallel()
			if _, err := updateStatus(current, target); err == nil {
				t.Fatalf("updateStatus(%q) returned nil error", current)
			}
		})
	}
}

func TestUpdaterCheckRejectsReleaseState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		requestedVersion string
		mutate           func(*releaseMetadata)
		wantErrContains  string
	}{
		{
			name: "draft release",
			mutate: func(release *releaseMetadata) {
				release.Draft = true
			},
			wantErrContains: "is a draft",
		},
		{
			name: "prerelease flag",
			mutate: func(release *releaseMetadata) {
				release.Prerelease = true
			},
			wantErrContains: "is a prerelease",
		},
		{
			name: "prerelease tag",
			mutate: func(release *releaseMetadata) {
				release.TagName = "v1.9.0-rc.1"
			},
			wantErrContains: "is a prerelease",
		},
		{
			name:             "mismatched requested tag",
			requestedVersion: "v1.9.0",
			mutate: func(release *releaseMetadata) {
				release.TagName = "v1.9.1"
			},
			wantErrContains: "returned mismatched tag",
		},
		{
			name: "invalid release tag",
			mutate: func(release *releaseMetadata) {
				release.TagName = "release-1.9"
			},
			wantErrContains: "is not valid SemVer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			release := releaseTestMetadata()
			tt.mutate(&release)
			body := releaseTestMarshalMetadata(t, release)
			updater := releaseTestUpdater(t, "1.8.0", releaseTestRoundTripFunc(
				func(*http.Request) (*http.Response, error) {
					return releaseTestResponse(http.StatusOK, body), nil
				},
			))

			_, err := updater.Check(context.Background(), tt.requestedVersion)
			if err == nil {
				t.Fatal("Check returned nil error")
			}
			if !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Fatalf("Check error = %q, want substring %q", err, tt.wantErrContains)
			}
		})
	}
}

func TestUpdaterCheckRejectsInvalidCurrentVersion(t *testing.T) {
	t.Parallel()
	body := releaseTestMarshalMetadata(t, releaseTestMetadata())
	updater := releaseTestUpdater(t, "1.9", releaseTestRoundTripFunc(
		func(*http.Request) (*http.Response, error) {
			return releaseTestResponse(http.StatusOK, body), nil
		},
	))

	_, err := updater.Check(context.Background(), "")
	if err == nil {
		t.Fatal("Check returned nil error")
	}
	if !strings.Contains(err.Error(), `current version "1.9" is not valid SemVer`) {
		t.Fatalf("Check error = %q, want invalid current version error", err)
	}
}

func TestUpdaterCheckRejectsInvalidRequestedVersionBeforeHTTP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		requested       string
		wantErrContains string
	}{
		{
			name:            "incomplete SemVer",
			requested:       "1.9",
			wantErrContains: "invalid requested version",
		},
		{
			name:            "surrounding whitespace",
			requested:       " v1.9.0",
			wantErrContains: "invalid requested version",
		},
		{
			name:            "prerelease",
			requested:       "v1.9.0-rc.1",
			wantErrContains: "prerelease updates are not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			called := false
			updater := releaseTestUpdater(t, "1.8.0", releaseTestRoundTripFunc(
				func(*http.Request) (*http.Response, error) {
					called = true
					return nil, errors.New("unexpected HTTP request")
				},
			))

			_, err := updater.Check(context.Background(), tt.requested)
			if err == nil {
				t.Fatal("Check returned nil error")
			}
			if !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Fatalf("Check error = %q, want substring %q", err, tt.wantErrContains)
			}
			if called {
				t.Fatal("Check issued an HTTP request for an invalid requested version")
			}
		})
	}
}

func TestNewRejectsEmptyCurrentVersion(t *testing.T) {
	t.Parallel()
	_, err := New(Config{
		ExecutablePath: "/usr/local/bin/uddns",
		GOOS:           "linux",
		GOARCH:         "amd64",
	})
	if err == nil {
		t.Fatal("New returned nil error")
	}
	if !strings.Contains(err.Error(), "current version must not be empty") {
		t.Fatalf("New error = %q, want empty current version error", err)
	}
}

func TestUpdaterCheckRequiresUniqueExactAssets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		mutate          func(*releaseMetadata)
		wantErrContains string
	}{
		{
			name: "missing exact archive",
			mutate: func(release *releaseMetadata) {
				release.Assets[0].Name = "uddns_1.9.0_linux_arm64.tar.gz"
			},
			wantErrContains: "release does not contain " + releaseTestAssetName,
		},
		{
			name: "duplicate exact archive",
			mutate: func(release *releaseMetadata) {
				release.Assets = append(release.Assets, release.Assets[0])
			},
			wantErrContains: "duplicate " + releaseTestAssetName + " assets",
		},
		{
			name: "missing checksum",
			mutate: func(release *releaseMetadata) {
				release.Assets = release.Assets[:1]
			},
			wantErrContains: "release does not contain checksums.txt",
		},
		{
			name: "duplicate checksum",
			mutate: func(release *releaseMetadata) {
				release.Assets = append(release.Assets, release.Assets[1])
			},
			wantErrContains: "duplicate checksums.txt assets",
		},
		{
			name: "missing checksum signature",
			mutate: func(release *releaseMetadata) {
				release.Assets = release.Assets[:2]
			},
			wantErrContains: "release does not contain " + checksumSignatureName,
		},
		{
			name: "duplicate checksum signature",
			mutate: func(release *releaseMetadata) {
				release.Assets = append(release.Assets, release.Assets[2])
			},
			wantErrContains: "duplicate " + checksumSignatureName + " assets",
		},
		{
			name: "archive missing URL",
			mutate: func(release *releaseMetadata) {
				release.Assets[0].BrowserDownloadURL = ""
			},
			wantErrContains: releaseTestAssetName + " is missing its download URL",
		},
		{
			name: "checksum missing URL",
			mutate: func(release *releaseMetadata) {
				release.Assets[1].BrowserDownloadURL = ""
			},
			wantErrContains: "checksums.txt is missing its download URL",
		},
		{
			name: "checksum signature missing URL",
			mutate: func(release *releaseMetadata) {
				release.Assets[2].BrowserDownloadURL = ""
			},
			wantErrContains: checksumSignatureName + " is missing its download URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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

func TestUpdaterApplyRejectsLegacyReleaseBeforeDownload(t *testing.T) {
	t.Parallel()
	updater, err := New(Config{
		CurrentVersion: "1.9.0",
		ExecutablePath: "/usr/local/bin/uddns",
		GOOS:           runtime.GOOS,
		GOARCH:         runtime.GOARCH,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	target := mustParseSemanticVersion(t, "1.8.0")
	name, err := assetName(
		defaultRepository,
		target.artifactVersion(),
		runtime.GOOS,
		runtime.GOARCH,
	)
	if err != nil {
		t.Fatalf("assetName returned error: %v", err)
	}
	_, err = updater.Apply(context.Background(), Plan{
		CurrentVersion: "1.9.0",
		TargetVersion:  target.canonical(),
		Status:         StatusNewerThanTarget,
		AssetName:      name,
		assetURL:       "https://github.com/unused",
		checksumURL:    "https://github.com/unused",
		signatureURL:   "https://github.com/unused",
		target:         target,
	}, ApplyOptions{AllowDowngrade: true})
	if err == nil || !strings.Contains(err.Error(), "predates self-update support") {
		t.Fatalf("Apply error = %v, want legacy release error", err)
	}
}
