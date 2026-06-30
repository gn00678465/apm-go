package lockfile

import (
	"path/filepath"
	"strings"
)

// Violation is a content-integrity failure: a deployed file whose on-disk bytes
// no longer hash to the value recorded in the lockfile (req-sc-001 / req-lk-017).
type Violation struct {
	Path     string
	Expected string
	Observed string
}

// VerifyDeployedState re-hashes every deployed file recorded in the lockfile
// (each entry's deployed_file_hashes plus the self-entry local_deployed_file_hashes)
// against disk under root and returns ALL content-integrity violations
// (req-sc-001). Unlike VerifyDeployedHashes (which fails closed on the first
// mismatch for frozen install), this collects the full set for `apm audit`.
// A missing or unreadable file is reported as a violation with an empty observed.
func VerifyDeployedState(lock *Lockfile, root string) []Violation {
	var viol []Violation
	check := func(hashes map[string]string) {
		for path, expected := range hashes {
			if strings.HasSuffix(path, "/") {
				continue
			}
			full := filepath.Join(root, filepath.FromSlash(path))
			actual, err := HashFileBytes(full)
			if err != nil {
				viol = append(viol, Violation{Path: path, Expected: expected, Observed: ""})
				continue
			}
			_, expHex, perr := ParseHashEnvelope(expected)
			_, actHex, _ := ParseHashEnvelope(actual)
			if perr != nil || expHex != actHex {
				viol = append(viol, Violation{Path: path, Expected: expected, Observed: actual})
			}
		}
	}
	for i := range lock.Dependencies {
		check(lock.Dependencies[i].DeployedHashes)
	}
	check(lock.LocalDeployedHashes)
	return viol
}
