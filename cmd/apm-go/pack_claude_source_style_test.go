package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/marketplace/build"
)

type claudeSourceStyleMetadataFetcher struct{}

func (claudeSourceStyleMetadataFetcher) FetchMetadata(string, string, string) (string, string, error) {
	return "", "", nil
}

func writeClaudeSourceStyleFixture(t *testing.T) string {
	t.Helper()
	dir := chdirTemp(t)
	originalFetcher := build.DefaultMetadataFetcher
	build.DefaultMetadataFetcher = claudeSourceStyleMetadataFetcher{}
	t.Cleanup(func() { build.DefaultMetadataFetcher = originalFetcher })

	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    - claude
  packages:
    - name: alice-tool
      source: alice/tool
      ref: 1111111111111111111111111111111111111111
    - name: bob-tool
      source: bob/tool
      ref: 2222222222222222222222222222222222222222
`)
	return dir
}

func readClaudeMarketplace(t *testing.T, dir string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "marketplace.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPackCmd_ClaudeSourceStyleURL_EndToEndAndCheckClean(t *testing.T) {
	dir := writeClaudeSourceStyleFixture(t)

	out, err := runPackCmd(t, "--claude-source-style", "url")
	if err != nil {
		t.Fatalf("pack --claude-source-style url returned error: %v (output: %s)", err, out)
	}
	data := readClaudeMarketplace(t, dir)
	for _, want := range []string{
		`"source": "url"`,
		`"url": "https://github.com/alice/tool"`,
		`"url": "https://github.com/bob/tool"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("marketplace.json missing %q:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), `"repo":`) {
		t.Errorf("url style must not emit GitHub shorthand repo keys:\n%s", data)
	}

	out, err = runPackCmd(t, "--claude-source-style", "url", "--dry-run", "--json", "-m", "claude")
	if err != nil {
		t.Fatalf("url style dry-run --json returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, `"unchanged": 2`) {
		t.Errorf("url style dry-run --json did not compare against the URL-shaped file:\n%s", out)
	}

	out, err = runPackCmd(t, "--claude-source-style", "url", "--check-clean", "--dry-run", "-m", "none")
	if err != nil {
		t.Fatalf("same-style --check-clean returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Marketplace working tree clean [outputs=claude]") {
		t.Errorf("same-style --check-clean output = %q, want clean result", out)
	}

	out, err = runPackCmd(t, "--check-clean", "--dry-run", "-m", "none")
	if err == nil {
		t.Fatalf("different-style --check-clean succeeded, want drift (output: %s)", out)
	}
	if exitCodeOf(err) != 4 {
		t.Errorf("different-style --check-clean exitCodeOf(err) = %d, want 4", exitCodeOf(err))
	}
	if !strings.Contains(out, "Marketplace working tree dirty [outputs=claude]") {
		t.Errorf("different-style --check-clean output = %q, want drift result", out)
	}
}

func TestPackCmd_ClaudeSourceStyleGithubIsDefaultByteOutput(t *testing.T) {
	dir := writeClaudeSourceStyleFixture(t)

	if out, err := runPackCmd(t); err != nil {
		t.Fatalf("default pack returned error: %v (output: %s)", err, out)
	}
	defaultData := append([]byte(nil), readClaudeMarketplace(t, dir)...)

	if err := os.Remove(filepath.Join(dir, ".claude-plugin", "marketplace.json")); err != nil {
		t.Fatal(err)
	}
	if out, err := runPackCmd(t, "--claude-source-style", "github"); err != nil {
		t.Fatalf("explicit github style returned error: %v (output: %s)", err, out)
	}
	if explicitData := readClaudeMarketplace(t, dir); !bytes.Equal(defaultData, explicitData) {
		t.Fatalf("default and explicit github style outputs differ:\ndefault:\n%s\nexplicit:\n%s", defaultData, explicitData)
	}
	if !bytes.Contains(defaultData, []byte(`"source": "github"`)) || !bytes.Contains(defaultData, []byte(`"repo": "alice/tool"`)) {
		t.Fatalf("default output did not retain GitHub shorthand:\n%s", defaultData)
	}
}

func TestPackCmd_ClaudeSourceStyleUnknownIsUsageError(t *testing.T) {
	chdirTemp(t)
	_, err := runPackCmd(t, "--claude-source-style", "ssh")
	if err == nil {
		t.Fatal("unknown Claude source style succeeded, want usage error")
	}
	if exitCodeOf(err) != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", exitCodeOf(err))
	}
	want := "Invalid value for '--claude-source-style': 'ssh' is not one of 'github', 'url'."
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}

func TestPackCmd_ClaudeSourceStyleHelpExplainsApmlessURLReason(t *testing.T) {
	out, err := runPackCmd(t, "--help")
	if err != nil {
		t.Fatalf("pack --help returned error: %v", err)
	}
	want := "Claude source style (apm-go-only): use url to install GitHub packages over HTTPS when SSH keys are unavailable."
	if !strings.Contains(out, want) {
		t.Errorf("help missing one-sentence apm-go-only explanation %q:\n%s", want, out)
	}
}
