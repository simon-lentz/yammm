package csv

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

const refusedCellSchema = `schema "r"
type Company { cid String primary
	d Date primary }
type Person {
	id String primary
	born Date required
	at Timestamp
	n Integer
	ok Boolean
	--> WORKS_AT (_:one) Company { since Integer required }
	--> KNOWS (_:many) Company { w Integer }
}
`

// A cell that does not coerce draws its E_CSV_COERCE alone: the raw instance
// holds an [instance.Refused] there, the validator reports nothing for it and
// refuses the row, at a property of each kind, an edge property and a key
// component, of a (one) and a (many) association.
func TestParseTyped_ACellThatDoesNotCoerceDrawsOneIssue(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), refusedCellSchema, "r.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	person, _ := s.Type("Person")
	const base = "id,born,works_at._target_cid,works_at._target_d,works_at.since"
	const good = "p1,2020-01-01,c1,2020-01-01,5"
	for name, c := range map[string]struct{ header, row string }{
		"a Date property":                  {base, "p1,xyz,c1,2020-01-01,5"},
		"a Date property of invalid bytes": {base, "p1,\xff\xfe,c1,2020-01-01,5"},
		"a Timestamp property":             {base + ",at", good + ",xyz"},
		"an Integer property":              {base + ",n", good + ",abc"},
		"a Boolean property":               {base + ",ok", good + ",maybe"},
		"a required edge property":         {base, "p1,2020-01-01,c1,2020-01-01,soon"},
		"a key component":                  {base, "p1,2020-01-01,c1,notadate,5"},
		"an edge property of a many edge":  {base + ",knows._target_cid,knows._target_d,knows.w", good + ",c1|c2,2020-01-01|2020-01-02,1|x"},
		"a key component of a many edge":   {base + ",knows._target_cid,knows._target_d", good + ",c1|c2,2020-01-01|bad"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			raws, pres := New(WithSchema(s)).ParseTyped(t.Context(), location.NewSourceID("p.csv"), "Person",
				strings.NewReader(c.header+"\n"+c.row+"\n"), person)
			if len(raws) != 1 || pres.Len() != 1 {
				t.Fatalf("parse: %d rows, %s", len(raws), pres)
			}
			for issue := range pres.Issues() {
				if issue.Code() != E_CSV_COERCE {
					t.Fatalf("parse drew %s, want one E_CSV_COERCE", pres)
				}
			}
			valid, vres := instance.NewValidator(s).ValidateOne(t.Context(), "Person", raws[0])
			if valid != nil || vres.Len() != 0 {
				t.Errorf("validate: instance %v, %s; want a refused row and no issue", valid, vres)
			}
		})
	}
}

// A refused cell's row is refused whatever else it holds, and the validator
// still reports every other property's break: the refused cell gates the
// passes after the property pass as a property error does, so the edge's
// partial key draws nothing.
func TestParseTyped_ARefusedCellGatesAsAPropertyError(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), refusedCellSchema, "r.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	person, _ := s.Type("Person")
	raws, pres := New(WithSchema(s)).ParseTyped(t.Context(), location.NewSourceID("p.csv"), "Person",
		strings.NewReader("id,n,works_at._target_cid\np1,abc,c1\n"), person)
	if len(raws) != 1 || pres.Len() != 1 || countCode(pres, E_CSV_COERCE) != 1 {
		t.Fatalf("parse: %d rows, %s", len(raws), pres)
	}
	valid, vres := instance.NewValidator(s).ValidateOne(t.Context(), "Person", raws[0])
	if valid != nil || vres.Len() != 1 || countCode(vres, instance.ErrMissingRequired) != 1 {
		t.Errorf("validate: instance %v, %s; want the missing born property alone", valid, vres)
	}
}

func countCode(res diag.Result, code diag.Code) int {
	n := 0
	for issue := range res.Issues() {
		if issue.Code() == code {
			n++
		}
	}
	return n
}
