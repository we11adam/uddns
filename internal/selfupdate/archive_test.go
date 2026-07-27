package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

type archiveTestMember struct {
	name     string
	body     []byte
	typeflag byte
	linkname string
	mode     os.FileMode
}

func TestExtractExecutableAcceptsReleaseArchives(t *testing.T) {
	t.Parallel()

	const executable = "uddns"
	want := []byte("#!/bin/sh\nexit 0\n")
	tests := []struct {
		name        string
		archiveName string
		build       func(*testing.T, string)
	}{
		{
			name:        "tar gzip",
			archiveName: "uddns_1.9.0_linux_amd64.tar.gz",
			build: func(t *testing.T, path string) {
				writeTarGzipTestArchive(t, path, []archiveTestMember{{
					name:     executable,
					body:     want,
					typeflag: tar.TypeReg,
					mode:     0o755,
				}})
			},
		},
		{
			name:        "zip",
			archiveName: "uddns_1.9.0_darwin_amd64.zip",
			build: func(t *testing.T, path string) {
				writeZipTestArchive(t, path, []archiveTestMember{{
					name: executable,
					body: want,
					mode: 0o755,
				}})
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			directory := t.TempDir()
			archivePath := filepath.Join(directory, "release")
			destination := filepath.Join(directory, "candidate")
			tt.build(t, archivePath)

			if err := extractExecutable(
				archivePath,
				tt.archiveName,
				executable,
				destination,
			); err != nil {
				t.Fatalf("extractExecutable returned error: %v", err)
			}

			got, err := os.ReadFile(destination)
			if err != nil {
				t.Fatalf("read extracted executable: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("extracted bytes = %q, want %q", got, want)
			}
			info, err := os.Stat(destination)
			if err != nil {
				t.Fatalf("stat extracted executable: %v", err)
			}
			if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
				t.Errorf("extracted mode = %o, want %o", got, want)
			}
		})
	}
}

func TestExtractExecutableRejectsUnsafeOrMalformedArchives(t *testing.T) {
	t.Parallel()

	const executable = "uddns"
	regular := archiveTestMember{
		name:     executable,
		body:     []byte("candidate"),
		typeflag: tar.TypeReg,
		mode:     0o755,
	}
	tests := []struct {
		name        string
		archiveName string
		build       func(*testing.T, string)
	}{
		{
			name:        "tar wrong executable name",
			archiveName: "release.tar.gz",
			build: func(t *testing.T, path string) {
				member := regular
				member.name = "nested/uddns"
				writeTarGzipTestArchive(t, path, []archiveTestMember{member})
			},
		},
		{
			name:        "zip wrong executable name",
			archiveName: "release.zip",
			build: func(t *testing.T, path string) {
				member := regular
				member.name = "uddns.exe"
				writeZipTestArchive(t, path, []archiveTestMember{member})
			},
		},
		{
			name:        "tar has multiple members",
			archiveName: "release.tar.gz",
			build: func(t *testing.T, path string) {
				extra := regular
				extra.name = "README.md"
				writeTarGzipTestArchive(t, path, []archiveTestMember{regular, extra})
			},
		},
		{
			name:        "zip has multiple members",
			archiveName: "release.zip",
			build: func(t *testing.T, path string) {
				extra := regular
				extra.name = "README.md"
				writeZipTestArchive(t, path, []archiveTestMember{regular, extra})
			},
		},
		{
			name:        "tar directory",
			archiveName: "release.tar.gz",
			build: func(t *testing.T, path string) {
				writeTarGzipTestArchive(t, path, []archiveTestMember{{
					name:     executable,
					typeflag: tar.TypeDir,
					mode:     os.ModeDir | 0o755,
				}})
			},
		},
		{
			name:        "zip directory",
			archiveName: "release.zip",
			build: func(t *testing.T, path string) {
				writeZipTestArchive(t, path, []archiveTestMember{{
					name: executable,
					mode: os.ModeDir | 0o755,
				}})
			},
		},
		{
			name:        "tar symbolic link",
			archiveName: "release.tar.gz",
			build: func(t *testing.T, path string) {
				writeTarGzipTestArchive(t, path, []archiveTestMember{{
					name:     executable,
					typeflag: tar.TypeSymlink,
					linkname: "/tmp/payload",
					mode:     os.ModeSymlink | 0o777,
				}})
			},
		},
		{
			name:        "zip symbolic link",
			archiveName: "release.zip",
			build: func(t *testing.T, path string) {
				writeZipTestArchive(t, path, []archiveTestMember{{
					name: executable,
					body: []byte("/tmp/payload"),
					mode: os.ModeSymlink | 0o777,
				}})
			},
		},
		{
			name:        "tar hard link",
			archiveName: "release.tar.gz",
			build: func(t *testing.T, path string) {
				writeTarGzipTestArchive(t, path, []archiveTestMember{{
					name:     executable,
					typeflag: tar.TypeLink,
					linkname: "payload",
					mode:     0o755,
				}})
			},
		},
		{
			name:        "tar empty executable",
			archiveName: "release.tar.gz",
			build: func(t *testing.T, path string) {
				member := regular
				member.body = nil
				writeTarGzipTestArchive(t, path, []archiveTestMember{member})
			},
		},
		{
			name:        "zip empty executable",
			archiveName: "release.zip",
			build: func(t *testing.T, path string) {
				member := regular
				member.body = nil
				writeZipTestArchive(t, path, []archiveTestMember{member})
			},
		},
		{
			name:        "truncated tar gzip",
			archiveName: "release.tar.gz",
			build: func(t *testing.T, path string) {
				member := regular
				member.body = randomArchiveTestBytes(t, 32<<10)
				writeTarGzipTestArchive(t, path, []archiveTestMember{member})
				truncateArchiveTestFile(t, path, 2)
			},
		},
		{
			name:        "concatenated gzip stream",
			archiveName: "release.tar.gz",
			build: func(t *testing.T, path string) {
				writeTarGzipTestArchive(t, path, []archiveTestMember{regular})
				appendGzipArchiveTestStream(t, path, []byte("trailing payload"))
			},
		},
		{
			name:        "truncated zip",
			archiveName: "release.zip",
			build: func(t *testing.T, path string) {
				writeZipTestArchive(t, path, []archiveTestMember{regular})
				truncateArchiveTestFile(t, path, 10)
			},
		},
		{
			name:        "unsupported extension",
			archiveName: "release.tgz",
			build: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("not an archive"), 0o600); err != nil {
					t.Fatalf("write unsupported archive: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			directory := t.TempDir()
			archivePath := filepath.Join(directory, "release")
			destination := filepath.Join(directory, "candidate")
			tt.build(t, archivePath)

			if err := extractExecutable(
				archivePath,
				tt.archiveName,
				executable,
				destination,
			); err == nil {
				t.Fatal("extractExecutable succeeded, want error")
			}
			if _, err := os.Lstat(destination); !os.IsNotExist(err) {
				t.Fatalf("failed extraction left destination behind: %v", err)
			}
		})
	}
}

func appendGzipArchiveTestStream(t *testing.T, path string, body []byte) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatalf("open archive for append: %v", err)
	}
	writer := gzip.NewWriter(file)
	if _, err := writer.Write(body); err != nil {
		t.Fatalf("append gzip stream: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close appended gzip stream: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close appended archive: %v", err)
	}
}

func TestWriteExecutableRejectsSizeMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		body         string
		expectedSize int64
	}{
		{name: "source is shorter", body: "short", expectedSize: 6},
		{name: "source is longer", body: "too long", expectedSize: 7},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			destination := filepath.Join(t.TempDir(), "candidate")
			if err := writeExecutable(
				destination,
				bytes.NewBufferString(tt.body),
				tt.expectedSize,
			); err == nil {
				t.Fatal("writeExecutable succeeded, want size mismatch")
			}
			if _, err := os.Lstat(destination); !os.IsNotExist(err) {
				t.Fatalf("failed write left destination behind: %v", err)
			}
		})
	}
}

func writeTarGzipTestArchive(
	t *testing.T,
	path string,
	members []archiveTestMember,
) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create tar.gz fixture: %v", err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)

	for _, member := range members {
		typeflag := member.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		header := &tar.Header{
			Name:     member.name,
			Mode:     int64(member.mode.Perm()),
			Size:     int64(len(member.body)),
			Typeflag: typeflag,
			Linkname: member.linkname,
		}
		if typeflag != tar.TypeReg {
			header.Size = 0
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if len(member.body) > 0 && header.Size > 0 {
			if _, err := tarWriter.Write(member.body); err != nil {
				t.Fatalf("write tar member: %v", err)
			}
		}
	}

	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar fixture: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip fixture: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close tar.gz fixture: %v", err)
	}
}

func writeZipTestArchive(t *testing.T, path string, members []archiveTestMember) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip fixture: %v", err)
	}
	zipWriter := zip.NewWriter(file)
	for _, member := range members {
		header := &zip.FileHeader{
			Name:   member.name,
			Method: zip.Store,
		}
		header.SetMode(member.mode)
		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			t.Fatalf("create zip member: %v", err)
		}
		if _, err := writer.Write(member.body); err != nil {
			t.Fatalf("write zip member: %v", err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip fixture: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}
}

func randomArchiveTestBytes(t *testing.T, size int) []byte {
	t.Helper()

	content := make([]byte, size)
	if _, err := rand.Read(content); err != nil {
		t.Fatalf("generate archive fixture: %v", err)
	}
	return content
}

func truncateArchiveTestFile(t *testing.T, path string, removeBytes int64) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat archive fixture: %v", err)
	}
	if info.Size() <= removeBytes {
		t.Fatal("archive fixture is too small to truncate")
	}
	if err := os.Truncate(path, info.Size()-removeBytes); err != nil {
		t.Fatalf("truncate archive fixture: %v", err)
	}
}
