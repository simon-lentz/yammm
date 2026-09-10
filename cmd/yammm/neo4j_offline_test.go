package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"testing"

	adaptern4j "github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/internal/yammmtest"
)

// recordedCall is one query a command issued.
type recordedCall struct {
	database string
	query    string
	params   map[string]any
}

// queryReply is one recorded server answer.
type queryReply struct {
	records []map[string]any
	err     error
}

// queryLog answers each call from a queue of recorded replies and remembers
// what was asked. It is the whole test double the neo4j commands need: past the
// connection every step is pure, so recorded records drive the parsers, the
// inference and the diff exactly as a server would.
type queryLog struct {
	replies []queryReply
	calls   []recordedCall
}

func (q *queryLog) run(_ context.Context, database, query string, params map[string]any) ([]map[string]any, error) {
	q.calls = append(q.calls, recordedCall{database: database, query: query, params: params})
	if len(q.calls) > len(q.replies) {
		return nil, fmt.Errorf("no recorded reply for query %d: %s", len(q.calls), query)
	}
	reply := q.replies[len(q.calls)-1]
	return reply.records, reply.err
}

// recordedConstraints is a SHOW CONSTRAINTS YIELD * reading of a database
// written by this adapter: a primary key, a required property, a scalar type
// constraint, and a relationship-scoped constraint the inference must skip.
func recordedConstraints() []map[string]any {
	return []map[string]any{
		{
			"name":            "book_catalog__Publisher_publisher_id_unique",
			"type":            "UNIQUENESS",
			"entityType":      "NODE",
			"labelsOrTypes":   []any{"book_catalog__Publisher"},
			"properties":      []any{"publisher_id"},
			"createStatement": "CREATE CONSTRAINT book_catalog__Publisher_publisher_id_unique IF NOT EXISTS FOR (n:book_catalog__Publisher) REQUIRE n.publisher_id IS UNIQUE",
		},
		{
			"name":          "book_catalog__Book_isbn_unique",
			"type":          "UNIQUENESS",
			"entityType":    "NODE",
			"labelsOrTypes": []any{"book_catalog__Book"},
			"properties":    []any{"isbn"},
		},
		{
			"name":          "book_catalog__Book_title_exists",
			"type":          "NODE_PROPERTY_EXISTENCE",
			"entityType":    "NODE",
			"labelsOrTypes": []any{"book_catalog__Book"},
			"properties":    []any{"title"},
		},
		{
			"name":          "book_catalog__Book_page_count_type",
			"type":          "NODE_PROPERTY_TYPE",
			"entityType":    "NODE",
			"labelsOrTypes": []any{"book_catalog__Book"},
			"properties":    []any{"page_count"},
			"propertyType":  "INTEGER",
		},
		{
			"name":          "book_catalog__WRITTEN_BY_since_exists",
			"type":          "RELATIONSHIP_PROPERTY_EXISTENCE",
			"entityType":    "RELATIONSHIP",
			"labelsOrTypes": []any{"WRITTEN_BY"},
			"properties":    []any{"since"},
		},
	}
}

// recordedRelationships is the relationship-signature reading of the same
// database.
func recordedRelationships() []map[string]any {
	return []map[string]any{
		{
			"relType":   "WRITTEN_BY",
			"srcLabels": []any{"book_catalog__Book"},
			"tgtLabels": []any{"book_catalog__Author"},
		},
		{
			"relType":   "PUBLISHED_BY",
			"srcLabels": []any{"book_catalog__Book"},
			"tgtLabels": []any{"book_catalog__Publisher"},
		},
	}
}

// TestIntrospectSchema_Golden is the offline harness A-430 names: recorded
// records in, the emitted scaffold out. Nothing past the command's --uri guard
// was reachable by any test before it, which is why emptying both inputs to
// InferSchema left the suite green.
func TestIntrospectSchema_Golden(t *testing.T) {
	t.Parallel()

	log := &queryLog{replies: []queryReply{
		{records: recordedConstraints()},
		{records: recordedRelationships()},
	}}

	dsl, err := introspectSchema(t.Context(), log.run, "neo4j", "book_catalog")
	if err != nil {
		t.Fatalf("introspectSchema: %v", err)
	}
	yammmtest.Golden(t, "neo4j_introspect_scaffold", []byte(dsl))
}

// TestIntrospectSchema_IssuesTheQueriesItDocuments pins what reaches the
// server: the constraints projection, then the relationship scan carrying the
// label prefix the adapter composes from the filter.
func TestIntrospectSchema_IssuesTheQueriesItDocuments(t *testing.T) {
	t.Parallel()

	log := &queryLog{replies: []queryReply{
		{records: recordedConstraints()},
		{records: recordedRelationships()},
	}}
	if _, err := introspectSchema(t.Context(), log.run, "books", "book_catalog"); err != nil {
		t.Fatalf("introspectSchema: %v", err)
	}

	if len(log.calls) != 2 {
		t.Fatalf("issued %d queries, want 2: %+v", len(log.calls), log.calls)
	}
	if log.calls[0].query != adaptern4j.IntrospectConstraintsQuery() {
		t.Errorf("first query = %q, want the constraints projection", log.calls[0].query)
	}
	wantQuery, wantParams := adaptern4j.New().IntrospectRelationshipsQueryFor("book_catalog")
	if log.calls[1].query != wantQuery {
		t.Errorf("second query = %q, want %q", log.calls[1].query, wantQuery)
	}
	yammmtest.Diff(t, wantParams, log.calls[1].params)
	for i, call := range log.calls {
		if call.database != "books" {
			t.Errorf("query %d ran against %q, want the --database value", i, call.database)
		}
	}
}

// TestIntrospectSchema_FailuresAreRuntime pins every failure the seam can
// carry. Each is an I/O or protocol failure rather than a usage error, and the
// exit code follows from that.
func TestIntrospectSchema_FailuresAreRuntime(t *testing.T) {
	t.Parallel()

	boom := errors.New("connection reset")
	unnamed := []map[string]any{{"type": "UNIQUENESS"}}

	tests := []struct {
		name    string
		replies []queryReply
		want    string
	}{
		{"the constraints query fails", []queryReply{{err: boom}}, "fetch constraints"},
		{"a constraint record has no name", []queryReply{{records: unnamed}}, "parse constraints"},
		{
			"the relationship query fails",
			[]queryReply{{records: recordedConstraints()}, {err: boom}},
			"fetch relationships",
		},
		{
			"a relationship record has no type",
			[]queryReply{{records: recordedConstraints()}, {records: []map[string]any{{"srcLabels": []any{"A"}}}}},
			"parse relationships",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			log := &queryLog{replies: tt.replies}
			_, err := introspectSchema(t.Context(), log.run, "neo4j", "")
			if err == nil {
				t.Fatal("no error")
			}
			if code := cli.ExitForError(err); code != cli.ExitRuntime {
				t.Errorf("exit code = %d, want %d", code, cli.ExitRuntime)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not name the phase %q", err, tt.want)
			}
		})
	}
}

// TestFetchRemoteState_DegradesOnAnUnreadableIndexRead: a server that cannot
// report indexes must leave the constraint reading intact, say the index half
// did not happen, and tell the operator so under a code a machine consumer can
// match.
func TestFetchRemoteState_DegradesOnAnUnreadableIndexRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		index queryReply
		cause string
	}{
		{"the index query fails", queryReply{err: errors.New("unsupported in this edition")}, "unsupported in this edition"},
		{"an index record has no name", queryReply{records: []map[string]any{{"type": "RANGE"}}}, "parse indexes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			log := &queryLog{replies: []queryReply{{records: recordedConstraints()}, tt.index}}

			var out bytes.Buffer
			sink := cli.NewDiagnosticSink(&out, cli.FormatText, true, false)
			state, err := fetchRemoteState(t.Context(), log.run, "neo4j", sink)
			if err != nil {
				t.Fatalf("a degraded index read must not fail the diff: %v", err)
			}
			if !state.indexFailed {
				t.Error("the index read failed and the state does not say so")
			}
			if len(state.constraints) != len(recordedConstraints()) {
				t.Errorf("kept %d constraints, want %d — the constraint diff must survive",
					len(state.constraints), len(recordedConstraints()))
			}
			if !sink.Result().HasCode(adaptern4j.W_NEO4J_INDEXES_UNREADABLE) {
				t.Errorf("a degraded read must report %s; got: %s",
					adaptern4j.W_NEO4J_INDEXES_UNREADABLE, sink.Result())
			}
			if sink.Result().HasErrors() {
				t.Error("a degraded read is a warning, not a failure")
			}
			// The cause is the whole diagnostic value: an operator acts on
			// "unsupported in this edition" and cannot act on "could not read".
			if !strings.Contains(sink.Result().String(), tt.cause) {
				t.Errorf("the warning does not name the cause %q; got: %s", tt.cause, sink.Result())
			}
		})
	}
}

// TestFetchRemoteState_RefusesAnUnreadableProjection pins the other outcome: a
// record that parsed but carries no type means the projection went stale, and
// no comparison is reported rather than one built from a partial reading.
func TestFetchRemoteState_RefusesAnUnreadableProjection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		replies []queryReply
		want    string
	}{
		{
			"a constraint with no type",
			[]queryReply{{records: []map[string]any{{"name": "c1", "labelsOrTypes": []any{"A"}}}}},
			"SHOW CONSTRAINTS",
		},
		{
			"an index with no type",
			[]queryReply{
				{records: recordedConstraints()},
				{records: []map[string]any{{"name": "i1", "labelsOrTypes": []any{"A"}}}},
			},
			"SHOW INDEXES",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			log := &queryLog{replies: tt.replies}
			sink := cli.NewDiagnosticSink(&bytes.Buffer{}, cli.FormatText, true, false)
			_, err := fetchRemoteState(t.Context(), log.run, "neo4j", sink)
			if err == nil {
				t.Fatal("an unreadable type column must not be reported as a clean comparison")
			}
			if code := cli.ExitForError(err); code != cli.ExitRuntime {
				t.Errorf("exit code = %d, want %d", code, cli.ExitRuntime)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not name the projection %q", err, tt.want)
			}
		})
	}
}

// TestFetchRemoteState_ReadsBothHalvesWhenTheServerAnswers is the clean control
// the degraded cases are read against.
func TestFetchRemoteState_ReadsBothHalvesWhenTheServerAnswers(t *testing.T) {
	t.Parallel()

	indexes := []map[string]any{
		{
			"name": "book_catalog__Book_title_idx", "type": "RANGE", "entityType": "NODE",
			"labelsOrTypes": []any{"book_catalog__Book"}, "properties": []any{"title"}, "state": "ONLINE",
		},
	}
	log := &queryLog{replies: []queryReply{{records: recordedConstraints()}, {records: indexes}}}

	var out bytes.Buffer
	sink := cli.NewDiagnosticSink(&out, cli.FormatText, true, false)
	state, err := fetchRemoteState(t.Context(), log.run, "neo4j", sink)
	if err != nil {
		t.Fatalf("fetchRemoteState: %v", err)
	}
	if state.indexFailed {
		t.Error("the index read succeeded and the state says it failed")
	}
	if len(state.constraints) != len(recordedConstraints()) || len(state.indexes) != 1 {
		t.Errorf("read %d constraints and %d indexes, want %d and 1",
			len(state.constraints), len(state.indexes), len(recordedConstraints()))
	}
	if sink.Result().Len() != 0 {
		t.Errorf("a clean read reported %s", sink.Result())
	}
	if log.calls[1].query != adaptern4j.IntrospectIndexesQuery() {
		t.Errorf("second query = %q, want the indexes projection", log.calls[1].query)
	}
}

// TestIntrospectSchema_CarriesTheLabelOptionsIntoTheScaffold pins that the
// command's label flags reach the adapter at both reads: the relationship scan
// filters on the prefix the adapter composes, and the constraint reading strips
// that prefix and splits at that separator, so a graph written with them
// scaffolds its types and its relationships.
func TestIntrospectSchema_CarriesTheLabelOptionsIntoTheScaffold(t *testing.T) {
	t.Parallel()

	relabel := func(recs []map[string]any) []map[string]any {
		out := make([]map[string]any, len(recs))
		for i, r := range recs {
			c := maps.Clone(r)
			if c["entityType"] != "RELATIONSHIP" {
				for _, k := range []string{"labelsOrTypes", "srcLabels", "tgtLabels"} {
					if ls, ok := c[k].([]any); ok {
						n := make([]any, len(ls))
						for j, l := range ls {
							n[j] = "app_" + strings.Replace(l.(string), "__", "_x_", 1)
						}
						c[k] = n
					}
				}
			}
			out[i] = c
		}
		return out
	}
	log := &queryLog{replies: []queryReply{
		{records: relabel(recordedConstraints())},
		{records: relabel(recordedRelationships())},
	}}

	dsl, err := introspectSchema(
		t.Context(), log.run, "books", "book_catalog",
		adaptern4j.WithLabelPrefix("app_"),
		adaptern4j.WithLabelSeparator("_x_"),
	)
	if err != nil {
		t.Fatalf("introspectSchema: %v", err)
	}

	if len(log.calls) != 2 {
		t.Fatalf("issued %d queries, want 2", len(log.calls))
	}
	got, _ := log.calls[1].params["prefix"].(string)
	if want := "app_book_catalog_x_"; got != want {
		t.Errorf("relationship scan prefix = %q, want %q — the label flags did not reach the adapter", got, want)
	}
	for _, want := range []string{`schema "book_catalog"`, "type Book {", "type Publisher {", "--> PUBLISHED_BY Publisher"} {
		if !strings.Contains(dsl, want) {
			t.Errorf("the scaffold lacks %s:\n%s", want, dsl)
		}
	}
}

// TestIntrospectSchema_RefusesAnUnreadableProjection mirrors the guard
// `neo4j diff` applies to the identical SHOW CONSTRAINTS projection. Without
// it, a missing type column infers a scaffold with no primary keys at exit 0,
// which reads as a database that declares none.
func TestIntrospectSchema_RefusesAnUnreadableProjection(t *testing.T) {
	t.Parallel()

	log := &queryLog{replies: []queryReply{
		{records: []map[string]any{{"name": "c1", "labelsOrTypes": []any{"A"}}}},
	}}

	_, err := introspectSchema(t.Context(), log.run, "neo4j", "book_catalog")
	if err == nil {
		t.Fatal("an unreadable type column must not scaffold a schema at exit 0")
	}
	if code := cli.ExitForError(err); code != cli.ExitRuntime {
		t.Errorf("exit code = %d, want %d", code, cli.ExitRuntime)
	}
	if !strings.Contains(err.Error(), "SHOW CONSTRAINTS") {
		t.Errorf("error %q does not name the projection", err)
	}
}
