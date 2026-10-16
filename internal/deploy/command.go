package deploy

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/apm-go/apm/internal/yamlcore"
)

// pyWhitespace is Python's \s for a str: the characters str.isspace() takes.
const pyWhitespace = `[\t\n\v\f\r\x{1c}-\x{1f} \x{85}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}]`

// The boundary python-frontmatter's YAMLHandler splits on
// (default_handlers.py:252), and the parts of the Oracle's command
// conversion (integration/command_integrator.py:39,236-240).
var (
	frontmatterBoundaryRE = regexp.MustCompile(`(?m)^-{3,}` + pyWhitespace + `*$`)
	inputNameRE           = regexp.MustCompile(`^[A-Za-z][\p{L}\p{N}_-]{0,63}$`)
	inputReferenceRE      = regexp.MustCompile(`\$\{\{?` + pyWhitespace + `*input` + pyWhitespace + `*:` + pyWhitespace + `*([\p{L}\p{N}_-]+)` + pyWhitespace + `*\}?\}`)
)

// promptDoc is a prompt file as the Oracle's load_frontmatter reads it
// (utils/yaml_io.py:474-530 through python-frontmatter's parse,
// __init__.py:72-96). meta is nil when the file has no frontmatter or its
// YAML is not a mapping.
type promptDoc struct {
	meta *pyDict
	body string
}

// frontmatterError is a frontmatter the Oracle refuses with yaml.YAMLError:
// its delimiters do not close, or its YAML does not load.
type frontmatterError struct{ err error }

func (e *frontmatterError) Error() string { return e.err.Error() }

// readPromptDoc reads content as the Oracle does: one leading BOM dropped,
// CRLF and CR read as LF (Path.read_text). A file that opens with a boundary
// line has frontmatter up to the next boundary line, and its body is
// stripped; any other file is all body, as it is.
func readPromptDoc(content []byte) (promptDoc, error) {
	if !utf8.Valid(content) {
		return promptDoc{}, errors.New("not valid UTF-8")
	}
	text := strings.TrimPrefix(string(content), "\ufeff")
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	if loc := frontmatterBoundaryRE.FindStringIndex(text); loc == nil || loc[0] != 0 {
		return promptDoc{body: text}, nil
	}
	text = pyStrip(text)
	bounds := frontmatterBoundaryRE.FindAllStringIndex(text, 2)
	if len(bounds) < 2 {
		return promptDoc{}, &frontmatterError{errors.New("frontmatter has no closing --- line")}
	}
	doc := promptDoc{body: pyStrip(text[bounds[1][1]:])}
	node, err := yamlcore.SafeLoad([]byte(text[bounds[0][1]:bounds[1][0]]))
	if errors.Is(err, io.EOF) {
		return doc, nil
	}
	if err != nil {
		return promptDoc{}, &frontmatterError{err}
	}
	v, err := constructPy(node)
	if err != nil {
		return promptDoc{}, &frontmatterError{err}
	}
	if d, ok := v.(*pyDict); ok {
		// python-frontmatter passes the keys as keyword arguments
		// (Post(content, handler, **metadata)), which takes strings only.
		for _, k := range d.keys {
			if _, ok := k.(pyStr); !ok {
				return promptDoc{}, &frontmatterError{fmt.Errorf("frontmatter key %s is not a string", k.repr())}
			}
		}
		doc.meta = d
	}
	return doc, nil
}

// isWorkflowPrompt reports whether the prompt at path has the shape of a
// Copilot App workflow (copilot_app_workflow_integrator.py:90-113), which no
// target deploys as a prompt or a command. A file that cannot be read is
// not one.
func isWorkflowPrompt(path string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	doc, err := readPromptDoc(content)
	if err != nil || doc.meta == nil {
		return false
	}
	for _, key := range []string{"interval", "schedule_hour", "schedule_day"} {
		if doc.meta.get(pyStr(key)) != nil {
			return true
		}
	}
	return false
}

// commandKeys are the frontmatter keys the Oracle's command conversion reads
// (_PRESERVED_COMMAND_KEYS, command_integrator.py:46-56); every other key is
// dropped with a warning that lists commandKeysShown.
var commandKeys = map[string]bool{
	"description": true, "allowed-tools": true, "allowedTools": true, "model": true,
	"argument-hint": true, "argumentHint": true, "input": true,
}

const commandKeysShown = "allowed-tools, argument-hint, description, input, model"

// CommandNote is a line the Oracle's install prints about a prompt it
// deployed, or refused to deploy, as a command (utils/diagnostics.py:486-519).
type CommandNote struct {
	DepKey string // "" for the project's own content
	// Package is the name the warning is prefixed with: the package's own
	// name, "_local" for the project's own content.
	Package string
	Info    bool
	Text    string
	// phase orders the notes of one package as the Oracle's integration
	// phases come (rejectedKind); every other note is a command's.
	phase int
}

const (
	phasePrompts = iota - 2
	phaseAgents
	phaseCommands
)

// convertPromptToCommand ports CommandIntegrator._transform_prompt_to_command
// and the frontmatter.dumps of its result (command_integrator.py:196-273,
// 397): the frontmatter keeps description, allowed-tools, model and
// argument-hint, input becomes arguments, and ${input:name} in the body
// becomes $name. It returns the command's bytes and the notes the Oracle
// prints for it on target, warnings first.
func convertPromptToCommand(name, target string, content []byte) ([]byte, []CommandNote, error) {
	doc, err := readPromptDoc(content)
	if err != nil {
		return nil, nil, err
	}
	meta := doc.meta
	if meta == nil {
		meta = &pyDict{}
	}
	out := &pyDict{}
	copyKey := func(to string, from ...string) {
		for _, k := range from {
			if v := meta.get(pyStr(k)); v != nil {
				out.set(pyStr(to), v)
				return
			}
		}
	}
	copyKey("description", "description")
	copyKey("allowed-tools", "allowed-tools", "allowedTools")
	copyKey("model", "model")
	copyKey("argument-hint", "argument-hint", "argumentHint")

	var notes []CommandNote
	var dropped []string
	for _, k := range meta.keys {
		if !commandKeys[string(k.(pyStr))] {
			dropped = append(dropped, string(k.(pyStr)))
		}
	}
	sort.Strings(dropped)
	if len(dropped) > 0 {
		notes = append(notes, CommandNote{Text: fmt.Sprintf(
			"%s command %s: frontmatter keys not supported for %s commands and were dropped: %s. Supported keys: %s.",
			pyCapitalize(target), name, target, strings.Join(dropped, ", "), commandKeysShown)})
	}

	body := doc.body
	valid, rejected := inputNames(meta.get(pyStr("input")))
	if len(rejected) > 0 {
		shown := strings.Join(rejected[:min(len(rejected), 5)], ", ")
		if len(rejected) > 5 {
			shown += " ..."
		}
		notes = append(notes, CommandNote{Text: fmt.Sprintf(
			"input: rejected %d invalid name(s) (must match [A-Za-z][\\w-]{0,63}): %s", len(rejected), shown)})
	}
	if len(valid) > 0 {
		args := make(pyList, len(valid))
		hints := make([]string, len(valid))
		for i, n := range valid {
			args[i] = pyStr(n)
			hints[i] = "<" + n + ">"
		}
		out.set(pyStr("arguments"), args)
		if out.get(pyStr("argument-hint")) == nil {
			out.set(pyStr("argument-hint"), pyStr(strings.Join(hints, " ")))
		}
		body = inputReferenceRE.ReplaceAllString(body, "$$$1")
		notes = append(notes, CommandNote{Info: true, Text: fmt.Sprintf(
			"Mapped input -> command arguments in %s.md: [%s]", name, strings.Join(valid, ", "))})
	}

	command := pyStrip("---\n" + pyDump(out) + "\n---\n\n" + body)
	return []byte(command), notes, nil
}

// inputNames ports _extract_input_names (command_integrator.py:78-136): the
// argument names of an input value, and the entries it rejects, as the
// warning shows them.
func inputNames(spec pyValue) (valid, rejected []string) {
	accept := func(candidate pyValue) {
		s, ok := candidate.(pyStr)
		if !ok {
			rejected = append(rejected, candidate.repr())
			return
		}
		switch stripped := pyStrip(string(s)); {
		case stripped == "":
		case inputNameRE.MatchString(stripped):
			valid = append(valid, stripped)
		default:
			rejected = append(rejected, stripped)
		}
	}
	switch x := spec.(type) {
	case pyList:
		for _, item := range x {
			switch it := item.(type) {
			case pyStr:
				accept(it)
			case *pyDict:
				for _, k := range it.keys {
					accept(k)
				}
			default:
				rejected = append(rejected, item.repr())
			}
		}
	case pyStr:
		accept(x)
	case *pyDict:
		for _, k := range x.keys {
			accept(k)
		}
	}
	return valid, rejected
}

// pyCapitalize is Python's str.capitalize() for an ASCII target name.
func pyCapitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

// commandNameError is a prompt whose name the Oracle refuses as a command
// file name (validate_path_segments, utils/path_security.py:124-170).
type commandNameError struct{ name, segment string }

func (e *commandNameError) Error() string {
	return fmt.Sprintf("Invalid command filename '%s': segment '%s' is a traversal sequence", e.name, e.segment)
}

func checkCommandName(name string) error {
	for _, segment := range strings.Split(strings.ReplaceAll(name, `\`, "/"), "/") {
		decoded := segment
		for range 8 {
			next := pyUnquote(decoded)
			if next == decoded {
				break
			}
			decoded = next
		}
		if segment == "." || segment == ".." || decoded == "." || decoded == ".." {
			return &commandNameError{name, segment}
		}
	}
	return nil
}

// pyUnquote is urllib.parse.unquote: each %XX is that byte, anything else is
// kept, and the bytes are read as UTF-8 with U+FFFD for what is not.
func pyUnquote(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			b = append(b, unhex(s[i+1])<<4|unhex(s[i+2]))
			i += 2
			continue
		}
		b = append(b, s[i])
	}
	return strings.ToValidUTF8(string(b), "\ufffd")
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c <= '9':
		return c - '0'
	case c <= 'F':
		return c - 'A' + 10
	}
	return c - 'a' + 10
}

// deployPromptCommand writes the prompt p, converted, to destPath: a file of
// its own also for a symlink deploy, as the claude rules are
// (deployClaudeInstructions).
func deployPromptCommand(p Primitive, deployDir, destPath, target string) ([]string, []CommandNote, error) {
	if err := checkCommandName(p.Name); err != nil {
		return nil, nil, err
	}
	content, err := os.ReadFile(p.SrcPath)
	if err != nil {
		return nil, nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	command, notes, err := convertPromptToCommand(p.Name, target, content)
	if err != nil {
		return nil, nil, err
	}
	if err := CheckDeployWrite(deployDir, destPath); err != nil {
		return nil, nil, err
	}
	absDest := filepath.Join(deployDir, filepath.FromSlash(destPath))
	if err := os.MkdirAll(filepath.Dir(absDest), 0755); err != nil {
		return nil, nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	if err := os.WriteFile(absDest, command, 0644); err != nil {
		return nil, nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	return []string{destPath}, notes, nil
}
