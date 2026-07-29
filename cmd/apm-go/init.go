package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/ux"
	"github.com/apm-go/apm/internal/yamlcore"
	"github.com/spf13/cobra"
)

func initCmd() *cobra.Command {
	var (
		yes        bool
		targetFlag string
		force      bool
	)

	cmd := &cobra.Command{
		Use:          "init [project-name]",
		Short:        "Initialize a new APM project",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Phase 1: Project name resolution
			if len(args) > 0 && args[0] != "." {
				pn := args[0]
				if strings.ContainsAny(pn, "/\\") || pn == ".." {
					return fmt.Errorf("invalid project name %q", pn)
				}
				if err := os.MkdirAll(pn, 0755); err != nil {
					return fmt.Errorf("create directory: %w", err)
				}
				if err := os.Chdir(pn); err != nil {
					return fmt.Errorf("chdir: %w", err)
				}
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("cannot determine directory: %w", err)
			}

			// Phase 2: Existing apm.yml check
			_, existsErr := os.Stat("apm.yml")
			apmYmlExists := existsErr == nil

			// Every prompt of an interactive run is recorded on one clack-style
			// connecting line (issue #14). ck stays nil for --yes and
			// non-interactive runs, which keep their plain output verbatim.
			var ck *ux.Clack
			if !yes && ux.CanPrompt() {
				ck = ux.NewClack(os.Stderr)
				fmt.Fprintln(os.Stderr)
				ck.Banner(apmGoBanner)
				ck.Intro("Setting up your APM project")
				ck.Detail("Press ^C at any time to quit.")
				ck.Bar()
			}

			if apmYmlExists {
				switch {
				case yes || force:
					if ck != nil {
						ck.Step("apm.yml already exists", "Overwriting (--force)")
						ck.Bar()
					} else {
						ux.Info(os.Stderr, "--yes specified, overwriting apm.yml...")
					}
				case ck != nil:
					ok, err := ck.Confirm("apm.yml already exists. Continue and overwrite?", false)
					if err != nil {
						return fmt.Errorf("confirm overwrite: %w", err)
					}
					if !ok {
						ck.Outro("Initialization cancelled.")
						return nil
					}
					ck.Bar()
				default:
					return fmt.Errorf("apm.yml already exists; use --yes to overwrite")
				}
			}

			var name, version, description, author string

			// Phase 3: Metadata collection
			if yes || !ux.CanPrompt() {
				name = filepath.Base(cwd)
				version = "1.0.0"
				description = fmt.Sprintf("APM project for %s", name)
				author = manifest.DetectAuthor()
			} else {
				defaultName := filepath.Base(cwd)
				defaultDesc := fmt.Sprintf("APM project for %s", defaultName)
				fields := []ux.Field{
					{Key: "name", Label: "Project name", Default: defaultName},
					{Key: "version", Label: "Version", Default: "1.0.0"},
					{Key: "description", Label: "Description", Default: defaultDesc},
					{Key: "author", Label: "Author", Default: manifest.DetectAuthor()},
				}
				vals, formErr := ck.Form("Project metadata", fields)
				if formErr != nil {
					return fmt.Errorf("read project metadata: %w", formErr)
				}
				ck.Bar()
				name = vals["name"]
				version = vals["version"]
				description = vals["description"]
				author = vals["author"]
				// The grouped form can't recompute the description default
				// reactively, so if the user changed the name but left the
				// prefilled description untouched, keep it in sync with the
				// entered name (avoids silently writing "APM project for <cwd>"
				// when the project was renamed).
				if description == defaultDesc && name != defaultName {
					description = fmt.Sprintf("APM project for %s", name)
				}
			}

			// Phase 4: Target selection
			var selectedTargets []string

			if targetFlag != "" {
				supported := make(map[string]bool)
				for _, s := range manifest.SupportedTargets {
					supported[s] = true
				}
				for _, t := range strings.Split(targetFlag, ",") {
					t = strings.TrimSpace(t)
					if !supported[t] {
						return fmt.Errorf("target %q is not supported by init; allowed: %s",
							t, strings.Join(manifest.SupportedTargets, ", "))
					}
					selectedTargets = append(selectedTargets, t)
				}
			} else if yes || !ux.CanPrompt() {
				selectedTargets = manifest.DetectTargets(cwd)
			} else {
				var existingTargets []string
				if apmYmlExists {
					existingTargets = readExistingTargets()
				}
				detected := manifest.DetectTargets(cwd)
				selectedTargets, err = interactiveTargetSelect(ck, detected, existingTargets)
				if err != nil {
					return fmt.Errorf("select targets: %w", err)
				}
				ck.Bar()
			}

			// Phase 5: Confirmation
			if ck != nil {
				body := []string{
					fmt.Sprintf("name:        %s", name),
					fmt.Sprintf("version:     %s", version),
					fmt.Sprintf("description: %s", description),
					fmt.Sprintf("author:      %s", author),
				}
				if len(selectedTargets) > 0 {
					body = append(body, fmt.Sprintf("targets:     %s", strings.Join(selectedTargets, ", ")))
				} else {
					body = append(body, "targets:     (none — auto-detect at compile time)")
				}
				ck.Note("About to create", body)
				ck.Bar()

				ok, err := ck.Confirm("Is this OK?", true)
				if err != nil {
					return fmt.Errorf("confirm creation: %w", err)
				}
				if !ok {
					ck.Outro("Aborted.")
					return nil
				}
			}

			// Phase 6: File generation. buildManifestNode assembles the
			// document in semantic key order (R2); the validation
			// pipeline below dumps it FIRST so the bytes it validates
			// (via a round-trip through SafeLoad/ParseManifest) are
			// exactly the bytes written to disk (design.md §2).
			node := buildManifestNode(manifestSpec{
				Name:        name,
				Version:     version,
				Description: description,
				Author:      author,
				Targets:     selectedTargets,
			})
			out, err := yamlcore.SafeDump(node)
			if err != nil {
				return fmt.Errorf("serialize: %w", err)
			}
			reloaded, err := yamlcore.SafeLoad(out)
			if err != nil {
				return fmt.Errorf("generated manifest is invalid: %w", err)
			}
			if _, _, err := manifest.ParseManifest(reloaded); err != nil {
				return fmt.Errorf("generated manifest fails validation: %w", err)
			}
			if err := os.WriteFile("apm.yml", out, 0644); err != nil {
				return fmt.Errorf("write apm.yml: %w", err)
			}

			// Phase 7: Success output
			if ck != nil {
				ck.Bar()
				ck.Step("APM project initialized successfully!", "Install a package:  apm-go install <owner>/<repo>")
				ck.Outro("Done!")
				return nil
			}
			fmt.Fprintln(os.Stderr)
			ux.Success(os.Stderr, "APM project initialized successfully!")
			fmt.Fprintln(os.Stderr)
			ux.Section(os.Stderr, "Next steps")
			ux.Info(os.Stderr, "Install a package:  apm-go install <owner>/<repo>")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive prompts and use auto-detected defaults")
	cmd.Flags().StringVar(&targetFlag, "target", "", "Comma-separated target list (skip prompt)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing apm.yml (alias for --yes on overwrite)")
	return cmd
}

// interactiveTargetSelect prompts for the target list via a huh MultiSelect
// (space to toggle, matching huh's own default keybinding), pre-selecting
// every already-configured (existing) or auto-detected target. If the user
// confirms with nothing selected, it asks once more (via ux.Confirm) whether
// to proceed without pinning any target, looping back to the MultiSelect
// prompt otherwise.
//
// Any error from the underlying MultiSelect/Confirm prompts (e.g. the huh
// form is aborted with Ctrl-C) is returned to the caller immediately instead
// of being swallowed: a swallowed error previously left `selected` nil and
// `cont` at its zero value (false), which re-entered this function
// recursively on every aborted prompt -- an abort loop with no way out.
func interactiveTargetSelect(ck *ux.Clack, detected, existing []string) ([]string, error) {
	opts := targetSelectOptions(detected, existing)

	for {
		selected, err := ck.MultiSelect("Select targets for this project", opts)
		if err != nil {
			return nil, err
		}
		if len(selected) > 0 {
			return selected, nil
		}

		ck.Warn("No targets selected. APM will auto-detect targets from your filesystem on every compile.")
		cont, err := ck.Confirm("Continue without pinning targets?", true)
		if err != nil {
			return nil, err
		}
		if cont {
			return nil, nil
		}
	}
}

// targetSelectOptions builds the MultiSelect option list interactiveTargetSelect
// prompts with: one entry per manifest.SupportedTargets (R8.3 -- the menu is
// derived from that slice, not an independent literal), pre-selected when
// the target is already configured (existing) or auto-detected, and labeled
// with the detection signal when auto-detected. Split out from
// interactiveTargetSelect so tests (AC25) can inspect the option set
// actually offered to the user without driving a live prompt.
func targetSelectOptions(detected, existing []string) []ux.Option {
	checked := make(map[string]bool)
	for _, t := range existing {
		checked[t] = true
	}
	for _, t := range detected {
		checked[t] = true
	}

	detectedSet := make(map[string]bool)
	for _, t := range detected {
		detectedSet[t] = true
	}

	opts := make([]ux.Option, len(manifest.SupportedTargets))
	for i, t := range manifest.SupportedTargets {
		label := t
		if detectedSet[t] {
			for _, sig := range manifest.SignalWhitelist {
				if sig.Target == t {
					label = fmt.Sprintf("%s  (detected %s)", t, sig.Path)
					break
				}
			}
		}
		opts[i] = ux.Option{Label: label, Value: t, Selected: checked[t]}
	}
	return opts
}

// readExistingTargets reads the target selection out of an already-written
// apm.yml, so interactiveTargetSelect can pre-select it (AC2/AC3). It tries
// the same yamlcore.SafeLoad -> manifest.ParseManifest pipeline install/pack
// use to produce m.Target first, instead of a second, ad hoc parser: a bare
// type-switch over a raw yaml.Unmarshal decode (the prior implementation)
// only handled a plain list or a single bare scalar, and silently dropped
// every other form ParseManifest's parseTargetField/parseTargetsField
// already accept -- CSV sugar on the singular scalar key ("target:
// claude,copilot", manifest.go's parseTargetField) and target aliases (e.g.
// "vscode" normalizing to "copilot" via ValidateTarget).
//
// 2026-07-30 codex Tier 2 B5: ParseManifest fails the WHOLE document on any
// schema violation, even one unrelated to targets (e.g. a missing required
// version: field) -- an apm.yml that is otherwise perfectly readable for its
// target: value then loses its ENTIRE preselection, which is a regression
// against the pre-ParseManifest ad hoc parser this function replaced (that
// one could still read target: out of a version-less file). So a
// ParseManifest failure now falls back to lenientReadTargets, which reads
// only the target/targets key directly and drops individual unparseable
// tokens instead of giving up on the whole file.
func readExistingTargets() []string {
	data, err := os.ReadFile("apm.yml")
	if err != nil {
		return nil
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil
	}
	if m, _, err := manifest.ParseManifest(node); err == nil {
		return m.Target
	}
	return lenientReadTargets(node)
}

// lenientReadTargets is readExistingTargets' fallback whenever
// manifest.ParseManifest returns an error -- readExistingTargets (line 335)
// calls it unconditionally on any ParseManifest failure, not only one
// unrelated to targets. An earlier version of this comment claimed the
// opposite ("a reason unrelated to targets"); that claim does not hold: a
// failure caused BY the target/targets value itself is exactly as fatal to
// ParseManifest as an unrelated one, because manifest.go's single
// range-and-return loop (manifest.go:100-196) returns on the first error it
// hits, before reaching later checks like the required version: field
// (manifest.go:204). Concretely, `targets: [claude, {foo: bar}]` with a
// valid version: fails ParseManifest with "unknown target" for the mapping
// element (targetTokenFromNode/ValidateTarget via parseTargetsField,
// manifest.go:298-317) -- a targets-related failure -- and this function is
// still the one that recovers the still-legal "claude" entry (verified
// directly: readExistingTargets() on that fixture returns [claude]).
//
// So this function reads the target/targets key directly off the raw node
// instead of going through parseTargetField/parseTargetsField (both
// unexported, and both HARD ERROR on an unparseable/empty targets: block,
// which is the wrong behavior for a best-effort preselection reader): each
// token is normalized via manifest.ValidateTarget and a token that fails
// validation is dropped rather than aborting the whole read -- this is also
// how a targets-related ParseManifest failure (one bad token) still yields a
// partial, useful preselection instead of losing it entirely. Both target:
// and targets: present at once is invalid apm.yml (manifest.go's
// hasConflictingTargetKeys) -- this does not guess a winner, matching
// ParseManifest's own refusal.
//
// 2026-07-30 codex Tier 2: the singular target: scalar and the plural
// targets: scalar are NOT parsed the same way by the real parser, so this
// fallback must not treat them the same either. manifest.go's
// parseTargetField (target:, manifest.go:265) splits a scalar on "," --
// "target: claude,copilot" is CSV sugar for two targets. manifest.go's
// parseTargetsField (targets:, manifest.go:312-313) does NOT split a scalar
// on "," -- "targets: claude,copilot" is a single-element list whose one
// element happens to contain a comma, i.e. one (invalid) token, not CSV
// sugar. An earlier version of this function CSV-split every scalar
// unconditionally, so a plural "targets: claude,copilot" was silently
// manufactured into two preselected targets an interactive init() run never
// actually wrote for that key. Whether to split is now keyed off which of
// target:/targets: was actually present (isSingular), not the node kind
// alone.
func lenientReadTargets(doc *yamllib.Node) []string {
	if doc.Kind != yamllib.DocumentNode || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if root.Kind != yamllib.MappingNode {
		return nil
	}

	var targetVal, targetsVal *yamllib.Node
	for i := 0; i < len(root.Content)-1; i += 2 {
		switch root.Content[i].Value {
		case "target":
			targetVal = root.Content[i+1]
		case "targets":
			targetsVal = root.Content[i+1]
		}
	}
	if targetVal != nil && targetsVal != nil {
		return nil
	}
	val := targetVal
	isSingular := true
	if val == nil {
		val = targetsVal
		isSingular = false
	}
	if val == nil {
		return nil
	}

	var rawTokens []string
	switch val.Kind {
	case yamllib.ScalarNode:
		if raw := strings.TrimSpace(val.Value); raw != "" {
			if isSingular {
				rawTokens = strings.Split(raw, ",")
			} else {
				rawTokens = []string{raw}
			}
		}
	case yamllib.SequenceNode:
		for _, item := range val.Content {
			if item.Kind == yamllib.ScalarNode {
				rawTokens = append(rawTokens, item.Value)
			}
		}
	}

	var targets []string
	for _, tok := range rawTokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		normalized, err := manifest.ValidateTarget(tok)
		if err != nil {
			continue
		}
		targets = append(targets, normalized)
	}
	return targets
}
