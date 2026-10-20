package deploy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// TestCodexAgentTOMLOracleBytes pins the bytes Oracle 0.33.0 wrote to
// .codex/agents/<name>.toml for each source (issue #123, run c49-r1). The
// three sources whose repr() has a \x take the escapes of issue #123
// comment 6284804045, item 1, and must read back as the source's body.
func TestCodexAgentTOMLOracleBytes(t *testing.T) {
	cases := []struct {
		name, src, want string
		body            string // set for a deviation: the text the TOML must hold
	}{
		{"plain", "---\nname: plain\ndescription: A plain agent\n---\nDo the thing.\n",
			"name = \"plain\"\ndescription = \"A plain agent\"\ndeveloper_instructions = \"Do the thing.\"\n", ""},
		{"multi", "---\nname: multi\ndescription: two lines\n---\n# Title\n\nLine one.\nLine two with\ttab.\n",
			"name = \"multi\"\ndescription = \"two lines\"\ndeveloper_instructions = \"# Title\\n\\nLine one.\\nLine two with\\ttab.\"\n", ""},
		{"quotes", "---\nname: quotes\ndescription: \"it's \\\"quoted\\\"\"\n---\nSay \"hi\" and it's fine.\n",
			"name = \"quotes\"\ndescription = \"it's \\\"quoted\\\"\"\ndeveloper_instructions = \"Say \\\"hi\\\" and it's fine.\"\n", ""},
		{"single", "---\nname: single\ndescription: it's\n---\nit's only single\n",
			"name = \"single\"\ndescription = \"it's\"\ndeveloper_instructions = \"it's only single\"\n", ""},
		{"dq", "---\nname: dq\n---\nonly \"double\"\n",
			"name = \"dq\"\ndescription = \"\"\ndeveloper_instructions = \"only \\\"double\\\"\"\n", ""},
		{"nofm", "Just a body, no frontmatter.\n",
			"name = \"nofm\"\ndescription = \"\"\ndeveloper_instructions = \"Just a body, no frontmatter.\"\n", ""},
		{"badfm", "---\nname: [unclosed\n---\nbody\n",
			"name = \"badfm\"\ndescription = \"\"\ndeveloper_instructions = \"body\"\n", ""},
		{"intname", "---\nname: 123\ndescription: yes\n---\nbody\n",
			"name = 123\ndescription = true\ndeveloper_instructions = \"body\"\n", ""},
		{"nulldesc", "---\nname: nulldesc\ndescription:\n---\nbody\n",
			"name = \"nulldesc\"\ndeveloper_instructions = \"body\"\n", ""},
		{"crlf", "---\r\nname: crlf\r\n---\r\nline1\r\nline2\r\n",
			"name = \"crlf\"\ndescription = \"\"\ndeveloper_instructions = \"line1\\nline2\"\n", ""},
		{"empty", "---\nname: empty\n---\n",
			"name = \"empty\"\ndescription = \"\"\ndeveloper_instructions = \"\"\n", ""},
		{"tq", "---\nname: tq\n---\ntriple \"\"\" quotes and ''' too\n",
			"name = \"tq\"\ndescription = \"\"\ndeveloper_instructions = \"triple \\\"\\\"\\\" quotes and ''' too\"\n", ""},
		// Oracle: "path C:\Users\x and \n literal and \x41 hex", which tomllib refuses.
		{"backslash", "---\nname: backslash\n---\npath C:\\Users\\x and \\n literal and \\x41 hex\n",
			"name = \"backslash\"\ndescription = \"\"\ndeveloper_instructions = \"path C:\\\\Users\\\\x and \\\\n literal and \\\\x41 hex\"\n",
			"path C:\\Users\\x and \\n literal and \\x41 hex"},
		// Oracle: "zero\u200bwidth and é and x7f del", U+007F lost.
		{"unicode", "---\nname: unicode\ndescription: café 中文 \U0001F600\n---\nzero\u200bwidth and é and \u007f del\n",
			"name = \"unicode\"\ndescription = \"café 中文 \U0001F600\"\ndeveloper_instructions = \"zero\\u200bwidth and é and \\u007f del\"\n",
			"zero\u200bwidth and é and \u007f del"},
		// Oracle: "bellx07 and escx1b end", both control characters lost.
		{"ctrl", "---\nname: ctrl\n---\nbell\u0007 and esc\u001b end\n",
			"name = \"ctrl\"\ndescription = \"\"\ndeveloper_instructions = \"bell\\u0007 and esc\\u001b end\"\n",
			"bell\u0007 and esc\u001b end"},
		{"bell-and-hex", "---\nname: bell-and-hex\n---\nring\u0007 then \\x41\n",
			"name = \"bell-and-hex\"\ndescription = \"\"\ndeveloper_instructions = \"ring\\u0007 then \\\\x41\"\n",
			"ring\u0007 then \\x41"},
	}
	dir := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := filepath.Join(dir, c.name+".agent.md")
			if err := os.WriteFile(src, []byte(c.src), 0644); err != nil {
				t.Fatal(err)
			}
			got, err := codexAgentTOML(Primitive{Type: TypeAgents, Name: c.name, SrcPath: src})
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
			if c.body == "" {
				return
			}
			var doc map[string]any
			if err := toml.Unmarshal(got, &doc); err != nil {
				t.Fatalf("not valid TOML: %v", err)
			}
			if doc["developer_instructions"] != c.body {
				t.Fatalf("developer_instructions = %q, want %q", doc["developer_instructions"], c.body)
			}
		})
	}
}
