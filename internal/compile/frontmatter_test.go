package compile

import "testing"

// TestParseInstruction_ApplyToScalarAndNoFrontmatter covers CMP-04: a
// string applyTo is kept verbatim, with its commas, and a file with no
// frontmatter at all yields empty applyTo + the full body.
func TestParseInstruction_ApplyToScalarAndNoFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantApplyTo string
		wantBody    string
	}{
		{
			name:        "quoted scalar",
			content:     "---\napplyTo: \"**/*.go\"\n---\nGo body.\n",
			wantApplyTo: "**/*.go",
			wantBody:    "Go body.",
		},
		{
			name:        "single-quoted scalar",
			content:     "---\napplyTo: '**/*.go'\n---\nGo body.\n",
			wantApplyTo: "**/*.go",
			wantBody:    "Go body.",
		},
		{
			name:        "no frontmatter -- empty applyTo, full body",
			content:     "Just a body, no frontmatter.\n",
			wantApplyTo: "",
			wantBody:    "Just a body, no frontmatter.",
		},
		{
			name:        "frontmatter with empty applyTo -- global",
			content:     "---\napplyTo:\n---\nGlobal body.\n",
			wantApplyTo: "",
			wantBody:    "Global body.",
		},
		{
			name:        "frontmatter without applyTo key at all -- global",
			content:     "---\ndescription: something\n---\nGlobal body.\n",
			wantApplyTo: "",
			wantBody:    "Global body.",
		},
		{
			name:        "comma-containing scalar kept verbatim (not split)",
			content:     "---\napplyTo: \"**/src/**, **/api/**\"\n---\nComma body.\n",
			wantApplyTo: "**/src/**, **/api/**",
			wantBody:    "Comma body.",
		},
		{
			name:        "brace pattern kept literal",
			content:     "---\napplyTo: \"**/*.{css,scss}\"\n---\nBrace body.\n",
			wantApplyTo: "**/*.{css,scss}",
			wantBody:    "Brace body.",
		},
		{
			name:        "empty body after frontmatter",
			content:     "---\napplyTo:\n---\n\n\n",
			wantApplyTo: "",
			wantBody:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseInstruction([]byte(tt.content))
			if err != nil {
				t.Fatalf("ParseInstruction: %v", err)
			}
			if got.ApplyTo != tt.wantApplyTo {
				t.Errorf("ApplyTo = %q, want %q", got.ApplyTo, tt.wantApplyTo)
			}
			if got.Body != tt.wantBody {
				t.Errorf("Body = %q, want %q", got.Body, tt.wantBody)
			}
		})
	}
}

// frontmatter markers must never leak into the parsed body (CMP-04).
func TestParseInstruction_FrontmatterMarkersNotInBody(t *testing.T) {
	content := "---\napplyTo: \"**/*.go\"\ndescription: x\n---\nBody text.\n"
	got, err := ParseInstruction([]byte(content))
	if err != nil {
		t.Fatalf("ParseInstruction: %v", err)
	}
	if got.Body != "Body text." {
		t.Errorf("Body = %q, want %q", got.Body, "Body text.")
	}
}

// TestParseInstruction_OracleApplyTo: the applyTo compile groups by and the
// body are what Oracle 0.33.0's parse_primitive_file gives each content
// (primitives/parser.py:115-116, normalize_apply_to). err marks a
// frontmatter the Oracle refuses, which compile skips.
func TestParseInstruction_OracleApplyTo(t *testing.T) {
	tests := []struct {
		name, content, wantApplyTo, wantBody string
		wantErr                              bool
	}{
		{"block list", "---\napplyTo:\n  - \"src/**/*.ts\"\n  - \"web/**/*.ts\"\ndescription: list\n---\nbody\n", "src/**/*.ts,web/**/*.ts", "body", false},
		{"block list one item", "---\napplyTo:\n  - \"only/**\"\n---\nbody\n", "only/**", "body", false},
		{"block list at column 0", "---\napplyTo:\n- \"z/**\"\n- \"w/**\"\ndescription: x\n---\nbody\n", "z/**,w/**", "body", false},
		{"flow list", "---\napplyTo: [\"x/**\", \"y/**\"]\n---\nbody\n", "x/**,y/**", "body", false},
		{"empty flow list", "---\napplyTo: []\n---\nbody\n", "", "body", false},
		{"null", "---\napplyTo: null\n---\nbody\n", "", "body", false},
		{"empty string", "---\napplyTo: \"\"\n---\nbody\n", "", "body", false},
		{"list with empty item", "---\napplyTo:\n  - \"a/**\"\n  - \"\"\n  - \"b/**\"\n---\nbody\n", "a/**,b/**", "body", false},
		{"list item with comma", "---\napplyTo:\n  - \"c/**, d/**\"\n---\nbody\n", "c/**\\, d/**", "body", false},
		{"list with int", "---\napplyTo:\n  - 1\n  - \"n/**\"\n---\nbody\n", "1,n/**", "body", false},
		{"list with nested list", "---\napplyTo:\n  - \"t/**\"\n  - [\"u/**\", \"v/**\"]\n---\nbody\n", "t/**,['u/**', 'v/**']", "body", false},
		{"list with bool, nested list, mapping", "---\napplyTo:\n  - yes\n  - [x, 2, null, \"it's\"]\n  - {k: v}\n---\nbody\n", "True,['x', 2, None, \"it's\"],{'k': 'v'}", "body", false},
		{"crlf block list", "---\r\napplyTo:\r\n  - \"r/**\"\r\n  - \"s/**\"\r\n---\r\nbody\r\n", "r/**,s/**", "body", false},
		{"true", "---\napplyTo: true\n---\nbody\n", "True", "body", false},
		{"yes", "---\napplyTo: yes\n---\nbody\n", "True", "body", false},
		{"float 1.50", "---\napplyTo: 1.50\n---\nbody\n", "1.5", "body", false},
		{"1e3 is a string", "---\napplyTo: 1e3\n---\nbody\n", "1e3", "body", false},
		{"0o17 is a string", "---\napplyTo: 0o17\n---\nbody\n", "0o17", "body", false},
		{"date", "---\napplyTo: 2024-01-02\n---\nbody\n", "2024-01-02", "body", false},
		{"flow mapping", "---\napplyTo: {a: b}\n---\nbody\n", "{'a': 'b'}", "body", false},
		{"trailing comment", "---\napplyTo: \"q/**\"  # trailing\n---\nbody\n", "q/**", "body", false},
		{"single-quote escape", "---\napplyTo: 'it''s/**'\n---\nbody\n", "it's/**", "body", false},
		{"folded", "---\napplyTo: >-\n  f/**,\n  g/**\n---\nbody\n", "f/**, g/**", "body", false},
		{"escaped comma", "---\napplyTo: \"a/**\\\\, b/**\"\n---\nbody\n", "a/**\\, b/**", "body", false},
		{"escaped backslash", "---\napplyTo: \"a\\\\b,c\"\n---\nbody\n", "a\\b,c", "body", false},
		{"comma in character class", "---\napplyTo: \"src/[a,b]/*.ts,x/**\"\n---\nbody\n", "src/[a,b]/*.ts,x/**", "body", false},
		{"non-ASCII and YAML tab escape", "---\napplyTo: \"src/日本/**, a\\tb\"\n---\nbody\n", "src/日本/**, a\tb", "body", false},
		{"bare scalar alias", "---\napplyTo: **/*.go\n---\nbody\n", "", "", true},
		{"unquoted brace comma list", "---\napplyTo: \"**/*.{css,scss},**/*.py\"\n---\nbody\n", "**/*.{css,scss},**/*.py", "body", false},
		{"flow list leading null", "---\napplyTo: [null, '**/*.py']\n---\nbody\n", "**/*.py", "body", false},
		{"flow list leading tilde", "---\napplyTo: [~, '**/*.py']\n---\nbody\n", "**/*.py", "body", false},
		{"block list leading empty item", "---\napplyTo:\n  -\n  - \"**/*.py\"\n---\nbody\n", "**/*.py", "body", false},
		{"flow list of nulls", "---\napplyTo: [null, ~]\n---\nbody\n", "", "body", false},
		{"flow list single quotes", "---\napplyTo: ['**/*.py', '**/*.rb']\n---\nbody\n", "**/*.py,**/*.rb", "body", false},
		{"unclosed flow", "---\nfoo: [unclosed\napplyTo: \"k/**\"\n---\nbody\n", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseInstruction([]byte(tt.content))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseInstruction(%q) = %+v, want an error", tt.content, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseInstruction(%q): %v", tt.content, err)
			}
			if got.ApplyTo != tt.wantApplyTo || got.Body != tt.wantBody {
				t.Errorf("ParseInstruction(%q) = %+v, want ApplyTo %q, Body %q", tt.content, got, tt.wantApplyTo, tt.wantBody)
			}
		})
	}
}
