package instancetest_test

import (
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/instancetest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

// TestVI_Defaults pins the builder's zero-option contract: primary key [1],
// no properties, and nil edges, compositions, and provenance.
func TestVI_Defaults(t *testing.T) {
	vi := instancetest.VI("Person")

	if got := vi.TypeName(); got != "Person" {
		t.Errorf("TypeName() = %q, want %q", got, "Person")
	}
	if got := vi.TypeID(); got != (schema.TypeID{}) {
		t.Errorf("TypeID() = %v, want zero", got)
	}
	if got := vi.PrimaryKey().String(); got != "[1]" {
		t.Errorf("PrimaryKey() = %s, want [1]", got)
	}
	if vi.Provenance() != nil {
		t.Errorf("Provenance() = %v, want nil", vi.Provenance())
	}
	for range vi.Edges() {
		t.Error("default instance must have no edges")
	}
	for range vi.Compositions() {
		t.Error("default instance must have no compositions")
	}
}

// TestVI_Options pins that every option lands on its field.
func TestVI_Options(t *testing.T) {
	typeID := schema.NewTypeID(location.SourceID{}, "Person")
	prov := location.NewProvenance("test.json", path.Root(), location.Span{})
	edges := map[string]*instance.ValidEdgeData{
		"manager": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{int64(9)}), immutable.WrapProperties(nil)),
		}),
	}
	composed := map[string]immutable.Value{
		"addresses": immutable.Wrap([]any{map[string]any{"id": int64(1)}}),
	}

	vi := instancetest.VI(
		"Person",
		instancetest.TypeID(typeID),
		instancetest.PK("us", int64(7)),
		instancetest.Props(map[string]any{"name": "Alice"}),
		instancetest.Edges(edges),
		instancetest.Composed(composed),
		instancetest.Provenance(prov),
	)

	if got := vi.TypeID(); got != typeID {
		t.Errorf("TypeID() = %v, want %v", got, typeID)
	}
	if got := vi.PrimaryKey().String(); got != `["us",7]` {
		t.Errorf("PrimaryKey() = %s, want [\"us\",7]", got)
	}
	if name, ok := vi.Property("name"); !ok {
		t.Error("Property(name) missing")
	} else if s, _ := name.String(); s != "Alice" {
		t.Errorf("Property(name) = %q, want %q", s, "Alice")
	}
	if _, ok := vi.Edge("manager"); !ok {
		t.Error("Edge(manager) missing")
	}
	if _, ok := vi.Composed("addresses"); !ok {
		t.Error("Composed(addresses) missing")
	}
	if vi.Provenance() == nil {
		t.Error("Provenance() = nil, want set")
	}
}
