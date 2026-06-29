package gitops

import (
	"testing"
)

func TestParseTagsOutput(t *testing.T) {
	output := `abc123def456	refs/tags/v1.0.0
def789abc012	refs/tags/v1.1.0
111222333444	refs/tags/v2.0.0-rc.1
`
	tags := parseTagsOutput(output)
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(tags))
	}
	if tags[0].Name != "v1.0.0" || tags[0].Commit != "abc123def456" {
		t.Errorf("tag 0: %+v", tags[0])
	}
	if tags[1].Name != "v1.1.0" {
		t.Errorf("tag 1: %+v", tags[1])
	}
	if tags[2].Name != "v2.0.0-rc.1" {
		t.Errorf("tag 2: %+v", tags[2])
	}
}

func TestParseTagsOutput_Empty(t *testing.T) {
	tags := parseTagsOutput("")
	if len(tags) != 0 {
		t.Errorf("expected 0 tags, got %d", len(tags))
	}
}
