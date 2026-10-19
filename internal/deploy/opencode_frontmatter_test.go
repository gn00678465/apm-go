package deploy

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The wanted texts are what Oracle 0.33.0 printed for the same agents of a
// package named depname, dép\tx or Plüg (~/.cache/apm-go-verify/
// issue126/agent/s6, impl/F-ex2, impl/F-ex3), unwrapped.
const (
	ocToolsTail = "; OpenCode requires a mapping of tool-name to boolean. OpenCode will reject this agent at load time. Fix: rewrite the frontmatter as 'tools: {Read: true, Grep: true}'."
	ocEntryTail = "); OpenCode requires string-keyed boolean values. OpenCode will reject this agent at load time. Fix: set every tool entry to 'true' or 'false' (e.g. 'Read: true')."
	ocColorTail = "; OpenCode requires a hex value (e.g. '#aabbcc') or one of ['accent', 'error', 'info', 'primary', 'secondary', 'success', 'warning']. OpenCode will reject this agent at load time. Fix: replace the color with a '#rgb' or '#rrggbb' hex literal or one of the listed theme names."
)

func TestOpencodeAgentNotes_OracleTexts(t *testing.T) {
	for _, tc := range []struct {
		pkg, file, content string
		want               []string
	}{
		{"depname", "a1.agent.md", "---\ndescription: a1\ntools: Read, Grep\ncolor: green\n---\nA1\n", []string{
			"OpenCode agent 'depname/a1.agent.md' has tools as str" + ocToolsTail,
			"OpenCode agent 'depname/a1.agent.md' has color='green'" + ocColorTail}},
		{"depname", "a2.agent.md", "---\ndescription: a2\ntools: [Read, Grep]\ncolor: \"#ff0000\"\n---\nA2\n", []string{
			"OpenCode agent 'depname/a2.agent.md' has tools as list" + ocToolsTail}},
		{"depname", "a3.agent.md", "---\ndescription: a3\ntools: {Read: true, Grep: 1}\ncolor: primary\n---\nA3\n", []string{
			"OpenCode agent 'depname/a3.agent.md' has a non-boolean tool entry ('Grep': 1" + ocEntryTail}},
		{"depname", "a4.agent.md", "---\ndescription: a4\ntools: {Read: true}\ncolor: 123\n---\nA4\n", []string{
			"OpenCode agent 'depname/a4.agent.md' has color=123" + ocColorTail}},
		{"depname", "a5.agent.md", "---\ndescription: a5\ncolor: \"#GGG\"\n---\nA5\n", []string{
			"OpenCode agent 'depname/a5.agent.md' has color='#GGG'" + ocColorTail}},
		{"depname", "a6.agent.md", "---\ndescription: a6\ncolor: \"#abc\"\ntools:\n---\nA6\n", []string{
			"OpenCode agent 'depname/a6.agent.md' has tools as NoneType" + ocToolsTail}},
		{"depname", "a7.agent.md", "---\ndescription: a7\ncolor: cyán\ntools: {Read: yes}\n---\nA7\n", []string{
			"OpenCode agent 'depname/a7.agent.md' has color='cy\\xe1n'" + ocColorTail}},
		{"depname", "a8.agent.md", "---\ndescription: a8\ncolor: Primary\n---\nA8\n", []string{
			"OpenCode agent 'depname/a8.agent.md' has color='Primary'" + ocColorTail}},
		{"depname", "a9.agent.md", "---\ndescription: a9 crlf\r\ntools: Read\r\n---\r\nA9\r\n", []string{
			"OpenCode agent 'depname/a9.agent.md' has tools as str" + ocToolsTail}},
		{"dép\tx", "b1.agent.md", "---\ndescription: b1\ntools: {1: true}\ncolor: \"#abc\\n\"\n---\nB1\n", []string{
			"OpenCode agent 'd?p?x/b1.agent.md' has a non-boolean tool entry (1: True" + ocEntryTail}},
		{"dép\tx", "b2.agent.md", "---\ndescription: b2\ntools: {Read: true, Bash: \"yes\", Grep: 2}\ncolor: null\n---\nB2\n", []string{
			"OpenCode agent 'd?p?x/b2.agent.md' has a non-boolean tool entry ('Bash': 'yes'" + ocEntryTail,
			"OpenCode agent 'd?p?x/b2.agent.md' has color=None" + ocColorTail}},
		{"dép\tx", "b3.agent.md", "---\n---\nB3\n", nil},
		{"dép\tx", "b4.agent.md", "---\ncolor: [unclosed\n---\nB4\n", nil},
		{"dép\tx", "b5.agent.md", "---\n- a\n- b\n---\nB5\n", nil},
		{"dép\tx", "b6.agent.md", "---\ncolor: 2020-01-02\ntools: 1.5\n---\nB6\n", []string{
			"OpenCode agent 'd?p?x/b6.agent.md' has tools as float" + ocToolsTail,
			"OpenCode agent 'd?p?x/b6.agent.md' has color=datetime.date(2020, 1, 2)" + ocColorTail}},
		{"dép\tx", "b7.agent.md", "---\ncolor: \"#ABCDEF\"\ntools: {Read: on, Write: off}\n---\nB7\n", nil},
		{"dép\tx", "b8.agent.md", "---\ncolor: \"gréen\\u2603\\U0001F600\"\n---\nB8\n", []string{
			"OpenCode agent 'd?p?x/b8.agent.md' has color='gr\\xe9en\\u2603\\U0001f600'" + ocColorTail}},
		{"dép\tx", "b9.agent.md", "---\ncolor: [a, \"é\"]\ntools: true\n---\nB9\n", []string{
			"OpenCode agent 'd?p?x/b9.agent.md' has tools as bool" + ocToolsTail,
			"OpenCode agent 'd?p?x/b9.agent.md' has color=['a', '\\xe9']" + ocColorTail}},
		{"dép\tx", "b10.agent.md", "---\ncolor: \"#abcd\"\ntools: {\"x\": null}\n---\nB10\n", []string{
			"OpenCode agent 'd?p?x/b10.agent.md' has a non-boolean tool entry ('x': None" + ocEntryTail,
			"OpenCode agent 'd?p?x/b10.agent.md' has color='#abcd'" + ocColorTail}},
		{"dép\tx", "b11.agent.md", "---  \ncolor: x\n---   \nB11\n", []string{
			"OpenCode agent 'd?p?x/b11.agent.md' has color='x'" + ocColorTail}},
		{"dép\tx", "b12.agent.md", "---\ncolor: x\n----\nB12\n", []string{
			"OpenCode agent 'd?p?x/b12.agent.md' has color='x'" + ocColorTail}},
		{"dép\tx", "b13.agent.md", "---\ncolor: {a: 1}\ntools: 2020-01-02 10:00:00\n---\nB13\n", []string{
			"OpenCode agent 'd?p?x/b13.agent.md' has tools as datetime" + ocToolsTail,
			"OpenCode agent 'd?p?x/b13.agent.md' has color={'a': 1}" + ocColorTail}},
		{"Plüg", "äx.md", "---\ncolor: red\n---\nP\n", []string{
			"OpenCode agent 'Pl?g/?x.md' has color='red'" + ocColorTail}},
	} {
		t.Run(tc.file, func(t *testing.T) {
			src := filepath.Join(t.TempDir(), tc.file)
			if err := os.WriteFile(src, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			shown := map[string]string{"depname": "depname", "d\u00e9p\tx": "d?p?x", "Pl\u00fcg": "Pl?g"}[tc.pkg]
			var got []string
			for _, n := range opencodeAgentNotes(src, tc.file, tc.pkg) {
				if n.Package != shown || n.Info {
					t.Errorf("note %+v: want a warning of package %q", n, shown)
				}
				got = append(got, n.Text)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("notes:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}
