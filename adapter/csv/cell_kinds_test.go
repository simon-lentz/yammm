package csv

import (
	"bytes"
	stdcsv "encoding/csv"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/schema"
)

// refused is what a row or a cell states for a value the writer refuses.
const refused = "\x00refused"

// cellColumns are the constraint kinds a property can have, each as the
// declaration of a property p.
var cellColumns = []struct{ kind, declares string }{
	{"String", "p String"},
	{"Integer", "p Integer"},
	{"Float", "p Float"},
	{"Boolean", "p Boolean"},
	{"Timestamp", "p Timestamp"},
	{"Date", "p Date"},
	{"UUID", "p UUID"},
	{"Enum", `p Enum["a", "b"]`},
	{"Pattern", `p Pattern["^t"]`},
	{"Vector", "p Vector[2]"},
	{"List", "p List<String>"},
	{"Alias", "p Count"},
}

// cellKinds is the table of what a document holds at a cell and what the writer
// does with it. A row states the cell for every column, and under names the
// columns whose kind reads the value and so stores it in its own form. The rule
// the expectations follow is what a reader of the cell gets back: text, a boolean
// and a finite number have a spelling the parser reads; a null is the empty cell;
// a list is its elements on the separator; a value of the column's own kind is
// written in the form the column stores; and nothing else has a spelling.
var cellKinds = []struct {
	name  string
	value any
	want  string
	under map[string]string
}{
	{"a bool", true, "true", nil},

	// A Float stores an integer as a float.
	{"an int64", int64(-64), "-64", map[string]string{"Float": "-64.0"}},

	// The reader holds an integer literal past int64 as its nearest float64, and a
	// whole float keeps its point under every column, an Integer included.
	{"an integer literal past int64", json.Number("9223372036854775808"), "9223372036854776000.0", nil},
	{"a float64", 1.5, "1.5", nil},
	{"a whole float64", 6.0, "6.0", nil},
	{"a negative zero", math.Copysign(0, -1), "-0.0", nil},

	{"a string", "text", "text", nil},
	{"an empty string", "", "", nil},
	{"a string holding the separator", "a|b", "a|b", nil},
	{"a string holding a CR LF", "a\r\nb", refused, nil},
	{"a timestamp string", "2026-01-02T03:04:05+00:00", "2026-01-02T03:04:05+00:00", map[string]string{"Timestamp": "2026-01-02T03:04:05Z"}},
	{"a UUID string", "6BA7B810-9DAD-11D1-80B4-00C04FD430C8", "6BA7B810-9DAD-11D1-80B4-00C04FD430C8", map[string]string{"UUID": "6ba7b810-9dad-11d1-80b4-00c04fd430c8"}},

	// A number literal: the number its text spells, or no number.
	{"a json.Number integer", json.Number("12"), "12", map[string]string{"Float": "12.0"}},
	{"a json.Number float", json.Number("1.5"), "1.5", nil},
	{"a json.Number no float64 holds", json.Number("1e400"), refused, nil},

	{"a string-keyed map", map[string]any{"a": int64(1)}, refused, nil},
	{"nil", nil, "", nil},

	// A list, each element by the rules above under the column's element kind,
	// which a Vector's is Float.
	{"a list of strings", []any{"a", "b"}, "a|b", nil},
	{"a list holding the separator", []any{"a|b", `c\d`}, `a\|b|c\\d`, nil},
	{"a list of mixed scalars", []any{int64(1), "x", true, 2.5}, "1|x|true|2.5", map[string]string{"Vector": "1.0|x|true|2.5"}},
	{"a list of floats", []any{1.0, 2.5}, "1.0|2.5", nil},
	{"a nested list", []any{[]any{int64(1), int64(2)}, []any{int64(3)}}, `1\|2|3`, nil},
	{"an empty list", []any{}, "", nil},
	{"a list holding a nil", []any{"a", nil}, refused, nil},
	{"a list of one empty string", []any{""}, refused, nil},
	{"a list of one empty list", []any{[]any{}}, refused, nil},
	{"a list holding a number no float64 holds", []any{1.5, json.Number("1e400")}, refused, nil},
	{"a list holding a map", []any{"a", map[string]any{"k": "v"}}, refused, nil},
}

// cellDepartures are the cells where the writer does not do what the table
// states, each with what it does instead. The test fails when a departure
// stops being one, so the list cannot outlive its repair. The column "*" is
// every column the row does not name under.
var cellDepartures = map[[2]string]string{}

func cellWant(row int, column string) (stated, held string) {
	r := cellKinds[row]
	stated = r.want
	if w, ok := r.under[column]; ok {
		stated = w
	}
	held = stated
	if d, ok := cellDepartures[[2]string{r.name, column}]; ok {
		held = d
	} else if d, ok := cellDepartures[[2]string{r.name, "*"}]; ok {
		held = d
	}
	return stated, held
}

// One table holds what the writer does with every kind of value a document can
// hold at a cell, under every constraint kind: the cell's exact text, or a
// refusal that matches [ErrUnrepresentable].
func TestMarshalSnapshot_EveryKindUnderEveryConstraint(t *testing.T) {
	t.Parallel()
	for _, column := range cellColumns {
		t.Run(column.kind, func(t *testing.T) {
			t.Parallel()
			s, res := schema.LoadString(t.Context(), "schema \"cells\"\n\ntype Count = Integer\n\ntype Holder {\n\tid String primary\n\t"+column.declares+"\n}\n", "cells.yammm")
			if res.HasErrors() {
				t.Fatalf("load schema: %s", res)
			}
			for i, row := range cellKinds {
				stated, held := cellWant(i, column.kind)
				snap := snappartstest.Loaded(t, s, snapparts.Parts{Instances: []snapparts.Instance{
					heldRoot(t, s, "Holder", "h1", map[string]any{"id": "h1", "p": row.value}),
				}})
				got, err := writtenCell(t, s, snap)
				switch {
				case err != nil && !errors.Is(err, ErrUnrepresentable):
					t.Errorf("%s: the writer failed outside the class: %v", row.name, err)
					continue
				case err != nil:
					got = refused
				}
				if got != held {
					t.Errorf("%s: %s, want %s", row.name, cellAnswer(got), cellAnswer(held))
				}
				if stated != held && got == stated {
					t.Errorf("%s: the writer now does what the table states (%s): remove the departure", row.name, cellAnswer(stated))
				}
			}
		})
	}
	for key := range cellDepartures {
		found := false
		for _, row := range cellKinds {
			found = found || row.name == key[0]
		}
		if !found {
			t.Errorf("a departure names %q, which is no row", key[0])
		}
	}
}

func cellAnswer(s string) string {
	if s == refused {
		return "refused"
	}
	return "written as " + string(mustJSON(s))
}

func mustJSON(s string) []byte {
	b, err := json.Marshal(s)
	if err != nil {
		return []byte(err.Error())
	}
	return b
}

// writtenCell writes snap and returns the text of Holder h1's p cell, as
// encoding/csv reads the file.
func writtenCell(t *testing.T, s schema.Schema, snap graph.Snapshot) (string, error) {
	t.Helper()
	files, err := New(WithSchema(s)).MarshalSnapshot(t.Context(), snap)
	if err != nil {
		return "", err
	}
	records, rerr := stdcsv.NewReader(bytes.NewReader(files["Holder"])).ReadAll()
	if rerr != nil || len(records) != 2 || len(records[0]) != 2 || records[0][1] != "p" {
		t.Fatalf("the file is not a header and one row of id and p: %q (%v)", files["Holder"], rerr)
	}
	return records[1][1], nil
}
