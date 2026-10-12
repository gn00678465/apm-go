package deploy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/apm-go/apm/internal/rootfs"
)

// orderedObject is a JSON object that keeps the order of its keys. Claude's
// .mcp.json is compared with the Oracle's byte for byte, and Python writes a
// dict in insertion order where encoding/json sorts a map.
type orderedObject struct {
	keys []string
	vals map[string]any
}

func newOrderedObject() *orderedObject {
	return &orderedObject{vals: map[string]any{}}
}

func (o *orderedObject) set(key string, val any) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = val
}

func (o *orderedObject) remove(key string) {
	delete(o.vals, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			return
		}
	}
}

// decodeOrderedJSON parses one JSON document; every object in it becomes an
// *orderedObject, every array a []any, every number a json.Number.
func decodeOrderedJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	val, err := decodeOrderedValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after the JSON document")
	}
	return val, nil
}

func decodeOrderedValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, isDelim := tok.(json.Delim)
	if !isDelim {
		return tok, nil
	}
	// Token fails on a closing delimiter where a value starts and on an
	// object key that is not a string, so delim opens an array or an object
	// and every key token is a string.
	if delim == '[' {
		arr := []any{}
		for dec.More() {
			val, err := decodeOrderedValue(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, val)
		}
		_, err := dec.Token()
		return arr, err
	}
	obj := newOrderedObject()
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		val, err := decodeOrderedValue(dec)
		if err != nil {
			return nil, err
		}
		obj.set(keyTok.(string), val)
	}
	_, err = dec.Token()
	return obj, err
}

// encodeOrderedJSON renders val the way Python's json.dumps(indent=2) lays a
// document out, without a trailing newline.
func encodeOrderedJSON(val any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeOrderedValue(&buf, val, ""); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeOrderedValue(buf *bytes.Buffer, val any, indent string) error {
	inner := indent + "  "
	switch v := val.(type) {
	case *orderedObject:
		if len(v.keys) == 0 {
			buf.WriteString("{}")
			return nil
		}
		buf.WriteString("{\n")
		for i, key := range v.keys {
			buf.WriteString(inner)
			// A string always encodes.
			_ = writeJSONScalar(buf, key)
			buf.WriteString(": ")
			if err := writeOrderedValue(buf, v.vals[key], inner); err != nil {
				return err
			}
			if i < len(v.keys)-1 {
				buf.WriteByte(',')
			}
			buf.WriteByte('\n')
		}
		buf.WriteString(indent + "}")
		return nil
	case []string:
		arr := make([]any, len(v))
		for i, s := range v {
			arr[i] = s
		}
		return writeOrderedValue(buf, arr, indent)
	case []any:
		if len(v) == 0 {
			buf.WriteString("[]")
			return nil
		}
		buf.WriteString("[\n")
		for i, item := range v {
			buf.WriteString(inner)
			if err := writeOrderedValue(buf, item, inner); err != nil {
				return err
			}
			if i < len(v)-1 {
				buf.WriteByte(',')
			}
			buf.WriteByte('\n')
		}
		buf.WriteString(indent + "]")
		return nil
	default:
		return writeJSONScalar(buf, v)
	}
}

func writeJSONScalar(buf *bytes.Buffer, val any) error {
	var scalar bytes.Buffer
	enc := json.NewEncoder(&scalar)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(val); err != nil {
		return err
	}
	buf.WriteString(strings.TrimSuffix(scalar.String(), "\n"))
	return nil
}

// writeOrderedMCPJSON is writeMergedMCPJSON for a file whose key order is
// part of the contract. The document and the servers it already has keep
// their order; a server written for the first time goes last, in the order
// of names; the keys of a written server follow keyOrder, which names every
// key an entry can have, then the keys of the existing entry that no writer
// manages. An object inside an entry is an *orderedObject. The file ends
// with one newline.
func writeOrderedMCPJSON(projectDir, rel, topKey string, entries map[string]map[string]any, considered map[string]bool, names, keyOrder []string, perm os.FileMode) error {
	rw, err := rootfs.OpenRootWriter(projectDir)
	if err != nil {
		return err
	}
	defer rw.Close()

	root := newOrderedObject()
	data, err := rw.ReadFile(rel)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return fmt.Errorf("read existing %s: %w", rw.Path(rel), err)
	default:
		parsed, perr := decodeOrderedJSON(data)
		if obj, isObject := parsed.(*orderedObject); isObject {
			root = obj
			break
		}
		// The refusal carries encoding/json's own error for the file, the
		// text this line had before the ordered decoder (ruling R13). A
		// file that holds null is an empty document, as it was then.
		var probe map[string]any
		uerr := json.Unmarshal(data, &probe)
		if uerr == nil {
			uerr = perr
		}
		if uerr != nil {
			return fmt.Errorf("existing %s is not valid, refusing to overwrite: %w", rw.Path(rel), uerr)
		}
	}

	servers, _ := root.vals[topKey].(*orderedObject)
	if servers == nil {
		servers = newOrderedObject()
	}
	build := func(name string) *orderedObject {
		entry := entries[name]
		out := newOrderedObject()
		for _, key := range keyOrder {
			if val, ok := entry[key]; ok {
				out.set(key, val)
			}
		}
		if prev, ok := servers.vals[name].(*orderedObject); ok {
			for _, key := range prev.keys {
				if !managedMCPKeys[key] {
					out.set(key, prev.vals[key])
				}
			}
		}
		return out
	}
	for _, name := range append([]string(nil), servers.keys...) {
		if !considered[name] {
			continue
		}
		if _, written := entries[name]; written {
			servers.set(name, build(name))
		} else {
			servers.remove(name)
		}
	}
	for _, name := range names {
		if _, written := entries[name]; !written {
			continue
		}
		if _, exists := servers.vals[name]; !exists {
			servers.set(name, build(name))
		}
	}
	root.set(topKey, servers)

	out, err := encodeOrderedJSON(root)
	if err != nil {
		return err
	}
	return rw.WriteFileAtomicMode(rel, append(out, '\n'), perm)
}
