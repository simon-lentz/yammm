package neo4j

import (
	"errors"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/cypher"
)

// keyRefusalSnapshot reads a document no validator judged: a Slot whose two
// Date key components are not dates, and a Holder whose one association names
// it.
func keyRefusalSnapshot(t *testing.T) graph.Snapshot {
	t.Helper()
	s := loadInline(t, `schema "keys"

type Slot {
	day Date primary
	until Date primary
}

type Holder {
	id String primary
	--> AT (_:many) Slot
}
`)
	slotT, _ := s.Type("Slot")
	holderT, _ := s.Type("Holder")
	slotKey := immutable.WrapKey([]any{"not-a-day", "not-an-until"})
	snap := snappartstest.Loaded(t, s, snapparts.Parts{
		Types: []schema.TypeID{slotT.ID(), holderT.ID()},
		Instances: []snapparts.Instance{
			{TypeID: slotT.ID(), PrimaryKey: slotKey, Properties: immutable.WrapProperties(map[string]any{"day": "not-a-day", "until": "not-an-until"})},
			{TypeID: holderT.ID(), PrimaryKey: immutable.WrapKey([]any{"h1"}), Properties: immutable.WrapProperties(map[string]any{"id": "h1"})},
		},
		Edges: []snapparts.Edge{{Relation: "AT", SourceType: holderT.ID(), SourceKey: immutable.WrapKey([]any{"h1"}), TargetKey: slotKey}},
	})
	return snap
}

// refusals returns the errors err joins, or err alone when it joins none.
func refusals(err error) []error {
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		return j.Unwrap()
	}
	return []error{err}
}

// Two refused key components of one root draw two refusals, and each names
// the context the call reports it in, the root's type at NodeQueries and the
// edge's endpoint at EdgeQueries, so no line of the joined error loses it.
func TestWriteQueries_EachRefusedKeyCarriesItsContext(t *testing.T) {
	t.Parallel()
	snap := keyRefusalSnapshot(t)
	_, nodeErr := NodeQueries(t.Context(), cypher.Config{}, snap)
	_, edgeErr := EdgeQueries(t.Context(), cypher.Config{}, snap)
	for _, tc := range []struct {
		call    string
		err     error
		context string
	}{
		{"NodeQueries", nodeErr, `type "Slot": `},
		{"EdgeQueries", edgeErr, `target type "Slot": `},
	} {
		if !errors.Is(tc.err, ErrUnrepresentable) {
			t.Errorf("%s = %v, want a refusal of the keys", tc.call, tc.err)
			continue
		}
		parts := refusals(tc.err)
		if len(parts) != 2 {
			t.Errorf("%s drew %d refusals, want one for each key component: %v", tc.call, len(parts), tc.err)
		}
		for i, key := range []string{"day", "until"} {
			want := tc.context + `primary key "` + key + `": `
			if i < len(parts) && !strings.HasPrefix(parts[i].Error(), want) {
				t.Errorf("%s refusal %d = %q, want it to start %q", tc.call, i, parts[i], want)
			}
		}
		for line := range strings.Lines(tc.err.Error()) {
			if !strings.HasPrefix(line, tc.context) {
				t.Errorf("%s: the line %q lost its context %q", tc.call, line, tc.context)
			}
		}
	}
}

// A refused source key names the edge's source endpoint, as a refused target
// key names its target.
func TestEdgeQueries_ARefusedSourceKeyNamesTheSource(t *testing.T) {
	t.Parallel()
	s := loadInline(t, `schema "keys"

type Slot {
	day Date primary
}

type Holder {
	day Date primary
	--> AT (_:many) Slot
}
`)
	slotT, _ := s.Type("Slot")
	holderT, _ := s.Type("Holder")
	holderKey := immutable.WrapKey([]any{"not-a-day"})
	slotKey := immutable.WrapKey([]any{"2020-01-01"})
	snap := snappartstest.Loaded(t, s, snapparts.Parts{
		Types: []schema.TypeID{slotT.ID(), holderT.ID()},
		Instances: []snapparts.Instance{
			{TypeID: slotT.ID(), PrimaryKey: slotKey, Properties: immutable.WrapProperties(map[string]any{"day": "2020-01-01"})},
			{TypeID: holderT.ID(), PrimaryKey: holderKey, Properties: immutable.WrapProperties(map[string]any{"day": "not-a-day"})},
		},
		Edges: []snapparts.Edge{{Relation: "AT", SourceType: holderT.ID(), SourceKey: holderKey, TargetKey: slotKey}},
	})
	_, err := EdgeQueries(t.Context(), cypher.Config{}, snap)
	if !errors.Is(err, ErrUnrepresentable) {
		t.Fatalf("EdgeQueries = %v, want a refusal of the source key", err)
	}
	want := `source type "Holder": primary key "day": `
	if !strings.HasPrefix(err.Error(), want) {
		t.Errorf("EdgeQueries = %q, want it to start %q", err, want)
	}
}

// TestEdgeQueries_AnImportedEndpointIsNamedAsTheEntrySchemaNamesIt pins that a
// refused endpoint key names the endpoint's type by the alias the entry schema
// imports it under, as a refused node key does, and never by its source path.
func TestEdgeQueries_AnImportedEndpointIsNamedAsTheEntrySchemaNamesIt(t *testing.T) {
	t.Parallel()
	s, err := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte("schema \"entry\"\n\nimport \"lib.yammm\" as lib\n\ntype Holder {\n\tid String primary\n\t--> AT (_:many) lib.Slot\n}\n"),
		"lib.yammm":   []byte("schema \"lib\"\n\ntype Slot {\n\tday Date primary\n}\n\ntype Owner {\n\tday Date primary\n\t--> HAS (_:many) Slot\n}\n"),
	}, "entry.yammm", schema.WithSourcesOnly(true), schema.WithModuleRoot("."))
	if err != nil {
		t.Fatalf("load schema: %s", err)
	}
	slotT, slotOK := s.ResolveTypeName("lib.Slot")
	ownerT, ownerOK := s.ResolveTypeName("lib.Owner")
	if !slotOK || !ownerOK {
		t.Fatal("lib.Slot or lib.Owner did not resolve through the entry schema")
	}
	holderT, _ := s.Type("Holder")
	root := func(id schema.TypeID, key, field string) snapparts.Instance {
		return snapparts.Instance{TypeID: id, PrimaryKey: immutable.WrapKey([]any{key}), Properties: immutable.WrapProperties(map[string]any{field: key})}
	}
	for _, tc := range []struct {
		name      string
		instances []snapparts.Instance
		edge      snapparts.Edge
		node      string
		edgeWant  string
	}{
		{
			"a refused target",
			[]snapparts.Instance{root(slotT.ID(), "not-a-day", "day"), root(holderT.ID(), "h1", "id")},
			snapparts.Edge{Relation: "AT", SourceType: holderT.ID(), SourceKey: immutable.WrapKey([]any{"h1"}), TargetKey: immutable.WrapKey([]any{"not-a-day"})},
			`type "lib.Slot": primary key "day": `, `target type "lib.Slot": primary key "day": `,
		},
		{
			"a refused source",
			[]snapparts.Instance{root(slotT.ID(), "2020-01-01", "day"), root(ownerT.ID(), "not-a-day", "day")},
			snapparts.Edge{Relation: "HAS", SourceType: ownerT.ID(), SourceKey: immutable.WrapKey([]any{"not-a-day"}), TargetKey: immutable.WrapKey([]any{"2020-01-01"})},
			`type "lib.Owner": primary key "day": `, `source type "lib.Owner": primary key "day": `,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			snap := snappartstest.Loaded(t, s, snapparts.Parts{
				Types:     []schema.TypeID{holderT.ID(), slotT.ID(), ownerT.ID()},
				Instances: tc.instances,
				Edges:     []snapparts.Edge{tc.edge},
			})
			_, nodeErr := NodeQueries(t.Context(), cypher.Config{}, snap)
			_, edgeErr := EdgeQueries(t.Context(), cypher.Config{}, snap)
			for _, got := range []struct {
				call string
				err  error
				want string
			}{{"NodeQueries", nodeErr, tc.node}, {"EdgeQueries", edgeErr, tc.edgeWant}} {
				if !errors.Is(got.err, ErrUnrepresentable) || !strings.HasPrefix(got.err.Error(), got.want) {
					t.Errorf("%s = %v; want a refusal starting %q", got.call, got.err, got.want)
				}
			}
		})
	}
}
