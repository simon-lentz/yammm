package constraintof_test

import (
	"testing"

	"github.com/simon-lentz/yammm/adapter/internal/constraintof"
	"github.com/simon-lentz/yammm/schema"
)

func loadType(t *testing.T) *schema.Type {
	t.Helper()
	s, res := schema.LoadString(t.Context(), `schema "c"

type Ratio = Float

type Grid {
	id String primary
	weight Ratio
	tags List<String>
	rows List<List<Integer>>
	at Vector[2]
}
`, "c.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	typ, _ := s.Type("Grid")
	return typ
}

func TestProperty(t *testing.T) {
	t.Parallel()
	typ := loadType(t)
	if c := constraintof.Property(typ, "weight"); c == nil || schema.ResolveAlias(c).Kind() != schema.KindFloat {
		t.Errorf("weight: got %v, want the aliased Float", c)
	}
	if c := constraintof.Property(typ, "undeclared"); c != nil {
		t.Errorf("an undeclared name: got %v, want nil", c)
	}
	if c := constraintof.Property(nil, "weight"); c != nil {
		t.Errorf("a nil type: got %v, want nil", c)
	}
}

func TestElement(t *testing.T) {
	t.Parallel()
	typ := loadType(t)
	kindOf := func(name string) (schema.ConstraintKind, bool) {
		c := constraintof.Element(constraintof.Property(typ, name))
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
		{"weight", 0, false},
		{"id", 0, false},
	} {
		got, has := kindOf(c.prop)
		if has != c.has || (has && got != c.want) {
			t.Errorf("%s: got (%v, %v), want (%v, %v)", c.prop, got, has, c.want, c.has)
		}
	}
	if constraintof.Element(nil) != nil {
		t.Error("a nil constraint has an element constraint")
	}
}
