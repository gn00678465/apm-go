package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// Bundle/plugin scaffold modes, the Go spelling of upstream's plugin_mode
// string (commands/init.py:139,149-153) and BundleFormat's canonical values
// (bundle/formats.py:20-36): "claude" is the legacy Claude-compatible layout
// (PREFERRED_PLUGIN_FORMAT = CLAUDE_PLUGIN, bundle/formats.py:31), "agent"
// is Agent Plugins v1, "apm" is the plain APM package bundle (pack-only,
// ticket 07).
const (
	pluginModeClaude = "claude"
	pluginModeAgent  = "agent"
	bundleModeApm    = "apm"
)

// pluginFormatChoices mirrors cli_plugin_format_choices()
// (bundle/formats.py:56-58): _CLI_CHOICES minus "apm", in upstream order.
// `plugin init` uses this list -- it does not offer the plain APM bundle.
var pluginFormatChoices = []string{"plugin", "agent-plugin", "claude", "claude-plugin"}

// packFormatChoices mirrors the full _CLI_CHOICES (bundle/formats.py:38):
// pluginFormatChoices plus "apm". Ready for `pack` (ticket 07); not wired
// into any command flag yet.
var packFormatChoices = []string{"plugin", "agent-plugin", "claude", "claude-plugin", "apm"}

// bundleFormatAliases mirrors _SELECTOR_ALIASES (bundle/formats.py:39-47) --
// the one canonical mapping from every CLI choice spelling to its mode.
// This must stay the ONLY definition of this table in the package (see
// TestBundleFormatAliasesSingleDefinition): coerceBundleFormat scopes it
// per call by intersecting with the caller's choices, so a narrower list
// (e.g. pluginFormatChoices) still rejects "apm" even though the table
// itself knows how to resolve it.
var bundleFormatAliases = map[string]string{
	"plugin":        pluginModeClaude,
	"agent-plugin":  pluginModeAgent,
	"claude":        pluginModeClaude,
	"claude-plugin": pluginModeClaude,
	"apm":           bundleModeApm,
}

// normalizeBundleFormatKey mirrors coerce_bundle_format's normalisation
// (bundle/formats.py:60-73): trim, lowercase, space/underscore -> hyphen.
// This is what makes "AGENT-PLUGIN", "agent_plugin", and "agent plugin" all
// resolve the same way.
func normalizeBundleFormatKey(value string) string {
	key := strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer(" ", "-", "_", "-").Replace(key)
}

// coerceBundleFormat mirrors coerce_bundle_format (bundle/formats.py:60-73),
// scoped to the caller's choice list. Any miss reports Click's Choice
// wording (click.Choice.convert) rendered from `choices`, because upstream's
// Choice validates the raw option BEFORE resolve_bundle_format ever sees it
// -- the user never observes coerce_bundle_format's own "Unknown bundle
// format" text.
func coerceBundleFormat(value string, choices []string) (string, error) {
	key := normalizeBundleFormatKey(value)
	for _, choice := range choices {
		if key != choice {
			continue
		}
		if mode, ok := bundleFormatAliases[choice]; ok {
			return mode, nil
		}
	}
	return "", fmt.Errorf("Invalid value for '--format': '%s' is not one of %s.",
		value, quoteJoin(choices))
}

// resolveBundleFormat mirrors resolve_bundle_format (bundle/formats.py:76-100):
// --format and --claude-plugin are each an explicit selector; more than one
// explicit selector is a usage error EVEN when both resolve to the same
// format. No selector falls back to PREFERRED_PLUGIN_FORMAT (claude).
//
// valueSet is "the flag was given at all" (upstream's `fmt is not None`),
// distinct from its value: an explicit `--format=` is a present-but-empty
// selector that Click's Choice rejects (Finding 1, F01/F08), not an absent
// one. `choices` scopes both validation and the rendered error text to the
// calling command's own selector list (e.g. pluginFormatChoices vs.
// packFormatChoices).
func resolveBundleFormat(value string, valueSet, claudePlugin bool, choices []string) (string, error) {
	var selections []string
	if valueSet {
		mode, err := coerceBundleFormat(value, choices)
		if err != nil {
			return "", err
		}
		selections = append(selections, mode)
	}
	if claudePlugin {
		selections = append(selections, pluginModeClaude)
	}
	if len(selections) > 1 {
		return "", fmt.Errorf("Choose one bundle format selector; received: %s",
			strings.Join(bundleFormatSelectionText(value, valueSet, claudePlugin), ", "))
	}
	if len(selections) == 1 {
		return selections[0], nil
	}
	return pluginModeClaude, nil
}

// resolvePluginFormat is `plugin init`'s binding of resolveBundleFormat to
// the 4-choice list (no "apm"); behaviour is unchanged from before this
// file was generalised into a shared resolver (ticket 04).
func resolvePluginFormat(format string, formatSet, claudePlugin bool) (string, error) {
	return resolveBundleFormat(format, formatSet, claudePlugin, pluginFormatChoices)
}

// bundleFormatSelectionText mirrors format_selection_text
// (bundle/formats.py:103-113).
func bundleFormatSelectionText(value string, valueSet, claudePlugin bool) []string {
	var out []string
	if valueSet {
		out = append(out, "--format "+value)
	}
	if claudePlugin {
		out = append(out, "--claude-plugin")
	}
	return out
}

// quoteJoin renders Click's Choice list: 'a', 'b', 'c'.
func quoteJoin(items []string) string {
	q := make([]string, len(items))
	for i, it := range items {
		q[i] = "'" + it + "'"
	}
	return strings.Join(q, ", ")
}

// bundleFormatChoiceValue is a pflag.Value whose Type() renders Click's
// Choice metavar so `--help` shows e.g. "--format [plugin|agent-plugin|
// claude|claude-plugin]" instead of cobra's default "--format string"
// (Finding 2, F01). Validation is deliberately NOT done in Set:
// resolveBundleFormat owns it so the conflict/empty/unknown error texts
// stay in one place. Shared by any command with its own choice list (e.g.
// `pack`, ticket 07).
type bundleFormatChoiceValue struct {
	v       *string
	choices []string
}

func (f bundleFormatChoiceValue) String() string     { return *f.v }
func (f bundleFormatChoiceValue) Set(s string) error { *f.v = s; return nil }
func (f bundleFormatChoiceValue) Type() string {
	return "[" + strings.Join(f.choices, "|") + "]"
}

// setBundleFormatFlagErrorFunc maps cobra's plain "flag needs an argument"
// parse error to Click's usage-error wording and exit code 2 ("Option
// '--format' requires an argument."). Shared by any command using the
// --format/--claude-plugin selector pair.
func setBundleFormatFlagErrorFunc(cmd *cobra.Command) {
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		if name, ok := strings.CutPrefix(err.Error(), "flag needs an argument: "); ok {
			return withExitCode(2, fmt.Errorf("Option '%s' requires an argument.", name))
		}
		return withExitCode(2, err)
	})
}
