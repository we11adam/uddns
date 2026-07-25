package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	defaultAPIBaseURL = "https://api.github.com"
	defaultOwner      = "we11adam"
	defaultRepository = "uddns"
)

var earliestSelfUpdateRelease = semanticVersion{
	core: [3]string{"1", "9", "0"},
}

type Status string

const (
	StatusDevelopment     Status = "development"
	StatusNewerThanTarget Status = "newer_than_target"
	StatusUpdateAvailable Status = "update_available"
	StatusUpToDate        Status = "up_to_date"
)

type Config struct {
	CurrentVersion string
	ExecutablePath string
	GOOS           string
	GOARCH         string

	HTTPClient *http.Client
	APIBaseURL string
}

type Plan struct {
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	Status         Status `json:"status"`
	AssetName      string `json:"asset_name"`

	assetURL    string
	checksumURL string
	target      semanticVersion
}

type ApplyOptions struct {
	AllowDevelopment bool
	AllowDowngrade   bool
}

type LocalPermissionError struct {
	Err error
}

func (e *LocalPermissionError) Error() string {
	return e.Err.Error()
}

func (e *LocalPermissionError) Unwrap() error {
	return e.Err
}

type Result struct {
	Changed     bool   `json:"changed"`
	FromVersion string `json:"from_version,omitempty"`
	ToVersion   string `json:"to_version,omitempty"`
	Path        string `json:"path"`
	BackupPath  string `json:"backup_path,omitempty"`
}

type Updater struct {
	currentVersion string
	executablePath string
	goos           string
	goarch         string
	httpClient     *http.Client
	apiBaseURL     string
	testEndpoint   bool
	verifyBinary   binaryVerifier
	verifyPlatform binaryPlatformVerifier
}

func New(config Config) (*Updater, error) {
	if config.CurrentVersion == "" {
		return nil, fmt.Errorf("current version must not be empty")
	}
	if config.ExecutablePath == "" {
		return nil, fmt.Errorf("executable path must not be empty")
	}
	if !filepath.IsAbs(config.ExecutablePath) {
		return nil, fmt.Errorf("executable path must be absolute")
	}
	if config.GOOS == "" || config.GOARCH == "" {
		return nil, fmt.Errorf("runtime target must not be empty")
	}

	apiBaseURL := strings.TrimRight(config.APIBaseURL, "/")
	testEndpoint := apiBaseURL != ""
	if apiBaseURL == "" {
		apiBaseURL = defaultAPIBaseURL
	}
	parsedBaseURL, err := url.Parse(apiBaseURL)
	if err != nil || parsedBaseURL.Host == "" {
		return nil, fmt.Errorf("invalid release API base URL")
	}
	if !testEndpoint && (parsedBaseURL.Scheme != "https" || parsedBaseURL.Host != "api.github.com") {
		return nil, fmt.Errorf("release API must use the official GitHub HTTPS endpoint")
	}

	client := config.HTTPClient
	client = hardenedHTTPClient(client, testEndpoint)

	return &Updater{
		currentVersion: config.CurrentVersion,
		executablePath: filepath.Clean(config.ExecutablePath),
		goos:           config.GOOS,
		goarch:         config.GOARCH,
		httpClient:     client,
		apiBaseURL:     apiBaseURL,
		testEndpoint:   testEndpoint,
		verifyBinary:   inspectBinaryVersion,
		verifyPlatform: inspectBinaryPlatform,
	}, nil
}

func NewForCurrentExecutable(currentVersion string) (*Updater, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve current executable: %w", err)
	}

	return New(Config{
		CurrentVersion: currentVersion,
		ExecutablePath: executablePath,
		GOOS:           runtime.GOOS,
		GOARCH:         runtime.GOARCH,
	})
}

func (u *Updater) Check(ctx context.Context, requestedVersion string) (Plan, error) {
	requested, err := requestedReleaseVersion(requestedVersion)
	if err != nil {
		return Plan{}, err
	}

	release, err := u.fetchRelease(ctx, requested)
	if err != nil {
		return Plan{}, err
	}
	if release.Draft {
		return Plan{}, fmt.Errorf("release %q is a draft", release.TagName)
	}
	if release.Prerelease {
		return Plan{}, fmt.Errorf("release %q is a prerelease", release.TagName)
	}

	target, err := parseSemanticVersion(release.TagName)
	if err != nil {
		return Plan{}, fmt.Errorf("release tag %q is not valid SemVer: %w", release.TagName, err)
	}
	if target.isPrerelease() {
		return Plan{}, fmt.Errorf("release tag %q is a prerelease", release.TagName)
	}
	if requested != nil && requested.canonical() != target.canonical() {
		return Plan{}, fmt.Errorf(
			"requested release %s returned mismatched tag %s",
			requested.canonical(),
			target.canonical(),
		)
	}

	name, err := assetName(defaultRepository, target.artifactVersion(), u.goos, u.goarch)
	if err != nil {
		return Plan{}, err
	}
	assetURL, err := uniqueAssetURL(release.Assets, name)
	if err != nil {
		return Plan{}, err
	}
	checksumURL, err := uniqueAssetURL(release.Assets, "checksums.txt")
	if err != nil {
		return Plan{}, err
	}
	if err := u.validateReleaseAssetURL(
		assetURL,
		release.TagName,
		name,
	); err != nil {
		return Plan{}, fmt.Errorf("invalid release asset URL: %w", err)
	}
	if err := u.validateReleaseAssetURL(
		checksumURL,
		release.TagName,
		"checksums.txt",
	); err != nil {
		return Plan{}, fmt.Errorf("invalid checksum URL: %w", err)
	}

	status, err := updateStatus(u.currentVersion, target)
	if err != nil {
		return Plan{}, err
	}

	return Plan{
		CurrentVersion: u.currentVersion,
		TargetVersion:  target.canonical(),
		Status:         status,
		AssetName:      name,
		assetURL:       assetURL,
		checksumURL:    checksumURL,
		target:         target,
	}, nil
}

func (u *Updater) Apply(ctx context.Context, plan Plan, options ApplyOptions) (Result, error) {
	if u.goos != runtime.GOOS || u.goarch != runtime.GOARCH {
		return Result{}, fmt.Errorf(
			"cannot install a %s/%s release from %s/%s",
			u.goos,
			u.goarch,
			runtime.GOOS,
			runtime.GOARCH,
		)
	}
	if !canApplyOnCurrentPlatform() {
		return Result{}, fmt.Errorf("self-update is not supported on %s", runtime.GOOS)
	}
	if plan.assetURL == "" || plan.checksumURL == "" || plan.AssetName == "" {
		return Result{}, fmt.Errorf("update plan is incomplete")
	}

	switch plan.Status {
	case StatusUpToDate:
		return Result{
			Path:        u.executablePath,
			FromVersion: plan.CurrentVersion,
			ToVersion:   plan.TargetVersion,
		}, nil
	case StatusDevelopment:
		if !options.AllowDevelopment {
			return Result{}, fmt.Errorf("development builds require --allow-dev to self-update")
		}
	case StatusNewerThanTarget:
		if !options.AllowDowngrade {
			return Result{}, fmt.Errorf("downgrades require --allow-downgrade")
		}
	case StatusUpdateAvailable:
	default:
		return Result{}, fmt.Errorf("update plan has unknown status %q", plan.Status)
	}
	if compareSemanticVersions(plan.target, earliestSelfUpdateRelease) < 0 {
		return Result{}, fmt.Errorf(
			"target %s predates self-update support (minimum %s); install it manually",
			plan.target.canonical(),
			earliestSelfUpdateRelease.canonical(),
		)
	}

	expectedName, err := assetName(
		defaultRepository,
		plan.target.artifactVersion(),
		u.goos,
		u.goarch,
	)
	if err != nil {
		return Result{}, err
	}
	if expectedName != plan.AssetName {
		return Result{}, fmt.Errorf("update plan asset does not match its target version")
	}

	targetDirectory := filepath.Dir(u.executablePath)
	stagingDirectory, err := os.MkdirTemp(targetDirectory, ".uddns-update-*")
	if err != nil {
		return Result{}, markLocalPermissionError(
			fmt.Errorf("create update staging directory: %w", err),
		)
	}
	defer os.RemoveAll(stagingDirectory)

	candidatePath, err := u.downloadCandidate(ctx, plan, stagingDirectory)
	if err != nil {
		return Result{}, err
	}
	if err := os.Chmod(candidatePath, 0o700); err != nil {
		return Result{}, markLocalPermissionError(
			fmt.Errorf("make staged executable runnable: %w", err),
		)
	}
	if err := u.verifyPlatform(
		ctx,
		candidatePath,
		u.goos,
		u.goarch,
	); err != nil {
		return Result{}, fmt.Errorf("verify staged executable platform: %w", err)
	}
	if err := applyCandidate(
		ctx,
		u.executablePath,
		candidatePath,
		plan.CurrentVersion,
		plan.target,
		u.verifyBinary,
	); err != nil {
		return Result{}, markLocalPermissionError(err)
	}

	return Result{
		Changed:     true,
		FromVersion: plan.CurrentVersion,
		ToVersion:   plan.TargetVersion,
		Path:        u.executablePath,
		BackupPath:  backupPath(u.executablePath),
	}, nil
}

func (u *Updater) Rollback(ctx context.Context) (Result, error) {
	if u.goos != runtime.GOOS || u.goarch != runtime.GOARCH {
		return Result{}, fmt.Errorf("rollback target does not match the current runtime")
	}
	if !canApplyOnCurrentPlatform() {
		return Result{}, fmt.Errorf("self-update is not supported on %s", runtime.GOOS)
	}

	fromVersion, toVersion, err := rollbackExecutable(
		ctx,
		u.executablePath,
		u.verifyBinary,
	)
	if err != nil {
		return Result{}, markLocalPermissionError(err)
	}
	return Result{
		Changed:     true,
		FromVersion: fromVersion,
		ToVersion:   toVersion,
		Path:        u.executablePath,
		BackupPath:  backupPath(u.executablePath),
	}, nil
}

func requestedReleaseVersion(value string) (*semanticVersion, error) {
	if value == "" || value == "latest" {
		return nil, nil
	}
	version, err := parseSemanticVersion(value)
	if err != nil {
		return nil, fmt.Errorf("invalid requested version %q: %w", value, err)
	}
	if version.isPrerelease() {
		return nil, fmt.Errorf("prerelease updates are not supported")
	}
	return &version, nil
}

func updateStatus(current string, target semanticVersion) (Status, error) {
	if current == "dev" {
		return StatusDevelopment, nil
	}
	currentVersion, err := parseSemanticVersion(current)
	if err != nil {
		return "", fmt.Errorf("current version %q is not valid SemVer: %w", current, err)
	}

	switch compareSemanticVersions(currentVersion, target) {
	case -1:
		return StatusUpdateAvailable, nil
	case 0:
		return StatusUpToDate, nil
	default:
		return StatusNewerThanTarget, nil
	}
}

func backupPath(executablePath string) string {
	return executablePath + ".previous"
}

func markLocalPermissionError(err error) error {
	if !errors.Is(err, os.ErrPermission) {
		return err
	}
	var marked *LocalPermissionError
	if errors.As(err, &marked) {
		return err
	}
	return &LocalPermissionError{Err: err}
}
