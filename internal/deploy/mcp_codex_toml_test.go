package deploy

import "testing"

// obj builds an orderedObject from key/value pairs, in order.
func obj(kv ...any) *orderedObject {
	o := newOrderedObject()
	for i := 0; i < len(kv); i += 2 {
		o.set(kv[i].(string), kv[i+1])
	}
	return o
}

// The bytes are what tomlkit.dumps, which the Oracle's codex adapter writes
// with (adapters/client/codex.py:126), gives for the same documents
// (C/scripts/tomlkit_ref.py and tomlkit_ref2.py, run with tomlkit from the
// Oracle 0.33.0 venv).
func TestEncodeTOMLkit_OracleBytes(t *testing.T) {
	tests := []struct {
		name string
		doc  *orderedObject
		want string
	}{
		{
			name: "remote and stdio servers, escapes and quoted keys",
			doc: obj("mcp_servers", obj(
				"io.example/demo", obj("url", "https://h/x", "bearer_token_env_var", "TOK",
					"http_headers", obj("X-A", "v\"q", "x.b", "c"), "env_http_headers", obj("Z", "ZV")),
				"plain", obj("command", "c\\d\teé\x01\x7f\n", "args", []string{}, "env", obj()),
				"one", obj("command", "x", "args", []string{"a"}, "env", obj("K", "1")),
			)),
			want: "[mcp_servers.\"io.example/demo\"]\nurl = \"https://h/x\"\nbearer_token_env_var = \"TOK\"\n\n" +
				"[mcp_servers.\"io.example/demo\".http_headers]\nX-A = \"v\\\"q\"\n\"x.b\" = \"c\"\n\n" +
				"[mcp_servers.\"io.example/demo\".env_http_headers]\nZ = \"ZV\"\n\n" +
				"[mcp_servers.plain]\ncommand = \"c\\\\d\\teé\\u0001\\u007f\\n\"\nargs = []\n\n[mcp_servers.plain.env]\n\n" +
				"[mcp_servers.one]\ncommand = \"x\"\nargs = [\"a\"]\n\n[mcp_servers.one.env]\nK = \"1\"\n",
		},
		{
			name: "a root key before the servers",
			doc:  obj("model", "o3", "mcp_servers", obj("a", obj("command", "x", "args", []string{}, "env", obj()))),
			want: "model = \"o3\"\n\n[mcp_servers.a]\ncommand = \"x\"\nargs = []\n\n[mcp_servers.a.env]\n",
		},
		{
			name: "scalars before subtables",
			doc:  obj("t", obj("sub", obj("k", "v"), "x", int64(1))),
			want: "[t]\nx = 1\n\n[t.sub]\nk = \"v\"\n",
		},
		{
			name: "other value types and an empty key",
			doc: obj("mcp_servers", obj(
				"a b", obj("n", int64(1), "f", 1.5, "b", true, "s", "it's", "e", "", "u", "日本"),
				"", obj("k", "v"))),
			want: "[mcp_servers.\"a b\"]\nn = 1\nf = 1.5\nb = true\ns = \"it's\"\ne = \"\"\nu = \"日本\"\n\n[mcp_servers.\"\"]\nk = \"v\"\n",
		},
		{
			name: "no server",
			doc:  obj("mcp_servers", obj()),
			want: "[mcp_servers]\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := encodeTOMLkit(tt.doc)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}
