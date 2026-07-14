package semver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type oracleFile struct {
	RangeMatch       []rangeMatchCase       `json:"range_match"`
	TagSelection     []tagSelectionCase     `json:"tag_selection"`
	BuildMetadataTie []buildMetadataTieCase `json:"build_metadata_tie"`
}

type rangeMatchCase struct {
	Range   string `json:"range"`
	Version string `json:"version"`
	Match   bool   `json:"match"`
	Note    string `json:"note,omitempty"`
}

type tagSelectionCase struct {
	Range    string   `json:"range"`
	Tags     []string `json:"tags"`
	Selected string   `json:"selected"`
	Note     string   `json:"note,omitempty"`
}

type buildMetadataTieCase struct {
	Tags     []string `json:"tags"`
	Selected string   `json:"selected"`
	Note     string   `json:"note,omitempty"`
}

func loadOracle(t *testing.T) oracleFile {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(wd, "..", "..")
	candidates := []string{
		filepath.Join(root, "conformance-kit", "oracle", "resolution", "semver-dialect.json"),
		filepath.Join(root, "..", "conformance-kit", "oracle", "resolution", "semver-dialect.json"),
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var o oracleFile
		if err := json.Unmarshal(data, &o); err != nil {
			t.Fatalf("parse oracle: %v", err)
		}
		return o
	}
	t.Skip("semver-dialect.json oracle not found")
	return oracleFile{}
}

func TestOracle_RangeMatch(t *testing.T) {
	oracle := loadOracle(t)
	for i, tc := range oracle.RangeMatch {
		t.Run(tc.Version, func(t *testing.T) {
			got, err := Satisfies(tc.Version, tc.Range)
			if err != nil {
				t.Fatalf("case %d: Satisfies(%q, %q) error: %v", i+1, tc.Version, tc.Range, err)
			}
			if got != tc.Match {
				t.Errorf("case %d: Satisfies(%q, %q) = %v, want %v (note: %s)",
					i+1, tc.Version, tc.Range, got, tc.Match, tc.Note)
			}
		})
	}
}

func TestOracle_TagSelection(t *testing.T) {
	oracle := loadOracle(t)
	for i, tc := range oracle.TagSelection {
		t.Run(tc.Selected, func(t *testing.T) {
			tags := make([]TagInfo, len(tc.Tags))
			for j, tag := range tc.Tags {
				tags[j] = TagInfo{Name: tag, Commit: "deadbeef"}
			}
			winner, found, err := MaxSatisfying(tags, tc.Range)
			if err != nil {
				t.Fatalf("case %d: MaxSatisfying error: %v", i+1, err)
			}
			if !found {
				t.Fatalf("case %d: no match found, expected %s", i+1, tc.Selected)
			}
			if winner.Name != tc.Selected {
				t.Errorf("case %d: MaxSatisfying = %q, want %q (note: %s)",
					i+1, winner.Name, tc.Selected, tc.Note)
			}
		})
	}
}

func TestOracle_BuildMetadataTie(t *testing.T) {
	oracle := loadOracle(t)
	for i, tc := range oracle.BuildMetadataTie {
		t.Run(tc.Selected, func(t *testing.T) {
			tags := make([]TagInfo, len(tc.Tags))
			for j, tag := range tc.Tags {
				tags[j] = TagInfo{Name: tag, Commit: "deadbeef"}
			}
			// build-metadata versions are equal under semver precedence,
			// so use wildcard range to match all
			winner, found, err := MaxSatisfying(tags, "*")
			if err != nil {
				t.Fatalf("case %d: MaxSatisfying error: %v", i+1, err)
			}
			if !found {
				t.Fatalf("case %d: no match found", i+1)
			}
			if winner.Name != tc.Selected {
				t.Errorf("case %d: MaxSatisfying = %q, want %q (note: %s)",
					i+1, winner.Name, tc.Selected, tc.Note)
			}
		})
	}
}

func TestIsSemverRange(t *testing.T) {
	tests := []struct {
		ref  string
		want bool
	}{
		{"^1.2.3", true},
		{"~1.2.0", true},
		{">=1.0.0 <2.0.0", true},
		{"*", true},
		{"^1 || ^2", true},
		{"1.2.3 - 1.5.0", true},

		// Bare versions are literal refs, not ranges (req-rs-003)
		{"1.2.3", false},
		{"0.2.3", false},

		// Branch names containing 'x' must not be misclassified (S-005)
		{"next", false},
		{"fix-proxy", false},
		{"apex", false},

		{"main", false},
		{"develop", false},
		{"abc123def", false},
		{"", false},
		{"refs/heads/main", false},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			if got := IsSemverRange(tt.ref); got != tt.want {
				t.Errorf("IsSemverRange(%q) = %v, want %v", tt.ref, got, tt.want)
			}
		})
	}
}

func TestIsPrerelease(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"1.2.3", false},
		{"v1.2.3", false},
		{"1.2.3-beta.1", true},
		{"v1.2.3-beta.1", true},
		{"1.2.3+build.5", false},
		{"not-a-version", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := IsPrerelease(tt.version); got != tt.want {
				t.Errorf("IsPrerelease(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int // -1, 0, 1
	}{
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.0.0", "1.0.0", 0},
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"1.0.0-alpha", "1.0.0", -1},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			got := CompareVersions(tt.a, tt.b)
			if (tt.want < 0 && got >= 0) || (tt.want > 0 && got <= 0) || (tt.want == 0 && got != 0) {
				t.Errorf("CompareVersions(%q, %q) = %d, want sign %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
