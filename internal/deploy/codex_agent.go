package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v4"
)

// codexAgentDoc is the three-key TOML document Codex CLI expects at
// .codex/agents/<name>.toml, mirroring Python's _write_codex_agent
// (agent_integrator.py:302-335). Field order matches the oracle's emitted
// key order.
type codexAgentDoc struct {
	Name                  string `toml:"name"`
	Description           string `toml:"description"`
	DeveloperInstructions string `toml:"developer_instructions"`
}

// transformCodexAgent reads sourcePath and converts it into a codexAgentDoc,
// mirroring the Python oracle's exact semantics (agent_integrator.py:302-335):
//  1. Symlink sources are rejected outright (Lstat, never followed).
//  2. name defaults to the filename stem with a trailing ".agent" stripped
//     (e.g. "foo.agent.md" -> "foo").
//  3. A frontmatter block matching ^---\s*\n(.*?)\n---\s*\n? (DOTALL) at the
//     very start of the file is cut from the body regardless of whether it
//     parses as YAML. When it does parse, string "name"/"description" keys
//     override the defaults; a parse failure is swallowed silently (matching
//     Python's bare `except: pass`) and defaults stand. Any other key
//     (model, tools, ...) is ignored.
//  4. description defaults to "".
//  5. developer_instructions = body with frontmatter removed, then
//     strings.TrimSpace (Python .strip()).
func transformCodexAgent(sourcePath string) (codexAgentDoc, error) {
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

	doc := codexAgentDoc{Name: codexAgentNameFallback(sourcePath)}
	text := string(data)
	body := text
	if loc := claudeFrontmatterRE.FindStringSubmatchIndex(text); loc != nil {
		body = text[loc[1]:]
		var fm map[string]any
		if err := yaml.Unmarshal([]byte(text[loc[2]:loc[3]]), &fm); err == nil {
			if v, ok := fm["name"].(string); ok {
				doc.Name = v
			}
			if v, ok := fm["description"].(string); ok {
				doc.Description = v
			}
		}
	}
	doc.DeveloperInstructions = strings.TrimSpace(body)
	return doc, nil
}

// codexAgentNameFallback derives the default agent name from the source
// filename: the stem (extension removed) with a trailing ".agent" stripped.
func codexAgentNameFallback(sourcePath string) string {
	stem := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
	return strings.TrimSuffix(stem, ".agent")
}

// deployCodexAgentTOML converts a TypeAgents primitive from markdown to the
// codexAgentDoc TOML document and writes it to .codex/agents/<p.Name>.toml.
// Byte-copying the markdown source here would produce a file with a .toml
// extension that Codex CLI cannot parse (research/findings.md).
func deployCodexAgentTOML(p Primitive, projectDir string) ([]string, error) {
	doc, err := transformCodexAgent(p.SrcPath)
	if err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	data, err := toml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	destPath := fmt.Sprintf(".codex/agents/%s.toml", p.Name)
	absDest := filepath.Join(projectDir, filepath.FromSlash(destPath))
	if err := os.MkdirAll(filepath.Dir(absDest), 0755); err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	if err := os.WriteFile(absDest, data, 0644); err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	return []string{destPath}, nil
}
