package graph

import (
	"maps"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

// canonicalizerSchema declares one type whose key canonicalizes and one whose
// key does not, in a schema that declares a canonicalizing kind, so the
// canonicalizer is active for both.
func canonicalizerSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, res := schema.NewBuilder().
		WithName("canon").
		WithSourceID(location.MustNewSourceID("test://canon.yammm")).
		AddType("Plain").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Stamped").
		WithPrimaryKey("observed_at", schema.TimestampConstraint{}).
		Done().
		Build()
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	return s
}

// TestCanonicalizerKey_AllocatesNothingForAPlainKey pins the type doc's
// promise for the key path: a type whose key has no canonical form costs one
// map lookup per instance and allocates nothing.
func TestCanonicalizerKey_AllocatesNothingForAPlainKey(t *testing.T) {
	s := canonicalizerSchema(t)
	plain, _ := s.Type("Plain")
	c := newCanonicalizer(s)
	k := immutable.WrapKey([]any{"x"})
	c.key(plain.ID(), k) // populates the memo
	if allocs := testing.AllocsPerRun(100, func() { c.key(plain.ID(), k) }); allocs != 0 {
		t.Errorf("key allocates %v per call on a type whose key does not canonicalize; want 0", allocs)
	}
}

// TestCanonicalizerKey_MemoisesPerType pins that the key path memoises the
// canonicalizing key positions as typeProps and edgeProps memoise theirs.
func TestCanonicalizerKey_MemoisesPerType(t *testing.T) {
	s := canonicalizerSchema(t)
	stamped, _ := s.Type("Stamped")
	plain, _ := s.Type("Plain")
	c := newCanonicalizer(s)

	if got := c.key(stamped.ID(), immutable.WrapKey([]any{"2020-01-02T03:04:05+00:00"})).String(); got != `["2020-01-02T03:04:05Z"]` {
		t.Fatalf("canonical key = %s", got)
	}
	positions, ok := c.byKeyType[stamped.ID()]
	if !ok || len(positions) != 1 || positions[0].index != 0 {
		t.Errorf("byKeyType[Stamped] = %v, %v; want one position at index 0", positions, ok)
	}
	c.key(plain.ID(), immutable.WrapKey([]any{"x"}))
	if positions, ok := c.byKeyType[plain.ID()]; !ok || len(positions) != 0 {
		t.Errorf("byKeyType[Plain] = %v, %v; want an empty entry", positions, ok)
	}
}

// TestCanonicalizerAddress_PlainKeyAllocatesNothing pins that a received
// address for a type whose key does not canonicalize is returned unparsed, so
// [Snapshot.InstanceByKey] on a String key stays one map read.
func TestCanonicalizerAddress_PlainKeyAllocatesNothing(t *testing.T) {
	s := canonicalizerSchema(t)
	plain, _ := s.Type("Plain")
	stamped, _ := s.Type("Stamped")
	c := newCanonicalizer(s)
	if allocs := testing.AllocsPerRun(100, func() { c.address(plain.ID(), `["x"]`) }); allocs != 0 {
		t.Errorf("address allocates %v per call on a type whose key does not canonicalize; want 0", allocs)
	}
	if got := c.address(stamped.ID(), `["2020-01-02T03:04:05+00:00"]`); got != `["2020-01-02T03:04:05Z"]` {
		t.Errorf("address on a Timestamp key = %s, want the canonical rendering", got)
	}
	if got := c.address(stamped.ID(), "not an address"); got != "not an address" {
		t.Errorf("address on a string ParseKey refuses = %q, want it as spelled", got)
	}
}

// TestRebuildSnapshot_IndexKeyIsTheCanonicalKey pins the index's own keying,
// which a lookup that canonicalizes its address cannot show: a rebuilt
// Timestamp-keyed instance entered by a raw spelling is indexed once, under
// the canonical text, on the rebuild path and on the Add path alike.
func TestRebuildSnapshot_IndexKeyIsTheCanonicalKey(t *testing.T) {
	s := canonicalizerSchema(t)
	stamped, _ := s.Type("Stamped")
	const raw, canonical = "2020-01-02T03:04:05+00:00", `["2020-01-02T03:04:05Z"]`
	rebuilt, res := RebuildSnapshot(s, SnapshotParts{
		Types: []schema.TypeID{stamped.ID()},
		Instances: map[schema.TypeID][]InstanceParts{stamped.ID(): {{
			TypeName: "Stamped", TypeID: stamped.ID(), PrimaryKey: immutable.WrapKey([]any{raw}),
			Properties: immutable.WrapProperties(map[string]any{"observed_at": raw}),
		}}},
	})
	if res.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", res)
	}
	g := New(s)
	if r := g.Add(t.Context(), instance.NewValidInstance("Stamped", stamped.ID(), immutable.WrapKey([]any{raw}),
		immutable.WrapProperties(map[string]any{"observed_at": raw}), nil, nil, nil)); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	for name, snap := range map[string]*Snapshot{"RebuildSnapshot": rebuilt, "Add": g.Snapshot()} {
		idx := snap.instanceIndex[stamped.ID()]
		if len(idx) != 1 {
			t.Errorf("%s path: index holds %d entries, want 1", name, len(idx))
		}
		if _, ok := idx[canonical]; !ok {
			t.Errorf("%s path: index keys = %v, want %s", name, slices.Collect(maps.Keys(idx)), canonical)
		}
	}
}

// TestCompareDuplicates_LoadedRecordsOrderBySourceName pins the tie-break's
// behaviour on the reload path: a loaded instance carries a provenance with a
// source name and a zero span, so two otherwise identical records order by
// source name and tie within one.
func TestCompareDuplicates_LoadedRecordsOrderBySourceName(t *testing.T) {
	id := schema.NewTypeID(location.MustNewSourceID("test://dup.yammm"), "T")
	key := immutable.WrapKey([]any{"k"})
	conflict := newInstance("T", id, key, immutable.WrapProperties(nil), nil, false)
	// The shape the decoder produces, built here because graph cannot import
	// snapshot. That a LOADED instance actually has this shape is pinned on the
	// snapshot side, by TestLoad_ProvenanceSurvivesOnlyWhenTheDocumentHadOne.
	loaded := func(source string) *Duplicate {
		prov := location.NewProvenance(source, path.Root(), location.Span{})
		inst := newInstance("T", id, key, immutable.WrapProperties(nil), prov, false)
		return newDuplicate(inst, conflict, nil, "", diag.Issue{})
	}
	if c := compareDuplicates(loaded("a.json"), loaded("b.json")); c >= 0 {
		t.Errorf("a.json against b.json = %d; want negative, ordered by source name", c)
	}
	if c := compareDuplicates(loaded("a.json"), loaded("a.json")); c != 0 {
		t.Errorf("a.json against a.json = %d; want a tie within one source name", c)
	}
}
