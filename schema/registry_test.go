package schema_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func TestRegistry_New(t *testing.T) {
	r := schema.NewRegistry()
	require.NotNil(t, r)
	assert.Equal(t, 0, r.Len())
}

func TestRegistry_Register(t *testing.T) {
	r := schema.NewRegistry()

	srcID := location.MustNewSourceID("test://test.yammm")
	s := schema.TestNewSchema("test", srcID, location.Span{}, "")

	err := r.Register(s)
	require.NoError(t, err)
	assert.Equal(t, 1, r.Len())
}

func TestRegistry_Register_Nil(t *testing.T) {
	r := schema.NewRegistry()

	err := r.Register(nil)
	require.NoError(t, err)
	assert.Equal(t, 0, r.Len())
}

func TestRegistry_Register_DuplicateSourceID(t *testing.T) {
	r := schema.NewRegistry()

	srcID := location.MustNewSourceID("test://test.yammm")
	s1 := schema.TestNewSchema("test1", srcID, location.Span{}, "")
	s2 := schema.TestNewSchema("test2", srcID, location.Span{}, "") // Same SourceID

	err := r.Register(s1)
	require.NoError(t, err)

	err = r.Register(s2)
	require.Error(t, err)

	var regErr *schema.RegistryError
	require.ErrorAs(t, err, &regErr)
	assert.Equal(t, schema.DuplicateSourceID, regErr.Kind)
}

func TestRegistry_Register_DuplicateName(t *testing.T) {
	r := schema.NewRegistry()

	s1 := schema.TestNewSchema("test", location.MustNewSourceID("test://a.yammm"), location.Span{}, "")
	s2 := schema.TestNewSchema("test", location.MustNewSourceID("test://b.yammm"), location.Span{}, "") // Same name

	err := r.Register(s1)
	require.NoError(t, err)

	err = r.Register(s2)
	require.Error(t, err)

	var regErr *schema.RegistryError
	require.ErrorAs(t, err, &regErr)
	assert.Equal(t, schema.DuplicateName, regErr.Kind)
}

func TestRegistry_LookupBySourceID(t *testing.T) {
	r := schema.NewRegistry()

	srcID := location.MustNewSourceID("test://test.yammm")
	s := schema.TestNewSchema("test", srcID, location.Span{}, "")
	_ = r.Register(s)

	found, ok := r.LookupBySourceID(srcID)
	assert.True(t, ok)
	assert.Same(t, s, found)

	_, ok = r.LookupBySourceID(location.MustNewSourceID("test://other.yammm"))
	assert.False(t, ok)
}

func TestRegistry_LookupByName(t *testing.T) {
	r := schema.NewRegistry()

	srcID := location.MustNewSourceID("test://test.yammm")
	s := schema.TestNewSchema("myschema", srcID, location.Span{}, "")
	_ = r.Register(s)

	found, ok := r.LookupByName("myschema")
	assert.True(t, ok)
	assert.Same(t, s, found)

	_, ok = r.LookupByName("other")
	assert.False(t, ok)
}

func TestRegistry_LookupType(t *testing.T) {
	r := schema.NewRegistry()

	srcID := location.MustNewSourceID("test://test.yammm")
	s := schema.TestNewSchema("test", srcID, location.Span{}, "")

	// Create a type and add it to the schema
	typ := schema.TestNewType("Person", srcID, location.Span{}, "", false, false)
	schema.TestSetSchemaTypes(s, []*schema.Type{typ})

	_ = r.Register(s)

	// Lookup by TypeID
	typeID := typ.ID()
	found, ok := r.LookupType(typeID)
	assert.True(t, ok)
	assert.Same(t, typ, found)

	// Lookup non-existent type
	fakeID := schema.NewTypeID(location.MustNewSourceID("test://other.yammm"), "Fake")
	_, ok = r.LookupType(fakeID)
	assert.False(t, ok)
}

func TestRegistry_Contains(t *testing.T) {
	r := schema.NewRegistry()

	srcID := location.MustNewSourceID("test://test.yammm")
	s := schema.TestNewSchema("test", srcID, location.Span{}, "")
	_ = r.Register(s)

	assert.True(t, r.Contains(srcID))
	assert.False(t, r.Contains(location.MustNewSourceID("test://other.yammm")))
}

func TestRegistry_Clone(t *testing.T) {
	r := schema.NewRegistry()

	srcID := location.MustNewSourceID("test://test.yammm")
	s := schema.TestNewSchema("test", srcID, location.Span{}, "")
	_ = r.Register(s)

	clone := r.Clone()
	require.NotNil(t, clone)
	assert.Equal(t, r.Len(), clone.Len())

	// Clone should have the same schema
	found, ok := clone.LookupBySourceID(srcID)
	assert.True(t, ok)
	assert.Same(t, s, found)

	// Modifying clone should not affect original
	srcID2 := location.MustNewSourceID("test://other.yammm")
	s2 := schema.TestNewSchema("other", srcID2, location.Span{}, "")
	_ = clone.Register(s2)

	assert.Equal(t, 2, clone.Len())
	assert.Equal(t, 1, r.Len()) // Original unchanged
}

func TestRegistry_All(t *testing.T) {
	r := schema.NewRegistry()

	s1 := schema.TestNewSchema("s1", location.MustNewSourceID("test://a.yammm"), location.Span{}, "")
	s2 := schema.TestNewSchema("s2", location.MustNewSourceID("test://b.yammm"), location.Span{}, "")
	_ = r.Register(s1)
	_ = r.Register(s2)

	all := r.All()
	assert.Len(t, all, 2)
}

// Test that Registry.All() returns schemas in deterministic order.
func TestRegistry_All_OrderDeterminism(t *testing.T) {
	r := schema.NewRegistry()

	// Register multiple schemas
	s1 := schema.TestNewSchema("alpha", location.MustNewSourceID("test://a.yammm"), location.Span{}, "")
	s2 := schema.TestNewSchema("beta", location.MustNewSourceID("test://b.yammm"), location.Span{}, "")
	s3 := schema.TestNewSchema("gamma", location.MustNewSourceID("test://c.yammm"), location.Span{}, "")
	_ = r.Register(s1)
	_ = r.Register(s2)
	_ = r.Register(s3)

	// Get first snapshot
	first := r.All()
	require.Len(t, first, 3)

	// Get second snapshot
	second := r.All()
	require.Len(t, second, 3)

	// Order should be deterministic across calls
	for i := range first {
		if first[i].Name() != second[i].Name() {
			t.Errorf("All() order not deterministic: first[%d].Name()=%q, second[%d].Name()=%q",
				i, first[i].Name(), i, second[i].Name())
		}
		if first[i].SourceID() != second[i].SourceID() {
			t.Errorf("All() order not deterministic: first[%d].SourceID()=%v, second[%d].SourceID()=%v",
				i, first[i].SourceID(), i, second[i].SourceID())
		}
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := schema.NewRegistry()

	// Pre-register some schemas
	for i := range 10 {
		srcID := location.MustNewSourceID("test://pre" + string(rune('a'+i)) + ".yammm")
		s := schema.TestNewSchema("pre"+string(rune('a'+i)), srcID, location.Span{}, "")
		_ = r.Register(s)
	}

	var wg sync.WaitGroup
	errChan := make(chan error, 100)

	// Concurrent readers
	for i := range 10 {
		wg.Go(func() {
			for range 100 {
				srcID := location.MustNewSourceID(fmt.Sprintf("test://pre%c.yammm", 'a'+i))
				_, _ = r.LookupBySourceID(srcID)
				_ = r.Len()
			}
		})
	}

	// Wait for all goroutines
	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		t.Errorf("concurrent access error: %v", err)
	}
}

func TestRegistryErrorKind_String(t *testing.T) {
	assert.Equal(t, "duplicate source ID", schema.DuplicateSourceID.String())
	assert.Equal(t, "duplicate name", schema.DuplicateName.String())
	assert.Equal(t, "invalid source ID", schema.InvalidSourceID.String())
	assert.Equal(t, "invalid name", schema.InvalidName.String())
}

func TestRegistry_Register_ZeroSourceID(t *testing.T) {
	r := schema.NewRegistry()

	// Schema with zero SourceID should be rejected
	s := schema.TestNewSchema("test", location.SourceID{}, location.Span{}, "")

	err := r.Register(s)
	require.Error(t, err)

	var regErr *schema.RegistryError
	require.ErrorAs(t, err, &regErr)
	assert.Equal(t, schema.InvalidSourceID, regErr.Kind)
}

func TestRegistry_Register_EmptyName(t *testing.T) {
	r := schema.NewRegistry()

	// Schema with empty name should be rejected
	srcID := location.MustNewSourceID("test://test.yammm")
	s := schema.TestNewSchema("", srcID, location.Span{}, "")

	err := r.Register(s)
	require.Error(t, err)

	var regErr *schema.RegistryError
	require.ErrorAs(t, err, &regErr)
	assert.Equal(t, schema.InvalidName, regErr.Kind)
}

// newIdenticalSchemaPair constructs two distinct *Schema pointers carrying
// identical SourceID, name, and types — so StructuralHash(s1) == StructuralHash(s2).
// Used by the idempotence tests.
func newIdenticalSchemaPair(tb testing.TB, srcID location.SourceID, schemaName, typeName string) (s1, s2 *schema.Schema) {
	tb.Helper()
	s1 = schema.TestNewSchema(schemaName, srcID, location.Span{}, "")
	t1 := schema.TestNewType(typeName, srcID, location.Span{}, "", false, false)
	schema.TestSetSchemaTypes(s1, []*schema.Type{t1})

	s2 = schema.TestNewSchema(schemaName, srcID, location.Span{}, "")
	t2 := schema.TestNewType(typeName, srcID, location.Span{}, "", false, false)
	schema.TestSetSchemaTypes(s2, []*schema.Type{t2})
	return s1, s2
}

func TestRegistry_Register_IdempotentExactMatch(t *testing.T) {
	r := schema.NewRegistry()
	srcID := location.MustNewSourceID("test://idempotent.yammm")

	s1, s2 := newIdenticalSchemaPair(t, srcID, "shared", "Person")

	// Sanity: the two schemas must have identical StructuralHash for the
	// idempotence path to fire.
	require.Equal(t, schema.StructuralHash(s1), schema.StructuralHash(s2),
		"test fixture invariant: identical schemas must hash identically")

	require.NoError(t, r.Register(s1))
	require.NoError(t, r.Register(s2), "idempotent re-registration must succeed as no-op")
	assert.Equal(t, 1, r.Len(), "registry must not double-count")

	// The registry must retain the first pointer (not the fresh one).
	found, ok := r.LookupBySourceID(srcID)
	require.True(t, ok)
	assert.Same(t, s1, found, "registry must retain the first-registered pointer on idempotent re-register")
}

func TestRegistry_Register_IdempotentNoTypeReindex(t *testing.T) {
	r := schema.NewRegistry()
	srcID := location.MustNewSourceID("test://noindex.yammm")

	s1 := schema.TestNewSchema("shared", srcID, location.Span{}, "")
	t1 := schema.TestNewType("Person", srcID, location.Span{}, "", false, false)
	schema.TestSetSchemaTypes(s1, []*schema.Type{t1})

	s2 := schema.TestNewSchema("shared", srcID, location.Span{}, "")
	t2 := schema.TestNewType("Person", srcID, location.Span{}, "", false, false)
	schema.TestSetSchemaTypes(s2, []*schema.Type{t2})

	require.NotSame(t, t1, t2, "fresh Type pointer required to observe re-index")

	require.NoError(t, r.Register(s1))
	require.NoError(t, r.Register(s2), "idempotent re-register must succeed")

	// byTypeID must still point to the original type, not the freshly-created
	// one. This pins that the idempotence path skips the re-index loop.
	found, ok := r.LookupType(t1.ID())
	require.True(t, ok)
	assert.Same(t, t1, found, "type pointer identity must survive idempotent re-register")
}

func TestRegistry_Register_ContentDivergence(t *testing.T) {
	r := schema.NewRegistry()
	srcID := location.MustNewSourceID("test://divergent.yammm")

	// Two schemas with the same SourceID but divergent type names produce
	// different StructuralHashes (hash.go writes the type name).
	s1 := schema.TestNewSchema("shared", srcID, location.Span{}, "")
	t1 := schema.TestNewType("Person", srcID, location.Span{}, "", false, false)
	schema.TestSetSchemaTypes(s1, []*schema.Type{t1})

	s2 := schema.TestNewSchema("shared", srcID, location.Span{}, "")
	t2 := schema.TestNewType("Vehicle", srcID, location.Span{}, "", false, false)
	schema.TestSetSchemaTypes(s2, []*schema.Type{t2})

	hash1 := schema.StructuralHash(s1)
	hash2 := schema.StructuralHash(s2)
	require.NotEqual(t, hash1, hash2, "fixture invariant: divergent content must hash differently")

	require.NoError(t, r.Register(s1))

	err := r.Register(s2)
	require.Error(t, err, "divergent re-registration must still error")

	var regErr *schema.RegistryError
	require.ErrorAs(t, err, &regErr)
	assert.Equal(t, schema.DuplicateSourceID, regErr.Kind)

	// Pin the diagnostic message shape. The "schema already registered with
	// source ID: <id>" prefix is preserved verbatim for backward compatibility
	// with consumers doing strings.Contains(err.Error(), "already registered
	// with source ID"). Hash details append as a suffix.
	msg := err.Error()
	assert.Contains(t, msg, "schema already registered with source ID:",
		"backward-compatibility prefix must survive the hash-diff addition")
	assert.Contains(t, msg, srcID.String(), "SourceID must appear in the error message")
	assert.Contains(t, msg, "content divergence", "message must name the divergence mode")
	assert.Contains(t, msg, "existing=", "existing hash must be named")
	assert.Contains(t, msg, "incoming=", "incoming hash must be named")
	assert.Contains(t, msg, hash1, "existing structural hash must appear")
	assert.Contains(t, msg, hash2, "incoming structural hash must appear")
	assert.Contains(t, msg, "sha256:",
		"hash format prefix must be preserved so log consumers can parse it")

	// The divergent registration must not mutate the registry.
	assert.Equal(t, 1, r.Len(), "divergent registration must not insert or overwrite")
	found, ok := r.LookupBySourceID(srcID)
	require.True(t, ok)
	assert.Same(t, s1, found, "registry must still hold the first-registered schema")
}

// Sanity regression: the pre-existing TestRegistry_Register_DuplicateSourceID
// and TestRegistry_Register_DuplicateName tests continue to exercise divergent
// re-registration (the prior tests use different schema names, which makes
// the hash differ via hash.go:30's writeStr(h, s.Name()) call). No update
// to those tests is required for PR-7.

func TestRegistry_Register_IdempotenceDoesNotWeakenNameCollision(t *testing.T) {
	// Same name, different SourceID → DuplicateName must still fire.
	// Pins that the idempotence-on-SourceID change did not accidentally
	// affect the name-collision path.
	r := schema.NewRegistry()

	s1 := schema.TestNewSchema("shared", location.MustNewSourceID("test://a.yammm"), location.Span{}, "")
	s2 := schema.TestNewSchema("shared", location.MustNewSourceID("test://b.yammm"), location.Span{}, "")

	require.NoError(t, r.Register(s1))

	err := r.Register(s2)
	require.Error(t, err)

	var regErr *schema.RegistryError
	require.ErrorAs(t, err, &regErr)
	assert.Equal(t, schema.DuplicateName, regErr.Kind,
		"name collisions must continue to fire DuplicateName, not DuplicateSourceID")
}

// --- Benchmarks (PR-7) ---
//
// Four benchmarks measure the cost of Register across two dimensions:
// schema size (tiny vs synthetic-large) and path (fresh registration vs
// idempotent re-register). The idempotent path pays two StructuralHash
// computations per call; the fresh path pays zero. The Large pair reports
// an "idempotent/fresh" ratio via b.ReportMetric so complexity-ceiling
// regressions surface in the benchmark output.

func BenchmarkRegisterFresh_TinySchema(b *testing.B) {
	s := buildTinyBenchSchema(b)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		r := schema.NewRegistry()
		if err := r.Register(s); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

func BenchmarkRegisterIdempotent_TinySchema(b *testing.B) {
	r := schema.NewRegistry()
	s1 := buildTinyBenchSchema(b)
	if err := r.Register(s1); err != nil {
		b.Fatalf("seed registration failed: %v", err)
	}
	s2 := buildTinyBenchSchema(b)
	if schema.StructuralHash(s1) != schema.StructuralHash(s2) {
		b.Fatal("idempotent-bench invariant: fresh and re-built schemas must hash identically")
	}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		if err := r.Register(s2); err != nil {
			b.Fatalf("idempotent re-register must succeed: %v", err)
		}
	}
}

func BenchmarkRegisterFresh_LargeSchema(b *testing.B) {
	s := buildLargeBenchSchema(b)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		r := schema.NewRegistry()
		if err := r.Register(s); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

func BenchmarkRegisterIdempotent_LargeSchema(b *testing.B) {
	// Run paired with BenchmarkRegisterFresh_LargeSchema via
	//   go test -bench 'BenchmarkRegister.*_LargeSchema' ./schema
	// to observe the idempotent-vs-fresh cost ratio side-by-side. The
	// idempotent path pays 2× StructuralHash on a ~120-type schema; the
	// fresh path pays zero hash cost but performs the full type-index
	// insert. An inline ratio-emission via testing.Benchmark is avoided
	// here: invoking testing.Benchmark from inside a benchmark deadlocks
	// on testing's internal lock. Tools like benchstat compute the ratio
	// cleanly from the paired ns/op output; this is the plan doc's
	// intended diagnostic (not a CI gate — variance on shared runners
	// would flake any tight threshold).
	r := schema.NewRegistry()
	s1 := buildLargeBenchSchema(b)
	if err := r.Register(s1); err != nil {
		b.Fatalf("seed registration failed: %v", err)
	}
	s2 := buildLargeBenchSchema(b)
	if schema.StructuralHash(s1) != schema.StructuralHash(s2) {
		b.Fatal("idempotent-bench invariant: fresh and re-built schemas must hash identically")
	}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		if err := r.Register(s2); err != nil {
			b.Fatalf("idempotent re-register must succeed: %v", err)
		}
	}
}
