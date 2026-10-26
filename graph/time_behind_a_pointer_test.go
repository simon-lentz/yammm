package graph_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// Graph.Add renders a time or a UUID behind a pointer or an interface as the
// validator renders the value it holds, so the graph stores the text the
// validator stores for the same input.
func TestAdd_StoresATimeOrAUUIDBehindACarrierAsItsText(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), "schema \"carriers\"\n\ntype T {\n\tid String primary\n\tat Timestamp\n\tday Date\n\tref UUID\n}\n", "carriers.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	id := uuid.MustParse("12345678-1234-1234-1234-123456789ABC")
	var heldAt any = at
	for name, props := range map[string]map[string]any{
		"behind a pointer":                 {"id": "x", "at": &at, "day": &at, "ref": &id},
		"held by an interface, behind one": {"id": "x", "at": &heldAt, "day": &heldAt},
	} {
		v := instance.NewValidator(s)
		want, vres := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: props})
		if vres.HasErrors() {
			t.Fatalf("%s: the validator refused: %s", name, vres)
		}
		g := graph.New(s)
		inst := instance.NewValidInstance("T", mustTypeID(t, s, "T"), immutable.WrapKey([]any{"x"}),
			immutable.WrapProperties(props), nil, nil, nil)
		if r := g.Add(t.Context(), inst); !r.OK() {
			t.Fatalf("%s: add: %s", name, r)
		}
		got := g.Snapshot().InstancesOf(mustTypeID(t, s, "T"))[0]
		for prop := range props {
			if prop == "id" {
				continue
			}
			stored, _ := got.Property(prop)
			validated, _ := want.Property(prop)
			text, isText := stored.Unwrap().(string)
			if !isText || text != validated.Unwrap() {
				t.Errorf("%s: %s stored %#v, the validator %#v", name, prop, stored.Unwrap(), validated.Unwrap())
			}
		}
	}
}
