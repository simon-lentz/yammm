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

// E2: Test that Registry.All() returns schemas in deterministic order
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
