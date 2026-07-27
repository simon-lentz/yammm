package neo4j

import (
	"strings"
	"testing"
)

const returnCountSuffix = "\nRETURN count(*) AS matched_rows"

// -----------------------------------------------------------------------------
// KeyMutability branch-coverage tests — two tests per node builder, pinned via
// golden-string equality. A future implementation change that alters the
// emitted Cypher surface (e.g., accidental switch from SET to SET DISTINCT,
// dropping the ON MATCH clause, reordering the SET statements) fails the
// golden comparison explicitly rather than silently breaking write semantics.
// -----------------------------------------------------------------------------

func TestBuildNodeMergeQuery_MutableKeys_Golden(t *testing.T) {
	t.Parallel()
	got := BuildNodeMergeQuery("ns__Type", []string{"a", "b"}, MutableKeys)
	want := "MERGE (n:ns__Type {a: $key_a, b: $key_b})\nSET n += $props"
	if got != want {
		t.Errorf("MutableKeys shape mismatch\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(got, "ON CREATE SET") || strings.Contains(got, "ON MATCH SET") {
		t.Errorf("MutableKeys must not emit ON CREATE / ON MATCH split: %q", got)
	}
}

func TestBuildNodeMergeQuery_ImmutableKeys_Golden(t *testing.T) {
	t.Parallel()
	got := BuildNodeMergeQuery("ns__Type", []string{"a", "b"}, ImmutableKeys)
	want := "MERGE (n:ns__Type {a: $key_a, b: $key_b})\n" +
		"ON CREATE SET n += $props\n" +
		"ON MATCH SET n += $update_props"
	if got != want {
		t.Errorf("ImmutableKeys shape mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestBuildBatchNodeMergeQuery_MutableKeys_Golden(t *testing.T) {
	t.Parallel()
	got := BuildBatchNodeMergeQuery("ns__Type", []string{"a", "b"}, MutableKeys)
	want := "UNWIND $rows AS row\n" +
		"MERGE (n:ns__Type {a: row.key_a, b: row.key_b})\n" +
		"SET n += row.props"
	if got != want {
		t.Errorf("MutableKeys batch shape mismatch\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(got, "ON CREATE SET") || strings.Contains(got, "ON MATCH SET") {
		t.Errorf("MutableKeys batch must not emit ON CREATE / ON MATCH split: %q", got)
	}
}

func TestBuildBatchNodeMergeQuery_ImmutableKeys_Golden(t *testing.T) {
	t.Parallel()
	got := BuildBatchNodeMergeQuery("ns__Type", []string{"a", "b"}, ImmutableKeys)
	want := "UNWIND $rows AS row\n" +
		"MERGE (n:ns__Type {a: row.key_a, b: row.key_b})\n" +
		"ON CREATE SET n += row.props\n" +
		"ON MATCH SET n += row.update_props"
	if got != want {
		t.Errorf("ImmutableKeys batch shape mismatch\n got: %q\nwant: %q", got, want)
	}
}

// -----------------------------------------------------------------------------
// RETURN count(*) AS matched_rows suffix pins — the per-call / per-chunk-value
// contract documented on the relationship builders. A future implementation
// change that accidentally drops the RETURN, renames the column, or promotes
// it to an aggregation-expression would silently break a link-engine
// silent-failure safety net; these pins catch it at the builder-test level.
// Parallel pins on the node builders preserve the "nodes stay RETURN-free"
// design intent.
// -----------------------------------------------------------------------------

func TestBuildRelationshipMergeQuery_ReturnSuffix(t *testing.T) {
	t.Parallel()
	for _, hasProps := range []bool{false, true} {
		got := BuildRelationshipMergeQuery(
			"A", []string{"x"},
			"KNOWS",
			"B", []string{"y"},
			hasProps,
		)
		if !strings.HasSuffix(got, returnCountSuffix) {
			t.Errorf("hasProps=%t: expected suffix %q, got %q", hasProps, returnCountSuffix, got)
		}
	}
}

func TestBuildBatchRelationshipMergeQuery_ReturnSuffix(t *testing.T) {
	t.Parallel()
	for _, hasProps := range []bool{false, true} {
		got := BuildBatchRelationshipMergeQuery(
			"A", []string{"x"},
			"KNOWS",
			"B", []string{"y"},
			hasProps,
		)
		if !strings.HasSuffix(got, returnCountSuffix) {
			t.Errorf("hasProps=%t: expected suffix %q, got %q", hasProps, returnCountSuffix, got)
		}
	}
}

func TestBuildRelationshipMergeQuery_NoProps_Golden(t *testing.T) {
	t.Parallel()
	got := BuildRelationshipMergeQuery(
		"A", []string{"x"},
		"KNOWS",
		"B", []string{"y"},
		false,
	)
	want := "MATCH (from:A {x: $from_key_x})\n" +
		"MATCH (to:B {y: $to_key_y})\n" +
		"MERGE (from)-[r:KNOWS]->(to)\n" +
		"RETURN count(*) AS matched_rows"
	if got != want {
		t.Errorf("shape mismatch\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(got, "SET r +=") {
		t.Errorf("hasProps=false must not emit SET r += clause: %q", got)
	}
}

func TestBuildRelationshipMergeQuery_WithProps_Golden(t *testing.T) {
	t.Parallel()
	got := BuildRelationshipMergeQuery(
		"A", []string{"x"},
		"KNOWS",
		"B", []string{"y"},
		true,
	)
	want := "MATCH (from:A {x: $from_key_x})\n" +
		"MATCH (to:B {y: $to_key_y})\n" +
		"MERGE (from)-[r:KNOWS]->(to)\n" +
		"SET r += $rel_props\n" +
		"RETURN count(*) AS matched_rows"
	if got != want {
		t.Errorf("shape mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestBuildBatchRelationshipMergeQuery_NoProps_Golden(t *testing.T) {
	t.Parallel()
	got := BuildBatchRelationshipMergeQuery(
		"A", []string{"x"},
		"KNOWS",
		"B", []string{"y"},
		false,
	)
	want := "UNWIND $rows AS row\n" +
		"MATCH (from:A {x: row.from_x})\n" +
		"MATCH (to:B {y: row.to_y})\n" +
		"MERGE (from)-[r:KNOWS]->(to)\n" +
		"RETURN count(*) AS matched_rows"
	if got != want {
		t.Errorf("shape mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestBuildBatchRelationshipMergeQuery_WithProps_Golden(t *testing.T) {
	t.Parallel()
	got := BuildBatchRelationshipMergeQuery(
		"A", []string{"x"},
		"KNOWS",
		"B", []string{"y"},
		true,
	)
	want := "UNWIND $rows AS row\n" +
		"MATCH (from:A {x: row.from_x})\n" +
		"MATCH (to:B {y: row.to_y})\n" +
		"MERGE (from)-[r:KNOWS]->(to)\n" +
		"SET r += row.rel_props\n" +
		"RETURN count(*) AS matched_rows"
	if got != want {
		t.Errorf("shape mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestBuildNodeMergeQuery_NoReturn(t *testing.T) {
	t.Parallel()
	for _, km := range []KeyMutability{MutableKeys, ImmutableKeys} {
		got := BuildNodeMergeQuery("Entity", []string{"id"}, km)
		if strings.Contains(got, "RETURN") {
			t.Errorf("km=%d: nodes must stay RETURN-free, got: %q", km, got)
		}
	}
}

func TestBuildBatchNodeMergeQuery_NoReturn(t *testing.T) {
	t.Parallel()
	for _, km := range []KeyMutability{MutableKeys, ImmutableKeys} {
		got := BuildBatchNodeMergeQuery("Entity", []string{"id"}, km)
		if strings.Contains(got, "RETURN") {
			t.Errorf("km=%d: batch nodes must stay RETURN-free, got: %q", km, got)
		}
	}
}

// -----------------------------------------------------------------------------
// Composite-key coverage — pins that the key-loop correctly interleaves
// separators across two or more key fields in both the param form ($key_<name>)
// and the batch form (row.key_<name>). Single-key variants are implicitly
// covered by the smoke/golden tests above; this is the N≥2 case.
// -----------------------------------------------------------------------------

func TestBuildNodeMergeQuery_CompositeKeys(t *testing.T) {
	t.Parallel()
	got := BuildNodeMergeQuery("X", []string{"a", "b", "c"}, MutableKeys)
	want := "MERGE (n:X {a: $key_a, b: $key_b, c: $key_c})\nSET n += $props"
	if got != want {
		t.Errorf("composite keys mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestBuildBatchNodeMergeQuery_CompositeKeys(t *testing.T) {
	t.Parallel()
	got := BuildBatchNodeMergeQuery("X", []string{"a", "b", "c"}, MutableKeys)
	want := "UNWIND $rows AS row\n" +
		"MERGE (n:X {a: row.key_a, b: row.key_b, c: row.key_c})\n" +
		"SET n += row.props"
	if got != want {
		t.Errorf("composite keys mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestBuildRelationshipMergeQuery_CompositeKeys(t *testing.T) {
	t.Parallel()
	got := BuildRelationshipMergeQuery(
		"Src", []string{"a", "b"},
		"REL",
		"Tgt", []string{"x", "y"},
		false,
	)
	want := "MATCH (from:Src {a: $from_key_a, b: $from_key_b})\n" +
		"MATCH (to:Tgt {x: $to_key_x, y: $to_key_y})\n" +
		"MERGE (from)-[r:REL]->(to)\n" +
		"RETURN count(*) AS matched_rows"
	if got != want {
		t.Errorf("composite keys mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestBuildBatchRelationshipMergeQuery_CompositeKeys(t *testing.T) {
	t.Parallel()
	got := BuildBatchRelationshipMergeQuery(
		"Src", []string{"a", "b"},
		"REL",
		"Tgt", []string{"x", "y"},
		true,
	)
	want := "UNWIND $rows AS row\n" +
		"MATCH (from:Src {a: row.from_a, b: row.from_b})\n" +
		"MATCH (to:Tgt {x: row.to_x, y: row.to_y})\n" +
		"MERGE (from)-[r:REL]->(to)\n" +
		"SET r += row.rel_props\n" +
		"RETURN count(*) AS matched_rows"
	if got != want {
		t.Errorf("composite keys mismatch\n got: %q\nwant: %q", got, want)
	}
}
