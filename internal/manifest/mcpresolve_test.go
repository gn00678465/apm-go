package manifest

import "testing"

func lookupMap(m map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := m[name]
		return v, ok
	}
}

func TestResolvePlaceholders_Bake(t *testing.T) {
	env := lookupMap(map[string]string{"TOKEN": "secret123"})

	tests := []struct {
		name       string
		value      string
		pos        FieldPos
		wantOut    string
		wantDiags  int
		wantRefuse bool
		wantOmit   bool
	}{
		{"env-dict defined", "${TOKEN}", PosEnvDict, "secret123", 0, false, false},
		{"env-dict defined env: form", "${env:TOKEN}", PosEnvDict, "secret123", 0, false, false},
		{"env-dict undefined", "${MISSING}", PosEnvDict, "${MISSING}", 1, false, true},
		{"header defined", "Bearer ${TOKEN}", PosHeader, "Bearer secret123", 0, false, false},
		{"header undefined", "Bearer ${MISSING}", PosHeader, "Bearer ${MISSING}", 1, false, true},
		{"registry-list defined", "${TOKEN}", PosRegistryList, "secret123", 0, false, false},
		{"registry-list undefined, no diag", "${MISSING}", PosRegistryList, "${MISSING}", 0, false, true},
		{"url defined", "https://x/${TOKEN}", PosURL, "https://x/secret123", 0, false, false},
		{"url undefined", "https://x/${MISSING}", PosURL, "https://x/${MISSING}", 1, true, false},
		{"args defined not resolved", "--token=${TOKEN}", PosArgs, "--token=${TOKEN}", 0, false, false},
		{"args undefined not resolved, no diag", "--token=${MISSING}", PosArgs, "--token=${MISSING}", 0, false, false},
		{"input any pos refuses", "${input:api-key}", PosEnvDict, "${input:api-key}", 1, true, false},
		{"input in args still refuses", "${input:api-key}", PosArgs, "${input:api-key}", 1, true, false},
		{"actions preserved", "${{ secrets.TOKEN }}", PosEnvDict, "${{ secrets.TOKEN }}", 0, false, false},
		{"actions mixed with env var only resolves env", "${{ secrets.X }} ${TOKEN}", PosEnvDict, "${{ secrets.X }} secret123", 0, false, false},
		{"actions immediately adjacent to env var, no separator", "${{x}}${TOKEN}", PosEnvDict, "${{x}}secret123", 0, false, false},
		{"env-shaped text inside actions is not resolved", "${{ '${TOKEN}' }}", PosEnvDict, "${{ '${TOKEN}' }}", 0, false, false},
		{"input-shaped text inside actions does not refuse", "${{ '${input:x}' }}", PosEnvDict, "${{ '${input:x}' }}", 0, false, false},
		{"no placeholder passthrough", "plain-value", PosEnvDict, "plain-value", 0, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, diags, refuse, omit := ResolvePlaceholders(tt.value, ResolveBake, tt.pos, env)
			if out != tt.wantOut {
				t.Errorf("out = %q, want %q", out, tt.wantOut)
			}
			if len(diags) != tt.wantDiags {
				t.Errorf("diags = %v (%d), want %d", diags, len(diags), tt.wantDiags)
			}
			if refuse != tt.wantRefuse {
				t.Errorf("refuse = %v, want %v", refuse, tt.wantRefuse)
			}
			if omit != tt.wantOmit {
				t.Errorf("omit = %v, want %v", omit, tt.wantOmit)
			}
		})
	}
}

func TestResolvePlaceholders_Translate(t *testing.T) {
	env := lookupMap(map[string]string{"TOKEN": "secret123"})

	tests := []struct {
		name    string
		value   string
		pos     FieldPos
		wantOut string
	}{
		{"env var left verbatim even when defined", "${TOKEN}", PosEnvDict, "${TOKEN}"},
		{"env: form left verbatim", "${env:TOKEN}", PosEnvDict, "${env:TOKEN}"},
		{"undefined var left verbatim, no lookup needed", "${MISSING}", PosEnvDict, "${MISSING}"},
		{"input left verbatim", "${input:api-key}", PosEnvDict, "${input:api-key}"},
		{"actions preserved", "${{ secrets.TOKEN }}", PosEnvDict, "${{ secrets.TOKEN }}"},
		{"url placeholder left verbatim", "https://x/${TOKEN}", PosURL, "https://x/${TOKEN}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, diags, refuse, omit := ResolvePlaceholders(tt.value, ResolveTranslate, tt.pos, env)
			if out != tt.wantOut {
				t.Errorf("out = %q, want %q", out, tt.wantOut)
			}
			if len(diags) != 0 {
				t.Errorf("diags = %v, want none", diags)
			}
			if refuse {
				t.Error("refuse should always be false in translate mode")
			}
			if omit {
				t.Error("omit should always be false in translate mode")
			}
		})
	}
}

// TestResolvePlaceholders_NoSentinelCollision guards against a
// mask-by-sentinel implementation that would corrupt an authored value
// containing NUL bytes; the index-based approach never rewrites value.
func TestResolvePlaceholders_NoSentinelCollision(t *testing.T) {
	env := lookupMap(map[string]string{"TOKEN": "secret123"})
	value := "\x00A0\x00${{x}}"
	out, _, refuse, omit := ResolvePlaceholders(value, ResolveBake, PosEnvDict, env)
	if out != value {
		t.Errorf("out = %q, want value untouched %q (sentinel collision corrupted authored NUL bytes)", out, value)
	}
	if refuse || omit {
		t.Errorf("refuse=%v omit=%v, want both false", refuse, omit)
	}
}

func TestHasPlaceholder(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"${TOKEN}", true},
		{"${env:TOKEN}", true},
		{"${input:api-key}", true},
		{"${{ secrets.TOKEN }}", true},
		{"plain-literal-secret", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			if got := HasPlaceholder(tt.value); got != tt.want {
				t.Errorf("HasPlaceholder(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
