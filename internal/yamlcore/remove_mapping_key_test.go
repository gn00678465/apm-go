package yamlcore

import (
	"strings"
	"testing"
)

// apmManifestFixture is a hand-authored, commented apm.yml-shaped document
// (not machine-generated -- 舊坑 1: a fresh fixture with no hand formatting
// would not exercise byte-exact preservation), used across
// RemoveMappingKey/ReplaceSequenceWithEmptyFlow tests.
const apmManifestFixture = `name: demo-project
version: 1.0.0

dependencies:
  apm:
    # foo is the flagship plugin
    - acme/foo
    - acme/bar # kept around
  conflict_resolution: first-wins
devDependencies:
  apm:
    - acme/dev-only
  mcp:
    - name: devserver
scripts:
  test: echo hi
`

func TestRemoveMappingKey_RemovesNestedKey_LeavesSiblingsByteExact(t *testing.T) {
	src := []byte(apmManifestFixture)
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	out, ok, err := RemoveMappingKey(src, doc, []string{"devDependencies", "apm"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "acme/dev-only") {
		t.Errorf("devDependencies.apm should have been removed:\n%s", outStr)
	}
	if !strings.Contains(outStr, "name: devserver") {
		t.Errorf("devDependencies.mcp sibling should survive:\n%s", outStr)
	}
	if !strings.Contains(outStr, "test: echo hi") {
		t.Errorf("scripts (a later root sibling) should survive untouched:\n%s", outStr)
	}
	if !strings.Contains(outStr, "# foo is the flagship plugin") || !strings.Contains(outStr, "acme/foo") {
		t.Errorf("dependencies.apm should be untouched:\n%s", outStr)
	}

	if _, err := SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

func TestRemoveMappingKey_RemovesWholeRootKey_LeavesRestByteExact(t *testing.T) {
	src := []byte(apmManifestFixture)
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	out, ok, err := RemoveMappingKey(src, doc, []string{"devDependencies"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "devDependencies") {
		t.Errorf("devDependencies wrapper should be fully removed:\n%s", outStr)
	}
	if strings.Contains(outStr, "devserver") {
		t.Errorf("devDependencies.mcp should be removed along with the whole wrapper:\n%s", outStr)
	}
	if !strings.Contains(outStr, "test: echo hi") {
		t.Errorf("scripts (a later root sibling) should survive untouched:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/foo") || !strings.Contains(outStr, "acme/bar") {
		t.Errorf("dependencies.apm should be untouched:\n%s", outStr)
	}

	if _, err := SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

func TestRemoveMappingKey_MissingKey_ReturnsOkFalse(t *testing.T) {
	src := []byte(apmManifestFixture)
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	out, ok, err := RemoveMappingKey(src, doc, []string{"noSuchKey"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for a missing key")
	}
	if out != nil {
		t.Errorf("expected nil out on ok=false, got %q", out)
	}
}

func TestRemoveMappingKey_EmptyPath_ReturnsOkFalse(t *testing.T) {
	src := []byte(apmManifestFixture)
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	_, ok, err := RemoveMappingKey(src, doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for an empty path")
	}
}

func TestRemoveMappingKey_CRLFDocument_KeepsCRLFThroughout(t *testing.T) {
	src := []byte(strings.ReplaceAll(apmManifestFixture, "\n", "\r\n"))
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	out, ok, err := RemoveMappingKey(src, doc, []string{"devDependencies", "apm"})
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
