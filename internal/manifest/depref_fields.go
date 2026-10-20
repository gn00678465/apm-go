package manifest

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v4"
)

// The fields each object form takes (models/dependency/object_fields.py:13-24,
// reference.py:833, registry_entry.py:84).
var (
	pathDepFields      = []string{"alias", "path", "skills", "targets"}
	gitDepFields       = []string{"alias", "allow_insecure", "git", "path", "ref", "skills", "targets", "type"}
	parentGitDepFields = []string{"alias", "git", "path", "ref"}
	registryDepFields  = []string{"alias", "id", "path", "registry", "skills", "targets", "version"}
)

func unknownFields(entry *yaml.Node, allowed []string) []string {
	var unknown []string
	for i := 0; i < len(entry.Content)-1; i += 2 {
		k := entry.Content[i].Value
		if !containsString(allowed, k) && !containsString(unknown, k) {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	return unknown
}

func reprJoin(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = PyRepr(n)
	}
	return strings.Join(quoted, ", ")
}

// rejectUnknownFields is reject_unknown_fields and reject_unknown_git_fields
// (object_fields.py:49-72): a field the form does not take is a typo the
// Oracle refuses rather than ignores.
func rejectUnknownFields(entry *yaml.Node, allowed []string, kind string) error {
	unknown := unknownFields(entry, allowed)
	if len(unknown) == 0 {
		return nil
	}
	if kind != "path" && containsString(unknown, "version") {
		return errors.New("Git dependency field 'version' is unsupported; use 'ref' for a branch, tag, or commit")
	}
	return fmt.Errorf("Unsupported field(s) for %s dependency: %s", kind, reprJoin(unknown))
}

// rejectUnknownRegistryFields is the check of registry_entry.py:83-90, which
// prints Python lists.
func rejectUnknownRegistryFields(entry *yaml.Node) error {
	unknown := unknownFields(entry, registryDepFields)
	if len(unknown) == 0 {
		return nil
	}
	return fmt.Errorf("Object-form registry entry has unknown fields: [%s]. Known fields: [%s]", reprJoin(unknown), reprJoin(registryDepFields))
}

var aliasPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// ParseAliasField is parse_alias_override (object_fields.py:26-46), for an
// apm.yml entry and a lock entry alike (deps/lockfile.py:286-296). A nil
// node, no key, and a null are no alias. The alias names the package's
// directory below apm_modules, so it is one path segment.
func ParseAliasField(node *yaml.Node) (string, error) {
	if node == nil {
		return "", nil
	}
	v := pyyaml11Value(node)
	if v.ShortTag() == "!!null" {
		return "", nil
	}
	if v.ShortTag() != "!!str" || strings.TrimSpace(v.Value) == "" {
		return "", errors.New("'alias' field must be a non-empty string")
	}
	alias := strings.TrimSpace(v.Value)
	if !aliasPattern.MatchString(alias) {
		return "", fmt.Errorf("Invalid alias: %s. Aliases can only contain letters, numbers, dots, underscores, and hyphens", alias)
	}
	if alias == "." || alias == ".." {
		return "", fmt.Errorf("Invalid dependency alias %s: '.' and '..' are reserved directory names. Set alias to a name such as 'my-skill.v2' in apm.yml and rerun apm-go install.", PyRepr(alias))
	}
	return alias, nil
}

// parseAllowInsecureField is reference.py:887-891: the field is a YAML
// boolean when it is there, a null included.
func parseAllowInsecureField(node *yaml.Node) (bool, error) {
	if node == nil {
		return false, nil
	}
	v := pyyaml11Value(node)
	if v.ShortTag() != "!!bool" {
		return false, errors.New("'allow_insecure' field must be a boolean")
	}
	return v.Value == "true", nil
}

func containsString(items []string, s string) bool {
	for _, item := range items {
		if item == s {
			return true
		}
	}
	return false
}
