package manifest

import (
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

// buildGitDepNodeWithSkills builds a {git: <gitVal>, skills: <skills>}
// dependency dict entry node. A nil skills node omits the key entirely
// (absent-key case).
func buildGitDepNodeWithSkills(gitVal string, skills *yaml.Node) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	n.Content = append(n.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "git"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: gitVal},
	)
	if skills != nil {
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "skills"},
			skills,
		)
	}
	return n
}

// buildDictNodeWithSkills builds an arbitrary dependency dict entry (from
// kv, string-scalar values only) plus an always-present `skills: [x]` key,
// used to prove non-git dict branches reject the key outright.
func buildDictNodeWithSkills(kv map[string]string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for k, v := range kv {
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v},
		)
	}
	n.Content = append(n.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "skills"},
		strSeqNode("x"),
	)
	return n
}

func strSeqNode(items ...string) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, it := range items {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: it})
	}
	return seq
}

func TestParseDepDict_Skills_ValidTrimsDedupesSorts(t *testing.T) {
	entry := buildGitDepNodeWithSkills("owner/repo", strSeqNode("b", "a", "a", " c "))
	d, err := ParseDepDict(entry, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(d.SkillSubset, want) {
		t.Errorf("SkillSubset = %#v, want %#v", d.SkillSubset, want)
	}
}

func TestParseDepDict_Skills_ExplicitNullMeansNoSubset(t *testing.T) {
	entry := buildGitDepNodeWithSkills("owner/repo", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"})
	d, err := ParseDepDict(entry, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.SkillSubset != nil {
		t.Errorf("SkillSubset = %#v, want nil", d.SkillSubset)
	}
}

func TestParseDepDict_Skills_AbsentKeyMeansNoSubset(t *testing.T) {
	entry := buildGitDepNodeWithSkills("owner/repo", nil)
	d, err := ParseDepDict(entry, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.SkillSubset != nil {
		t.Errorf("SkillSubset = %#v, want nil", d.SkillSubset)
	}
}

func TestParseDepDict_Skills_NonSequenceScalarRejected(t *testing.T) {
	entry := buildGitDepNodeWithSkills("owner/repo", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "foo"})
	_, err := ParseDepDict(entry, 0)
	if err == nil || !strings.Contains(err.Error(), "must be a list of skill names") {
		t.Fatalf("expected 'must be a list of skill names' error, got: %v", err)
	}
}

func TestParseDepDict_Skills_NonSequenceMappingRejected(t *testing.T) {
	entry := buildGitDepNodeWithSkills("owner/repo", &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
	_, err := ParseDepDict(entry, 0)
	if err == nil || !strings.Contains(err.Error(), "must be a list of skill names") {
		t.Fatalf("expected 'must be a list of skill names' error, got: %v", err)
	}
}

func TestParseDepDict_Skills_EmptySequenceRejected(t *testing.T) {
	entry := buildGitDepNodeWithSkills("owner/repo", &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"})
	_, err := ParseDepDict(entry, 0)
	if err == nil || !strings.Contains(err.Error(), "must contain at least one name") {
		t.Fatalf("expected 'must contain at least one name' error, got: %v", err)
	}
}

func TestParseDepDict_Skills_NonStringScalarItemRejected(t *testing.T) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!int", Value: "42"},
	}}
	entry := buildGitDepNodeWithSkills("owner/repo", seq)
	_, err := ParseDepDict(entry, 0)
	if err == nil || !strings.Contains(err.Error(), "non-empty string") {
		t.Fatalf("expected 'non-empty string' error, got: %v", err)
	}
}

func TestParseDepDict_Skills_BlankStringItemRejected(t *testing.T) {
	entry := buildGitDepNodeWithSkills("owner/repo", strSeqNode("   "))
	_, err := ParseDepDict(entry, 0)
	if err == nil || !strings.Contains(err.Error(), "non-empty string") {
		t.Fatalf("expected 'non-empty string' error, got: %v", err)
	}
}

func TestParseDepDict_Skills_PathTraversalNamesRejected(t *testing.T) {
	for _, bad := range []string{".", "..", "a/b", `a\b`, "C:", "a:b"} {
		t.Run(bad, func(t *testing.T) {
			entry := buildGitDepNodeWithSkills("owner/repo", strSeqNode(bad))
			_, err := ParseDepDict(entry, 0)
			if err == nil {
				t.Fatalf("skill name %q: expected error, got nil", bad)
			}
		})
	}
}

func TestParseDepDict_Skills_RejectedOnNonGitBranches(t *testing.T) {
	cases := map[string]*yaml.Node{
		"registry (id)":  buildDictNodeWithSkills(map[string]string{"id": "some/pkg"}),
		"local (path)":   buildDictNodeWithSkills(map[string]string{"path": "./local"}),
		"name (literal)": buildDictNodeWithSkills(map[string]string{"name": "literal"}),
		"git: parent":    buildDictNodeWithSkills(map[string]string{"git": "parent", "path": "sub/path"}),
		// marketplace rejects skills via its pre-existing unknown-key
		// switch (allowed: name, marketplace, version) -- this case proves
		// the coverage rather than adding a new rejection (codex gate).
		"marketplace": buildDictNodeWithSkills(map[string]string{"name": "plug", "marketplace": "mkt"}),
	}
	for label, entry := range cases {
		t.Run(label, func(t *testing.T) {
			_, err := ParseDepDict(entry, 0)
			if err == nil {
				t.Fatalf("%s: expected error rejecting 'skills' key, got nil", label)
			}
		})
	}
}
