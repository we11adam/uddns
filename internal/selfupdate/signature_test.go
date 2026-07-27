package selfupdate

import (
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"aead.dev/minisign"
)

func TestParseTrustedPublicKeys(t *testing.T) {
	first, _, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate first key: %v", err)
	}
	second, _, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate second key: %v", err)
	}

	keys, err := parseTrustedPublicKeys([]string{
		" " + first.String() + " ",
		second.String(),
	})
	if err != nil {
		t.Fatalf("parseTrustedPublicKeys returned error: %v", err)
	}
	if len(keys) != 2 || !keys[0].Equal(first) || !keys[1].Equal(second) {
		t.Fatal("parsed keys do not match input")
	}

	for _, values := range [][]string{{""}, {"not-a-minisign-key"}} {
		if _, err := parseTrustedPublicKeys(values); err == nil {
			t.Fatalf("parseTrustedPublicKeys(%q) returned nil error", values)
		}
	}
}

func TestVerifyChecksumSignature(t *testing.T) {
	publicKey, privateKey, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	otherPublicKey, _, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate other signing key: %v", err)
	}

	checksums := []byte("0123456789abcdef  release.tar.gz\n")
	signature := minisign.Sign(privateKey, checksums)
	tests := []struct {
		name      string
		checksums []byte
		signature []byte
		keys      []minisign.PublicKey
		wantErr   bool
	}{
		{
			name:      "valid",
			checksums: checksums,
			signature: signature,
			keys:      []minisign.PublicKey{publicKey},
		},
		{
			name:      "valid after key rotation",
			checksums: checksums,
			signature: signature,
			keys:      []minisign.PublicKey{otherPublicKey, publicKey},
		},
		{
			name:      "tampered checksums",
			checksums: append(append([]byte(nil), checksums...), 'x'),
			signature: signature,
			keys:      []minisign.PublicKey{publicKey},
			wantErr:   true,
		},
		{
			name:      "malformed signature",
			checksums: checksums,
			signature: []byte("not a minisign signature"),
			keys:      []minisign.PublicKey{publicKey},
			wantErr:   true,
		},
		{
			name:      "wrong key",
			checksums: checksums,
			signature: signature,
			keys:      []minisign.PublicKey{otherPublicKey},
			wantErr:   true,
		},
		{
			name:      "no trusted keys",
			checksums: checksums,
			signature: signature,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directory := t.TempDir()
			checksumPath := filepath.Join(directory, "checksums.txt")
			signaturePath := filepath.Join(directory, checksumSignatureName)
			if err := os.WriteFile(checksumPath, tt.checksums, 0o600); err != nil {
				t.Fatalf("write checksums: %v", err)
			}
			if err := os.WriteFile(signaturePath, tt.signature, 0o600); err != nil {
				t.Fatalf("write signature: %v", err)
			}

			err := verifyChecksumSignature(checksumPath, signaturePath, tt.keys)
			if (err != nil) != tt.wantErr {
				t.Fatalf("verifyChecksumSignature error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDownloadCandidateVerifiesSignatureBeforeArchiveDownload(t *testing.T) {
	publicKey, privateKey, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	const assetName = "release.tar.gz"
	checksums := []byte(strings.Repeat("0", 64) + "  " + assetName + "\n")
	signature := minisign.Sign(privateKey, []byte("different checksums"))
	var archiveRequests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		switch request.URL.Path {
		case "/checksums.txt":
			_, _ = writer.Write(checksums)
		case "/" + checksumSignatureName:
			_, _ = writer.Write(signature)
		case "/" + assetName:
			archiveRequests.Add(1)
			_, _ = io.WriteString(writer, "archive")
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	updater := downloadTestUpdater(server.Client())
	updater.trustedPublicKeys = []minisign.PublicKey{publicKey}
	_, err = updater.downloadCandidate(context.Background(), Plan{
		AssetName:    assetName,
		assetURL:     server.URL + "/" + assetName,
		checksumURL:  server.URL + "/checksums.txt",
		signatureURL: server.URL + "/" + checksumSignatureName,
	}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "signature verification failed") {
		t.Fatalf("downloadCandidate error = %v, want signature verification failure", err)
	}
	if got := archiveRequests.Load(); got != 0 {
		t.Fatalf("archive requests = %d, want none before signature verification", got)
	}
}

func TestNewRejectsInvalidTrustedPublicKey(t *testing.T) {
	_, err := New(Config{
		CurrentVersion:    "1.8.0",
		ExecutablePath:    filepath.Join(t.TempDir(), "uddns"),
		GOOS:              "linux",
		GOARCH:            "amd64",
		TrustedPublicKeys: []string{"not-a-minisign-key"},
	})
	if err == nil || !strings.Contains(err.Error(), "trusted release public key") {
		t.Fatalf("New error = %v, want invalid trusted key error", err)
	}
}

func TestEmbeddedReleasePublicKeys(t *testing.T) {
	original := releasePublicKeys
	t.Cleanup(func() {
		releasePublicKeys = original
	})
	releasePublicKeys = "first,second"
	got := embeddedReleasePublicKeys()
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("embeddedReleasePublicKeys = %q, want [first second]", got)
	}
}
