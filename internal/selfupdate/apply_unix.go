//go:build darwin || freebsd || linux

package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

type updateLock struct {
	file *os.File
}

func canApplyOnCurrentPlatform() bool {
	return true
}

func applyCandidate(
	ctx context.Context,
	targetPath string,
	candidatePath string,
	expectedCurrentVersion string,
	expectedVersion semanticVersion,
	verifier binaryVerifier,
) error {
	lock, err := acquireUpdateLock(targetPath)
	if err != nil {
		return err
	}
	defer lock.close()

	targetInfo, err := regularFileInfo(targetPath, "installed executable")
	if err != nil {
		return err
	}
	if err := verifyInstalledVersion(
		ctx,
		targetPath,
		expectedCurrentVersion,
		verifier,
	); err != nil {
		return err
	}
	if _, err := regularFileInfo(candidatePath, "staged executable"); err != nil {
		return err
	}
	if err := setFileIdentity(candidatePath, targetInfo); err != nil {
		return fmt.Errorf("prepare staged executable permissions: %w", err)
	}
	if err := verifyExpectedVersion(ctx, candidatePath, expectedVersion, verifier); err != nil {
		return fmt.Errorf("verify staged executable: %w", err)
	}

	pendingPath := pendingBackupPath(targetPath)
	pendingTemporary := filepath.Join(filepath.Dir(candidatePath), "uddns.pending")
	if err := preparePendingBackup(
		targetPath,
		pendingTemporary,
		pendingPath,
		targetInfo,
	); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf(
				"pending recovery backup already exists at %s; resolve it before updating",
				pendingPath,
			)
		}
		return fmt.Errorf("prepare executable recovery backup: %w", err)
	}

	return publishCandidateWithBackup(
		targetPath,
		candidatePath,
		pendingPath,
		backupPath(targetPath),
	)
}

func verifyInstalledVersion(
	ctx context.Context,
	targetPath string,
	expectedValue string,
	verifier binaryVerifier,
) error {
	actualValue, err := verifier(ctx, targetPath)
	if err != nil {
		return fmt.Errorf("inspect installed executable: %w", err)
	}
	if expectedValue == "dev" {
		if actualValue != expectedValue {
			return fmt.Errorf(
				"installed executable changed from %s to %s after the update check; check again",
				expectedValue,
				actualValue,
			)
		}
		return nil
	}

	expected, err := parseSemanticVersion(expectedValue)
	if err != nil {
		return fmt.Errorf("invalid checked version %q: %w", expectedValue, err)
	}
	actual, err := parseSemanticVersion(actualValue)
	if err != nil {
		return fmt.Errorf("installed executable reported invalid version %q: %w", actualValue, err)
	}
	if actual.canonical() != expected.canonical() {
		return fmt.Errorf(
			"installed executable changed from %s to %s after the update check; check again",
			expected.canonical(),
			actual.canonical(),
		)
	}
	return nil
}

func rollbackExecutable(
	ctx context.Context,
	targetPath string,
	verifier binaryVerifier,
) (string, string, error) {
	lock, err := acquireUpdateLock(targetPath)
	if err != nil {
		return "", "", err
	}
	defer lock.close()

	targetInfo, err := regularFileInfo(targetPath, "installed executable")
	if err != nil {
		return "", "", err
	}
	previousPath := backupPath(targetPath)
	previousInfo, err := regularFileInfo(previousPath, "previous executable")
	if err != nil {
		return "", "", err
	}

	fromVersion, err := verifier(ctx, targetPath)
	if err != nil {
		return "", "", fmt.Errorf("inspect installed executable: %w", err)
	}
	toVersion, err := verifier(ctx, previousPath)
	if err != nil {
		return "", "", fmt.Errorf("inspect previous executable: %w", err)
	}

	stagingDirectory, err := os.MkdirTemp(filepath.Dir(targetPath), ".uddns-rollback-*")
	if err != nil {
		return "", "", fmt.Errorf("create rollback staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDirectory)

	rollbackCandidate := filepath.Join(stagingDirectory, "uddns.rollback")
	if err := copyRegularFile(previousPath, rollbackCandidate, previousInfo); err != nil {
		return "", "", fmt.Errorf("stage previous executable: %w", err)
	}
	if err := verifyExpectedVersionValue(
		ctx,
		rollbackCandidate,
		toVersion,
		verifier,
	); err != nil {
		return "", "", fmt.Errorf("verify previous executable: %w", err)
	}

	pendingPath := pendingBackupPath(targetPath)
	pendingTemporary := filepath.Join(stagingDirectory, "uddns.pending")
	if err := preparePendingBackup(
		targetPath,
		pendingTemporary,
		pendingPath,
		targetInfo,
	); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", "", fmt.Errorf(
				"pending recovery backup already exists at %s; resolve it before rolling back",
				pendingPath,
			)
		}
		return "", "", fmt.Errorf("prepare rollback recovery backup: %w", err)
	}
	if err := publishCandidateWithBackup(
		targetPath,
		rollbackCandidate,
		pendingPath,
		previousPath,
	); err != nil {
		return "", "", err
	}
	return fromVersion, toVersion, nil
}

func preparePendingBackup(
	sourcePath string,
	temporaryPath string,
	pendingPath string,
	sourceInfo os.FileInfo,
) error {
	if err := copyRegularFile(sourcePath, temporaryPath, sourceInfo); err != nil {
		return err
	}

	// Linking the fully written and synced temporary file publishes pendingPath
	// atomically without overwriting a recovery artifact from an earlier crash.
	if err := os.Link(temporaryPath, pendingPath); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	_ = os.Remove(temporaryPath)

	if err := syncDirectory(filepath.Dir(pendingPath)); err != nil {
		cleanupErr := os.Remove(pendingPath)
		if cleanupErr != nil {
			return fmt.Errorf(
				"sync recovery backup: %w (backup retained at %s: %v)",
				err,
				pendingPath,
				cleanupErr,
			)
		}
		return fmt.Errorf("sync recovery backup: %w", err)
	}
	return nil
}

func publishCandidateWithBackup(
	targetPath string,
	candidatePath string,
	pendingPath string,
	previousPath string,
) error {
	directory := filepath.Dir(targetPath)

	// candidatePath and pendingPath are created below targetPath's parent
	// directory, so both renames are same-filesystem atomic operations.
	if err := os.Rename(candidatePath, targetPath); err != nil {
		cleanupErr := os.Remove(pendingPath)
		if cleanupErr != nil {
			return fmt.Errorf(
				"replace installed executable: %w (recovery backup retained at %s: %v)",
				err,
				pendingPath,
				cleanupErr,
			)
		}
		if syncErr := syncDirectory(directory); syncErr != nil {
			return fmt.Errorf(
				"replace installed executable: %w (cleanup sync failed: %v)",
				err,
				syncErr,
			)
		}
		return fmt.Errorf("replace installed executable: %w", err)
	}

	if err := os.Rename(pendingPath, previousPath); err != nil {
		recoveryErr := os.Rename(pendingPath, targetPath)
		if recoveryErr != nil {
			return fmt.Errorf(
				"replacement completed but its backup could not be published (%v) or restored; recovery backup remains at %s: %w",
				err,
				pendingPath,
				recoveryErr,
			)
		}
		if syncErr := syncDirectory(directory); syncErr != nil {
			return fmt.Errorf(
				"backup publish failed and the original executable was restored, but directory sync failed: %w",
				syncErr,
			)
		}
		return fmt.Errorf(
			"backup publish failed; the original executable was restored: %w",
			err,
		)
	}
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("replacement completed but directory sync failed: %w", err)
	}
	return nil
}

func acquireUpdateLock(targetPath string) (*updateLock, error) {
	lockPath := filepath.Join(
		filepath.Dir(targetPath),
		"."+filepath.Base(targetPath)+".update.lock",
	)
	descriptor, err := unix.Open(
		lockPath,
		unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return nil, fmt.Errorf("open update lock: %w", err)
	}
	file := os.NewFile(uintptr(descriptor), lockPath)
	if file == nil {
		_ = unix.Close(descriptor)
		return nil, fmt.Errorf("open update lock: invalid file descriptor")
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect update lock: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("update lock is not a regular file")
	}
	if err := unix.Flock(descriptor, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, fmt.Errorf("another self-update is already running")
		}
		return nil, fmt.Errorf("lock self-update: %w", err)
	}
	return &updateLock{file: file}, nil
}

func (lock *updateLock) close() {
	if lock == nil || lock.file == nil {
		return
	}
	_ = unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
	_ = lock.file.Close()
}

func regularFileInfo(path, description string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", description, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file", description)
	}
	if info.Size() <= 0 || info.Size() > maxExecutableSize {
		return nil, fmt.Errorf("%s has an invalid size", description)
	}
	return info, nil
}

func setFileIdentity(path string, sourceInfo os.FileInfo) error {
	if err := os.Chmod(path, sourceInfo.Mode().Perm()); err != nil {
		return err
	}

	sourceStat, ok := sourceInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot inspect installed executable ownership")
	}
	destinationInfo, err := os.Lstat(path)
	if err != nil {
		return err
	}
	destinationStat, ok := destinationInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot inspect staged executable ownership")
	}
	if destinationStat.Uid != sourceStat.Uid || destinationStat.Gid != sourceStat.Gid {
		if err := os.Chown(path, int(sourceStat.Uid), int(sourceStat.Gid)); err != nil {
			return err
		}
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func copyRegularFile(sourcePath, destinationPath string, sourceInfo os.FileInfo) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	openedInfo, err := source.Stat()
	if err != nil {
		return err
	}
	if !openedInfo.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file")
	}
	if !os.SameFile(sourceInfo, openedInfo) ||
		openedInfo.Size() != sourceInfo.Size() ||
		!openedInfo.ModTime().Equal(sourceInfo.ModTime()) {
		return fmt.Errorf("source changed while preparing the update")
	}

	destination, err := os.OpenFile(
		destinationPath,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o600,
	)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		_ = destination.Close()
		if !success {
			_ = os.Remove(destinationPath)
		}
	}()

	written, err := io.Copy(destination, io.LimitReader(source, maxExecutableSize+1))
	if err != nil {
		return err
	}
	if written != sourceInfo.Size() {
		return fmt.Errorf("source changed while preparing the update")
	}
	if err := destination.Sync(); err != nil {
		return err
	}
	if err := destination.Close(); err != nil {
		return err
	}
	if err := setFileIdentity(destinationPath, sourceInfo); err != nil {
		return err
	}
	success = true
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	err = directory.Sync()
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
		return nil
	}
	return err
}

func pendingBackupPath(executablePath string) string {
	return executablePath + ".update-pending"
}
