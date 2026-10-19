package deploy

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/resolver"
)

// commandOracleCases are prompt files and the .claude/commands/<name>.md
// Oracle 0.33.0's `install --target claude` wrote for them, with the
// warnings and the info line it printed (Rich's line wrapping joined). err
// marks a prompt the Oracle skipped as malformed YAML.
var commandOracleCases = []struct {
	name, in, out string
	warnings      []string
	info          string
	err           bool
}{
	{name: "bom", in: "\ufeff---\ndescription: bom\n---\nbom body\n", out: "---\ndescription: bom\n---\n\nbom body"},
	{name: "broken_yaml", in: "---\ndescription: [unclosed\n---\nBody\n", err: true},
	{name: "camel_keys", in: "---\ndescription: camel\nallowedTools: [Read, Grep]\nargumentHint: \"<file>\"\n---\nBody\n", out: "---\nallowed-tools:\n- Read\n- Grep\nargument-hint: <file>\ndescription: camel\n---\n\nBody"},
	{name: "crlf", in: "---\r\ndescription: crlf\r\n---\r\ncrlf body\r\nline2\r\n", out: "---\ndescription: crlf\n---\n\ncrlf body\nline2"},
	{name: "desc_only", in: "---\ndescription: Only a description\n---\nBody text\n", out: "---\ndescription: Only a description\n---\n\nBody text"},
	{name: "dropped_keys", in: "---\ndescription: d\nauthor: me\nmode: agent\ntools: [a]\n---\nBody\n", out: "---\ndescription: d\n---\n\nBody", warnings: []string{"Claude command dropped_keys: frontmatter keys not supported for claude commands and were dropped: author, mode, tools. Supported keys: allowed-tools, argument-hint, description, input, model."}},
	{name: "emoji", in: "---\ndescription: \"\\U0001F600 smile\"\n---\nBody\n", out: "---\ndescription: \"\\U0001F600 smile\"\n---\n\nBody"},
	{name: "empty_frontmatter", in: "---\n---\nBody\n", out: "---\n{}\n---\n\nBody"},
	{name: "input_hint_kept", in: "---\nargument-hint: \"[topic]\"\ninput: [topic]\n---\n${input:topic}\n", out: "---\nargument-hint: '[topic]'\narguments:\n- topic\n---\n\n$topic", info: "Mapped input -> command arguments in input_hint_kept.md: [topic]"},
	{name: "input_list", in: "---\ninput:\n  - name\n  - bad name\n  - 42\n  - \" padded \"\n  - \"\"\n---\nHi ${input:name} and ${{ input:padded }} and ${ input : name }.\n", out: "---\nargument-hint: <name> <padded>\narguments:\n- name\n- padded\n---\n\nHi $name and $padded and $name.", warnings: []string{"input: rejected 2 invalid name(s) (must match [A-Za-z][\\w-]{0,63}): bad name, 42"}, info: "Mapped input -> command arguments in input_list.md: [name, padded]"},
	{name: "input_many_rejected", in: "---\ninput: [a b, c d, e f, g h, i j, k l, ok]\n---\n${input:ok}\n", out: "---\nargument-hint: <ok>\narguments:\n- ok\n---\n\n$ok", warnings: []string{"input: rejected 6 invalid name(s) (must match [A-Za-z][\\w-]{0,63}): a b, c d, e f, g h, i j ..."}, info: "Mapped input -> command arguments in input_many_rejected.md: [ok]"},
	{name: "input_mapping", in: "---\ninput:\n  first: a\n  2nd: b\n---\n${input:first}\n", out: "---\nargument-hint: <first>\narguments:\n- first\n---\n\n$first", warnings: []string{"input: rejected 1 invalid name(s) (must match [A-Za-z][\\w-]{0,63}): 2nd"}, info: "Mapped input -> command arguments in input_mapping.md: [first]"},
	{name: "input_mapping_list", in: "---\ninput:\n  - feature_name: \"the feature\"\n  - feature-desc: \"what it does\"\n  - [nested]\n---\nBuild ${input:feature_name}: ${input:feature-desc}\n", out: "---\nargument-hint: <feature_name> <feature-desc>\narguments:\n- feature_name\n- feature-desc\n---\n\nBuild $feature_name: $feature-desc", warnings: []string{"input: rejected 1 invalid name(s) (must match [A-Za-z][\\w-]{0,63}): ['nested']"}, info: "Mapped input -> command arguments in input_mapping_list.md: [feature_name, feature-desc]"},
	{name: "input_string", in: "---\ninput: topic\n---\nWrite about ${input:topic}.\n", out: "---\nargument-hint: <topic>\narguments:\n- topic\n---\n\nWrite about $topic.", info: "Mapped input -> command arguments in input_string.md: [topic]"},
	{name: "integer_model", in: "---\nmodel: 3\ndescription: 2024-01-02\n---\nBody\n", out: "---\ndescription: 2024-01-02\nmodel: 3\n---\n\nBody"},
	{name: "kebab_wins", in: "---\nallowed-tools: Bash\nallowedTools: Read\nargument-hint: kebab\nargumentHint: camel\n---\nBody\n", out: "---\nallowed-tools: Bash\nargument-hint: kebab\n---\n\nBody"},
	{name: "list_frontmatter", in: "---\n- a\n---\nBody\n", out: "---\n{}\n---\n\nBody"},
	{name: "long_description", in: "---\ndescription: This description is long enough that PyYAML folds it at the eighty column width into two lines\n---\nBody\n", out: "---\ndescription: This description is long enough that PyYAML folds it at the eighty column\n  width into two lines\n---\n\nBody"},
	{name: "multiline_description", in: "---\ndescription: |\n  line one\n  line two\n---\nBody\n", out: "---\ndescription: 'line one\n\n  line two\n\n  '\n---\n\nBody"},
	{name: "nested_tools", in: "---\nallowed-tools:\n  - Bash(git:*)\n  - {Read: [a, b]}\n  - []\n---\nBody\n", out: "---\nallowed-tools:\n- Bash(git:*)\n- Read:\n  - a\n  - b\n- []\n---\n\nBody"},
	{name: "no_frontmatter", in: "Just a body\n\nwith two paragraphs\n", out: "---\n{}\n---\n\nJust a body\n\nwith two paragraphs"},
	{name: "non_ascii", in: "---\ndescription: 日本語の説明\n---\n本文\n", out: "---\ndescription: 日本語の説明\n---\n\n本文"},
	{name: "quoted_values", in: "---\ndescription: \"a: b\"\nmodel: \"1.0\"\nargument-hint: \"#tag\"\nallowed-tools: \"yes\"\n---\nBody\n", out: "---\nallowed-tools: 'yes'\nargument-hint: '#tag'\ndescription: 'a: b'\nmodel: '1.0'\n---\n\nBody"},
	{name: "trailing_space", in: "---\ndescription: trail\n---\n\n\nbody with trailing   \n\n\n", out: "---\ndescription: trail\n---\n\nbody with trailing"},
	{name: "unclosed", in: "---\ndescription: never closed\nBody\n", err: true},
}

func TestConvertPromptToCommand_OracleMatch(t *testing.T) {
	for _, tc := range commandOracleCases {
		t.Run(tc.name, func(t *testing.T) {
			out, notes, err := convertPromptToCommand(tc.name, "claude", []byte(tc.in), nil)
			if tc.err {
				var fmErr *frontmatterError
				if !errors.As(err, &fmErr) {
					t.Fatalf("err = %v, want a frontmatter error", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != tc.out {
				t.Errorf("command:\n got %q\nwant %q", out, tc.out)
			}
			var warnings []string
			info := ""
			for _, n := range notes {
				if n.Info {
					info = n.Text
				} else {
					warnings = append(warnings, n.Text)
				}
			}
			if !slices.Equal(warnings, tc.warnings) || info != tc.info {
				t.Errorf("notes: got warnings %q info %q, want %q %q", warnings, info, tc.warnings, tc.info)
			}
		})
	}
}

// The dropped-keys warning names the target the way the Oracle's
// str.capitalize() does (measured: "Opencode command ...").
func TestConvertPromptToCommand_TargetName(t *testing.T) {
	_, notes, err := convertPromptToCommand("p", "opencode", []byte("---\nauthor: me\n---\nb\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "Opencode command p: frontmatter keys not supported for opencode commands and were dropped: author. Supported keys: allowed-tools, argument-hint, description, input, model."
	if len(notes) != 1 || notes[0].Text != want {
		t.Errorf("notes = %+v, want %q", notes, want)
	}
}

func promptNames(prims []Primitive) []string {
	var names []string
	for _, p := range prims {
		if p.Type == TypePrompts {
			names = append(names, p.Name)
		}
	}
	return names
}

func TestCollectPrompts_OracleFileSet(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "rootp.prompt.md", "root")
	mkFile(t, dir, "README.md", "not a prompt")
	mkFile(t, dir, ".apm/prompts/p.prompt.md", "---\ndescription: p\n---\nbody\n")
	mkFile(t, dir, ".apm/prompts/plain.md", "---\ndescription: plain\n---\nbody\n")
	mkFile(t, dir, ".apm/prompts/wf.prompt.md", "---\ninterval: daily\n---\nworkflow\n")
	mkFile(t, dir, ".apm/prompts/hourly.prompt.md", "---\nschedule_hour: 3\n---\nworkflow\n")
	mkFile(t, dir, ".apm/prompts/broken.prompt.md", "---\ninterval: [\n---\nnot readable, so not a workflow\n")
	mkFile(t, dir, ".apm/prompts/sub/deep.prompt.md", "not recursive")

	prims, err := CollectDependencyPrimitives("acme/pkg", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := promptNames(prims), []string{"rootp", "broken", "p"}; !slices.Equal(got, want) {
		t.Errorf("dependency prompts = %v, want %v", got, want)
	}

	if got, want := promptNames(CollectLocalPrimitives(dir)), []string{"rootp", "broken", "p"}; !slices.Equal(got, want) {
		t.Errorf("project prompts = %v, want %v", got, want)
	}
	// --global reads ~/.apm/.apm/prompts/ alone.
	if got, want := promptNames(CollectScopeLocalPrimitives(dir, true)), []string{"broken", "p"}; !slices.Equal(got, want) {
		t.Errorf("--global prompts = %v, want %v", got, want)
	}
}

// The Oracle reads the project's root prompts only when it has a .apm/
// directory.
func TestCollectPrompts_ProjectRootNeedsAPMDir(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "top.prompt.md", "top")
	if got := promptNames(CollectLocalPrimitives(dir)); len(got) != 0 {
		t.Errorf("prompts without .apm/ = %v, want none", got)
	}
	mkdir(t, filepath.Join(dir, ".apm"))
	if got, want := promptNames(CollectLocalPrimitives(dir)), []string{"top"}; !slices.Equal(got, want) {
		t.Errorf("prompts with .apm/ = %v, want %v", got, want)
	}
}

func depManifest(keys ...string) (*manifest.Manifest, *resolver.ResolutionResult) {
	m := &manifest.Manifest{Name: "proj", Version: "1.0.0"}
	resolved := &resolver.ResolutionResult{}
	for _, key := range keys {
		m.ParsedDeps = append(m.ParsedDeps, &manifest.DependencyReference{RepoURL: key, Source: "git"})
		resolved.Deps = append(resolved.Deps, resolver.ResolvedDep{Key: key, RepoURL: key, Kind: resolver.KindGitSemver, Depth: 1})
	}
	return m, resolved
}

// A prompt and a command of .apm/commands/ with one name go to one command
// path: in one package the prompt is deployed, the path is recorded once,
// with the hash of the bytes written.
func TestRun_PromptWinsOverCommandInOnePackage(t *testing.T) {
	dir := t.TempDir()
	mod := filepath.Join(dir, "apm_modules", "acme/pkg")
	mkFile(t, mod, ".apm/commands/x.md", "command bytes\n")
	mkFile(t, mod, ".apm/prompts/x.prompt.md", "---\ndescription: from the prompt\n---\nprompt body\n")
	m, resolved := depManifest("acme/pkg")

	result, err := Run([]string{"claude", "opencode"}, dir, m, resolved, nil, "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "---\ndescription: from the prompt\n---\n\nprompt body"
	dr := result.PerDep["acme/pkg"]
	for _, path := range []string{".claude/commands/x.md", ".opencode/commands/x.md"} {
		if got := readDeployed(t, dir, path); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
		n := 0
		for _, f := range dr.Files {
			if f == path {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s listed %d times in %v", path, n, dr.Files)
		}
		if h, _ := lockfile.HashFileBytes(filepath.Join(dir, path)); dr.Hashes[path] != h {
			t.Errorf("%s hash = %s, want %s", path, dr.Hashes[path], h)
		}
	}
	if len(result.Diags) != 0 {
		t.Errorf("diags = %v, want none", result.Diags)
	}
}

// Across packages the first in priority order keeps the path: the project's
// own content, then the order apm.yml declares.
func TestRun_PromptAndCommandAcrossPackages(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/commands/x.md", "local command")
	mkFile(t, filepath.Join(dir, "apm_modules", "acme/a"), ".apm/prompts/x.prompt.md", "dep prompt")
	mkFile(t, filepath.Join(dir, "apm_modules", "acme/a"), ".apm/commands/y.md", "a command")
	mkFile(t, filepath.Join(dir, "apm_modules", "acme/b"), ".apm/prompts/y.prompt.md", "b prompt")
	m, resolved := depManifest("acme/a", "acme/b")

	result, err := Run([]string{"claude"}, dir, m, resolved, nil, "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := readDeployed(t, dir, ".claude/commands/x.md"); got != "local command" {
		t.Errorf("x.md = %q, want the project's command", got)
	}
	if got := readDeployed(t, dir, ".claude/commands/y.md"); got != "a command" {
		t.Errorf("y.md = %q, want acme/a's command", got)
	}
	if dr := result.PerDep["acme/b"]; dr != nil {
		t.Errorf("acme/b deployed %v, want nothing", dr.Files)
	}
	for _, want := range []string{
		`prompts "x" from dependency:acme/a shadowed by commands "x" from local`,
		`prompts "y" from dependency:acme/b shadowed by commands "y" from dependency:acme/a`,
	} {
		if !slices.Contains(result.Diags, want) {
			t.Errorf("diags %q lack %q", result.Diags, want)
		}
	}
}

func TestRun_PromptCommandNotes(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/prompts/mine.prompt.md", "---\nmode: agent\n---\nb\n")
	mod := filepath.Join(dir, "apm_modules", "acme/pkg")
	mkFile(t, mod, "apm.yml", "name: depname\nversion: 1.0.0\n")
	mkFile(t, mod, ".apm/prompts/bad.prompt.md", "---\ndescription: [\n---\nb\n")
	mkFile(t, mod, ".apm/prompts/rich.prompt.md", "---\nauthor: me\ninput: [name, bad name]\n---\n${input:name}\n")
	mkFile(t, mod, ".apm/prompts/...prompt.md", "named ..")
	m, resolved := depManifest("acme/pkg")

	result, err := Run([]string{"claude", "opencode"}, dir, m, resolved, nil, "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Each target in turn, the project's own content first, as Run deploys.
	type note struct {
		dep, pkg, text string
		info           bool
	}
	var want []note
	for _, target := range []string{"Claude", "Opencode"} {
		want = append(want,
			note{"", "_local", target + " command mine: frontmatter keys", false},
			note{"acme/pkg", "depname", "Rejected command filename: Invalid command filename '..'", false},
			note{"acme/pkg", "depname", "Skipped command bad.prompt.md: malformed or over-budget YAML frontmatter (", false},
			note{"acme/pkg", "depname", target + " command rich: frontmatter keys", false},
			note{"acme/pkg", "depname", "input: rejected 1 invalid name(s)", false},
			note{"acme/pkg", "depname", "Mapped input -> command arguments in rich.md: [name]", true},
		)
	}
	matches := len(result.CommandNotes) == len(want)
	for i := 0; matches && i < len(want); i++ {
		n, w := result.CommandNotes[i], want[i]
		matches = n.DepKey == w.dep && n.Package == w.pkg && n.Info == w.info && strings.HasPrefix(n.Text, w.text)
	}
	if !matches {
		t.Errorf("notes:\n got %+v\nwant %+v", result.CommandNotes, want)
	}
	if result.FailedBuckets["acme/pkg"] {
		t.Error("a skipped prompt failed its package's bucket")
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude/commands/bad.md")); !os.IsNotExist(err) {
		t.Errorf("bad.md: %v, want no file", err)
	}
	if got := readDeployed(t, dir, ".claude/commands/rich.md"); got != "---\nargument-hint: <name>\narguments:\n- name\n---\n\n$name" {
		t.Errorf("rich.md = %q", got)
	}
}

// A --global deploy links what it copies, but a command is converted: it is
// a file of its own, as a claude rule is.
func TestRun_PromptCommandGlobalIsAFile(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	mkFile(t, dir, "top.prompt.md", "not read for --global")
	mkFile(t, dir, ".apm/prompts/mine.prompt.md", "mine")
	mkFile(t, filepath.Join(dir, "apm_modules", "acme/pkg"), ".apm/prompts/p.prompt.md", "p")
	m, resolved := depManifest("acme/pkg")

	result, err := Run([]string{"claude"}, dir, m, resolved, nil, home, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".claude/commands/mine.md", ".claude/commands/p.md"} {
		info, err := os.Lstat(filepath.Join(home, rel))
		if err != nil || !info.Mode().IsRegular() {
			t.Errorf("%s: %v, want a regular file", rel, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude/commands/top.md")); !os.IsNotExist(err) {
		t.Errorf("top.md: %v, want no file", err)
	}
	if got := result.PerDep[""].Files; !slices.Equal(got, []string{".claude/commands/mine.md"}) {
		t.Errorf("local files = %v", got)
	}
}

// The root of a package holds its root prompts, so a root that cannot be
// listed fails the bucket: its file list may lack them.
func TestRun_UnlistableDependencyRootFailsBucket(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permission bits that restrict this user")
	}
	dir := t.TempDir()
	mod := filepath.Join(dir, "apm_modules", "acme/pkg")
	mkFile(t, mod, "p.prompt.md", "root prompt")
	mkFile(t, mod, ".apm/agents/a.agent.md", "agent")
	if err := os.Chmod(mod, 0o311); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(mod, 0o755) })
	m, resolved := depManifest("acme/pkg")

	result, err := Run([]string{"claude"}, dir, m, resolved, nil, "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.FailedBuckets["acme/pkg"] {
		t.Errorf("FailedBuckets = %v, want acme/pkg", result.FailedBuckets)
	}
	want := "acme/pkg: read apm_modules/acme/pkg failed: permission denied"
	if !slices.Contains(result.Diags, want) {
		t.Errorf("diags %q lack %q", result.Diags, want)
	}
}

// A package's root prompts follow the symlink rule of the rest of the
// package (issue #89): one that stays inside is read where it points, under
// its own name; one that leaves the package fails the collection.
func TestCollectPrompts_DependencyRootSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	mod := filepath.Join(dir, "pkg")
	mkFile(t, mod, "docs/real.md", "real")
	mkFile(t, dir, "outside.md", "outside")
	if err := os.Symlink("docs/real.md", filepath.Join(mod, "inside.prompt.md")); err != nil {
		t.Fatal(err)
	}
	prims, err := CollectDependencyPrimitives("acme/pkg", mod)
	if err != nil {
		t.Fatal(err)
	}
	if len(prims) != 1 || prims[0].Name != "inside" || prims[0].SrcPath != filepath.Join(mod, "docs", "real.md") {
		t.Fatalf("prims = %+v, want inside read from docs/real.md", prims)
	}

	if err := os.Symlink("../outside.md", filepath.Join(mod, "out.prompt.md")); err != nil {
		t.Fatal(err)
	}
	_, err = CollectDependencyPrimitives("acme/pkg", mod)
	if want := "dependency acme/pkg: symlink out.prompt.md resolves to "; err == nil || !strings.HasPrefix(err.Error(), want) {
		t.Errorf("err = %v, want one starting %q", err, want)
	}
}
