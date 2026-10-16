package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/ux"
	"github.com/apm-go/apm/internal/yamlcore"
)

// bootstrapProjectName is the name of an apm.yml an install creates: the
// directory's name, or my-project when that name is not one a project can
// have (core/project_name.py:3-19).
func bootstrapProjectName(candidate string) string {
	if strings.TrimSpace(candidate) == "" || strings.ContainsAny(candidate, `/\`) || candidate == ".." {
		return "my-project"
	}
	return candidate
}

// newManifestBytes is the apm.yml an install creates when there is none, the
// Oracle's _get_default_config (commands/_helpers.py:629-636) with targets,
// validated as init validates the file it writes. withComments=false drops
// the targets comments: the Oracle's `install -g --mcp` writes its file again
// with yaml.safe_dump, which loses them (install/mcp/command.py:189-194).
func newManifestBytes(name string, targets []string, withComments bool) ([]byte, error) {
	node := buildManifestNode(manifestSpec{
		Name:        name,
		Version:     "1.0.0",
		Description: "APM project for " + name,
		Author:      manifest.DetectAuthor(),
		Targets:     targets,
	})
	if !withComments {
		clearComments(node)
	}
	out, err := yamlcore.SafeDumpManifest(node)
	if err != nil {
		return nil, fmt.Errorf("serialize apm.yml: %w", err)
	}
	reloaded, err := yamlcore.SafeLoad(out)
	if err != nil {
		return nil, fmt.Errorf("generated apm.yml is invalid: %w", err)
	}
	if _, _, err := manifest.ParseManifest(reloaded); err != nil {
		return nil, fmt.Errorf("generated apm.yml fails validation: %w", err)
	}
	return out, nil
}

func clearComments(n *yamllib.Node) {
	n.HeadComment, n.LineComment, n.FootComment = "", "", ""
	for _, c := range n.Content {
		clearComments(c)
	}
}

// manifestDisplay is how the install's lines name apm.yml: the Oracle names
// the user-scope file by its path below HOME (commands/install.py
// manifest_display).
func manifestDisplay(global bool) string {
	if global {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(globalDir(home), "apm.yml")
		}
	}
	return "apm.yml"
}

// manifestOwnerName is the directory an install names a created apm.yml
// after: the project directory, or for --global the home directory
// (commands/install.py:1479-1481).
func manifestOwnerName(global bool) string {
	dir, err := os.Getwd()
	if global {
		dir, err = os.UserHomeDir()
	}
	if err != nil {
		return bootstrapProjectName("")
	}
	return bootstrapProjectName(filepath.Base(dir))
}

// createInstallManifest writes the apm.yml that `install <pkg>` creates when
// there is none, with the --target value, and prints the Oracle's two lines
// (commands/install.py:1478-1498, install/manifest_helpers.py:79-98).
func createInstallManifest(targetFlag string, global bool) ([]byte, error) {
	targets := manifestTargetsFromTargetFlag(targetFlag)
	out, err := newManifestBytes(manifestOwnerName(global), targets, true)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile("apm.yml", out, 0644); err != nil {
		return nil, fmt.Errorf("write apm.yml: %w", err)
	}
	display := manifestDisplay(global)
	ux.Success(os.Stdout, "Created %s", display)
	if len(targets) > 0 {
		ux.Info(os.Stdout, "Targets set: %s (persisted to %s)", strings.Join(targets, ", "), display)
	}
	return out, nil
}

// removeCreatedManifest removes the apm.yml a failed install created, so the
// failure leaves the directory as it was (install/transaction.py:37-72).
func removeCreatedManifest() {
	if err := os.Remove("apm.yml"); err != nil {
		if !os.IsNotExist(err) {
			ux.Warn(os.Stdout, "Failed to remove apm.yml created by this install. Delete apm.yml manually before retrying.")
		}
		return
	}
	ux.Info(os.Stdout, "Removed apm.yml created by the failed install.")
}

// errNoManifest is an install with no package and no apm.yml: the Oracle's
// lines (commands/install.py:1500-1507) with apm-go in the hints.
func errNoManifest(global bool) error {
	ux.Error(os.Stdout, "No %s found", manifestDisplay(global))
	if global {
		ux.Info(os.Stdout, "Run 'apm-go install -g <org/repo>' to auto-create + install")
	} else {
		ux.Info(os.Stdout, "Run 'apm-go init' to create one, or:")
		ux.Info(os.Stdout, "  apm-go install <org/repo> to auto-create + install")
	}
	return withSilentExitCode(1, errors.New("no apm.yml found"))
}

// oracleInstallAllTargets is what the Oracle's `--target all` selects for
// install, expand_all("install") (core/target_catalog.py:276-288), before
// manifestTargetsFromTargetFlag maps each name.
var oracleInstallAllTargets = []string{
	"claude", "codex", "cursor", "gemini", "grok-build", "kiro", "opencode", "vscode", "windsurf",
}

// manifestTargetsFromTargetFlag is the targets: list an install that creates
// apm.yml writes for its --target value, the Oracle's
// manifest_targets_from_target_option (core/target_detection.py:481-499).
// It reads the raw tokens, not deploy.SplitTargetFlag's: an alias that is
// not one of copilot's runtimes (agy) is not written, as in the Oracle.
func manifestTargetsFromTargetFlag(flag string) []string {
	var out []string
	add := func(name string) {
		switch name {
		case "vscode", "agents":
			name = "copilot"
		}
		if slices.Contains(oracleManifestTargetNames, name) && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	for _, part := range strings.Split(flag, ",") {
		token := strings.TrimSpace(part)
		if token == "all" {
			for _, name := range oracleInstallAllTargets {
				add(name)
			}
			continue
		}
		add(token)
	}
	slices.Sort(out)
	return out
}
