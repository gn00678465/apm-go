package tagpattern

import (
	"strings"
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

// TestFilterTags_PatternWithoutVersionPlaceholder_DropsEverything documents
// the fail-silent mode that Validate exists to prevent: a pattern with no
// "{version}" placeholder compiles into a regex with no "version" capture
// group, so ExtractVersion's SubexpIndex lookup returns -1 and every tag is
// reported as non-matching. The user-visible symptom is "no matching version"
// -- pointing at the tags rather than at the malformed pattern.
func TestFilterTags_PatternWithoutVersionPlaceholder_DropsEverything(t *testing.T) {
	tags := []semver.TagInfo{
		{Name: "v1.0.0", Commit: "aaa"},
		{Name: "v2.3.4", Commit: "bbb"},
		{Name: "pkg-v2.3.4", Commit: "ccc"},
	}
	got := FilterTags(tags, "{name}", "pkg")
	if len(got) != 0 {
		t.Fatalf("precondition changed: expected the broken pattern to drop every tag, got %v", got)
	}
	re := Compile("{name}", "pkg")
	if idx := re.SubexpIndex("version"); idx != -1 {
		t.Errorf("SubexpIndex(\"version\") = %d, want -1 (no capture group is what makes this silent)", idx)
	}
}

// TestValidate mirrors upstream v0.27.0 marketplace/tag_pattern.py's
// validate_tag_pattern, rule for rule:
//   - blank (or whitespace-only) is rejected
//   - any "{...}" token other than {version}/{name} is rejected
//   - "{version}" must appear exactly once (zero and two both rejected)
//   - the accepted value is returned strip()'d
func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    string
		wantErr string // substring that must appear; "" means expect success
	}{
		{"plain default", "v{version}", "v{version}", ""},
		{"bare version", "{version}", "{version}", ""},
		{"monorepo form", "{name}-v{version}", "{name}-v{version}", ""},
		{"name after version", "{version}-{name}", "{version}-{name}", ""},
		{"surrounding space normalized", "  v{version}\t", "v{version}", ""},
		{"empty", "", "", "non-empty"},
		{"whitespace only", "   ", "", "non-empty"},
		{"no version placeholder", "{name}", "", "exactly one"},
		{"no placeholder at all", "release", "", "exactly one"},
		{"two version placeholders", "v{version}-{version}", "", "exactly one"},
		{"unsupported placeholder", "{foo}-v{version}", "", "unsupported placeholder"},
		{"empty braces", "{}v{version}", "", "unsupported placeholder"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Validate(tt.pattern, "build.tagPattern")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate(%q) unexpected error: %v", tt.pattern, err)
				}
				if got != tt.want {
					t.Errorf("Validate(%q) = %q, want %q", tt.pattern, got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate(%q) = %q, want error containing %q", tt.pattern, got, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate(%q) error = %q, want it to contain %q", tt.pattern, err.Error(), tt.wantErr)
			}
			if !strings.Contains(err.Error(), "build.tagPattern") {
				t.Errorf("Validate(%q) error = %q, must name the context so the user knows which key is wrong", tt.pattern, err.Error())
			}
		})
	}
}
