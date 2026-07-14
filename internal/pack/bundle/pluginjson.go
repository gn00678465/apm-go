package bundle

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Author is plugin.json's structured author object.
type Author struct {
	Name  string
	Email string
	URL   string
}

// PluginManifest is a synthesized plugin.json payload, in the exact field
// order Python's synthesize_plugin_json_from_apm_yml/build_plugin_manifest
// write them (deps/plugin_parser.py:963-990): name, version, description,
// author, license, homepage, repository, keywords, then (added by
// build_plugin_manifest, claude only) mcpServers.
//
// This type lives in package bundle (rather than pluginmanifest, its
// primary consumer) so BundleProducer's own "find existing plugin.json OR
// synthesize one" fallback (_find_or_synthesize_plugin_json,
// plugin_exporter.py:336-352) can call Synthesize directly without
// importing pluginmanifest -- pluginmanifest already needs to import
// bundle for JSONValue/ReadMCPServers/SanitizeServers, so the reverse
// import would be a package cycle.
type PluginManifest struct {
	Name        string
	Version     string
	Description string
	Author      *Author
	License     string
	Homepage    string
	Repository  string
	Keywords    []string
	// MCPServers is only set for the claude ecosystem; nil/empty means
	// "omit the mcpServers key entirely" (matches Python's manifest.pop /
	// conditional assignment, build_plugin_manifest:372-378).
	MCPServers *JSONValue
}

// Synthesize builds a PluginManifest by reading root (apm.yml's top-level
// YAML mapping node) directly -- deliberately independent of
// manifest.Manifest (design.md "Surgical Changes": PluginManifestProducer's
// extra fields -- Homepage/Repository/Keywords/structured Author -- are not
// added to the reviewed public manifest.Manifest schema; this is apm.yml's
// own narrow second read, mirroring output.go's LoadOutputPathOverrides
// precedent). Mirrors synthesize_plugin_json_from_apm_yml
// (deps/plugin_parser.py:930-992) field-for-field. name is required: an
// apm.yml with a missing or empty name: yields an error (matching Python's
// ValueError), just like manifest.ParseManifest's own mf-002 check --
// callers run their own required-field check because this reads the YAML
// directly rather than through ParseManifest.
func Synthesize(root *yaml.Node) (*PluginManifest, error) {
	if root == nil || root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("apm.yml must contain at least a 'name' field to synthesize plugin.json")
	}

	name := scalarField(root, "name")
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("apm.yml must contain at least a 'name' field to synthesize plugin.json")
	}

	m := &PluginManifest{Name: name}
	m.Version = scalarField(root, "version")
	m.Description = scalarField(root, "description")
	m.Author = synthesizeAuthor(mappingValue(root, "author"))
	m.License = scalarField(root, "license")
	m.Homepage = scalarField(root, "homepage")
	m.Repository = scalarField(root, "repository")
	m.Keywords = synthesizeKeywords(mappingValue(root, "keywords"))
	return m, nil
}

// synthesizeAuthor mirrors plugin_parser.py:969-981: a plain scalar author
// becomes {"name": author}; a mapping keeps only name (required -- the
// WHOLE author field is dropped if absent)/email/url.
func synthesizeAuthor(val *yaml.Node) *Author {
	if val == nil {
		return nil
	}
	switch val.Kind {
	case yaml.ScalarNode:
		if val.Tag == "!!null" || val.Value == "" {
			return nil
		}
		return &Author{Name: val.Value}
	case yaml.MappingNode:
		name := scalarField(val, "name")
		if name == "" {
			return nil
		}
		return &Author{
			Name:  name,
			Email: scalarField(val, "email"),
			URL:   scalarField(val, "url"),
		}
	default:
		return nil
	}
}

// synthesizeKeywords mirrors plugin_parser.py:988-990: a single scalar
// becomes a one-element list; a sequence is copied element-for-element.
func synthesizeKeywords(val *yaml.Node) []string {
	if val == nil {
		return nil
	}
	switch val.Kind {
	case yaml.ScalarNode:
		if val.Tag == "!!null" || val.Value == "" {
			return nil
		}
		return []string{val.Value}
	case yaml.SequenceNode:
		if len(val.Content) == 0 {
			return nil
		}
		out := make([]string, 0, len(val.Content))
		for _, item := range val.Content {
			if item.Kind == yaml.ScalarNode {
				out = append(out, item.Value)
			}
		}
		return out
	default:
		return nil
	}
}

// mappingValue and scalarField are minimal local copies of
// internal/marketplace/build/output.go's ymlMappingValue/ymlScalarString --
// this file's own doc comment explains why this package keeps its own
// narrow YAML-navigation instead of importing/extending shared schema types.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func scalarField(m *yaml.Node, key string) string {
	v := mappingValue(m, key)
	if v == nil || v.Kind != yaml.ScalarNode || v.Tag == "!!null" {
		return ""
	}
	return v.Value
}

// ToJSONValue builds the ordered JSONValue tree for m, in the fixed field
// order documented on PluginManifest -- omitting every field that is empty/
// nil/absent, matching Python's conditional dict-key assignment
// (deps/plugin_parser.py:963-990, core/plugin_manifest.py:372-378).
func (m *PluginManifest) ToJSONValue() JSONValue {
	var fields []JSONField
	fields = append(fields, JSONField{Key: "name", Val: StringValue(m.Name)})
	if m.Version != "" {
		fields = append(fields, JSONField{Key: "version", Val: StringValue(m.Version)})
	}
	if m.Description != "" {
		fields = append(fields, JSONField{Key: "description", Val: StringValue(m.Description)})
	}
	if m.Author != nil {
		fields = append(fields, JSONField{Key: "author", Val: authorValue(m.Author)})
	}
	if m.License != "" {
		fields = append(fields, JSONField{Key: "license", Val: StringValue(m.License)})
	}
	if m.Homepage != "" {
		fields = append(fields, JSONField{Key: "homepage", Val: StringValue(m.Homepage)})
	}
	if m.Repository != "" {
		fields = append(fields, JSONField{Key: "repository", Val: StringValue(m.Repository)})
	}
	if len(m.Keywords) > 0 {
		fields = append(fields, JSONField{Key: "keywords", Val: ArrayOfStrings(m.Keywords)})
	}
	if m.MCPServers != nil && !m.MCPServers.IsEmptyObject() {
		fields = append(fields, JSONField{Key: "mcpServers", Val: *m.MCPServers})
	}
	return ObjectValue(fields...)
}

func authorValue(a *Author) JSONValue {
	fields := []JSONField{{Key: "name", Val: StringValue(a.Name)}}
	if a.Email != "" {
		fields = append(fields, JSONField{Key: "email", Val: StringValue(a.Email)})
	}
	if a.URL != "" {
		fields = append(fields, JSONField{Key: "url", Val: StringValue(a.URL)})
	}
	return ObjectValue(fields...)
}
