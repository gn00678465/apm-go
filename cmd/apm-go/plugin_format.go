package main

import (
	"fmt"
	"strings"
)

// Plugin scaffold modes, the Go spelling of upstream's plugin_mode string
// (commands/init.py:139,149-153): "claude" is the legacy Claude-compatible
// layout (PREFERRED_PLUGIN_FORMAT = CLAUDE_PLUGIN, bundle/formats.py:31),
// "agent" is Agent Plugins v1.
const (
	pluginModeClaude = "claude"
	pluginModeAgent  = "agent"
)

// pluginFormatChoices mirrors cli_plugin_format_choices()
// (bundle/formats.py:56-58): _CLI_CHOICES minus "apm", in upstream order.
var pluginFormatChoices = []string{"plugin", "agent-plugin", "claude", "claude-plugin"}

// pluginFormatAliases mirrors _SELECTOR_ALIASES (bundle/formats.py:39-47)
// minus the pack-only "apm" entry.
var pluginFormatAliases = map[string]string{
	"plugin":        pluginModeClaude,
	"agent-plugin":  pluginModeAgent,
	"claude":        pluginModeClaude,
	"claude-plugin": pluginModeClaude,
}

// coercePluginFormat mirrors coerce_bundle_format (bundle/formats.py:60-73)
// for the plugin-init subset: normalise (trim, lowercase, space/underscore
// -> hyphen), then alias-lookup. Any miss reports Click's Choice wording
// (click.Choice.convert), because upstream's Choice validates the raw
// option BEFORE resolve_bundle_format ever sees it -- the user never
// observes coerce_bundle_format's own "Unknown bundle format" text.
func coercePluginFormat(value string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(value))
	key = strings.NewReplacer(" ", "-", "_", "-").Replace(key)
	if mode, ok := pluginFormatAliases[key]; ok {
		return mode, nil
	}
	return "", fmt.Errorf("Invalid value for '--format': '%s' is not one of %s.",
		value, quoteJoin(pluginFormatChoices))
}

// resolvePluginFormat mirrors resolve_bundle_format (bundle/formats.py:76-100):
// --format and --claude-plugin are each an explicit selector; more than one
// explicit selector is a usage error EVEN when both resolve to the same
// format. No selector falls back to PREFERRED_PLUGIN_FORMAT (claude).
//
// formatSet is "the flag was given at all" (upstream's `fmt is not None`),
// distinct from its value: an explicit `--format=` is a present-but-empty
// selector that Click's Choice rejects (Finding 1, F01/F08), not an absent
// one.
func resolvePluginFormat(format string, formatSet, claudePlugin bool) (string, error) {
	var selections []string
	if formatSet {
		mode, err := coercePluginFormat(format)
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
			strings.Join(pluginFormatSelectionText(format, formatSet, claudePlugin), ", "))
	}
	if len(selections) == 1 {
		return selections[0], nil
	}
	return pluginModeClaude, nil
}

// pluginFormatSelectionText mirrors format_selection_text
// (bundle/formats.py:103-113).
func pluginFormatSelectionText(format string, formatSet, claudePlugin bool) []string {
	var out []string
	if formatSet {
		out = append(out, "--format "+format)
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

// formatChoiceValue is a pflag.Value whose Type() renders Click's Choice
// metavar so `--help` shows "--format [plugin|agent-plugin|claude|
// claude-plugin]" instead of cobra's default "--format string" (Finding 2,
// F01). Validation is deliberately NOT done in Set: resolvePluginFormat
// owns it so the conflict/empty/unknown error texts stay in one place.
type formatChoiceValue struct{ v *string }

func (f formatChoiceValue) String() string     { return *f.v }
func (f formatChoiceValue) Set(s string) error { *f.v = s; return nil }
func (f formatChoiceValue) Type() string {
	return "[" + strings.Join(pluginFormatChoices, "|") + "]"
}
