package agentcatalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// node is a JSON value that remembers its source: object members keep their
// order and scalars keep their exact bytes, so a file the person edited by
// hand is written back as they wrote it. A generic decode and encode would
// lose the key order.
type node struct {
	kind    byte // 'o' object, 'a' array, 's' scalar
	members []*member
	items   []*node
	raw     []byte // scalar: its bytes as written
}

type member struct {
	key string
	val *node
}

func newObject() *node { return &node{kind: 'o'} }
func newArray() *node  { return &node{kind: 'a'} }

// scalarOf is the node of a Go value: a string, bool, number or nil.
func scalarOf(v any) *node {
	return &node{kind: 's', raw: marshal(v)}
}

// marshal is v as JSON, without escaping <, > and & the way json.Marshal does.
func marshal(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimRight(b.Bytes(), "\n")
}

// stringArray is the node of a list of strings.
func stringArray(items []string) *node {
	n := newArray()
	for _, s := range items {
		n.items = append(n.items, scalarOf(s))
	}
	return n
}

// get is the member named key, or nil.
func (n *node) get(key string) *node {
	if n == nil || n.kind != 'o' {
		return nil
	}
	for _, m := range n.members {
		if m.key == key {
			return m.val
		}
	}
	return nil
}

// set replaces the member named key, or appends it.
func (n *node) set(key string, v *node) {
	for _, m := range n.members {
		if m.key == key {
			m.val = v
			return
		}
	}
	n.members = append(n.members, &member{key: key, val: v})
}

// remove drops the member named key.
func (n *node) remove(key string) {
	for i, m := range n.members {
		if m.key == key {
			n.members = append(n.members[:i], n.members[i+1:]...)
			return
		}
	}
}

// rename gives a member a new key, keeping its place.
func (n *node) rename(from, to string) {
	for _, m := range n.members {
		if m.key == from {
			m.key = to
			return
		}
	}
}

// keys are the member keys in file order.
func (n *node) keys() []string {
	if n == nil || n.kind != 'o' {
		return nil
	}
	out := make([]string, 0, len(n.members))
	for _, m := range n.members {
		out = append(out, m.key)
	}
	return out
}

// str is a string scalar's value; "" for anything else.
func (n *node) str() string {
	if n == nil || n.kind != 's' {
		return ""
	}
	var s string
	if json.Unmarshal(n.raw, &s) != nil {
		return ""
	}
	return s
}

// strs is an array of strings; non-strings are skipped.
func (n *node) strs() []string {
	if n == nil || n.kind != 'a' {
		return nil
	}
	out := []string{}
	for _, it := range n.items {
		if it.kind == 's' && len(it.raw) > 0 && it.raw[0] == '"' {
			out = append(out, it.str())
		}
	}
	return out
}

// strMap is an object of string values.
func (n *node) strMap() map[string]string {
	out := map[string]string{}
	if n == nil || n.kind != 'o' {
		return out
	}
	for _, m := range n.members {
		if m.val.kind == 's' && len(m.val.raw) > 0 && m.val.raw[0] == '"' {
			out[m.key] = m.val.str()
		}
	}
	return out
}

// parseNode parses JSON text. A syntax error reports its line and column.
func parseNode(data []byte) (*node, *parseError) {
	var probe any
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, locate(data, err)
	}
	p := &nodeParser{data: data}
	p.skipSpace()
	n := p.value()
	return n, nil
}

// parseError is where in a file the JSON stops being valid.
type parseError struct {
	Line, Column int
	Message      string
}

func (e *parseError) Error() string { return e.Message }

func locate(data []byte, err error) *parseError {
	off := int64(len(data))
	var se *json.SyntaxError
	var ue *json.UnmarshalTypeError
	switch {
	case errors.As(err, &se):
		off = se.Offset
	case errors.As(err, &ue):
		off = ue.Offset
	}
	if off > int64(len(data)) {
		off = int64(len(data))
	}
	if off < 0 {
		off = 0
	}
	// Offset counts the bytes read up to and including the bad one.
	if off > 0 {
		off--
	}
	prefix := data[:off]
	line := bytes.Count(prefix, []byte("\n")) + 1
	col := int(off) - (bytes.LastIndexByte(prefix, '\n') + 1) + 1
	return &parseError{Line: line, Column: col, Message: err.Error()}
}

type nodeParser struct {
	data []byte
	pos  int
}

func (p *nodeParser) skipSpace() {
	for p.pos < len(p.data) && strings.IndexByte(" \t\r\n", p.data[p.pos]) >= 0 {
		p.pos++
	}
}

// value parses the next value; the input is already known valid.
func (p *nodeParser) value() *node {
	switch p.data[p.pos] {
	case '{':
		p.pos++
		n := newObject()
		p.skipSpace()
		if p.data[p.pos] == '}' {
			p.pos++
			return n
		}
		for {
			p.skipSpace()
			key := p.scalar()
			p.skipSpace()
			p.pos++ // :
			p.skipSpace()
			v := p.value()
			var k string
			_ = json.Unmarshal(key.raw, &k)
			n.members = append(n.members, &member{key: k, val: v})
			p.skipSpace()
			c := p.data[p.pos]
			p.pos++
			if c == '}' {
				return n
			}
		}
	case '[':
		p.pos++
		n := newArray()
		p.skipSpace()
		if p.data[p.pos] == ']' {
			p.pos++
			return n
		}
		for {
			p.skipSpace()
			n.items = append(n.items, p.value())
			p.skipSpace()
			c := p.data[p.pos]
			p.pos++
			if c == ']' {
				return n
			}
		}
	}
	return p.scalar()
}

func (p *nodeParser) scalar() *node {
	start := p.pos
	if p.data[p.pos] == '"' {
		p.pos++
		for p.data[p.pos] != '"' {
			if p.data[p.pos] == '\\' {
				p.pos++
			}
			p.pos++
		}
		p.pos++
	} else {
		for p.pos < len(p.data) && strings.IndexByte(",]} \t\r\n", p.data[p.pos]) < 0 {
			p.pos++
		}
	}
	return &node{kind: 's', raw: p.data[start:p.pos]}
}

// encode writes the node with two-space indentation, one member or item per
// line. The text has no trailing newline.
func (n *node) encode() []byte {
	var b bytes.Buffer
	n.write(&b, 0)
	return b.Bytes()
}

func (n *node) write(b *bytes.Buffer, depth int) {
	pad := strings.Repeat("  ", depth+1)
	switch n.kind {
	case 'o':
		if len(n.members) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		for i, m := range n.members {
			fmt.Fprintf(b, "%s%s: ", pad, marshal(m.key))
			m.val.write(b, depth+1)
			if i < len(n.members)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(strings.Repeat("  ", depth) + "}")
	case 'a':
		if len(n.items) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, it := range n.items {
			b.WriteString(pad)
			it.write(b, depth+1)
			if i < len(n.items)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(strings.Repeat("  ", depth) + "]")
	default:
		b.Write(n.raw)
	}
}

// clone is a deep copy: the copy can change without touching the original.
func (n *node) clone() *node {
	if n == nil {
		return nil
	}
	c := &node{kind: n.kind, raw: n.raw}
	for _, m := range n.members {
		c.members = append(c.members, &member{key: m.key, val: m.val.clone()})
	}
	for _, it := range n.items {
		c.items = append(c.items, it.clone())
	}
	return c
}
