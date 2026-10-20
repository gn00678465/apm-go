package main

import (
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/ux"
)

// installTargets is what install and update deploy to.
type installTargets struct {
	targets, diags []string
	// ambiguous names, sorted, the harnesses a project detected when it
	// detected more than one; targets is then empty.
	ambiguous []string
	// mcp are the targets the MCP servers go to: at --global those of
	// targets that take MCP at user scope, without the copilot taken when
	// nothing was detected at ~ (owner ruling, issue #125 comment
	// 6282389166, item 2).
	mcp []string
	// userScope is the targets of targets that take no MCP at user scope.
	userScope userScopeSkip
}

// resolveInstallTargets resolves the targets of install and update: a
// project's by the Oracle's signal table, the user scope's (deployDir set,
// HOME) by its user-scope roots.
func resolveInstallTargets(targetFlag string, manifestTargets []string, deployDir string) installTargets {
	if deployDir == "" {
		targets, diags, ambiguous := deploy.ResolveInstallTargets(targetFlag, manifestTargets, ".")
		return installTargets{targets: targets, diags: diags, ambiguous: ambiguous, mcp: targets}
	}
	targets, diags, fallback := deploy.UserScopeTargets(targetFlag, manifestTargets, deployDir)
	t := installTargets{targets: targets, diags: diags}
	// The Oracle's all gives copilot as vscode, the VS Code runtime, which
	// takes no MCP at user scope (measured, issue125 gmcp-or/m-all).
	all := slices.Contains(manifestTargets, "all")
	if targetFlag != "" {
		tokens, _ := deploy.SplitTargetFlag(targetFlag)
		all = slices.Contains(tokens, "all")
	}
	for _, name := range targets {
		switch {
		case name == "copilot" && all:
			t.userScope.add(name, "vscode")
		case !deploy.UserScopeMCP(name):
			t.userScope.add(name, name)
		case !fallback:
			t.mcp = append(t.mcp, name)
		}
	}
	slices.Sort(t.userScope.names)
	return t
}

// userScopeSkip is the targets an install at user scope configures no MCP
// server for, because the Oracle's runtime of each takes none there
// (deploy.UserScopeMCP); copilot, as all gives it, is the Oracle's vscode
// (measured, issue125 gmcp-or/m-all).
type userScopeSkip struct {
	// targets are the skipped targets.
	targets []string
	// names are the Oracle's runtime names of targets, sorted.
	names []string
}

func (u *userScopeSkip) add(target, name string) {
	u.targets = append(u.targets, target)
	u.names = append(u.names, name)
}

// warn prints the Oracle's warning for the skipped targets
// (integration/mcp_integrator_install.py:907-913).
func (u *userScopeSkip) warn() {
	if len(u.names) > 0 {
		ux.Warn(os.Stdout, "Skipped workspace-only runtimes at user scope: %s -- omit --global to install these", strings.Join(u.names, ", "))
	}
}

// hasContentToIntegrate reports whether an install of m has a package, an MCP
// server or the project's own content to integrate: the Oracle resolves the
// targets, and refuses several detected harnesses, only then
// (install/phases/targets.py; measured, issue125 proj2-or/m7).
func hasContentToIntegrate(m *manifest.Manifest) bool {
	return len(m.ParsedDeps) > 0 || len(m.ParsedDevDeps) > 0 || len(m.MCPServers) > 0 || len(m.MCPDevServers) > 0 ||
		len(deploy.CollectScopeLocalPrimitives(".", false)) > 0
}

// errMultipleHarnesses refuses a project in which several harnesses were
// detected and nothing names the targets, with exit 2, as the Oracle's
// AmbiguousHarnessError does (core/target_detection.py:1145-1146). The first
// line is the Oracle's; the rest is apm-go's own, because the Oracle's text
// names `apm targets` and `install --dry-run`, which apm-go does not have
// (owner ruling, issue #125 comment 6282389166, item 1).
func errMultipleHarnesses(ambiguous []string) error {
	var b strings.Builder
	b.WriteString("Multiple harnesses detected: " + strings.Join(ambiguous, ", ") + "\n\n")
	b.WriteString("To fix, do one of the following:\n")
	b.WriteString("  1. Pass --target <name> (e.g. --target " + ambiguous[0] + ")\n")
	b.WriteString("  2. Add a targets: field to apm.yml, e.g.:\n")
	b.WriteString("       targets:\n")
	b.WriteString("         - " + ambiguous[0] + "\n")
	return withExitCode(2, &multipleHarnessError{err: errors.New(b.String())})
}

// multipleHarnessError is errMultipleHarnesses's failure, printed with no
// usage text, as noDeployTargetError is.
type multipleHarnessError struct{ err error }

func (e *multipleHarnessError) Error() string { return e.err.Error() }
func (e *multipleHarnessError) Unwrap() error { return e.err }

// sortedTargets is the targets of the Targets: line, sorted as the Oracle's
// resolve_targets sorts them (core/target_detection.py:1121-1135).
func sortedTargets(targets []string) string {
	return strings.Join(slices.Sorted(slices.Values(targets)), ", ")
}

// printUnplacedMCP prints the MCP block of an install at user scope whose n
// MCP servers no target takes there, as the Oracle does before it fails
// (integration/mcp_integrator_install.py:895-921): the Targeting line names
// the targets, then the two warnings.
func printUnplacedMCP(n int, targets []string, explicitTarget bool, skipped *userScopeSkip) {
	ux.Plain(os.Stdout, "+- MCP Servers (%d)", n)
	how := "declared %s from apm.yml"
	if explicitTarget {
		how = "specific %s"
	}
	noun := pluralWord(len(targets), "target", "targets")
	if explicitTarget {
		noun = pluralWord(len(targets), "runtime", "runtimes")
	}
	ux.Info(os.Stdout, "Targeting "+how+": %s", noun, strings.Join(targets, ", "))
	skipped.warn()
	ux.Warn(os.Stdout, "No runtimes support user-scope MCP installation (supported: Copilot CLI, Claude Code, Codex CLI, Gemini CLI, Antigravity CLI, Hermes, Kiro, Windsurf, JetBrains Copilot)")
}
