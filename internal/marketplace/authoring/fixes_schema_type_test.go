package authoring

import (
	"testing"
)

// SPEC marketplace-check-outdated-fixes §D: name and source must be YAML
// strings, judged by the resolved YAML 1.2 tag (decision D-1). Expected
// messages are the Oracle's _require_str text (yml_schema.py:453-455).

func TestLoadAuthoringConfig_NonStringName_Rejected(t *testing.T) {
	for _, tc := range []struct{ label, value string }{
		{"Int", "123"},
		{"Bool", "true"},
		{"Float", "1.0"},
		{"OctalInt", "0o17"},
		{"Sequence", "[a]"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			err := loadExpectingError(t, "    - name: "+tc.value+"\n      source: owner/repo\n      ref: main\n")
			if got, want := err.Error(), "'packages[0].name' must be a non-empty string"; got != want {
				t.Errorf("error = %q, want %q", got, want)
			}
			if !IsConfigValidationError(err) {
				t.Errorf("err = %v, want a config validation error (exit 2)", err)
			}
		})
	}
}

func TestLoadAuthoringConfig_NonStringSource_OracleMessage(t *testing.T) {
	for _, tc := range []struct{ label, value string }{
		{"Int", "123"},
		{"Sequence", "[a/b]"},
		{"Mapping", "{a: b}"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			err := loadExpectingError(t, "    - name: tool\n      source: "+tc.value+"\n      ref: main\n")
			if got, want := err.Error(), "'packages[0].source' must be a non-empty string"; got != want {
				t.Errorf("error = %q, want %q", got, want)
			}
			if !IsConfigValidationError(err) {
				t.Errorf("err = %v, want a config validation error (exit 2)", err)
			}
		})
	}
}
