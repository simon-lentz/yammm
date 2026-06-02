package snapshottest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot/snapshottest"
)

// buildTestSchema is a Person + Company schema with optional employer —
// shape matches batch_assembler_test.go's fixture so the helper exercises
// the same validator + graph + finalize paths.
func buildTestSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
		WithName("snapshottest-build").
		WithSourceID(location.MustNewSourceID("test://snapshottest-build.yammm")).
		AddType("Company").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("employer", schema.LocalTypeRef("Company", location.Span{}), true, false).
		Done().
		Build()
	require.False(t, result.HasErrors(), "buildTestSchema: %s", result.String())
	return s
}

// requiredEmployerSchema flips employer to required so a Person without
// a corresponding Company surfaces an unresolved-required Check failure
// — used by the Finalize-failure test below.
func requiredEmployerSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
		WithName("snapshottest-build-req").
		WithSourceID(location.MustNewSourceID("test://snapshottest-build-req.yammm")).
		AddType("Company").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("employer", schema.LocalTypeRef("Company", location.Span{}), false, false).
		Done().
		Build()
	require.False(t, result.HasErrors())
	return s
}

func TestBuildTestSnapshot_HappyPath(t *testing.T) {
	s := buildTestSchema(t)

	snap := snapshottest.BuildTestSnapshot(
		t, s,
		snapshottest.Record{
			TypeName: "Company",
			Raw:      instance.RawInstance{Properties: map[string]any{"id": "acme", "name": "ACME"}},
		},
		snapshottest.Record{
			TypeName: "Person",
			Raw:      instance.RawInstance{Properties: map[string]any{"id": "alice", "name": "Alice"}},
		},
	)

	require.NotNil(t, snap)
	assert.Equal(t, 1, len(snap.InstancesOf("Company")))
	assert.Equal(t, 1, len(snap.InstancesOf("Person")))
	assert.False(t, snap.Diagnostics().HasErrors(),
		"happy-path snapshot has unexpected diagnostics: %s", snap.Diagnostics().String())
}

func TestBuildTestSnapshot_AddFailure_Fatal(t *testing.T) {
	s := buildTestSchema(t)

	rec := &recordingTB{TB: t}
	// Bad Person: missing required `id` property — ValidateOne fires
	// E_MISSING_REQUIRED, BatchAssembler.Add returns *ErrorWithContext,
	// BuildTestSnapshot fatals.
	bad := snapshottest.Record{
		TypeName: "Person",
		Raw:      instance.RawInstance{Properties: map[string]any{"name": "no-id"}},
	}
	snapshottest.BuildTestSnapshot(rec, s, bad)

	assert.True(t, rec.fatalled, "expected Fatalf to fire on validation failure")
	require.NotEmpty(t, rec.errors)
	assert.True(t, strings.Contains(rec.errors[0], "BuildTestSnapshot: Add[0] (Person)"),
		"error message did not include expected prefix; got: %s", rec.errors[0])
}

func TestBuildTestSnapshot_FinalizeFailure_Fatal(t *testing.T) {
	s := requiredEmployerSchema(t)

	rec := &recordingTB{TB: t}
	// Person with employer reference to a Company that won't be added —
	// Check fires E_UNRESOLVED_REQUIRED, Finalize returns
	// *ErrorWithContext tagged "batch_finalize", BuildTestSnapshot
	// fatals.
	snapshottest.BuildTestSnapshot(
		rec, s,
		snapshottest.Record{
			TypeName: "Person",
			Raw: instance.RawInstance{Properties: map[string]any{
				"id":       "alice",
				"name":     "Alice",
				"employer": map[string]any{"_target_id": "missing"},
			}},
		},
	)

	assert.True(t, rec.fatalled, "expected Fatalf to fire on Check failure")
	require.NotEmpty(t, rec.errors)
	assert.True(t, strings.Contains(rec.errors[0], "BuildTestSnapshot: Finalize"),
		"error message did not include expected prefix; got: %s", rec.errors[0])
}
