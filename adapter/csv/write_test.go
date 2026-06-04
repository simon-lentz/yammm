package csv

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func validateInstances(t *testing.T, s *schema.Schema, typeName string, records []map[string]any) []*instance.ValidInstance {
	t.Helper()
	v := instance.NewValidator(s)
	var valids []*instance.ValidInstance
	for _, props := range records {
		valid, result := v.ValidateOne(context.Background(), typeName, instance.RawInstance{Properties: props})
		if !result.OK() {
			t.Fatalf("validate %s: %v", typeName, result)
		}
		valids = append(valids, valid)
	}
	return valids
}

func buildSnapshot(t *testing.T, s *schema.Schema, instances map[string][]map[string]any) *graph.Snapshot {
	t.Helper()
	v := instance.NewValidator(s)
	g := graph.New(s)
	for typeName, records := range instances {
		for _, props := range records {
			valid, result := v.ValidateOne(context.Background(), typeName, instance.RawInstance{Properties: props})
			if !result.OK() {
				t.Fatalf("validate %s: %v", typeName, result)
			}
			addResult := g.Add(context.Background(), valid)
			if err := addResult.Err(); err != nil {
				t.Fatalf("graph.Add %s: %v", typeName, err)
			}
		}
	}
	return g.Snapshot()
}

func TestMarshalTyped_Basic(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	a := New()

	valids := validateInstances(t, s, "Entity", []map[string]any{
		{"id": "e1", "name": "Alice", "count": int64(5), "score": 3.14, "active": true, "created_at": "2024-06-15", "updated_at": "2024-01-01T00:00:00Z"},
	})

	data, err := a.MarshalTyped(context.Background(), valids, st)
	require.NoError(t, err)

	output := string(data)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	require.Len(t, lines, 2) // header + 1 row

	// Header should include property columns.
	assert.Contains(t, lines[0], "id")
	assert.Contains(t, lines[0], "name")

	// Data row should include values.
	assert.Contains(t, lines[1], "e1")
	assert.Contains(t, lines[1], "Alice")
}

func TestMarshalTyped_NoHeader(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	a := New()

	valids := validateInstances(t, s, "Entity", []map[string]any{
		{"id": "e1", "name": "Alice"},
	})

	data, err := a.MarshalTyped(context.Background(), valids, st, WithWriteHeader(false))
	require.NoError(t, err)

	output := string(data)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	assert.Len(t, lines, 1) // no header
}

func TestWriteTyped_Basic(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	a := New()

	valids := validateInstances(t, s, "Entity", []map[string]any{
		{"id": "e1", "name": "Alice"},
	})

	var buf bytes.Buffer
	n, err := a.WriteTyped(context.Background(), &buf, valids, st)
	require.NoError(t, err)
	assert.Greater(t, n, int64(0))
	assert.Contains(t, buf.String(), "e1")
}

func TestMarshalSnapshot_Basic(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	a := New()

	snap := buildSnapshot(t, s, map[string][]map[string]any{
		"Entity": {
			{"id": "e1", "name": "Alice"},
			{"id": "e2", "name": "Bob"},
		},
	})

	output, err := a.MarshalSnapshot(context.Background(), snap)
	require.NoError(t, err)

	entityCSV, ok := output["Entity"]
	require.True(t, ok, "expected Entity key in output")
	assert.Contains(t, string(entityCSV), "e1")
	assert.Contains(t, string(entityCSV), "e2")
}

func TestMarshalSnapshot_NilSnapshot(t *testing.T) {
	t.Parallel()
	a := New()
	_, err := a.MarshalSnapshot(context.Background(), nil)
	assert.ErrorIs(t, err, ErrNilSnapshot)
}

func TestWriteSnapshot_Basic(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	a := New()

	snap := buildSnapshot(t, s, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "Alice"}},
	})

	writers := make(map[string]*bytes.Buffer)
	err := a.WriteSnapshot(context.Background(), func(typeName string) (io.Writer, error) {
		buf := &bytes.Buffer{}
		writers[typeName] = buf
		return buf, nil
	}, snap)
	require.NoError(t, err)

	entityBuf, ok := writers["Entity"]
	require.True(t, ok)
	assert.Contains(t, entityBuf.String(), "e1")
}

func TestRoundTrip_ParseThenWrite(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	a := New()

	// Parse CSV input.
	input := "id,name\ne1,Alice\ne2,Bob\n"
	parsed, result := a.ParseTyped(context.Background(), location.SourceID{}, "Entity", strings.NewReader(input), st)
	require.True(t, result.OK(), result.String())

	// Validate parsed instances.
	v := instance.NewValidator(s)
	var valids []*instance.ValidInstance
	for _, raw := range parsed {
		valid, vResult := v.ValidateOne(context.Background(), "Entity", raw)
		require.True(t, vResult.OK(), vResult.String())
		valids = append(valids, valid)
	}

	// Write back to CSV.
	data, err := a.MarshalTyped(context.Background(), valids, st)
	require.NoError(t, err)

	output := string(data)
	assert.Contains(t, output, "e1")
	assert.Contains(t, output, "Alice")
	assert.Contains(t, output, "e2")
	assert.Contains(t, output, "Bob")
}

func TestMarshalTyped_WithFKColumns(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "with_relations.yammm")
	st, _ := s.Type("Employee")
	a := New()

	// Validate an Employee with an FK reference.
	v := instance.NewValidator(s)
	valid, result := v.ValidateOne(context.Background(), "Employee", instance.RawInstance{
		Properties: map[string]any{
			"employee_id": "emp1",
			"name":        "Alice",
			"works_at":    map[string]any{"_target_company_id": "c1"},
		},
	})
	require.True(t, result.OK(), result.String())

	data, err := a.MarshalTyped(context.Background(), []*instance.ValidInstance{valid}, st)
	require.NoError(t, err)

	output := string(data)
	// Header should include the FK field name.
	assert.Contains(t, output, "works_at")
	// Data row should contain the FK value.
	assert.Contains(t, output, "c1")
}

func TestMarshalSnapshot_FKColumnsEmpty(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "with_relations.yammm")
	a := New()

	// Build snapshot with both Company and Employee (edge resolves in graph).
	snap := buildSnapshot(t, s, map[string][]map[string]any{
		"Company":  {{"company_id": "c1", "name": "Acme"}},
		"Employee": {{"employee_id": "emp1", "name": "Alice", "works_at": map[string]any{"_target_company_id": "c1"}}},
	})

	output, err := a.MarshalSnapshot(context.Background(), snap)
	require.NoError(t, err)

	// Employee CSV should exist.
	empCSV, ok := output["Employee"]
	require.True(t, ok)
	assert.Contains(t, string(empCSV), "emp1")
	assert.Contains(t, string(empCSV), "Alice")
}
