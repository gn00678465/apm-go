package main

import (
	"os"
	"sort"
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
// and APM_CONFIG_DIR pinned to the sandbox — applied last so a case.json
// cannot point either at the invoking user's real config, intentionally or
// not. The returned map IS the env delta recorded in evidence: the runner
// never inherits the parent's full environment, so this map is the complete
// set of variables actually passed to the child.
func buildEnv(caseEnv map[string]string, home, configDir string) map[string]string {
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
	env["APM_CONFIG_DIR"] = configDir

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
