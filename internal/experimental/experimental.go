// Package experimental gates opt-in, potentially-unstable features behind named
// flags, mirroring the original apm-cli `apm experimental` subsystem. Flags are
// persisted in the user config (`$APM_CONFIG_DIR/config.json`, default
// `~/.apm/config.json`) so `apm experimental enable <flag>` is sticky.
//
// Security invariant (matches the original): experimental flags MUST NOT gate
// security-critical behaviour (hash/lockfile integrity, token handling, path
// validation). They gate feature AVAILABILITY only — when a flag is on, every
// security control still runs.
package experimental

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Flag describes one experimental feature. default is always off.
type Flag struct {
	Name        string
	Description string
	Hint        string
}

// flags is the static registry of known experimental features.
var flags = map[string]Flag{
	"registries": {
		Name:        "registries",
		Description: "Enable REST-based APM package registries in apm.yml.",
		Hint:        "Use registries: in apm.yml. See https://microsoft.github.io/apm/guides/registries/",
	},
}

// Known returns the descriptor for a flag name.
func Known(name string) (Flag, bool) {
	f, ok := flags[name]
	return f, ok
}

// All returns every known flag, sorted by name.
func All() []Flag {
	out := make([]Flag, 0, len(flags))
	for _, f := range flags {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func configPath() (string, error) {
	dir := os.Getenv("APM_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		dir = filepath.Join(home, ".apm")
	}
	return filepath.Join(dir, "config.json"), nil
}

type config struct {
	Experimental map[string]bool `json:"experimental,omitempty"`
}

// load reads the config, returning an empty config on any read/parse error
// (missing or corrupt config never blocks unrelated commands).
func load() config {
	var c config
	p, err := configPath()
	if err != nil {
		return c
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(data, &c)
	return c
}

func save(c config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// IsEnabled reports whether an experimental flag is enabled.
func IsEnabled(name string) bool {
	return load().Experimental[name]
}

// Enable turns on a known experimental flag and persists it.
func Enable(name string) error {
	if _, ok := flags[name]; !ok {
		return unknownFlag(name)
	}
	c := load()
	if c.Experimental == nil {
		c.Experimental = map[string]bool{}
	}
	c.Experimental[name] = true
	return save(c)
}

// Disable turns off a known experimental flag and persists it.
func Disable(name string) error {
	if _, ok := flags[name]; !ok {
		return unknownFlag(name)
	}
	c := load()
	delete(c.Experimental, name)
	return save(c)
}

// RequireEnabled returns a remediation error when the feature is off.
func RequireEnabled(name string) error {
	if IsEnabled(name) {
		return nil
	}
	return fmt.Errorf("experimental feature %q is not enabled; enable it with: apm-go experimental enable %s", name, name)
}

func unknownFlag(name string) error {
	return fmt.Errorf("unknown experimental feature %q", name)
}
