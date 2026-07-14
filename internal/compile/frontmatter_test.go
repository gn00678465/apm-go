package compile

import "testing"

// TestParseInstruction_ApplyToScalarListAndNoFrontmatter covers CMP-04: a
// scalar applyTo (quoted or bare) is kept verbatim, a YAML list (flow or
// block form) yields its first non-null element, and a file with no
// frontmatter at all yields empty applyTo + the full body. Oracle
// ground-truth: primitives/parser.py:95-119, scratch probes 2026-07-11.
func TestParseInstruction_ApplyToScalarListAndNoFrontmatter(t *testing.T) {
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
			name:        "bare scalar",
			content:     "---\napplyTo: **/*.go\n---\nGo body.\n",
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
			name:        "flow list -- first element wins",
			content:     "---\napplyTo: ['**/*.py', '**/*.rb']\n---\nList body.\n",
			wantApplyTo: "**/*.py",
			wantBody:    "List body.",
		},
		{
			name:        "block list -- first element wins",
			content:     "---\napplyTo:\n  - \"**/*.py\"\n  - \"**/*.rb\"\n---\nBlock body.\n",
			wantApplyTo: "**/*.py",
			wantBody:    "Block body.",
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
			got := ParseInstruction([]byte(tt.content))
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
	got := ParseInstruction([]byte(content))
	if got.Body != "Body text." {
		t.Errorf("Body = %q, want %q", got.Body, "Body text.")
	}
}

// A leading null element (explicit `null`/`~`/empty) in either list form
// must be skipped in favor of the first genuinely non-null element
// (oracle: parser.py:114-116 `non_null = [str(v) for v in value if v is
// not None]`).
func TestExtractApplyTo_SkipsLeadingNullListElements(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "flow list leading null",
			content: "---\napplyTo: [null, '**/*.py']\n---\nBody.\n",
			want:    "**/*.py",
		},
		{
			name:    "flow list leading tilde-null",
			content: "---\napplyTo: [~, '**/*.py']\n---\nBody.\n",
			want:    "**/*.py",
		},
		{
			name:    "block list leading null",
			content: "---\napplyTo:\n  -\n  - \"**/*.py\"\n---\nBody.\n",
			want:    "**/*.py",
		},
		{
			name:    "flow list all null yields empty (global)",
			content: "---\napplyTo: [null, ~]\n---\nBody.\n",
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseInstruction([]byte(tt.content))
			if got.ApplyTo != tt.want {
				t.Errorf("ApplyTo = %q, want %q", got.ApplyTo, tt.want)
			}
		})
	}
}
