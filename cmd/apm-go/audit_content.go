package main

import (
	"fmt"
	"io"
	"sort"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/security"
)

// collectDeployedFilePaths returns the deduplicated, sorted set of every
// deployed file path recorded in lock, across BOTH dependency entries and
// the project's own local self-entry -- `audit --content` must scan
// deps + local, not just one (Phase 7 checklist A2).
func collectDeployedFilePaths(lock *lockfile.Lockfile) []string {
	seen := make(map[string]bool)
	var paths []string
	add := func(list []string) {
		for _, p := range list {
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			paths = append(paths, p)
		}
	}
	for i := range lock.Dependencies {
		add(lock.Dependencies[i].DeployedFiles)
	}
	add(lock.LocalDeployedFiles)
	sort.Strings(paths)
	return paths
}

// runAuditContentScan implements `apm-go audit --content`: a hidden-Unicode
// scan over every deployed file recorded in apm.lock.yaml, mirroring the
// content-scan pillar of Python's bare `apm audit` (content_scanner.py). It
// reuses internal/security's shared scanner (the same one `pack` already
// runs warn-only over bundle sources, internal/pack/bundle/producer.go)
// rather than re-implementing the suspicious-range table.
//
// Exit codes (checklist A4): 0 clean (or info-only), 1 critical findings
// present, 2 warning-only (no critical).
func runAuditContentScan(out, errOut io.Writer, lock *lockfile.Lockfile) error {
	paths := collectDeployedFilePaths(lock)

	var all []security.ScanFinding
	for _, p := range paths {
		all = append(all, security.ScanFile(p)...)
	}

	if len(all) == 0 {
		fmt.Fprintf(out, "audit --content: %d file(s) scanned, no hidden characters\n", len(paths))
		return nil
	}

	hasCritical, counts := security.Classify(all)
	for _, f := range all {
		fmt.Fprintf(errOut, "%s: %s %s at %s:%d:%d (%s)\n",
			f.Severity, f.Category, f.Codepoint, f.File, f.Line, f.Column, f.Description)
	}

	if hasCritical {
		return withExitCode(1, fmt.Errorf("audit --content failed: %d critical finding(s), %d warning(s) across %d file(s)",
			counts[security.SeverityCritical], counts[security.SeverityWarning], len(paths)))
	}
	if counts[security.SeverityWarning] > 0 {
		return withExitCode(2, fmt.Errorf("audit --content: %d warning(s) across %d file(s), no critical findings",
			counts[security.SeverityWarning], len(paths)))
	}

	// Only info-level findings (e.g. a leading BOM) -- not actionable.
	fmt.Fprintf(out, "audit --content: %d file(s) scanned, %d info-level finding(s) (no action needed)\n",
		len(paths), counts[security.SeverityInfo])
	return nil
}
