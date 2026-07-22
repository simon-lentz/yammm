package jschema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// compactWidthLimit is the layout threshold: a value whose single-line
// rendering is at most this many characters renders compact; anything wider
// expands one entry per line. Layout is a pure function of the value tree, so
// output stays byte-deterministic; changing this constant changes rendered
// output and therefore every golden file.
const compactWidthLimit = 60

// indentStep is one level of expanded-form indentation.
const indentStep = "  "

type valKind uint8

const (
	kindObject valKind = iota
	kindArray
	kindRaw
)

// val is one JSON value in an emitted document. Objects preserve insertion
// order: key order is part of the output contract (human-readable goldens,
// stable diffs), which is why this exists instead of map[string]any (whose
// encoding/json rendering alphabetizes keys).
type val struct {
	kind valKind
	obj  []kv
	arr  []val
	raw  json.RawMessage
}

// kv is one ordered object member.
type kv struct {
	K string
	V val
}

func object(pairs ...kv) val { return val{kind: kindObject, obj: pairs} }

func array(vs ...val) val { return val{kind: kindArray, arr: vs} }

// scalar renders x as a JSON scalar with HTML escaping disabled, so text like
// "endDate > startDate" survives into the output verbatim rather than as
// > escapes. Inputs are generator-controlled (strings, int64s, float64s,
// bools); anything unmarshalable is a generator bug and panics rather than
// producing broken output.
func scalar(x any) val {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(x); err != nil {
		panic(fmt.Sprintf("jschema: unmarshalable scalar %T: %v", x, err))
	}
	// Encoder.Encode appends a newline; the raw fragment must be bare.
	return val{kind: kindRaw, raw: json.RawMessage(strings.TrimSuffix(sb.String(), "\n"))}
}

// raw wraps a pre-rendered JSON fragment verbatim. The fragment must be a
// single-line value: it participates in compact-width measurement as-is.
func raw(j json.RawMessage) val { return val{kind: kindRaw, raw: j} }

// compact returns v's single-line rendering: `{ "k": v, ... }` for objects,
// `[v, ...]` for arrays, and the raw fragment for scalars. Empty containers
// render as {} and []. Recomputed per render decision; document trees are
// small (one per schema type/relation), so the quadratic worst case is
// irrelevant in practice.
func (v val) compact() string {
	switch v.kind {
	case kindObject:
		if len(v.obj) == 0 {
			return "{}"
		}
		var sb strings.Builder
		sb.WriteString("{ ")
		for i, m := range v.obj {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(quoteKey(m.K))
			sb.WriteString(": ")
			sb.WriteString(m.V.compact())
		}
		sb.WriteString(" }")
		return sb.String()
	case kindArray:
		if len(v.arr) == 0 {
			return "[]"
		}
		var sb strings.Builder
		sb.WriteByte('[')
		for i, e := range v.arr {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(e.compact())
		}
		sb.WriteByte(']')
		return sb.String()
	default:
		return string(v.raw)
	}
}

// render writes v to buf at the given indentation level: compact when the
// single-line form fits compactWidthLimit, expanded (one member per line,
// indentStep per level) otherwise. Scalars and empty containers are always
// compact by construction.
func (v val) render(buf *bytes.Buffer, indent int) {
	if c := v.compact(); len(c) <= compactWidthLimit {
		buf.WriteString(c)
		return
	}
	inner := strings.Repeat(indentStep, indent+1)
	outer := strings.Repeat(indentStep, indent)
	switch v.kind {
	case kindObject:
		buf.WriteString("{\n")
		for i, m := range v.obj {
			if i > 0 {
				buf.WriteString(",\n")
			}
			buf.WriteString(inner)
			buf.WriteString(quoteKey(m.K))
			buf.WriteString(": ")
			m.V.render(buf, indent+1)
		}
		buf.WriteString("\n")
		buf.WriteString(outer)
		buf.WriteByte('}')
	case kindArray:
		buf.WriteString("[\n")
		for i, e := range v.arr {
			if i > 0 {
				buf.WriteString(",\n")
			}
			buf.WriteString(inner)
			e.render(buf, indent+1)
		}
		buf.WriteString("\n")
		buf.WriteString(outer)
		buf.WriteByte(']')
	default:
		// A raw fragment wider than the limit still renders as-is: raw is
		// single-line by contract and has no expanded form.
		buf.Write(v.raw)
	}
}

// quoteKey renders an object key as a JSON string with the same escaping
// rules as scalar values.
func quoteKey(k string) string { return scalar(k).compact() }

// renderDocument renders v as a complete document: top-level value plus a
// single trailing newline (text files end in a newline; goldens must be
// stable under EOF-normalizing tooling).
func renderDocument(v val) []byte {
	var buf bytes.Buffer
	v.render(&buf, 0)
	buf.WriteByte('\n')
	return buf.Bytes()
}
