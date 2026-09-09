package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/pack/bundle"
	"github.com/apm-go/apm/internal/pluginjson"
	"github.com/apm-go/apm/internal/ux"
	"github.com/spf13/cobra"
)

// maxManifestBytes is the primary enforcement point for the 5 MiB cap
// (T013 step 6): readAndValidate rejects an oversized file before any read
// completes, so pluginjson.Validate's own defensive length check is a
// second line of defense only, never the first to see an oversized
// manifest's bytes.
const maxManifestBytes = 5 * 1024 * 1024

// pluginValidateCmd is `apm-go plugin validate` (mission plugin-manifest-
// validate-01M21E5Q, closing the second half of issue #13). Upstream
// (pinned b75a02b1/v0.29.0 and `main`) has no counterpart command --
// research.md R-01 -- so this command's output contract is NOT a
// tools/parity corpus case; it is pinned by contracts/cli-plugin-
// validate.md and enforced by tools/gate/realexec.sh's dedicated steps
// (ticket 34, .scratch/parity-runner/issues/34-oracle-less-command-output-
// contract.md), at the same byte-for-byte strength (stdout, stderr, exit
// code, read-only file tree) the corpus itself uses.
func pluginValidateCmd() *cobra.Command {
	var strict bool
	var verbose bool

	cmd := &cobra.Command{
		Use:          "validate [path]",
		Short:        "Validate a plugin.json manifest against the Claude Code plugin schema",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// cobra.Args (e.g. MaximumNArgs) is not used here so this exact
			// usage-error text and wrapping stay under this command's own
			// control rather than cobra's default "accepts at most N arg(s)"
			// shape (T015 note).
			if len(args) > 1 {
				return withUsageError(fmt.Errorf("accepts at most 1 arg(s), received %d", len(args)))
			}
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			return runPluginValidate(cmd, path, strict, verbose)
		},
	}
	// Any other cobra flag-parse error (unknown flag, malformed value) --
	// this command has no Oracle counterpart to match wording against, so
	// the generic usage-error contract (exit 2, stderr Usage/Try-help
	// block) is all contracts/cli-plugin-validate.md requires.
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return withUsageError(err)
	})
	cmd.Flags().BoolVar(&strict, "strict", false, "Fail (exit 1) when any warning is found; printed counts are unchanged")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "List the recognized fields present in the manifest before the results")
	return cmd
}

// runPluginValidate implements the fixed locate -> announce -> read
// (regular-file/boundary/size) -> Validate -> render -> exit sequence
// (data-model.md "State transitions"). Locate failure (no candidate exists
// at all) prints before any progress line, matching contracts/cli-plugin-
// validate.md's "找不到 manifest" row; every failure past that point --
// including a disallowed file type, an escaping symlink, an oversized file,
// and an OS-reported I/O error -- is a "could not read" failure that prints
// after the progress line with no Results/Summary block (same contract,
// "無法讀取 manifest" row). Both step failures return withSilentExitCode(1,
// ...) so main's error renderer does not print a second, redundant line.
func runPluginValidate(cmd *cobra.Command, path string, strict, verbose bool) error {
	w := cmd.OutOrStdout()

	loc, locErr := locatePluginManifest(path)
	if locErr != nil {
		ux.Error(w, "%s", locErr)
		return withSilentExitCode(1, locErr)
	}

	ux.Progress(w, "Validating plugin '%s'...", displayManifestPath(loc.path))

	report, readErr := readAndValidate(loc.path, loc.boundary)
	if readErr != nil {
		ux.Error(w, "%s", readErr)
		return withSilentExitCode(1, readErr)
	}

	_, warnings, errs := renderReport(w, report, verbose)

	if errs > 0 {
		return withSilentExitCode(1, fmt.Errorf("plugin manifest failed validation with %d error(s)", errs))
	}
	if strict && warnings > 0 {
		return withSilentExitCode(1, fmt.Errorf("plugin manifest failed --strict validation with %d warning(s)", warnings))
	}
	return nil
}

// errNotRegularFile and errEscapesPath are the two read-boundary policy
// violations locatePluginManifest cannot rule out by name alone (WP03
// review findings 1-2): NFR-003 grants no exception for a symlink, FIFO,
// device, or socket -- whether it is the caller's own path argument or a
// directory-probed candidate -- and no exception for a candidate that
// resolves outside the directory it was probed under. Both are enforced in
// readAndValidate, uniformly, regardless of how manifestPath was obtained.
var (
	errNotRegularFile = errors.New("not a regular file")
	errEscapesPath    = errors.New("outside the given path")
)

// readError renders the read-failure wording contracts/cli-plugin-
// validate.md's "無法讀取 manifest" row carries (WP03 review finding 6):
// one family, covering a disallowed file type, an escaping path, an
// oversized file, and a genuine OS I/O error alike.
func readError(path string, cause error) error {
	return fmt.Errorf("could not read '%s': %w", filepath.ToSlash(path), cause)
}

// manifestFile is the minimal handle readAndValidate reads a manifest
// through. *os.File satisfies it directly; tests substitute a fake whose
// Read yields more bytes than its own Stat reported, which is how the
// bounded-read refusal below is proven deterministically -- a real
// filesystem race (grow the file between the size check and the read
// finishing) cannot be reproduced reliably across platforms in a test.
type manifestFile = io.ReadCloser

// openManifestFile is the seam readAndValidate opens the manifest through,
// overridable by tests. The size comes from Stat on this same open handle
// (fstat on the fd), not a second path-based Stat call, so a link swapped
// into manifestPath after openManifestFile returns cannot change what size
// gets checked.
//
// The open itself goes through openManifestFileForPlatform
// (plugin_validate_open_unix.go / plugin_validate_open_windows.go), which
// is where the check-then-open race (WP03 round-2 review finding 1) is
// constrained, to a degree that differs by platform: on unix,
// O_NOFOLLOW|O_NONBLOCK makes the open refuse a symlink swapped into path
// after the pre-open Lstat below and return immediately rather than block
// on a FIFO with no writer; on windows there is no such flag and no FIFO
// type, so a swapped-in symlink IS followed by this open, and the Fstat +
// os.SameFile comparison against that Lstat, performed below after this
// call returns, is the only guard on that platform. Neither platform's
// open result can be trusted alone -- the comparison below is required on
// both.
var openManifestFile = func(path string) (manifestFile, os.FileInfo, error) {
	f, err := openManifestFileForPlatform(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, info, nil
}

// readAndValidate performs T013 steps 6-7 (regular-file/boundary check,
// size cap, then read) followed by WP01's pure Validate. boundary is the
// directory a directory-probed candidate must resolve inside, or "" when
// manifestPath was the caller's own path argument (WP03 review finding 2:
// an explicit argument gets no containment check, only the same
// regular-file requirement -- there is no directory to escape).
//
// The boundary closes two gaps found in WP03 review:
//   - finding 1/2: a symlink is rejected outright (errNotRegularFile),
//     never resolved-and-checked-for-escape; a directory-probed candidate
//     whose real location (parent symlinks included) falls outside
//     boundary is rejected as errEscapesPath. The file actually opened is
//     held to the same constraint via a post-open os.SameFile comparison
//     against the pre-open Lstat, so a link swapped into manifestPath
//     between the two calls cannot substitute a different file.
//   - finding 3: the actual read is bounded to maxManifestBytes+1 via
//     io.LimitReader regardless of what Stat/Fstat reported, so a file
//     grown after the size check is refused here instead of being read in
//     full and handed to pluginjson.Validate.
func readAndValidate(manifestPath, boundary string) (pluginjson.Report, error) {
	checkInfo, err := os.Lstat(manifestPath)
	if err != nil {
		return pluginjson.Report{}, readError(manifestPath, err)
	}
	if !checkInfo.Mode().IsRegular() {
		return pluginjson.Report{}, readError(manifestPath, errNotRegularFile)
	}
	if boundary != "" && !pathStaysWithin(boundary, manifestPath) {
		return pluginjson.Report{}, readError(manifestPath, errEscapesPath)
	}

	f, openInfo, err := openManifestFile(manifestPath)
	if err != nil {
		return pluginjson.Report{}, readError(manifestPath, err)
	}
	defer f.Close()

	if !openInfo.Mode().IsRegular() || !os.SameFile(checkInfo, openInfo) {
		return pluginjson.Report{}, readError(manifestPath, errNotRegularFile)
	}

	if openInfo.Size() > maxManifestBytes {
		return pluginjson.Report{}, readError(manifestPath, fmt.Errorf("file exceeds 5 MiB cap (%d bytes)", openInfo.Size()))
	}
	// Bounded regardless of what Stat/Fstat reported: a file grown after
	// that check (finding 3's TOCTOU) is caught here instead of being read
	// in full and handed to pluginjson.Validate, which has no visibility
	// into how much was actually requested from disk.
	data, err := io.ReadAll(io.LimitReader(f, maxManifestBytes+1))
	if err != nil {
		return pluginjson.Report{}, readError(manifestPath, err)
	}
	if len(data) > maxManifestBytes {
		return pluginjson.Report{}, readError(manifestPath, fmt.Errorf("file exceeds 5 MiB cap (%d bytes)", len(data)))
	}
	return pluginjson.Validate(data), nil
}

// displayManifestPath relativizes manifestPath to the current working
// directory for the "Validating plugin '<relative manifest path>'..." line
// (WP03 review finding 4): an absolute path or directory argument must not
// leak an absolute path into that line. The path actually opened is
// unaffected -- only this display value is relativized.
//
// The contract (kitty-specs/plugin-manifest-validate-01M21E5Q/contracts/
// cli-plugin-validate.md, commit 5097ad2, WP03 round-2 review finding 3)
// names the one case where an absolute path may appear: filepath.Rel fails
// whenever manifestPath and cwd sit on different windows volumes, and the
// sanctioned fallback there is the CLEANED absolute path, still
// slash-converted -- not the raw manifestPath as given, which could carry
// an uncleaned "..\" segment through untouched.
func displayManifestPath(manifestPath string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return absoluteSlashPath(manifestPath)
	}
	rel, err := filepath.Rel(cwd, manifestPath)
	if err != nil {
		return absoluteSlashPath(manifestPath)
	}
	return filepath.ToSlash(rel)
}

// absoluteSlashPath renders manifestPath as a cleaned absolute path with
// '/' separators -- displayManifestPath's sole fallback, per the contract
// amendment above.
func absoluteSlashPath(manifestPath string) string {
	abs, err := filepath.Abs(manifestPath)
	if err != nil {
		abs = filepath.Clean(manifestPath)
	}
	return filepath.ToSlash(abs)
}

// checkRenderOrder is the fixed output order (data-model.md, T014 step 4).
var checkRenderOrder = []pluginjson.Check{
	pluginjson.Structure,
	pluginjson.Name,
	pluginjson.Fields,
	pluginjson.Paths,
	pluginjson.Unrecognized,
}

// renderReport renders report exactly as contracts/cli-plugin-validate.md
// specifies and returns the passed/warnings/errors counts the caller needs
// for its exit-code decision (T015). Report.Findings is already ordered by
// Check, errors before warnings within a Check, both in file order
// (data-model.md) -- this function renders that order as-is, never
// re-sorting.
func renderReport(w io.Writer, report pluginjson.Report, verbose bool) (passed, warnings, errs int) {
	if verbose {
		for _, field := range report.KnownFieldsPresent {
			ux.Info(w, "%s", field)
		}
	}
	fmt.Fprintln(w)
	ux.Info(w, "Validation Results:")

	byCheck := make(map[pluginjson.Check][]pluginjson.Finding, len(checkRenderOrder))
	for _, f := range report.Findings {
		byCheck[f.Check] = append(byCheck[f.Check], f)
	}

	order := checkRenderOrder
	if report.StructureFailed {
		order = checkRenderOrder[:1]
	}
	for _, check := range order {
		findings := byCheck[check]
		if len(findings) == 0 {
			ux.Success(w, "%s: passed", check)
			passed++
			continue
		}
		for _, f := range findings {
			switch f.Level {
			case pluginjson.LevelError:
				ux.Error(w, "%s: %s", check, f.Message)
				errs++
			case pluginjson.LevelWarning:
				ux.Warn(w, "%s: %s", check, f.Message)
				warnings++
			}
		}
	}

	fmt.Fprintln(w)
	ux.Info(w, "Summary: %d passed, %d warnings, %d errors", passed, warnings, errs)
	return passed, warnings, errs
}

// manifestLocation is locatePluginManifest's result: the candidate path to
// read, plus the directory readAndValidate must hold it inside (boundary),
// or "" when path was the caller's own argument and no directory-escape
// check applies (WP03 review finding 2).
type manifestLocation struct {
	path     string
	boundary string
}

// locatePluginManifest resolves path to exactly one manifest candidate by
// NAME (T013): a directory is probed via bundle.PluginJSONCandidates in
// upstream order; anything else that exists is that candidate. Locating
// deliberately does not check the candidate's type or containment -- per
// contracts/cli-plugin-validate.md, those are read-boundary failures
// (readAndValidate's job, reported after the progress line), not location
// failures. Lstat failing (path does not exist) falls through to directory
// probing, which then fails every candidate too, producing the same "not
// found" message with path as <dir> -- a reasonable, untested-but-safe
// fallback for a typo'd path.
func locatePluginManifest(path string) (manifestLocation, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return locateInDir(path)
	}
	if info.IsDir() {
		return locateInDir(path)
	}
	return manifestLocation{path: path}, nil
}

// locateInDir implements T013 steps 3-5: the first candidate whose Lstat
// succeeds is selected by name alone -- selection never falls back to the
// next candidate because the selected one turns out to be disallowed;
// NFR-003 fails that selection's read outright (via readAndValidate),
// exactly as if no candidate existing at all, but only after the progress
// line has named it.
func locateInDir(dir string) (manifestLocation, error) {
	for _, candidate := range bundle.PluginJSONCandidates {
		candPath := filepath.Join(dir, candidate)
		if _, err := os.Lstat(candPath); err == nil {
			return manifestLocation{path: candPath, boundary: dir}, nil
		}
	}
	return manifestLocation{}, notFoundError(dir)
}

// pathStaysWithin resolves candPath's real location BEFORE any read --
// following every symlink in the chain, parent directory components
// included -- and reports whether it stays inside dir's tree (NFR-003:
// never follow a symlink to outside path). This runs unconditionally, not
// only when candPath's own Lstat shows a symlink: a symlinked PARENT
// directory (e.g. dir/.claude-plugin -> /outside) escapes the boundary just
// as surely as a symlinked leaf, and checking only the leaf's mode bit, as
// the pre-fix code did, missed it entirely (WP03 review finding 1). A
// target that cannot be resolved (broken link, permission error) is treated
// as escaping -- there is nothing safe to read through it either way.
func pathStaysWithin(dir, candPath string) bool {
	resolved, err := filepath.EvalSymlinks(candPath)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absDir, absResolved)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// notFoundError renders the exact "no plugin.json found" message
// (contracts/cli-plugin-validate.md): dir is the directory argument AS
// GIVEN by the user (or "." if defaulted), never resolved to an absolute
// path.
func notFoundError(dir string) error {
	return fmt.Errorf("no plugin.json found in %s (looked in %s)", dir, candidateListText())
}

// candidateListText renders bundle.PluginJSONCandidates with forward
// slashes regardless of host OS: the message is documentation-style text,
// not a filesystem path, and filepath.Join's candidates use the OS
// separator (backslash on Windows), which would otherwise leak into
// output the contract fixes with forward slashes.
func candidateListText() string {
	parts := make([]string, len(bundle.PluginJSONCandidates))
	for i, c := range bundle.PluginJSONCandidates {
		parts[i] = filepath.ToSlash(c)
	}
	return strings.Join(parts, ", ")
}
