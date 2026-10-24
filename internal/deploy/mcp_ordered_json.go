package deploy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
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
