package neo4j

import (
	"slices"
	"strings"
	"testing"
)

// vectorOptions builds a driver-shaped options map for a VECTOR index.
func vectorOptions(dim int, sim string) map[string]any {
	return map[string]any{
		"indexConfig": map[string]any{
			"vector.dimensions":          int64(dim),
			"vector.similarity_function": sim,
		},
	}
}

// wantCounts asserts the four-way classification sizes.
func wantCounts(t *testing.T, r *IndexDiffResult, match, drift, create, drop int) {
	t.Helper()
	if len(r.Match) != match || len(r.Drift) != drift || len(r.Create) != create || len(r.Drop) != drop {
		t.Errorf("counts match/drift/create/drop = %d/%d/%d/%d; want %d/%d/%d/%d",
			len(r.Match), len(r.Drift), len(r.Create), len(r.Drop), match, drift, create, drop)
	}
}

func TestDiffIndexes_AllMatch(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Index{
		{Kind: IndexRange, Label: "test__Entity", Properties: []string{"state"}},
		{Kind: IndexVector, Label: "test__Entity", Properties: []string{"embedding"}, VectorDimensions: 768, VectorSimilarity: "cosine"},
	}
	actual := []RemoteIndex{
		{Name: "i1", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"state"}},
		{Name: "i2", Type: "VECTOR", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"embedding"}, Options: vectorOptions(768, "cosine")},
	}

	wantCounts(t, a.DiffIndexes(desired, actual, "test"), 2, 0, 0, 0)
}

func TestDiffIndexes_CreateNew(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Index{
		{Kind: IndexRange, Label: "test__Entity", Properties: []string{"state"}},
		{Kind: IndexRange, Label: "test__Entity", Properties: []string{"published_on"}},
	}
	actual := []RemoteIndex{
		{Name: "i1", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"state"}},
	}

	result := a.DiffIndexes(desired, actual, "test")
	wantCounts(t, result, 1, 0, 1, 0)
	if !slices.Equal(result.Create[0].Properties, []string{"published_on"}) {
		t.Errorf("Create[0].Properties = %v; want [published_on]", result.Create[0].Properties)
	}
}

func TestDiffIndexes_DropOrphaned(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Index{
		{Kind: IndexRange, Label: "test__Entity", Properties: []string{"state"}},
	}
	actual := []RemoteIndex{
		{Name: "i1", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"state"}},
		{Name: "i2", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"old_field"}},
	}

	result := a.DiffIndexes(desired, actual, "test")
	wantCounts(t, result, 1, 0, 0, 1)
	if result.Drop[0].Properties[0] != "old_field" {
		t.Errorf("Drop[0] property = %q; want old_field", result.Drop[0].Properties[0])
	}
}

// TestDiffIndexes_OrderSensitive pins the deliberate divergence from the
// constraint diff: a composite index whose remote property order differs is a
// distinct index — Create + Drop, not Match.
func TestDiffIndexes_OrderSensitive(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Index{
		{Kind: IndexRange, Label: "test__Entity", Properties: []string{"a", "b"}},
	}
	actual := []RemoteIndex{
		{Name: "i1", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"b", "a"}},
	}

	wantCounts(t, a.DiffIndexes(desired, actual, "test"), 0, 0, 1, 1)
}

func TestDiffIndexes_VectorDimensionDrift(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Index{
		{Kind: IndexVector, Label: "test__Entity", Properties: []string{"embedding"}, VectorDimensions: 768, VectorSimilarity: "cosine"},
	}
	actual := []RemoteIndex{
		{Name: "i1", Type: "VECTOR", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"embedding"}, Options: vectorOptions(512, "cosine")},
	}

	result := a.DiffIndexes(desired, actual, "test")
	wantCounts(t, result, 0, 1, 0, 0)
	if !strings.Contains(result.Drift[0].Reason, "dimension") {
		t.Errorf("Drift reason = %q; want a dimension mismatch", result.Drift[0].Reason)
	}
}

func TestDiffIndexes_VectorSimilarityDrift(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Index{
		{Kind: IndexVector, Label: "test__Entity", Properties: []string{"embedding"}, VectorDimensions: 768, VectorSimilarity: "cosine"},
	}
	actual := []RemoteIndex{
		{Name: "i1", Type: "VECTOR", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"embedding"}, Options: vectorOptions(768, "euclidean")},
	}

	result := a.DiffIndexes(desired, actual, "test")
	wantCounts(t, result, 0, 1, 0, 0)
	if !strings.Contains(result.Drift[0].Reason, "similarity") {
		t.Errorf("Drift reason = %q; want a similarity mismatch", result.Drift[0].Reason)
	}
}

// TestDiffIndexes_VectorMissingConfig_NoDrift pins that a remote vector index
// without readable config is not claimed as drift (older-server tolerance): it
// matches on the semantic key.
func TestDiffIndexes_VectorMissingConfig_NoDrift(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Index{
		{Kind: IndexVector, Label: "test__Entity", Properties: []string{"embedding"}, VectorDimensions: 768, VectorSimilarity: "cosine"},
	}
	actual := []RemoteIndex{
		{Name: "i1", Type: "VECTOR", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"embedding"}},
	}

	wantCounts(t, a.DiffIndexes(desired, actual, "test"), 1, 0, 0, 0)
}

func TestDiffIndexes_FiltersNonOwned(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Index{
		{Kind: IndexRange, Label: "test__Entity", Properties: []string{"state"}},
	}
	actual := []RemoteIndex{
		{Name: "i1", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"state"}},
		{Name: "other", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"other_schema__Foo"}, Properties: []string{"x"}},
	}

	// The other-schema index is neither matched nor reported as a drop.
	wantCounts(t, a.DiffIndexes(desired, actual, "test"), 1, 0, 0, 0)
}

// TestDiffIndexes_EmptyDesired_AllDrop pins the always-on drift behavior: a
// schema-owned remote index with no declaration is reported as a drop.
func TestDiffIndexes_EmptyDesired_AllDrop(t *testing.T) {
	t.Parallel()
	a := New()

	actual := []RemoteIndex{
		{Name: "i1", Type: "RANGE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"state"}},
		{Name: "i2", Type: "VECTOR", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"embedding"}, Options: vectorOptions(768, "cosine")},
	}

	wantCounts(t, a.DiffIndexes(nil, actual, "test"), 0, 0, 0, 2)
}

func TestDiffIndexes_Empty(t *testing.T) {
	t.Parallel()
	a := New()
	wantCounts(t, a.DiffIndexes(nil, nil, "test"), 0, 0, 0, 0)
}
