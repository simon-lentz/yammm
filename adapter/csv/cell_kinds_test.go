package csv

import (
	"bytes"
	stdcsv "encoding/csv"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"
	"unsafe"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/instancetest"
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

type (
	cellBool   bool
	cellInt    int
	cellUint   uint16
	cellFloat  float64
	cellString string
	cellStruct struct{ X int }
)

// cellKinds is the table of what a cell is handed and what the writer does
// with it. A row states the cell for every column, and under names the columns
// whose kind reads the value and so stores it in its own form. The rule the
// expectations follow is what a reader of the cell gets back: text, a boolean
// and a finite number have a spelling the parser reads; a nil is the empty
// cell; a list is its elements on the separator; a value of the column's own
// kind is written in the form the column stores; and nothing else has a
// spelling.
var cellKinds = []struct {
	name  string
	value any
	want  string
	under map[string]string
}{
	// reflect.Bool
	{"a bool", true, "true", nil},
	{"a named bool", cellBool(false), "false", nil},

	// reflect.Int to reflect.Int64: a Float stores an integer as a float.
	{"an int", int(42), "42", map[string]string{"Float": "42.0"}},
	{"an int8", int8(-8), "-8", map[string]string{"Float": "-8.0"}},
	{"an int16", int16(16), "16", map[string]string{"Float": "16.0"}},
	{"an int32", int32(32), "32", map[string]string{"Float": "32.0"}},
	{"an int64", int64(-64), "-64", map[string]string{"Float": "-64.0"}},
	{"a named int", cellInt(7), "7", map[string]string{"Float": "7.0"}},

	// reflect.Uint to reflect.Uintptr
	{"a uint", uint(1), "1", map[string]string{"Float": "1.0"}},
	{"a uint8", uint8(8), "8", map[string]string{"Float": "8.0"}},
	{"a uint16", uint16(16), "16", map[string]string{"Float": "16.0"}},
	{"a uint32", uint32(32), "32", map[string]string{"Float": "32.0"}},
	{"a uint64 past int64", uint64(1 << 63), "9223372036854775808", map[string]string{"Float": "9223372036854776000.0"}},
	{"a named uint", cellUint(9), "9", map[string]string{"Float": "9.0"}},
	{"a uintptr", uintptr(7), "7", map[string]string{"Float": "7.0"}},

	// reflect.Float32 and reflect.Float64: a whole float keeps its point
	// under every column, an Integer included.
	{"a float64", 1.5, "1.5", nil},
	{"a whole float64", 6.0, "6.0", nil},
	{"a negative zero", math.Copysign(0, -1), "-0.0", nil},
	{"a float32", float32(0.25), "0.25", nil},
	{"a named float", cellFloat(2.5), "2.5", nil},
	{"a NaN", math.NaN(), refused, nil},
	{"an infinity", math.Inf(1), refused, nil},
	{"a float32 infinity", float32(math.Inf(-1)), refused, nil},

	// reflect.Complex64 and reflect.Complex128
	{"a complex64", complex64(1), refused, nil},
	{"a complex128", complex(1, 2), refused, nil},

	// reflect.String
	{"a string", "text", "text", nil},
	{"an empty string", "", "", nil},
	{"a named string", cellString("text"), "text", nil},
	{"a string holding the separator", "a|b", "a|b", nil},
	{"a string that is not valid UTF-8", "a\xffb", refused, nil},
	{"a string holding a CR LF", "a\r\nb", refused, nil},
	{"a timestamp string", "2026-01-02T03:04:05+00:00", "2026-01-02T03:04:05+00:00", map[string]string{"Timestamp": "2026-01-02T03:04:05Z"}},
	{"a UUID string", "6BA7B810-9DAD-11D1-80B4-00C04FD430C8", "6BA7B810-9DAD-11D1-80B4-00C04FD430C8", map[string]string{"UUID": "6ba7b810-9dad-11d1-80b4-00c04fd430c8"}},

	// json.Number: the number its text spells, or no number.
	{"a json.Number integer", json.Number("12"), "12", map[string]string{"Float": "12.0"}},
	{"a json.Number float", json.Number("1.5"), "1.5", nil},
	{"a json.Number no float64 holds", json.Number("1e400"), refused, nil},
	{"a json.Number that spells no number", json.Number("abc"), refused, nil},
	{"an empty json.Number", json.Number(""), refused, nil},

	// reflect.Struct, and the two values a column of their kind stores as text.
	{"a struct", cellStruct{1}, refused, nil},
	{"a time.Time", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), refused, map[string]string{"Timestamp": "2026-01-02T03:04:05Z", "Date": "2026-01-02"}},
	{"a time.Time past year 9999", time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), refused, nil},

	// reflect.Array
	{"an array", [2]int{1, 2}, refused, nil},
	{"a uuid.UUID", uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8"), refused, map[string]string{"UUID": "6ba7b810-9dad-11d1-80b4-00c04fd430c8"}},

	// reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer
	{"a string-keyed map", map[string]any{"a": 1}, refused, nil},
	{"a map keyed by an integer", map[int]int{1: 2}, refused, nil},
	{"a channel", make(chan int), refused, nil},
	{"a function", func() {}, refused, nil},
	{"an unsafe.Pointer", unsafe.Pointer(new(int)), refused, nil},

	// reflect.Pointer: a pointer is read as the scalar it points to, under every
	// column, as the one form every writer writes reads it.
	{"a pointer to an int", new(5), "5", map[string]string{"Float": "5.0"}},
	{"a pointer to a string", new("text"), "text", nil},
	{"a pointer to a time.Time", new(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)), refused, map[string]string{"Timestamp": "2026-01-02T03:04:05Z", "Date": "2026-01-02"}},
	{"a pointer to a time.Time past year 9999", new(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)), refused, nil},

	// nil, and a typed nil of each nilable kind: the empty cell, which reads
	// back as null.
	{"nil", nil, "", nil},
	{"a nil pointer", (*int)(nil), "", nil},
	{"a nil map", map[string]any(nil), "", nil},
	{"a nil map keyed by an integer", map[int]int(nil), "", nil},
	{"a nil slice", []any(nil), "", nil},
	{"a nil channel", (chan int)(nil), "", nil},
	{"a nil function", (func())(nil), "", nil},
	{"a nil unsafe.Pointer", unsafe.Pointer(nil), "", nil},

	// reflect.Slice: a list, each element by the rules above under the
	// column's element kind, which a Vector's is Float.
	{"a list of strings", []any{"a", "b"}, "a|b", nil},
	{"a []string holding the separator", []string{"a|b", `c\d`}, `a\|b|c\\d`, nil},
	{"a list of mixed scalars", []any{1, "x", true, 2.5}, "1|x|true|2.5", map[string]string{"Vector": "1.0|x|true|2.5"}},
	{"a list holding a uintptr", []any{uintptr(7), "x"}, "7|x", map[string]string{"Vector": "7.0|x"}},
	{"a []byte", []byte("ab"), "97|98", map[string]string{"Vector": "97.0|98.0"}},
	{"a []float64", []float64{1, 2.5}, "1.0|2.5", nil},
	{"a nested list", []any{[]any{1, 2}, []any{3}}, `1\|2|3`, nil},
	{"an empty list", []any{}, "", nil},
	{"a list holding a nil", []any{"a", nil}, refused, nil},
	{"a list of one empty string", []any{""}, refused, nil},
	{"a list of one empty list", []any{[]any{}}, refused, nil},
	{"a list holding a NaN", []any{1.5, math.NaN()}, refused, nil},
	{"a list holding a struct", []any{"a", cellStruct{1}}, refused, nil},
	{"a list holding a string that is not valid UTF-8", []any{"a", "\xff"}, refused, nil},
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

// One table holds what the writer does with every Go kind a cell can be
// handed, under every constraint kind: the cell's exact text, or a refusal
// that matches [ErrUnrepresentable].
func TestMarshalSnapshot_EveryKindUnderEveryConstraint(t *testing.T) {
	t.Parallel()
	for _, column := range cellColumns {
		t.Run(column.kind, func(t *testing.T) {
			t.Parallel()
			s, res := schema.LoadString(t.Context(), "schema \"cells\"\n\ntype Count = Integer\n\ntype Holder {\n\tid String primary\n\t"+column.declares+"\n}\n", "cells.yammm")
			if res.HasErrors() {
				t.Fatalf("load schema: %s", res)
			}
			holder, _ := s.Type("Holder")
			for i, row := range cellKinds {
				stated, held := cellWant(i, column.kind)
				g := graph.New(s)
				if r := g.Add(t.Context(), instancetest.VI("Holder", instancetest.TypeID(holder.ID()), instancetest.PK("h1"),
					instancetest.Props(map[string]any{"id": "h1", "p": row.value}))); !r.OK() {
					t.Errorf("%s: add: %s", row.name, r)
					continue
				}
				got, err := writtenCell(t, s, g.Snapshot())
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
func writtenCell(t *testing.T, s *schema.Schema, snap *graph.Snapshot) (string, error) {
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
