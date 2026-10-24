package deploy

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// decodeOrderedTOML reads a TOML document into *orderedObject tables whose
// keys keep the order the document has them in, as tomlkit keeps them when
// the Oracle reads .codex/config.toml (adapters/client/codex.py:105-126).
func decodeOrderedTOML(data []byte) (*orderedObject, error) {
	var values map[string]any
	if err := toml.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	order := map[string]int{}
	note := func(path []string) {
		for i := range path {
			k := strings.Join(path[:i+1], "\x00")
			if _, ok := order[k]; !ok {
				order[k] = len(order)
			}
		}
	}
	var noteValue func(path []string, v *unstable.Node)
	noteValue = func(path []string, v *unstable.Node) {
		if v.Kind != unstable.InlineTable {
			return
		}
		for it := v.Children(); it.Next(); {
			kv := it.Node()
			p := append(append([]string(nil), path...), tomlKeyParts(kv)...)
			note(p)
			noteValue(p, kv.Value())
		}
	}
	p := unstable.Parser{}
	p.Reset(data)
	var table []string
	for p.NextExpression() {
		e := p.Expression()
		switch e.Kind {
		case unstable.Table, unstable.ArrayTable:
			table = tomlKeyParts(e)
			note(table)
		case unstable.KeyValue:
			path := append(append([]string(nil), table...), tomlKeyParts(e)...)
			note(path)
			noteValue(path, e.Value())
		}
	}
	if err := p.Error(); err != nil {
		return nil, err
	}
	return orderedTOMLTable(nil, values, order), nil
}

func tomlKeyParts(n *unstable.Node) []string {
	var parts []string
	for it := n.Key(); it.Next(); {
		parts = append(parts, string(it.Node().Data))
	}
	return parts
}

func orderedTOMLTable(path []string, m map[string]any, order map[string]int) *orderedObject {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	pos := func(k string) (int, bool) {
		i, ok := order[strings.Join(append(append([]string(nil), path...), k), "\x00")]
		return i, ok
	}
	sort.Slice(keys, func(i, j int) bool {
		pi, oki := pos(keys[i])
		pj, okj := pos(keys[j])
		if oki != okj {
			return oki
		}
		if oki && pi != pj {
			return pi < pj
		}
		return keys[i] < keys[j]
	})
	out := newOrderedObject()
	for _, k := range keys {
		out.set(k, orderedTOMLValue(append(append([]string(nil), path...), k), m[k], order))
	}
	return out
}

func orderedTOMLValue(path []string, v any, order map[string]int) any {
	switch x := v.(type) {
	case map[string]any:
		return orderedTOMLTable(path, x, order)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = orderedTOMLValue(path, item, order)
		}
		return out
	}
	return v
}

// encodeTOMLkit writes root as tomlkit.dumps writes a document built from
// Python dicts (adapters/client/codex.py:126): a table's keys that hold no
// table first, then its tables, each under a header that a blank line
// precedes; a table that holds only tables gets no header of its own; a
// string is a basic string with tomlkit's escapes and an array is inline.
// A document the user edited by hand loses its comments and layout, as it
// did with go-toml before; tomlkit keeps them.
func encodeTOMLkit(root *orderedObject) ([]byte, error) {
	var b bytes.Buffer
	if err := writeTOMLBody(&b, nil, root); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func writeTOMLBody(b *bytes.Buffer, path []string, t *orderedObject) error {
	for _, k := range t.keys {
		v := t.vals[k]
		if tomlTable(v) != nil || tomlTables(v) != nil {
			continue
		}
		b.WriteString(tomlKey(k) + " = ")
		if err := writeTOMLValue(b, v); err != nil {
			return fmt.Errorf("%s: %w", strings.Join(append(append([]string(nil), path...), k), "."), err)
		}
		b.WriteByte('\n')
	}
	for _, k := range t.keys {
		v := t.vals[k]
		p := append(append([]string(nil), path...), k)
		if sub := tomlTable(v); sub != nil {
			if !superTable(sub) {
				tomlHeader(b, "["+tomlPath(p)+"]")
			}
			if err := writeTOMLBody(b, p, sub); err != nil {
				return err
			}
			continue
		}
		for _, item := range tomlTables(v) {
			tomlHeader(b, "[["+tomlPath(p)+"]]")
			if err := writeTOMLBody(b, p, item); err != nil {
				return err
			}
		}
	}
	return nil
}

func tomlHeader(b *bytes.Buffer, header string) {
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	b.WriteString(header + "\n")
}

// superTable: a table that holds tables alone, which tomlkit writes with no
// header of its own.
func superTable(t *orderedObject) bool {
	if len(t.keys) == 0 {
		return false
	}
	for _, k := range t.keys {
		if tomlTable(t.vals[k]) == nil && tomlTables(t.vals[k]) == nil {
			return false
		}
	}
	return true
}

func tomlTable(v any) *orderedObject {
	switch x := v.(type) {
	case *orderedObject:
		return x
	case map[string]string:
		out := newOrderedObject()
		for _, k := range sortedKeys(x) {
			out.set(k, x[k])
		}
		return out
	case map[string]any:
		return orderedTOMLTable(nil, x, nil)
	}
	return nil
}

// tomlTables is an array of tables, written as [[...]].
func tomlTables(v any) []*orderedObject {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return nil
	}
	out := make([]*orderedObject, 0, len(arr))
	for _, item := range arr {
		t := tomlTable(item)
		if t == nil {
			return nil
		}
		out = append(out, t)
	}
	return out
}

func tomlPath(path []string) string {
	parts := make([]string, len(path))
	for i, k := range path {
		parts[i] = tomlKey(k)
	}
	return strings.Join(parts, ".")
}

// tomlKey is a bare key when it is ASCII letters, digits, - and _ alone,
// else a basic string (tomlkit/items.py:436-444).
func tomlKey(k string) string {
	if k == "" || strings.IndexFunc(k, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}) >= 0 {
		return tomlString(k)
	}
	return k
}

// tomlString is tomlkit's basic string: \b \t \n \f \r \" \\ in their short
// form, every other control character and DEL as \u00xx, everything else
// as it is (tomlkit/_utils.py:106-156).
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func writeTOMLValue(b *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case string:
		b.WriteString(tomlString(x))
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case float64:
		b.WriteString(tomlFloat(x))
	case []string:
		arr := make([]any, len(x))
		for i, s := range x {
			arr[i] = s
		}
		return writeTOMLValue(b, arr)
	case []any:
		b.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				b.WriteString(", ")
			}
			if t := tomlTable(item); t != nil {
				if err := writeTOMLInline(b, t); err != nil {
					return err
				}
				continue
			}
			if err := writeTOMLValue(b, item); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	default:
		// A date or time: go-toml writes it as TOML has it.
		out, err := toml.Marshal(map[string]any{"v": v})
		if err != nil {
			return err
		}
		b.WriteString(strings.TrimSuffix(strings.TrimPrefix(string(out), "v = "), "\n"))
	}
	return nil
}

func writeTOMLInline(b *bytes.Buffer, t *orderedObject) error {
	b.WriteByte('{')
	for i, k := range t.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(" " + tomlKey(k) + " = ")
		v := t.vals[k]
		if sub := tomlTable(v); sub != nil {
			if err := writeTOMLInline(b, sub); err != nil {
				return err
			}
			continue
		}
		if err := writeTOMLValue(b, v); err != nil {
			return err
		}
	}
	if len(t.keys) > 0 {
		b.WriteByte(' ')
	}
	b.WriteByte('}')
	return nil
}

// tomlFloat is Python's repr of the float, which tomlkit writes, with TOML's
// inf and nan.
func tomlFloat(f float64) string {
	return manifest.PyFloatRepr(f)
}
