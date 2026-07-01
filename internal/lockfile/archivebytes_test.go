package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestVerifyArchiveBytes(t *testing.T) {
	data := []byte("hello-archive")
	sum := sha256.Sum256(data)
	good := "sha256:" + hex.EncodeToString(sum[:])

	if err := VerifyArchiveBytes(data, good); err != nil {
		t.Errorf("matching hash should pass: %v", err)
	}
	// mismatch fails closed
	if err := VerifyArchiveBytes([]byte("tampered"), good); err == nil {
		t.Errorf("mismatch must fail")
	}
	// non-sha256 envelope fails closed even if hex matches
	if err := VerifyArchiveBytes(data, "sha512:"+hex.EncodeToString(sum[:])); err == nil {
		t.Errorf("non-sha256 algorithm must fail closed")
	} else if !strings.Contains(err.Error(), "unsupported algorithm") {
		t.Errorf("wrong error: %v", err)
	}
}
