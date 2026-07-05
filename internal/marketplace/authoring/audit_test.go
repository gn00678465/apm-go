package authoring

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/marketplace"
	"github.com/apm-go/apm/internal/yamlcore"
)

// ── classify_dependency (mkt-043 修訂版) ─────────────────────────────────

func TestClassifyDepString(t *testing.T) {
	tests := []struct {
		name string
		dep  string
		want DepClassification
	}{
		{"marketplace ref", "pkg@mkt", DepMarketplace},
		{"marketplace ref with tag", "pkg@mkt#v1.0.0", DepMarketplace},
		{"marketplace ref with slash in fragment ref", "pkg@mkt#feature/branch", DepMarketplace},
		{"local relative", "./local-pkg", DepLocal},
		{"local parent-relative", "../local-pkg", DepLocal},
		{"local absolute unix", "/abs/path", DepLocal},
		{"local home-relative", "~/pkg", DepLocal},
		{"local windows drive", `C:\pkg`, DepLocal},
		{"local windows backslash relative", `.\pkg`, DepLocal},
		{"protocol-relative is not local", "//evil.com/pkg", DepBypass},
		{"bare shorthand bypasses", "owner/repo", DepBypass},
		{"shorthand with ref bypasses", "owner/repo#v1.0.0", DepBypass},
		{"full https url bypasses", "https://github.com/owner/repo", DepBypass},
		{"scp remote bypasses", "git@github.com:owner/repo.git", DepBypass},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			got := classifyDepString(tt.dep)

			// Assert
			if got != tt.want {
				t.Errorf("classifyDepString(%q) = %v, want %v", tt.dep, got, tt.want)
			}
		})
	}
}

// TestClassifyDepEntry_DictForms covers the three dict-shaped entries
// classify_dependency's flattening step recognizes (mkt-043's "dict {name,
// marketplace} 或 {git:} 物件"): a marketplace-pinned dict is never a bypass
// issue, a {path: ...} dict is local, a {git: ...} dict always bypasses
// (design.md/checklist's simplified reading -- see this file's doc comment
// on classifyDepEntry for the deliberate deviation from a value-shape
// re-check), and anything else (e.g. a registry {id: ...} dict) is not
// classifiable at all and must be skipped, not miscounted as a bypass.
func TestClassifyDepEntry_DictForms(t *testing.T) {
	tests := []struct {
		name    string
		yamlDoc string
		wantOK  bool
		wantCls DepClassification
	}{
		{"marketplace dict", "name: good\nmarketplace: acme\n", true, DepMarketplace},
		{"path dict", "path: ./local-thing\n", true, DepLocal},
		{"git dict", "git: https://example.com/owner/repo.git\n", true, DepBypass},
		{"unrecognized dict is skipped", "id: some-registry-id\n", false, 0},
		{"git dict with blank value is skipped", "git: \"\"\n", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			node := mustParseSingleNode(t, tt.yamlDoc)

			// Act
			cls, _, ok := classifyDepEntry(node)

			// Assert
			if ok != tt.wantOK {
				t.Fatalf("classifyDepEntry() ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && cls != tt.wantCls {
				t.Errorf("classifyDepEntry() classification = %v, want %v", cls, tt.wantCls)
			}
		})
	}
}

func TestClassifyDepEntry_ScalarForms(t *testing.T) {
	// Arrange
	node := mustParseSingleNode(t, "pkg@mkt#v1.0.0\n")

	// Act
	cls, dep, ok := classifyDepEntry(node)

	// Assert
	if !ok || cls != DepMarketplace || dep != "pkg@mkt#v1.0.0" {
		t.Errorf("classifyDepEntry() = (%v, %q, %v), want (DepMarketplace, \"pkg@mkt#v1.0.0\", true)", cls, dep, ok)
	}
}

func TestClassifyDepEntry_BlankScalarIsSkipped(t *testing.T) {
	// Arrange
	node := mustParseSingleNode(t, "\"   \"\n")

	// Act
	_, _, ok := classifyDepEntry(node)

	// Assert
	if ok {
		t.Error("classifyDepEntry() ok = true for a blank scalar, want false (skipped)")
	}
}

// TestClassifyDepEntry_SequenceEntryIsSkipped covers a malformed
// dependencies.apm[] element that is neither a scalar nor a mapping (e.g. a
// nested list) -- must be skipped, not panic.
func TestClassifyDepEntry_SequenceEntryIsSkipped(t *testing.T) {
	// Arrange
	node := mustParseSingleNode(t, "[a, b]\n")

	// Act
	_, _, ok := classifyDepEntry(node)

	// Assert
	if ok {
		t.Error("classifyDepEntry() ok = true for a sequence entry, want false (skipped)")
	}
}

// ── collectApmDepEntries: scans both dependencies AND devDependencies ────

func TestCollectApmDepEntries_BothSections(t *testing.T) {
	// Arrange
	doc := `name: plugin-a
version: 1.0.0
dependencies:
  apm:
    - name: good
      marketplace: acme
    - owner/repo#v1.0.0
devDependencies:
  apm:
    - git: https://example.com/evil/repo.git
    - path: ./dev-local
`
	root := mustParseRootMapping(t, doc)

	// Act
	entries := collectApmDepEntries(root)

	// Assert
	if len(entries) != 4 {
		t.Fatalf("len(entries) = %d, want 4 (2 from dependencies + 2 from devDependencies)", len(entries))
	}
}

func TestCollectApmDepEntries_MissingSectionsYieldsNil(t *testing.T) {
	// Arrange
	root := mustParseRootMapping(t, "name: plugin-a\nversion: 1.0.0\n")

	// Act
	entries := collectApmDepEntries(root)

	// Assert
	if len(entries) != 0 {
		t.Errorf("len(entries) = %d, want 0", len(entries))
	}
}

// TestCollectApmDepEntries_WrongShapesAreSkipped covers the tolerant half of
// mkt-043's scan: a dependencies: block that is not a mapping, or an
// apm: key that is not a list, must be skipped rather than panicking or
// erroring -- this file's own fetch pipeline must never let one plugin's
// oddly-shaped (but still valid YAML) apm.yml abort the whole audit.
func TestCollectApmDepEntries_WrongShapesAreSkipped(t *testing.T) {
	// Arrange
	root := mustParseRootMapping(t, "name: plugin-a\nversion: 1.0.0\n"+
		"dependencies: not-a-mapping\n"+
		"devDependencies:\n  apm: not-a-list\n")

	// Act
	entries := collectApmDepEntries(root)

	// Assert
	if len(entries) != 0 {
		t.Errorf("len(entries) = %d, want 0", len(entries))
	}
}

// ── suggestReplacement: mkt-043 修訂版's dict-form-only suggestion text ───

func TestSuggestReplacement_NeverUsesStringShorthandForm(t *testing.T) {
	tests := []struct {
		name string
		dep  string
	}{
		{"plain shorthand", "owner/repo"},
		{"shorthand with ref", "owner/repo#v2.0.0"},
		{"full https url with .git suffix", "https://github.com/owner/repo.git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			got := suggestReplacement(tt.dep, "acme-marketplace")

			// Assert: must point at the dict form, never the "pkg@mkt" string
			// shorthand the dependency parser does not accept (mkt-033).
			if !strings.Contains(got, "{name:") || !strings.Contains(got, "marketplace: acme-marketplace") {
				t.Errorf("suggestReplacement(%q) = %q, want it to reference the dict form {name: ..., marketplace: acme-marketplace}", tt.dep, got)
			}
			if strings.Contains(got, "repo@acme-marketplace") {
				t.Errorf("suggestReplacement(%q) = %q, want it to NOT contain the string-shorthand form 'repo@acme-marketplace'", tt.dep, got)
			}
		})
	}
}

func TestSuggestReplacement_PackageHintExtraction(t *testing.T) {
	tests := []struct {
		name    string
		dep     string
		wantHas string
	}{
		{"strips ref fragment and .git suffix", "https://github.com/owner/cool-tool.git#v1", "name: cool-tool"},
		{"bare name with no path segments", "just-a-name", "name: just-a-name"},
		{"trailing slash falls back to the generic 'package' hint", "https://github.com/owner/", "name: package"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := suggestReplacement(tt.dep, "mkt")
			if !strings.Contains(got, tt.wantHas) {
				t.Errorf("suggestReplacement(%q) = %q, want it to contain %q", tt.dep, got, tt.wantHas)
			}
		})
	}
}

// ── resolvePluginGithubCoords: audit's deliberately narrow fetch reach ───

func TestResolvePluginGithubCoords(t *testing.T) {
	tests := []struct {
		name         string
		source       any
		fallbackHost string
		wantOK       bool
		wantHost     string
		wantOwner    string
		wantRepo     string
		wantRef      string
		wantPath     string
	}{
		{
			name:         "full github dict",
			source:       map[string]any{"type": "github", "repo": "acme/tool", "ref": "v1.0.0", "host": "github.example.com", "path": "sub/dir"},
			fallbackHost: "github.com",
			wantOK:       true, wantHost: "github.example.com", wantOwner: "acme", wantRepo: "tool", wantRef: "v1.0.0", wantPath: "sub/dir/apm.yml",
		},
		{
			name:         "unpinned ref falls back to HEAD",
			source:       map[string]any{"type": "github", "repo": "acme/tool"},
			fallbackHost: "github.com",
			wantOK:       true, wantHost: "github.com", wantOwner: "acme", wantRepo: "tool", wantRef: "HEAD", wantPath: "apm.yml",
		},
		{
			name:         "missing host falls back to marketplace host then github.com",
			source:       map[string]any{"type": "github", "repo": "acme/tool"},
			fallbackHost: "",
			wantOK:       true, wantHost: "github.com", wantOwner: "acme", wantRepo: "tool", wantRef: "HEAD", wantPath: "apm.yml",
		},
		{
			name:         "string source is unsupported",
			source:       "./relative/plugin",
			fallbackHost: "github.com",
			wantOK:       false,
		},
		{
			name:         "non-github dict type is unsupported",
			source:       map[string]any{"type": "git-subdir", "repo": "acme/tool"},
			fallbackHost: "github.com",
			wantOK:       false,
		},
		{
			name:         "repo missing a slash is unsupported",
			source:       map[string]any{"type": "github", "repo": "not-owner-slash-repo"},
			fallbackHost: "github.com",
			wantOK:       false,
		},
		{
			name:         "repo with more than one slash is unsupported",
			source:       map[string]any{"type": "github", "repo": "owner/repo/extra"},
			fallbackHost: "github.com",
			wantOK:       false,
		},
		{
			name:         "path traversal in path field is rejected",
			source:       map[string]any{"type": "github", "repo": "acme/tool", "path": "../../etc"},
			fallbackHost: "github.com",
			wantOK:       false,
		},
		{
			name:         "nil source is unsupported",
			source:       nil,
			fallbackHost: "github.com",
			wantOK:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			host, owner, repo, ref, path, ok := resolvePluginGithubCoords(tt.source, tt.fallbackHost)

			// Assert
			if ok != tt.wantOK {
				t.Fatalf("resolvePluginGithubCoords() ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if host != tt.wantHost || owner != tt.wantOwner || repo != tt.wantRepo || ref != tt.wantRef || path != tt.wantPath {
				t.Errorf("resolvePluginGithubCoords() = (%q, %q, %q, %q, %q), want (%q, %q, %q, %q, %q)",
					host, owner, repo, ref, path, tt.wantHost, tt.wantOwner, tt.wantRepo, tt.wantRef, tt.wantPath)
			}
		})
	}
}

// ── auditPlugin / RunAudit: fetch-status classification + issue detection ─

// fakeApmYMLFetcher is a test seam matching ApmYMLFetcher, keyed by "owner/repo/path@ref".
type fakeApmYMLFetcher struct {
	responses map[string][]byte
	errs      map[string]error
	calls     []string
}

func (f *fakeApmYMLFetcher) key(host, owner, repo, path, ref string) string {
	return host + ":" + owner + "/" + repo + "/" + path + "@" + ref
}

func (f *fakeApmYMLFetcher) FetchRaw(host, owner, repo, path, ref string) ([]byte, error) {
	k := f.key(host, owner, repo, path, ref)
	f.calls = append(f.calls, k)
	if err, ok := f.errs[k]; ok {
		return nil, err
	}
	if data, ok := f.responses[k]; ok {
		return data, nil
	}
	return nil, ErrApmYMLNotFound
}

// panicApmYMLFetcher proves a code path takes zero network action, mirroring
// refcheck_test.go's panicLister convention.
type panicApmYMLFetcher struct{}

func (panicApmYMLFetcher) FetchRaw(host, owner, repo, path, ref string) ([]byte, error) {
	panic("FetchRaw must not be called: plugin source is not addressable")
}

func githubPlugin(name, repo, ref string) marketplace.MarketplacePlugin {
	src := map[string]any{"type": "github", "repo": repo}
	if ref != "" {
		src["ref"] = ref
	}
	return marketplace.MarketplacePlugin{Name: name, Source: src}
}

func TestAuditPlugin_UnsupportedSource_NeverFetches(t *testing.T) {
	// Arrange
	plugin := marketplace.MarketplacePlugin{Name: "p", Source: "./relative"}

	// Act
	report := auditPlugin(plugin, "acme", "github.com", panicApmYMLFetcher{})

	// Assert
	if report.FetchStatus != FetchUnsupportedSource {
		t.Errorf("FetchStatus = %v, want FetchUnsupportedSource", report.FetchStatus)
	}
}

func TestAuditPlugin_NoManifest(t *testing.T) {
	// Arrange
	plugin := githubPlugin("p", "acme/tool", "")
	fetcher := &fakeApmYMLFetcher{}

	// Act
	report := auditPlugin(plugin, "acme", "github.com", fetcher)

	// Assert
	if report.FetchStatus != FetchNoManifest {
		t.Errorf("FetchStatus = %v, want FetchNoManifest", report.FetchStatus)
	}
}

func TestAuditPlugin_NetworkError(t *testing.T) {
	// Arrange
	plugin := githubPlugin("p", "acme/tool", "v1")
	fetcher := &fakeApmYMLFetcher{errs: map[string]error{
		"github.com:acme/tool/apm.yml@v1": errors.New("could not reach GitHub (network error)"),
	}}

	// Act
	report := auditPlugin(plugin, "acme", "github.com", fetcher)

	// Assert
	if report.FetchStatus != FetchNetworkError {
		t.Errorf("FetchStatus = %v, want FetchNetworkError", report.FetchStatus)
	}
	if report.Detail == "" {
		t.Error("Detail is empty, want the underlying fetch error message")
	}
}

func TestAuditPlugin_ParseError_MalformedYAML(t *testing.T) {
	// Arrange
	plugin := githubPlugin("p", "acme/tool", "v1")
	fetcher := &fakeApmYMLFetcher{responses: map[string][]byte{
		"github.com:acme/tool/apm.yml@v1": []byte("{not: valid: yaml:::"),
	}}

	// Act
	report := auditPlugin(plugin, "acme", "github.com", fetcher)

	// Assert
	if report.FetchStatus != FetchParseError {
		t.Errorf("FetchStatus = %v, want FetchParseError", report.FetchStatus)
	}
}

func TestAuditPlugin_ParseError_RootNotAMapping(t *testing.T) {
	// Arrange
	plugin := githubPlugin("p", "acme/tool", "v1")
	fetcher := &fakeApmYMLFetcher{responses: map[string][]byte{
		"github.com:acme/tool/apm.yml@v1": []byte("- just\n- a\n- list\n"),
	}}

	// Act
	report := auditPlugin(plugin, "acme", "github.com", fetcher)

	// Assert
	if report.FetchStatus != FetchParseError {
		t.Errorf("FetchStatus = %v, want FetchParseError", report.FetchStatus)
	}
}

func TestAuditPlugin_OK_CleanDeps_NoIssues(t *testing.T) {
	// Arrange
	plugin := githubPlugin("p", "acme/tool", "v1")
	apmYML := "name: tool\nversion: 1.0.0\n" +
		"dependencies:\n  apm:\n    - name: good\n      marketplace: acme\n    - path: ./local\n"
	fetcher := &fakeApmYMLFetcher{responses: map[string][]byte{
		"github.com:acme/tool/apm.yml@v1": []byte(apmYML),
	}}

	// Act
	report := auditPlugin(plugin, "acme", "github.com", fetcher)

	// Assert
	if report.FetchStatus != FetchOK {
		t.Fatalf("FetchStatus = %v, want FetchOK", report.FetchStatus)
	}
	if len(report.Issues) != 0 {
		t.Errorf("Issues = %+v, want none", report.Issues)
	}
}

// TestAuditPlugin_OK_BypassInDependenciesAndDevDependencies covers mkt-043's
// "掃 dependencies 與 devDependencies": a bypass in either section must be
// flagged, and the suggestion must point at the dict form.
func TestAuditPlugin_OK_BypassInDependenciesAndDevDependencies(t *testing.T) {
	// Arrange
	plugin := githubPlugin("p", "acme/tool", "v1")
	apmYML := "name: tool\nversion: 1.0.0\n" +
		"dependencies:\n  apm:\n    - owner/repo#v1.0.0\n" +
		"devDependencies:\n  apm:\n    - git: https://example.com/evil/repo.git\n"
	fetcher := &fakeApmYMLFetcher{responses: map[string][]byte{
		"github.com:acme/tool/apm.yml@v1": []byte(apmYML),
	}}

	// Act
	report := auditPlugin(plugin, "acme-marketplace", "github.com", fetcher)

	// Assert
	if report.FetchStatus != FetchOK {
		t.Fatalf("FetchStatus = %v, want FetchOK", report.FetchStatus)
	}
	if len(report.Issues) != 2 {
		t.Fatalf("len(Issues) = %d, want 2 (one from dependencies, one from devDependencies)", len(report.Issues))
	}
	for _, issue := range report.Issues {
		if issue.Classification != DepBypass {
			t.Errorf("issue %+v classification = %v, want DepBypass", issue, issue.Classification)
		}
		if strings.Contains(issue.Suggestion, "@acme-marketplace") {
			t.Errorf("issue %+v suggestion = %q, want it to not use the 'X@Y' string form", issue, issue.Suggestion)
		}
	}
}

func TestRunAudit_IsolatesPerPluginFailures(t *testing.T) {
	// Arrange: one plugin fetches cleanly, one has a bypass, one is
	// unsupported, one hits a network error -- none of these should abort
	// the others (mirrors Python's run_audit try/except-per-plugin isolation).
	m := &marketplace.MarketplaceManifest{
		Plugins: []marketplace.MarketplacePlugin{
			githubPlugin("clean", "acme/clean", "v1"),
			githubPlugin("bypass", "acme/bypass", "v1"),
			{Name: "unsupported", Source: "./relative"},
			githubPlugin("network-error", "acme/broken", "v1"),
		},
	}
	fetcher := &fakeApmYMLFetcher{
		responses: map[string][]byte{
			"github.com:acme/clean/apm.yml@v1":  []byte("name: c\nversion: 1.0.0\n"),
			"github.com:acme/bypass/apm.yml@v1": []byte("name: b\nversion: 1.0.0\ndependencies:\n  apm:\n    - owner/repo\n"),
		},
		errs: map[string]error{
			"github.com:acme/broken/apm.yml@v1": errors.New("boom"),
		},
	}

	// Act
	reports := RunAudit(m, "acme-marketplace", "github.com", fetcher)

	// Assert
	if len(reports) != 4 {
		t.Fatalf("len(reports) = %d, want 4", len(reports))
	}
	byName := make(map[string]PluginAuditReport, len(reports))
	for _, r := range reports {
		byName[r.PluginName] = r
	}
	if byName["clean"].FetchStatus != FetchOK || len(byName["clean"].Issues) != 0 {
		t.Errorf("clean report = %+v", byName["clean"])
	}
	if byName["bypass"].FetchStatus != FetchOK || len(byName["bypass"].Issues) != 1 {
		t.Errorf("bypass report = %+v", byName["bypass"])
	}
	if byName["unsupported"].FetchStatus != FetchUnsupportedSource {
		t.Errorf("unsupported report = %+v", byName["unsupported"])
	}
	if byName["network-error"].FetchStatus != FetchNetworkError {
		t.Errorf("network-error report = %+v", byName["network-error"])
	}
}

// ── githubRawFetcher: the production ApmYMLFetcher ───────────────────────

func withAuditGitHubAPIBase(t *testing.T, base string) {
	t.Helper()
	orig := auditGithubAPIBaseFor
	auditGithubAPIBaseFor = func(string) string { return base }
	t.Cleanup(func() { auditGithubAPIBaseFor = orig })
}

func TestGithubRawFetcher_HappyPath(t *testing.T) {
	// Arrange
	body := []byte("name: tool\nversion: 1.0.0\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/tool/contents/sub/apm.yml" || r.URL.Query().Get("ref") != "v1.0.0" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Accept") != "application/vnd.github.v3.raw" {
			http.Error(w, "wrong accept header", http.StatusBadRequest)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	withAuditGitHubAPIBase(t, srv.URL)

	// Act
	data, err := (githubRawFetcher{}).FetchRaw("github.com", "acme", "tool", "sub/apm.yml", "v1.0.0")

	// Assert
	if err != nil {
		t.Fatalf("FetchRaw() returned error: %v", err)
	}
	if string(data) != string(body) {
		t.Errorf("FetchRaw() = %q, want %q", data, body)
	}
}

func TestGithubRawFetcher_NonOKStatus(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	withAuditGitHubAPIBase(t, srv.URL)

	// Act
	_, err := (githubRawFetcher{}).FetchRaw("github.com", "acme", "tool", "apm.yml", "HEAD")

	// Assert
	if err == nil {
		t.Fatal("FetchRaw() returned no error for a 403 response")
	}
	if errors.Is(err, ErrApmYMLNotFound) {
		t.Error("FetchRaw() classified a 403 as ErrApmYMLNotFound, want a distinct error")
	}
}

func TestGithubRawFetcher_NotFound(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	withAuditGitHubAPIBase(t, srv.URL)

	// Act
	_, err := (githubRawFetcher{}).FetchRaw("github.com", "acme", "tool", "apm.yml", "HEAD")

	// Assert
	if !errors.Is(err, ErrApmYMLNotFound) {
		t.Errorf("FetchRaw() error = %v, want ErrApmYMLNotFound", err)
	}
}

func TestGithubRawFetcher_ForwardsPATForTrustedHost(t *testing.T) {
	// Arrange
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte("name: t\nversion: 1.0.0\n"))
	}))
	t.Cleanup(srv.Close)
	withAuditGitHubAPIBase(t, srv.URL)
	t.Setenv(auditGithubPATEnvVar, "t-secret-pat")

	// Act
	_, err := (githubRawFetcher{}).FetchRaw("github.com", "acme", "tool", "apm.yml", "HEAD")

	// Assert
	if err != nil {
		t.Fatalf("FetchRaw() returned error: %v", err)
	}
	if gotAuth != "token t-secret-pat" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "token t-secret-pat")
	}
}

// TestGithubRawFetcher_UntrustedHost_NeverReachesNetwork covers the
// credsec-motivated host gate: an attacker-controlled plugin dict source
// naming a non-GitHub-family host must fail before any request is sent (so
// GITHUB_APM_PAT is never at risk of forwarding to it), which auditPlugin
// then surfaces as FetchNetworkError -- an "unverifiable" outcome that DOES
// trip --strict, rather than being silently skipped.
func TestGithubRawFetcher_UntrustedHost_NeverReachesNetwork(t *testing.T) {
	// Arrange
	requestSeen := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestSeen = true
		w.Write([]byte("name: t\nversion: 1.0.0\n"))
	}))
	t.Cleanup(srv.Close)
	withAuditGitHubAPIBase(t, srv.URL)
	t.Setenv(auditGithubPATEnvVar, "t-should-not-leak")

	// Act
	_, err := (githubRawFetcher{}).FetchRaw("evil.example.com", "acme", "tool", "apm.yml", "HEAD")

	// Assert
	if err == nil {
		t.Fatal("FetchRaw() returned no error for an untrusted host")
	}
	if requestSeen {
		t.Error("FetchRaw() reached the test server for an untrusted host, want zero network access")
	}
	if strings.Contains(err.Error(), "t-should-not-leak") {
		t.Errorf("FetchRaw() error leaked the PAT: %v", err)
	}
}

func TestIsGithubFamilyAuditHost(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		githubHost string
		want       bool
	}{
		{"github.com", "github.com", "", true},
		{"case-insensitive", "GitHub.Com", "", true},
		{"subdomain", "api.github.com", "", true},
		{"ghe cloud", "acme.ghe.com", "", true},
		{"unrelated host", "example.com", "", false},
		{"ghes via env", "ghe.example.com", "ghe.example.com", true},
		{"ghes env unset never trusts", "ghe.example.com", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.githubHost != "" {
				t.Setenv(auditGithubHostEnvVar, tt.githubHost)
			}
			if got := isGithubFamilyAuditHost(tt.host); got != tt.want {
				t.Errorf("isGithubFamilyAuditHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

// ── test helpers ──────────────────────────────────────────────────────────

// mustParseSingleNode parses doc (a one-line YAML fragment, e.g. one
// dependencies.apm[] entry) and returns its single top-level content node --
// exactly the *yaml.Node shape classifyDepEntry receives for one sequence
// element.
func mustParseSingleNode(t *testing.T, doc string) *yaml.Node {
	t.Helper()
	root, err := yamlcore.SafeLoad([]byte(doc))
	if err != nil {
		t.Fatalf("parse %q: %v", doc, err)
	}
	if len(root.Content) == 0 {
		t.Fatalf("parse %q: empty document", doc)
	}
	return root.Content[0]
}

// mustParseRootMapping parses doc and returns its top-level mapping node --
// the shape collectApmDepEntries receives (a whole plugin apm.yml's root).
func mustParseRootMapping(t *testing.T, doc string) *yaml.Node {
	t.Helper()
	return mustParseSingleNode(t, doc)
}
