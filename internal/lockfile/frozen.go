package lockfile

import (
	"fmt"
	"os"
	"strings"

	"github.com/apm-go/apm/internal/manifest"
)

// IsCIEnvironment checks if CI env var is truthy per req-lk-018.
func IsCIEnvironment() bool {
	val, ok := os.LookupEnv("CI")
	if !ok {
		return false
	}
	return IsTruthyCI(val)
}

// IsTruthyCI returns true if the CI env var value is truthy.
// Truthy = present AND NOT any of: "", "0", "false" (case-insensitive).
func IsTruthyCI(val string) bool {
	if val == "" || val == "0" || strings.EqualFold(val, "false") {
		return false
	}
	return true
}

// CheckFrozenInstall verifies all direct deps have lockfile pins (req-lk-006).
func CheckFrozenInstall(m *manifest.Manifest, lock *Lockfile) error {
	if lock == nil {
		return fmt.Errorf("frozen install requires a lockfile but none was found")
	}
	for _, dep := range m.ParsedDeps {
		key := dep.RepoURL
		if dep.VirtualPath != "" {
			key += "/" + dep.VirtualPath
		}
		if dep.IsLocal {
			key = dep.LocalPath
		}
		if lock.FindByKey(key) == nil {
			return fmt.Errorf("frozen install: direct dependency %q has no pin in lockfile", key)
		}
	}
	return nil
}
