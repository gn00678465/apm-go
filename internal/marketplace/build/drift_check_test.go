package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/apm-go/apm/internal/marketplace/authoring"
)

func TestJsonKeyDiff_ScalarChange(t *testing.T) {
	old := map[string]any{"owner": map[string]any{"name": "Old"}}
	new_ := map[string]any{"owner": map[string]any{"name": "New"}}

	diffs := jsonKeyDiff(old, new_, "")
	if len(diffs) != 1 || diffs[0].Path != "owner.name" || diffs[0].Old != "Old" || diffs[0].New != "New" {
		t.Fatalf("diffs = %+v, want a single owner.name change", diffs)
	}
}

func TestJsonKeyDiff_AddedAndRemovedKeys(t *testing.T) {
	old := map[string]any{"a": 1.0}
	new_ := map[string]any{"b": 2.0}

	diffs := jsonKeyDiff(old, new_, "")
	if len(diffs) != 2 {
		t.Fatalf("diffs = %+v, want 2 entries (a removed, b added)", diffs)
	}
	byPath := map[string]DriftDifference{}
	for _, d := range diffs {
		byPath[d.Path] = d
	}
	if byPath["a"].New != nil || byPath["a"].Old != 1.0 {
		t.Errorf("a diff = %+v, want Old=1.0 New=nil", byPath["a"])
	}
	if byPath["b"].Old != nil || byPath["b"].New != 2.0 {
		t.Errorf("b diff = %+v, want Old=nil New=2.0", byPath["b"])
	}
}

func TestJsonKeyDiff_NoDifference(t *testing.T) {
	v := map[string]any{"name": "same", "plugins": []any{map[string]any{"name": "a"}}}
	diffs := jsonKeyDiff(v, v, "")
	if len(diffs) != 0 {
		t.Errorf("diffs = %+v, want none for identical values", diffs)
	}
}

func TestJsonKeyDiff_ListIndexMismatch(t *testing.T) {
	old := map[string]any{"plugins": []any{"a", "b"}}
	new_ := map[string]any{"plugins": []any{"a"}}

	diffs := jsonKeyDiff(old, new_, "")
	if len(diffs) != 1 || diffs[0].Path != "plugins[1]" || diffs[0].New != nil {
		t.Fatalf("diffs = %+v, want plugins[1] removed", diffs)
	}
}

func writeMarketplaceDriftFixture(t *testing.T, root string, onDisk any) {
	t.Helper()
	dir := filepath.Join(root, ".claude-plugin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(onDisk, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "marketplace.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func driftFixtureConfig() *authoring.AuthoringConfig {
	return &authoring.AuthoringConfig{
		Name:    "demo",
		Version: "1.0.0",
		Owner:   authoring.Owner{Name: "Acme"},
		Outputs: []string{"claude"},
	}
}

func TestCheckMarketplaceDrift_Unchanged(t *testing.T) {
	root := t.TempDir()
	cfg := driftFixtureConfig()
	doc, _, err := ComposeDocument("claude", cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeMarketplaceDriftFixture(t, root, doc)

	report, err := CheckMarketplaceDrift(cfg, nil, root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK {
		t.Fatalf("report.OK = false, want true; outputs=%+v", report.Outputs)
	}
	if len(report.Outputs) != 1 || report.Outputs[0].Status != "unchanged" {
		t.Errorf("outputs = %+v, want a single unchanged claude output", report.Outputs)
	}
}

func TestCheckMarketplaceDrift_Missing(t *testing.T) {
	root := t.TempDir()
	cfg := driftFixtureConfig()

	report, err := CheckMarketplaceDrift(cfg, nil, root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatal("report.OK = true, want false (no file on disk at all)")
	}
	if report.Outputs[0].Status != "missing" {
		t.Errorf("status = %q, want %q", report.Outputs[0].Status, "missing")
	}
	msgs := report.ErrorMessages()
	if len(msgs) != 1 || msgs[0] != ".claude-plugin/marketplace.json: missing on disk (would be created)" {
		t.Errorf("ErrorMessages() = %v", msgs)
	}
}

func TestCheckMarketplaceDrift_Drift(t *testing.T) {
	root := t.TempDir()
	cfg := driftFixtureConfig()
	writeMarketplaceDriftFixture(t, root, map[string]any{
		"name":    "demo",
		"owner":   map[string]any{"name": "Stale Owner"},
		"plugins": []any{},
	})

	report, err := CheckMarketplaceDrift(cfg, nil, root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatal("report.OK = true, want false")
	}
	out := report.Outputs[0]
	if out.Status != "drift" {
		t.Fatalf("status = %q, want %q", out.Status, "drift")
	}
	found := false
	for _, d := range out.Differences {
		if d.Path == "owner.name" && d.Old == "Stale Owner" && d.New == "Acme" {
			found = true
		}
	}
	if !found {
		t.Errorf("differences = %+v, want an owner.name Stale Owner -> Acme entry", out.Differences)
	}
}

func TestRenderDiffLines_TruncatesAndCountsExtra(t *testing.T) {
	report := DriftOutputReport{
		Differences: []DriftDifference{
			{Path: "a", Old: "1", New: "2"},
			{Path: "b", Old: "1", New: "2"},
			{Path: "c", Old: "1", New: "2"},
		},
	}
	lines := RenderDiffLines(report, 2)
	if len(lines) != 3 {
		t.Fatalf("lines = %v, want 2 rendered + 1 summary", lines)
	}
	if lines[2] != "  ... and 1 more differences" {
		t.Errorf("summary line = %q", lines[2])
	}
}

func TestJsonCompactASCII_EscapesNonASCII(t *testing.T) {
	got := jsonCompactASCII("café")
	want := "\"caf\\u00e9\""
	if got != want {
		t.Errorf("jsonCompactASCII = %q, want %q (non-ASCII escaped, matching Python's ensure_ascii=True)", got, want)
	}
}
