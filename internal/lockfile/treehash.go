package lockfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// ComputeTreeSHA256 computes the canonical git tree hash per spec §5.6.4.
// Uses `git ls-tree` for correct mode bits and tracked-file enumeration.
func ComputeTreeSHA256(repoDir string, commit string) (string, error) {
	out, err := runGit(repoDir, "ls-tree", "-r", "-z", "--full-tree", commit)
	if err != nil {
		return "", fmt.Errorf("git ls-tree: %w", err)
	}

	entries, err := parseLsTree(out)
	if err != nil {
		return "", err
	}

	tree, err := buildTreeStructure(entries, repoDir, commit)
	if err != nil {
		return "", err
	}

	hash := computeCanonicalTreeHash(tree)
	return HashEnvelope("sha256", hash), nil
}

// VerifyTreeSHA256 re-computes tree_sha256 and compares with expected (req-lk-015).
func VerifyTreeSHA256(expected string, repoDir string, commit string) error {
	actual, err := ComputeTreeSHA256(repoDir, commit)
	if err != nil {
		return fmt.Errorf("tree_sha256 verify: %w", err)
	}
	_, expectedHex, err := ParseHashEnvelope(expected)
	if err != nil {
		return fmt.Errorf("tree_sha256 verify: invalid expected: %w", err)
	}
	_, actualHex, _ := ParseHashEnvelope(actual)
	if expectedHex != actualHex {
		return fmt.Errorf("tree_sha256 integrity violation: expected %s, observed %s", expected, actual)
	}
	return nil
}

type lsTreeEntry struct {
	mode   string // e.g. "100644"
	objSHA string // git object SHA-1
	path   string // full path relative to repo root
}

type treeNode struct {
	name     string
	mode     string
	blobHash string               // sha256 hex of content (for files/symlinks)
	children map[string]*treeNode // for directories
}

func parseLsTree(output string) ([]lsTreeEntry, error) {
	var entries []lsTreeEntry
	// -z flag: NUL-separated records, unquoted paths
	for _, record := range strings.Split(output, "\x00") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		// Format: "<mode> <type> <sha1>\t<path>"
		tabIdx := strings.IndexByte(record, '\t')
		if tabIdx < 0 {
			return nil, fmt.Errorf("malformed ls-tree record: %q", record)
		}
		meta := record[:tabIdx]
		path := record[tabIdx+1:]

		parts := strings.Fields(meta)
		if len(parts) < 3 {
			return nil, fmt.Errorf("malformed ls-tree meta: %q", meta)
		}

		entries = append(entries, lsTreeEntry{
			mode:   parts[0],
			objSHA: parts[2],
			path:   path,
		})
	}
	return entries, nil
}

func buildTreeStructure(entries []lsTreeEntry, repoDir, commit string) (*treeNode, error) {
	root := &treeNode{name: "", children: make(map[string]*treeNode)}

	for _, e := range entries {
		// ponytail: skip submodules (mode 160000), spec §5.6.4 doesn't define gitlink handling
		if e.mode == "160000" {
			continue
		}
		blobBytes, err := readGitBlob(repoDir, e.objSHA)
		if err != nil {
			return nil, fmt.Errorf("reading blob %s for %s: %w", e.objSHA, e.path, err)
		}
		h := sha256.Sum256(blobBytes)
		blobSHA256 := hex.EncodeToString(h[:])

		segments := strings.Split(e.path, "/")
		insertIntoTree(root, segments, e.mode, blobSHA256)
	}

	return root, nil
}

func insertIntoTree(node *treeNode, segments []string, mode, blobHash string) {
	if len(segments) == 1 {
		node.children[segments[0]] = &treeNode{
			name:     segments[0],
			mode:     mode,
			blobHash: blobHash,
		}
		return
	}
	dirName := segments[0]
	child, ok := node.children[dirName]
	if !ok {
		child = &treeNode{name: dirName, mode: "040000", children: make(map[string]*treeNode)}
		node.children[dirName] = child
	}
	insertIntoTree(child, segments[1:], mode, blobHash)
}

func computeCanonicalTreeHash(node *treeNode) string {
	names := make([]string, 0, len(node.children))
	for name := range node.children {
		names = append(names, name)
	}
	sort.Strings(names)

	var buf bytes.Buffer
	for _, name := range names {
		child := node.children[name]
		var hash string
		if child.children != nil && len(child.children) > 0 {
			hash = computeCanonicalTreeHash(child)
		} else {
			hash = child.blobHash
		}
		fmt.Fprintf(&buf, "%s %s %s\n", child.mode, name, hash)
	}

	h := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(h[:])
}

func readGitBlob(repoDir, sha string) ([]byte, error) {
	out, err := runGitBytes(repoDir, "cat-file", "blob", sha)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func runGit(dir string, args ...string) (string, error) {
	out, err := runGitBytes(dir, args...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func runGitBytes(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), string(ee.Stderr))
		}
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}
