//go:build darwin || freebsd || linux

package selfupdate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyCandidateAtomicallyReplacesExecutableAndCreatesBackup(t *testing.T) {
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "uddns")
	stagingDirectory := filepath.Join(directory, ".uddns-update-test")
	if err := os.Mkdir(stagingDirectory, 0o700); err != nil {
		t.Fatalf("create staging directory: %v", err)
	}
	candidatePath := filepath.Join(stagingDirectory, "uddns")

	writeApplyTestFile(t, targetPath, "old executable", 0o751)
	writeApplyTestFile(t, candidatePath, "new executable", 0o600)
	candidateInfo, err := os.Stat(candidatePath)
	if err != nil {
		t.Fatalf("stat candidate: %v", err)
	}

	err = applyCandidate(
		context.Background(),
		targetPath,
		candidatePath,
		"v1.8.0",
		mustParseSemanticVersion(t, "1.9.0"),
		applyTestVerifier(map[string]string{
			"old executable": "v1.8.0",
			"new executable": "v1.9.0",
		}),
	)
	if err != nil {
		t.Fatalf("applyCandidate returned error: %v", err)
	}

	assertApplyTestContents(t, targetPath, "new executable")
	assertApplyTestContents(t, backupPath(targetPath), "old executable")
	assertApplyTestMode(t, targetPath, 0o751)
	assertApplyTestMode(t, backupPath(targetPath), 0o751)

	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("stat updated target: %v", err)
	}
	if !os.SameFile(candidateInfo, targetInfo) {
		t.Error("updated target is not the staged file renamed into place")
	}
	if _, err := os.Lstat(candidatePath); !os.IsNotExist(err) {
		t.Fatalf("candidate still exists after replacement: %v", err)
	}
}

func TestApplyCandidateVersionMismatchLeavesTargetUnchanged(t *testing.T) {
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "uddns")
	stagingDirectory := filepath.Join(directory, ".uddns-update-test")
	if err := os.Mkdir(stagingDirectory, 0o700); err != nil {
		t.Fatalf("create staging directory: %v", err)
	}
	candidatePath := filepath.Join(stagingDirectory, "uddns")

	writeApplyTestFile(t, targetPath, "old executable", 0o755)
	writeApplyTestFile(t, candidatePath, "wrong executable", 0o600)
	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}

	err = applyCandidate(
		context.Background(),
		targetPath,
		candidatePath,
		"v1.8.0",
		mustParseSemanticVersion(t, "1.9.0"),
		applyTestVerifier(map[string]string{
			"old executable":   "v1.8.0",
			"wrong executable": "v2.0.0",
		}),
	)
	if err == nil {
		t.Fatal("applyCandidate returned nil error for a mismatched candidate")
	}
	if !strings.Contains(err.Error(), "reported version v2.0.0, expected v1.9.0") {
		t.Fatalf("applyCandidate error = %q, want version mismatch", err)
	}

	assertApplyTestContents(t, targetPath, "old executable")
	unchangedInfo, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("stat unchanged target: %v", err)
	}
	if !os.SameFile(targetInfo, unchangedInfo) {
		t.Error("target was replaced after candidate version verification failed")
	}
	if _, err := os.Lstat(backupPath(targetPath)); !os.IsNotExist(err) {
		t.Fatalf("backup exists after candidate version verification failed: %v", err)
	}
}

func TestApplyCandidateRejectsStaleCurrentVersion(t *testing.T) {
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "uddns")
	stagingDirectory := filepath.Join(directory, ".uddns-update-test")
	if err := os.Mkdir(stagingDirectory, 0o700); err != nil {
		t.Fatalf("create staging directory: %v", err)
	}
	candidatePath := filepath.Join(stagingDirectory, "uddns")

	writeApplyTestFile(t, targetPath, "changed executable", 0o755)
	writeApplyTestFile(t, candidatePath, "new executable", 0o600)
	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}

	err = applyCandidate(
		context.Background(),
		targetPath,
		candidatePath,
		"v1.8.0",
		mustParseSemanticVersion(t, "1.9.0"),
		applyTestVerifier(map[string]string{
			"changed executable": "v1.8.1",
			"new executable":     "v1.9.0",
		}),
	)
	if err == nil {
		t.Fatal("applyCandidate returned nil error for a stale checked version")
	}
	if !strings.Contains(
		err.Error(),
		"installed executable changed from v1.8.0 to v1.8.1 after the update check",
	) {
		t.Fatalf("applyCandidate error = %q, want stale checked version error", err)
	}

	assertApplyTestContents(t, targetPath, "changed executable")
	unchangedInfo, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("stat unchanged target: %v", err)
	}
	if !os.SameFile(targetInfo, unchangedInfo) {
		t.Error("target was replaced after its version changed")
	}
	assertApplyTestContents(t, candidatePath, "new executable")
	if _, err := os.Lstat(backupPath(targetPath)); !os.IsNotExist(err) {
		t.Fatalf("backup exists after stale current version was rejected: %v", err)
	}
}

func TestApplyCandidateRejectsNonRegularTargets(t *testing.T) {
	tests := []struct {
		name       string
		createPath func(*testing.T, string)
	}{
		{
			name: "symlink",
			createPath: func(t *testing.T, path string) {
				t.Helper()
				referent := path + ".referent"
				writeApplyTestFile(t, referent, "old executable", 0o755)
				if err := os.Symlink(referent, path); err != nil {
					t.Fatalf("create target symlink: %v", err)
				}
			},
		},
		{
			name: "directory",
			createPath: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatalf("create target directory: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directory := t.TempDir()
			targetPath := filepath.Join(directory, "uddns")
			tt.createPath(t, targetPath)

			stagingDirectory := filepath.Join(directory, ".uddns-update-test")
			if err := os.Mkdir(stagingDirectory, 0o700); err != nil {
				t.Fatalf("create staging directory: %v", err)
			}
			candidatePath := filepath.Join(stagingDirectory, "uddns")
			writeApplyTestFile(t, candidatePath, "new executable", 0o600)

			err := applyCandidate(
				context.Background(),
				targetPath,
				candidatePath,
				"v1.8.0",
				mustParseSemanticVersion(t, "1.9.0"),
				unexpectedApplyTestVerifier(t),
			)
			if err == nil {
				t.Fatal("applyCandidate returned nil error for a non-regular target")
			}
			if !strings.Contains(err.Error(), "installed executable must be a regular file") {
				t.Fatalf("applyCandidate error = %q, want non-regular target error", err)
			}
		})
	}
}

func TestApplyCandidateRejectsNonRegularCandidates(t *testing.T) {
	tests := []struct {
		name       string
		createPath func(*testing.T, string)
	}{
		{
			name: "symlink",
			createPath: func(t *testing.T, path string) {
				t.Helper()
				referent := path + ".referent"
				writeApplyTestFile(t, referent, "new executable", 0o600)
				if err := os.Symlink(referent, path); err != nil {
					t.Fatalf("create candidate symlink: %v", err)
				}
			},
		},
		{
			name: "directory",
			createPath: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatalf("create candidate directory: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directory := t.TempDir()
			targetPath := filepath.Join(directory, "uddns")
			writeApplyTestFile(t, targetPath, "old executable", 0o755)

			stagingDirectory := filepath.Join(directory, ".uddns-update-test")
			if err := os.Mkdir(stagingDirectory, 0o700); err != nil {
				t.Fatalf("create staging directory: %v", err)
			}
			candidatePath := filepath.Join(stagingDirectory, "uddns")
			tt.createPath(t, candidatePath)

			err := applyCandidate(
				context.Background(),
				targetPath,
				candidatePath,
				"v1.8.0",
				mustParseSemanticVersion(t, "1.9.0"),
				applyTestVerifier(map[string]string{
					"old executable": "v1.8.0",
				}),
			)
			if err == nil {
				t.Fatal("applyCandidate returned nil error for a non-regular candidate")
			}
			if !strings.Contains(err.Error(), "staged executable must be a regular file") {
				t.Fatalf("applyCandidate error = %q, want non-regular candidate error", err)
			}
			assertApplyTestContents(t, targetPath, "old executable")
		})
	}
}

func TestApplyCandidateRejectsConcurrentUpdate(t *testing.T) {
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "uddns")
	writeApplyTestFile(t, targetPath, "old executable", 0o755)

	stagingDirectory := filepath.Join(directory, ".uddns-update-test")
	if err := os.Mkdir(stagingDirectory, 0o700); err != nil {
		t.Fatalf("create staging directory: %v", err)
	}
	candidatePath := filepath.Join(stagingDirectory, "uddns")
	writeApplyTestFile(t, candidatePath, "new executable", 0o600)

	lock, err := acquireUpdateLock(targetPath)
	if err != nil {
		t.Fatalf("acquire first update lock: %v", err)
	}
	defer lock.close()

	err = applyCandidate(
		context.Background(),
		targetPath,
		candidatePath,
		"v1.8.0",
		mustParseSemanticVersion(t, "1.9.0"),
		unexpectedApplyTestVerifier(t),
	)
	if err == nil {
		t.Fatal("applyCandidate returned nil error while the update lock was held")
	}
	if !strings.Contains(err.Error(), "another self-update is already running") {
		t.Fatalf("applyCandidate error = %q, want concurrent update error", err)
	}
	assertApplyTestContents(t, targetPath, "old executable")
}

func TestApplyCandidateFailurePreservesExistingPrevious(t *testing.T) {
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "uddns")
	previousPath := backupPath(targetPath)
	stagingDirectory := filepath.Join(directory, ".uddns-update-test")
	if err := os.Mkdir(stagingDirectory, 0o700); err != nil {
		t.Fatalf("create staging directory: %v", err)
	}
	candidatePath := filepath.Join(stagingDirectory, "uddns")

	writeApplyTestFile(t, targetPath, "current executable", 0o755)
	writeApplyTestFile(t, previousPath, "older executable", 0o755)
	writeApplyTestFile(t, candidatePath, "new executable", 0o600)

	verifier := applyTestVerifier(map[string]string{
		"current executable": "v1.9.0",
		"new executable":     "v1.10.0",
	})
	removingVerifier := func(ctx context.Context, path string) (string, error) {
		reportedVersion, err := verifier(ctx, path)
		if err != nil {
			return "", err
		}
		if path == candidatePath {
			if err := os.Remove(candidatePath); err != nil {
				return "", err
			}
		}
		return reportedVersion, nil
	}

	err := applyCandidate(
		context.Background(),
		targetPath,
		candidatePath,
		"v1.9.0",
		mustParseSemanticVersion(t, "v1.10.0"),
		removingVerifier,
	)
	if err == nil || !strings.Contains(err.Error(), "replace installed executable") {
		t.Fatalf("applyCandidate error = %v, want replace error", err)
	}
	assertApplyTestContents(t, targetPath, "current executable")
	assertApplyTestContents(t, previousPath, "older executable")
	if _, err := os.Lstat(pendingBackupPath(targetPath)); !os.IsNotExist(err) {
		t.Fatalf("pending backup remains after recovered failure: %v", err)
	}
}

func TestApplyCandidateRejectsExistingPendingRecoveryBackup(t *testing.T) {
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "uddns")
	stagingDirectory := filepath.Join(directory, ".uddns-update-test")
	if err := os.Mkdir(stagingDirectory, 0o700); err != nil {
		t.Fatalf("create staging directory: %v", err)
	}
	candidatePath := filepath.Join(stagingDirectory, "uddns")

	writeApplyTestFile(t, targetPath, "current executable", 0o755)
	writeApplyTestFile(t, candidatePath, "new executable", 0o600)
	writeApplyTestFile(t, pendingBackupPath(targetPath), "recovery executable", 0o755)

	err := applyCandidate(
		context.Background(),
		targetPath,
		candidatePath,
		"v1.9.0",
		mustParseSemanticVersion(t, "v1.10.0"),
		applyTestVerifier(map[string]string{
			"current executable": "v1.9.0",
			"new executable":     "v1.10.0",
		}),
	)
	if err == nil || !strings.Contains(err.Error(), "pending recovery backup already exists") {
		t.Fatalf("applyCandidate error = %v, want pending recovery error", err)
	}
	assertApplyTestContents(t, targetPath, "current executable")
	assertApplyTestContents(t, pendingBackupPath(targetPath), "recovery executable")
}

func TestRollbackExecutableExchangesCurrentAndPrevious(t *testing.T) {
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "uddns")
	previousPath := backupPath(targetPath)
	writeApplyTestFile(t, targetPath, "current executable", 0o751)
	writeApplyTestFile(t, previousPath, "previous executable", 0o711)

	fromVersion, toVersion, err := rollbackExecutable(
		context.Background(),
		targetPath,
		applyTestVerifier(map[string]string{
			"current executable":  "v2.0.0",
			"previous executable": "v1.9.0",
		}),
	)
	if err != nil {
		t.Fatalf("rollbackExecutable returned error: %v", err)
	}
	if fromVersion != "v2.0.0" {
		t.Errorf("fromVersion = %q, want %q", fromVersion, "v2.0.0")
	}
	if toVersion != "v1.9.0" {
		t.Errorf("toVersion = %q, want %q", toVersion, "v1.9.0")
	}

	assertApplyTestContents(t, targetPath, "previous executable")
	assertApplyTestContents(t, previousPath, "current executable")
	assertApplyTestMode(t, targetPath, 0o711)
	assertApplyTestMode(t, previousPath, 0o751)
}

func TestRollbackExecutableRestoresDevelopmentBackup(t *testing.T) {
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "uddns")
	previousPath := backupPath(targetPath)
	writeApplyTestFile(t, targetPath, "current executable", 0o755)
	writeApplyTestFile(t, previousPath, "development executable", 0o755)

	fromVersion, toVersion, err := rollbackExecutable(
		context.Background(),
		targetPath,
		applyTestVerifier(map[string]string{
			"current executable":     "v1.9.0",
			"development executable": "dev",
		}),
	)
	if err != nil {
		t.Fatalf("rollbackExecutable returned error: %v", err)
	}
	if fromVersion != "v1.9.0" || toVersion != "dev" {
		t.Fatalf("unexpected rollback versions: %q -> %q", fromVersion, toVersion)
	}
	assertApplyTestContents(t, targetPath, "development executable")
	assertApplyTestContents(t, previousPath, "current executable")
}

func TestRollbackExecutableRestoresCurrentWhenBackupPublishFails(t *testing.T) {
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "uddns")
	previousPath := backupPath(targetPath)
	writeApplyTestFile(t, targetPath, "current executable", 0o755)
	writeApplyTestFile(t, previousPath, "previous executable", 0o755)

	verifier := applyTestVerifier(map[string]string{
		"current executable":  "v2.0.0",
		"previous executable": "v1.9.0",
	})
	mutatingVerifier := func(ctx context.Context, path string) (string, error) {
		version, err := verifier(ctx, path)
		if err != nil {
			return "", err
		}
		if path != previousPath && version == "v1.9.0" {
			if err := os.Remove(previousPath); err != nil {
				return "", err
			}
			if err := os.Mkdir(previousPath, 0o700); err != nil {
				return "", err
			}
		}
		return version, nil
	}

	_, _, err := rollbackExecutable(
		context.Background(),
		targetPath,
		mutatingVerifier,
	)
	if err == nil || !strings.Contains(err.Error(), "original executable was restored") {
		t.Fatalf("rollbackExecutable error = %v, want recovered rollback error", err)
	}
	assertApplyTestContents(t, targetPath, "current executable")
	info, statErr := os.Lstat(previousPath)
	if statErr != nil {
		t.Fatalf("inspect mutated previous path: %v", statErr)
	}
	if !info.IsDir() {
		t.Fatalf("mutated previous path mode = %v, want directory", info.Mode())
	}
}

func TestRollbackExecutableWithoutPreviousLeavesTargetUnchanged(t *testing.T) {
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "uddns")
	writeApplyTestFile(t, targetPath, "current executable", 0o755)
	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}

	_, _, err = rollbackExecutable(
		context.Background(),
		targetPath,
		unexpectedApplyTestVerifier(t),
	)
	if err == nil {
		t.Fatal("rollbackExecutable returned nil error without a previous executable")
	}
	if !strings.Contains(err.Error(), "inspect previous executable") {
		t.Fatalf("rollbackExecutable error = %q, want missing previous error", err)
	}

	assertApplyTestContents(t, targetPath, "current executable")
	unchangedInfo, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("stat unchanged target: %v", err)
	}
	if !os.SameFile(targetInfo, unchangedInfo) {
		t.Error("target was replaced when no previous executable existed")
	}
}

func applyTestVerifier(versions map[string]string) binaryVerifier {
	return func(ctx context.Context, path string) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		version, ok := versions[string(content)]
		if !ok {
			return "", fmt.Errorf("no fake version for executable content %q", content)
		}
		return version, nil
	}
}

func unexpectedApplyTestVerifier(t *testing.T) binaryVerifier {
	t.Helper()

	return func(context.Context, string) (string, error) {
		t.Error("binary verifier was called unexpectedly")
		return "", fmt.Errorf("unexpected binary verification")
	}
}

func writeApplyTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

func assertApplyTestContents(t *testing.T, path, want string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(content) != want {
		t.Errorf("%s contents = %q, want %q", path, content, want)
	}
}

func assertApplyTestMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %#o, want %#o", path, got, want)
	}
}
