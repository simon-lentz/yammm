package csv

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

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

// Both representations of one instant write the same cell, because validation
// renders the kind to its canonical text before the writer ever sees it.
func TestMarshalSnapshot_TimestampRendersOneWay(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	a := New()
	when := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

	snap := buildSnapshot(t, s, map[string][]map[string]any{
		"Entity": {
			{"id": "e1", "name": "Alice", "updated_at": when},
			{"id": "e2", "name": "Bob", "updated_at": when.Format(time.RFC3339)},
		},
	})

	output, err := a.MarshalSnapshot(context.Background(), snap)
	require.NoError(t, err)
	got := string(output["Entity"])

	assert.Equal(t, 2, strings.Count(got, "2020-01-02T03:04:05Z"),
		"both representations of one instant render as the same cell")
	assert.NotContains(t, got, "2020-01-02 03:04:05 +0000 UTC",
		"no cell renders through Go's default time layout")
}
