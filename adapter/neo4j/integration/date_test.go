//go:build neo4j_integration

package integration

import (
	"context"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	n4j "github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/schema"
)

// The calendar date a Date-constrained value reaches the server as, for the
// cases only a server settles.
//
// dbtype.Date carries a whole time.Time and the driver derives its day number
// by an integer division that truncates toward zero, so a pre-1970 instant with
// a time of day arrived a day late. No unit test could see it: the conversion
// is the driver's, not the adapter's.
func TestCoerceDate_ServerStoresTheWallClockDate(t *testing.T) {
	ctx := context.Background()
	d := driver(t)

	s, res := schema.LoadString(ctx,
		"schema \"dt\"\n\ntype Ev {\n\tid String primary\n\ton_date Date\n}\n", "dt.yammm")
	if s == nil {
		t.Fatalf("load schema: %s", res)
	}
	typ, ok := s.Type("Ev")
	if !ok {
		t.Fatal("no type Ev")
	}
	prop, ok := typ.Property("on_date")
	if !ok {
		t.Fatal("no property on_date")
	}

	for _, tc := range []struct {
		name string
		in   time.Time
		want string
	}{
		{"pre-1970 with a time of day", time.Date(1969, 7, 20, 18, 0, 0, 0, time.UTC), "1969-07-20"},
		{"pre-1970 at midnight", time.Date(1969, 7, 20, 0, 0, 0, 0, time.UTC), "1969-07-20"},
		{"the epoch itself", time.Date(1970, 1, 1, 13, 0, 0, 0, time.UTC), "1970-01-01"},
		{"post-1970 with a time of day", time.Date(2001, 7, 20, 18, 0, 0, 0, time.UTC), "2001-07-20"},
	} {
		v, err := n4j.Coerce(prop.Constraint(), tc.in)
		if err != nil {
			t.Errorf("%s: Coerce: %v", tc.name, err)
			continue
		}
		out, err := neo4jdriver.ExecuteQuery(ctx, d, "RETURN toString($v) AS stored",
			map[string]any{"v": v}, neo4jdriver.EagerResultTransformer)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		got, _ := out.Records[0].AsMap()["stored"].(string)
		if got != tc.want {
			t.Errorf("%s: %s stored as %s, want %s", tc.name, tc.in.Format(time.RFC3339), got, tc.want)
		}
	}
}
