package graph_test

import (
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
)

// The validator refuses a string that is not valid UTF-8, so every fixture
// here is built with instancetest or as parts, which run no validation.

const keyUTF8Schema = `schema "keyutf8"

type Target {
	key String primary
}

type Holder {
	id String primary
	text String
	--> LINK (_:many) Target
	*-> PARTS (_:many) Part
}

part type Part {
	pid String primary
}
`

// keyText is a named type over string, which a key holds as it arrived.
type keyText string

func loadKeyUTF8Schema(t *testing.T) (*schema.Schema, schema.TypeID, schema.TypeID) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), keyUTF8Schema, "keyutf8.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	return s, mustTypeID(t, s, "Holder"), mustTypeID(t, s, "Part")
}

// requireInvalidKey asserts res refuses a key with E_GRAPH_INVALID_PK, in
// diagnostics that are valid UTF-8.
func requireInvalidKey(t *testing.T, res diag.Result) {
	t.Helper()
	if !res.HasCode(diag.E_GRAPH_INVALID_PK) {
		t.Errorf("no %s for a key component that is not valid UTF-8: %s", diag.E_GRAPH_INVALID_PK, res)
	}
	for issue := range res.Issues() {
		if !utf8.ValidString(issue.Message()) {
			t.Errorf("the message is not valid UTF-8: %q", issue.Message())
		}
		for _, d := range issue.Details() {
			if !utf8.ValidString(d.Value) {
				t.Errorf("detail %s is not valid UTF-8: %q", d.Key, d.Value)
			}
		}
	}
}

func holderVI(holder schema.TypeID, key string, opts ...instancetest.VIOption) *instance.ValidInstance {
	return instancetest.VI("Holder", append([]instancetest.VIOption{
		instancetest.TypeID(holder), instancetest.PK(key),
		instancetest.Props(map[string]any{"id": key}),
	}, opts...)...)
}

// An address is a key's JSON rendering, which writes each byte of an invalid
// UTF-8 sequence as U+FFFD: [graph.ParseKey] reads such a component back as
// other bytes, and two keys differing only in those bytes render one address.
// Add refuses the key wherever it stands and installs nothing.
func TestAdd_RefusesAKeyComponentThatIsNotValidUTF8(t *testing.T) {
	t.Parallel()
	s, holder, part := loadKeyUTF8Schema(t)

	for _, c := range []struct {
		name string
		inst *instance.ValidInstance
	}{
		{"a root's key", holderVI(holder, "a\xffb")},
		{"a root's key truncated mid-sequence", holderVI(holder, "caf\xc3")},
		{"a root's key held in a named type", instancetest.VI("Holder", instancetest.TypeID(holder),
			instancetest.PK(keyText("a\xffb")), instancetest.Props(map[string]any{"id": keyText("a\xffb")}))},
		{"a target key", holderVI(holder, "h1", instancetest.Edges(map[string]*instance.ValidEdgeData{
			"LINK": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
				instance.NewValidEdgeTarget(immutable.WrapKey([]any{"a\xffb"}), immutable.Properties{}),
			}),
		}))},
		{"a composed child's key", holderVI(holder, "h1", instancetest.Composed(map[string]immutable.Value{
			"PARTS": immutable.Wrap([]any{
				instancetest.VI("Part", instancetest.TypeID(part), instancetest.PK("a\xffb"),
					instancetest.Props(map[string]any{"pid": "a\xffb"})),
			}),
		}))},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			g := graph.New(s)
			res := g.Add(t.Context(), c.inst)
			if res.OK() {
				t.Errorf("Add accepted the key")
			}
			requireInvalidKey(t, res)
			if n := len(g.Snapshot().InstancesOf(holder)); n != 0 {
				t.Errorf("the refused instance installed: %d Holder instances", n)
			}
		})
	}

	t.Run("two keys differing only in invalid bytes fold to no one address", func(t *testing.T) {
		t.Parallel()
		g := graph.New(s)
		for _, key := range []string{"a\xffb", "a\xfeb"} {
			requireInvalidKey(t, g.Add(t.Context(), holderVI(holder, key)))
		}
		snap := g.Snapshot()
		if n, d := len(snap.InstancesOf(holder)), len(snap.Duplicates()); n != 0 || d != 0 {
			t.Errorf("the graph holds %d instances and %d duplicates, want none: the two keys are different values", n, d)
		}
	})

	// U+FFFD is a character: a key holding it is its own address.
	t.Run("a key holding U+FFFD is accepted", func(t *testing.T) {
		t.Parallel()
		g := graph.New(s)
		if res := g.Add(t.Context(), holderVI(holder, "a�b")); !res.OK() {
			t.Fatalf("Add: %s", res)
		}
		inst, ok := g.Snapshot().InstanceByKey(holder, graph.FormatKey("a�b"))
		if !ok {
			t.Fatalf("the instance is not at its address")
		}
		got, err := graph.ParseKey(inst.PrimaryKey().String())
		if err != nil || len(got) != 1 || got[0] != "a�b" {
			t.Errorf("ParseKey read the key back as %q, %v", got, err)
		}
	})
}

// RebuildSnapshot holds parts to the key rule Add holds an instance to.
func TestRebuildSnapshot_RefusesAKeyComponentThatIsNotValidUTF8(t *testing.T) {
	t.Parallel()
	s, holder, part := loadKeyUTF8Schema(t)
	root := func(id schema.TypeID, prop, key string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeID:     id,
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{prop: key}),
		}
	}
	const bad = "a\xffb"

	for _, c := range []struct {
		name  string
		parts graph.SnapshotParts
	}{
		{"a root's key", graph.SnapshotParts{Instances: []graph.InstanceParts{root(holder, "id", bad)}}},
		{"a composed child's key", graph.SnapshotParts{Instances: []graph.InstanceParts{{
			TypeID:     holder,
			PrimaryKey: immutable.WrapKey([]any{"h1"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "h1"}),
			Composed:   map[string][]graph.InstanceParts{"PARTS": {root(part, "pid", bad)}},
		}}}},
		{"an unresolved record's target key", graph.SnapshotParts{
			Instances: []graph.InstanceParts{root(holder, "id", "h1")},
			Unresolved: []graph.UnresolvedParts{{
				SourceType: holder, SourceKey: immutable.WrapKey([]any{"h1"}),
				Relation: "LINK", TargetKey: immutable.WrapKey([]any{bad}), Reason: "target_missing",
			}},
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			snap, res := graph.RebuildSnapshot(s, c.parts)
			if snap != nil {
				t.Errorf("RebuildSnapshot built a snapshot holding the key")
			}
			requireInvalidKey(t, res)
		})
	}
}

// Neither constructor validates a value: a property that is no key component
// is held as it arrived, and each writer refuses it.
func TestBypass_APropertyThatIsNotValidUTF8IsHeldAsItArrived(t *testing.T) {
	t.Parallel()
	s, holder, _ := loadKeyUTF8Schema(t)
	const bad = "a\xffb"

	g := graph.New(s)
	if res := g.Add(t.Context(), instancetest.VI("Holder", instancetest.TypeID(holder), instancetest.PK("h1"),
		instancetest.Props(map[string]any{"id": "h1", "text": bad}))); !res.OK() {
		t.Fatalf("Add: %s", res)
	}
	rebuilt, res := graph.RebuildSnapshot(s, graph.SnapshotParts{Instances: []graph.InstanceParts{{
		TypeID:     holder,
		PrimaryKey: immutable.WrapKey([]any{"h1"}),
		Properties: immutable.WrapProperties(map[string]any{"id": "h1", "text": bad}),
	}}})
	if rebuilt == nil {
		t.Fatalf("RebuildSnapshot: %s", res)
	}
	for name, snap := range map[string]*graph.Snapshot{"Add": g.Snapshot(), "RebuildSnapshot": rebuilt} {
		inst, ok := snap.InstanceByKey(holder, `["h1"]`)
		if !ok {
			t.Fatalf("%s: no instance at h1", name)
		}
		if got, _ := inst.Property("text"); got.Unwrap() != bad {
			t.Errorf("%s holds text %q, want %q", name, got.Unwrap(), bad)
		}
	}
}
