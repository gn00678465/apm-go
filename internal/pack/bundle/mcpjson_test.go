package bundle

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func decodeObj(t *testing.T, jsonText string) JSONValue {
	t.Helper()
	v, err := DecodeJSONValue([]byte(jsonText))
	if err != nil {
		t.Fatalf("decode %s: %v", jsonText, err)
	}
	return v
}

// ── SanitizeValue: key-name dropping ──────────────────────────────────────

func TestSanitizeValue_ExactKeyNamesDropped(t *testing.T) {
	for _, key := range []string{"env", "environment", "headers", "authorization"} {
		v := decodeObj(t, `{"`+key+`":{"X":"y"},"safe":"ok"}`)
		var dropped []string
		cleaned := SanitizeValue(v, "server", &dropped)
		if _, ok := cleaned.Get(key); ok {
			t.Errorf("key %q not dropped", key)
		}
		if _, ok := cleaned.Get("safe"); !ok {
			t.Errorf("key %q: unrelated 'safe' key incorrectly dropped", key)
		}
		if len(dropped) != 1 || dropped[0] != "server."+key {
			t.Errorf("key %q: dropped = %v, want [server.%s]", key, dropped, key)
		}
	}
}

func TestSanitizeValue_SubstringKeyNamesDropped(t *testing.T) {
	// "accessKey"/"API_KEY" are not exact matches but contain "key" -- the
	// substring rule is deliberately over-broad (plugin_manifest.py:80-84).
	for _, key := range []string{"accessKey", "API_KEY", "privateKey"} {
		v := decodeObj(t, `{"`+key+`":"secretvalue"}`)
		var dropped []string
		cleaned := SanitizeValue(v, "server", &dropped)
		if _, ok := cleaned.Get(key); ok {
			t.Errorf("key %q not dropped by substring rule", key)
		}
		if len(dropped) != 1 {
			t.Errorf("key %q: dropped = %v, want exactly one entry", key, dropped)
		}
	}
}

func TestSanitizeValue_ServerNameNotSensitivityTested(t *testing.T) {
	// A server literally named "my-keychain" must survive: only the VALUES
	// beneath a server are recursed into, never the server name itself.
	servers := decodeObj(t, `{"my-keychain":{"command":"npx"}}`)
	cleaned, dropped := SanitizeServers(servers)
	if _, ok := cleaned.Get("my-keychain"); !ok {
		t.Error("server named 'my-keychain' was incorrectly dropped")
	}
	if len(dropped) != 0 {
		t.Errorf("dropped = %v, want none (nothing sensitive under this server)", dropped)
	}
}

func TestSanitizeValue_NestedArbitraryDepthDropped(t *testing.T) {
	v := decodeObj(t, `{"config":{"nested":{"apikey":"secret"}}}`)
	var dropped []string
	cleaned := SanitizeValue(v, "srv", &dropped)
	config, _ := cleaned.Get("config")
	nested, _ := config.Get("nested")
	if _, ok := nested.Get("apikey"); ok {
		t.Error("deeply nested apikey key was not dropped")
	}
	want := "srv.config.nested.apikey"
	if len(dropped) != 1 || dropped[0] != want {
		t.Errorf("dropped = %v, want [%s]", dropped, want)
	}
}

// ── redactSecretValues: each of the six value-shaped rules ───────────────

func TestRedactSecretValues_URLUserinfo(t *testing.T) {
	got, changed := redactSecretValues("https://user:hunter2@example.com/path")
	if !changed || got != "https://***REDACTED***@example.com/path" {
		t.Errorf("got = %q, changed = %v", got, changed)
	}
}

func TestRedactSecretValues_InlineFlagEquals(t *testing.T) {
	got, changed := redactSecretValues("--token=sk-abcdef123456")
	if !changed || got != "--token="+redacted {
		t.Errorf("got = %q, changed = %v", got, changed)
	}
}

func TestRedactSecretValues_SpaceSeparatedSingleString(t *testing.T) {
	got, changed := redactSecretValues("npx server --token sk-abcdef123456")
	if !changed || !strings.Contains(got, "--token "+redacted) {
		t.Errorf("got = %q, changed = %v", got, changed)
	}
}

func TestRedactSecretValues_EnvAssignment(t *testing.T) {
	got, changed := redactSecretValues("API_KEY=sk-abcdef123456 npx server")
	if !changed || !strings.Contains(got, "API_KEY="+redacted) {
		t.Errorf("got = %q, changed = %v", got, changed)
	}
}

func TestRedactSecretValues_BearerAuthScheme(t *testing.T) {
	got, changed := redactSecretValues("Authorization: Bearer abcdefgh12345678")
	if !changed || !strings.Contains(got, "Bearer "+redacted) {
		t.Errorf("got = %q, changed = %v", got, changed)
	}
}

func TestRedactSecretValues_KnownProviderPrefixes(t *testing.T) {
	cases := []string{
		"ghp_" + strings.Repeat("a", 20),
		"sk-" + strings.Repeat("a", 20),
		"AKIA" + strings.Repeat("A", 12),
	}
	for _, token := range cases {
		got, changed := redactSecretValues("value=" + token)
		if !changed || strings.Contains(got, token) {
			t.Errorf("token %q: got = %q, changed = %v (want redacted)", token, got, changed)
		}
	}
}

func TestRedactSecretValues_NoSecretShapeUnchanged(t *testing.T) {
	got, changed := redactSecretValues("just a normal value")
	if changed || got != "just a normal value" {
		t.Errorf("got = %q, changed = %v, want unchanged", got, changed)
	}
}

// ── list-context flag/value separation ────────────────────────────────────

func TestSanitizeValue_ListContext_FlagValueSeparateElements(t *testing.T) {
	v := decodeObj(t, `{"args":["--token","sk-abcdef123456","--verbose"]}`)
	var dropped []string
	cleaned := SanitizeValue(v, "srv", &dropped)
	args, _ := cleaned.Get("args")
	if len(args.A) != 3 {
		t.Fatalf("args = %+v, want 3 elements", args.A)
	}
	if args.A[0].S != "--token" {
		t.Errorf("args[0] = %q, want the flag name preserved", args.A[0].S)
	}
	if args.A[1].S != redacted {
		t.Errorf("args[1] = %q, want %q (the separate value element redacted)", args.A[1].S, redacted)
	}
	if args.A[2].S != "--verbose" {
		t.Errorf("args[2] = %q, want unrelated flag preserved", args.A[2].S)
	}
	found := false
	for _, d := range dropped {
		if d == "srv.args[1]" {
			found = true
		}
	}
	if !found {
		t.Errorf("dropped = %v, want srv.args[1] present", dropped)
	}
}

// ── ReadMCPServers ──────────────────────────────────────────────────────

func TestReadMCPServers_FileAbsent_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	got := ReadMCPServers(dir)
	if !got.IsEmptyObject() {
		t.Errorf("got = %+v, want empty object for absent .mcp.json", got)
	}
}

func TestReadMCPServers_ValidFile_ReturnsServersPreservingOrder(t *testing.T) {
	dir := t.TempDir()
	content := `{"mcpServers":{"zebra":{"command":"a"},"apple":{"command":"b"}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ReadMCPServers(dir)
	if len(got.O) != 2 || got.O[0].Key != "zebra" || got.O[1].Key != "apple" {
		t.Fatalf("got order = %+v, want zebra, apple", got.O)
	}
}

func TestReadMCPServers_MalformedJSON_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ReadMCPServers(dir)
	if !got.IsEmptyObject() {
		t.Errorf("got = %+v, want empty object for malformed .mcp.json", got)
	}
}

func TestReadMCPServers_NonObjectMCPServersValue_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers":[1,2,3]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ReadMCPServers(dir)
	if !got.IsEmptyObject() {
		t.Errorf("got = %+v, want empty object when mcpServers is not an object", got)
	}
}

func TestReadMCPServers_Symlink_ReturnsEmpty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on windows")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	if err := os.WriteFile(real, []byte(`{"mcpServers":{"a":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".mcp.json")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	got := ReadMCPServers(dir)
	if !got.IsEmptyObject() {
		t.Errorf("got = %+v, want empty object for a symlinked .mcp.json", got)
	}
}
