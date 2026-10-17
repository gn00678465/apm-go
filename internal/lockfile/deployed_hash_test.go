package lockfile

import (
	"os"
	"path/filepath"
	"testing"
)

// The expected values are the Oracle's compute_file_hash of each content
// (utils/content_hash.py:124-153), and sha256sum of the raw bytes for the
// raw column.
var deployedHashCases = []struct {
	name    string
	content string
	want    string
	raw     string
}{
	{"CRLF text", "---\r\nname: a\r\n---\r\nbody\r\n",
		"sha256:99e1d00854eea2452373644477db75086d5cb42ab2dd93f32d6a6ace71341c01",
		"sha256:c537f896b4102e35580f510f7e2c205a9c00a79cf5ab475c697898edc59f5f42"},
	{"LF text", "---\nname: a\n---\nbody\n",
		"sha256:99e1d00854eea2452373644477db75086d5cb42ab2dd93f32d6a6ace71341c01",
		"sha256:99e1d00854eea2452373644477db75086d5cb42ab2dd93f32d6a6ace71341c01"},
	{"bare CR kept", "a\rb\r\nc\n",
		"sha256:0252f0b9ed9f1a78a16cc53f77522edcc06346c533d15afabc22afd913c8c7ae",
		"sha256:f9888915267df32c4e1aabdbc65e9e910c6247d99633a9bcd194d14107ced5e1"},
	{"NUL byte hashed raw", "a\r\n\x00b\r\n",
		"sha256:b4a86374856c8e7309304c3e55bb516bdc30c036fd4cbc555004907e116d4694",
		"sha256:b4a86374856c8e7309304c3e55bb516bdc30c036fd4cbc555004907e116d4694"},
	{"not UTF-8 hashed raw", "caf\xe9\r\nx\r\n",
		"sha256:d566b13b9e32ed754300d07cc98270a7f241c978b610bf150cbc855f36244724",
		"sha256:d566b13b9e32ed754300d07cc98270a7f241c978b610bf150cbc855f36244724"},
}

func TestHashDeployedFile_MatchesOracleComputeFileHash(t *testing.T) {
	for _, tc := range deployedHashCases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "f.md")
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := HashDeployedFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("HashDeployedFile = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestDeployedFileMatches_AcceptsNormalizedOrRawRecord(t *testing.T) {
	for _, tc := range deployedHashCases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "f.md")
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			for _, recorded := range []string{tc.want, tc.raw, tc.raw[len("sha256:"):]} {
				ok, err := DeployedFileMatches(path, recorded)
				if err != nil || !ok {
					t.Errorf("DeployedFileMatches(%s) = %v, %v; want true", recorded, ok, err)
				}
			}
			other := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
			if ok, err := DeployedFileMatches(path, other); err != nil || ok {
				t.Errorf("DeployedFileMatches(other) = %v, %v; want false", ok, err)
			}
			if ok, _ := DeployedFileMatches(path, "sha512:"+tc.raw[len("sha256:"):]); ok {
				t.Error("a sha512 record matched a sha256 digest")
			}
		})
	}
}

func TestVerifyDeployedState_AcceptsOnlyTheNormalizedHash(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("x\r\ny\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// sha256sum of "x\ny\n" and of "x\r\ny\r\n".
	normalized := "sha256:09834d488008f5f1ef589a2d7cedc52425bee9dd23b2212e4c1d673c5cbb54e4"
	raw := "sha256:6adc129c2038f41c45d1a27f913c4e7b7d97c46efa2f1d18f170478c15f9cbf4"
	if v := VerifyDeployedState(&Lockfile{LocalDeployedHashes: map[string]string{"a.md": normalized}}, root); len(v) != 0 {
		t.Errorf("normalized record: violations %+v, want none", v)
	}
	v := VerifyDeployedState(&Lockfile{LocalDeployedHashes: map[string]string{"a.md": raw}}, root)
	if len(v) != 1 || v[0].Observed != normalized {
		t.Errorf("raw record: violations %+v, want one observing %s", v, normalized)
	}
}
