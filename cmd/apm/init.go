package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/manifest"
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
				} else if isInteractive() {
					if !confirmPrompt("apm.yml already exists. Continue and overwrite?", false) {
						fmt.Fprintln(os.Stderr, "Initialization cancelled.")
						return nil
					}
				} else {
					return fmt.Errorf("apm.yml already exists; use --yes to overwrite")
				}
			}

			var name, version, description, author string

			// Phase 3: Metadata collection
			if yes || !isInteractive() {
				name = filepath.Base(cwd)
				version = "1.0.0"
				description = fmt.Sprintf("APM project for %s", name)
				author = manifest.DetectAuthor()
			} else {
				fmt.Fprintln(os.Stderr, "\nSetting up your APM project...")
				fmt.Fprintln(os.Stderr, "Press ^C at any time to quit.")
				fmt.Fprintln(os.Stderr)

				name = promptWithDefault("Project name", filepath.Base(cwd))
				version = promptWithDefault("Version", "1.0.0")
				description = promptWithDefault("Description", fmt.Sprintf("APM project for %s", name))
				author = promptWithDefault("Author", manifest.DetectAuthor())
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
			} else if yes || !isInteractive() {
				selectedTargets = manifest.DetectTargets(cwd)
			} else {
				var existingTargets []string
				if apmYmlExists {
					existingTargets = readExistingTargets()
				}
				detected := manifest.DetectTargets(cwd)
				selectedTargets = interactiveTargetSelect(detected, existingTargets)
			}

			// Phase 5: Confirmation
			if !yes && isInteractive() {
				fmt.Fprintln(os.Stderr, "\n+--- About to create ---+")
				fmt.Fprintf(os.Stderr, "  name:        %s\n", name)
				fmt.Fprintf(os.Stderr, "  version:     %s\n", version)
				fmt.Fprintf(os.Stderr, "  description: %s\n", description)
				fmt.Fprintf(os.Stderr, "  author:      %s\n", author)
				if len(selectedTargets) > 0 {
					fmt.Fprintf(os.Stderr, "  targets:     %s\n", strings.Join(selectedTargets, ", "))
				} else {
					fmt.Fprintf(os.Stderr, "  targets:     (none — auto-detect at compile time)\n")
				}
				fmt.Fprintln(os.Stderr, "+-----------------------+")

				if !confirmPrompt("\nIs this OK?", true) {
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
			fmt.Fprintln(os.Stderr, "\n[*] APM project initialized successfully!")
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

func isInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

var stdinScanner *bufio.Scanner

func getScanner() *bufio.Scanner {
	if stdinScanner == nil {
		stdinScanner = bufio.NewScanner(os.Stdin)
	}
	return stdinScanner
}

func promptWithDefault(label, defaultVal string) string {
	fmt.Fprintf(os.Stderr, "%s [%s]: ", label, defaultVal)
	scanner := getScanner()
	if scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			return line
		}
	}
	return defaultVal
}

func confirmPrompt(label string, defaultYes bool) bool {
	hint := "[Y/n]"
	if !defaultYes {
		hint = "[y/N]"
	}
	fmt.Fprintf(os.Stderr, "%s %s: ", label, hint)
	scanner := getScanner()
	if scanner.Scan() {
		line := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if line == "" {
			return defaultYes
		}
		return line == "y" || line == "yes"
	}
	return defaultYes
}

func interactiveTargetSelect(detected, existing []string) []string {
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

	for {
		fmt.Fprintln(os.Stderr, "\nSelect targets for this project:")
		for i, t := range promptTargetsOrdered {
			mark := "[ ]"
			if checked[t] {
				mark = "[x]"
			}
			suffix := ""
			if detectedSet[t] {
				for _, sig := range manifest.SignalWhitelist {
					if sig.Target == t {
						suffix = fmt.Sprintf("  (detected %s)", sig.Path)
						break
					}
				}
			}
			fmt.Fprintf(os.Stderr, "  %d. %s %s%s\n", i+1, mark, t, suffix)
		}

		fmt.Fprintln(os.Stderr, "\n[i] Type a number to toggle, ranges like '1-3' or '1,3,5',")
		fmt.Fprintln(os.Stderr, "    'all' / 'none' to flip every entry, or press Enter to confirm.")
		fmt.Fprintf(os.Stderr, "\nToggle (1-%d, ranges, 'all'/'none', or Enter to confirm): ", len(promptTargetsOrdered))

		scanner := getScanner()
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())

		if input == "" || input == "done" {
			break
		}

		if input == "all" || input == "none" {
			for _, t := range promptTargetsOrdered {
				checked[t] = !checked[t]
			}
			continue
		}

		indices := parseToggleInput(input, len(promptTargetsOrdered))
		for _, idx := range indices {
			t := promptTargetsOrdered[idx]
			checked[t] = !checked[t]
		}
	}

	var result []string
	for _, t := range promptTargetsOrdered {
		if checked[t] {
			result = append(result, t)
		}
	}

	if len(result) == 0 {
		fmt.Fprintln(os.Stderr, "\n[!] No targets selected. APM will auto-detect targets from your")
		fmt.Fprintln(os.Stderr, "    filesystem on every compile.")
		if confirmPrompt("Continue without pinning targets?", true) {
			return nil
		}
		return interactiveTargetSelect(detected, existing)
	}

	return result
}

func parseToggleInput(input string, max int) []int {
	var indices []int
	parts := strings.Split(input, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			lo, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
			hi, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err1 != nil || err2 != nil {
				continue
			}
			for i := lo; i <= hi && i <= max; i++ {
				if i >= 1 {
					indices = append(indices, i-1)
				}
			}
		} else {
			n, err := strconv.Atoi(part)
			if err != nil || n < 1 || n > max {
				continue
			}
			indices = append(indices, n-1)
		}
	}
	return indices
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
