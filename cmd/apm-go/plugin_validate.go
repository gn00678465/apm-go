package main

import (
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
// (T013 step 6): the Stat here rejects an oversized file before any read,
// so pluginjson.Validate's own defensive length check is a second line of
// defense only, never the first to see an oversized manifest's bytes.
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

// runPluginValidate implements the fixed locate -> stat (size cap) -> read
// -> Validate -> render -> exit sequence (data-model.md "State
// transitions"). Any step failure prints its own ` x <message>` line and
// returns withSilentExitCode(1, ...) so main's error renderer does not
// print a second, redundant line.
func runPluginValidate(cmd *cobra.Command, path string, strict, verbose bool) error {
	w := cmd.OutOrStdout()

	manifestPath, locErr := locatePluginManifest(path)
	if locErr != nil {
		ux.Error(w, "%s", locErr)
		return withSilentExitCode(1, locErr)
	}

	displayPath := filepath.ToSlash(manifestPath)
	ux.Progress(w, "Validating plugin '%s'...", displayPath)

	report, readErr := readAndValidate(manifestPath)
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

// readAndValidate performs T013 steps 6-7 (size cap, then read) followed by
// WP01's pure Validate. An oversized file never reaches os.ReadFile: its
// Report is synthesized directly in the same shape structureFailure would
// produce, so rendering (T014) does not need to know which path produced it.
func readAndValidate(manifestPath string) (pluginjson.Report, error) {
	info, err := os.Stat(manifestPath)
	if err != nil {
		return pluginjson.Report{}, fmt.Errorf("could not read %s: %w", filepath.ToSlash(manifestPath), err)
	}
	if info.Size() > maxManifestBytes {
		return pluginjson.Report{
			Findings: []pluginjson.Finding{{
				Check:   pluginjson.Structure,
				Level:   pluginjson.LevelError,
				Message: fmt.Sprintf("file exceeds 5 MiB cap (%d bytes)", info.Size()),
			}},
			StructureFailed: true,
		}, nil
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return pluginjson.Report{}, fmt.Errorf("could not read %s: %w", filepath.ToSlash(manifestPath), err)
	}
	return pluginjson.Validate(data), nil
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

// locatePluginManifest resolves path to exactly one manifest file (T013):
// a regular file (or a symlink -- trusted as given, since the user named it
// explicitly rather than apm-go discovering it) IS the manifest; a
// directory is probed via bundle.PluginJSONCandidates in upstream order.
// Lstat failing (path does not exist) falls through to directory probing,
// which then fails every candidate too, producing the same "not found"
// message with path as <dir> -- a reasonable, untested-but-safe fallback
// for a typo'd path.
func locatePluginManifest(path string) (string, error) {
	if info, err := os.Lstat(path); err == nil && !info.IsDir() {
		return path, nil
	}
	return locateInDir(path)
}

// locateInDir implements T013 steps 3-5: the first candidate that EXISTS
// (Lstat succeeds, regular file or symlink) is selected -- selection never
// falls back to the next candidate just because the selected one turns out
// to be an escaping symlink; NFR-003 fails that selection outright, exactly
// like no candidate existing at all.
func locateInDir(dir string) (string, error) {
	var selected string
	for _, candidate := range bundle.PluginJSONCandidates {
		candPath := filepath.Join(dir, candidate)
		if _, err := os.Lstat(candPath); err == nil {
			selected = candPath
			break
		}
	}
	if selected == "" {
		return "", notFoundError(dir)
	}
	info, err := os.Lstat(selected)
	if err != nil {
		return "", notFoundError(dir)
	}
	if info.Mode()&os.ModeSymlink != 0 && !symlinkStaysWithin(dir, selected) {
		return "", notFoundError(dir)
	}
	return selected, nil
}

// symlinkStaysWithin resolves candPath's link target BEFORE any read and
// reports whether it stays inside dir's tree (NFR-003: never follow a
// symlink to outside path). A target that cannot be resolved (broken link,
// permission error) is treated as escaping -- there is nothing safe to read
// through it either way.
func symlinkStaysWithin(dir, candPath string) bool {
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
