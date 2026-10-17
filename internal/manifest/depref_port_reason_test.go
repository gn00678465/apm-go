package manifest

import (
	"strings"
	"testing"
)

// Issue #122 item 83: the reason a git URL or a shorthand with a port is
// refused for is the Oracle's (measured with Oracle 0.33.0, card evidence
// r15/). The Oracle tests the virtual package path before it splits host and
// port (models/dependency/reference.py:1845 before :1454), so a last segment
// with an extension gives the virtual path's reason (reference.py:1270-1305);
// any other bad port gives identity.py:51-62.
func TestParseDep_PortAndFileURLReasons(t *testing.T) {
	const (
		port    = "Invalid shorthand port. Expected an integer from 1 to 65535"
		virtual = ". Individual files must end with one of: .prompt.md, .instructions.md, .agent.md. For subdirectory packages, the path should not have a file extension."
	)
	// The reason ends the error; what comes before it is apm-go's own.
	hasReason := func(err error, reason string) bool {
		return err != nil && (err.Error() == reason || strings.HasSuffix(err.Error(), ": "+reason))
	}
	for _, tt := range []struct {
		value, reason string
	}{
		{"file:///home/madao/x/b.git", "Invalid virtual package path 'madao/x/b.git'" + virtual},
		{"file://host/o/b.git", "Invalid virtual package path 'o/b.git'" + virtual},
		{"file:///srv/b", port},
		{"file:///home/madao/x/b", port},
		{"example.com:0/o/r", port},
		{"example.com:99999/o/r", port},
		{"example.com:abc/o/r", port},
	} {
		t.Run(tt.value, func(t *testing.T) {
			if _, err := ParseDepString(tt.value); !hasReason(err, tt.reason) {
				t.Errorf("ParseDepString(%q) = %v, want the reason %q", tt.value, err, tt.reason)
			}
			entry := buildMappingNode(map[string]string{"git": tt.value})
			if _, err := ParseDepDict(entry, 0); !hasReason(err, tt.reason) {
				t.Errorf("ParseDepDict(git: %q) = %v, want the reason %q", tt.value, err, tt.reason)
			}
		})
	}
}
