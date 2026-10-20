package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

// codexAgentDoc is the three-key document the Oracle writes to
// .codex/agents/<name>.toml (integration/agent_integrator.py:457-517). Name
// and Description hold what PyYAML read from the frontmatter; a pyNone is a
// key toml.dumps leaves out.
type codexAgentDoc struct {
	Name                  pyValue
	Description           pyValue
	DeveloperInstructions string
}

// transformCodexAgent reads sourcePath as the Oracle's _write_codex_agent
// does: a symlink is refused; name defaults to fileName (the stem with a
// trailing ".agent" stripped) and description to ""; a frontmatter block
// matching the Oracle's _FRONTMATTER_RE is cut from the body whether or not
// it loads, and when it loads to a mapping its "name" and "description"
// replace the defaults. A value PyYAML gives as a float, list, mapping or
// date keeps the default, as apm-go did before issue #123; its TOML is not
// the Oracle's.
func transformCodexAgent(sourcePath, fileName string) (codexAgentDoc, error) {
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return codexAgentDoc{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return codexAgentDoc{}, fmt.Errorf("codex agent source is a symlink: %s", sourcePath)
	}

	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return codexAgentDoc{}, err
	}

	doc := codexAgentDoc{Name: pyStr(fileName), Description: pyStr("")}
	text := string(oracleText(data))
	body := text
	if loc := agentFrontmatterRE.FindStringSubmatchIndex(text); loc != nil {
		body = text[loc[1]:]
		if fm := loadFrontmatterDict(text[loc[2]:loc[3]]); fm != nil {
			doc.Name = codexScalar(fm, "name", doc.Name)
			doc.Description = codexScalar(fm, "description", doc.Description)
		}
	}
	doc.DeveloperInstructions = pyStrip(body)
	return doc, nil
}

func loadFrontmatterDict(text string) *pyDict {
	node, err := yamlcore.SafeLoad([]byte(text))
	if err != nil {
		return nil
	}
	v, err := constructPy(node)
	if err != nil {
		return nil
	}
	d, _ := v.(*pyDict)
	return d
}

func codexScalar(fm *pyDict, key string, def pyValue) pyValue {
	switch v := fm.get(pyStr(key)).(type) {
	case pyStr, pyInt, pyBool, pyNone:
		return v
	}
	return def
}

// bytes is toml 0.10.2's dumps of the document (toml/encoder.py:35-84,
// 175-230): one "key = value" line per key that is not None, in order.
func (d codexAgentDoc) bytes() []byte {
	var sb strings.Builder
	for _, kv := range []struct {
		key string
		v   pyValue
	}{{"name", d.Name}, {"description", d.Description}, {"developer_instructions", pyStr(d.DeveloperInstructions)}} {
		var value string
		switch v := kv.v.(type) {
		case pyNone:
			continue
		case pyStr:
			value = tomlDumpStr(string(v))
		case pyBool:
			value = strings.ToLower(v.repr())
		case pyInt:
			value = v.str()
		}
		sb.WriteString(kv.key + " = " + value + "\n")
	}
	return []byte(sb.String())
}

// tomlDumpStr is toml 0.10.2's _dump_str (toml/encoder.py:86-114): Python's
// repr() without its quotes, a single-quoted one with \' read back and "
// escaped, in double quotes. Where repr() has a \x (a character written as
// \xhh, or a backslash before an x in the text), _dump_str drops a
// backslash and changes the text or writes TOML that does not parse, so
// apm-go writes each \xhh as \u00hh and keeps every other escape. Deviation
// (issue #123 comment 6284804045, item 1).
func tomlDumpStr(s string) string {
	r := manifest.PyRepr(s)
	v := r[1 : len(r)-1]
	if r[0] == '\'' {
		v = strings.ReplaceAll(v, `\'`, `'`)
		v = strings.ReplaceAll(v, `"`, `\"`)
	}
	if strings.Contains(v, `\x`) {
		var sb strings.Builder
		for i := 0; i < len(v); i++ {
			if v[i] == '\\' && i+1 < len(v) {
				if v[i+1] == 'x' {
					sb.WriteString(`\u00`)
				} else {
					sb.WriteString(v[i : i+2])
				}
				i++
				continue
			}
			sb.WriteByte(v[i])
		}
		v = sb.String()
	}
	return `"` + v + `"`
}

// deployCodexAgentTOML writes the TypeAgents primitive p, as codexAgentTOML
// gives it, to .codex/agents/<p.Name>.toml.
func deployCodexAgentTOML(p Primitive, projectDir string) ([]string, error) {
	data, err := codexAgentTOML(p)
	if err != nil {
		return nil, err
	}
	destPath := fmt.Sprintf(".codex/agents/%s.toml", p.Name)
	if err := CheckDeployWrite(projectDir, destPath); err != nil {
		return nil, err
	}
	switch placeFile(p, destPath) {
	case placeSkip:
		return nil, nil
	case placeAdopt:
		return []string{destPath}, nil
	}
	absDest := filepath.Join(projectDir, filepath.FromSlash(destPath))
	if err := os.MkdirAll(filepath.Dir(absDest), 0755); err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	if err := os.WriteFile(absDest, data, 0644); err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	return []string{destPath}, nil
}

// codexAgentTOML converts the agent p from markdown to the TOML document
// Codex CLI reads; a byte copy of the markdown would not parse.
func codexAgentTOML(p Primitive) ([]byte, error) {
	doc, err := transformCodexAgent(p.SrcPath, p.Name)
	if err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	return doc.bytes(), nil
}
