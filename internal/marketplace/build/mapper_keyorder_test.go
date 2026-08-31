package build

import (
	"encoding/json"
	"regexp"
	"testing"
)

// TestClaudePlugin_KeyOrderMatchesOracle locks the plugins[] entry key
// order against the pinned Oracle's dict insertion order
// (marketplace/output_mappers.py:197-208): name, description, version,
// author, license, repository, tags, category, homepage, source.
//
// Ticket 28: apm-go declared Category third (right after Description),
// which is byte-visible in every produced marketplace.json for any package
// declaring BOTH a version and a category. The pre-existing golden fixture
// could not catch it -- its single entry has no version/author/license/
// repository/tags, so it renders identically under either ordering. This
// test populates every field precisely so the ordering cannot be ambiguous.
func TestClaudePlugin_KeyOrderMatchesOracle(t *testing.T) {
	// Arrange
	plugin := ClaudePlugin{
		Name:        "pkg-a",
		Description: "a package",
		Version:     "1.0.0",
		Author:      map[string]string{"name": "Someone"},
		License:     "MIT",
		Repository:  "https://github.com/acme/pkg-a",
		Tags:        []string{"cli"},
		Category:    "tools",
		Homepage:    "https://example.invalid",
		Source:      "./pkg-a",
	}

	// Act
	out, err := json.Marshal(plugin)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Assert
	want := []string{
		"name", "description", "version", "author",
		"license", "repository", "tags", "category", "homepage", "source",
	}
	got := topLevelKeysInOrder(t, out)
	if len(got) != len(want) {
		t.Fatalf("key count = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("key %d = %q, want %q\nfull order: %v\nwant:       %v", i, got[i], want[i], got, want)
		}
	}
}

// TestClaudePlugin_CategoryFollowsTags is the minimal, direct regression
// guard for the exact swap ticket 28 found: with only name, version,
// category and source set (the shape `pack -m all` produced live on both
// sides), the Oracle emits version BEFORE category.
func TestClaudePlugin_CategoryFollowsTags(t *testing.T) {
	// Arrange
	plugin := ClaudePlugin{
		Name:     "pkg-a",
		Version:  "1.0.0",
		Category: "tools",
		Source:   "./pkg-a",
	}

	// Act
	out, err := json.Marshal(plugin)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Assert
	want := []string{"name", "version", "category", "source"}
	got := topLevelKeysInOrder(t, out)
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("key order = %v, want %v", got, want)
		}
	}
}

var topLevelKeyRe = regexp.MustCompile(`"([a-z_]+)":`)

// topLevelKeysInOrder returns the object's keys in emitted byte order.
// encoding/json writes struct fields in declaration order, so the byte
// sequence IS the contract being asserted -- decoding into a map would
// discard exactly the property under test.
func topLevelKeysInOrder(t *testing.T, out []byte) []string {
	t.Helper()
	var keys []string
	depth := 0
	for i := 0; i < len(out); i++ {
		switch out[i] {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		case '"':
			if depth != 1 {
				continue
			}
			m := topLevelKeyRe.FindSubmatch(out[i:])
			if m == nil || !hasPrefixAt(out, i, m[0]) {
				continue
			}
			keys = append(keys, string(m[1]))
			i += len(m[0]) - 1
		}
	}
	return keys
}

func hasPrefixAt(b []byte, i int, prefix []byte) bool {
	if i+len(prefix) > len(b) {
		return false
	}
	return string(b[i:i+len(prefix)]) == string(prefix)
}
