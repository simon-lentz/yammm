package snapshot_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
	"github.com/simon-lentz/yammm/snapshot/snapshottest"
)

// probeTB captures whether a helper reported a failure, so a test can assert
// that an assertion helper fails when it should.
type probeTB struct {
	testing.TB
	errored bool
	msg     string
}

func (p *probeTB) Helper() {}

func (p *probeTB) Errorf(format string, args ...any) {
	p.errored = true
	p.msg = fmt.Sprintf(format, args...)
}

// A root instance carrying no type identity emits no type_id at all, rather
// than a contentless {"schema_path":"","name":""} object a later load would
// read as an empty type name. The composed-child guard has the same job; this
// is the root-instance one, which nothing reached.
func TestMarshal_ZeroTypeIDRootEmitsNoTypeID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchema(t)

	// graph.Add panics on a zero TypeID, so the state reaches a snapshot only
	// through RebuildSnapshot — a caller assembling parts directly.
	snap, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{tidOf(t, s, "Person")},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			tidOf(t, s, "Person"): {{
				TypeName:   "Person",
				PrimaryKey: immutable.WrapKey([]any{"p1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "Alice"}),
			}},
		},
	})
	if result.HasErrors() {
		t.Fatalf("rebuild: %s", result)
	}

	data, mres := snapshot.Marshal(ctx, snap)
	if err := mres.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), `"schema_path":""`) {
		t.Errorf("marshal emitted a contentless type_id:\n%s", data)
	}
	if strings.Contains(string(data), `"type_id"`) {
		t.Errorf("marshal emitted a type_id for an instance that carries none:\n%s", data)
	}
}

// The duplicates section carries the same guard as the root path, and nothing
// reached it either.
func TestMarshal_ZeroTypeIDDuplicateEmitsNoTypeID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchema(t)

	inst := graph.InstanceParts{
		TypeName:   "Person",
		PrimaryKey: immutable.WrapKey([]any{"p1"}),
		Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "Alice"}),
	}
	snap, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types:     []schema.TypeID{tidOf(t, s, "Person")},
		Instances: map[schema.TypeID][]graph.InstanceParts{tidOf(t, s, "Person"): {inst}},
		Duplicates: []graph.DuplicateParts{{
			Type:     tidOf(t, s, "Person"),
			Key:      immutable.WrapKey([]any{"p1"}),
			Instance: inst,
		}},
	})
	if result.HasErrors() {
		t.Fatalf("rebuild: %s", result)
	}

	data, mres := snapshot.Marshal(ctx, snap)
	if err := mres.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), `"schema_path":""`) {
		t.Errorf("marshal emitted a contentless type_id in the duplicates section:\n%s", data)
	}
}

// A persisted type_id naming a resolvable type other than the one the tag form
// resolves to is a contradiction, and the load reports it rather than silently
// keeping the tag form's answer. The contradiction is expressible only on the
// legacy wire, which names types where v3 denotes them.
func TestLoad_ContradictoryTypeIDIsReported(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)

	// TypeName says the local Part; TypeID says the imported one of the same
	// name. Only a caller assembling parts directly can build this.
	imported := mustTypeIDIn(t, s, "base", "Part")
	snap, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{tidOf(t, s, "Part")},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			tidOf(t, s, "Part"): {{
				TypeName:   "Part",
				TypeID:     imported,
				PrimaryKey: immutable.WrapKey([]any{"p1"}),
				Properties: immutable.WrapProperties(map[string]any{"name": "p1"}),
			}},
		},
	})
	if result.HasErrors() {
		t.Fatalf("rebuild: %s", result)
	}
	data, mres := snapshot.MarshalLegacyV2(ctx, snap)
	if err := mres.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}

	_, lres := snapshot.Load(ctx, data, s)
	found := false
	for issue := range lres.Issues() {
		if issue.Code() == diag.E_SNAPSHOT_TYPEID_MISMATCH {
			found = true
		}
	}
	if !found {
		t.Errorf("load accepted a document whose type_id contradicts its tag form: %s\n%s", lres, data)
	}
}

// DiffSnapshots compares a duplicate's full instance tree, so a difference in
// a duplicate's properties is a reported difference. The projection carried a
// bare identity string until this branch widened it, and nothing asserted the
// widening had any power.
func TestDiffSnapshots_SeesDuplicatePropertyDifference(t *testing.T) {
	t.Parallel()
	s := testSchema(t)

	// Two instances share a primary key, so the second lands in the
	// duplicates section, where its properties are the only difference.
	build := func(dupName string) *graph.Snapshot {
		g := graph.New(s)
		g.Add(context.Background(),
			mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}))
		g.Add(context.Background(), instancetest.VI(
			"Person",
			instancetest.TypeID(mustTypeID(t, s, "Person")),
			instancetest.PK("p1"),
			instancetest.Props(map[string]any{"id": "p1", "name": dupName}),
		))
		return g.Snapshot()
	}

	want, got := build("Bob"), build("Carol")
	if len(want.Duplicates()) != 1 || len(got.Duplicates()) != 1 {
		t.Fatalf("fixture must produce one duplicate each, got %d and %d",
			len(want.Duplicates()), len(got.Duplicates()))
	}

	probe := &probeTB{TB: t}
	snapshottest.DiffSnapshots(probe, want, got)
	if !probe.errored {
		t.Error("DiffSnapshots passed two snapshots whose duplicates differ in a property")
	}

	// The control: identical snapshots must still compare equal, or the
	// assertion above would pass on any input.
	probe = &probeTB{TB: t}
	snapshottest.DiffSnapshots(probe, build("Bob"), build("Bob"))
	if probe.errored {
		t.Errorf("DiffSnapshots reported a difference between identical snapshots: %s", probe.msg)
	}
}
