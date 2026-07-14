// Package bundle implements apm-go pack's BundleProducer (plugin-format
// bundle export) and the .mcp.json reader/sanitizer shared with
// PluginManifestProducer, mirroring Python's bundle/plugin_exporter.py and
// core/plugin_manifest.py's collect_mcp_servers (research/
// pack-parity-findings.md §2.4/§3).
package bundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// ValueKind discriminates JSONValue's active field, mirroring the shape of
// a decoded JSON document.
type ValueKind int

const (
	KindNull ValueKind = iota
	KindBool
	KindNumber
	KindString
	KindArray
	KindObject
)

// JSONValue is a JSON value that preserves object key insertion order.
// Needed because Python's json.dumps(..., sort_keys=False) preserves
// dict insertion order when embedding .mcp.json's mcpServers into
// plugin.json (findings §2.4/§3.6) -- encoding/json's map[string]any
// marshaling always sorts keys alphabetically, which would silently
// reorder mcpServers relative to the source .mcp.json.
type JSONValue struct {
	Kind ValueKind
	B    bool
	N    json.Number
	S    string
	A    []JSONValue
	O    []JSONField
}

// JSONField is one key/value pair of an ordered JSON object.
type JSONField struct {
	Key string
	Val JSONValue
}

// StringValue, ObjectValue, and ArrayOfStrings are small constructors for
// building a JSONValue tree from native Go values (used by
// pluginmanifest/write.go to assemble plugin.json's non-mcpServers fields).
func StringValue(s string) JSONValue { return JSONValue{Kind: KindString, S: s} }

func ObjectValue(fields ...JSONField) JSONValue {
	return JSONValue{Kind: KindObject, O: fields}
}

func ArrayOfStrings(ss []string) JSONValue {
	v := JSONValue{Kind: KindArray, A: make([]JSONValue, len(ss))}
	for i, s := range ss {
		v.A[i] = StringValue(s)
	}
	return v
}

// IsEmptyObject reports whether v is an object with zero fields (or not an
// object at all) -- used to decide whether an optional section (e.g.
// mcpServers) should be omitted entirely.
func (v JSONValue) IsEmptyObject() bool {
	return v.Kind != KindObject || len(v.O) == 0
}

// Get looks up key in an object value, mirroring dict.get. Returns
// (JSONValue{}, false) for a non-object or a missing key.
func (v JSONValue) Get(key string) (JSONValue, bool) {
	if v.Kind != KindObject {
		return JSONValue{}, false
	}
	for _, f := range v.O {
		if f.Key == key {
			return f.Val, true
		}
	}
	return JSONValue{}, false
}

// DecodeJSONValue parses data into an order-preserving JSONValue tree.
func DecodeJSONValue(data []byte) (JSONValue, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeNext(dec)
	if err != nil {
		return JSONValue{}, err
	}
	return v, nil
}

func decodeNext(dec *json.Decoder) (JSONValue, error) {
	tok, err := dec.Token()
	if err != nil {
		return JSONValue{}, err
	}
	return decodeToken(dec, tok)
}

func decodeToken(dec *json.Decoder, tok json.Token) (JSONValue, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return decodeObject(dec)
		case '[':
			return decodeArray(dec)
		}
	case bool:
		return JSONValue{Kind: KindBool, B: t}, nil
	case json.Number:
		return JSONValue{Kind: KindNumber, N: t}, nil
	case string:
		return JSONValue{Kind: KindString, S: t}, nil
	case nil:
		return JSONValue{Kind: KindNull}, nil
	}
	return JSONValue{}, fmt.Errorf("bundle: unexpected JSON token %v (%T)", tok, tok)
}

func decodeObject(dec *json.Decoder) (JSONValue, error) {
	obj := JSONValue{Kind: KindObject}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return JSONValue{}, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return JSONValue{}, fmt.Errorf("bundle: unexpected object key token %v", keyTok)
		}
		val, err := decodeNext(dec)
		if err != nil {
			return JSONValue{}, err
		}
		obj.O = append(obj.O, JSONField{Key: key, Val: val})
	}
	if _, err := dec.Token(); err != nil { // consume '}'
		return JSONValue{}, err
	}
	return obj, nil
}

func decodeArray(dec *json.Decoder) (JSONValue, error) {
	arr := JSONValue{Kind: KindArray}
	for dec.More() {
		val, err := decodeNext(dec)
		if err != nil {
			return JSONValue{}, err
		}
		arr.A = append(arr.A, val)
	}
	if _, err := dec.Token(); err != nil { // consume ']'
		return JSONValue{}, err
	}
	return arr, nil
}

// SortedClone returns a deep copy of v with every object's fields sorted
// alphabetically by key (array element order is untouched) -- mirrors
// Python's json.dumps(..., sort_keys=True), used for the bundle's own
// hooks.json/.mcp.json output (plugin_exporter.py:616,622), as opposed to
// plugin.json's sort_keys=False (insertion order preserved).
func (v JSONValue) SortedClone() JSONValue {
	switch v.Kind {
	case KindObject:
		clone := JSONValue{Kind: KindObject, O: make([]JSONField, len(v.O))}
		for i, f := range v.O {
			clone.O[i] = JSONField{Key: f.Key, Val: f.Val.SortedClone()}
		}
		sort.Slice(clone.O, func(i, j int) bool { return clone.O[i].Key < clone.O[j].Key })
		return clone
	case KindArray:
		clone := JSONValue{Kind: KindArray, A: make([]JSONValue, len(v.A))}
		for i, e := range v.A {
			clone.A[i] = e.SortedClone()
		}
		return clone
	default:
		return v
	}
}

// MarshalIndent serializes v as 2-space-indented JSON (no trailing
// newline -- callers append their own), matching Python's
// json.dumps(x, indent=2)'s exact layout: "{\n  \"key\": value,\n...\n}",
// empty object/array collapsed to "{}"/"[]", no HTML-escaping (Python's
// json.dumps never HTML-escapes either).
func MarshalIndent(v JSONValue) []byte {
	var buf bytes.Buffer
	writeIndented(&buf, v, "")
	return buf.Bytes()
}

func writeIndented(buf *bytes.Buffer, v JSONValue, indent string) {
	switch v.Kind {
	case KindNull:
		buf.WriteString("null")
	case KindBool:
		if v.B {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case KindNumber:
		buf.WriteString(v.N.String())
	case KindString:
		writeJSONString(buf, v.S)
	case KindArray:
		writeIndentedArray(buf, v.A, indent)
	case KindObject:
		writeIndentedObject(buf, v.O, indent)
	}
}

func writeIndentedArray(buf *bytes.Buffer, items []JSONValue, indent string) {
	if len(items) == 0 {
		buf.WriteString("[]")
		return
	}
	buf.WriteString("[\n")
	childIndent := indent + "  "
	for i, e := range items {
		buf.WriteString(childIndent)
		writeIndented(buf, e, childIndent)
		if i < len(items)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString(indent)
	buf.WriteByte(']')
}

func writeIndentedObject(buf *bytes.Buffer, fields []JSONField, indent string) {
	if len(fields) == 0 {
		buf.WriteString("{}")
		return
	}
	buf.WriteString("{\n")
	childIndent := indent + "  "
	for i, f := range fields {
		buf.WriteString(childIndent)
		writeJSONString(buf, f.Key)
		buf.WriteString(": ")
		writeIndented(buf, f.Val, childIndent)
		if i < len(fields)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString(indent)
	buf.WriteByte('}')
}

// writeJSONString writes s as a quoted JSON string, escaping only what the
// JSON grammar requires (quote, backslash, control characters) -- never
// HTML-escaping '<'/'>'/'&', matching Python's json.dumps default.
func writeJSONString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(buf, `\u%04x`, r)
			} else {
				buf.WriteRune(r)
			}
		}
	}
	buf.WriteByte('"')
}

// maxMergeDepth mirrors plugin_exporter.py's _MAX_MERGE_DEPTH.
const maxMergeDepth = 20

// DeepMerge recursively merges overlay into base, mirroring
// plugin_exporter.py's _deep_merge: when overwrite is false, an existing
// base key wins (overlay only fills in keys base doesn't have, recursing
// when both sides are objects); when overwrite is true, overlay keys win
// (still recursing into nested objects rather than replacing them
// wholesale). Non-object base/overlay values are treated as empty objects.
// Returns an error if merge nesting exceeds maxMergeDepth.
func DeepMerge(base, overlay JSONValue, overwrite bool) (JSONValue, error) {
	return deepMerge(base, overlay, overwrite, 0)
}

func deepMerge(base, overlay JSONValue, overwrite bool, depth int) (JSONValue, error) {
	if depth > maxMergeDepth {
		return JSONValue{}, fmt.Errorf("hooks/MCP config exceeds maximum nesting depth (%d)", maxMergeDepth)
	}
	result := JSONValue{Kind: KindObject}
	if base.Kind == KindObject {
		result.O = append(result.O, base.O...)
	}
	for _, of := range overlay.O {
		idx := indexOfField(result.O, of.Key)
		if idx < 0 {
			result.O = append(result.O, JSONField{Key: of.Key, Val: of.Val})
			continue
		}
		existing := result.O[idx].Val
		switch {
		case overwrite && existing.Kind == KindObject && of.Val.Kind == KindObject:
			merged, err := deepMerge(existing, of.Val, true, depth+1)
			if err != nil {
				return JSONValue{}, err
			}
			result.O[idx].Val = merged
		case overwrite:
			result.O[idx].Val = of.Val
		case existing.Kind == KindObject && of.Val.Kind == KindObject:
			merged, err := deepMerge(existing, of.Val, false, depth+1)
			if err != nil {
				return JSONValue{}, err
			}
			result.O[idx].Val = merged
		default:
			// overwrite=false and not both objects: base wins, no-op.
		}
	}
	return result, nil
}

func indexOfField(fields []JSONField, key string) int {
	for i, f := range fields {
		if f.Key == key {
			return i
		}
	}
	return -1
}
