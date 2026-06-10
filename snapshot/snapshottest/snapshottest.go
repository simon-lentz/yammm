// Package snapshottest provides test helpers for snapshot persistence
// tests. It is a separate package (not _test.go helpers in snapshot/) so
// the graph and snapshot test suites can share one structural-equivalence
// vocabulary for round-trip assertions.
package snapshottest

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// AssertRoundTrip marshals a snapshot, loads it back, and verifies
// structural equivalence via [DiffSnapshots]. Fails the test with a
// (-want +got) diff on mismatch.
func AssertRoundTrip(tb testing.TB, snap *graph.Snapshot, s *schema.Schema, opts ...snapshot.Option) {
	tb.Helper()
	ctx := context.Background()

	data, result := snapshot.Marshal(ctx, snap, opts...)
	if err := result.Err(); err != nil {
		tb.Fatalf("AssertRoundTrip: Marshal failed: %v", err)
	}

	loaded, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		tb.Fatalf("AssertRoundTrip: Load failed: %v", err)
	}

	DiffSnapshots(tb, snap, loaded)
}

// AssertDeterministic marshals a snapshot twice with the same options and
// verifies byte-level equality.
func AssertDeterministic(tb testing.TB, snap *graph.Snapshot, _ *schema.Schema, opts ...snapshot.Option) {
	tb.Helper()
	ctx := context.Background()

	data1, result := snapshot.Marshal(ctx, snap, opts...)
	if err := result.Err(); err != nil {
		tb.Fatalf("AssertDeterministic: first Marshal failed: %v", err)
	}

	data2, result := snapshot.Marshal(ctx, snap, opts...)
	if err := result.Err(); err != nil {
		tb.Fatalf("AssertDeterministic: second Marshal failed: %v", err)
	}

	if string(data1) != string(data2) {
		tb.Error("AssertDeterministic: two marshals produced different output")
	}
}

// snapProjection is the comparable view DiffSnapshots builds from a
// snapshot's public accessors. Properties come from Clone() so go-cmp can
// walk plain maps rather than immutable wrappers.
type snapProjection struct {
	Types      []string
	Instances  map[string][]instProjection
	Duplicates []string
	Unresolved []unresProjection
}

type instProjection struct {
	PK         string
	Properties map[string]any
	Edges      []edgeProjection
	Composed   map[string][]instProjection
	SourceName string
}

type edgeProjection struct {
	Relation   string
	TargetType string
	TargetPK   string
	Properties map[string]any
}

type unresProjection struct {
	SourceType string
	Relation   string
	TargetType string
	TargetKey  string
	Required   bool
	Reason     string
	Properties map[string]any
}

// DiffSnapshots compares two snapshots structurally with go-cmp and fails
// the test with a (-want +got) diff on mismatch. Numeric property values
// compare by float64 value, tolerating the wire format's int64↔float64
// representations across a marshal/load boundary.
func DiffSnapshots(tb testing.TB, want, got *graph.Snapshot) {
	tb.Helper()
	numericCoercion := cmp.FilterValues(func(a, b any) bool {
		_, aInt := a.(int64)
		_, aFloat := a.(float64)
		_, bInt := b.(int64)
		_, bFloat := b.(float64)
		return (aInt || aFloat) && (bInt || bFloat)
	}, cmp.Transformer("toFloat64", func(v any) float64 {
		switch n := v.(type) {
		case int64:
			return float64(n)
		case float64:
			return n
		}
		panic("unreachable: filter admits only int64/float64")
	}))
	// EquateEmpty: a built snapshot carries nil property maps where a
	// loaded one carries empty maps; the distinction is not part of the
	// structural contract.
	if d := cmp.Diff(project(want), project(got), numericCoercion, cmpopts.EquateEmpty()); d != "" {
		tb.Errorf("snapshots differ (-want +got):\n%s", d)
	}
}

func project(s *graph.Snapshot) snapProjection {
	p := snapProjection{
		Types:     s.Types(),
		Instances: make(map[string][]instProjection, len(s.Types())),
	}
	for _, typeName := range s.Types() {
		for _, inst := range s.InstancesOf(typeName) {
			ip := instProjection{
				PK:         inst.PrimaryKey().String(),
				Properties: inst.Properties().Clone(),
			}
			for _, e := range s.EdgesFrom(inst) {
				ip.Edges = append(ip.Edges, edgeProjection{
					Relation:   e.Relation(),
					TargetType: e.Target().TypeName(),
					TargetPK:   e.Target().PrimaryKey().String(),
					Properties: e.Properties().Clone(),
				})
			}
			if rels := inst.ComposedRelations(); len(rels) > 0 {
				ip.Composed = make(map[string][]instProjection, len(rels))
				for _, rel := range rels {
					for _, child := range inst.Composed(rel) {
						ip.Composed[rel] = append(ip.Composed[rel], instProjection{
							PK:         child.PrimaryKey().String(),
							Properties: child.Properties().Clone(),
						})
					}
				}
			}
			if prov := inst.Provenance(); prov != nil {
				ip.SourceName = prov.SourceName()
			}
			p.Instances[typeName] = append(p.Instances[typeName], ip)
		}
	}
	for _, d := range s.Duplicates() {
		p.Duplicates = append(p.Duplicates, d.Instance.TypeName()+d.Instance.PrimaryKey().String())
	}
	for _, u := range s.Unresolved() {
		p.Unresolved = append(p.Unresolved, unresProjection{
			SourceType: u.Source.TypeName(),
			Relation:   u.Relation,
			TargetType: u.TargetType,
			TargetKey:  u.TargetKey,
			Required:   u.Required,
			Reason:     u.Reason,
			Properties: u.Properties().Clone(),
		})
	}
	return p
}
