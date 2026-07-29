package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	yamllib "go.yaml.in/yaml/v4"
)

// TestErrNoDeployTarget_TeachesPluralTargetsKey is AC4 (R1.3): the
// no-deployment-target teaching error must recommend the plural targets:
// key, and must not print the singular target: example anymore.
func TestErrNoDeployTarget_TeachesPluralTargetsKey(t *testing.T) {
	msg := errNoDeployTarget().Error()

	if !strings.Contains(msg, "targets:") {
		t.Errorf("errNoDeployTarget() = %q, want it to mention 'targets:' (plural)", msg)
	}
	if regexp.MustCompile(`(?m)^\s*target:\s*$`).MatchString(msg) {
		t.Errorf("errNoDeployTarget() = %q, must not print the singular 'target:' example", msg)
	}
}

// TestInstallCmd_NoDeployTarget_PrintsPluralTargetsExample is the 2026-07-30
// codex Tier 2 M2 fix: AC4 was previously only verified (a) at the function
// level via TestErrNoDeployTarget_TeachesPluralTargetsKey, and (b) manually,
// once, via the prebuilt binary (verification-record.md). This drives the
// real `install` command end to end -- local .apm/ primitives present, no
// resolvable deploy target -- and asserts on the actual process-visible
// error and exit code cobra returns, not just errNoDeployTarget() in
// isolation.
func TestInstallCmd_NoDeployTarget_PrintsPluralTargetsExample(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	if err := os.MkdirAll(filepath.Join(".apm", "instructions"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".apm", "instructions", "demo.instructions.md"), []byte("# demo"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := installCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error: no deploy target is resolvable")
	}
	if exitCodeOf(err) != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", exitCodeOf(err))
	}
	msg := err.Error()
	if !strings.Contains(msg, "targets:") {
		t.Errorf("err = %q, want it to mention plural 'targets:'", msg)
	}
	if regexp.MustCompile(`(?m)^\s*target:\s*$`).MatchString(msg) {
		t.Errorf("err = %q, must not print the singular 'target:' example", msg)
	}
}

// TestInstall_LegacySingularTargetKey_StillDeploys is AC29 (R1.4/C4): an
// apm.yml written with only the singular target: key (pre-existing project,
// never touched by this task's init changes) must still resolve a deploy
// target and deploy local primitives end to end -- proving
// internal/manifest's dual-key parsing on the deploy path was not broken by
// this task's changes elsewhere.
func TestInstall_LegacySingularTargetKey_StillDeploys(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	if err := os.MkdirAll(filepath.Join(".apm", "instructions"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".apm", "instructions", "demo.instructions.md"), []byte("# demo"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ntarget:\n  - claude\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	// No --target flag: the deploy target must resolve from the manifest's
	// singular target: key alone.
	if err := runInstall(deps, false, true, "", nil, nil); err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".claude", "rules", "demo.md")); err != nil {
		t.Errorf("expected local instructions to deploy to .claude/rules/demo.md: %v", err)
	}
	if _, err := os.Stat("apm.lock.yaml"); err != nil {
		t.Errorf("expected apm.lock.yaml to be written: %v", err)
	}
}

// The five tests below close a granularity gap flagged in
// 07-29-targets-init-shape's implement task: readExistingTargets/
// lenientReadTargets (init.go:303-441) has five distinct early-return
// branches, but before this task only two of them (the "no target/targets
// key" shape, indirectly, and the CSV/alias/both-keys shapes) had direct
// test coverage. Per .trellis/spec/guides/loop-graph-engineering.md model 9
// (verification granularity must match claim granularity), each branch gets
// its own test naming the exact init.go lines it exercises, and each was
// confirmed to actually exercise that branch by a manual mutation pass
// (temporarily breaking the guarded condition and re-running -run for that
// test alone to see it go red) before being left in this form.

// TestReadExistingTargets_SafeLoadFailure_ReturnsNil is init.go:328-331: a
// syntactically invalid apm.yml (yamlcore.SafeLoad itself errors, before
// manifest.ParseManifest or lenientReadTargets ever run) must not panic or
// leak a partial selection -- it returns nil for the whole read.
func TestReadExistingTargets_SafeLoadFailure_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Unterminated flow sequence -- confirmed via a direct yamlcore.SafeLoad
	// probe to fail with "did not find expected ',' or ']'" rather than
	// silently coercing to something parseable.
	os.WriteFile("apm.yml", []byte("targets: [claude\n"), 0644)
	targets := readExistingTargets()
	if targets != nil {
		t.Errorf("got %v, want nil (SafeLoad failure must short-circuit before any target extraction)", targets)
	}
}

// TestReadExistingTargets_RootNotMapping_ReturnsNil is init.go:385-387
// (lenientReadTargets's root.Kind != MappingNode guard) reached through the
// real readExistingTargets pipeline: a document whose root is a sequence,
// not a mapping, parses fine at the YAML level (yamlcore.SafeLoad succeeds)
// but manifest.ParseManifest rejects it at manifest.go:84-86 ("top-level
// must be a YAML mapping"), which routes into lenientReadTargets with that
// same non-mapping root.
//
// The fixture is deliberately "- targets\n- claude\n", not an innocuous
// "- claude\n- copilot\n": lenientReadTargets' key/value scan
// (init.go:390-397) walks root.Content in (key, value) pairs by raw index
// with no Kind check of its own -- it relies entirely on the
// root.Kind != MappingNode guard to keep a SequenceNode from ever reaching
// that loop. A plain "- claude\n- copilot\n" fixture would still return nil
// even with the guard deleted, because "claude"/"copilot" never match the
// "target"/"targets" case labels -- that would make the test pass for the
// wrong reason (mutation-checked: confirmed by temporarily deleting the
// guard and re-running only this test, which still went green against that
// fixture). "- targets\n- claude\n" instead lines up so index 0
// ("targets") would be read as the key and index 1 ("claude") as the value
// if the guard were skipped, which would wrongly preselect "claude" -- so
// this fixture actually distinguishes "guard present" from "guard absent"
// (mutation-checked: deleting the guard against this fixture flips the
// test to red, expected [] got [claude]).
func TestReadExistingTargets_RootNotMapping_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("- targets\n- claude\n"), 0644)
	targets := readExistingTargets()
	if targets != nil {
		t.Errorf("got %v, want nil (a sequence document has no target/targets key to read, regardless of what its elements look like)", targets)
	}
}

// TestReadExistingTargets_NoTargetKey_ReturnsNil is init.go:407-409
// (lenientReadTargets's val == nil guard, reached after both targetVal and
// targetsVal come back nil from the key scan): a mapping document with
// neither target: nor targets: present, and with version: omitted so
// manifest.ParseManifest fails (mf-003, manifest.go:204) and falls through
// to lenientReadTargets in the first place.
func TestReadExistingTargets_NoTargetKey_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: p\n"), 0644)
	targets := readExistingTargets()
	if targets != nil {
		t.Errorf("got %v, want nil (no target/targets key present)", targets)
	}
}

// TestReadExistingTargets_BlankTokenAfterTrim_IsSkippedNotKept targets
// init.go:431-434 (the tok == "" after strings.TrimSpace continue): a
// targets: sequence containing one real entry and one blank-string entry
// must not surface the blank as a preselected target.
//
// Mutation finding (claim-evidence-guide.md: reporting this honestly rather
// than as an unqualified "this test pins that line"): deleting this guard
// does NOT flip this test red. manifest.ValidateTarget (target.go:61-75)
// already rejects "" itself ("unknown target \"\""), so the following
// `if err != nil { continue }` (init.go:436-438) drops a blank token
// through the exact same manifest.ValidateTarget path this file's
// TestReadExistingTargets_NoTargetKey_ReturnsNil-adjacent tests already
// exercise -- confirmed by temporarily disabling the tok == "" check and
// re-running this test alone, which still passed. So init.go:431-434 is a
// (harmless) early-exit ahead of an equivalent, already-covered rejection,
// not a distinct behavior this black-box test can isolate; what this test
// actually pins is the externally-observable contract -- a blank targets:
// entry is dropped, not preselected -- regardless of which of the two
// guards is doing the dropping.
func TestReadExistingTargets_BlankTokenAfterTrim_IsSkippedNotKept(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: p\ntargets:\n  - claude\n  - \"\"\n"), 0644)
	targets := readExistingTargets()
	if len(targets) != 1 || targets[0] != "claude" {
		t.Errorf("got %v, want [claude] (blank token dropped, not kept as an empty entry)", targets)
	}
}

// TestLenientReadTargets_NonDocumentGuard_DefenseInDepth is init.go:381-383
// (doc.Kind != yamllib.DocumentNode || len(doc.Content) == 0).
//
// Reachability finding (claim-evidence-guide.md: this is a claim about code,
// so it needs file:line + a concrete check, not an adjective): this branch
// is NOT reachable through the real readExistingTargets() call path today.
// Evidence:
//  1. readExistingTargets always passes lenientReadTargets the exact node
//     yamlcore.SafeLoad returned (init.go:328,335) -- never a hand-built or
//     independently-decoded node.
//  2. A direct probe of yamlcore.SafeLoad across "" , "   \n", "---\n",
//     "null\n", "~\n", "# comment only\n", a bare sequence, and a bare
//     mapping showed every successful decode (err == nil) produces
//     doc.Kind == yamllib.DocumentNode with len(doc.Content) == 1; every
//     input that would violate that shape (empty input, whitespace-only,
//     comment-only) instead makes the decode step itself return an error,
//     which readExistingTargets already short-circuits on at init.go:329-331
//     before lenientReadTargets is ever called.
//  3. manifest.ParseManifest has the byte-for-byte identical guard at
//     manifest.go:78 ("if doc.Kind != yaml.DocumentNode || len(doc.Content)
//     == 0"), checked before lenientReadTargets's copy of it would ever run
//     -- so even if some future SafeLoad change produced such a node, the
//     manifest.ParseManifest call inside readExistingTargets (init.go:332)
//     would already fail identically before lenientReadTargets saw it.
//
// So this guard is intentional defense-in-depth mirroring
// manifest.ParseManifest's own check, not exercised via the real pipeline.
// It is tested here by calling lenientReadTargets directly (it is
// unexported but in the same package) with a hand-built, non-Document node
// -- the only way to actually drive this line, since SafeLoad itself never
// produces one.
func TestLenientReadTargets_NonDocumentGuard_DefenseInDepth(t *testing.T) {
	// A MappingNode passed as the "doc" argument itself (Kind: MappingNode,
	// not DocumentNode) -- the shape SafeLoad's decode step never produces,
	// but which lenientReadTargets must still not misread if ever handed
	// one directly.
	//
	// Deliberately nested (Content[0] is itself a well-formed mapping with
	// a real targets:/claude pair), not a flat "Content = [scalar(targets),
	// scalar(claude)]" list: with a flat Content, doc.Content[0] would be
	// the "targets" scalar itself, which the SECOND guard
	// (root.Kind != MappingNode, init.go:385-387) already rejects on its
	// own -- that shape would still return nil with the first guard
	// deleted, silently passing for the wrong reason. Nesting a genuine
	// mapping one level down means deleting ONLY the first guard (doc.Kind
	// != DocumentNode) lets root := doc.Content[0] land on that inner
	// mapping and the function would wrongly walk it and return
	// ["claude"] -- mutation-checked: confirmed by temporarily disabling
	// the doc.Kind/len(Content) check and re-running this test alone,
	// which went red (got [claude], want nil) against this fixture.
	wrongKindDoc := &yamllib.Node{
		Kind: yamllib.MappingNode,
		Content: []*yamllib.Node{
			{
				Kind: yamllib.MappingNode,
				Content: []*yamllib.Node{
					{Kind: yamllib.ScalarNode, Value: "targets"},
					{Kind: yamllib.ScalarNode, Value: "claude"},
				},
			},
		},
	}
	if got := lenientReadTargets(wrongKindDoc); got != nil {
		t.Errorf("got %v, want nil (non-DocumentNode root must be rejected by the guard, not walked as if it were doc.Content[0])", got)
	}

	// Content == nil with the correct Kind -- isolates the len(doc.Content)
	// == 0 half of the same guard. Mutation-checked: disabling the guard
	// against this fixture panics on the doc.Content[0] index instead of
	// returning nil, which also fails the test (a panic is a test
	// failure), confirming this half is load-bearing too.
	emptyDoc := &yamllib.Node{Kind: yamllib.DocumentNode, Content: nil}
	if got := lenientReadTargets(emptyDoc); got != nil {
		t.Errorf("got %v, want nil (empty Content must be rejected before indexing doc.Content[0])", got)
	}
}
