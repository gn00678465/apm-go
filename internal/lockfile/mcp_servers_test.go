package lockfile

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
)

// TestParseLockfile_MCPServers covers un-060's parse-side prerequisite: the
// top-level mcp_servers list round-trips through ParseLockfile.
func TestParseLockfile_MCPServers(t *testing.T) {
	// Arrange
	yamlSrc := "lockfile_version: \"1\"\n" +
		"mcp_servers:\n" +
		"  - alpha-server\n" +
		"  - beta-server\n" +
		"dependencies: []\n"
	node, err := yamlcore.SafeLoad([]byte(yamlSrc))
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}

	// Act
	lf, err := ParseLockfile(node)
	if err != nil {
		t.Fatalf("ParseLockfile: %v", err)
	}

	// Assert
	if len(lf.MCPServers) != 2 || lf.MCPServers[0] != "alpha-server" || lf.MCPServers[1] != "beta-server" {
		t.Errorf("MCPServers = %v, want [alpha-server beta-server]", lf.MCPServers)
	}
}

// TestParseLockfile_MCPServers_FailOpenWhenAbsent locks down the design's
// fail-open requirement (design.md's "前置: lockfile 舊版無 mcp_servers 欄位時
// fail-open"): a lockfile written before this field existed must parse
// cleanly with an empty/nil MCPServers, never an error, and never treated as
// "explicitly no servers" by any caller that checks len() == 0 either way --
// this just locks that ParseLockfile doesn't choke or panic on absence.
func TestParseLockfile_MCPServers_FailOpenWhenAbsent(t *testing.T) {
	// Arrange
	yamlSrc := "lockfile_version: \"1\"\n" +
		"dependencies:\n" +
		"  - repo_url: github.com/acme/foo\n" +
		"    source: git\n"
	node, err := yamlcore.SafeLoad([]byte(yamlSrc))
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}

	// Act
	lf, err := ParseLockfile(node)
	if err != nil {
		t.Fatalf("ParseLockfile: %v", err)
	}

	// Assert
	if len(lf.MCPServers) != 0 {
		t.Errorf("MCPServers = %v, want empty (fail-open) for a lockfile predating this field", lf.MCPServers)
	}
}

// TestWriteLockfile_RoundTrip_MCPServers covers un-060's write-side
// prerequisite: a fresh Lockfile with MCPServers set serializes the
// mcp_servers key, and re-parsing the output reproduces the same list.
func TestWriteLockfile_RoundTrip_MCPServers(t *testing.T) {
	// Arrange
	lf := &Lockfile{
		Version:    "1",
		MCPServers: []string{"alpha-server", "beta-server"},
	}

	// Act
	out, err := WriteLockfile(lf, nil)
	if err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}
	node, err := yamlcore.SafeLoad(out)
	if err != nil {
		t.Fatalf("SafeLoad(out): %v", err)
	}
	reparsed, err := ParseLockfile(node)
	if err != nil {
		t.Fatalf("ParseLockfile(out): %v", err)
	}

	// Assert
	if len(reparsed.MCPServers) != 2 || reparsed.MCPServers[0] != "alpha-server" || reparsed.MCPServers[1] != "beta-server" {
		t.Errorf("round-tripped MCPServers = %v, want [alpha-server beta-server]", reparsed.MCPServers)
	}
}

// TestWriteLockfile_MCPServers_OmittedWhenEmpty is the "purely additive"
// check mirroring the marketplace provenance tests: a Lockfile with no MCP
// servers must not gain an empty mcp_servers: [] key.
func TestWriteLockfile_MCPServers_OmittedWhenEmpty(t *testing.T) {
	lf := &Lockfile{Version: "1"}
	out, err := WriteLockfile(lf, nil)
	if err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}
	if strings.Contains(string(out), "mcp_servers") {
		t.Errorf("expected no mcp_servers key when MCPServers is empty, got:\n%s", string(out))
	}
}

// TestWriteLockfile_RoundTrip_MCPServersNoDoubleEmit is the adversarial
// regression the mkt-031 provenance tests call out by name: knownTopKeys
// (write.go) is a separate explicit list from the mcp_servers serialization
// block -- omitting "mcp_servers" from knownTopKeys would make the
// passthrough "preserve unknown top-level keys from original" loop treat an
// already-known, already-emitted mcp_servers key as an unrecognized one and
// copy it onto the root a SECOND time.
func TestWriteLockfile_RoundTrip_MCPServersNoDoubleEmit(t *testing.T) {
	// Arrange
	yamlSrc := "lockfile_version: \"1\"\n" +
		"mcp_servers:\n" +
		"  - alpha-server\n" +
		"dependencies: []\n"
	node, err := yamlcore.SafeLoad([]byte(yamlSrc))
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}
	lf, err := ParseLockfile(node)
	if err != nil {
		t.Fatalf("ParseLockfile: %v", err)
	}

	// Act -- re-serialize against the ORIGINAL node, like
	// deployAndFinalize's WriteLockfile(newLock, existingNode) call.
	origNode, err := yamlcore.SafeLoad([]byte(yamlSrc))
	if err != nil {
		t.Fatalf("SafeLoad (orig): %v", err)
	}
	out, err := WriteLockfile(lf, origNode)
	if err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	// Assert -- mcp_servers appears exactly once, not twice.
	outStr := string(out)
	if n := strings.Count(outStr, "mcp_servers:"); n != 1 {
		t.Errorf("mcp_servers: appears %d times in output, want exactly 1 (double-emit bug):\n%s", n, outStr)
	}
}

// TestIsSemanticEqual_MCPServersParticipates locks down that MCPServers
// participates in IsSemanticEqual (mirrors LocalDeployedFiles/marketplace
// provenance): a real change to the deployed MCP server set must trigger a
// lockfile rewrite in deployAndFinalize's no-op check, not be masked as
// "Already up to date".
func TestIsSemanticEqual_MCPServersParticipates(t *testing.T) {
	a := &Lockfile{Version: "1", MCPServers: []string{"alpha-server"}}
	b := &Lockfile{Version: "1", MCPServers: []string{"alpha-server", "beta-server"}}
	if IsSemanticEqual(a, b) {
		t.Error("lockfiles with different mcp_servers should not be equal")
	}

	c := &Lockfile{Version: "1", MCPServers: []string{"alpha-server"}}
	if !IsSemanticEqual(a, c) {
		t.Error("lockfiles with identical mcp_servers should be equal")
	}
}

// TestLockfile_RemoveKeys covers un-070~072's delete API: removing a single
// key, removing multiple keys, and the post-removal index staying correct
// for FindByKey (both hits and misses).
func TestLockfile_RemoveKeys(t *testing.T) {
	lf := &Lockfile{
		Dependencies: []LockedDep{
			{RepoURL: "github.com/acme/a"},
			{RepoURL: "github.com/acme/b"},
			{RepoURL: "github.com/acme/c", VirtualPath: "sub"},
		},
	}

	// Force the index to build before mutation, to prove RemoveKeys rebuilds
	// it rather than leaving a stale one behind.
	if lf.FindByKey("github.com/acme/a") == nil {
		t.Fatal("setup: expected to find a before removal")
	}

	// Act -- remove a single key.
	lf.RemoveKeys([]string{"github.com/acme/a"})

	// Assert
	if len(lf.Dependencies) != 2 {
		t.Fatalf("after removing 1 key: len(Dependencies) = %d, want 2", len(lf.Dependencies))
	}
	if lf.FindByKey("github.com/acme/a") != nil {
		t.Error("github.com/acme/a should be gone after RemoveKeys")
	}
	if lf.FindByKey("github.com/acme/b") == nil {
		t.Error("github.com/acme/b should still be found")
	}
	if lf.FindByKey("github.com/acme/c/sub") == nil {
		t.Error("github.com/acme/c/sub should still be found")
	}

	// Act -- remove multiple keys at once, emptying the lockfile.
	lf.RemoveKeys([]string{"github.com/acme/b", "github.com/acme/c/sub"})

	// Assert
	if len(lf.Dependencies) != 0 {
		t.Errorf("after removing all keys: len(Dependencies) = %d, want 0", len(lf.Dependencies))
	}
	if lf.FindByKey("github.com/acme/b") != nil {
		t.Error("github.com/acme/b should be gone after second RemoveKeys")
	}
}

// TestLockfile_RemoveKeys_UnknownKeyIsNoop ensures RemoveKeys silently
// ignores keys that aren't present, matching Python's tolerant removal
// (the caller already validated existence upstream; RemoveKeys itself
// should not panic or error on a miss).
func TestLockfile_RemoveKeys_UnknownKeyIsNoop(t *testing.T) {
	lf := &Lockfile{
		Dependencies: []LockedDep{{RepoURL: "github.com/acme/a"}},
	}
	lf.RemoveKeys([]string{"github.com/does/not-exist"})
	if len(lf.Dependencies) != 1 {
		t.Errorf("len(Dependencies) = %d, want 1 (unknown key should be a no-op)", len(lf.Dependencies))
	}
	if lf.FindByKey("github.com/acme/a") == nil {
		t.Error("github.com/acme/a should still be found after a no-op removal")
	}
}
