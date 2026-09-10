package main

import (
	"maps"
	"strings"
	"testing"

	adaptern4j "github.com/simon-lentz/yammm/adapter/neo4j"
)

// labelPrefix is the prefix the prefixed-graph readings below were written with.
const labelPrefix = "app_"

// prefixLabels returns recs with labelPrefix prepended to every node label,
// the reading of a database written with that label prefix.
func prefixLabels(recs []map[string]any) []map[string]any {
	out := make([]map[string]any, len(recs))
	for i, r := range recs {
		c := maps.Clone(r)
		if c["entityType"] != "RELATIONSHIP" {
			for _, k := range []string{"labelsOrTypes", "srcLabels", "tgtLabels"} {
				if ls, ok := c[k].([]any); ok {
					n := make([]any, len(ls))
					for j, l := range ls {
						n[j] = labelPrefix + l.(string)
					}
					c[k] = n
				}
			}
		}
		out[i] = c
	}
	return out
}

// scaffold drives introspectSchema over recorded records and returns the DSL
// it emits.
func scaffold(t *testing.T, cons, rels []map[string]any, filter string, opts ...adaptern4j.Option) string {
	t.Helper()
	log := &queryLog{replies: []queryReply{{records: cons}, {records: rels}}}
	dsl, err := introspectSchema(t.Context(), log.run, "neo4j", filter, opts...)
	if err != nil {
		t.Fatalf("introspectSchema: %v", err)
	}
	return dsl
}

// scaffoldOutcome reports the first of wants absent from dsl.
func scaffoldOutcome(dsl string, wants ...string) string {
	for _, want := range wants {
		if !strings.Contains(dsl, want) {
			return "scaffold lacks " + want + ":\n" + dsl
		}
	}
	return ""
}

// TestIntrospectSchema_ScaffoldsAPrefixedGraph asserts the artefact a
// prefixed graph yields, not the query the command issues: the types, their
// keys and their relationships, under the schema the operator asked for.
func TestIntrospectSchema_ScaffoldsAPrefixedGraph(t *testing.T) {
	t.Parallel()

	t.Run("with the schema filter", func(t *testing.T) {
		t.Parallel()
		dsl := scaffold(t, prefixLabels(recordedConstraints()), prefixLabels(recordedRelationships()),
			"book_catalog", adaptern4j.WithLabelPrefix(labelPrefix))
		checkRepairState(t, "introspect: prefixed graph with the schema filter",
			scaffoldOutcome(dsl, `schema "book_catalog"`, "type Book {", "type Publisher {", "isbn String primary", "--> PUBLISHED_BY Publisher"))
	})

	t.Run("without a filter, the schema is named without its prefix", func(t *testing.T) {
		t.Parallel()
		dsl := scaffold(t, prefixLabels(recordedConstraints()), prefixLabels(recordedRelationships()),
			"", adaptern4j.WithLabelPrefix(labelPrefix))
		checkRepairState(t, "introspect: prefixed graph without a filter",
			scaffoldOutcome(dsl, `schema "book_catalog"`, "type Book {"))
	})
}

// TestIntrospectSchema_FilterIsComparedAsTheLabelWritesIt: the relationship
// scan sanitizes the filter and the constraint reading must compare the same
// spelling, or a schema name the label sanitizes matches nothing.
func TestIntrospectSchema_FilterIsComparedAsTheLabelWritesIt(t *testing.T) {
	t.Parallel()
	dsl := scaffold(t, recordedConstraints(), recordedRelationships(), "book-catalog")
	checkRepairState(t, "introspect: a filter the label sanitizes", scaffoldOutcome(dsl, "type Book {"))
}

// TestIntrospectSchema_AFilterMatchingNothingSaysSo: constraints were read and
// none carried a label this configuration writes, so the scaffold says so
// instead of standing as an empty database's.
func TestIntrospectSchema_AFilterMatchingNothingSaysSo(t *testing.T) {
	t.Parallel()
	dsl := scaffold(t, recordedConstraints(), recordedRelationships(), "book_catalog", adaptern4j.WithLabelPrefix(labelPrefix))
	checkRepairState(t, "introspect: a filter matching nothing", scaffoldOutcome(dsl, "TODO: no constraint"))
}
