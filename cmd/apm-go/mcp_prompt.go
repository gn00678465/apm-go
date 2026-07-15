package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/apm-go/apm/internal/ux"
)

// looksSecret reports whether a header/variable name denotes a credential, so
// its value is read without echo. Mirrors the Python original's keyword set
// (registry/operations.py).
func looksSecret(name string) bool {
	n := strings.ToLower(name)
	for _, kw := range []string{"token", "key", "secret", "password", "api"} {
		if strings.Contains(n, kw) {
			return true
		}
	}
	return false
}

// isNonInteractiveEnv reports whether we're in CI or an apm E2E test run,
// where interactive credential prompts must be skipped (matching the Python
// original's env detection) so an install never blocks a pipeline waiting on
// stdin.
func isNonInteractiveEnv() bool {
	for _, v := range []string{"APM_E2E_TESTS", "CI", "GITHUB_ACTIONS", "TRAVIS", "JENKINS_URL", "BUILDKITE"} {
		if os.Getenv(v) != "" {
			return true
		}
	}
	return false
}

// canPromptCreds reports whether it's safe to interactively prompt for MCP
// credentials: ux.CanPrompt() (real term.IsTerminal on stdin+stderr, not CI)
// must hold, stdout must ALSO be a TTY (matching the Python original's
// writer.py `is_tty = sys.stdin.isatty() and sys.stdout.isatty()`), and it
// must not be a CI/E2E environment. Requiring stdout too means a
// piped/captured run (e.g. `apm-go install ... | tee`, or a script capturing
// output) is treated as non-interactive and never blocks waiting on a prompt
// the user cannot even see.
func canPromptCreds() bool {
	return ux.CanPrompt() && stdoutIsTTY() && !isNonInteractiveEnv()
}

// stdoutIsTTY reports whether os.Stdout is a real terminal, mirroring
// ux.CanPrompt()'s stdin/stderr check for stdout.
func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// collectHeaderValues asks `ask` for each required header's value and builds
// the header map to inject into the deployed remote. An Authorization header
// is entered as a bare token and wrapped as "Bearer <token>" (the shape
// remote MCP servers such as github-mcp-server expect); other headers take
// the raw entered value. An empty entry skips that header, so the server is
// deployed without it rather than with a blank credential.
func collectHeaderValues(requiredHeaders []string, ask func(label string, secret bool) string) map[string]string {
	headers := map[string]string{}
	for _, name := range requiredHeaders {
		if strings.EqualFold(name, "Authorization") {
			if token := ask("token", true); token != "" {
				headers["Authorization"] = "Bearer " + token
			}
			continue
		}
		if v := ask(name, looksSecret(name)); v != "" {
			headers[name] = v
		}
	}
	return headers
}

// promptRegistryHeaders interactively collects values for a registry remote's
// required headers, returning the header map to set on the deploy dep. Returns
// nil when there is nothing to prompt for or prompting is disabled
// (non-interactive / CI), so the caller can fall back to a diagnostic.
func promptRegistryHeaders(requiredHeaders []string) map[string]string {
	if len(requiredHeaders) == 0 || !canPromptCreds() {
		return nil
	}
	ux.Section(os.Stderr, "Credentials needed")
	hdrs := collectHeaderValues(requiredHeaders, ttyAsk)
	if len(hdrs) == 0 {
		return nil
	}
	return hdrs
}

// ttyAsk prompts for a single credential value. Secret values are read
// without echo via ux.Password (a masked huh Input) so a token never lands
// in the terminal (or its scrollback) or any log; non-secret values use
// ux.InputText.
func ttyAsk(label string, secret bool) string {
	if secret {
		val, err := ux.Password(label)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(val)
	}
	val, err := ux.InputText(label, "")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(val)
}

// promptReplaceMCP shows the replacement diff and asks the user to confirm
// replacing an existing, differing MCP entry, defaulting to No (D2, mirroring
// the Python original's writer.py TTY branch). Passed as confirmReplaceFunc
// only when stdin is interactive.
func promptReplaceMCP(name string, diff []string) (bool, error) {
	ux.Warn(os.Stderr, "MCP server %q already exists. Replacement diff:", name)
	items := make([]ux.Item, len(diff))
	for i, line := range diff {
		items[i] = ux.Item{Text: line}
	}
	ux.BulletList(os.Stderr, items)
	return ux.Confirm(fmt.Sprintf("Replace MCP server %q?", name), false)
}
