//go:build unix

package main

import (
	"fmt"
	"path/filepath"
	"time"
)

// runCaseSide runs one case against one side (Oracle or Target) inside a
// fresh sandbox, and writes its evidence under <outDir>/<side>/<id>/. binPath
// is the resolved command (binary + any leading wrapper args, e.g. `uv run
// --project … apm`); c.Argv is appended as the tail.
func runCaseSide(binPath []string, c Case, outDir, side string, timeout time.Duration) (Record, error) {
	sb, err := newSandbox(c.FixtureDir())
	if err != nil {
		return Record{}, fmt.Errorf("case %s (%s): %w", c.ID, side, err)
	}
	defer sb.cleanup()

	preTree, err := walkTree(sb.Cwd, "cwd")
	if err != nil {
		return Record{}, fmt.Errorf("case %s (%s): snapshotting fixture: %w", c.ID, side, err)
	}

	env := buildEnv(c.Env, sb.Home, sb.ConfigDir)
	argv := make([]string, 0, len(binPath)+len(c.Argv))
	argv = append(argv, binPath...)
	argv = append(argv, c.Argv...)

	res := runProcess(argv, env, c.Stdin, sb.Cwd, timeout)

	tree, roots, err := postRunTree(sb, preTree)
	if err != nil {
		return Record{}, fmt.Errorf("case %s (%s): walking post-run tree: %w", c.ID, side, err)
	}

	rec := Record{
		ID:       c.ID,
		Argv:     argv,
		EnvDelta: env,
		ExitCode: res.ExitCode,
		TimedOut: res.TimedOut,
		Stdout:   string(res.Stdout),
		Stderr:   string(res.Stderr),
		Tree:     tree,
	}

	caseOutDir := filepath.Join(outDir, side, c.ID)
	if err := writeRecordJSON(caseOutDir, rec); err != nil {
		return Record{}, fmt.Errorf("case %s (%s): %w", c.ID, side, err)
	}
	if err := copyEvidenceFiles(caseOutDir, roots, tree); err != nil {
		return Record{}, fmt.Errorf("case %s (%s): %w", c.ID, side, err)
	}

	return rec, nil
}

// postRunTree walks cwd and APM_CONFIG_DIR after the run, merges them with
// the deleted entries diffDeleted finds against the pre-run fixture
// snapshot, and returns the merged, sorted tree plus the label->root map
// evidence copying needs.
func postRunTree(sb *sandbox, preTree []TreeEntry) ([]TreeEntry, map[string]string, error) {
	roots := map[string]string{"cwd": sb.Cwd, "config": sb.ConfigDir}

	postCwd, err := walkTree(sb.Cwd, "cwd")
	if err != nil {
		return nil, nil, err
	}
	postConfig, err := walkTree(sb.ConfigDir, "config")
	if err != nil {
		return nil, nil, err
	}

	afterPaths := make(map[string]bool, len(postCwd)+len(postConfig))
	for _, e := range postCwd {
		afterPaths[e.Path] = true
	}
	for _, e := range postConfig {
		afterPaths[e.Path] = true
	}
	deleted := diffDeleted(preTree, afterPaths)

	tree := make([]TreeEntry, 0, len(postCwd)+len(postConfig)+len(deleted))
	tree = append(tree, postCwd...)
	tree = append(tree, postConfig...)
	tree = append(tree, deleted...)
	sortTreeEntries(tree)

	return tree, roots, nil
}
