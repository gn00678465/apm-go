package pluginmanifest

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
)

func TestProduce_BothEcosystems_TwoFilesWritten(t *testing.T) {
	dir := t.TempDir()
	data, err := os.ReadFile(writeManifestFixture(t, dir, "name: demo\nversion: 1.0.0\n"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	written, err := Produce(&buf, dir, doc.Content[0], []string{"claude", "copilot"}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 2 {
		t.Fatalf("written = %v, want both ecosystems", written)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json")); statErr != nil {
		t.Errorf("expected claude plugin.json: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".github", "plugin", "plugin.json")); statErr != nil {
		t.Errorf("expected copilot plugin.json: %v", statErr)
	}
}

func TestProduce_OnlyClaudeTarget_WritesOnlyClaude(t *testing.T) {
	dir := t.TempDir()
	data := writeAndReadManifest(t, dir, "name: demo\nversion: 1.0.0\n")
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	written, err := Produce(&buf, dir, doc.Content[0], []string{"claude"}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 || written[0] != "claude" {
		t.Fatalf("written = %v, want [claude]", written)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".github", "plugin", "plugin.json")); !os.IsNotExist(statErr) {
		t.Errorf("copilot plugin.json must not be written (stat err = %v)", statErr)
	}
}

func TestProduce_ClaudeGetsSanitizedMCPServers_CopilotDoesNot(t *testing.T) {
	dir := t.TempDir()
	mcpContent := `{"mcpServers":{"demo":{"command":"npx","env":{"API_KEY":"sk-secret"}}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(mcpContent), 0o644); err != nil {
		t.Fatal(err)
	}
	data := writeAndReadManifest(t, dir, "name: demo\nversion: 1.0.0\n")
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if _, err := Produce(&buf, dir, doc.Content[0], []string{"claude", "copilot"}, false, false); err != nil {
		t.Fatal(err)
	}

	claudeData, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(claudeData), `"mcpServers"`) {
		t.Errorf("claude plugin.json = %s, want mcpServers present", claudeData)
	}
	if !strings.Contains(string(claudeData), `"demo"`) {
		t.Errorf("claude plugin.json = %s, want server name preserved", claudeData)
	}
	if strings.Contains(string(claudeData), "sk-secret") || strings.Contains(string(claudeData), `"env"`) {
		t.Errorf("claude plugin.json = %s, want env/secret stripped", claudeData)
	}

	copilotData, err := os.ReadFile(filepath.Join(dir, ".github", "plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(copilotData), "mcpServers") {
		t.Errorf("copilot plugin.json = %s, must never include mcpServers", copilotData)
	}
}

func TestProduce_JSONLayout_IndentAndTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	data := writeAndReadManifest(t, dir, "name: demo\nversion: 1.0.0\ndescription: A demo\n")
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := Produce(&buf, dir, doc.Content[0], []string{"claude"}, false, false); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.HasPrefix(text, "{\n  \"name\": \"demo\",\n  \"version\": \"1.0.0\",\n  \"description\": \"A demo\"") {
		t.Errorf("output = %s, want 2-space indent with name/version/description in that order", text)
	}
	if !strings.HasSuffix(text, "}\n") {
		t.Errorf("output = %s, want trailing newline after the closing brace", text)
	}
}

func TestProduce_MissingName_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	data := writeAndReadManifest(t, dir, "version: 1.0.0\n")
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := Produce(&buf, dir, doc.Content[0], []string{"claude"}, false, false); err == nil {
		t.Fatal("expected an error for a manifest with no name")
	}
}

func writeManifestFixture(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "apm.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeAndReadManifest(t *testing.T, dir, content string) []byte {
	t.Helper()
	path := writeManifestFixture(t, dir, content)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
