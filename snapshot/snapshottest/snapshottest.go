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
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// BuildSnapshot adds the given pre-validated instances to a fresh graph
// over s and returns its snapshot — the shared constructor for round-trip
// fixtures. Add diagnostics are intentionally not asserted: fixtures may
// deliberately construct duplicate or unresolved shapes; assert on the
// snapshot instead.
func BuildSnapshot(tb testing.TB, s *schema.Schema, instances ...*instance.ValidInstance) *graph.Snapshot {
	tb.Helper()
	g := graph.New(s)
	for _, inst := range instances {
		g.Add(context.Background(), inst)
	}
	return g.Snapshot()
}

// AssertRoundTrip marshals a snapshot, loads it back, and verifies
// structural equivalence via [DiffSnapshots]. Fails the test with a
// (-want +got) diff on mismatch.
//
// Not valid for a snapshot carrying pre-v0.12 shape, because the round trip
// repairs it while [DiffSnapshots] compares exactly: a whole float narrowed to
// int64 by an older writer heals to float64, and a composed child that lost its
// type identity to the older decoder regains it. Round-trip such a snapshot
// once first; both repairs are idempotent, so every later round trip holds.
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
func AssertDeterministic(tb testing.TB, snap *graph.Snapshot, opts ...snapshot.Option) {
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
	Duplicates []instProjection
	Unresolved []unresProjection
}

type instProjection struct {
	TypeName   string
	TypeID     string
	PK         string
	Properties map[string]any
	Edges      []edgeProjection
	Composed   map[string][]instProjection
	Provenance *provProjection
}

// provProjection distinguishes an instance carrying provenance from one
// without: a nil pointer means none, a zero-valued one means empty fields.
// Both path forms are carried because RawPath is populated only on a parse
// failure, so it alone leaves a well-formed path empty on both sides.
type provProjection struct {
	SourceName string
	Path       string
	RawPath    string
}

type edgeProjection struct {
	Relation   string
	TargetType string
	TargetPK   string
	Properties map[string]any
}

// SourceKey is load-bearing: without it two unresolved records hanging off
// different source instances of the same type compare equal.
type unresProjection struct {
	SourceType string
	SourceKey  string
	Relation   string
	TargetType string
	TargetKey  string
	Required   bool
	Reason     string
	Properties map[string]any
}

// DiffSnapshots compares two snapshots structurally with go-cmp and fails
// the test with a (-want +got) diff on mismatch. Values compare exactly,
// dynamic type included: schema-aware float emission keeps a KindFloat value
// float64 across a marshal/load boundary, so an int64/float64 mismatch is a
// real defect, not a wire artifact to tolerate.
func DiffSnapshots(tb testing.TB, want, got *graph.Snapshot) {
	tb.Helper()
	// EquateEmpty: a built snapshot carries nil property maps where a loaded
	// one carries empty maps; that distinction is outside the contract.
	if d := cmp.Diff(project(want), project(got), cmpopts.EquateEmpty()); d != "" {
		tb.Errorf("snapshots differ (-want +got):\n%s", d)
	}
}

func project(s *graph.Snapshot) snapProjection {
	ids := s.Types()
	p := snapProjection{
		Types:     make([]string, 0, len(ids)),
		Instances: make(map[string][]instProjection, len(ids)),
	}
	for _, typeID := range ids {
		key := typeID.String()
		p.Types = append(p.Types, key)
		for _, inst := range s.InstancesOf(typeID) {
			ip := projectInstanceTree(inst)
			for _, e := range s.EdgesFrom(inst) {
				ip.Edges = append(ip.Edges, edgeProjection{
					Relation:   e.Relation(),
					TargetType: e.Target().TypeID().String(),
					TargetPK:   e.Target().PrimaryKey().String(),
					Properties: e.Properties().Clone(),
				})
			}
			p.Instances[key] = append(p.Instances[key], ip)
		}
	}
	for _, d := range s.Duplicates() {
		p.Duplicates = append(p.Duplicates, projectInstanceTree(d.Instance))
	}
	for _, u := range s.Unresolved() {
		p.Unresolved = append(p.Unresolved, unresProjection{
			SourceType: u.Source.TypeName(),
			SourceKey:  u.Source.PrimaryKey().String(),
			Relation:   u.Relation,
			TargetType: u.TargetType.String(),
			TargetKey:  u.TargetKey,
			Required:   u.Required,
			Reason:     u.Reason,
			Properties: u.Properties().Clone(),
		})
	}
	return p
}

// projectInstanceTree projects an instance and its full composition tree —
// the wire format is recursive (composed children carry their own composed
// children and provenance), so the comparison descends to match. Edges are
// appended by the caller for root instances only; composed children cannot
// carry edges.
func projectInstanceTree(inst *graph.Instance) instProjection {
	ip := instProjection{
		TypeName:   inst.TypeName(),
		TypeID:     inst.TypeID().String(),
		PK:         inst.PrimaryKey().String(),
		Properties: inst.Properties().Clone(),
	}
	if rels := inst.ComposedRelations(); len(rels) > 0 {
		ip.Composed = make(map[string][]instProjection, len(rels))
		for _, rel := range rels {
			for _, child := range inst.Composed(rel) {
				ip.Composed[rel] = append(ip.Composed[rel], projectInstanceTree(child))
			}
		}
	}
	if prov := inst.Provenance(); prov != nil {
		ip.Provenance = &provProjection{
			SourceName: prov.SourceName(),
			Path:       prov.Path().String(),
			RawPath:    prov.RawPath(),
		}
	}
	return ip
}
