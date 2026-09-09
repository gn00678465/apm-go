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

// openRootFile is the seam both readAndValidate branches open a manifest
// through, overridable by tests (WP03 round-4 review findings 1-2): root
// is already opened at the boundary directory -- the directory-probed
// candidate's own directory, or the caller's path argument's parent
// directory -- and rel is the name to open within it, so the two branches
// share this one open implementation instead of the probed candidate going
// through os.Root while a named file argument ran a second, weaker flow
// that could follow a link swapped in outside that boundary.
//
// The size comes from Stat on this same open handle (fstat on the fd), not
// a second path-based Stat call, so a link swapped into rel after
// openRootFile returns cannot change what size gets checked.
//
// os.Root guarantees the open cannot resolve outside root, but that alone
// does not close every gap: it does not make the open non-blocking, and it
// still follows a symlink whose target stays inside root. rootOpenExtraFlags
// (plugin_validate_open_unix.go / plugin_validate_open_windows.go) supplies
// O_NONBLOCK on unix so a candidate swapped to a FIFO with no writer between
// the pre-open Lstat and this Open returns immediately instead of blocking
// forever; windows has no such flag and no FIFO type. Neither platform's
// open result can be trusted alone for the symlink case -- the Fstat +
// os.SameFile comparison performed by the caller after this returns is the
// guard both platforms still rely on for that.
var openRootFile = func(root *os.Root, rel string) (manifestFile, os.FileInfo, error) {
	f, err := root.OpenFile(rel, os.O_RDONLY|rootOpenExtraFlags, 0)
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
func readAndValidate(manifestPath, boundary string) (pluginjson.Report, error) {
	if boundary != "" {
		return readManifestInRoot(manifestPath, boundary)
	}

	// No probed directory to escape, but naming a file directly is not
	// authorisation to follow an external link swapped in during the
	// check-then-open race either (WP03 round-4 review finding 2): the
	// open below runs through the same os.Root boundary as the
	// directory-probed branch, rooted at manifestPath's own parent
	// directory, instead of a second, weaker open flow keyed on the raw
	// path alone. The regular-file check plus the post-open os.SameFile
	// comparison in finishRead remains the full policy on top of that.
	checkInfo, err := os.Lstat(manifestPath)
	if err != nil {
		return pluginjson.Report{}, readError(manifestPath, err)
	}
	if !checkInfo.Mode().IsRegular() {
		return pluginjson.Report{}, readError(manifestPath, errNotRegularFile)
	}

	root, err := os.OpenRoot(filepath.Dir(manifestPath))
	if err != nil {
		return pluginjson.Report{}, readError(manifestPath, err)
	}
	defer root.Close()

	f, openInfo, err := openRootFile(root, filepath.Base(manifestPath))
	if err != nil {
		return pluginjson.Report{}, readError(manifestPath, rootEscapeErr(err))
	}
	defer f.Close()

	return finishRead(manifestPath, checkInfo, openInfo, f)
}

// readManifestInRoot is the boundary!="" half of readAndValidate (WP03
// round-3 review, HIGH finding): the pre-fix code resolved manifestPath's
// containment with filepath.EvalSymlinks and then reopened it by path --
// two more independent lookups on top of the leading Lstat, each re-walking
// dir/.claude-plugin from scratch. A parent symlink flipped outside, then
// inside, then back to the SAME outside target across those lookups could
// pass the containment check (which observed the transient "inside" state)
// and the os.SameFile comparison (Lstat and Open observed the same outside
// file) without the escaping state ever being what containment examined --
// proven by TestPluginValidate_Finding_ParentSymlinkRaceAcrossLookups.
//
// os.OpenRoot(boundary) pins one directory handle; Lstat and openRootFile
// below both resolve rel against that SAME handle, and each resolves and
// escape-checks every component of the walk fresh, at the moment of that
// specific call -- there is no separate containment step whose verdict can
// go stale before the open it was meant to gate. This closes the parent
// case; the leaf-level race between Lstat and Open (a symlink, or a
// candidate swapped to a FIFO, between the two) is unaffected by this and
// remains os.SameFile's and rootOpenExtraFlags's job respectively, exactly
// as on the no-boundary path above.
func readManifestInRoot(manifestPath, boundary string) (pluginjson.Report, error) {
	rel, err := filepath.Rel(boundary, manifestPath)
	if err != nil {
		return pluginjson.Report{}, readError(manifestPath, errEscapesPath)
	}

	root, err := os.OpenRoot(boundary)
	if err != nil {
		return pluginjson.Report{}, readError(manifestPath, err)
	}
	defer root.Close()

	checkInfo, err := root.Lstat(rel)
	if err != nil {
		return pluginjson.Report{}, readError(manifestPath, rootEscapeErr(err))
	}
	// Symlink candidates are rejected outright, never resolved-and-checked
	// (WP03 review finding 1/2): os.Root permits a symlink that stays
	// within the root, so this check is what still enforces apm-go's
	// stricter no-symlink-candidate policy.
	if !checkInfo.Mode().IsRegular() {
		return pluginjson.Report{}, readError(manifestPath, errNotRegularFile)
	}

	f, openInfo, err := openRootFile(root, rel)
	if err != nil {
		return pluginjson.Report{}, readError(manifestPath, rootEscapeErr(err))
	}
	defer f.Close()

	return finishRead(manifestPath, checkInfo, openInfo, f)
}

// rootEscapeErr maps an os.Root containment failure to errEscapesPath so
// the read-failure message stays "outside the given path" (matching the
// pre-fix wording) regardless of which os.Root method detected the escape;
// any other os.Root error (the candidate no longer exists, a permission
// error) passes through unchanged.
//
// os.Root does not export its escape sentinel outside the os package
// (go1.27, os/file.go's unexported errPathEscapes) -- detection is by the
// one message text os.Root has used since the feature shipped, "path
// escapes from parent", which internal/rootfs/rootwriter.go's own doc
// comment already cites verbatim for this exact os.Root behavior.
func rootEscapeErr(err error) error {
	if strings.Contains(err.Error(), "path escapes from parent") {
		return errEscapesPath
	}
	return err
}

// finishRead applies the shared regular-file / os.SameFile / size-cap /
// bounded-read policy (T013 steps 6-7) to an already Lstat-checked, opened
// handle, then hands the bytes to WP01's pure Validate. Shared by both
// readAndValidate paths so this policy exists in exactly one place.
func finishRead(manifestPath string, checkInfo, openInfo os.FileInfo, f manifestFile) (pluginjson.Report, error) {
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
