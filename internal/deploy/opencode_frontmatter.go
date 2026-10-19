package deploy

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/apm-go/apm/internal/yamlcore"
)

// agentFrontmatterRE is AgentIntegrator._FRONTMATTER_RE
// (integration/agent_integrator.py:417-420).
var agentFrontmatterRE = regexp.MustCompile(`(?s)\A---` + pyWhitespace + `*\n(.*?)\n---` + pyWhitespace + `*\n?`)

var opencodeHexColorRE = regexp.MustCompile(`\A#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})\n?\z`)

var opencodeThemeColors = map[string]bool{
	"primary": true, "secondary": true, "accent": true, "success": true,
	"warning": true, "error": true, "info": true,
}

// opencodeAgentNotes ports AgentIntegrator._warn_opencode_frontmatter and
// validate_opencode_frontmatter (integration/agent_integrator.py:370-413,
// opencode_frontmatter.py:46-116): the warnings the Oracle prints for an
// agent it writes to .opencode/agents/, whose frontmatter OpenCode refuses.
// A source it cannot read as text, or whose frontmatter does not load, gets
// none. file is the agent's file name in the Oracle's apm_modules, where a
// symlink of the package is a file of the symlink's name.
func opencodeAgentNotes(src, file, pkg string) []CommandNote {
	if info, err := os.Lstat(src); err != nil || info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	content, err := os.ReadFile(src)
	if err != nil || !utf8.Valid(content) {
		return nil
	}
	m := agentFrontmatterRE.FindSubmatch(oracleText(content))
	if m == nil {
		return nil
	}
	node, err := yamlcore.SafeLoad(m[1])
	if err != nil {
		return nil
	}
	v, err := constructPy(node)
	if err != nil {
		return nil
	}
	fm, ok := v.(*pyDict)
	if !ok || len(fm.keys) == 0 {
		return nil
	}
	id := printableASCII(file)
	if pkg != "" {
		id = printableASCII(pkg) + "/" + id
	}
	var texts []string
	if tools := fm.get(pyStr("tools")); tools != nil {
		if d, ok := tools.(*pyDict); !ok {
			texts = append(texts, fmt.Sprintf("OpenCode agent '%s' has tools as %s; "+
				"OpenCode requires a mapping of tool-name to boolean. "+
				"OpenCode will reject this agent at load time. "+
				"Fix: rewrite the frontmatter as 'tools: {Read: true, Grep: true}'.", id, pyTypeName(tools)))
		} else {
			for i, k := range d.keys {
				_, keyStr := k.(pyStr)
				_, valBool := d.values[i].(pyBool)
				if !keyStr || !valBool {
					texts = append(texts, fmt.Sprintf("OpenCode agent '%s' has a non-boolean tool entry "+
						"(%s: %s); OpenCode requires string-keyed boolean values. "+
						"OpenCode will reject this agent at load time. "+
						"Fix: set every tool entry to 'true' or 'false' (e.g. 'Read: true').",
						id, pyASCII(k), pyASCII(d.values[i])))
					break
				}
			}
		}
	}
	if color := fm.get(pyStr("color")); color != nil {
		if s, ok := color.(pyStr); !ok || !opencodeThemeColors[string(s)] && !opencodeHexColorRE.MatchString(string(s)) {
			texts = append(texts, fmt.Sprintf("OpenCode agent '%s' has color=%s; "+
				"OpenCode requires a hex value (e.g. '#aabbcc') or one of "+
				"['accent', 'error', 'info', 'primary', 'secondary', 'success', 'warning']. "+
				"OpenCode will reject this agent at load time. "+
				"Fix: replace the color with a '#rgb' or '#rrggbb' hex literal "+
				"or one of the listed theme names.", id, pyASCII(color)))
		}
	}
	notes := make([]CommandNote, len(texts))
	for i, t := range texts {
		notes[i] = CommandNote{Package: printableASCII(pkg), Text: t, phase: phaseAgents}
	}
	return notes
}

// pyTypeName is type(v).__name__ of what constructPy builds.
func pyTypeName(v pyValue) string {
	switch v.(type) {
	case pyNone:
		return "NoneType"
	case pyBool:
		return "bool"
	case pyInt:
		return "int"
	case pyFloat:
		return "float"
	case pyStr:
		return "str"
	case pyDate:
		return "date"
	case pyDatetime:
		return "datetime"
	case pyList:
		return "list"
	}
	return "dict"
}

// pyASCII is Python's ascii(): repr() with each non-ASCII character escaped.
func pyASCII(v pyValue) string {
	var b strings.Builder
	for _, r := range v.repr() {
		switch {
		case r < 0x80:
			b.WriteRune(r)
		case r <= 0xff:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r <= 0xffff:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	return b.String()
}

// printableASCII ports printable_ascii_text (utils/diagnostics.py:53-56):
// each non-ASCII character, and each byte a file name has that is not UTF-8,
// which Python decodes to one surrogate each, is a ?, and so is a control
// character.
func printableASCII(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r >= 0x7f {
			b.WriteByte('?')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
