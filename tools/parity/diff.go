//go:build unix

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/apm-go/apm/internal/ux"
)

// CaseDiff is one line of <out>/diff.jsonl: which fields differed between
// Oracle and Target for a case, the case's own authoritative
// expected_taxonomy alongside a purely advisory heuristic guess, and
// whether every differing field is covered by a waivers.json entry.
type CaseDiff struct {
	ID           string   `json:"id"`
	Fields       []string `json:"fields"`
	Taxonomy     Taxonomy `json:"taxonomy"`
	Waived       bool     `json:"waived"`
	WaiverReason string   `json:"waiver_reason"`
}

// Taxonomy holds both taxonomy sources side by side (ticket-review.md §C:
// "heuristic 可以作為提示，但不應是唯一分類來源") -- Expected comes from the
// case manifest and is authoritative; Heuristic is a best-effort guess
// derived only from which fields differed, never used to override Expected.
type Taxonomy struct {
	Expected  []string `json:"expected"`
	Heuristic []string `json:"heuristic"`
}

// diffDetail is <out>/diff/<id>.json: the old/new value for every field that
// differed, kept alongside diff.jsonl's field-name list so a reviewer never
// has to re-run the case to see what actually changed.
type diffDetail struct {
	ExitCode     *intDiff          `json:"exit_code,omitempty"`
	Stdout       *stringDiff       `json:"stdout,omitempty"`
	Stderr       *stringDiff       `json:"stderr,omitempty"`
	Tree         *treeDiffDetail   `json:"tree,omitempty"`
	HelpSemantic *helpSemanticDiff `json:"help_semantic,omitempty"`
}

type intDiff struct {
	Old int `json:"old"`
	New int `json:"new"`
}

// stringDiff keeps BOTH the untouched raw old/new (read straight from the
// .bin evidence) and the sandbox-path-normalized old/new (ticket 02 attempt
// 2, D2) -- a reviewer sees exactly what each side actually printed, not
// only the post-substitution view that made the two sides comparable.
type stringDiff struct {
	Raw        stringOldNew `json:"raw"`
	Normalized stringOldNew `json:"normalized"`
}

type stringOldNew struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// treeDiffDetail is the tree field's raw evidence: paths only Oracle has,
// paths only Target has, and paths present on both sides whose kind/size/
// sha256 (or, for files, raw bytes) differ.
type treeDiffDetail struct {
	Added   []TreeEntry       `json:"added,omitempty"`
	Removed []TreeEntry       `json:"removed,omitempty"`
	Changed []treeEntryChange `json:"changed,omitempty"`
}

type treeEntryChange struct {
	Path string    `json:"path"`
	Old  TreeEntry `json:"old"`
	New  TreeEntry `json:"new"`
}

// diffCase compares exit_code, normalized stdout, normalized stderr, and
// tree. outDir/c.ID is used both to read each side's byte-exact stdout.bin/
// stderr.bin (record.go's writeRawBodies always writes these, regardless of
// size or UTF-8 validity) for the text fields, and to locate each side's
// copied fs/ evidence for the tree's raw-byte comparison.
func diffCase(outDir string, c Case, oracleRec, targetRec Record) (CaseDiff, diffDetail, error) {
	oracleCaseDir := filepath.Join(outDir, "oracle", c.ID)
	targetCaseDir := filepath.Join(outDir, "target", c.ID)

	oCwd, oCfg, oHome := sandboxPathsFromEnvDelta(oracleRec.EnvDelta)
	tCwd, tCfg, tHome := sandboxPathsFromEnvDelta(targetRec.EnvDelta)

	var fields []string
	var detail diffDetail

	if oracleRec.ExitCode != targetRec.ExitCode {
		fields = append(fields, "exit_code")
		detail.ExitCode = &intDiff{Old: oracleRec.ExitCode, New: targetRec.ExitCode}
	}

	for _, f := range []struct {
		name string
		out  **stringDiff
	}{
		{"stdout", &detail.Stdout},
		{"stderr", &detail.Stderr},
	} {
		sd, differ, err := diffNormalizedField(oracleCaseDir, targetCaseDir, f.name, oCwd, oCfg, oHome, tCwd, tCfg, tHome, c.RewriteBinaryName)
		if err != nil {
			return CaseDiff{}, diffDetail{}, fmt.Errorf("case %s: comparing %s: %w", c.ID, f.name, err)
		}
		if differ {
			fields = append(fields, f.name)
			sdCopy := sd
			*f.out = &sdCopy
		}
	}

	oracleFSRoot := filepath.Join(oracleCaseDir, "fs")
	targetFSRoot := filepath.Join(targetCaseDir, "fs")
	if td, differ := diffTrees(oracleFSRoot, targetFSRoot, oracleRec.Tree, targetRec.Tree, oCwd, oCfg, oHome, tCwd, tCfg, tHome, c.RewriteBinaryName); differ {
		fields = append(fields, "tree")
		detail.Tree = &td
	}

	if isHelpCase(c.Argv) {
		hs, differ, err := diffHelpSemantic(oracleCaseDir, targetCaseDir, oCwd, oCfg, oHome, tCwd, tCfg, tHome, c.RewriteBinaryName)
		if err != nil {
			return CaseDiff{}, diffDetail{}, fmt.Errorf("case %s: comparing help_semantic: %w", c.ID, err)
		}
		if differ {
			fields = append(fields, "help_semantic")
			detail.HelpSemantic = &hs
		}
	}

	cd := CaseDiff{
		ID:       c.ID,
		Fields:   fields,
		Taxonomy: Taxonomy{Expected: c.ExpectedTaxonomy, Heuristic: heuristicTaxonomy(fields, c.Argv)},
	}
	return cd, detail, nil
}

// diffNormalizedField reads <side-case-dir>/<field>.bin -- the byte-exact
// raw capture, unconditionally written regardless of size or UTF-8
// validity -- directly from disk for both sides and normalizes each with
// that side's own sandbox paths. Reading straight from the .bin evidence
// (rather than Record's inline Stdout/Stderr, which is capped at 64KiB and
// omitted entirely for non-UTF-8 output) means every case gets normalized
// the same way regardless of output size or encoding: falling back to
// comparing un-normalized raw sha256 for large/non-UTF-8 bodies would make
// such a case appear to differ purely because each side's sandbox got its
// own unique temp path, never because of an actual behavioural difference.
// Go's string/regexp operations here work over the byte sequence as-is, so
// this is safe even when the bytes aren't valid UTF-8. Whether the field
// differs is decided on the NORMALIZED value only; the returned stringDiff
// additionally carries the untouched raw old/new (ticket 02 attempt 2, D2)
// so a reviewer never has to guess what the un-normalized bytes looked like.
func diffNormalizedField(oracleCaseDir, targetCaseDir, field, oCwd, oCfg, oHome, tCwd, tCfg, tHome string, rewriteBinaryName bool) (sd stringDiff, differ bool, err error) {
	oRaw, err := os.ReadFile(filepath.Join(oracleCaseDir, field+".bin"))
	if err != nil {
		return stringDiff{}, false, fmt.Errorf("reading oracle %s: %w", field+".bin", err)
	}
	tRaw, err := os.ReadFile(filepath.Join(targetCaseDir, field+".bin"))
	if err != nil {
		return stringDiff{}, false, fmt.Errorf("reading target %s: %w", field+".bin", err)
	}

	oNorm := normalizeString(string(oRaw), oCwd, oCfg, oHome, rewriteBinaryName)
	tNorm := normalizeString(string(tRaw), tCwd, tCfg, tHome, rewriteBinaryName)

	if oNorm == tNorm {
		return stringDiff{}, false, nil
	}
	return stringDiff{
		Raw:        stringOldNew{Old: string(oRaw), New: string(tRaw)},
		Normalized: stringOldNew{Old: oNorm, New: tNorm},
	}, true, nil
}

// diffTrees compares two sides' Tree entries by path. Paths are already
// sandbox-relative labels ("cwd/...", "config/..." -- tree.go's walkTree),
// so no path normalization is needed to match them up. A path present on
// both sides as a "file" is ALWAYS compared via normalizedFileBytesEqual,
// never short-circuited on TreeEntry.Size/SHA256 equality: those are
// computed from each side's raw, un-normalized bytes at capture time, so a
// file that merely embeds its own sandbox's absolute path (e.g. a registry
// manifest recording the fixture dir it was seeded from) would otherwise
// have a different size/sha256 on every run purely because each side's
// temp dir name has a different length -- never because of an actual
// behavioural difference (ticket 02 attempt 2, bytes normalisation). Non-
// file entries (dir/symlink) have no byte content to normalize, so they
// still compare via kind/size/sha256 directly.
func diffTrees(oracleFSRoot, targetFSRoot string, oTree, tTree []TreeEntry, oCwd, oCfg, oHome, tCwd, tCfg, tHome string, rewriteBinaryName bool) (treeDiffDetail, bool) {
	oMap := indexTreeByPath(oTree)
	tMap := indexTreeByPath(tTree)

	var added, removed []TreeEntry
	var changed []treeEntryChange

	for path, oe := range oMap {
		te, ok := tMap[path]
		if !ok {
			removed = append(removed, oe)
			continue
		}
		switch {
		case oe.Kind != te.Kind:
			changed = append(changed, treeEntryChange{Path: path, Old: oe, New: te})
		case oe.Kind != "file":
			if oe.Size != te.Size || oe.SHA256 != te.SHA256 {
				changed = append(changed, treeEntryChange{Path: path, Old: oe, New: te})
			}
		case !normalizedFileBytesEqual(oracleFSRoot, targetFSRoot, path, oCwd, oCfg, oHome, tCwd, tCfg, tHome, rewriteBinaryName):
			changed = append(changed, treeEntryChange{Path: path, Old: oe, New: te})
		}
	}
	for path, te := range tMap {
		if _, ok := oMap[path]; !ok {
			added = append(added, te)
		}
	}

	if len(added) == 0 && len(removed) == 0 && len(changed) == 0 {
		return treeDiffDetail{}, false
	}

	sortTreeEntries(added)
	sortTreeEntries(removed)
	sort.Slice(changed, func(i, j int) bool { return changed[i].Path < changed[j].Path })

	return treeDiffDetail{Added: added, Removed: removed, Changed: changed}, true
}

func indexTreeByPath(tree []TreeEntry) map[string]TreeEntry {
	m := make(map[string]TreeEntry, len(tree))
	for _, e := range tree {
		m[e.Path] = e
	}
	return m
}

// normalizedFileBytesEqual reads path (a tree entry's sandbox-relative
// label, e.g. "cwd/apm.yml") back from each side's copied fs/ evidence
// directory (record.go's copyEvidenceFiles). When both sides' bytes are
// valid UTF-8, it compares them the same way stdout/stderr are compared:
// after normalizeString substitutes away each side's own sandbox paths --
// otherwise a file that merely stores its own sandbox's absolute path would
// show as changed on every run. A file that isn't valid UTF-8 on either
// side is compared as raw bytes instead (acceptance: "binary files compare
// raw"). Any read failure is treated as a difference rather than silently
// skipped -- evidence that can't be verified is not evidence of a match.
func normalizedFileBytesEqual(oracleFSRoot, targetFSRoot, path, oCwd, oCfg, oHome, tCwd, tCfg, tHome string, rewriteBinaryName bool) bool {
	rel := filepath.FromSlash(path)
	oData, oErr := os.ReadFile(filepath.Join(oracleFSRoot, rel))
	tData, tErr := os.ReadFile(filepath.Join(targetFSRoot, rel))
	if oErr != nil || tErr != nil {
		return false
	}
	if !utf8.Valid(oData) || !utf8.Valid(tData) {
		return bytes.Equal(oData, tData)
	}
	oNorm := normalizeString(string(oData), oCwd, oCfg, oHome, rewriteBinaryName)
	tNorm := normalizeString(string(tData), tCwd, tCfg, tHome, rewriteBinaryName)
	return oNorm == tNorm
}

// heuristicTaxonomy is an advisory-only guess at which finding classes a
// case's differing fields might correspond to (ticket-review.md §C: "exit
// -> F08, help text -> F01, tree/bytes -> F09/F03"). It is never used to
// override a case's expected_taxonomy and never deletes the raw field-level
// diff -- it exists purely to help a reviewer triage diff.jsonl faster.
func heuristicTaxonomy(fields []string, argv []string) []string {
	var tax []string
	seen := make(map[string]bool)
	add := func(t string) {
		if !seen[t] {
			seen[t] = true
			tax = append(tax, t)
		}
	}

	isHelp := containsHelpFlag(argv)
	for _, f := range fields {
		switch f {
		case "exit_code":
			add("F08")
		case "stdout", "stderr":
			if isHelp {
				add("F01")
			}
		case "tree":
			add("F09")
			add("F03")
		}
	}
	return tax
}

func containsHelpFlag(argv []string) bool {
	for _, a := range argv {
		if a == "--help" || a == "-h" {
			return true
		}
	}
	return false
}

// applyWaiver returns a copy of cd with Waived/WaiverReason set when cd has
// at least one differing field and some SINGLE waiver entry for cd.ID
// covers every one of those fields (acceptance: "EVERY differing field is
// listed in that waiver's fields. Partial coverage = unwaived."). A diff
// with no fields is left as-is: there is nothing to waive.
func applyWaiver(cd CaseDiff, waivers []Waiver) CaseDiff {
	if len(cd.Fields) == 0 {
		return cd
	}
	for _, w := range waivers {
		if w.ID != cd.ID {
			continue
		}
		if coversAllFields(w.Fields, cd.Fields) {
			cd.Waived = true
			cd.WaiverReason = w.Reason
			return cd
		}
	}
	return cd
}

func coversAllFields(waiverFields, diffFields []string) bool {
	set := make(map[string]bool, len(waiverFields))
	for _, f := range waiverFields {
		set[f] = true
	}
	for _, f := range diffFields {
		if !set[f] {
			return false
		}
	}
	return true
}

// countUnwaived returns how many diffs have at least one differing field
// that isn't covered by a waiver -- the runner's exit-1 gate (acceptance:
// "Exit 0 iff every case's diff is empty or fully waived").
func countUnwaived(diffs []CaseDiff) int {
	n := 0
	for _, d := range diffs {
		if len(d.Fields) > 0 && !d.Waived {
			n++
		}
	}
	return n
}

// writeDiffJSONL writes <out>/diff.jsonl: one line per case, in the order
// diffs was built (LoadCases' sorted order), so a run's output is
// reproducible and diffable across invocations.
func writeDiffJSONL(outDir string, diffs []CaseDiff) error {
	var buf bytes.Buffer
	for _, d := range diffs {
		data, err := json.Marshal(d)
		if err != nil {
			return fmt.Errorf("marshalling diff for %s: %w", d.ID, err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	return os.WriteFile(filepath.Join(outDir, "diff.jsonl"), buf.Bytes(), 0o644)
}

// writeDiffDetail writes <out>/diff/<id>.json -- only called for cases that
// actually have a diff, since an empty detail carries no evidence worth a
// file.
func writeDiffDetail(outDir, id string, detail diffDetail) error {
	dir := filepath.Join(outDir, "diff")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating diff dir: %w", err)
	}
	data, err := json.MarshalIndent(detail, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling diff detail for %s: %w", id, err)
	}
	return os.WriteFile(filepath.Join(dir, id+".json"), data, 0o644)
}

// writeSummary writes <out>/summary.txt via ux.Table: id, which fields
// differed, whether the diff is waived, and the case's expected taxonomy.
func writeSummary(outDir string, diffs []CaseDiff) error {
	rows := make([][]string, 0, len(diffs))
	for _, d := range diffs {
		rows = append(rows, []string{
			d.ID,
			strings.Join(d.Fields, ","),
			fmt.Sprintf("%v", d.Waived),
			strings.Join(d.Taxonomy.Expected, ","),
		})
	}

	var buf bytes.Buffer
	ux.Table(&buf, []string{"id", "fields differing", "waived", "taxonomy"}, rows)
	return os.WriteFile(filepath.Join(outDir, "summary.txt"), buf.Bytes(), 0o644)
}
