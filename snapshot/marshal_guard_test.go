package snapshot_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/instancetest"
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

// DiffSnapshots compares a duplicate's full instance tree, so a difference in
// a duplicate's properties is a reported difference.
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
