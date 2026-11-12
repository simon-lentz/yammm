package gogen

import (
	"bytes"
	"fmt"
)

// dateGoName is the emitted Date type. It is in reservedNames rather than
// reserved on demand, so the name is taken before any schema entity is
// assigned and cannot depend on whether a Date position exists.
const dateGoName = "Date"

// dateCodecHelpers is the two unexported functions every Date codec delegates
// to. Unexported names cannot collide with schema-derived identifiers, which
// are always exported, so they need no reservation.
const dateCodecHelpers = `// marshalDate renders t as a JSON string in the form YYYY-MM-DD.
func marshalDate(t time.Time) ([]byte, error) {
	return json.Marshal(t.Format(time.DateOnly))
}

// unmarshalDate parses a JSON string in the form YYYY-MM-DD into *dst. A JSON
// null leaves *dst unchanged, as time.Time's own UnmarshalJSON does.
func unmarshalDate(b []byte, dst *time.Time) error {
	if string(b) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return err
	}
	*dst = t
	return nil
}

`

// writeDateDecl writes a struct embedding time.Time and a JSON codec pair
// exchanging the value as YYYY-MM-DD, the text the validator reads. Embedding
// keeps time.Time's methods promoted, so a .Format caller compiles unchanged.
func writeDateDecl(w *bytes.Buffer, name string) {
	fmt.Fprintf(w, "// %s is exchanged as a JSON string in the form YYYY-MM-DD.\n", name)
	fmt.Fprintf(w, "type %s struct{ time.Time }\n\n", name)
	fmt.Fprintf(w, "func (v %s) MarshalJSON() ([]byte, error) { return marshalDate(v.Time) }\n\n", name)
	fmt.Fprintf(w, "func (v *%s) UnmarshalJSON(b []byte) error { return unmarshalDate(b, &v.Time) }\n\n", name)
}

// emitTimestampDecl writes a struct embedding time.Time and nothing else: the
// promoted codec exchanges RFC 3339 with nanoseconds, the text a Timestamp is
// stored as.
func (g *generator) emitTimestampDecl(name string) {
	g.needsTime = true
	fmt.Fprintf(g.buf, "type %s struct{ time.Time }\n\n", name)
}
