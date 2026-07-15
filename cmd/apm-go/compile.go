package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/compile"
	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/ux"
	"github.com/apm-go/apm/internal/yamlcore"
	"github.com/spf13/cobra"
)

// compileCmd implements apm-go's minimal agents-family subset of the Python
// oracle's `apm compile`: it compiles local + dependency *.instructions.md
// primitives into a single project-root AGENTS.md for antigravity/codex/
// opencode (see .trellis/tasks/07-11-agents-md-compile/design.md). v1
// deliberately exposes only -t/--target -- no --dry-run/--watch/--validate/
// --root/--clean/--single-agents/--no-links/--no-constitution, all of which
// are documented non-goals (design.md §1).
func compileCmd() *cobra.Command {
	var targetFlag string
	cmd := &cobra.Command{
		Use:          "compile",
		Short:        "Compile installed instructions into a project AGENTS.md",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCompile(targetFlag, ".")
		},
	}
	cmd.Flags().StringVarP(&targetFlag, "target", "t", "",
		"explicit agents-family target(s) to compile for, comma-separated (antigravity, codex, opencode)")
	return cmd
}

func runCompile(targetFlag, projectDir string) error {
	m, err := loadCompileManifest(projectDir)
	if err != nil {
		return err
	}
	if m == nil {
		// loadCompileManifest already reported the diagnostic.
		return withExitCode(1, errors.New("not an APM project - no apm.yml found"))
	}

	if !compile.HasCompilableContent(projectDir) {
		ux.Error(os.Stderr, "No instruction files found in .apm/ directory")
		return withExitCode(1, errors.New("no instruction files found in .apm/ directory"))
	}

	resolved, _ := deploy.ResolveTargets(targetFlag, m.Target, projectDir)
	agentsTargets := compile.FilterAgentsFamily(resolved)
	if len(agentsTargets) == 0 {
		label := "none"
		if len(resolved) > 0 {
			label = strings.Join(resolved, ",")
		}
		msg := fmt.Sprintf("compile for target(s) %s not implemented in apm-go yet", label)
		ux.Error(os.Stderr, "%s", msg)
		return withExitCode(2, errors.New(msg))
	}

	result, err := compile.Run(projectDir, m)
	if err != nil {
		return fmt.Errorf("compile: %w", err)
	}
	if result.Wrote {
		ux.Success(os.Stdout, "Compiled %d instruction(s) to %s", result.InstructionCount, result.Path)
	} else {
		ux.Info(os.Stdout, "No changes detected; preserving existing AGENTS.md for idempotency")
	}
	return nil
}

// loadCompileManifest reads and parses projectDir/apm.yml. A missing
// apm.yml reports the oracle-matching diagnostic and returns (nil, nil) --
// callers exit 1 in that case (design.md §2; oracle: commands/compile/
// cli.py:347-351).
func loadCompileManifest(projectDir string) (*manifest.Manifest, error) {
	data, err := os.ReadFile(filepath.Join(projectDir, "apm.yml"))
	if err != nil {
		if os.IsNotExist(err) {
			ux.Error(os.Stderr, "Not an APM project - no apm.yml found")
			return nil, nil
		}
		return nil, fmt.Errorf("read apm.yml: %w", err)
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, fmt.Errorf("parse apm.yml: %w", err)
	}
	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		return nil, fmt.Errorf("parse apm.yml: %w", err)
	}
	return m, nil
}
