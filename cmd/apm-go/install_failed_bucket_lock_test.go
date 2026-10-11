package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #46: a bucket with a failed deploy is not cleaned in that run, so the
// lock must keep its old paths and hashes for a later run to clean.

const (
	failedBucketAgent     = "---\nname: a\ndescription: d\n---\nbody\n"
	failedBucketSkill     = "---\nname: s2\ndescription: d\n---\nskill\n"
	failedBucketSkillFile = "      - .claude/skills/s2/SKILL.md\n"
	failedBucketSkillHash = "      .claude/skills/s2/SKILL.md: sha256:bcf0e3e2cbe39bf16f0ecd4168bff95026cb0d894fdf1b9802085f7e24f263b6\n"
	failedBucketUserFile  = "x\n"
)

// failedBucketGlobalDependency installs a package with an agent and two
// skills, puts a user's directory where the agent's symlink was, removes
// skill s2 from the source, and installs again. It returns that install's
// stdout.
func failedBucketGlobalDependency(t *testing.T) (home, out string) {
	t.Helper()
	home, src := globalStaleScope(t, map[string]string{
		".apm/agents/a.md":        failedBucketAgent,
		".apm/skills/s1/SKILL.md": "---\nname: s1\ndescription: d\n---\nskill\n",
		".apm/skills/s2/SKILL.md": failedBucketSkill,
	})
	globalStaleInstall(t, "claude", src)
	agent := filepath.Join(home, ".claude", "agents", "a.md")
	if err := os.Remove(agent); err != nil {
		t.Fatal(err)
	}
	writeGlobalStaleFile(t, filepath.Join(agent, "f"), failedBucketUserFile)
	for _, rel := range []string{".apm/skills/s2/SKILL.md", ".apm/skills/s2"} {
		if err := os.Remove(filepath.Join(src, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
	return home, globalStaleInstall(t, "claude")
}

func assertFailedBucketLockKeepsSkill(t *testing.T, home string) {
	t.Helper()
	lock := globalStaleRead(t, filepath.Join(home, ".apm", "apm.lock.yaml"))
	if !strings.Contains(lock, failedBucketSkillFile) || !strings.Contains(lock, failedBucketSkillHash) {
		t.Errorf("lock lost the path or hash of .claude/skills/s2/SKILL.md:\n%s", lock)
	}
}

func TestInstallGlobal_FailedBucket_LockKeepsPathOfRemovedSource(t *testing.T) {
	home, out := failedBucketGlobalDependency(t)

	if !strings.Contains(out, "deploy a to claude failed") {
		t.Fatalf("precondition: the agent deploy did not fail:\n%s", out)
	}
	assertFailedBucketLockKeepsSkill(t, home)
	if !globalStaleIsSymlink(t, filepath.Join(home, ".claude", "skills", "s2")) {
		t.Error(".claude/skills/s2 is not a symlink any more")
	}
	if strings.Contains(out, "Cleaned") {
		t.Errorf("a failed bucket must not be cleaned:\n%s", out)
	}
	if got := globalStaleRead(t, filepath.Join(home, ".claude", "agents", "a.md", "f")); got != failedBucketUserFile {
		t.Errorf("the user's file = %q, want %q", got, failedBucketUserFile)
	}
}

func TestInstallGlobal_FailedBucket_LockKeepsPathWhileFailureRemains(t *testing.T) {
	home, _ := failedBucketGlobalDependency(t)

	out := globalStaleInstall(t, "claude")

	if !strings.Contains(out, "deploy a to claude failed") {
		t.Fatalf("precondition: the agent deploy did not fail:\n%s", out)
	}
	assertFailedBucketLockKeepsSkill(t, home)
	if !globalStaleIsSymlink(t, filepath.Join(home, ".claude", "skills", "s2")) {
		t.Error(".claude/skills/s2 is not a symlink any more")
	}
}

func TestInstallGlobal_FailedBucket_CleanedAfterFailureIsFixed(t *testing.T) {
	home, _ := failedBucketGlobalDependency(t)
	agent := filepath.Join(home, ".claude", "agents", "a.md")
	for _, p := range []string{filepath.Join(agent, "f"), agent} {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}

	out := globalStaleInstall(t, "claude")

	if _, err := os.Lstat(filepath.Join(home, ".claude", "skills", "s2")); !os.IsNotExist(err) {
		t.Errorf(".claude/skills/s2 still present (Lstat err = %v)", err)
	}
	assertGlobalStaleLock(t, home, false, ".claude/skills/s2/SKILL.md")
	if !globalStaleIsSymlink(t, agent) {
		t.Error(".claude/agents/a.md is not a symlink again")
	}
	if got := globalStaleRead(t, agent); got != failedBucketAgent {
		t.Errorf("a.md through its symlink = %q, want %q", got, failedBucketAgent)
	}
	if !strings.Contains(out, "Cleaned 1 stale file from _local/dep-") || strings.Count(out, ".claude/skills/s2\n") != 1 {
		t.Errorf("want one cleaned line listing .claude/skills/s2:\n%s", out)
	}
	if again := globalStaleInstall(t, "claude"); !strings.Contains(again, "Already up to date") {
		t.Errorf("the next install is not up to date:\n%s", again)
	}
}

func TestInstallGlobal_FailedBucket_LocalContentKeptThenCleaned(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{
		".apm/agents/keep.md": globalStaleKeepAgent,
	})
	mine := filepath.Join(home, ".apm", ".apm", "agents", "mine.md")
	writeGlobalStaleFile(t, mine, "---\nname: mine\ndescription: mine\n---\nmine\n")
	writeGlobalStaleFile(t, filepath.Join(home, ".apm", ".apm", "agents", "other.md"), "---\nname: other\ndescription: other\n---\nother\n")
	globalStaleInstall(t, "claude", src)
	link := filepath.Join(home, ".claude", "agents", "mine.md")
	other := filepath.Join(home, ".claude", "agents", "other.md")
	if err := os.Remove(other); err != nil {
		t.Fatal(err)
	}
	writeGlobalStaleFile(t, filepath.Join(other, "f"), failedBucketUserFile)
	if err := os.Remove(mine); err != nil {
		t.Fatal(err)
	}

	failed := globalStaleInstall(t, "claude")

	if !strings.Contains(failed, "deploy other to claude failed") {
		t.Fatalf("precondition: the local agent deploy did not fail:\n%s", failed)
	}
	if lock := readLockfile(t); !staleCleanupContains(lock.LocalDeployedFiles, ".claude/agents/mine.md") ||
		lock.LocalDeployedHashes[".claude/agents/mine.md"] != "sha256:28cae304ef105adecc7487fb8af607bd1a5308795da68b699088c08c517eaefe" {
		t.Errorf("local_deployed_files lost .claude/agents/mine.md or its hash: %v %v", lock.LocalDeployedFiles, lock.LocalDeployedHashes)
	}
	if !globalStaleIsSymlink(t, link) {
		t.Error(".claude/agents/mine.md is not a symlink any more")
	}

	for _, p := range []string{filepath.Join(other, "f"), other} {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	fixed := globalStaleInstall(t, "claude")

	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf(".claude/agents/mine.md still present (Lstat err = %v)", err)
	}
	if lock := readLockfile(t); staleCleanupContains(lock.LocalDeployedFiles, ".claude/agents/mine.md") {
		t.Errorf("local_deployed_files still lists .claude/agents/mine.md: %v", lock.LocalDeployedFiles)
	}
	if !strings.Contains(fixed, "Cleaned 1 stale file from <local .apm/>") || strings.Count(fixed, ".claude/agents/mine.md\n") != 1 {
		t.Errorf("want one cleaned line for the local bucket listing .claude/agents/mine.md:\n%s", fixed)
	}
}

// A copy deploy leaves the copy of a removed source on disk, so its record
// has to stay in the lock through the failed run as well.
func TestInstall_FailedBucket_CopyDeployLockKeepsPathThenCleaned(t *testing.T) {
	staleCleanupProject(t, staleCleanupClaudeManifest, map[string]string{
		".apm/agents/a.md":        failedBucketAgent,
		".apm/skills/s2/SKILL.md": failedBucketSkill,
	})
	staleCleanupInstall(t, "")
	staleCleanupRemove(t, ".claude/agents/a.md")
	blockDeployOf(t, ".claude/agents/a.md")
	staleCleanupRemove(t, ".apm/skills/s2/SKILL.md")
	staleCleanupRemove(t, ".apm/skills/s2")

	failed := staleCleanupInstall(t, "")

	if !strings.Contains(failed, "deploy a to claude failed") {
		t.Fatalf("precondition: the agent deploy did not fail:\n%s", failed)
	}
	if lock := readLockfile(t); !staleCleanupContains(lock.LocalDeployedFiles, ".claude/skills/s2/SKILL.md") ||
		lock.LocalDeployedHashes[".claude/skills/s2/SKILL.md"] != "sha256:bcf0e3e2cbe39bf16f0ecd4168bff95026cb0d894fdf1b9802085f7e24f263b6" {
		t.Errorf("local_deployed_files lost .claude/skills/s2/SKILL.md or its hash: %v %v", lock.LocalDeployedFiles, lock.LocalDeployedHashes)
	}
	assertStaleCleanupExists(t, true, ".claude/skills/s2/SKILL.md")

	staleCleanupRemove(t, ".claude/agents/a.md")
	fixed := staleCleanupInstall(t, "")

	assertStaleCleanupExists(t, false, ".claude/skills/s2/SKILL.md")
	if lock := readLockfile(t); staleCleanupContains(lock.LocalDeployedFiles, ".claude/skills/s2/SKILL.md") {
		t.Errorf("local_deployed_files still lists .claude/skills/s2/SKILL.md: %v", lock.LocalDeployedFiles)
	}
	if !strings.Contains(fixed, "Cleaned 1 stale file from <local .apm/>") {
		t.Errorf("missing cleaned line:\n%s", fixed)
	}
}
