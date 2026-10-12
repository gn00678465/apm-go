package deploy

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

func writeClaudeMCPFixture(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, ".mcp.json")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The expected file is what the Oracle v0.33.0 wrote for `apm install` with
// the same .mcp.json and these two servers declared in apm.yml.
func TestWriteMCP_Claude_KeepsNestedValuesOfExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := writeClaudeMCPFixture(t, dir, `{
  "$schema": "https://example.com/schema.json",
  "mcpServers": {
    "mine": {"command": "node", "args": [], "settings": {"retries": 3, "tags": ["a", "b"], "nested": {"deep": [1, {"k": null}], "label": "a <b> & c", "on": true, "empty": {}}}, "type": "stdio"}
  },
  "zeta": [{"b": 1, "a": 2}]
}
`)
	args := []string{"-y", "pkg"}
	prims := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{
			Name: "s1", Registry: false, Transport: "stdio", Command: "cmd", Args: &args,
			Env: map[string]string{"B_KEY": "2", "A_KEY": "1"}, EnvOrder: []string{"B_KEY", "A_KEY"},
		}),
		mcpPrim("local", &manifest.MCPDependency{Name: "s2", Registry: false, Transport: "http", URL: "https://example.com/mcp"}),
	}
	if _, _, diags, err := (&claudeAdapter{}).WriteMCP(prims, dir); err != nil {
		t.Fatalf("WriteMCP: %v (diags=%v)", err, diags)
	}
	want := `{
  "$schema": "https://example.com/schema.json",
  "mcpServers": {
    "mine": {
      "command": "node",
      "args": [],
      "settings": {
        "retries": 3,
        "tags": [
          "a",
          "b"
        ],
        "nested": {
          "deep": [
            1,
            {
              "k": null
            }
          ],
          "label": "a <b> & c",
          "on": true,
          "empty": {}
        }
      },
      "type": "stdio"
    },
    "s1": {
      "type": "stdio",
      "command": "cmd",
      "env": {
        "B_KEY": "2",
        "A_KEY": "1"
      },
      "args": [
        "-y",
        "pkg"
      ]
    },
    "s2": {
      "type": "http",
      "url": "https://example.com/mcp"
    }
  },
  "zeta": [
    {
      "b": 1,
      "a": 2
    }
  ]
}
`
	if got := readFileString(t, path); got != want {
		t.Errorf(".mcp.json differs from the Oracle's\n--- got\n%s\n--- want\n%s", got, want)
	}
}

// A server apm-go writes again keeps the keys no writer manages, after the
// managed ones; a managed key this run does not produce (env) is dropped.
// The expected file is Python's json.dumps(indent=2) of that document.
func TestWriteMCP_Claude_RewrittenServerKeepsUnmanagedNestedKeys(t *testing.T) {
	dir := t.TempDir()
	path := writeClaudeMCPFixture(t, dir, `{"mcpServers": {"s1": {"env": {"STALE": "1"}, "autoApprove": ["read", "write"], "command": "old", "limits": {"timeout": 30, "none": []}}, "other": {"url": "https://example.com/other"}}}`)
	prims := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{Name: "s1", Registry: false, Transport: "stdio", Command: "cmd"}),
	}
	if _, _, diags, err := (&claudeAdapter{}).WriteMCP(prims, dir); err != nil {
		t.Fatalf("WriteMCP: %v (diags=%v)", err, diags)
	}
	want := `{
  "mcpServers": {
    "s1": {
      "type": "stdio",
      "command": "cmd",
      "autoApprove": [
        "read",
        "write"
      ],
      "limits": {
        "timeout": 30,
        "none": []
      }
    },
    "other": {
      "url": "https://example.com/other"
    }
  }
}
`
	if got := readFileString(t, path); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestWriteMCP_Claude_InvalidExistingFileIsRefusedAndKept(t *testing.T) {
	tests := []struct{ name, content string }{
		{"empty file", ""},
		{"top level is an array", "[]"},
		{"top level is a string", `"mcpServers"`},
		{"second document", "{} {}"},
		{"stray closing brace", "}"},
		{"key is a number", `{1: 2}`},
		{"key is an object", `{{}: 2}`},
		{"value missing", `{"mcpServers": }`},
		{"array closed as value", `{"mcpServers": ]}`},
		{"bad literal in array", `{"mcpServers": {"s1": {"args": [tru]}}}`},
		{"array closed with a brace", `{"mcpServers": [1}`},
		{"object never closed", `{"mcpServers": {"s1": {}}`},
	}
	prims := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{Name: "s1", Registry: false, Transport: "stdio", Command: "cmd"}),
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeClaudeMCPFixture(t, dir, tt.content)
			_, _, _, err := (&claudeAdapter{}).WriteMCP(prims, dir)
			var probe map[string]any
			want := "is not valid, refusing to overwrite: " + json.Unmarshal([]byte(tt.content), &probe).Error()
			if err == nil || !strings.HasSuffix(err.Error(), want) {
				t.Fatalf("err = %v, want it to end with %q", err, want)
			}
			if got := readFileString(t, path); got != tt.content {
				t.Errorf("file changed to %q", got)
			}
		})
	}
}

// The Oracle writes its document over a .mcp.json that holds null; apart
// from the empty args list it adds (ticket 32 item 26), these are its bytes.
func TestWriteMCP_Claude_NullExistingFileIsAnEmptyDocument(t *testing.T) {
	dir := t.TempDir()
	path := writeClaudeMCPFixture(t, dir, "null")
	prims := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{Name: "s1", Registry: false, Transport: "stdio", Command: "cmd"}),
	}
	if _, _, _, err := (&claudeAdapter{}).WriteMCP(prims, dir); err != nil {
		t.Fatal(err)
	}
	const want = `{
  "mcpServers": {
    "s1": {
      "type": "stdio",
      "command": "cmd"
    }
  }
}
`
	if got := readFileString(t, path); got != want {
		t.Errorf(".mcp.json = %q, want %q", got, want)
	}
}

func TestWriteMCP_Claude_UnreadableLocationIsAnError(t *testing.T) {
	prims := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{Name: "s1", Registry: false, Transport: "stdio", Command: "cmd"}),
	}
	t.Run("project directory is a file", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "project")
		if err := os.WriteFile(dir, []byte("not a directory"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := (&claudeAdapter{}).WriteMCP(prims, dir); err == nil {
			t.Fatal("no error")
		}
		if got := readFileString(t, dir); got != "not a directory" {
			t.Errorf("file changed to %q", got)
		}
	})
	t.Run(".mcp.json is a directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, ".mcp.json"), 0755); err != nil {
			t.Fatal(err)
		}
		_, _, _, err := (&claudeAdapter{}).WriteMCP(prims, dir)
		if err == nil || !strings.Contains(err.Error(), "read existing ") {
			t.Fatalf("err = %v, want a read error", err)
		}
	})
}

// No caller passes such a value today; the writer must still fail before it
// replaces the file.
func TestWriteOrderedMCPJSON_ValueJSONCannotHoldLeavesFileAsItWas(t *testing.T) {
	const before = "{\n  \"mcpServers\": {}\n}\n"
	tests := []struct {
		name  string
		entry map[string]any
	}{
		{"in an entry", map[string]any{"type": "stdio", "command": math.NaN()}},
		{"in a list", map[string]any{"type": "stdio", "args": []any{"-y", math.NaN()}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeClaudeMCPFixture(t, dir, before)
			err := writeOrderedMCPJSON(dir, ".mcp.json", "mcpServers",
				map[string]map[string]any{"s1": tt.entry}, map[string]bool{"s1": true}, []string{"s1"}, claudeMCPKeyOrder, 0600)
			if err == nil {
				t.Fatal("no error")
			}
			if got := readFileString(t, path); got != before {
				t.Errorf("file changed to %q", got)
			}
		})
	}
}
