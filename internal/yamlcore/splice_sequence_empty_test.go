package yamlcore

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestReplaceSequenceWithEmptyFlow_ReplacesAllElements_LeavesSiblingsByteExact(t *testing.T) {
	src := []byte(apmManifestFixture)
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	out, ok, err := ReplaceSequenceWithEmptyFlow(src, doc, []string{"dependencies", "apm"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "acme/foo") || strings.Contains(outStr, "acme/bar") {
		t.Errorf("dependencies.apm elements should be gone:\n%s", outStr)
	}
	if !strings.Contains(outStr, "apm: []") {
		t.Errorf("expected an inline empty 'apm: []', got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "conflict_resolution: first-wins") {
		t.Errorf("sibling dependencies.conflict_resolution should survive:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/dev-only") {
		t.Errorf("devDependencies.apm (a later root sibling) should survive untouched:\n%s", outStr)
	}
	if !strings.Contains(outStr, "test: echo hi") {
		t.Errorf("scripts (a later root sibling) should survive untouched:\n%s", outStr)
	}

	reparsed, err := SafeLoad(out)
	if err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
	root := reparsed.Content[0]
	deps := findMappingChildForTest(t, root, "dependencies")
	apmSeq := findMappingChildForTest(t, deps, "apm")
	if apmSeq.Kind != yaml.SequenceNode || len(apmSeq.Content) != 0 {
		t.Errorf("expected dependencies.apm to re-parse as an empty sequence, got kind=%v len=%d", apmSeq.Kind, len(apmSeq.Content))
	}
}

func TestReplaceSequenceWithEmptyFlow_MissingPath_ReturnsOkFalse(t *testing.T) {
	src := []byte(apmManifestFixture)
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	out, ok, err := ReplaceSequenceWithEmptyFlow(src, doc, []string{"noSuchKey", "apm"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for a missing path")
	}
	if out != nil {
		t.Errorf("expected nil out on ok=false, got %q", out)
	}
}

func TestReplaceSequenceWithEmptyFlow_CRLFDocument_KeepsCRLFThroughout(t *testing.T) {
	src := []byte(strings.ReplaceAll(apmManifestFixture, "\n", "\r\n"))
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	out, ok, err := ReplaceSequenceWithEmptyFlow(src, doc, []string{"dependencies", "apm"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	stripped := strings.ReplaceAll(string(out), "\r\n", "")
	if strings.Contains(stripped, "\n") {
		t.Errorf("expected every line ending to be CRLF, found a bare LF:\n%q", out)
	}
	if _, err := SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}
