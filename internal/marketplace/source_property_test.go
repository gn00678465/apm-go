package marketplace

import (
	"math/rand"
	"strings"
	"testing"
	"testing/quick"
)

// encodeTimes percent-encodes marker k times the way a hostile client would
// layer encodings: "." -> "%2e" on the first round, then every "%" -> "%25".
func encodeTimes(marker string, k int) string {
	s := marker
	for i := 0; i < k; i++ {
		s = strings.ReplaceAll(s, "%", "%25")
		s = strings.ReplaceAll(s, ".", "%2e")
		s = strings.ReplaceAll(s, "~", "%7e")
	}
	return s
}

// Property: a traversal marker (".", "..", "~") is rejected at every
// encoding depth the decoder unwinds (0..7), and a plain alnum segment is
// accepted at every depth (encoding an alnum segment is the identity).
func TestSegmentTraversalRejectedAtAnyDepthProperty(t *testing.T) {
	markers := []string{".", "..", "~"}
	f := func(mi, depth uint8, a, b byte) bool {
		k := int(depth) % 8
		hostile := encodeTimes(markers[int(mi)%len(markers)], k)
		if err := validateSourcePathSegment(hostile, "ctx"); err == nil {
			return false
		}
		benign := string([]byte{'a' + a%26, '0' + b%10})
		return validateSourcePathSegment(encodeTimes(benign, k), "ctx") == nil
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 400, Rand: rand.New(rand.NewSource(3))}); err != nil {
		t.Fatal(err)
	}
}
