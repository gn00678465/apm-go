package lockfile

import (
	"os"
	"strings"
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
