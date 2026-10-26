package csv_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/adapter/csv"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

const emptyCostSchema = `schema "empty"

type Firm {
	code Date primary
}

type Person {
	id String primary
	n Integer required
	f Float required
	d Date required
	t Timestamp required
	--> KNOWS (_:many) Firm {
		weight Integer required
		day Date
	}
}
`

// An empty cell's value depends on its property alone, so a row of empty cells
// whose kinds have no empty rendering costs no more than a row of filled ones:
// the parser builds no refusal it then discards, per row or per target.
func TestParse_AnEmptyCellCostsNoMoreThanAFilledOne(t *testing.T) {
	s, res := schema.LoadString(context.Background(), emptyCostSchema, "empty.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	person, _ := s.Type("Person")
	a := csv.New(csv.WithSchema(s))
	src := location.MustNewSourceID("test://empty.csv")
	perRow := func(cells string) float64 {
		cost := func(rows int) float64 {
			var b strings.Builder
			b.WriteString("id,n,f,d,t,knows._target_code,knows.weight,knows.day\n")
			for i := range rows {
				fmt.Fprintf(&b, "p%d,%s\n", i, cells)
			}
			doc := b.String()
			return testing.AllocsPerRun(5, func() {
				_, _ = a.ParseTyped(context.Background(), src, "Person", strings.NewReader(doc), person)
			})
		}
		return (cost(200) - cost(100)) / 100
	}
	filled := perRow("1,2.5,2024-01-01,2024-01-01T00:00:00Z,2024-01-01|2024-01-02,5|6,2024-01-01|2024-01-02")
	empty := perRow(",,,,2024-01-01|2024-01-02,,")
	if empty > filled {
		t.Errorf("a row of empty cells costs %.1f allocations, a filled row %.1f", empty, filled)
	}
}
