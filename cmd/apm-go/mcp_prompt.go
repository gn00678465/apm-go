package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/apm-go/apm/internal/ux"
	"golang.org/x/term"
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
// credentials: BOTH stdin and stdout must be a TTY (matching the Python
// original's writer.py `is_tty = sys.stdin.isatty() and sys.stdout.isatty()`),
// and not a CI/E2E environment. Requiring stdout too means a piped/captured
// run (e.g. `apm-go install ... | tee`, or a script capturing output) is
// treated as non-interactive and never blocks waiting on a prompt the user
// cannot even see.
func canPromptCreds() bool {
	return isInteractive() && stdoutIsTTY() && !isNonInteractiveEnv()
}

// stdoutIsTTY reports whether os.Stdout is a character device (a terminal),
// mirroring isInteractive()'s stdin check for stdout.
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
	fmt.Fprintln(os.Stderr, "Credentials needed:")
	hdrs := collectHeaderValues(requiredHeaders, ttyAsk)
	if len(hdrs) == 0 {
		return nil
	}
	return hdrs
}

// ttyAsk prompts on stderr and reads one line from stdin. Secret values are
// read without echo via x/term so a token never lands in the terminal (or its
// scrollback) or any log.
func ttyAsk(label string, secret bool) string {
	fmt.Fprintf(os.Stderr, "  %s: ", label)
	if secret {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	scanner := getScanner()
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

// promptReplaceMCP shows the replacement diff and asks the user to confirm
// replacing an existing, differing MCP entry, defaulting to No (D2, mirroring
// the Python original's writer.py TTY branch). Passed as confirmReplaceFunc
// only when stdin is interactive.
func promptReplaceMCP(name string, diff []string) (bool, error) {
	ux.Warn(os.Stderr, "MCP server %q already exists. Replacement diff:", name)
	for _, line := range diff {
		fmt.Fprintf(os.Stderr, "%s\n", line)
	}
	return confirmPrompt(fmt.Sprintf("Replace MCP server %q?", name), false), nil
}
