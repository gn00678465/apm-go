//go:build unix

package main

import (
	"os"
	"sort"
	"strings"
)

// allowListedEnvKeys are the only variables inherited from the invoking
// process's environment; everything else the invoker has set (credentials,
// proxies, ad-hoc debug flags) never reaches the child.
var allowListedEnvKeys = []string{"PATH", "LANG", "LC_ALL"}

// fixedEnv is applied on top of the allow-listed inherited vars so every run
// is non-interactive and undecorated regardless of the invoking terminal.
var fixedEnv = map[string]string{
	"NO_COLOR": "1",
	"CI":       "1",
	"TERM":     "dumb",
}

// buildEnv constructs the full environment for a subprocess: allow-listed
// inherited vars, then fixed vars, then the case's own overrides, then HOME
// and UV_CACHE_DIR pinned to the sandbox — applied last so a case.json
// cannot point either of them at the invoking user's real config,
// intentionally or not. UV_CACHE_DIR exists because the Oracle is launched
// via `uv run`, whose default cache lives under $HOME/.cache/uv: that is the
// launcher's artefact, not the product's, and it must stay out of the
// evidence roots (otherwise every Oracle run shows a spurious home/ tree
// diff). APM_CONFIG_DIR is deliberately NOT pinned here: the Oracle has no
// such variable at all (it always resolves its registry under $HOME/.apm),
// so forcing it only made apm-go diverge from the Oracle on registry
// location by construction (ticket 15). A case that wants to exercise
// apm-go's honouring of APM_CONFIG_DIR as an explicit override sets it via
// caseEnv like any other var, and it passes through unmodified. The
// returned map IS the env delta recorded in evidence: the runner never
// inherits the parent's full environment, so this map is the complete set
// of variables actually passed to the child.
func buildEnv(caseEnv map[string]string, home, launcherCache string) map[string]string {
	env := make(map[string]string, len(allowListedEnvKeys)+len(fixedEnv)+len(caseEnv)+2)

	for _, k := range allowListedEnvKeys {
		if v, ok := os.LookupEnv(k); ok {
			env[k] = v
		}
	}
	for k, v := range fixedEnv {
		env[k] = v
	}
	for k, v := range caseEnv {
		env[k] = v
	}
	env["HOME"] = home
	env["UV_CACHE_DIR"] = launcherCache

	return env
}

// expandCaseEnv returns a copy of caseEnv with every "<TMP>" occurrence in
// each value replaced by cwd -- the sandbox's own run cwd, not known until
// newSandbox creates it, so a case.json can only reference it by this
// placeholder (e.g. "<TMP>/altcfg" to point APM_CONFIG_DIR at a directory
// inside the run's own cwd, which is itself an evidence root).
func expandCaseEnv(caseEnv map[string]string, cwd string) map[string]string {
	if len(caseEnv) == 0 {
		return caseEnv
	}
	env := make(map[string]string, len(caseEnv))
	for k, v := range caseEnv {
		env[k] = strings.ReplaceAll(v, "<TMP>", cwd)
	}
	return env
}

// envSlice renders an env map as a sorted KEY=VALUE slice suitable for
// exec.Cmd.Env. Sorting keeps evidence byte-for-byte reproducible.
func envSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}
