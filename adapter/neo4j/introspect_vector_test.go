package neo4j

import (
	"strings"
	"testing"
)

// The vector-introspection tests live here rather than in introspect_test.go
// because that file is grandfathered under the repo's testify freeze
// (.golangci.yml, linters.exclusions.rules), whose comment states the list only
// shrinks and that no file is ever added to it. Adding new testify call sites to
// a grandfathered file passes depguard — the import already exists — while
// moving the freeze's end state further away, so new test code goes in a
// testify-free file instead.

func parseOneIndex(t *testing.T, rec map[string]any) RemoteIndex {
	t.Helper()
	indexes, err := ParseRemoteIndexes([]map[string]any{rec})
	if err != nil {
		t.Fatalf("ParseRemoteIndexes: %v", err)
	}
	if len(indexes) != 1 {
		t.Fatalf("parsed %d indexes, want 1", len(indexes))
	}
	return indexes[0]
}

func TestParseRemoteIndexes_VectorOptions(t *testing.T) {
	t.Parallel()
	idx := parseOneIndex(t, map[string]any{
		"name":          "index_test__Document_embedding_vector_idx",
		"type":          "VECTOR",
		"entityType":    "NODE",
		"labelsOrTypes": []any{"index_test__Document"},
		"properties":    []any{"embedding"},
		"options": map[string]any{
			"indexProvider": "vector-2.0",
			"indexConfig": map[string]any{
				"vector.dimensions":          int64(768),
				"vector.similarity_function": "cosine",
			},
		},
	})

	dim, ok := idx.VectorDimensions()
	if !ok || dim != 768 {
		t.Errorf("VectorDimensions() = %d, %v; want 768, true", dim, ok)
	}
	sim, ok := idx.VectorSimilarity()
	if !ok || sim != "cosine" {
		t.Errorf("VectorSimilarity() = %q, %v; want \"cosine\", true", sim, ok)
	}
}

func TestRemoteIndex_VectorOptions_Absent(t *testing.T) {
	t.Parallel()
	idx := parseOneIndex(t, map[string]any{
		"name":          "idx_range",
		"type":          "RANGE",
		"entityType":    "NODE",
		"labelsOrTypes": []any{"A"},
		"properties":    []any{"x"},
	})

	if _, ok := idx.VectorDimensions(); ok {
		t.Error("VectorDimensions() should report unreadable for an index with no options")
	}
	if _, ok := idx.VectorSimilarity(); ok {
		t.Error("VectorSimilarity() should report unreadable for an index with no options")
	}
}

func TestRemoteIndex_VectorOptions_Malformed(t *testing.T) {
	t.Parallel()
	idx := parseOneIndex(t, map[string]any{
		"name":          "idx_weird",
		"type":          "VECTOR",
		"entityType":    "NODE",
		"labelsOrTypes": []any{"A"},
		"properties":    []any{"v"},
		"options":       map[string]any{"indexConfig": "not-a-map"},
	})

	if _, ok := idx.VectorDimensions(); ok {
		t.Error("VectorDimensions() should report unreadable for a malformed indexConfig")
	}
	if _, ok := idx.VectorSimilarity(); ok {
		t.Error("VectorSimilarity() should report unreadable for a malformed indexConfig")
	}
}

// A dimension the code cannot interpret exactly must read as unreadable, not as
// a truncated integer: the caller's next step is a drift comparison, and a
// truncated value compares equal to a dimension the database does not have.
func TestRemoteIndex_VectorDimensions_NonIntegralFloat(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		val  any
		want bool
	}{
		{"driver int64", int64(768), true},
		{"plain int", 768, true},
		{"integral float64", float64(768), true},
		{"fractional float64", 768.9, false},
		{"string", "768", false},
		{"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			idx := parseOneIndex(t, map[string]any{
				"name":          "v",
				"type":          "VECTOR",
				"entityType":    "NODE",
				"labelsOrTypes": []any{"A"},
				"properties":    []any{"v"},
				"options": map[string]any{
					"indexConfig": map[string]any{"vector.dimensions": tc.val},
				},
			})
			if _, ok := idx.VectorDimensions(); ok != tc.want {
				t.Errorf("VectorDimensions() readable = %v; want %v for %T(%v)", ok, tc.want, tc.val, tc.val)
			}
		})
	}
}

// ParseRemoteIndexes must copy the options map, INCLUDING the nested
// indexConfig. That nested map holds the only values this package reads through
// Options, so a shallow copy leaves exactly the drift-comparison inputs aliased
// to the caller's record — and a caller that pools or rewrites its records
// between polls then mutates every RemoteIndex already parsed, including the
// copies filed under Match and Drop.
func TestParseRemoteIndexes_OptionsAreDeepCopied(t *testing.T) {
	t.Parallel()
	inner := map[string]any{
		"vector.dimensions":          int64(768),
		"vector.similarity_function": "cosine",
	}
	outer := map[string]any{"indexConfig": inner}
	idx := parseOneIndex(t, map[string]any{
		"name": "v", "type": "VECTOR", "entityType": "NODE",
		"labelsOrTypes": []any{"A"}, "properties": []any{"v"}, "options": outer,
	})

	// Mutate the NESTED map the caller still holds. A shallow copy shares it.
	inner["vector.dimensions"] = int64(1)
	inner["vector.similarity_function"] = "euclidean"
	// ...and the outer one, which even a shallow copy must survive.
	delete(outer, "indexConfig")

	dim, ok := idx.VectorDimensions()
	if !ok || dim != 768 {
		t.Errorf("VectorDimensions() = %d, %v after mutating the caller's nested map; want 768, true", dim, ok)
	}
	sim, ok := idx.VectorSimilarity()
	if !ok || sim != "cosine" {
		t.Errorf("VectorSimilarity() = %q, %v after mutating the caller's nested map; want \"cosine\", true", sim, ok)
	}
}

// A float64 dimension outside the int range must read as unreadable rather than
// saturate. float64(math.MaxInt) rounds UP to 2^63, so a guard written as
// `n > math.MaxInt` is false at exactly 2^63 — the one value most likely to
// arrive from a driver that widened the column.
func TestRemoteIndex_VectorDimensions_OutOfRangeFloat(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		val  float64
	}{
		{"2^63", float64(1 << 63)},
		{"beyond 2^63", float64(1<<63) * 2},
		{"below -2^63", -float64(1<<63) * 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			idx := parseOneIndex(t, map[string]any{
				"name": "v", "type": "VECTOR", "entityType": "NODE",
				"labelsOrTypes": []any{"A"}, "properties": []any{"v"},
				"options": map[string]any{
					"indexConfig": map[string]any{"vector.dimensions": tc.val},
				},
			})
			if got, ok := idx.VectorDimensions(); ok {
				t.Errorf("VectorDimensions() = %d, true for %v; a value outside the int range must read as unreadable", got, tc.val)
			}
		})
	}
}

func TestIntrospectIndexesQuery_YieldsOptionsAndState(t *testing.T) {
	t.Parallel()
	q := IntrospectIndexesQuery()
	for _, want := range []string{
		"options",          // vector configuration, for drift detection
		"state",            // FAILED/POPULATING must not read as in sync
		"owningConstraint", // constraint-backing indexes must be visible as blockers
		"type <> 'LOOKUP'",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("IntrospectIndexesQuery() is missing %q:\n%s", want, q)
		}
	}
	// Constraint-backing indexes must NOT be filtered out server-side: they block
	// a CREATE both by name and by definition, and filtering them would put both
	// blocks outside what the diff can observe.
	if strings.Contains(q, "owningConstraint IS NULL") {
		t.Errorf("IntrospectIndexesQuery() still filters out constraint-backing indexes:\n%s", q)
	}
}

// Index options genuinely contain lists — a POINT index reports spatial bounds
// as lists of doubles — so copying only maps would leave the values a later
// drift comparison reads aliased to the caller's record.
func TestParseRemoteIndexes_ListOptionsAreCopied(t *testing.T) {
	t.Parallel()
	bounds := []any{-100.0, -100.0}
	cfg := map[string]any{"spatial.cartesian.min": bounds}
	idx := parseOneIndex(t, map[string]any{
		"name": "p", "type": "POINT", "entityType": "NODE",
		"labelsOrTypes": []any{"A"}, "properties": []any{"loc"},
		"options": map[string]any{"indexConfig": cfg},
	})

	bounds[0] = 999.0
	got, _ := idx.Options["indexConfig"].(map[string]any)
	list, _ := got["spatial.cartesian.min"].([]any)
	if len(list) == 0 || list[0] != -100.0 {
		t.Errorf("mutating the caller's list changed the parsed options: %v", list)
	}
}
