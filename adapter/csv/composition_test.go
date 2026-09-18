package csv

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// Account sorts before Order, so a refusal that looked at the first type alone,
// or requested a writer before looking at every type, would show.
const compositionSchema = `schema "orders"

type Account {
	account_id String primary
}

type Order {
	order_id String primary
	note String
	tags List<String>
	*-> LINES (many) Line
	*-> NOTES (many) Remark
}

part type Line {
	sku String required
}

part type Remark {
	text String required
}
`

// compositionSnapshot builds the named types' instances from raw objects,
// composed children included, through the validator and graph.Add as an import
// does.
func compositionSnapshot(t *testing.T, byType map[string][]map[string]any) (*graph.Snapshot, *schema.Schema) {
	t.Helper()
	s, result := schema.LoadString(context.Background(), compositionSchema, "orders.yammm")
	if result.HasErrors() {
		t.Fatalf("load schema: %s", result)
	}
	v := instance.NewValidator(s)
	g := graph.New(s)
	for typeName, objects := range byType {
		for _, props := range objects {
			valid, res := v.ValidateOne(context.Background(), typeName, instance.RawInstance{Properties: props})
			if !res.OK() {
				t.Fatalf("validate %s %v: %s", typeName, props, res)
			}
			if err := g.Add(context.Background(), valid).Err(); err != nil {
				t.Fatalf("graph.Add: %v", err)
			}
		}
	}
	return g.Snapshot(), s
}

func lines(skus ...string) []any {
	out := make([]any, len(skus))
	for i, sku := range skus {
		out[i] = map[string]any{"sku": sku}
	}
	return out
}

// A CSV row is flat, so a composed child has no column. Both writers refuse
// such a snapshot, naming the first offender in type order, then key order,
// then composition order, before any output: WriteSnapshot requests no writer,
// not even Account's, which sorts first and holds no child.
func TestSnapshotWriters_RefuseAComposedChild(t *testing.T) {
	t.Parallel()
	snap, _ := compositionSnapshot(t, map[string][]map[string]any{
		"Account": {{"account_id": "a1"}},
		"Order": {
			{"order_id": "o1"},
			{"order_id": "o2", "lines": lines("a", "b"), "notes": []any{map[string]any{"text": "x"}}},
			{"order_id": "o3", "lines": lines("c")},
		},
	})
	requireRefusal := func(t *testing.T, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("a snapshot holding composed children was written")
		}
		for _, want := range []string{`type "Order"`, `"o2"`, `composition "LINES"`, "2 composed children"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal %q does not name %s", err, want)
			}
		}
	}

	t.Run("MarshalSnapshot", func(t *testing.T) {
		t.Parallel()
		out, err := New().MarshalSnapshot(context.Background(), snap)
		requireRefusal(t, err)
		if out != nil {
			t.Errorf("a refused export returned output: %q", out)
		}
	})

	t.Run("WriteSnapshot requests no writer", func(t *testing.T) {
		t.Parallel()
		var requested []string
		err := New().WriteSnapshot(context.Background(), func(typeName string) (io.Writer, error) {
			requested = append(requested, typeName)
			return io.Discard, nil
		}, snap)
		requireRefusal(t, err)
		if len(requested) != 0 {
			t.Errorf("the refusal came after writers were requested for %v; a refused export writes nothing", requested)
		}
	})
}

// The refusal comes before any row is rendered, so it is the error a snapshot
// gets even where rendering would refuse too: o1's tags hold one empty element,
// which the writer refuses on its own. Its text is pinned whole, one child being
// the singular case.
func TestMarshalSnapshot_TheCompositionRefusalPrecedesRendering(t *testing.T) {
	t.Parallel()
	snap, _ := compositionSnapshot(t, map[string][]map[string]any{
		"Order": {{"order_id": "o1", "tags": []any{""}, "lines": lines("a")}},
	})

	_, err := New().MarshalSnapshot(context.Background(), snap)
	const want = `csv adapter: type "Order" instance ["o1"]: composition "LINES" holds a composed child, which a CSV row has no column for, so the export would drop it`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v\nwant    %s", err, want)
	}
}

// A composition with no children loses nothing, so the snapshot is written, and
// the file reads back and validates as the instances it came from.
func TestMarshalSnapshot_ACompositionWithNoChildrenIsWritten(t *testing.T) {
	t.Parallel()
	snap, s := compositionSnapshot(t, map[string][]map[string]any{
		"Order": {{"order_id": "o1", "note": "first"}, {"order_id": "o2"}},
	})

	out, err := New().MarshalSnapshot(context.Background(), snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	orderType, _ := s.Type("Order")
	raws, result := New().ParseTyped(context.Background(),
		location.MustNewSourceID("test://Order.csv"), "Order", strings.NewReader(string(out["Order"])), orderType)
	if !result.OK() {
		t.Fatalf("re-parse: %s", result)
	}
	valids, vres := instance.NewValidator(s).Validate(context.Background(), "Order", raws)
	if !vres.OK() || len(valids) != 2 {
		t.Fatalf("re-validate: %d valid, %s", len(valids), vres)
	}
}
