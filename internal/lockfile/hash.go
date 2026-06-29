package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// HashEnvelope formats a digest as "<algo>:<hex>" per req-lk-016.
func HashEnvelope(algo string, hexStr string) string {
	return algo + ":" + hexStr
}

// ParseHashEnvelope parses "<algo>:<hex>" or bare 64-char hex (sha256 assumed).
func ParseHashEnvelope(s string) (algo, hexStr string, err error) {
	if idx := strings.IndexByte(s, ':'); idx > 0 {
		algo = s[:idx]
		hexStr = s[idx+1:]
		switch algo {
		case "sha256", "sha384", "sha512":
			return algo, hexStr, nil
		default:
			return "", "", fmt.Errorf("unsupported hash algorithm %q", algo)
		}
	}
	if len(s) == 64 {
		return "sha256", s, nil
	}
	return "", "", fmt.Errorf("invalid hash envelope %q", s)
}

// HashFileBytes computes SHA-256 of file contents and returns envelope string.
func HashFileBytes(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("hash file %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash file %s: %w", path, err)
	}
	return HashEnvelope("sha256", hex.EncodeToString(h.Sum(nil))), nil
}

// ComputeDeployedFileHashes hashes all deployed files relative to rootDir (req-lk-012).
// Directories (paths ending with "/") are skipped — they have no hash.
func ComputeDeployedFileHashes(files []string, rootDir string) (map[string]string, error) {
	result := make(map[string]string, len(files))
	for _, f := range files {
		if strings.HasSuffix(f, "/") {
			continue
		}
		full := filepath.Join(rootDir, filepath.FromSlash(f))
		h, err := HashFileBytes(full)
		if err != nil {
			return nil, err
		}
		result[f] = h
	}
	return result, nil
}

// VerifyDeployedHashes re-checks deployed_file_hashes against disk bytes (req-lk-017).
func VerifyDeployedHashes(hashes map[string]string, rootDir string) error {
	for path, expected := range hashes {
		if strings.HasSuffix(path, "/") {
			continue
		}
		algo, expectedHex, err := ParseHashEnvelope(expected)
		if err != nil {
			return fmt.Errorf("integrity check failed for %s: invalid expected hash: %w", path, err)
		}
		if algo != "sha256" {
			return fmt.Errorf("integrity check failed for %s: unsupported algorithm %q (only sha256 supported)", path, algo)
		}
		full := filepath.Join(rootDir, filepath.FromSlash(path))
		actual, err := HashFileBytes(full)
		if err != nil {
			return fmt.Errorf("integrity check failed for %s: %w", path, err)
		}
		_, actualHex, _ := ParseHashEnvelope(actual)
		if expectedHex != actualHex {
			return fmt.Errorf("integrity violation: %s expected %s, observed %s", path, expected, actual)
		}
	}
	return nil
}

// VerifyArchiveHash checks registry archive SHA-256 before extraction (req-lk-013).
func VerifyArchiveHash(archivePath string, expectedHash string) error {
	algo, expectedHex, err := ParseHashEnvelope(expectedHash)
	if err != nil {
		return fmt.Errorf("archive hash verify: invalid expected hash: %w", err)
	}
	if algo != "sha256" {
		return fmt.Errorf("archive hash verify: unsupported algorithm %q (only sha256 supported)", algo)
	}
	actual, err := HashFileBytes(archivePath)
	if err != nil {
		return fmt.Errorf("archive hash verify: %w", err)
	}
	_, actualHex, _ := ParseHashEnvelope(actual)
	if expectedHex != actualHex {
		return fmt.Errorf("archive integrity violation: %s expected %s, observed %s", archivePath, expectedHash, actual)
	}
	return nil
}
