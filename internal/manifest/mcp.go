package manifest

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v4"
)

type MCPDependency struct {
	Name      string
	Transport string
	Command   string
	Args      *[]string // nil = absent; empty slice = explicit []
	URL       string
	Env       map[string]string
	Headers   map[string]string
	Registry  any // nil=default, false=self-defined, string=custom URL
}

func ParseMCPEntry(entry *yaml.Node) (*MCPDependency, error) {
	if entry.Kind == yaml.ScalarNode {
		return &MCPDependency{Name: entry.Value}, nil
	}
	if entry.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("MCP entry must be a string or mapping")
	}

	m := &MCPDependency{}
	for i := 0; i < len(entry.Content)-1; i += 2 {
		k := entry.Content[i].Value
		v := entry.Content[i+1]
		switch k {
		case "name":
			m.Name = v.Value
		case "transport":
			m.Transport = v.Value
		case "command":
			m.Command = v.Value
		case "url":
			m.URL = v.Value
		case "registry":
			if v.Tag == "!!bool" || v.Value == "false" || v.Value == "true" {
				if strings.EqualFold(v.Value, "false") {
					m.Registry = false
				} else {
					m.Registry = true
				}
			} else if v.Kind == yaml.ScalarNode {
				m.Registry = v.Value
			}
		case "args":
			args := []string{}
			if v.Kind == yaml.SequenceNode {
				for _, a := range v.Content {
					args = append(args, a.Value)
				}
			}
			m.Args = &args
		case "env":
			if v.Kind == yaml.MappingNode {
				m.Env = make(map[string]string)
				for j := 0; j < len(v.Content)-1; j += 2 {
					m.Env[v.Content[j].Value] = v.Content[j+1].Value
				}
			}
		case "headers":
			if v.Kind == yaml.MappingNode {
				m.Headers = make(map[string]string)
				for j := 0; j < len(v.Content)-1; j += 2 {
					m.Headers[v.Content[j].Value] = v.Content[j+1].Value
				}
			}
		}
	}
	return m, nil
}

func ValidateMCP(m *MCPDependency) error {
	isSelfDefined := m.Registry == false
	if !isSelfDefined {
		return nil
	}

	if m.Transport == "" {
		return fmt.Errorf("self-defined MCP server requires 'transport'")
	}

	switch m.Transport {
	case "stdio":
		if m.Command == "" {
			return fmt.Errorf("MCP transport 'stdio' requires 'command'")
		}
		if strings.ContainsAny(m.Command, " \t") && m.Args == nil {
			return fmt.Errorf("MCP command %q contains whitespace but no 'args' key is present", m.Command)
		}
	case "http", "sse", "streamable-http":
		if m.URL == "" {
			return fmt.Errorf("MCP transport %q requires 'url'", m.Transport)
		}
	default:
		return fmt.Errorf("unknown MCP transport %q", m.Transport)
	}

	return nil
}

// ── Placeholder recognition (mf-013) — recognition only, no parse-time rejection ──

var (
	EnvVarRe   = regexp.MustCompile(`\$\{(?:env:)?([A-Za-z_][A-Za-z0-9_]*)\}`)
	InputVarRe = regexp.MustCompile(`\$\{input:([^}]+)\}`)
	ActionsRe  = regexp.MustCompile(`\$\{\{.*?\}\}`)
)

type PlaceholderType int

const (
	PlaceholderEnv PlaceholderType = iota
	PlaceholderInput
	PlaceholderActions
)

type Placeholder struct {
	Type    PlaceholderType
	Raw     string
	VarName string
}

func RecognizePlaceholders(s string) []Placeholder {
	var result []Placeholder

	for _, m := range ActionsRe.FindAllString(s, -1) {
		result = append(result, Placeholder{Type: PlaceholderActions, Raw: m})
	}
	for _, m := range InputVarRe.FindAllStringSubmatch(s, -1) {
		result = append(result, Placeholder{Type: PlaceholderInput, Raw: m[0], VarName: m[1]})
	}
	for _, m := range EnvVarRe.FindAllStringSubmatch(s, -1) {
		result = append(result, Placeholder{Type: PlaceholderEnv, Raw: m[0], VarName: m[1]})
	}
	return result
}

// ── Marketplace source validation (mf-017) ──

func ValidateMarketplaceSource(source string) error {
	if source == "" {
		return fmt.Errorf("marketplace source is empty")
	}

	// (a) reject .. segments
	for _, seg := range strings.Split(source, "/") {
		if seg == ".." {
			return fmt.Errorf("marketplace source %q contains '..' path segment", source)
		}
	}

	// local path must start with ./
	if strings.HasPrefix(source, ".") {
		if !strings.HasPrefix(source, "./") {
			return fmt.Errorf("local marketplace source must start with './'")
		}
		return nil
	}

	// remote URL
	if strings.Contains(source, "://") {
		u, err := url.Parse(source)
		if err != nil {
			return fmt.Errorf("marketplace source %q is not a valid URL", source)
		}
		// (c) https only for remote
		if u.Scheme != "https" {
			return fmt.Errorf("remote marketplace source must use https://, got %q", u.Scheme)
		}
		// (b) no userinfo
		if u.User != nil {
			return fmt.Errorf("marketplace source %q must not contain userinfo", source)
		}
		// (b) no port
		if u.Port() != "" {
			return fmt.Errorf("marketplace source %q must not contain a port", source)
		}
		// (b) no query
		if u.RawQuery != "" {
			return fmt.Errorf("marketplace source %q must not contain a query string", source)
		}
		return nil
	}

	// shorthand form (host/owner/repo or owner/repo) — accepted
	return nil
}
