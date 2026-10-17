package lockfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"unicode/utf8"
)

// HashDeployedFile is the hash a lock records for a deployed file, the
// Oracle's compute_file_hash (utils/content_hash.py:89-153): content that is
// valid UTF-8 with no NUL byte is hashed with each CRLF replaced by LF, a bare
// CR kept, so a file git checked out with CRLF on Windows hashes as it does on
// Linux; other content is hashed as it is.
func HashDeployedFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("hash file %s: %w", path, err)
	}
	return HashBytes(CanonicalDeployedBytes(data)), nil
}

// CanonicalDeployedBytes is the content HashDeployedFile hashes.
func CanonicalDeployedBytes(b []byte) []byte {
	if bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b) {
		return b
	}
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
}

// DeployedFileMatches reports whether the file at path holds the content a
// lock recorded as recorded, for a cleanup that decides whether the file is
// still what was deployed. Besides HashDeployedFile's value it accepts the
// hash of the raw bytes: apm-go v0.4.0 recorded that, CRLF included, on
// Windows, and without it the first cleanup after the upgrade would keep
// those files as edited since deploy. The Oracle compares the normalized
// value alone (integration/cleanup.py:448-470); frozen install and audit do
// too (VerifyDeployedState).
func DeployedFileMatches(path, recorded string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("hash file %s: %w", path, err)
	}
	algo, want, err := ParseHashEnvelope(recorded)
	if err != nil || algo != "sha256" {
		return false, nil
	}
	for _, b := range [][]byte{CanonicalDeployedBytes(data), data} {
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) == want {
			return true, nil
		}
	}
	return false, nil
}
