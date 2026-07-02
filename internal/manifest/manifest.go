package manifest

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/yamlcore"
)

type DiagLevel int

const (
	LevelWarning DiagLevel = iota
	LevelError
)

type Diagnostic struct {
	Level   DiagLevel
	Req     string
	Message string
}

type Registry struct {
	URL      string
	Insecure bool
	Aliases  []string
}

type Manifest struct {
	Name               string
	Version            string
	Description        string
	Author             string
	License            string
	DefaultHost        string
	Target             []string
	Type               string
	Scripts            map[string]string
	Registries         map[string]Registry
	Includes           any // "auto" or []string
	Workspaces         bool
	ConflictResolution string
	DefaultRegistry    string // registries.default (empty = none)
	ParsedDeps         []*DependencyReference
	ParsedDevDeps      []*DependencyReference
	MCPServers         []*MCPDependency
	MCPDevServers      []*MCPDependency

	node *yaml.Node
}

var semverRegex = regexp.MustCompile(
	`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
		`(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?` +
		`(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`,
)

func ParseManifest(doc *yaml.Node) (*Manifest, []Diagnostic, error) {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, nil, fmt.Errorf("invalid YAML document")
	}
	root := doc.Content[0]

	// mf-001: top-level must be mapping
	if root.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("top-level must be a YAML mapping")
	}

	m := &Manifest{node: doc}
	var diags []Diagnostic

	for i := 0; i < len(root.Content)-1; i += 2 {
		key := root.Content[i]
		val := root.Content[i+1]
		k := key.Value

		switch k {
		case "name":
			m.Name = val.Value
		case "version":
			m.Version = val.Value
		case "description":
			m.Description = val.Value
		case "author":
			m.Author = val.Value
		case "license":
			m.License = val.Value
		case "default_host":
			m.DefaultHost = val.Value
		case "type":
			m.Type = val.Value
		case "target":
			targets, err := parseTargetField(val)
			if err != nil {
				return nil, nil, err
			}
			m.Target = targets
		case "includes":
			if val.Kind == yaml.ScalarNode {
				m.Includes = val.Value
			} else if val.Kind == yaml.SequenceNode {
				var paths []string
				for _, item := range val.Content {
					paths = append(paths, item.Value)
				}
				m.Includes = paths
			}
		case "scripts":
			m.Scripts = parseStringMap(val)
		case "registries":
			regs, def, err := parseRegistries(val)
			if err != nil {
				return nil, nil, err
			}
			m.Registries = regs
			m.DefaultRegistry = def
		case "dependencies":
			deps, mcpServers, cr, err := validateDepBlock(val)
			if err != nil {
				return nil, nil, err
			}
			m.ParsedDeps = append(m.ParsedDeps, deps...)
			m.MCPServers = append(m.MCPServers, mcpServers...)
			if cr != "" {
				m.ConflictResolution = cr
			}
		case "devDependencies":
			deps, mcpServers, _, err := validateDepBlock(val)
			if err != nil {
				return nil, nil, err
			}
			m.ParsedDevDeps = append(m.ParsedDevDeps, deps...)
			m.MCPDevServers = append(m.MCPDevServers, mcpServers...)
		case "workspaces":
			// mf-021: non-blocking diagnostic
			m.Workspaces = true
			diags = append(diags, Diagnostic{
				Level:   LevelWarning,
				Req:     "req-mf-021",
				Message: "workspaces is reserved for v0.2; ignored",
			})
		case "policy":
			if err := validatePolicyBlock(val); err != nil {
				return nil, nil, err
			}
		case "marketplace":
			if err := validateMarketplaceBlock(val); err != nil {
				return nil, nil, err
			}
		default:
			// Unknown keys (including x-*) preserved by Node — no action needed
		}
	}

	// mf-002: name required, non-empty
	if m.Name == "" {
		return nil, nil, fmt.Errorf("name is required and must be a non-empty string")
	}

	// mf-003: version required
	if m.Version == "" {
		return nil, nil, fmt.Errorf("version is required")
	}

	// mf-004: version SHOULD match semver
	if !semverRegex.MatchString(m.Version) {
		diags = append(diags, Diagnostic{
			Level:   LevelWarning,
			Req:     "req-mf-004",
			Message: fmt.Sprintf("version %q does not match semver 2.0.0", m.Version),
		})
	}

	// Target adapter diagnostics (tg-004)
	for _, t := range m.Target {
		if t != "all" && !HasAdapter(t) {
			diags = append(diags, Diagnostic{
				Level:   LevelWarning,
				Req:     "req-tg-004",
				Message: fmt.Sprintf("no registered handler for target %q", t),
			})
		}
	}

	return m, diags, nil
}

func parseTargetField(val *yaml.Node) ([]string, error) {
	switch val.Kind {
	case yaml.ScalarNode:
		normalized, err := ValidateTarget(val.Value)
		if err != nil {
			return nil, err
		}
		return []string{normalized}, nil
	case yaml.SequenceNode:
		var targets []string
		for _, item := range val.Content {
			normalized, err := ValidateTarget(item.Value)
			if err != nil {
				return nil, err
			}
			targets = append(targets, normalized)
		}
		return targets, nil
	default:
		return nil, fmt.Errorf("target must be a string or list of strings")
	}
}

func parseStringMap(val *yaml.Node) map[string]string {
	if val.Kind != yaml.MappingNode {
		return nil
	}
	m := make(map[string]string)
	for i := 0; i < len(val.Content)-1; i += 2 {
		m[val.Content[i].Value] = val.Content[i+1].Value
	}
	return m
}

func parseRegistries(val *yaml.Node) (map[string]Registry, string, error) {
	if val.Kind != yaml.MappingNode {
		return nil, "", fmt.Errorf("registries must be a mapping")
	}
	regs := make(map[string]Registry)
	var defaultName string
	for i := 0; i < len(val.Content)-1; i += 2 {
		name := val.Content[i].Value

		// "default" key is a scalar registry name, not a registry entry.
		if name == "default" {
			if val.Content[i+1].Kind == yaml.ScalarNode {
				defaultName = val.Content[i+1].Value
			}
			continue
		}

		entry := val.Content[i+1]
		if entry.Kind != yaml.MappingNode {
			return nil, "", fmt.Errorf("registries.%s must be a mapping", name)
		}

		var reg Registry
		for j := 0; j < len(entry.Content)-1; j += 2 {
			k := entry.Content[j].Value
			v := entry.Content[j+1]
			switch k {
			case "url":
				reg.URL = v.Value
			case "insecure":
				reg.Insecure = strings.EqualFold(v.Value, "true")
			case "aliases":
				if v.Kind == yaml.SequenceNode {
					for _, a := range v.Content {
						reg.Aliases = append(reg.Aliases, a.Value)
					}
				}
			default:
				if !yamlcore.IsVendorExtKey(k) {
					// mf-015: reject unknown keys (typo guard)
					return nil, "", fmt.Errorf("unknown key %q in registries.%s", k, name)
				}
			}
		}

		// mf-014: URL must be https or http
		if reg.URL == "" {
			return nil, "", fmt.Errorf("registries.%s.url is required", name)
		}
		if !strings.HasPrefix(reg.URL, "https://") && !strings.HasPrefix(reg.URL, "http://") {
			return nil, "", fmt.Errorf("registries.%s.url must use https:// or http:// scheme", name)
		}
		// sc-007/008: reject embedded credentials — they would leak into
		// resolved_url in the lockfile and bypass the credential attach gate.
		if u, perr := url.Parse(reg.URL); perr == nil && u.User != nil {
			return nil, "", fmt.Errorf("registries.%s.url must not contain embedded credentials (userinfo)", name)
		}

		// sc-006: http:// requires insecure or loopback/private
		if strings.HasPrefix(reg.URL, "http://") && !reg.Insecure {
			u, err := url.Parse(reg.URL)
			if err != nil || !isLoopbackOrPrivate(u.Hostname()) {
				return nil, "", fmt.Errorf("registries.%s.url uses http:// without insecure:true or loopback/private host", name)
			}
		}

		regs[name] = reg
	}
	return regs, defaultName, nil
}

// EffectiveRegistry resolves which registry a source=="registry" dependency uses:
// the dependency's own registry name, else the manifest default. Empty on both is
// an error (unconfigured default registry).
func (m *Manifest) EffectiveRegistry(ref *DependencyReference) (string, error) {
	name := ref.RegistryName
	if name == "" {
		name = m.DefaultRegistry
	}
	if name == "" {
		return "", fmt.Errorf("registry dependency %q has no registry and no default registry is configured", ref.RepoURL)
	}
	return name, nil
}

func isLoopbackOrPrivate(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		// Try resolving — but for parse-time we only check literal IPs
		if host == "localhost" {
			return true
		}
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

// validateDepBlock checks structural validity of a dependencies/devDependencies block.
// Returns collected DependencyReferences and conflict_resolution value.
func validateDepBlock(val *yaml.Node) ([]*DependencyReference, []*MCPDependency, string, error) {
	if val.Kind != yaml.MappingNode {
		return nil, nil, "", fmt.Errorf("dependencies must be a mapping")
	}
	var deps []*DependencyReference
	var mcpServers []*MCPDependency
	var conflictRes string
	for i := 0; i < len(val.Content)-1; i += 2 {
		k := val.Content[i].Value
		list := val.Content[i+1]

		if k == "conflict_resolution" {
			if list.Kind == yaml.ScalarNode {
				conflictRes = list.Value
			}
		} else if k == "apm" {
			if list.Kind != yaml.SequenceNode {
				return nil, nil, "", fmt.Errorf("dependencies.apm must be a list")
			}
			for idx, entry := range list.Content {
				if entry.Kind == yaml.MappingNode {
					d, err := ParseDepDict(entry, idx)
					if err != nil {
						return nil, nil, "", err
					}
					deps = append(deps, d)
				} else if entry.Kind == yaml.ScalarNode {
					d, err := ParseDepString(entry.Value)
					if err != nil {
						return nil, nil, "", err
					}
					deps = append(deps, d)
				}
			}
		} else if k == "mcp" {
			if list.Kind != yaml.SequenceNode {
				return nil, nil, "", fmt.Errorf("dependencies.mcp must be a list")
			}
			for _, entry := range list.Content {
				m, err := ParseMCPEntry(entry)
				if err != nil {
					return nil, nil, "", err
				}
				if err := ValidateMCP(m); err != nil {
					return nil, nil, "", err
				}
				mcpServers = append(mcpServers, m)
			}
		}
	}
	return deps, mcpServers, conflictRes, nil
}

// validateDepEntry is replaced by ParseDepDict in depref.go

func isLocalPath(s string) bool {
	return strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") ||
		strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~/") ||
		strings.HasPrefix(s, ".\\") || strings.HasPrefix(s, "..\\") ||
		strings.HasPrefix(s, "~\\")
}

func containsEscape(p string) bool {
	p = strings.ReplaceAll(p, "\\", "/")
	parts := strings.Split(p, "/")
	depth := 0
	for _, part := range parts {
		if part == ".." {
			depth--
			if depth < 0 {
				return true
			}
		} else if part != "." && part != "" {
			depth++
		}
	}
	return false
}

func validatePolicyBlock(val *yaml.Node) error {
	if val.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(val.Content)-1; i += 2 {
		k := val.Content[i].Value
		v := val.Content[i+1]
		if k == "hash_algorithm" {
			algo := v.Value
			switch algo {
			case "sha256", "sha384", "sha512":
				// ok
			default:
				// mf-018
				return fmt.Errorf("policy.hash_algorithm %q is not supported; use sha256, sha384, or sha512", algo)
			}
		}
	}
	return nil
}

func validateMarketplaceBlock(val *yaml.Node) error {
	if val.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(val.Content)-1; i += 2 {
		k := val.Content[i].Value
		v := val.Content[i+1]
		if k == "packages" && v.Kind == yaml.SequenceNode {
			for _, pkg := range v.Content {
				if pkg.Kind != yaml.MappingNode {
					continue
				}
				for j := 0; j < len(pkg.Content)-1; j += 2 {
					if pkg.Content[j].Value == "source" {
						src := pkg.Content[j+1].Value
						if err := ValidateMarketplaceSource(src); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

// NodeHasKey checks if a mapping node contains a key with the given name.
func NodeHasKey(node *yaml.Node, key string) bool {
	if node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			return true
		}
	}
	return false
}
