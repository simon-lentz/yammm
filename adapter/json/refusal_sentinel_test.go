package json

import (
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/instance"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// shortTargetKeySnapshot builds a snapshot whose edge names a target by one key
// component where the target type declares two. graph.Add never resolves such
// an edge, because it refuses the only instance whose key could match, so the
// shape cannot reach a writer through it; graph.RebuildSnapshot validates
// identity, denoted types, root types and cardinality and checks no key arity,
// so a .ys document can carry one to the writer.
func shortTargetKeySnapshot(t *testing.T) *graph.Snapshot {
	t.Helper()
	s, res := schema.NewBuilder().
		WithName("arity").
		WithSourceID(location.MustNewSourceID("test://arity.yammm")).
		AddType("Company").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithPrimaryKey("region", schema.StringConstraint{}).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithRelation("EMPLOYER", schema.LocalTypeRef("Company", location.Span{}), true, false).
		Done().
		Build()
	if res.HasErrors() {
		t.Fatalf("build schema: %s", res)
	}
	companyT, _ := s.Type("Company")
	personT, _ := s.Type("Person")

	shortKey := immutable.WrapKey([]any{"c1"})
	personKey := immutable.WrapKey([]any{"p1"})

	snap, r := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{companyT.ID(), personT.ID()},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			companyT.ID(): {{
				TypeName: "Company", TypeID: companyT.ID(), PrimaryKey: shortKey,
				Properties: immutable.WrapProperties(map[string]any{"id": "c1", "region": "eu"}),
			}},
			personT.ID(): {{
				TypeName: "Person", TypeID: personT.ID(), PrimaryKey: personKey,
				Properties: immutable.WrapProperties(map[string]any{"id": "p1"}),
			}},
		},
		Edges: []graph.EdgeParts{{
			Relation:   "EMPLOYER",
			SourceType: personT.ID(), SourceKey: personKey,
			TargetType: companyT.ID(), TargetKey: shortKey,
			Properties: immutable.WrapProperties(nil),
		}},
	})
	if err := r.Err(); err != nil {
		t.Fatalf("RebuildSnapshot refused the parts, so the refusal has no subject: %v", err)
	}
	return snap
}

// rebuiltEdges builds a snapshot of two Companies and one Person through
// graph.RebuildSnapshot, which reconstructs a document and does not validate
// one: graph.Add refuses the first two shapes below itself and leaves the third
// unresolved for good, since it refuses the only instance that could resolve
// it, but a .ys document can carry all of them to a writer. Company declares a two-part key; EMPLOYER is a (one)
// association.
func rebuiltEdges(t *testing.T, edges func(person, company schema.TypeID) []graph.EdgeParts) *graph.Snapshot {
	t.Helper()
	return rebuiltEdgesTo(t, func(person, company, _ schema.TypeID) []graph.EdgeParts { return edges(person, company) })
}

// rebuiltEdgesTo is rebuiltEdges with a third type, Person itself, offered as
// an edge target: a type the EMPLOYER association does not declare.
func rebuiltEdgesTo(t *testing.T, edges func(person, company, other schema.TypeID) []graph.EdgeParts) *graph.Snapshot {
	t.Helper()
	s, res := schema.NewBuilder().
		WithName("shapes").
		WithSourceID(location.MustNewSourceID("test://shapes.yammm")).
		AddType("Company").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithPrimaryKey("region", schema.StringConstraint{}).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithRelation("EMPLOYER", schema.LocalTypeRef("Company", location.Span{}), true, false).
		Done().
		Build()
	if res.HasErrors() {
		t.Fatalf("build schema: %s", res)
	}
	companyT, _ := s.Type("Company")
	personT, _ := s.Type("Person")
	company := func(id string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeName: "Company", TypeID: companyT.ID(), PrimaryKey: immutable.WrapKey([]any{id, "eu"}),
			Properties: immutable.WrapProperties(map[string]any{"id": id, "region": "eu"}),
		}
	}
	snap, r := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{companyT.ID(), personT.ID()},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			companyT.ID(): {company("c1"), company("c2")},
			personT.ID(): {{
				TypeName: "Person", TypeID: personT.ID(), PrimaryKey: immutable.WrapKey([]any{"p1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "p1"}),
			}},
		},
		Edges: edges(personT.ID(), companyT.ID(), personT.ID()),
	})
	if err := r.Err(); err != nil {
		t.Fatalf("RebuildSnapshot refused the parts, so the refusal has no subject: %v", err)
	}
	return snap
}

// The writer's promise is that ParseObject and the validator accept every
// shape it emits. Each shape below is one a constructor admits and the writer
// cannot keep that promise for, so it refuses, as a class and naming the edge,
// rather than emit a document its own reader refuses.
func TestWriters_AnUnrepresentableShapeIsRefusedAsAClass(t *testing.T) {
	t.Parallel()
	edge := func(rel string, from, to schema.TypeID, key ...any) graph.EdgeParts {
		return graph.EdgeParts{
			Relation: rel, SourceType: from, SourceKey: immutable.WrapKey([]any{"p1"}),
			TargetType: to, TargetKey: immutable.WrapKey(key), Properties: immutable.WrapProperties(nil),
		}
	}
	for _, c := range []struct {
		name, mentions string
		snap           func(t *testing.T) *graph.Snapshot
	}{
		{"a target key of the wrong arity", "target key has 1 components", shortTargetKeySnapshot},
		{
			"a (one) association carrying two edges", `"EMPLOYER"`,
			func(t *testing.T) *graph.Snapshot {
				t.Helper()
				return rebuiltEdges(t, func(p, c schema.TypeID) []graph.EdgeParts {
					return []graph.EdgeParts{edge("EMPLOYER", p, c, "c1", "eu"), edge("EMPLOYER", p, c, "c2", "eu")}
				})
			},
		},
		{
			// graph.Add resolves every edge to the association's declared target, so
			// only a rebuilt snapshot holds one that points elsewhere. Its _target_
			// fields would be another type's keys, which the validator refuses.
			"an edge to a type the association does not declare", "declares the target",
			func(t *testing.T) *graph.Snapshot {
				t.Helper()
				return rebuiltEdgesTo(t, func(p, _, other schema.TypeID) []graph.EdgeParts {
					return []graph.EdgeParts{edge("EMPLOYER", p, other, "p1")}
				})
			},
		},
		{
			"an edge under a relation its type does not declare", `"NOPE"`,
			func(t *testing.T) *graph.Snapshot {
				t.Helper()
				return rebuiltEdges(t, func(p, c schema.TypeID) []graph.EdgeParts {
					return []graph.EdgeParts{edge("NOPE", p, c, "c1", "eu")}
				})
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			snap := c.snap(t)
			a := New()
			_, err := a.MarshalObject(context.Background(), snap)
			if !errors.Is(err, ErrUnrepresentable) {
				t.Errorf("MarshalObject: %v does not match ErrUnrepresentable", err)
			}
			if err != nil && !strings.Contains(err.Error(), c.mentions) {
				t.Errorf("the refusal does not name %s: %v", c.mentions, err)
			}
			if _, werr := a.WriteObject(context.Background(), io.Discard, snap); !errors.Is(werr, ErrUnrepresentable) {
				t.Errorf("WriteObject: %v does not match ErrUnrepresentable", werr)
			}
		})
	}
}

// The class separates a refusal of the data from a nil argument, an encoding
// failure and a cancellation, which reach a caller through one return value.
// Its own doc comment claims the first two; nothing else pins them.
func TestWriters_TheRefusalClassCoversTheDataAlone(t *testing.T) {
	t.Parallel()

	_, nilErr := New().MarshalObject(context.Background(), nil)
	if !errors.Is(nilErr, ErrNilResult) {
		t.Errorf("a nil result = %v, want ErrNilResult", nilErr)
	}
	if errors.Is(nilErr, ErrUnrepresentable) {
		t.Errorf("a nil result matched the refusal class: %v", nilErr)
	}

	// encoding/json cannot render a non-finite float. instance.CanonicalValue
	// refuses one, so canonicalOrRaw hands it back raw and the document's own
	// Marshal is what fails — the writer's only encoding-failure path.
	inf := infiniteFloatSnapshot(t)
	if _, err := New().MarshalObject(context.Background(), inf); err == nil {
		t.Error("a non-finite float was encoded")
	} else {
		if !strings.Contains(err.Error(), "json marshal:") {
			t.Fatalf("the fixture did not reach the encode failure, so it guards nothing: %v", err)
		}
		if errors.Is(err, ErrUnrepresentable) {
			t.Errorf("an encoding failure matched the refusal class: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().MarshalObject(ctx, inf); err == nil {
		t.Error("a cancelled marshal reported success")
	} else {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("the fixture did not reach the cancellation, so it guards nothing: %v", err)
		}
		if errors.Is(err, ErrUnrepresentable) {
			t.Errorf("a cancellation matched the refusal class: %v", err)
		}
	}
}

// infiniteFloatSnapshot builds a graph through instance.NewValidInstance, which
// runs no validation, because a validated Float is never non-finite.
func infiniteFloatSnapshot(t *testing.T) *graph.Snapshot {
	t.Helper()
	s, res := schema.NewBuilder().
		WithName("nonfinite").
		WithSourceID(location.MustNewSourceID("test://nonfinite.yammm")).
		AddType("Reading").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("ratio", schema.FloatConstraint{}).
		Done().
		Build()
	if res.HasErrors() {
		t.Fatalf("build schema: %s", res)
	}
	readingT, _ := s.Type("Reading")

	g := graph.New(s)
	inst := instance.NewValidInstance("Reading", readingT.ID(),
		immutable.WrapKey([]any{"r1"}),
		immutable.WrapProperties(map[string]any{"id": "r1", "ratio": math.Inf(1)}),
		nil, nil, nil)
	if r := g.Add(context.Background(), inst); r.HasErrors() {
		t.Fatalf("graph.Add: %s", r)
	}
	return g.Snapshot()
}
