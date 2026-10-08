package yamlcore

import (
	"testing"

	"go.yaml.in/yaml/v4"
)

// scalarAt returns the value node of key inside the first element of the
// top-level "items" sequence of src.
func scalarAt(t *testing.T, src []byte, key string) *yaml.Node {
	t.Helper()
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "items" {
			continue
		}
		el := root.Content[i+1].Content[0]
		for j := 0; j+1 < len(el.Content); j += 2 {
			if el.Content[j].Value == key {
				return el.Content[j+1]
			}
		}
	}
	t.Fatalf("no key %q in the first item", key)
	return nil
}

func TestReplaceScalarValue(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		key   string
		value string
		want  string
	}{
		{
			name:  "plain",
			src:   "items:\n  - name: a\n    ref: abc\n    subdir: x\n",
			key:   "ref",
			value: "def0",
			want:  "items:\n  - name: a\n    ref: def0\n    subdir: x\n",
		},
		{
			name:  "single quoted keeps single quotes",
			src:   "items:\n  - name: a\n    version: '1.0.0'\n",
			key:   "version",
			value: "2.0.0",
			want:  "items:\n  - name: a\n    version: '2.0.0'\n",
		},
		{
			name:  "double quoted keeps double quotes",
			src:   "items:\n  - name: a\n    version: \"1.0.0\"\n",
			key:   "version",
			value: "2.0.0",
			want:  "items:\n  - name: a\n    version: \"2.0.0\"\n",
		},
		{
			name:  "trailing comment and its spacing stay",
			src:   "items:\n  - name: a\n    ref: abc   # pinned by hand\n    subdir: x\n",
			key:   "ref",
			value: "def0",
			want:  "items:\n  - name: a\n    ref: def0   # pinned by hand\n    subdir: x\n",
		},
		{
			name:  "quoted value holding a hash and a quote",
			src:   "items:\n  - name: a\n    ref: 'it''s #1' # note\n",
			key:   "ref",
			value: "def0",
			want:  "items:\n  - name: a\n    ref: 'def0' # note\n",
		},
		{
			name:  "CRLF",
			src:   "items:\r\n  - name: a\r\n    ref: abc\r\n    subdir: x\r\n",
			key:   "ref",
			value: "def0",
			want:  "items:\r\n  - name: a\r\n    ref: def0\r\n    subdir: x\r\n",
		},
		{
			name:  "value on the dash line",
			src:   "items:\n  - ref: abc\n    name: a\n",
			key:   "ref",
			value: "def0",
			want:  "items:\n  - ref: def0\n    name: a\n",
		},
		{
			name:  "non-ASCII before the value on the same line",
			src:   "items:\n  - \"鍵\": abc\n",
			key:   "鍵",
			value: "def0",
			want:  "items:\n  - \"鍵\": def0\n",
		},
		{
			name:  "last line without a newline",
			src:   "items:\n  - ref: abc",
			key:   "ref",
			value: "def0",
			want:  "items:\n  - ref: def0",
		},
		{
			name:  "a plain value that would read as a number is quoted",
			src:   "items:\n  - ref: abc\n",
			key:   "ref",
			value: "1234567890123456789012345678901234567890",
			want:  "items:\n  - ref: \"1234567890123456789012345678901234567890\"\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte(tc.src)
			out, ok := ReplaceScalarValue(src, scalarAt(t, src, tc.key), tc.value)
			if !ok {
				t.Fatal("ok = false, want true")
			}
			if string(out) != tc.want {
				t.Errorf("out =\n%q\nwant\n%q", out, tc.want)
			}
			if string(src) != tc.src {
				t.Errorf("src was modified: %q", src)
			}
		})
	}
}

func TestReplaceScalarValue_DeclinesWhatItCannotReplaceInPlace(t *testing.T) {
	cases := []struct {
		name string
		src  string
		key  string
	}{
		{"literal block", "items:\n  - ref: |\n      abc\n", "ref"},
		{"folded block", "items:\n  - ref: >\n      abc\n", "ref"},
		{"multi-line plain", "items:\n  - ref: abc\n      def\n", "ref"},
		{"multi-line double quoted", "items:\n  - ref: \"abc\n      def\"\n", "ref"},
		{"flow mapping, middle key", "items:\n  - {ref: abc, name: a}\n", "ref"},
		{"flow mapping, last key", "items:\n  - {name: a, ref: abc}\n", "ref"},
		{"explicit tag", "items:\n  - ref: !!str abc\n", "ref"},
		{"mapping value", "items:\n  - ref:\n      a: b\n", "ref"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte(tc.src)
			out, ok := ReplaceScalarValue(src, scalarAt(t, src, tc.key), "def0")
			if ok {
				t.Fatalf("ok = true, out = %q; want ok = false", out)
			}
		})
	}
}
