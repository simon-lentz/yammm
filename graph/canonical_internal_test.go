package graph

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
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

// TestCompareDuplicates_LoadedRecordsOrderBySourceName pins the tie-break's
// behaviour on the reload path: a loaded instance carries a provenance with a
// source name and a zero span, so two otherwise identical records order by
// source name and tie within one.
func TestCompareDuplicates_LoadedRecordsOrderBySourceName(t *testing.T) {
	id := schema.NewTypeID(location.MustNewSourceID("test://dup.yammm"), "T")
	key := immutable.WrapKey([]any{"k"})
	conflict := newInstance("T", id, key, immutable.WrapProperties(nil), nil, false)
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
