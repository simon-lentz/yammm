package json

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

const compositeKeySchema = `schema "compositekey"

type Region {
	country String primary
	code String primary
}

type Office {
	id String primary
	--> IN (_) Region
}
`

// An edge target with a composite primary key pairs each component name with
// its own value. The names and the values are chosen so that pairing them in
// the wrong order is visible: swapping them yields two members that both exist
// and both hold the other's value.
func TestMarshalObject_CompositeKeyEdgeTargetPairsNameToValue(t *testing.T) {
	t.Parallel()

	s, res := schema.LoadString(t.Context(), compositeKeySchema, "compositekey.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}

	v := instance.NewValidator(s)
	g := graph.New(s)
	raws := []struct {
		typeName string
		props    map[string]any
	}{
		{"Region", map[string]any{"country": "no", "code": "osl"}},
		{"Office", map[string]any{
			"id": "o1",
			"in": map[string]any{"_target_country": "no", "_target_code": "osl"},
		}},
	}
	for _, r := range raws {
		vi, vres := v.ValidateOne(t.Context(), r.typeName, instance.RawInstance{Properties: r.props})
		if vres.HasErrors() {
			t.Fatalf("validate %s: %s", r.typeName, vres.String())
		}
		if ar := g.Add(t.Context(), vi); !ar.OK() {
			t.Fatalf("add %s: %s", r.typeName, ar.String())
		}
	}

	doc, err := New().MarshalObject(t.Context(), g.Snapshot())
	if err != nil {
		t.Fatalf("MarshalObject: %v", err)
	}

	for _, want := range []string{`"_target_country":"no"`, `"_target_code":"osl"`} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("output does not hold %s\n%s", want, doc)
		}
	}
}
