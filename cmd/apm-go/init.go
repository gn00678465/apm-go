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

var promptTargetsOrdered = []string{
	"copilot", "claude", "opencode", "codex", "antigravity",
}

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

			if apmYmlExists {
				if yes || force {
					fmt.Fprintln(os.Stderr, "--yes specified, overwriting apm.yml...")
				} else if ux.CanPrompt() {
					ok, err := ux.Confirm("apm.yml already exists. Continue and overwrite?", false)
					if err != nil {
						return fmt.Errorf("confirm overwrite: %w", err)
					}
					if !ok {
						fmt.Fprintln(os.Stderr, "Initialization cancelled.")
						return nil
					}
				} else {
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
				fmt.Fprintln(os.Stderr, "\nSetting up your APM project...")
				fmt.Fprintln(os.Stderr, "Press ^C at any time to quit.")
				fmt.Fprintln(os.Stderr)

				name, err = ux.InputText("Project name", filepath.Base(cwd))
				if err != nil {
					return fmt.Errorf("read project name: %w", err)
				}
				version, err = ux.InputText("Version", "1.0.0")
				if err != nil {
					return fmt.Errorf("read version: %w", err)
				}
				description, err = ux.InputText("Description", fmt.Sprintf("APM project for %s", name))
				if err != nil {
					return fmt.Errorf("read description: %w", err)
				}
				author, err = ux.InputText("Author", manifest.DetectAuthor())
				if err != nil {
					return fmt.Errorf("read author: %w", err)
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
				selectedTargets, err = interactiveTargetSelect(detected, existingTargets)
				if err != nil {
					return fmt.Errorf("select targets: %w", err)
				}
			}

			// Phase 5: Confirmation
			if !yes && ux.CanPrompt() {
				fmt.Fprintln(os.Stderr)
				ux.Section(os.Stderr, "About to create")
				items := []ux.Item{
					{Text: fmt.Sprintf("name:        %s", name)},
					{Text: fmt.Sprintf("version:     %s", version)},
					{Text: fmt.Sprintf("description: %s", description)},
					{Text: fmt.Sprintf("author:      %s", author)},
				}
				if len(selectedTargets) > 0 {
					items = append(items, ux.Item{Text: fmt.Sprintf("targets:     %s", strings.Join(selectedTargets, ", "))})
				} else {
					items = append(items, ux.Item{Text: "targets:     (none — auto-detect at compile time)"})
				}
				ux.BulletList(os.Stderr, items)

				ok, err := ux.Confirm("Is this OK?", true)
				if err != nil {
					return fmt.Errorf("confirm creation: %w", err)
				}
				if !ok {
					fmt.Fprintln(os.Stderr, "Aborted.")
					return nil
				}
			}

			// Phase 6: File generation
			data := buildManifestData(name, version, description, author, selectedTargets)
			raw, err := yamllib.Marshal(data)
			if err != nil {
				return fmt.Errorf("serialize: %w", err)
			}
			node, err := yamlcore.SafeLoad(raw)
			if err != nil {
				return fmt.Errorf("generated manifest is invalid: %w", err)
			}
			if _, _, err := manifest.ParseManifest(node); err != nil {
				return fmt.Errorf("generated manifest fails validation: %w", err)
			}
			out, err := yamlcore.SafeDump(node)
			if err != nil {
				return fmt.Errorf("serialize: %w", err)
			}
			if err := os.WriteFile("apm.yml", out, 0644); err != nil {
				return fmt.Errorf("write apm.yml: %w", err)
			}

			// Phase 7: Success output
			fmt.Fprintln(os.Stderr)
			ux.Success(os.Stderr, "APM project initialized successfully!")
			fmt.Fprintln(os.Stderr, "\nNext steps:")
			fmt.Fprintln(os.Stderr, "  * Install a package:  apm-go install <owner>/<repo>")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive prompts and use auto-detected defaults")
	cmd.Flags().StringVar(&targetFlag, "target", "", "Comma-separated target list (skip prompt)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing apm.yml (alias for --yes on overwrite)")
	return cmd
}

func buildManifestData(name, version, description, author string, targets []string) map[string]any {
	data := map[string]any{
		"name":        name,
		"version":     version,
		"description": description,
		"author":      author,
	}
	if len(targets) > 0 {
		data["target"] = targets
	}
	data["dependencies"] = map[string]any{
		"apm": []any{},
		"mcp": []any{},
	}
	data["includes"] = "auto"
	data["scripts"] = map[string]any{}
	return data
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
func interactiveTargetSelect(detected, existing []string) ([]string, error) {
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

	opts := make([]ux.Option, len(promptTargetsOrdered))
	for i, t := range promptTargetsOrdered {
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

	for {
		selected, err := ux.MultiSelect("Select targets for this project", opts)
		if err != nil {
			return nil, err
		}
		if len(selected) > 0 {
			return selected, nil
		}

		ux.Warn(os.Stderr, "No targets selected. APM will auto-detect targets from your filesystem on every compile.")
		cont, err := ux.Confirm("Continue without pinning targets?", true)
		if err != nil {
			return nil, err
		}
		if cont {
			return nil, nil
		}
	}
}

func readExistingTargets() []string {
	data, err := os.ReadFile("apm.yml")
	if err != nil {
		return nil
	}
	var doc map[string]any
	if err := yamllib.Unmarshal(data, &doc); err != nil {
		return nil
	}
	switch v := doc["target"].(type) {
	case string:
		return []string{v}
	case []any:
		var result []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}
