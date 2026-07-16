package manifest

import "testing"

// mustParseDepForIdentity is a small local helper (kept separate from the
// package's existing mustParse-style helpers, if any) so this test file has
// no ordering dependency on other test files.
func mustParseDepForIdentity(t *testing.T, s string) *DependencyReference {
	t.Helper()
	d, err := ParseDepString(s)
	if err != nil {
		t.Fatalf("ParseDepString(%q): %v", s, err)
	}
	return d
}

// TestCanonicalRepoIdentity_GitHubEquivalentForms proves that shorthand,
// host-qualified shorthand, HTTPS, SSH, and SCP forms of the exact same
// GitHub repository all collapse to the same identity -- the "same
// repository, different URL spelling" case design.md §0 exists to unify.
func TestCanonicalRepoIdentity_GitHubEquivalentForms(t *testing.T) {
	forms := []string{
		"owner/repo",
		"github.com/owner/repo",
		"https://github.com/owner/repo",
		"https://github.com/owner/repo.git",
		"ssh://git@github.com/owner/repo.git",
		"git@github.com:owner/repo.git",
	}

	var want string
	for i, s := range forms {
		d := mustParseDepForIdentity(t, s)
		got := CanonicalRepoIdentity(d)
		if i == 0 {
			want = got
			if want == "" {
				t.Fatalf("CanonicalRepoIdentity(%q) is empty, want non-empty", s)
			}
			continue
		}
		if got != want {
			t.Errorf("CanonicalRepoIdentity(%q) = %q, want %q (same repo as %q)", s, got, want, forms[0])
		}
	}
}

// TestCanonicalRepoIdentity_GitHubCaseFold proves owner/repo (and an
// explicit "github.com" host, in any case) fold to the same identity on
// GitHub -- GitHub itself treats owner/repo names case-insensitively.
func TestCanonicalRepoIdentity_GitHubCaseFold(t *testing.T) {
	lower := mustParseDepForIdentity(t, "owner/repo")
	upper := mustParseDepForIdentity(t, "Owner/Repo")
	mixedHost := mustParseDepForIdentity(t, "GitHub.COM/OWNER/REPO")

	idLower := CanonicalRepoIdentity(lower)
	idUpper := CanonicalRepoIdentity(upper)
	idMixed := CanonicalRepoIdentity(mixedHost)

	if idLower != idUpper {
		t.Errorf("GitHub owner/repo case-fold: CanonicalRepoIdentity(%q)=%q != CanonicalRepoIdentity(%q)=%q", "owner/repo", idLower, "Owner/Repo", idUpper)
	}
	if idLower != idMixed {
		t.Errorf("GitHub host+owner/repo case-fold: CanonicalRepoIdentity(%q)=%q != CanonicalRepoIdentity(%q)=%q", "owner/repo", idLower, "GitHub.COM/OWNER/REPO", idMixed)
	}
}

// TestCanonicalRepoIdentity_NonGitHubHostPreservesCase proves a self-hosted
// (non-GitHub) host does NOT case-fold Owner/Repo -- "Owner/Repo" and
// "owner/repo" on a self-hosted git server are two different identities,
// per design.md §0's "自架 host 保守不動" rule.
func TestCanonicalRepoIdentity_NonGitHubHostPreservesCase(t *testing.T) {
	lower := mustParseDepForIdentity(t, "gitlab.example.com/owner/repo")
	upper := mustParseDepForIdentity(t, "gitlab.example.com/Owner/Repo")

	idLower := CanonicalRepoIdentity(lower)
	idUpper := CanonicalRepoIdentity(upper)

	if idLower == idUpper {
		t.Errorf("non-GitHub host owner/repo case must NOT fold: got the same identity %q for both %q and %q", idLower, "owner/repo", "Owner/Repo")
	}

	// The host component itself is still case-insensitive (DNS is
	// case-insensitive), independent of the GitHub-only owner/repo fold.
	mixedHost := mustParseDepForIdentity(t, "GitLab.Example.COM/owner/repo")
	if CanonicalRepoIdentity(mixedHost) != idLower {
		t.Errorf("host case must still fold for non-GitHub hosts: %q != %q", CanonicalRepoIdentity(mixedHost), idLower)
	}
}

// TestCanonicalRepoIdentity_SelectorNotPartOfIdentity proves ref, virtual
// path, and alias -- the "resolution selector" per design.md §0 -- never
// affect repository identity, and are never case-folded by this function.
func TestCanonicalRepoIdentity_SelectorNotPartOfIdentity(t *testing.T) {
	base := mustParseDepForIdentity(t, "owner/repo")
	withRef := mustParseDepForIdentity(t, "owner/repo#v1.0.0")
	withOtherRef := mustParseDepForIdentity(t, "owner/repo#v2.0.0")
	withVP := mustParseDepForIdentity(t, "owner/repo/skills/foo")

	idBase := CanonicalRepoIdentity(base)
	if got := CanonicalRepoIdentity(withRef); got != idBase {
		t.Errorf("ref must not affect identity: got %q, want %q", got, idBase)
	}
	if got := CanonicalRepoIdentity(withOtherRef); got != idBase {
		t.Errorf("a different ref must not affect identity: got %q, want %q", got, idBase)
	}
	if got := CanonicalRepoIdentity(withVP); got != idBase {
		t.Errorf("virtual path must not affect identity: got %q, want %q", got, idBase)
	}

	// A mixed-case ref must round-trip untouched -- CanonicalRepoIdentity
	// must never mutate/lower-case the Reference field itself.
	mixedRef := mustParseDepForIdentity(t, "owner/repo#Feature-Branch")
	_ = CanonicalRepoIdentity(mixedRef)
	if mixedRef.Reference != "Feature-Branch" {
		t.Fatalf("Reference must stay case-exact, got %q", mixedRef.Reference)
	}

	a := &DependencyReference{Owner: "owner", Repo: "repo", Alias: "foo"}
	b := &DependencyReference{Owner: "owner", Repo: "repo", Alias: "bar"}
	if CanonicalRepoIdentity(a) != CanonicalRepoIdentity(b) {
		t.Errorf("alias must not affect identity")
	}
}

// TestCanonicalRepoIdentity_LocalGitPathPreservesPath proves a "git: ./path"
// local-repo-as-git-source dependency (Owner/Repo empty, RepoURL is the
// path) keeps its path as the identity, mirroring ToCanonical's existing
// special case, rather than collapsing every such dependency to the same
// empty-owner/empty-repo identity.
func TestCanonicalRepoIdentity_LocalGitPathPreservesPath(t *testing.T) {
	a := &DependencyReference{RepoURL: "./vendor/one", Source: "git"}
	b := &DependencyReference{RepoURL: "./vendor/two", Source: "git"}
	if CanonicalRepoIdentity(a) == CanonicalRepoIdentity(b) {
		t.Errorf("two different local git repo paths must not collapse to the same identity")
	}
	if got := CanonicalRepoIdentity(a); got != "./vendor/one" {
		t.Errorf("CanonicalRepoIdentity(local git path) = %q, want %q", got, "./vendor/one")
	}
}

// TestCanonicalRepoIdentity_NonGitSourcesGetOwnNamespace guards the codex
// gate finding on the first draft: a registry id or marketplace plugin whose
// spelling coincides with "owner/repo" must never share an identity with the
// real GitHub repository of that name (or with each other).
func TestCanonicalRepoIdentity_NonGitSourcesGetOwnNamespace(t *testing.T) {
	git := &DependencyReference{Owner: "owner", Repo: "repo", RepoURL: "owner/repo", Source: "git"}
	registry := &DependencyReference{RepoURL: "owner/repo", RegistryName: "main", Source: "registry"}
	marketplace := &DependencyReference{MarketplaceName: "mkt", MarketplacePluginName: "owner/repo", Source: "marketplace"}

	ids := map[string]string{
		"git":         CanonicalRepoIdentity(git),
		"registry":    CanonicalRepoIdentity(registry),
		"marketplace": CanonicalRepoIdentity(marketplace),
	}
	seen := map[string]string{}
	for kind, id := range ids {
		if id == "" {
			t.Errorf("%s identity is empty, want a stable non-empty identity", kind)
		}
		if prev, dup := seen[id]; dup {
			t.Errorf("%s and %s collide on identity %q", kind, prev, id)
		}
		seen[id] = kind
	}
}

// TestCanonicalRepoIdentity_PrefixedNamespaceIsUnambiguous guards the %q
// component quoting: a colon inside one component must not re-associate into
// a different (name, id) split that encodes identically (codex gate finding
// on the plain ":"-joined second draft).
func TestCanonicalRepoIdentity_PrefixedNamespaceIsUnambiguous(t *testing.T) {
	a := &DependencyReference{RegistryName: "a:b", RepoURL: "c", Source: "registry"}
	b := &DependencyReference{RegistryName: "a", RepoURL: "b:c", Source: "registry"}
	if CanonicalRepoIdentity(a) == CanonicalRepoIdentity(b) {
		t.Errorf("registry (a:b, c) and (a, b:c) collide on identity %q", CanonicalRepoIdentity(a))
	}
}

// TestCanonicalRepoIdentity_NameLiteralFoldsWithGitForm proves a bare name
// literal ("name: Owner/Repo", Source "") -- a default-host git shorthand --
// merges with the parsed owner/repo git form of the same repository.
func TestCanonicalRepoIdentity_NameLiteralFoldsWithGitForm(t *testing.T) {
	literal := &DependencyReference{RepoURL: "Owner/Repo"}
	parsed := &DependencyReference{Owner: "owner", Repo: "repo", RepoURL: "owner/repo", Source: "git"}
	if got, want := CanonicalRepoIdentity(literal), CanonicalRepoIdentity(parsed); got != want {
		t.Errorf("name-literal identity = %q, parsed git identity = %q; want equal", got, want)
	}
}

// TestCanonicalRepoIdentity_LocalAndParentHaveNoIdentity mirrors
// DependencyReference.IdentityKey's existing rule: local/parent references
// have no stable repository identity.
func TestCanonicalRepoIdentity_LocalAndParentHaveNoIdentity(t *testing.T) {
	local := &DependencyReference{IsLocal: true, LocalPath: "./foo"}
	if got := CanonicalRepoIdentity(local); got != "" {
		t.Errorf("local dep identity = %q, want empty", got)
	}
	parent := &DependencyReference{IsParent: true, VirtualPath: "some/path"}
	if got := CanonicalRepoIdentity(parent); got != "" {
		t.Errorf("parent dep identity = %q, want empty", got)
	}
}

// TestCanonicalRepoIdentity_NilIsEmpty guards the nil-safety of the shared
// helper -- callers across resolver/manifest/deploy must not need their own
// nil check before calling it.
func TestCanonicalRepoIdentity_NilIsEmpty(t *testing.T) {
	if got := CanonicalRepoIdentity(nil); got != "" {
		t.Errorf("nil dep identity = %q, want empty", got)
	}
}
