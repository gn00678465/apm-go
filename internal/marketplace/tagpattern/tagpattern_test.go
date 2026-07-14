package tagpattern

import (
	"testing"

	"github.com/apm-go/apm/internal/semver"
)

func TestExtractVersion(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		pkgName     string
		tagName     string
		wantVersion string
		wantOK      bool
	}{
		{"default pattern matches v-prefixed tag", "", "any", "v1.2.3", "1.2.3", true},
		{"empty pattern falls back to v{version}", "", "any", "1.2.3", "", false},
		{"explicit v{version} pattern", "v{version}", "any", "v2.0.0", "2.0.0", true},
		{"bare version pattern (no v prefix)", "{version}", "any", "1.2.3", "1.2.3", true},
		{"name-scoped pattern matches its own package", "{name}-v{version}", "tool-a", "tool-a-v1.0.0", "1.0.0", true},
		{"name-scoped pattern rejects another package's tag", "{name}-v{version}", "tool-a", "tool-b-v1.0.0", "", false},
		{"unrelated branch head never matches", "v{version}", "any", "main", "", false},
		{"pattern with dots is matched literally, not as wildcard", "release.{version}", "any", "releaseXv1.0.0", "", false},
		{"pattern with dots matches the literal dot", "release.{version}", "any", "release.1.0.0", "1.0.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			re := Compile(tt.pattern, tt.pkgName)

			// Act
			gotVersion, gotOK := ExtractVersion(re, tt.tagName)

			// Assert
			if gotOK != tt.wantOK {
				t.Fatalf("ExtractVersion(%q) ok = %v, want %v", tt.tagName, gotOK, tt.wantOK)
			}
			if gotOK && gotVersion != tt.wantVersion {
				t.Errorf("ExtractVersion(%q) version = %q, want %q", tt.tagName, gotVersion, tt.wantVersion)
			}
		})
	}
}

func TestFilterTags(t *testing.T) {
	// Arrange
	tags := []semver.TagInfo{
		{Name: "v1.0.0", Commit: "aaa"},
		{Name: "v1.1.0", Commit: "bbb"},
		{Name: "main", Commit: "ccc"},
		{Name: "unrelated-tag", Commit: "ddd"},
	}

	// Act
	got := FilterTags(tags, "v{version}", "any")

	// Assert
	want := []semver.TagInfo{
		{Name: "1.0.0", Commit: "aaa"},
		{Name: "1.1.0", Commit: "bbb"},
	}
	if len(got) != len(want) {
		t.Fatalf("FilterTags() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("FilterTags()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFilterTags_MonorepoNameScoping(t *testing.T) {
	// Arrange: a monorepo where two packages' tags interleave -- only the
	// requested package's tags must survive the filter.
	tags := []semver.TagInfo{
		{Name: "tool-a-v1.0.0", Commit: "aaa"},
		{Name: "tool-b-v2.0.0", Commit: "bbb"},
		{Name: "tool-a-v1.1.0", Commit: "ccc"},
	}

	// Act
	got := FilterTags(tags, "{name}-v{version}", "tool-a")

	// Assert
	want := []semver.TagInfo{
		{Name: "1.0.0", Commit: "aaa"},
		{Name: "1.1.0", Commit: "ccc"},
	}
	if len(got) != len(want) {
		t.Fatalf("FilterTags() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("FilterTags()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
