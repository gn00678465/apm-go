package bundle

import "testing"

func TestMergeFileMap_DepVsDep_FirstWins(t *testing.T) {
	fm := NewFileMap()
	fm.MergeFileMap([]Component{{Source: "/dep-a/agents/foo.md", OutputRel: "agents/foo.md"}}, "dep-a", false)
	fm.MergeFileMap([]Component{{Source: "/dep-b/agents/foo.md", OutputRel: "agents/foo.md"}}, "dep-b", false)

	src, ok := fm.Source("agents/foo.md")
	if !ok || src != "/dep-a/agents/foo.md" {
		t.Errorf("source = %q, want dep-a's (first writer wins)", src)
	}
	if len(fm.Collisions) != 1 {
		t.Fatalf("collisions = %v, want exactly one", fm.Collisions)
	}
}

func TestMergeFileMap_DepVsDep_Force_LastWins(t *testing.T) {
	fm := NewFileMap()
	fm.MergeFileMap([]Component{{Source: "/dep-a/agents/foo.md", OutputRel: "agents/foo.md"}}, "dep-a", true)
	fm.MergeFileMap([]Component{{Source: "/dep-b/agents/foo.md", OutputRel: "agents/foo.md"}}, "dep-b", true)

	src, ok := fm.Source("agents/foo.md")
	if !ok || src != "/dep-b/agents/foo.md" {
		t.Errorf("source = %q, want dep-b's (last writer wins with --force)", src)
	}
	if len(fm.Collisions) != 1 {
		t.Fatalf("collisions = %v, want exactly one (force changes the winner, not the warning)", fm.Collisions)
	}
}

// TestMergeFileMap_RootVsDep_DepWins locks design.md's documented direction:
// when the SAME merge function is called for deps first and the root
// package last (export_plugin_bundle's actual call order,
// plugin_exporter.py:510,526), the root package's own file loses to a
// dependency's same-named file without --force -- the OPPOSITE of hooks/mcp
// (see producer.go's DeepMerge usage, where root always wins via
// overwrite=true).
func TestMergeFileMap_RootVsDep_DepWins(t *testing.T) {
	fm := NewFileMap()
	// Deps are merged first (export_plugin_bundle's loop over
	// lockfile.get_all_dependencies() happens before the root-package merge).
	fm.MergeFileMap([]Component{{Source: "/dep/agents/foo.md", OutputRel: "agents/foo.md"}}, "acme/dep", false)
	// Root package merged last.
	fm.MergeFileMap([]Component{{Source: "/root/agents/foo.md", OutputRel: "agents/foo.md"}}, "root-pkg", false)

	src, ok := fm.Source("agents/foo.md")
	if !ok || src != "/dep/agents/foo.md" {
		t.Errorf("source = %q, want the dependency's file to win over the root package (opposite of hooks/mcp direction)", src)
	}
}

func TestMergeFileMap_NoCollision_BothKept(t *testing.T) {
	fm := NewFileMap()
	fm.MergeFileMap([]Component{{Source: "/a/agents/foo.md", OutputRel: "agents/foo.md"}}, "a", false)
	fm.MergeFileMap([]Component{{Source: "/b/agents/bar.md", OutputRel: "agents/bar.md"}}, "b", false)
	if len(fm.Keys()) != 2 {
		t.Errorf("keys = %v, want 2 distinct entries", fm.Keys())
	}
	if len(fm.Collisions) != 0 {
		t.Errorf("collisions = %v, want none", fm.Collisions)
	}
}

func TestMergeFileMap_InvalidOutputRel_Dropped(t *testing.T) {
	fm := NewFileMap()
	fm.MergeFileMap([]Component{
		{Source: "/x", OutputRel: "/absolute/path.md"},
		{Source: "/y", OutputRel: "../escape.md"},
		{Source: "/z", OutputRel: "agents/../../escape.md"},
		{Source: "/ok", OutputRel: "agents/good.md"},
	}, "owner", false)
	if len(fm.Keys()) != 1 || fm.Keys()[0] != "agents/good.md" {
		t.Errorf("keys = %v, want only agents/good.md", fm.Keys())
	}
}

// ── hooks/mcp merge direction (root wins, opposite of file_map) ──────────

func TestDeepMerge_HooksHelperPattern_RootOverwritesDeps(t *testing.T) {
	// Mirrors producer.go's actual call pattern: deps merge among
	// themselves with overwrite=false (first dep wins), then the root
	// package merges into the accumulated result with overwrite=true (root
	// always wins) -- the opposite direction from file_map's dep-wins rule.
	depA, _ := DecodeJSONValue([]byte(`{"PreToolUse":"dep-a-hook"}`))
	depB, _ := DecodeJSONValue([]byte(`{"PreToolUse":"dep-b-hook"}`))
	root, _ := DecodeJSONValue([]byte(`{"PreToolUse":"root-hook"}`))

	merged, err := DeepMerge(JSONValue{Kind: KindObject}, depA, false)
	if err != nil {
		t.Fatal(err)
	}
	merged, err = DeepMerge(merged, depB, false)
	if err != nil {
		t.Fatal(err)
	}
	preTool, _ := merged.Get("PreToolUse")
	if preTool.S != "dep-a-hook" {
		t.Fatalf("after dep-vs-dep merge: PreToolUse = %q, want dep-a-hook (first dep wins)", preTool.S)
	}

	merged, err = DeepMerge(merged, root, true)
	if err != nil {
		t.Fatal(err)
	}
	preTool, _ = merged.Get("PreToolUse")
	if preTool.S != "root-hook" {
		t.Errorf("after root merge: PreToolUse = %q, want root-hook (root always wins, opposite of file_map)", preTool.S)
	}
}
