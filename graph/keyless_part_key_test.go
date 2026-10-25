package graph_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// addKeylessPart offers a keyless part with key badKey to every constructor:
// composed under its root at Graph.Add, attached by Graph.AddComposed, and
// rebuilt by RebuildSnapshot. It fails the test when a constructor keeps any
// of the record it refused.
func addKeylessPart(t *testing.T, badKey immutable.Key) map[string]diag.Result {
	t.Helper()
	ctx := t.Context()
	s, res := schema.LoadString(ctx, `schema "kl"

type Doc {
	id String primary
	*-> LINES (_:many) Line
}

part type Line {
	label String
}
`, "kl.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	docT, _ := s.Type("Doc")
	lineT, _ := s.Type("Line")
	lineProps := immutable.WrapProperties(map[string]any{"label": "l"})
	docKey := immutable.WrapKey([]any{"d1"})
	docProps := immutable.WrapProperties(map[string]any{"id": "d1"})
	line := instance.NewValidInstance("Line", lineT.ID(), badKey, lineProps, nil, nil, nil)

	results := map[string]diag.Result{}
	g := graph.New(s)
	results["Graph.Add"] = g.Add(ctx, instance.NewValidInstance("Doc", docT.ID(), docKey, docProps, nil,
		map[string]immutable.Value{"LINES": immutable.Wrap([]any{line})}, nil))
	if n := len(g.Snapshot().InstancesOf(docT.ID())); n != 0 {
		t.Errorf("Graph.Add installed %d roots from a refused record", n)
	}
	g2 := graph.New(s)
	if r := g2.Add(ctx, instance.NewValidInstance("Doc", docT.ID(), docKey, docProps, nil, nil, nil)); !r.OK() {
		t.Fatalf("Add the root: %s", r)
	}
	results["Graph.AddComposed"] = g2.AddComposed(ctx, docT.ID(), docKey.String(), "LINES", line)
	if roots := g2.Snapshot().InstancesOf(docT.ID()); len(roots) != 1 || roots[0].ComposedCount("LINES") != 0 {
		t.Errorf("Graph.AddComposed attached a refused child")
	}
	_, results["RebuildSnapshot"] = graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{docT.ID()},
		Instances: []graph.InstanceParts{{
			TypeID: docT.ID(), PrimaryKey: docKey, Properties: docProps,
			Composed: map[string][]graph.InstanceParts{"LINES": {{TypeID: lineT.ID(), PrimaryKey: badKey, Properties: lineProps}}},
		}},
	})
	return results
}

// requireComponentRefused fails unless each result is one Error
// E_GRAPH_INVALID_PK naming the part's type and component 1.
func requireComponentRefused(t *testing.T, results map[string]diag.Result) {
	t.Helper()
	for name, r := range results {
		issues := slices.Collect(r.Issues())
		if len(issues) != 1 || issues[0].Code() != diag.E_GRAPH_INVALID_PK || issues[0].Severity() != diag.Error {
			t.Errorf("%s: %s, want one Error E_GRAPH_INVALID_PK", name, r)
			continue
		}
		if msg := issues[0].Message(); !strings.Contains(msg, "component 1 is not a scalar graph.ParseKey reads back") || !strings.Contains(msg, "Line") {
			t.Errorf("%s: message %q, want it to name the type and component 1", name, msg)
		}
	}
}

// TestKeylessPartKey_HoldsOnlyScalarsParseKeyReads pins the keyless half of the
// stored-key rule at every constructor: a keyless part whose key holds a list
// draws one E_GRAPH_INVALID_PK naming the component, and the graph keeps
// nothing of the record it refused.
func TestKeylessPartKey_HoldsOnlyScalarsParseKeyReads(t *testing.T) {
	t.Parallel()
	requireComponentRefused(t, addKeylessPart(t, immutable.WrapKey([]any{"a", []any{"x"}})))
}

// TestKeylessPartKey_RefusesATimeOrUUIDComponent pins that a keyless part's key
// holds no time.Time or uuid.UUID, held or behind a pointer: only a key
// property's constraint renders one, and a keyless part has none.
func TestKeylessPartKey_RefusesATimeOrUUIDComponent(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	id := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	for name, component := range map[string]any{"time": at, "pointer to a time": &at, "UUID": id, "pointer to a UUID": &id} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			requireComponentRefused(t, addKeylessPart(t, immutable.WrapKey([]any{"a", component})))
		})
	}
}
