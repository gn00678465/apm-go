package yamlcore

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

// packagesFixture is a hand-authored (not machine-generated), commented
// apm.yml-shaped document with a block-style marketplace.packages sequence,
// used across most tests in this file per the task's "fixture must contain
// comments and manual formatting" requirement (舊坑 1: a fixture generated
// fresh, with no hand formatting, would not have caught the real
// PatchMappingPath byte-corruption bug from the earlier --mcp task).
//
// "packages" is deliberately NOT the last key of the document (a top-level
// "version:" key follows the marketplace: block) so the last element's span
// end is bounded by that sibling key, not swept all the way to EOF -- that
// EOF-sweep edge case (a document-tail comment/blank-line region getting
// folded into the very last element's span, the same "merge into the
// earlier span" rule applied with nothing after it) is covered separately
// and more narrowly by TestSpliceSequenceElement_Remove_MergesFollowingStandaloneCommentIntoRemovedSpan's
// own fixture.
const packagesFixture = `name: demo-marketplace
description: Example marketplace for splice tests

marketplace:
  owner:
    name: acme-org
    url: https://github.com/acme-org
  build:
    tagPattern: "v{version}"
  outputs:
    claude: {}
  packages:
    # foo is our flagship plugin
    - name: foo
      description: Flagship tool
      source: ./packages/foo
      tags: [cli, flagship]
    - name: bar # legacy compatibility shim
      description: Legacy shim
      source: ./packages/bar
      version: "^1.0.0"
    - name: baz
      description: Experimental package
      source: ./packages/baz
      ref: v0.1.0-experimental
version: 1.0.0
`

func mustLoadFixture(t *testing.T, src string) (*yaml.Node, *yaml.Node) {
	t.Helper()
	doc, err := SafeLoad([]byte(src))
	if err != nil {
		t.Fatalf("fixture does not parse: %v", err)
	}
	return doc, doc
}

func newPackageNode(pairs ...string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for i := 0; i+1 < len(pairs); i += 2 {
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: pairs[i], Tag: "!!str"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: pairs[i+1], Tag: "!!str"},
		)
	}
	return n
}

// TestSpliceSequenceElement_Add_AppendsAtTail_PreservesRestByteExact covers
// the `add` op: a new element is inserted at the end of the sequence's
// span (right before the top-level "version:" key that follows the
// marketplace: block), and every other byte of the document -- including
// the three existing packages' own comments and hand formatting -- must
// survive untouched.
func TestSpliceSequenceElement_Add_AppendsAtTail_PreservesRestByteExact(t *testing.T) {
	src := []byte(packagesFixture)
	doc, _ := mustLoadFixture(t, packagesFixture)

	newNode := newPackageNode("name", "qux", "description", "New package", "source", "./packages/qux")

	out, ok, err := SpliceSequenceElement(src, doc, []string{"marketplace", "packages"}, SeqAdd, -1, newNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for a block-style sequence")
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	const untouchedHead = `name: demo-marketplace
description: Example marketplace for splice tests

marketplace:
  owner:
    name: acme-org
    url: https://github.com/acme-org
  build:
    tagPattern: "v{version}"
  outputs:
    claude: {}
  packages:
    # foo is our flagship plugin
    - name: foo
      description: Flagship tool
      source: ./packages/foo
      tags: [cli, flagship]
    - name: bar # legacy compatibility shim
      description: Legacy shim
      source: ./packages/bar
      version: "^1.0.0"
    - name: baz
      description: Experimental package
      source: ./packages/baz
      ref: v0.1.0-experimental
`
	if !strings.HasPrefix(outStr, untouchedHead) {
		t.Errorf("bytes before the insertion point were altered; want prefix:\n%s\ngot:\n%s", untouchedHead, outStr)
	}
	if !strings.Contains(outStr, "name: qux") || !strings.Contains(outStr, "source: ./packages/qux") {
		t.Errorf("new package entry not found in output:\n%s", outStr)
	}
	const wantTail = `    - name: qux
      description: New package
      source: ./packages/qux
version: 1.0.0
`
	if !strings.HasSuffix(outStr, wantTail) {
		t.Errorf("new element was not inserted right before the sibling 'version:' key, or that key's bytes were altered; want suffix:\n%s\ngot:\n%s", wantTail, outStr)
	}

	reparsed, err := SafeLoad(out)
	if err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
	root := reparsed.Content[0]
	mkt := findMappingChildForTest(t, root, "marketplace")
	pkgs := findMappingChildForTest(t, mkt, "packages")
	if len(pkgs.Content) != 4 {
		t.Errorf("expected 4 packages after add, got %d", len(pkgs.Content))
	}
}

// TestSpliceSequenceElement_Remove_FirstElement_PreservesLeadingComment
// guards the spike-verified boundary rule: a comment that leads the first
// element of a sequence belongs to the sequence, not the element -- so
// removing element 0 must NOT remove that comment, only the element itself.
func TestSpliceSequenceElement_Remove_FirstElement_PreservesLeadingComment(t *testing.T) {
	src := []byte(packagesFixture)
	doc, _ := mustLoadFixture(t, packagesFixture)

	out, ok, err := SpliceSequenceElement(src, doc, []string{"marketplace", "packages"}, SeqRemove, 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for a block-style sequence")
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if !strings.Contains(outStr, "# foo is our flagship plugin") {
		t.Errorf("leading comment for the first element should be preserved (belongs to the sequence, not the element), got:\n%s", outStr)
	}
	if strings.Contains(outStr, "name: foo") {
		t.Errorf("removed element 'foo' is still present:\n%s", outStr)
	}
	if !strings.Contains(outStr, "name: bar # legacy compatibility shim") {
		t.Errorf("untouched element 'bar' (and its inline comment) was altered:\n%s", outStr)
	}
	if !strings.Contains(outStr, "name: baz") {
		t.Errorf("untouched element 'baz' was altered:\n%s", outStr)
	}

	reparsed, err := SafeLoad(out)
	if err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
	root := reparsed.Content[0]
	mkt := findMappingChildForTest(t, root, "marketplace")
	pkgs := findMappingChildForTest(t, mkt, "packages")
	if len(pkgs.Content) != 2 {
		t.Errorf("expected 2 packages after removing the first, got %d", len(pkgs.Content))
	}
}

// standaloneCommentBetweenElementsFixture is a dedicated, minimal fixture
// for the explicit "元素間獨立註解併入前一元素 span" test (implement.md
// step 5): a standalone comment line sits between elements 0 (foo) and 1
// (baz), and "packages" is followed by a sibling "build:" key so the last
// element's own span stays tightly bounded, keeping this fixture focused on
// just the one behavior under test.
const standaloneCommentBetweenElementsFixture = `marketplace:
  packages:
    - name: foo
      source: ./foo
    # baz is still experimental, keep pinned
    - name: baz
      source: ./baz
  build:
    tagPattern: "v{version}"
`

// TestSpliceSequenceElement_Remove_MergesFollowingStandaloneCommentIntoRemovedSpan
// is the explicit, spike-documented edge case: a standalone comment line
// between element i and element i+1 is byte-attached to element i's span
// (not element i+1's), so removing element i also removes that comment --
// even though a human reading the source would likely read the comment as
// describing element i+1. This mirrors ruamel's behavior and is accepted
// by design.md as a known, tested boundary (not a bug).
func TestSpliceSequenceElement_Remove_MergesFollowingStandaloneCommentIntoRemovedSpan(t *testing.T) {
	src := []byte(standaloneCommentBetweenElementsFixture)
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	// Remove "foo" (index 0). The standalone comment "# baz is still
	// experimental, keep pinned" sits between foo and baz in the source, so
	// it is part of foo's span and must disappear along with foo.
	out, ok, err := SpliceSequenceElement(src, doc, []string{"marketplace", "packages"}, SeqRemove, 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for a block-style sequence")
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "name: foo") {
		t.Errorf("removed element 'foo' is still present:\n%s", outStr)
	}
	if strings.Contains(outStr, "# baz is still experimental, keep pinned") {
		t.Errorf("standalone comment between the removed element and the next one should have been removed along with it, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "name: baz") {
		t.Errorf("element 'baz' itself must survive (only its preceding comment is lost):\n%s", outStr)
	}
	if !strings.Contains(outStr, "tagPattern:") {
		t.Errorf("sibling 'build:' key after packages was altered:\n%s", outStr)
	}

	if _, err := SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

// TestSpliceSequenceElement_Set_ReplacesOnlyTargetElement_LeavesOthersByteExact
// covers the `set` op: only the target element's byte span is replaced by
// the freshly rendered node; every other element (including its own
// comments) is untouched. It targets index 0 (foo) specifically to also
// confirm the same "belongs to the sequence, not the element" rule Remove
// exercises: foo's leading comment survives the replacement.
func TestSpliceSequenceElement_Set_ReplacesOnlyTargetElement_LeavesOthersByteExact(t *testing.T) {
	src := []byte(packagesFixture)
	doc, _ := mustLoadFixture(t, packagesFixture)

	newNode := newPackageNode("name", "quux", "description", "Replacement for foo", "source", "./packages/quux")

	out, ok, err := SpliceSequenceElement(src, doc, []string{"marketplace", "packages"}, SeqSet, 0, newNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for a block-style sequence")
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "name: foo") || strings.Contains(outStr, "Flagship tool") {
		t.Errorf("old element 'foo' should have been replaced, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "# foo is our flagship plugin") {
		t.Errorf("leading comment for the replaced element should be preserved (belongs to the sequence, not the element), got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "name: quux") || !strings.Contains(outStr, "source: ./packages/quux") {
		t.Errorf("new element 'quux' not found in output:\n%s", outStr)
	}
	if !strings.Contains(outStr, "name: bar # legacy compatibility shim") {
		t.Errorf("untouched element 'bar' (and its inline comment) was altered:\n%s", outStr)
	}
	if !strings.Contains(outStr, "name: baz") {
		t.Errorf("untouched element 'baz' was altered:\n%s", outStr)
	}

	reparsed, err := SafeLoad(out)
	if err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
	root := reparsed.Content[0]
	mkt := findMappingChildForTest(t, root, "marketplace")
	pkgs := findMappingChildForTest(t, mkt, "packages")
	if len(pkgs.Content) != 3 {
		t.Errorf("expected 3 packages after set, got %d", len(pkgs.Content))
	}
}

// TestSpliceSequenceElement_CRLFFixture_KeepsCRLFThroughout guards against
// the same class of bug PatchMappingPath's own CRLF test guards against: the
// yaml dumper always emits bare "\n", so a rendered fragment spliced
// verbatim into a CRLF-authored document must be normalized to "\r\n" or the
// result is a mixed-line-ending file.
func TestSpliceSequenceElement_CRLFFixture_KeepsCRLFThroughout(t *testing.T) {
	src := []byte("marketplace:\r\n" +
		"  packages:\r\n" +
		"    - name: foo\r\n" +
		"      source: ./foo\r\n" +
		"    - name: bar\r\n" +
		"      source: ./bar\r\n")
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	newNode := newPackageNode("name", "baz", "source", "./baz")
	out, ok, err := SpliceSequenceElement(src, doc, []string{"marketplace", "packages"}, SeqAdd, -1, newNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}

	if strings.Contains(strings.ReplaceAll(string(out), "\r\n", ""), "\n") {
		t.Errorf("output has a bare LF not paired with CR (mixed line endings):\n%q", out)
	}
	if !strings.HasPrefix(string(out), "marketplace:\r\n  packages:\r\n    - name: foo\r\n") {
		t.Errorf("untouched CRLF prefix was altered, got:\n%q", out)
	}
	if !strings.Contains(string(out), "name: baz") {
		t.Errorf("new element not found in output:\n%q", out)
	}
	if _, err := SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

// TestSpliceSequenceElement_Remove_LastElement_PreservesFollowingSiblingComment
// guards F1 (HIGH, data loss): removing the *last* element of a sequence
// must stop at the end of that element's own content -- it must NOT extend
// to the next sibling key's own line (spanEndOffset's rule, correct for a
// whole-value PatchMappingPath replace but wrong here), which would also
// sweep away any comment or blank line that leads that sibling key as if it
// were "trailing" bytes of the removed element.
func TestSpliceSequenceElement_Remove_LastElement_PreservesFollowingSiblingComment(t *testing.T) {
	src := []byte(`marketplace:
  packages:
    - name: only
      source: ./only
  # build settings
  build:
    tagPattern: "v{version}"
`)
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	out, ok, err := SpliceSequenceElement(src, doc, []string{"marketplace", "packages"}, SeqRemove, 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "name: only") {
		t.Errorf("removed element 'only' is still present:\n%s", outStr)
	}
	if !strings.Contains(outStr, "# build settings") {
		t.Errorf("sibling key's leading comment was incorrectly swept into the removed element's span:\n%s", outStr)
	}
	if !strings.Contains(outStr, "build:") || !strings.Contains(outStr, "tagPattern:") {
		t.Errorf("sibling 'build:' key was altered:\n%s", outStr)
	}

	if _, err := SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

// TestSpliceSequenceElement_Remove_LastElement_DashOnOwnLine_NoOrphanDash
// guards F2 (MEDIUM): when an element's leading "-" marker is written on
// its own line (rather than sharing the line with the first key), the
// element node's parsed Line points at the first *key* line, not the
// dash's own line. Removing the last such element must also remove its
// dash line -- otherwise an orphaned "-" is left immediately before the
// next sibling key, which yaml parses as a phantom trailing null sequence
// item instead of leaving a clean, one-shorter sequence.
func TestSpliceSequenceElement_Remove_LastElement_DashOnOwnLine_NoOrphanDash(t *testing.T) {
	src := []byte(`marketplace:
  packages:
    - name: foo
      source: ./foo
    -
      name: bar
      source: ./bar
  build:
    tagPattern: "v{version}"
`)
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	out, ok, err := SpliceSequenceElement(src, doc, []string{"marketplace", "packages"}, SeqRemove, 1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "name: bar") {
		t.Errorf("removed element 'bar' is still present:\n%s", outStr)
	}

	reparsed, err := SafeLoad(out)
	if err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
	root := reparsed.Content[0]
	mkt := findMappingChildForTest(t, root, "marketplace")
	pkgs := findMappingChildForTest(t, mkt, "packages")
	if len(pkgs.Content) != 1 {
		t.Fatalf("expected 1 package left, got %d (an orphan '-' produces a phantom null entry): %#v", len(pkgs.Content), pkgs.Content)
	}
	if pkgs.Content[0].Kind != yaml.MappingNode {
		t.Errorf("remaining package should still be a mapping, got Kind=%v (an orphan '-' parses as a null scalar)", pkgs.Content[0].Kind)
	}
}

// TestSpliceSequenceElement_Remove_FirstElement_DashOnOwnLineNextElement
// covers the non-last case of the same dash-line fix: removing an element
// that precedes one whose dash is written on its own line must stop the
// removed span exactly at that next element's dash line, not one line
// later (which would strand the next element's own content without its
// dash, corrupting it instead of the one actually being removed).
func TestSpliceSequenceElement_Remove_FirstElement_DashOnOwnLineNextElement(t *testing.T) {
	src := []byte(`marketplace:
  packages:
    - name: foo
      source: ./foo
    -
      name: bar
      source: ./bar
    - name: baz
      source: ./baz
`)
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	out, ok, err := SpliceSequenceElement(src, doc, []string{"marketplace", "packages"}, SeqRemove, 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "name: foo") {
		t.Errorf("removed element 'foo' is still present:\n%s", outStr)
	}

	reparsed, err := SafeLoad(out)
	if err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
	root := reparsed.Content[0]
	mkt := findMappingChildForTest(t, root, "marketplace")
	pkgs := findMappingChildForTest(t, mkt, "packages")
	if len(pkgs.Content) != 2 {
		t.Fatalf("expected 2 packages left, got %d: %#v", len(pkgs.Content), pkgs.Content)
	}
	for i, want := range []string{"bar", "baz"} {
		nameNode := findMappingChildForTest(t, pkgs.Content[i], "name")
		if nameNode.Value != want {
			t.Errorf("pkgs.Content[%d].name = %q, want %q", i, nameNode.Value, want)
		}
	}
}

// TestSpliceSequenceElement_Add_InsertsBeforeSiblingLeadingComment guards
// the `add` path against the same sibling-comment boundary bug F1 fixes for
// remove/set: a new element must be spliced in right after the last
// existing element's own content -- not after a comment that leads the
// next sibling key, which would leave the new element sandwiched between
// that comment and the key it actually describes.
func TestSpliceSequenceElement_Add_InsertsBeforeSiblingLeadingComment(t *testing.T) {
	src := []byte(`marketplace:
  packages:
    - name: only
      source: ./only
  # build settings
  build:
    tagPattern: "v{version}"
`)
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	newNode := newPackageNode("name", "second", "source", "./second")
	out, ok, err := SpliceSequenceElement(src, doc, []string{"marketplace", "packages"}, SeqAdd, -1, newNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	const wantOrder = "source: ./only\n    - name: second\n      source: ./second\n  # build settings\n  build:\n"
	if !strings.Contains(outStr, wantOrder) {
		t.Errorf("new element was not inserted right after the last existing element and before the sibling's leading comment; got:\n%s", outStr)
	}

	if _, err := SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

// TestSpliceSequenceElement_Remove_CRLFFixture_KeepsCRLFThroughout covers
// the `remove` op's own CRLF-normalization contract (until now only the
// `add` op had a CRLF fixture test), guarding the same class of bug: a
// splice must never leave a document with mixed line endings.
func TestSpliceSequenceElement_Remove_CRLFFixture_KeepsCRLFThroughout(t *testing.T) {
	src := []byte("marketplace:\r\n" +
		"  packages:\r\n" +
		"    - name: foo\r\n" +
		"      source: ./foo\r\n" +
		"    - name: bar\r\n" +
		"      source: ./bar\r\n")
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	out, ok, err := SpliceSequenceElement(src, doc, []string{"marketplace", "packages"}, SeqRemove, 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}

	if strings.Contains(strings.ReplaceAll(string(out), "\r\n", ""), "\n") {
		t.Errorf("output has a bare LF not paired with CR (mixed line endings):\n%q", out)
	}
	const want = "marketplace:\r\n  packages:\r\n    - name: bar\r\n      source: ./bar\r\n"
	if string(out) != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if _, err := SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

// TestSpliceSequenceElement_Set_CRLFFixture_KeepsCRLFThroughout covers the
// `set` op's own CRLF-normalization contract, same rationale as the
// `remove` CRLF test above.
func TestSpliceSequenceElement_Set_CRLFFixture_KeepsCRLFThroughout(t *testing.T) {
	src := []byte("marketplace:\r\n" +
		"  packages:\r\n" +
		"    - name: foo\r\n" +
		"      source: ./foo\r\n" +
		"    - name: bar\r\n" +
		"      source: ./bar\r\n")
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	newNode := newPackageNode("name", "baz", "source", "./baz")
	out, ok, err := SpliceSequenceElement(src, doc, []string{"marketplace", "packages"}, SeqSet, 1, newNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}

	if strings.Contains(strings.ReplaceAll(string(out), "\r\n", ""), "\n") {
		t.Errorf("output has a bare LF not paired with CR (mixed line endings):\n%q", out)
	}
	if !strings.HasPrefix(string(out), "marketplace:\r\n  packages:\r\n    - name: foo\r\n      source: ./foo\r\n") {
		t.Errorf("untouched CRLF prefix was altered, got:\n%q", out)
	}
	if strings.Contains(string(out), "name: bar") {
		t.Errorf("replaced element 'bar' is still present:\n%q", out)
	}
	if !strings.Contains(string(out), "name: baz") {
		t.Errorf("replacement element not found in output:\n%q", out)
	}
	if _, err := SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

// TestSpliceSequenceElement_FlowStyleSequence_ReturnsOkFalse covers the
// task's explicit "flow-style 回 ok=false" requirement: a
// marketplace.packages sequence written in flow style ([ {...}, {...} ])
// does not match the block-sequence shape SpliceSequenceElement's
// line/column-based span math depends on, so it must decline (ok=false,
// err=nil) rather than attempt a splice -- callers fall back to a
// whole-value replace (design.md's option 1), never a full-document
// re-encode.
func TestSpliceSequenceElement_FlowStyleSequence_ReturnsOkFalse(t *testing.T) {
	src := []byte(`marketplace:
  packages: [ { name: foo }, { name: bar } ]
`)
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}
	newNode := newPackageNode("name", "baz")

	for _, tc := range []struct {
		name string
		op   SeqOp
		idx  int
		node *yaml.Node
	}{
		{"add", SeqAdd, -1, newNode},
		{"remove", SeqRemove, 0, nil},
		{"set", SeqSet, 0, newNode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, ok, err := SpliceSequenceElement(src, doc, []string{"marketplace", "packages"}, tc.op, tc.idx, tc.node)
			if err != nil {
				t.Fatalf("expected no error for a flow-style fallback, got: %v", err)
			}
			if ok {
				t.Fatal("expected ok=false for a flow-style sequence")
			}
			if out != nil {
				t.Errorf("expected nil output when ok=false, got: %q", out)
			}
		})
	}
}

// TestSpliceSequenceElement_MissingPath_ReturnsOkFalse covers the general
// "path does not resolve the way SpliceSequenceElement requires" contract
// (missing key, non-mapping intermediate, or non-sequence final value) --
// mirroring PatchMappingPath's own ok=false-on-mismatch contract.
func TestSpliceSequenceElement_MissingPath_ReturnsOkFalse(t *testing.T) {
	src := []byte(`marketplace:
  owner:
    name: acme
`)
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		path []string
	}{
		{"missing key", []string{"marketplace", "packages"}},
		{"final value is a mapping, not a sequence", []string{"marketplace", "owner"}},
		{"intermediate value is not a mapping", []string{"marketplace", "owner", "name", "packages"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, ok, err := SpliceSequenceElement(src, doc, tc.path, SeqRemove, 0, nil)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if ok {
				t.Fatal("expected ok=false")
			}
			if out != nil {
				t.Errorf("expected nil output when ok=false, got: %q", out)
			}
		})
	}
}

// TestSpliceSequenceElement_RemoveIndexOutOfRange_ReturnsError covers
// caller misuse (as opposed to a structural fallback case): an out-of-range
// idx for remove/set is a programming error and must surface as a non-nil
// error, not a silent ok=false fallback.
func TestSpliceSequenceElement_RemoveIndexOutOfRange_ReturnsError(t *testing.T) {
	src := []byte(packagesFixture)
	doc, _ := mustLoadFixture(t, packagesFixture)

	if _, _, err := SpliceSequenceElement(src, doc, []string{"marketplace", "packages"}, SeqRemove, 99, nil); err == nil {
		t.Fatal("expected an error for an out-of-range remove index")
	}
	if _, _, err := SpliceSequenceElement(src, doc, []string{"marketplace", "packages"}, SeqSet, -1, newPackageNode("name", "x")); err == nil {
		t.Fatal("expected an error for a negative set index")
	}
}
