package neo4j

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

func loadConstraintType(t *testing.T) schema.Type {
	t.Helper()
	s, res := schema.LoadString(t.Context(), `schema "c"

type Ratio = Float

type Ratios = List<Float>

type Grid {
	id String primary
	weight Ratio
	tags List<String>
	rows List<List<Integer>>
	at Vector[2]
	rs Ratios
}
`, "c.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	typ, _ := s.Type("Grid")
	return typ
}

func TestPropertyConstraint(t *testing.T) {
	t.Parallel()
	typ := loadConstraintType(t)
	if c := propertyConstraint(typ, "weight"); c == nil || schema.ResolveAlias(c).Kind() != schema.KindFloat {
		t.Errorf("weight: got %v, want the aliased Float", c)
	}
	if c := propertyConstraint(typ, "undeclared"); c != nil {
		t.Errorf("an undeclared name: got %v, want nil", c)
	}
	if c := propertyConstraint(schema.Type{}, "weight"); c != nil {
		t.Errorf("a nil type: got %v, want nil", c)
	}
}

func TestElementConstraint(t *testing.T) {
	t.Parallel()
	typ := loadConstraintType(t)
	kindOf := func(name string) (schema.ConstraintKind, bool) {
		c := elementConstraint(propertyConstraint(typ, name))
		if c == nil {
			return 0, false
		}
		return schema.ResolveAlias(c).Kind(), true
	}
	for _, c := range []struct {
		prop string
		want schema.ConstraintKind
		has  bool
	}{
		{"tags", schema.KindString, true},
		{"rows", schema.KindList, true},
		{"at", schema.KindFloat, true},
		{"rs", schema.KindFloat, true},
		{"weight", 0, false},
		{"id", 0, false},
	} {
		got, has := kindOf(c.prop)
		if has != c.has || (has && got != c.want) {
			t.Errorf("%s: got (%v, %v), want (%v, %v)", c.prop, got, has, c.want, c.has)
		}
	}
	if elementConstraint(nil) != nil {
		t.Error("a nil constraint has an element constraint")
	}
}
