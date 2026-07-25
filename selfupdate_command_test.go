package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/we11adam/uddns/internal/selfupdate"
)

type fakeSelfUpdater struct {
	plan          selfupdate.Plan
	checkError    error
	result        selfupdate.Result
	applyError    error
	rollbackError error

	checkVersion  string
	applyPlan     selfupdate.Plan
	applyOptions  selfupdate.ApplyOptions
	checkCalls    int
	applyCalls    int
	rollbackCalls int
}

func (f *fakeSelfUpdater) Check(
	_ context.Context,
	requestedVersion string,
) (selfupdate.Plan, error) {
	f.checkCalls++
	f.checkVersion = requestedVersion
	return f.plan, f.checkError
}

func (f *fakeSelfUpdater) Apply(
	_ context.Context,
	plan selfupdate.Plan,
	options selfupdate.ApplyOptions,
) (selfupdate.Result, error) {
	f.applyCalls++
	f.applyPlan = plan
	f.applyOptions = options
	return f.result, f.applyError
}

func (f *fakeSelfUpdater) Rollback(context.Context) (selfupdate.Result, error) {
	f.rollbackCalls++
	return f.result, f.rollbackError
}

func TestRunSelfUpdateCheck(t *testing.T) {
	fake := &fakeSelfUpdater{
		plan: selfupdate.Plan{
			CurrentVersion: "1.8.0",
			TargetVersion:  "v1.9.0",
			Status:         selfupdate.StatusUpdateAvailable,
			AssetName:      "uddns_1.9.0_linux_amd64.tar.gz",
		},
	}
	stdout, stderr, code := runFakeSelfUpdate(
		t,
		fake,
		"--check",
		"--version",
		"v1.9.0",
	)
	if code != 0 {
		t.Fatalf("expected success, got %d: %s", code, stderr)
	}
	if fake.checkCalls != 1 || fake.applyCalls != 0 || fake.rollbackCalls != 0 {
		t.Fatalf("unexpected calls: %#v", fake)
	}
	if fake.checkVersion != "v1.9.0" {
		t.Fatalf("unexpected requested version: %q", fake.checkVersion)
	}
	if !strings.Contains(stdout, "update available: 1.8.0 -> v1.9.0") {
		t.Fatalf("unexpected output: %q", stdout)
	}
}

func TestRunSelfUpdateCheckStatuses(t *testing.T) {
	tests := []struct {
		status selfupdate.Status
		want   string
	}{
		{status: selfupdate.StatusDevelopment, want: "development build"},
		{status: selfupdate.StatusNewerThanTarget, want: "is newer than target"},
		{status: selfupdate.StatusUpToDate, want: "up to date"},
	}
	for _, test := range tests {
		t.Run(string(test.status), func(t *testing.T) {
			fake := &fakeSelfUpdater{
				plan: selfupdate.Plan{
					CurrentVersion: "1.9.0",
					TargetVersion:  "v1.9.0",
					Status:         test.status,
				},
			}
			stdout, stderr, code := runFakeSelfUpdate(t, fake, "--check")
			if code != 0 {
				t.Fatalf("expected success, got %d: %s", code, stderr)
			}
			if !strings.Contains(stdout, test.want) {
				t.Fatalf("unexpected output: %q", stdout)
			}
		})
	}
}

func TestRunSelfUpdateCheckJSON(t *testing.T) {
	fake := &fakeSelfUpdater{
		plan: selfupdate.Plan{
			CurrentVersion: "1.8.0",
			TargetVersion:  "v1.9.0",
			Status:         selfupdate.StatusUpdateAvailable,
			AssetName:      "asset",
		},
	}
	stdout, stderr, code := runFakeSelfUpdate(t, fake, "--check", "--json")
	if code != 0 {
		t.Fatalf("expected success, got %d: %s", code, stderr)
	}
	var plan selfupdate.Plan
	if err := json.Unmarshal([]byte(stdout), &plan); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if plan.Status != selfupdate.StatusUpdateAvailable || plan.AssetName != "asset" {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestRunSelfUpdateAppliesPlan(t *testing.T) {
	fake := &fakeSelfUpdater{
		plan: selfupdate.Plan{
			CurrentVersion: "dev",
			TargetVersion:  "v1.9.0",
			Status:         selfupdate.StatusDevelopment,
		},
		result: selfupdate.Result{
			Changed:     true,
			FromVersion: "dev",
			ToVersion:   "v1.9.0",
			Path:        "/usr/local/bin/uddns",
			BackupPath:  "/usr/local/bin/uddns.previous",
		},
	}
	stdout, stderr, code := runFakeSelfUpdate(
		t,
		fake,
		"--allow-dev",
		"--allow-downgrade",
	)
	if code != 0 {
		t.Fatalf("expected success, got %d: %s", code, stderr)
	}
	if fake.checkCalls != 1 || fake.applyCalls != 1 {
		t.Fatalf("expected check and apply, got %#v", fake)
	}
	if !fake.applyOptions.AllowDevelopment || !fake.applyOptions.AllowDowngrade {
		t.Fatalf("apply options were not forwarded: %#v", fake.applyOptions)
	}
	if !strings.Contains(stdout, "updated uddns from dev to v1.9.0") ||
		!strings.Contains(stdout, fake.result.BackupPath) {
		t.Fatalf("unexpected output: %q", stdout)
	}
}

func TestRunSelfUpdateAlreadyCurrent(t *testing.T) {
	fake := &fakeSelfUpdater{
		plan: selfupdate.Plan{Status: selfupdate.StatusUpToDate},
		result: selfupdate.Result{
			ToVersion: "v1.9.0",
			Path:      "/usr/local/bin/uddns",
		},
	}
	stdout, stderr, code := runFakeSelfUpdate(t, fake)
	if code != 0 {
		t.Fatalf("expected success, got %d: %s", code, stderr)
	}
	if stdout != "already up to date: v1.9.0\n" {
		t.Fatalf("unexpected output: %q", stdout)
	}
}

func TestRunSelfUpdateRollback(t *testing.T) {
	fake := &fakeSelfUpdater{
		result: selfupdate.Result{
			Changed:     true,
			FromVersion: "v1.9.0",
			ToVersion:   "1.8.0",
			BackupPath:  "/usr/local/bin/uddns.previous",
		},
	}
	stdout, stderr, code := runFakeSelfUpdate(t, fake, "--rollback")
	if code != 0 {
		t.Fatalf("expected success, got %d: %s", code, stderr)
	}
	if fake.rollbackCalls != 1 || fake.checkCalls != 0 || fake.applyCalls != 0 {
		t.Fatalf("unexpected calls: %#v", fake)
	}
	if !strings.Contains(stdout, "rolled back uddns") {
		t.Fatalf("unexpected output: %q", stdout)
	}
}

func TestRunSelfUpdateJSONResult(t *testing.T) {
	fake := &fakeSelfUpdater{
		plan: selfupdate.Plan{Status: selfupdate.StatusUpdateAvailable},
		result: selfupdate.Result{
			Changed:   true,
			ToVersion: "v1.9.0",
			Path:      "/tmp/uddns",
		},
	}
	stdout, stderr, code := runFakeSelfUpdate(t, fake, "--json")
	if code != 0 {
		t.Fatalf("expected success, got %d: %s", code, stderr)
	}
	var result selfupdate.Result
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode result JSON: %v", err)
	}
	if !result.Changed || result.ToVersion != "v1.9.0" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestRunSelfUpdateRejectsInvalidFlagCombinations(t *testing.T) {
	tests := [][]string{
		{"extra"},
		{"--unknown"},
		{"--rollback", "--check"},
		{"--rollback", "--version", "v1.8.0"},
		{"--rollback", "--allow-dev"},
		{"--check", "--allow-dev"},
		{"--check", "--allow-downgrade"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			factoryCalls := 0
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := runSelfUpdateCommand(
				context.Background(),
				args,
				&stdout,
				&stderr,
				commandDependencies{
					newSelfUpdater: func() (selfUpdater, error) {
						factoryCalls++
						return &fakeSelfUpdater{}, nil
					},
				},
			)
			if code != 2 {
				t.Fatalf("expected usage error for %v, got %d", args, code)
			}
			if factoryCalls != 0 {
				t.Fatalf("factory called for invalid arguments %v", args)
			}
		})
	}
}

func TestRunSelfUpdateHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runSelfUpdateCommand(
		context.Background(),
		[]string{"--help"},
		&stdout,
		&stderr,
		commandDependencies{},
	)
	if code != 0 {
		t.Fatalf("expected help success, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Usage of uddns self-update") {
		t.Fatalf("unexpected help output: %q", stderr.String())
	}
}

func TestRunSelfUpdateReportsOperationalErrors(t *testing.T) {
	tests := []struct {
		name string
		fake *fakeSelfUpdater
		args []string
	}{
		{
			name: "check",
			fake: &fakeSelfUpdater{checkError: errors.New("check failed")},
		},
		{
			name: "apply",
			fake: &fakeSelfUpdater{applyError: errors.New("apply failed")},
		},
		{
			name: "rollback",
			fake: &fakeSelfUpdater{rollbackError: errors.New("rollback failed")},
			args: []string{"--rollback"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, stderr, code := runFakeSelfUpdate(t, test.fake, test.args...)
			if code != 1 {
				t.Fatalf("expected operational error, got %d", code)
			}
			if !strings.Contains(stderr, "failed") {
				t.Fatalf("unexpected stderr: %q", stderr)
			}
		})
	}
}

func TestRunSelfUpdateReportsFactoryAndPermissionErrors(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runSelfUpdateCommand(
		context.Background(),
		nil,
		&stdout,
		&stderr,
		commandDependencies{
			newSelfUpdater: func() (selfUpdater, error) {
				return nil, os.ErrPermission
			},
		},
	)
	if code != 1 {
		t.Fatalf("expected operational error, got %d", code)
	}
	if strings.Contains(stderr.String(), "sudo") {
		t.Fatalf("factory error unexpectedly suggested sudo: %q", stderr.String())
	}

	stderr.Reset()
	code = runSelfUpdateCommand(
		context.Background(),
		nil,
		&stdout,
		&stderr,
		commandDependencies{},
	)
	if code != 1 || !strings.Contains(stderr.String(), "unavailable") {
		t.Fatalf("unexpected missing dependency result: %d %q", code, stderr.String())
	}
}

func TestRunSelfUpdateSuggestsSudoOnlyForLocalApplyPermissionErrors(t *testing.T) {
	fake := &fakeSelfUpdater{
		applyError: &selfupdate.LocalPermissionError{Err: os.ErrPermission},
	}
	_, stderr, code := runFakeSelfUpdate(t, fake)
	if code != 1 {
		t.Fatalf("expected operational error, got %d", code)
	}
	if !strings.Contains(stderr, "try running the same command with sudo") {
		t.Fatalf("expected sudo hint, got %q", stderr)
	}

	fake = &fakeSelfUpdater{checkError: os.ErrPermission}
	_, stderr, code = runFakeSelfUpdate(t, fake, "--check")
	if code != 1 {
		t.Fatalf("expected operational error, got %d", code)
	}
	if strings.Contains(stderr, "sudo") {
		t.Fatalf("check error unexpectedly suggested sudo: %q", stderr)
	}
}

func TestSelfUpdateOutputWriteFailures(t *testing.T) {
	plan := selfupdate.Plan{
		Status:        selfupdate.StatusUpdateAvailable,
		TargetVersion: "v1.9.0",
	}
	if code := printSelfUpdatePlan(failingWriter{}, failingWriter{}, plan, false); code != 1 {
		t.Fatalf("expected text plan write error, got %d", code)
	}
	if code := printSelfUpdatePlan(failingWriter{}, failingWriter{}, plan, true); code != 1 {
		t.Fatalf("expected JSON plan write error, got %d", code)
	}
	result := selfupdate.Result{Changed: true, ToVersion: "v1.9.0"}
	if code := printSelfUpdateResult(
		failingWriter{},
		failingWriter{},
		result,
		false,
		false,
	); code != 1 {
		t.Fatalf("expected text result write error, got %d", code)
	}
	if code := printSelfUpdateResult(
		failingWriter{},
		failingWriter{},
		result,
		true,
		false,
	); code != 1 {
		t.Fatalf("expected JSON result write error, got %d", code)
	}
}

func runFakeSelfUpdate(
	t *testing.T,
	fake *fakeSelfUpdater,
	args ...string,
) (string, string, int) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runSelfUpdateCommand(
		context.Background(),
		args,
		&stdout,
		&stderr,
		commandDependencies{
			newSelfUpdater: func() (selfUpdater, error) {
				return fake, nil
			},
		},
	)
	return stdout.String(), stderr.String(), code
}
