//go:build unix

package main

import (
	"bytes"
	"fmt"
	"os"
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

	env := buildEnv(expandCaseEnv(c.Env, sb.Cwd), sb.Home, sb.LauncherCache)
	if c.PathPrepend != "" {
		prepend := filepath.Join(c.Dir, c.PathPrepend)
		if c.PathExclusive {
			env["PATH"] = prepend
		} else {
			env["PATH"] = prepend + string(os.PathListSeparator) + env["PATH"]
		}
	}

	// Setup runs happen before the pre-run tree snapshot: they seed state
	// (e.g. registering a marketplace) that Argv's own run depends on, and
	// must not be attributed to Argv as a tree diff of its own.
	for i, setupArgv := range c.SetupArgv {
		argv := make([]string, 0, len(binPath)+len(setupArgv))
		argv = append(argv, binPath...)
		argv = append(argv, setupArgv...)
		res := runProcess(argv, env, "", sb.Cwd, timeout)
		if res.ExitCode != 0 {
			return Record{}, fmt.Errorf("case %s (%s): setup_argv[%d] %v exited %d: stdout=%q stderr=%q",
				c.ID, side, i, setupArgv, res.ExitCode, res.Stdout, res.Stderr)
		}
	}

	preTree, err := walkTree(sb.Cwd, "cwd")
	if err != nil {
		return Record{}, fmt.Errorf("case %s (%s): snapshotting fixture: %w", c.ID, side, err)
	}

	argv := make([]string, 0, len(binPath)+len(c.Argv))
	argv = append(argv, binPath...)
	argv = append(argv, c.Argv...)

	res := runProcess(argv, env, c.Stdin, sb.Cwd, timeout)

	tree, roots, err := postRunTree(sb, preTree)
	if err != nil {
		return Record{}, fmt.Errorf("case %s (%s): walking post-run tree: %w", c.ID, side, err)
	}

	if err := checkForbiddenSubstrings(c, side, res.Stdout, res.Stderr, tree, roots); err != nil {
		return Record{}, err
	}

	rec := NewRecord(c.ID, argv, env, res.ExitCode, res.TimedOut, res.Stdout, res.Stderr, tree)

	caseOutDir := filepath.Join(outDir, side, c.ID)
	if err := writeRecordJSON(caseOutDir, rec); err != nil {
		return Record{}, fmt.Errorf("case %s (%s): %w", c.ID, side, err)
	}
	if err := writeRawBodies(caseOutDir, res.Stdout, res.Stderr); err != nil {
		return Record{}, fmt.Errorf("case %s (%s): %w", c.ID, side, err)
	}
	if err := copyEvidenceFiles(caseOutDir, roots, tree); err != nil {
		return Record{}, fmt.Errorf("case %s (%s): %w", c.ID, side, err)
	}

	return rec, nil
}

// checkForbiddenSubstrings is ticket 08's token-non-leak gate (Case.
// ForbidSubstrings): fails closed the moment any listed substring turns up
// in this side's raw stdout, raw stderr, or any "file" tree entry's on-disk
// content. Runs before the sandbox is cleaned up (runCaseSide's defer),
// since the tree's "file" entries are read from roots (still-live sandbox
// directories), not from copied evidence. A no-op when the case sets no
// ForbidSubstrings.
func checkForbiddenSubstrings(c Case, side string, stdout, stderr []byte, tree []TreeEntry, roots map[string]string) error {
	if len(c.ForbidSubstrings) == 0 {
		return nil
	}
	for _, sub := range c.ForbidSubstrings {
		if sub == "" {
			continue
		}
		needle := []byte(sub)
		if bytes.Contains(stdout, needle) {
			return fmt.Errorf("case %s (%s): forbidden substring %q found in stdout", c.ID, side, sub)
		}
		if bytes.Contains(stderr, needle) {
			return fmt.Errorf("case %s (%s): forbidden substring %q found in stderr", c.ID, side, sub)
		}
		for _, e := range tree {
			if e.Kind != "file" {
				continue
			}
			label, rel, ok := splitLabel(e.Path)
			if !ok {
				continue
			}
			root, ok := roots[label]
			if !ok {
				continue
			}
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				continue
			}
			if bytes.Contains(data, needle) {
				return fmt.Errorf("case %s (%s): forbidden substring %q found in fs file %s", c.ID, side, sub, e.Path)
			}
		}
	}
	return nil
}

// postRunTree walks cwd and HOME after the run, merges them with the
// deleted entries diffDeleted finds against the pre-run fixture snapshot,
// and returns the merged, sorted tree plus the label->root map evidence
// copying needs. HOME is captured because the Oracle always writes its
// persistent state (e.g. the marketplace registry) under $HOME/.apm/ --
// without this, that state never enters the tree/fs evidence at all (ticket
// 02 attempt 3, amending ticket 01 AC5: eval-ticket-02-r2.md Issue 1).
// HOME-rooted paths are recorded under the "home/" label and normalised to
// <HOME> the same way "cwd/" already normalises to <TMP>. There is no
// separate "config" root: since ticket 15 the runner no longer forces
// APM_CONFIG_DIR on either side, so the one case that sets it explicitly
// points it at a path under Cwd, which the cwd walk already covers.
func postRunTree(sb *sandbox, preTree []TreeEntry) ([]TreeEntry, map[string]string, error) {
	roots := map[string]string{"cwd": sb.Cwd, "home": sb.Home}

	postCwd, err := walkTree(sb.Cwd, "cwd")
	if err != nil {
		return nil, nil, err
	}
	postHome, err := walkTree(sb.Home, "home")
	if err != nil {
		return nil, nil, err
	}

	afterPaths := make(map[string]bool, len(postCwd)+len(postHome))
	for _, e := range postCwd {
		afterPaths[e.Path] = true
	}
	for _, e := range postHome {
		afterPaths[e.Path] = true
	}
	deleted := diffDeleted(preTree, afterPaths)

	tree := make([]TreeEntry, 0, len(postCwd)+len(postHome)+len(deleted))
	tree = append(tree, postCwd...)
	tree = append(tree, postHome...)
	tree = append(tree, deleted...)
	sortTreeEntries(tree)

	return tree, roots, nil
}
