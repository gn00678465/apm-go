package manifest

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
)

func parseYML(t *testing.T, src string) *Manifest {
	t.Helper()
	node, err := yamlcore.SafeLoad([]byte(src))
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}
	m, _, err := ParseManifest(node)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	return m
}

// R9: id: object form carries registry name + version selector.
func TestParseDepDict_RegistryIDForm(t *testing.T) {
	m := parseYML(t, `
name: proj
version: 0.1.0
registries:
  corp:
    url: https://registry.example.com/api/corp
  default: corp
dependencies:
  apm:
    - id: acme/sample
      version: 1.0.0
      registry: corp
`)
	if m.DefaultRegistry != "corp" {
		t.Errorf("DefaultRegistry = %q, want corp", m.DefaultRegistry)
	}
	if len(m.ParsedDeps) != 1 {
		t.Fatalf("want 1 dep, got %d", len(m.ParsedDeps))
	}
	d := m.ParsedDeps[0]
	if d.Source != "registry" || d.RepoURL != "acme/sample" || d.Reference != "1.0.0" || d.RegistryName != "corp" {
		t.Errorf("got %+v", d)
	}
}

// The registry object form takes no `ref:`; Oracle 0.33.0 refuses an unknown
// field with this text (models/dependency/registry_entry.py:83-90; measured
// with `foo: 1`, issue151-impl/reg1/extra-or/i1.out).
func TestParseDepDict_RegistryRefusesUnknownField(t *testing.T) {
	node, err := yamlcore.SafeLoad([]byte(`
name: proj
version: 0.1.0
registries:
  corp: {url: "https://r.example.com/corp"}
  default: corp
dependencies:
  apm:
    - id: acme/sample
      version: 2.0.0
      ref: 1.0.0
`))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = ParseManifest(node)
	want := "dependency entry 0: Object-form registry entry has unknown fields: ['ref']. Known fields: ['alias', 'id', 'path', 'registry', 'skills', 'targets', 'version']"
	if err == nil || err.Error() != want {
		t.Errorf("error %v, want %s", err, want)
	}
}

// sc-007/008: registry URLs with embedded credentials are rejected at parse time
// so they never leak into resolved_url or bypass the attach gate.
func TestParseRegistries_RejectsUserinfo(t *testing.T) {
	src := `
name: proj
version: 0.1.0
registries:
  corp:
    url: https://user:pass@reg.example.com/api/corp
`
	node, err := yamlcore.SafeLoad([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ParseManifest(node); err == nil || !strings.Contains(err.Error(), "embedded credentials") {
		t.Fatalf("want embedded-credentials rejection, got %v", err)
	}
}

func TestEffectiveRegistry(t *testing.T) {
	m := &Manifest{DefaultRegistry: "corp"}
	// explicit registry on dep
	if got, err := m.EffectiveRegistry(&DependencyReference{RepoURL: "a/b", RegistryName: "snap"}); err != nil || got != "snap" {
		t.Errorf("explicit: got %q err %v, want snap", got, err)
	}
	// fall back to default
	if got, err := m.EffectiveRegistry(&DependencyReference{RepoURL: "a/b"}); err != nil || got != "corp" {
		t.Errorf("default: got %q err %v, want corp", got, err)
	}
	// no default -> error
	m2 := &Manifest{}
	if _, err := m2.EffectiveRegistry(&DependencyReference{RepoURL: "a/b"}); err == nil {
		t.Errorf("want error when no registry and no default")
	}
}
