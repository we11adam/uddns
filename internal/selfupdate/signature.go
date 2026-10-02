package selfupdate

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"aead.dev/minisign"
)

const checksumSignatureName = "checksums.txt.minisig"

// releasePublicKeys is injected into release builds as a comma-separated list.
// Keeping the trust roots in the binary prevents release metadata or assets from
// replacing them during an update.
var releasePublicKeys string

func embeddedReleasePublicKeys() []string {
	if strings.TrimSpace(releasePublicKeys) == "" {
		return nil
	}
	return strings.Split(releasePublicKeys, ",")
}

func parseTrustedPublicKeys(values []string) ([]minisign.PublicKey, error) {
	keys := make([]minisign.PublicKey, 0, len(values))
	for index, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("trusted release public key %d is empty", index+1)
		}

		var key minisign.PublicKey
		if err := key.UnmarshalText([]byte(value)); err != nil {
			return nil, fmt.Errorf(
				"trusted release public key %d is invalid: %w",
				index+1,
				err,
			)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func verifyChecksumSignature(
	checksumPath string,
	signaturePath string,
	trustedPublicKeys []minisign.PublicKey,
) error {
	if len(trustedPublicKeys) == 0 {
		return errors.New("no trusted release signing keys configured")
	}

	checksums, err := os.ReadFile(checksumPath)
	if err != nil {
		return fmt.Errorf("read checksums for signature verification: %w", err)
	}
	if len(checksums) > maxChecksumSize {
		return errors.New("checksums exceed the size limit")
	}
	signature, err := os.ReadFile(signaturePath)
	if err != nil {
		return fmt.Errorf("read checksum signature: %w", err)
	}
	if len(signature) > maxSignatureSize {
		return errors.New("checksum signature exceeds the size limit")
	}

	for _, publicKey := range trustedPublicKeys {
		if minisign.Verify(publicKey, checksums, signature) {
			return nil
		}
	}
	return errors.New("checksum signature verification failed")
}
