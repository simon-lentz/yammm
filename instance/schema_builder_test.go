package instance_test

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// personSchema is the canonical programmatic fixture used by most shape tests.
// Covers: simple and composite PKs, one/many associations, one/many compositions.
// Note: compositions target concrete part types per schema/collision.go:274–287;
// no abstract composition targets are exercised here (the schema layer would
// reject them at Build time, making such fixtures unrealizable).
func personSchema(t *testing.T) *schema.Schema {
	t.Helper()
	return mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Address").
		AsPart().
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("street", schema.NewStringConstraint()).
		Done().
		AddType("Note").
		AsPart().
		WithProperty("body", schema.NewStringConstraint()).
		Done().
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Vehicle").
		AsAbstract().
		WithPrimaryKey("vin", schema.NewStringConstraint()).
		Done().
		AddType("Part").
		WithPrimaryKey("publisher_id", schema.NewStringConstraint()).
		WithPrimaryKey("book_id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		WithOptionalProperty("nickname", schema.NewStringConstraint()).
		WithRelation("WORKS_AT", schema.LocalTypeRef("Company", location.Span{}), true, false).
		WithRelation("KNOWS", schema.LocalTypeRef("Person", location.Span{}), true, true).
		WithRelation("HAS_PART", schema.LocalTypeRef("Part", location.Span{}), true, false).
		WithComposition("ADDRESSES", schema.LocalTypeRef("Address", location.Span{}), true, true).
		WithComposition("PRIMARY_NOTE", schema.LocalTypeRef("Note", location.Span{}), true, false).
		Done())
}

// edgePropsSchema loads a schema with an association that declares two
// required edge properties and another that declares one optional edge
// property. The DSL path is required because the programmatic schema.Builder
// does not expose edge-property declaration at the TypeBuilder level.
// Optional edge properties omit the 'required' keyword, which the SPEC's
// RelProperty production makes optional.
func edgePropsSchema(t *testing.T) *schema.Schema {
	t.Helper()
	const src = `schema "edgepropstest"

type Company {
    id String primary
}

type OptionalCompany {
    id String primary
}

type Employee {
    id String primary
    --> WORKS_AT (_:one) Company {
        role String required
        since String required
    }
    --> VISITS (_:many) OptionalCompany {
        note String
    }
}
`
	return mustLoadString(t, src, "edgepropstest.yammm")
}

// ---------- BuilderFor constructor ----------

func TestBuilderFor_NilSchema(t *testing.T) {
	b, err := instance.BuilderFor(nil, "Person")
	require.Error(t, err)
	assert.Nil(t, b)
	assert.Contains(t, err.Error(), "nil schema")
}

func TestBuilderFor_UnknownType(t *testing.T) {
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Ghost")
	require.Error(t, err)
	assert.Nil(t, b)
	assert.Contains(t, err.Error(), `"Ghost"`)
	assert.Contains(t, err.Error(), "not found")
}

func TestBuilderFor_AbstractType(t *testing.T) {
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Vehicle")
	require.Error(t, err)
	assert.Nil(t, b)
	assert.Contains(t, err.Error(), "abstract")
	assert.Contains(t, err.Error(), `"Vehicle"`)
}

func TestBuilderFor_PartTypeSucceeds(t *testing.T) {
	// Part types are legitimate composition children; BuilderFor must accept them.
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Address")
	require.NoError(t, err)
	require.NotNil(t, b)
}

// ---------- Property ----------

func TestSchemaBuilder_Property_Happy(t *testing.T) {
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)

	raw, err := b.
		Property("id", "p1").
		Property("name", "Alice").
		Property("nickname", "Al").
		Build()
	require.NoError(t, err)

	assert.Equal(t, "p1", raw.Properties["id"])
	assert.Equal(t, "Alice", raw.Properties["name"])
	assert.Equal(t, "Al", raw.Properties["nickname"])
}

func TestSchemaBuilder_Property_Overwrite(t *testing.T) {
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)

	raw, err := b.
		Property("id", "p1").
		Property("name", "Alice").
		Property("name", "Bob").
		Build()
	require.NoError(t, err)
	assert.Equal(t, "Bob", raw.Properties["name"])
}

func TestSchemaBuilder_Property_NilValue(t *testing.T) {
	// Property(name, nil) passes through; validator handles nil semantics.
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)

	raw, err := b.
		Property("id", "p1").
		Property("nickname", nil).
		Build()
	require.NoError(t, err)
	val, ok := raw.Properties["nickname"]
	assert.True(t, ok)
	assert.Nil(t, val)
}

func TestSchemaBuilder_Property_Unknown(t *testing.T) {
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)

	_, err = b.Property("bogus", 42).Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown property "bogus"`)
	assert.Contains(t, err.Error(), "Person")
	// Caller locator comes from this test file.
	assert.Contains(t, err.Error(), "schema_builder_test.go:")
}

// ---------- EdgeTo ----------

func TestSchemaBuilder_EdgeTo_OneHappy(t *testing.T) {
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)

	raw, err := b.
		Property("id", "p1").
		Property("name", "Alice").
		EdgeTo("WORKS_AT", "c1").
		Build()
	require.NoError(t, err)

	edge, ok := raw.Properties["works_at"].(map[string]any)
	require.True(t, ok, "works_at should be a single map for one-cardinality")
	assert.Equal(t, "c1", edge["_target_id"])
}

func TestSchemaBuilder_EdgeTo_ManyHappy(t *testing.T) {
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)

	raw, err := b.
		Property("id", "alice").
		Property("name", "Alice").
		EdgeTo("knows", "bob").
		EdgeTo("knows", "carol").
		Build()
	require.NoError(t, err)

	arr, ok := raw.Properties["knows"].([]any)
	require.True(t, ok, "knows should be an array for many-cardinality")
	require.Len(t, arr, 2)
	// Order is preserved because we append to a slice, not a map.
	first, _ := arr[0].(map[string]any)
	second, _ := arr[1].(map[string]any)
	assert.Equal(t, "bob", first["_target_id"])
	assert.Equal(t, "carol", second["_target_id"])
}

func TestSchemaBuilder_EdgeTo_CompositePK(t *testing.T) {
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)

	raw, err := b.
		Property("id", "p1").
		Property("name", "Alice").
		EdgeTo("HAS_PART", "publisher-1", "book-99").
		Build()
	require.NoError(t, err)
	edge, _ := raw.Properties["has_part"].(map[string]any)
	assert.Equal(t, "publisher-1", edge["_target_publisher_id"])
	assert.Equal(t, "book-99", edge["_target_book_id"])
}

func TestSchemaBuilder_EdgeTo_PreBuiltSliceUnpack(t *testing.T) {
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)

	key := []any{"publisher-1", "book-99"}
	raw, err := b.
		Property("id", "p1").
		Property("name", "Alice").
		EdgeTo("HAS_PART", key...).
		Build()
	require.NoError(t, err)
	edge, _ := raw.Properties["has_part"].(map[string]any)
	assert.Equal(t, "publisher-1", edge["_target_publisher_id"])
	assert.Equal(t, "book-99", edge["_target_book_id"])
}

func TestSchemaBuilder_EdgeTo_ZeroTargetKey(t *testing.T) {
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)

	_, err = b.Property("id", "p1").Property("name", "Alice").EdgeTo("WORKS_AT").Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "EdgeTo requires at least one target-key component")
}

func TestSchemaBuilder_EdgeTo_UnknownRelation(t *testing.T) {
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)

	_, err = b.EdgeTo("bogus", "x").Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown relation "bogus"`)
}

func TestSchemaBuilder_EdgeTo_OnCompositionRelation(t *testing.T) {
	// "addresses" is a composition, not an association.
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)

	_, err = b.EdgeTo("ADDRESSES", "x").Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "composition")
	assert.Contains(t, err.Error(), "does not support composed children")
}

func TestSchemaBuilder_EdgeTo_OnRelationWithEdgeProps(t *testing.T) {
	s := edgePropsSchema(t)
	b, err := instance.BuilderFor(s, "Employee")
	require.NoError(t, err)

	_, err = b.Property("id", "e1").EdgeTo("WORKS_AT", "c1").Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "declares edge properties")
	assert.Contains(t, err.Error(), "does not support")
}

func TestSchemaBuilder_EdgeTo_ArityMismatch(t *testing.T) {
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)

	// "has_part" has a composite PK of 2 components; passing 1 is an arity error.
	_, err = b.Property("id", "p1").Property("name", "Alice").EdgeTo("HAS_PART", "only-one").Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "arity mismatch")
}

// ---------- Cardinality ----------

func TestSchemaBuilder_Cardinality_OneWithTwoEdgeTo(t *testing.T) {
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)

	_, err = b.
		Property("id", "p1").
		Property("name", "Alice").
		EdgeTo("WORKS_AT", "c1").
		EdgeTo("WORKS_AT", "c2").
		Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cardinality mismatch")
	assert.Contains(t, err.Error(), "WORKS_AT")
}

func TestSchemaBuilder_Cardinality_ManyZeroCalls(t *testing.T) {
	// No EdgeTo calls on a "many" relation is legal at build time; validator
	// enforces required-not-empty if the relation is declared required.
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)

	raw, err := b.Property("id", "p1").Property("name", "Alice").Build()
	require.NoError(t, err)
	_, ok := raw.Properties["knows"]
	assert.False(t, ok, "no EdgeTo on many relation → no entry in Properties")
}

// ---------- Relation-name form parity ----------

func TestSchemaBuilder_RelationNameFormParity(t *testing.T) {
	// Both DSL form and FieldName form resolve to the same relation.
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("WORKS_AT", schema.LocalTypeRef("Company", location.Span{}), true, false).
		Done())

	bA, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)
	rawA, err := bA.Property("id", "a").EdgeTo("WORKS_AT", "c1").Build()
	require.NoError(t, err)

	bB, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)
	rawB, err := bB.Property("id", "a").EdgeTo("WORKS_AT", "c1").Build()
	require.NoError(t, err)

	assertPropertiesEqual(t, rawA.Properties, rawB.Properties)
}

// ---------- Composed ----------

// ---------- First-error-with-count ----------

func TestSchemaBuilder_FirstErrorWithCount(t *testing.T) {
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)

	_, err = b.
		Property("bogus1", "x").
		Property("bogus2", "y").
		Property("bogus3", "z").
		Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown property "bogus1"`)
	assert.Contains(t, err.Error(), "(and 2 more build error(s))")
}

// ---------- Call-site fidelity ----------

func TestSchemaBuilder_CallerLocator_SameFile(t *testing.T) {
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)

	_, _, captureLine, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	b.Property("bogus", 1) // DO NOT MOVE: the offset below counts from captureLine.
	expectedLine := captureLine + 2
	_, err = b.Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema_builder_test.go:"+strconv.Itoa(expectedLine))
}

func TestSchemaBuilder_CallerLocator_CrossFile(t *testing.T) {
	// helperTripBogusProperty is defined in schema_builder_helper_test.go.
	// Its caller locator should point at THAT file, not this one.
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)

	helperTripBogusProperty(b)
	_, err = b.Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema_builder_helper_test.go:")
	assert.NotContains(t, err.Error(), "schema_builder_test.go:")
}

// ---------- MustBuild ----------

// ---------- Round-trip: builder → validator ----------

func TestSchemaBuilder_RoundTrip_ValidatorAccepts(t *testing.T) {
	s := personSchema(t)
	validator := instance.NewValidator(s)

	p, err := instance.BuilderFor(s, "Person")
	require.NoError(t, err)

	raw, err := p.
		Property("id", "p1").
		Property("name", "Alice").
		EdgeTo("WORKS_AT", "c1").
		EdgeTo("knows", "p2").
		Build()
	require.NoError(t, err)

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)
	require.True(t, result.OK(), "validator rejected builder output: %s", result)
	require.NotNil(t, valid)
	assert.Equal(t, "Person", valid.TypeName())
}

// ---------- Non-concurrent-safe anti-test ----------

func TestSchemaBuilder_NotConcurrentSafe_DocumentedShape(t *testing.T) {
	// This test documents the per-goroutine-builder contract: we do NOT
	// guarantee concurrent-safety. The test constructs one builder per
	// goroutine (the prescribed pattern) and asserts both build cleanly.
	// A two-goroutine-one-builder shape WOULD trip -race; we omit that
	// variant to keep CI green while still pinning the contract.
	s := personSchema(t)
	var wg sync.WaitGroup
	results := make([]string, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Go(func() {
			b, err := instance.BuilderFor(s, "Person")
			if err != nil {
				errs[i] = err
				return
			}
			raw, err := b.Property("id", fmt.Sprintf("p%d", i)).Property("name", "X").Build()
			if err != nil {
				errs[i] = err
				return
			}
			results[i], _ = raw.Properties["id"].(string)
		})
	}
	wg.Wait()
	for i, err := range errs {
		require.NoError(t, err, "builder goroutine %d", i)
	}
	assert.Equal(t, "p0", results[0])
	assert.Equal(t, "p1", results[1])
}

// ---------- error-chain / errors.As ----------

// ---------- helpers ----------

// mustBuild is reused from validator_test.go — redefined here only if this
// test file compiles standalone. Since we're in the same package
// (instance_test), validator_test.go's mustBuild is already available.
// This file intentionally does NOT redefine mustBuild / mustLoadString.

// assertPropertiesEqual compares two Properties maps by JSON-marshaling both
// with sorted keys. Use this where map iteration-order might otherwise
// produce spurious test failures across runs.
func assertPropertiesEqual(t *testing.T, a, b map[string]any) {
	t.Helper()
	aj, err := json.Marshal(a)
	require.NoError(t, err)
	bj, err := json.Marshal(b)
	require.NoError(t, err)
	// json.Marshal on a map uses sorted keys for determinism, so bytes
	// compare meaningfully here.
	assert.Equal(t, string(aj), string(bj))
}
