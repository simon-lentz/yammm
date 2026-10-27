package graph_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
)

// A nil list an instance built past the validator holds stays nil at every
// List the graph canonicalizes, as at one it does not: it holds no value, and
// becomes no empty list.
func TestAdd_ANilListStaysNilWhereTheGraphCanonicalizes(t *testing.T) {
	t.Parallel()
	s := loadAs(t, `schema "p"

type T {
	id String primary
	tags List<String>
	stamps List<Timestamp>
	days List<Date>
	ids List<UUID>
}
`)
	typ, ok := s.Type("T")
	if !ok {
		t.Fatal("no type T")
	}
	lists := []string{"tags", "stamps", "days", "ids"}
	raw := map[string]any{"id": "x"}
	for _, name := range lists {
		raw[name] = []any(nil)
	}
	props := immutable.WrapProperties(raw, immutable.WithClone(true))
	g := graph.New(s)
	if res := g.Add(t.Context(), instance.NewValidInstance("T", typ.ID(), immutable.WrapKey([]any{"x"}), props, nil, nil, nil)); res.Err() != nil {
		t.Fatalf("add: %v", res.Err())
	}
	for inst := range g.Snapshot().AllInstances() {
		for _, name := range lists {
			val, ok := inst.Property(name)
			if !ok || !val.IsNil() {
				t.Errorf("%s: stored = %v, IsNil = %v; want a nil list", name, ok, val.IsNil())
			}
		}
	}
}

// A key property that holds none behind a carrier is absent, as a nil one is,
// so the graph refuses it before any writer writes a null key.
func TestAdd_AKeyPropertyThatHoldsNoneIsAbsent(t *testing.T) {
	t.Parallel()
	s := loadAs(t, `schema "p"

type T {
	id String primary
}
`)
	typ, ok := s.Type("T")
	if !ok {
		t.Fatal("no type T")
	}
	var nilString *string
	for name, none := range map[string]any{
		"a pointer to a nil pointer": &nilString,
		"a pointer to a nil slice":   new([]any),
	} {
		props := immutable.WrapProperties(map[string]any{"id": none})
		res := graph.New(s).Add(t.Context(), instance.NewValidInstance("T", typ.ID(), immutable.WrapKey([]any{none}), props, nil, nil, nil))
		if res.Err() == nil || !strings.Contains(res.String(), `primary key property "id" is absent or null`) {
			t.Errorf("%s: Add = %s; want the key property refused as absent", name, res)
		}
	}
}
