package graph_test

import (
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
)

// A null a rebuilt record holds at a List stays null at every List the graph
// canonicalizes, as at one it does not: it holds no value, and becomes no empty
// list.
func TestRebuild_ANullListStaysNullWhereTheGraphCanonicalizes(t *testing.T) {
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
		raw[name] = nil
	}
	props := immutable.WrapProperties(raw, immutable.WithClone(true))
	snap := rebuild(s, snapparts.Parts{Instances: []snapparts.Instance{{
		TypeID: typ.ID(), PrimaryKey: immutable.WrapKey([]any{"x"}), Properties: props,
	}}})
	for inst := range snap.AllInstances() {
		for _, name := range lists {
			val, ok := inst.Property(name)
			if !ok || !val.IsNil() {
				t.Errorf("%s: stored = %v, IsNil = %v; want null", name, ok, val.IsNil())
			}
		}
	}
}
