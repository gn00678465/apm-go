package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/apm-go/apm/internal/marketplace/build"
)

// `pack --json`'s output contract (ticket 17 phase 5), mirroring the
// Oracle's own envelope literal (commands/pack.py:530-541) and
// BuildReport.failure_to_json_dict (marketplace/builder.py:263-288).
//
// These are structs rather than maps on purpose: encoding/json emits map
// keys in sorted order, while the Oracle emits Python dict insertion order.
// Field declaration order below IS the wire order, and must stay in the
// Oracle's sequence.
type packJSONEnvelope struct {
	OK               bool                    `json:"ok"`
	DryRun           bool                    `json:"dry_run"`
	Warnings         []string                `json:"warnings"`
	Errors           []packJSONError         `json:"errors"`
	Marketplace      packJSONMarketplace     `json:"marketplace"`
	Bundle           any                     `json:"bundle"`
	PluginManifests  packJSONPluginManifests `json:"plugin_manifests"`
	VersionAlignment any                     `json:"version_alignment"`
	Drift            any                     `json:"drift"`
}

// packJSONFailure is the pre-build failure shape. It deliberately has FEWER
// keys than packJSONEnvelope: failure_to_json_dict stops at "bundle" and
// never carries plugin_manifests/version_alignment/drift, because a failure
// that prevents the build from starting has no producer or gate results to
// report.
type packJSONFailure struct {
	OK          bool                `json:"ok"`
	DryRun      bool                `json:"dry_run"`
	Warnings    []string            `json:"warnings"`
	Errors      []packJSONError     `json:"errors"`
	Marketplace packJSONMarketplace `json:"marketplace"`
	Bundle      any                 `json:"bundle"`
}

type packJSONError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type packJSONMarketplace struct {
	Outputs []packJSONMarketplaceOutput `json:"outputs"`
}

type packJSONMarketplaceOutput struct {
	Format    string `json:"format"`
	Path      string `json:"path"`
	Added     int    `json:"added"`
	Updated   int    `json:"updated"`
	Unchanged int    `json:"unchanged"`
	Skipped   int    `json:"skipped"`
}

type packJSONPluginManifests struct {
	Written []string `json:"written"`
	Skipped []string `json:"skipped"`
	DryRun  []string `json:"dry_run"`
}

// packJSONVersionAlignment / packJSONDrift mirror
// VersionAlignmentReport.to_json_dict (marketplace/version_check.py:49-63)
// and DriftReport.to_json_dict (marketplace/drift_check.py:75-79). Both are
// null in the envelope when their gate did not run.
type packJSONVersionAlignment struct {
	Strategy string                            `json:"strategy"`
	Expected string                            `json:"expected"`
	OK       bool                              `json:"ok"`
	Packages []packJSONVersionAlignmentPackage `json:"packages"`
}

type packJSONVersionAlignmentPackage struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	OK      bool   `json:"ok"`
	Reason  string `json:"reason"`
}

type packJSONDrift struct {
	OK      bool                  `json:"ok"`
	Outputs []packJSONDriftOutput `json:"outputs"`
}

type packJSONDriftOutput struct {
	Format      string                    `json:"format"`
	Path        string                    `json:"path"`
	Status      string                    `json:"status"`
	Differences []packJSONDriftDifference `json:"differences"`
}

type packJSONDriftDifference struct {
	Path string `json:"path"`
	Old  any    `json:"old"`
	New  any    `json:"new"`
}

// emitPackJSON writes the success envelope. The Oracle uses
// json.dumps(envelope, indent=2) here -- two-space indent, and a trailing
// newline from click.echo.
func emitPackJSON(w io.Writer, env packJSONEnvelope) error {
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}

// emitPackJSONFailure writes the pre-build failure envelope and returns the
// error carrying its exit code. Unlike the success path this is COMPACT:
// _emit_json_error_or_raise calls json.dumps with no indent argument
// (commands/pack.py:74-77), then ctx.exit(1) -- so a consumer sees a single
// line, and the exit code is 1 regardless of what the failure was.
func emitPackJSONFailure(w io.Writer, code, message string) error {
	b, err := json.Marshal(packJSONFailure{
		Warnings:    []string{},
		Errors:      []packJSONError{{Code: code, Message: message}},
		Marketplace: packJSONMarketplace{Outputs: []packJSONMarketplaceOutput{}},
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%s\n", b); err != nil {
		return err
	}
	return withSilentExitCode(1, fmt.Errorf("%s", message))
}

// marketplaceOutputsJSON converts the deferred render records into the
// envelope's marketplace.outputs entries.
func marketplaceOutputsJSON(renders []marketplaceRender) []packJSONMarketplaceOutput {
	out := make([]packJSONMarketplaceOutput, 0, len(renders))
	for _, r := range renders {
		out = append(out, packJSONMarketplaceOutput{
			Format:    r.format,
			Path:      r.absPath,
			Added:     r.diff.Added,
			Updated:   r.diff.Updated,
			Unchanged: r.diff.Unchanged,
			Skipped:   r.diff.Removed,
		})
	}
	return out
}

// versionAlignmentJSON / driftJSON convert a gate report into its envelope
// payload. Both return nil (JSON null) when the gate did not run.
func versionAlignmentJSON(r *build.VersionAlignmentReport) any {
	if r == nil {
		return nil
	}
	pkgs := make([]packJSONVersionAlignmentPackage, 0, len(r.Packages))
	for _, p := range r.Packages {
		pkgs = append(pkgs, packJSONVersionAlignmentPackage{
			Path:    p.Path,
			Version: p.Version,
			OK:      p.OK,
			Reason:  p.Reason,
		})
	}
	return packJSONVersionAlignment{
		Strategy: r.Strategy,
		Expected: r.Expected,
		OK:       r.OK,
		Packages: pkgs,
	}
}

func driftJSON(r *build.DriftReport) any {
	if r == nil {
		return nil
	}
	outs := make([]packJSONDriftOutput, 0, len(r.Outputs))
	for _, o := range r.Outputs {
		diffs := make([]packJSONDriftDifference, 0, len(o.Differences))
		for _, d := range o.Differences {
			diffs = append(diffs, packJSONDriftDifference{Path: d.Path, Old: d.Old, New: d.New})
		}
		outs = append(outs, packJSONDriftOutput{
			Format:      o.Format,
			Path:        o.Path,
			Status:      o.Status,
			Differences: diffs,
		})
	}
	return packJSONDrift{OK: r.OK, Outputs: outs}
}

// packGateResults is runReleaseGates' output: which gates failed, plus the
// reports themselves so `--json` can carry their payloads. A nil report
// means that gate did not run (or was skipped for want of a marketplace
// block), which the envelope renders as null.
type packGateResults struct {
	versionFailed bool
	driftFailed   bool
	version       *build.VersionAlignmentReport
	drift         *build.DriftReport
}

// packLogWriter is where every human-facing pack line goes. Under --json
// that is stderr, leaving stdout carrying nothing but the envelope so a
// consuming pipeline can parse it directly -- the second half of the flag's
// own help text ("Emit machine-readable JSON to stdout; logs go to
// stderr"), which the Oracle implements by flipping its module-level
// _console_stderr. ux.SetConsoleStderr (called once in runPack) is the
// matching half for lines that go through ux.Error/ux.Warn/ux.Info with an
// os.Stderr writer, which otherwise get redirected to stdout.
func packLogWriter(cmd *cobra.Command, opts packOptions) io.Writer {
	if opts.jsonOutput {
		return cmd.ErrOrStderr()
	}
	return cmd.OutOrStdout()
}
