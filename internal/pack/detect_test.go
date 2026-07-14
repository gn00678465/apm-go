package pack

import "testing"

// TestDetectOutputs_TriggerMatrix covers findings §1.3's full 9-row table:
// each column is an independent trigger, and any combination may fire
// simultaneously.
func TestDetectOutputs_TriggerMatrix(t *testing.T) {
	cases := []struct {
		name           string
		hasDeps        bool
		hasMarketplace bool
		targets        []string
		wantBundle     bool
		wantMarket     bool
		wantPlugin     bool
		wantErr        bool
	}{
		{"none -> nothing to pack", false, false, nil, false, false, false, true},
		{"deps only -> bundle", true, false, nil, true, false, false, false},
		{"marketplace only", false, true, nil, false, true, false, false},
		{"target claude only -> plugin manifest", false, false, []string{"claude"}, false, false, true, false},
		{"deps + marketplace", true, true, nil, true, true, false, false},
		{"deps + target claude", true, false, []string{"claude"}, true, false, true, false},
		{"marketplace + target copilot", false, true, []string{"copilot"}, false, true, true, false},
		{"all three", true, true, []string{"claude", "copilot"}, true, true, true, false},
		{"target codex only -> nothing to pack", false, false, []string{"codex"}, false, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bundle, marketplace, pluginManifest, err := DetectOutputs(tc.hasDeps, tc.hasMarketplace, tc.targets)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("DetectOutputs(%v, %v, %v) = nil error, want ErrNothingToPack", tc.hasDeps, tc.hasMarketplace, tc.targets)
				}
				if err != ErrNothingToPack {
					t.Errorf("err = %v, want ErrNothingToPack", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("DetectOutputs(%v, %v, %v) unexpected error: %v", tc.hasDeps, tc.hasMarketplace, tc.targets, err)
			}
			if bundle != tc.wantBundle || marketplace != tc.wantMarket || pluginManifest != tc.wantPlugin {
				t.Errorf("DetectOutputs(%v, %v, %v) = (%v, %v, %v), want (%v, %v, %v)",
					tc.hasDeps, tc.hasMarketplace, tc.targets,
					bundle, marketplace, pluginManifest,
					tc.wantBundle, tc.wantMarket, tc.wantPlugin)
			}
		})
	}
}

// TestDetectOutputs_TargetsNonPluginEcosystemDoesNotTrigger locks the P0
// quick-win's original gap: a target: value that is neither claude nor
// copilot (e.g. codex/cursor/opencode) must never trigger
// PluginManifestProducer, even combined with other non-triggering targets.
func TestDetectOutputs_TargetsNonPluginEcosystemDoesNotTrigger(t *testing.T) {
	_, _, pluginManifest, err := DetectOutputs(false, false, []string{"codex", "cursor", "opencode"})
	if err != ErrNothingToPack {
		t.Fatalf("err = %v, want ErrNothingToPack", err)
	}
	if pluginManifest {
		t.Error("pluginManifest = true for an all-non-plugin-ecosystem target list")
	}
}
