package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMergeDeployedFiles_PreservesOldTarget(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude", "skills", "s"), 0755)
	os.WriteFile(filepath.Join(dir, ".claude", "skills", "s", "SKILL.md"), []byte("x"), 0644)

	oldFiles := []string{".claude/skills/s/SKILL.md"}
	oldHashes := map[string]string{".claude/skills/s/SKILL.md": "sha256:aaa"}
	newFiles := []string{".agents/skills/s/SKILL.md"}
	newHashes := map[string]string{".agents/skills/s/SKILL.md": "sha256:bbb"}

	files, hashes := mergeDeployedFiles(oldFiles, oldHashes, newFiles, newHashes, dir)

	want := []string{".agents/skills/s/SKILL.md", ".claude/skills/s/SKILL.md"}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("files = %v, want %v", files, want)
	}
	if hashes[".claude/skills/s/SKILL.md"] != "sha256:aaa" {
		t.Error("old hash lost")
	}
	if hashes[".agents/skills/s/SKILL.md"] != "sha256:bbb" {
		t.Error("new hash lost")
	}
}

func TestMergeDeployedFiles_DropsRemovedFiles(t *testing.T) {
	dir := t.TempDir()

	oldFiles := []string{"stale/removed.md"}
	oldHashes := map[string]string{"stale/removed.md": "sha256:old"}
	newFiles := []string{"fresh/new.md"}
	newHashes := map[string]string{"fresh/new.md": "sha256:new"}

	files, hashes := mergeDeployedFiles(oldFiles, oldHashes, newFiles, newHashes, dir)

	if len(files) != 1 || files[0] != "fresh/new.md" {
		t.Errorf("files = %v, want [fresh/new.md] (stale file should be dropped)", files)
	}
	if _, ok := hashes["stale/removed.md"]; ok {
		t.Error("stale hash should not be preserved")
	}
}

func TestMergeDeployedFiles_NewWinsOnConflict(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "shared"), 0755)
	os.WriteFile(filepath.Join(dir, "shared", "file.md"), []byte("x"), 0644)

	oldFiles := []string{"shared/file.md"}
	oldHashes := map[string]string{"shared/file.md": "sha256:old"}
	newFiles := []string{"shared/file.md"}
	newHashes := map[string]string{"shared/file.md": "sha256:new"}

	files, hashes := mergeDeployedFiles(oldFiles, oldHashes, newFiles, newHashes, dir)

	if len(files) != 1 || files[0] != "shared/file.md" {
		t.Errorf("files = %v, want [shared/file.md]", files)
	}
	if hashes["shared/file.md"] != "sha256:new" {
		t.Errorf("hash = %q, want sha256:new (new wins)", hashes["shared/file.md"])
	}
}

func TestMergeDeployedFiles_EmptyOld(t *testing.T) {
	dir := t.TempDir()
	newFiles := []string{"a.md", "b.md"}
	newHashes := map[string]string{"a.md": "h1", "b.md": "h2"}

	files, hashes := mergeDeployedFiles(nil, nil, newFiles, newHashes, dir)

	if !reflect.DeepEqual(files, []string{"a.md", "b.md"}) {
		t.Errorf("files = %v", files)
	}
	if len(hashes) != 2 {
		t.Errorf("hashes len = %d", len(hashes))
	}
}
