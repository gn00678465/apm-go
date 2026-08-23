package main

import (
	"strings"
	"testing"
)

// TestResolvePluginFormat mirrors upstream resolve_bundle_format +
// coerce_bundle_format (bundle/formats.py:38-101) for the plugin-init
// subset (no "apm").
func TestResolvePluginFormat(t *testing.T) {
	cases := []struct {
		name         string
		format       string
		formatSet    bool
		claudePlugin bool
		wantMode     string
		wantErr      string
	}{
		{name: "no selector defaults to claude", wantMode: "claude"},
		{name: "explicit empty --format= is rejected like Click's Choice", formatSet: true,
			wantErr: "Invalid value for '--format': '' is not one of 'plugin', 'agent-plugin', 'claude', 'claude-plugin'."},
		{name: "plugin alias is claude", format: "plugin", formatSet: true, wantMode: "claude"},
		{name: "claude alias", format: "claude", formatSet: true, wantMode: "claude"},
		{name: "claude-plugin alias", format: "claude-plugin", formatSet: true, wantMode: "claude"},
		{name: "agent-plugin", format: "agent-plugin", formatSet: true, wantMode: "agent"},
		{name: "--claude-plugin flag alone", claudePlugin: true, wantMode: "claude"},
		{name: "uppercase normalised", format: "AGENT-PLUGIN", formatSet: true, wantMode: "agent"},
		{name: "underscore normalised", format: "agent_plugin", formatSet: true, wantMode: "agent"},
		{name: "space normalised", format: "agent plugin", formatSet: true, wantMode: "agent"},
		{name: "surrounding whitespace trimmed", format: "  claude  ", formatSet: true, wantMode: "claude"},
		{name: "apm rejected with Click Choice wording", format: "apm", formatSet: true,
			wantErr: "Invalid value for '--format': 'apm' is not one of 'plugin', 'agent-plugin', 'claude', 'claude-plugin'."},
		{name: "unknown rejected with Click Choice wording", format: "bogus", formatSet: true,
			wantErr: "Invalid value for '--format': 'bogus' is not one of 'plugin', 'agent-plugin', 'claude', 'claude-plugin'."},
		{name: "both selectors rejected even when same format", format: "claude", formatSet: true, claudePlugin: true,
			wantErr: "Choose one bundle format selector; received: --format claude, --claude-plugin"},
		{name: "both selectors rejected when different", format: "agent-plugin", formatSet: true, claudePlugin: true,
			wantErr: "Choose one bundle format selector; received: --format agent-plugin, --claude-plugin"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolvePluginFormat(tc.format, tc.formatSet, tc.claudePlugin)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil (mode=%q)", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %q, want containing %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantMode {
				t.Fatalf("mode = %q, want %q", got, tc.wantMode)
			}
		})
	}
}
