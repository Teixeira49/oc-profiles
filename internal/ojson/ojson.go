// Package ojson implements a minimal order-preserving JSON document model.
//
// opencode.json is hand-edited by users and rewritten by gentle-ai, so key
// order matters for readable diffs. encoding/json maps lose order; this
// package keeps it and re-encodes with the same layout as
// `json.dumps(indent=2, ensure_ascii=False)` (2-space indent, raw UTF-8,
// no HTML escaping).
package ojson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Object is a JSON object that remembers key insertion order.
type Object struct {
	keys []string
	vals map[string]any
}

// NewObject returns an empty ordered object.
func NewObject() *Object {
	return &Object{vals: map[string]any{}}
}

// Keys returns the keys in order. The returned slice is a copy.
func (o *Object) Keys() []string {
	return append([]string(nil), o.keys...)
}

// Len returns the number of keys.
func (o *Object) Len() int { return len(o.keys) }

// Get returns the value for key.
func (o *Object) Get(key string) (any, bool) {
	v, ok := o.vals[key]
	return v, ok
}

// GetObject returns the value for key when it is an object.
func (o *Object) GetObject(key string) (*Object, bool) {
	v, ok := o.vals[key]
	if !ok {
		return nil, false
	}
	obj, ok := v.(*Object)
	return obj, ok
}

// GetString returns the value for key when it is a string.
func (o *Object) GetString(key string) (string, bool) {
	v, ok := o.vals[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// Has reports whether key exists.
func (o *Object) Has(key string) bool {
	_, ok := o.vals[key]
	return ok
}

// Set replaces the value of an existing key in place, or appends a new key.
func (o *Object) Set(key string, val any) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = val
}

// SetSorted replaces an existing key in place. A new key is inserted before
// the first existing key that sorts after it, which reproduces alphabetical
// layout when the object is already sorted.
func (o *Object) SetSorted(key string, val any) {
	if _, ok := o.vals[key]; ok {
		o.vals[key] = val
		return
	}
	idx := len(o.keys)
	for i, k := range o.keys {
		if k > key {
			idx = i
			break
		}
	}
	o.InsertAt(idx, key, val)
}

// InsertAt inserts a new key at position idx (clamped). If the key already
// exists its value is replaced in place and idx is ignored.
func (o *Object) InsertAt(idx int, key string, val any) {
	if _, ok := o.vals[key]; ok {
		o.vals[key] = val
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx > len(o.keys) {
		idx = len(o.keys)
	}
	o.keys = append(o.keys, "")
	copy(o.keys[idx+1:], o.keys[idx:])
	o.keys[idx] = key
	o.vals[key] = val
}

// IndexOf returns the position of key or -1.
func (o *Object) IndexOf(key string) int {
	for i, k := range o.keys {
		if k == key {
			return i
		}
	}
	return -1
}

// Delete removes key. It reports whether the key existed.
func (o *Object) Delete(key string) bool {
	if _, ok := o.vals[key]; !ok {
		return false
	}
	delete(o.vals, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
	return true
}

// Clone returns a deep copy of v (objects, arrays and scalars).
func Clone(v any) any {
	switch t := v.(type) {
	case *Object:
		c := NewObject()
		for _, k := range t.keys {
			c.Set(k, Clone(t.vals[k]))
		}
		return c
	case []any:
		c := make([]any, len(t))
		for i, e := range t {
			c[i] = Clone(e)
		}
		return c
	default:
		return v
	}
}

// CloneObject deep-copies an object.
func CloneObject(o *Object) *Object {
	return Clone(o).(*Object)
}

// Parse decodes a JSON document, preserving object key order. Numbers are
// kept as json.Number so they round-trip byte-for-byte.
func Parse(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := parseValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("ojson: trailing data after top-level value")
	}
	return v, nil
}

// ParseObject decodes a document whose top-level value must be an object.
func ParseObject(data []byte) (*Object, error) {
	v, err := Parse(data)
	if err != nil {
		return nil, err
	}
	obj, ok := v.(*Object)
	if !ok {
		return nil, fmt.Errorf("ojson: top-level value is not an object")
	}
	return obj, nil
}

func parseValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := NewObject()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := kt.(string)
				if !ok {
					return nil, fmt.Errorf("ojson: expected object key, got %v", kt)
				}
				val, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				obj.Set(key, val)
			}
			if _, err := dec.Token(); err != nil { // consume '}'
				return nil, err
			}
			return obj, nil
		case '[':
			arr := []any{}
			for dec.More() {
				val, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
			if _, err := dec.Token(); err != nil { // consume ']'
				return nil, err
			}
			return arr, nil
		}
		return nil, fmt.Errorf("ojson: unexpected delimiter %v", t)
	default:
		return tok, nil // string, json.Number, bool, nil
	}
}

// Marshal encodes v with a 2-space indent and no trailing newline.
func Marshal(v any) ([]byte, error) {
	return MarshalOpts(v, false)
}

// MarshalOpts is Marshal with optional HTML escaping of <, > and & (as
// encoding/json does by default). gentle-ai writes opencode.json with HTML
// escaping, so re-encoding in the same style keeps unrelated bytes stable.
func MarshalOpts(v any, escapeHTML bool) ([]byte, error) {
	var buf bytes.Buffer
	if err := encode(&buf, v, 0, escapeHTML); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encode(buf *bytes.Buffer, v any, depth int, html bool) error {
	switch t := v.(type) {
	case *Object:
		if len(t.keys) == 0 {
			buf.WriteString("{}")
			return nil
		}
		buf.WriteString("{\n")
		for i, k := range t.keys {
			indent(buf, depth+1)
			if err := encodeString(buf, k, html); err != nil {
				return err
			}
			buf.WriteString(": ")
			if err := encode(buf, t.vals[k], depth+1, html); err != nil {
				return err
			}
			if i < len(t.keys)-1 {
				buf.WriteByte(',')
			}
			buf.WriteByte('\n')
		}
		indent(buf, depth)
		buf.WriteByte('}')
	case []any:
		if len(t) == 0 {
			buf.WriteString("[]")
			return nil
		}
		buf.WriteString("[\n")
		for i, e := range t {
			indent(buf, depth+1)
			if err := encode(buf, e, depth+1, html); err != nil {
				return err
			}
			if i < len(t)-1 {
				buf.WriteByte(',')
			}
			buf.WriteByte('\n')
		}
		indent(buf, depth)
		buf.WriteByte(']')
	case string:
		return encodeString(buf, t, html)
	case json.Number:
		buf.WriteString(t.String())
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case nil:
		buf.WriteString("null")
	default:
		// Fall back to encoding/json for plain Go values (e.g. float64, int).
		b, err := json.Marshal(t)
		if err != nil {
			return err
		}
		buf.Write(b)
	}
	return nil
}

func encodeString(buf *bytes.Buffer, s string, html bool) error {
	var tmp bytes.Buffer
	enc := json.NewEncoder(&tmp)
	enc.SetEscapeHTML(html)
	if err := enc.Encode(s); err != nil {
		return err
	}
	buf.Write(bytes.TrimRight(tmp.Bytes(), "\n"))
	return nil
}

func indent(buf *bytes.Buffer, depth int) {
	for i := 0; i < depth; i++ {
		buf.WriteString("  ")
	}
}

// UsesHTMLEscaping reports whether a JSON document was written with HTML
// escaping (it contains \u003c, \u003e or \u0026 escapes).
func UsesHTMLEscaping(data []byte) bool {
	for _, esc := range []string{`\u003c`, `\u003e`, `\u0026`} {
		if bytes.Contains(data, []byte(esc)) {
			return true
		}
	}
	return false
}
