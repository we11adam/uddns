package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const maxExecutableSize = 128 << 20

func extractExecutable(
	archivePath string,
	archiveName string,
	expectedExecutable string,
	destination string,
) error {
	switch {
	case strings.HasSuffix(archiveName, ".tar.gz"):
		return extractTarGzipExecutable(archivePath, expectedExecutable, destination)
	case strings.HasSuffix(archiveName, ".zip"):
		return extractZipExecutable(archivePath, expectedExecutable, destination)
	default:
		return fmt.Errorf("unsupported archive format")
	}
}

func extractTarGzipExecutable(archivePath, expectedExecutable, destination string) error {
	success := false
	defer func() {
		if !success {
			_ = os.Remove(destination)
		}
	}()

	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	bufferedFile := bufio.NewReader(file)
	gzipReader, err := gzip.NewReader(bufferedFile)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer gzipReader.Close()
	gzipReader.Multistream(false)

	tarReader := tar.NewReader(gzipReader)
	header, err := tarReader.Next()
	if err != nil {
		return fmt.Errorf("read tar member: %w", err)
	}
	if header.Name != expectedExecutable {
		return fmt.Errorf("archive must contain only %s", expectedExecutable)
	}
	if header.Typeflag != tar.TypeReg {
		return fmt.Errorf("archive executable must be a regular file")
	}
	if header.Size <= 0 || header.Size > maxExecutableSize {
		return fmt.Errorf("archive executable has an invalid size")
	}
	if err := writeExecutable(destination, tarReader, header.Size); err != nil {
		return err
	}
	if _, err := tarReader.Next(); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("archive must contain exactly one file")
		}
		return fmt.Errorf("read trailing tar member: %w", err)
	}
	if _, err := io.Copy(io.Discard, gzipReader); err != nil {
		return fmt.Errorf("validate gzip stream: %w", err)
	}
	if _, err := bufferedFile.Peek(1); err == nil {
		return fmt.Errorf("gzip archive contains trailing data")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("inspect trailing gzip data: %w", err)
	}
	success = true
	return nil
}

func extractZipExecutable(archivePath, expectedExecutable, destination string) error {
	success := false
	defer func() {
		if !success {
			_ = os.Remove(destination)
		}
	}()

	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip archive: %w", err)
	}
	defer reader.Close()

	if len(reader.File) != 1 {
		return fmt.Errorf("archive must contain exactly one file")
	}
	member := reader.File[0]
	if member.Name != expectedExecutable {
		return fmt.Errorf("archive must contain only %s", expectedExecutable)
	}
	if !member.Mode().IsRegular() {
		return fmt.Errorf("archive executable must be a regular file")
	}
	if member.UncompressedSize64 == 0 || member.UncompressedSize64 > maxExecutableSize {
		return fmt.Errorf("archive executable has an invalid size")
	}

	memberReader, err := member.Open()
	if err != nil {
		return fmt.Errorf("open zip executable: %w", err)
	}
	defer memberReader.Close()
	if err := writeExecutable(
		destination,
		memberReader,
		int64(member.UncompressedSize64),
	); err != nil {
		return err
	}
	success = true
	return nil
}

func writeExecutable(destination string, source io.Reader, expectedSize int64) error {
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

	written, err := io.Copy(file, io.LimitReader(source, maxExecutableSize+1))
	if err != nil {
		return fmt.Errorf("extract executable: %w", err)
	}
	if written != expectedSize {
		return fmt.Errorf("archive executable size mismatch")
	}
	if written > maxExecutableSize {
		return fmt.Errorf("archive executable exceeds the size limit")
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
