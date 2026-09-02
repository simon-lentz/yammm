package csv

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func loadTestSchema(t *testing.T, name string) *schema.Schema {
	t.Helper()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)
	s, result := schema.Load(context.Background(), filepath.Join("testdata", name))
	if err := result.Err(); err != nil {
		t.Fatalf("load schema %s: %v", name, err)
	}
	return s
}

func TestParseTyped_Basic(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	a := New()

	input := "id,name,count,score,active,created_at\ne1,Alice,5,3.14,true,2024-06-15\ne2,Bob,10,2.71,false,2024-01-01\n"
	results, result := a.ParseTyped(context.Background(), location.SourceID{}, "Entity", strings.NewReader(input), st)
	require.True(t, result.OK(), result.String())
	require.Len(t, results, 2)

	// First row.
	p := results[0].Properties
	assert.Equal(t, "e1", p["id"])
	assert.Equal(t, "Alice", p["name"])
	assert.Equal(t, int64(5), p["count"])
	assert.Equal(t, 3.14, p["score"])
	assert.Equal(t, true, p["active"])
	assert.Equal(t, "2024-06-15", p["created_at"])
}

func TestParseTyped_NullHandling(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	a := New()

	input := "id,name,count\ne1,Alice,\n"
	results, result := a.ParseTyped(context.Background(), location.SourceID{}, "Entity", strings.NewReader(input), st)
	require.True(t, result.OK(), result.String())
	require.Len(t, results, 1)

	assert.Nil(t, results[0].Properties["count"])
}

func TestParseTyped_CoercionError(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	a := New()

	// "abc" is not a valid Integer.
	input := "id,name,count\ne1,Alice,abc\n"
	results, result := a.ParseTyped(context.Background(), location.SourceID{}, "Entity", strings.NewReader(input), st)

	// Should still return a result (with the raw string as fallback).
	require.Len(t, results, 1)
	assert.Equal(t, "abc", results[0].Properties["count"]) // raw fallback

	// Should have a diagnostic.
	assert.False(t, result.OK())
}

func TestParseTyped_ListProperty(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	a := New()

	input := "id,name,tags\ne1,Alice,a|b|c\n"
	results, result := a.ParseTyped(context.Background(), location.SourceID{}, "Entity", strings.NewReader(input), st)
	require.True(t, result.OK(), result.String())
	require.Len(t, results, 1)
	assert.Equal(t, []any{"a", "b", "c"}, results[0].Properties["tags"])
}

func TestParseTyped_NilSchemaType(t *testing.T) {
	t.Parallel()
	a := New()

	input := "id,count\ne1,42\n"
	results, result := a.ParseTyped(context.Background(), location.SourceID{}, "Entity", strings.NewReader(input), nil)
	require.True(t, result.OK(), result.String())
	require.Len(t, results, 1)

	// Without schema, values stay as strings.
	assert.Equal(t, "42", results[0].Properties["count"])
}

func TestParseWithTypeColumn_Basic(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "with_relations.yammm")
	a := New(WithTypeColumn("$type"))

	input := "$type,company_id,name,employee_id,works_at\nCompany,c1,Acme,,\nEmployee,,Alice,emp1,c1\n"
	resolver := func(name string) *schema.Type {
		st, _ := s.Type(name)
		return st
	}

	results, result := a.ParseWithTypeColumn(context.Background(), location.SourceID{}, strings.NewReader(input), resolver)
	require.True(t, result.OK(), result.String())

	assert.Len(t, results["Company"], 1)
	assert.Equal(t, "c1", results["Company"][0].Properties["company_id"])

	assert.Len(t, results["Employee"], 1)
	assert.Equal(t, "emp1", results["Employee"][0].Properties["employee_id"])
}

func TestParseWithTypeColumn_NoTypeColumnConfigured(t *testing.T) {
	t.Parallel()
	a := New() // no WithTypeColumn

	_, result := a.ParseWithTypeColumn(context.Background(), location.SourceID{}, strings.NewReader(""), nil)
	assert.False(t, result.OK())
}

func TestParseTyped_ContextCancellation(t *testing.T) {
	t.Parallel()
	a := New()

	// Multi-row input; cancel context immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	input := "id,name\ne1,Alice\ne2,Bob\n"
	_, result := a.ParseTyped(ctx, location.SourceID{}, "Entity", strings.NewReader(input), nil)
	assert.False(t, result.OK())

	// Should contain a cancellation diagnostic.
	found := false
	for issue := range result.Issues() {
		if issue.Code().String() == "E_CONTEXT_CANCELLED" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected E_CONTEXT_CANCELLED diagnostic")
}

func TestParseTyped_HeaderOnlyEmptyCSV(t *testing.T) {
	t.Parallel()
	a := New()

	input := "id,name\n"
	results, result := a.ParseTyped(context.Background(), location.SourceID{}, "Entity", strings.NewReader(input), nil)
	require.True(t, result.OK(), result.String())
	assert.Empty(t, results, "header-only CSV should produce zero instances")
}

func TestParseTyped_FewerColumnsThanHeader(t *testing.T) {
	t.Parallel()
	a := New()

	// Row has only 1 value but header has 3 columns.
	// Go's csv.Reader enforces consistent field counts by default,
	// so this produces a diagnostic error and the row is skipped.
	input := "id,name,count\ne1\n"
	results, result := a.ParseTyped(context.Background(), location.SourceID{}, "Entity", strings.NewReader(input), nil)

	assert.False(t, result.OK(), "mismatched column count should produce a diagnostic")
	assert.Empty(t, results, "malformed row should be skipped")
}
