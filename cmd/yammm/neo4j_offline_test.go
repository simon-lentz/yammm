package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/neo4j"
)

// recordedCall is one query a command issued.
type recordedCall struct {
	database string
	query    string
}

// queryReply is one recorded server answer.
type queryReply struct {
	records []map[string]any
	err     error
}

// queryLog answers each call from a queue of recorded replies and remembers
// what was asked. It is the whole test double the neo4j commands need: past the
// connection every step is pure, so recorded records drive the parsers and the
// diff exactly as a server would.
type queryLog struct {
	replies []queryReply
	calls   []recordedCall
}

func (q *queryLog) run(_ context.Context, database, query string) ([]map[string]any, error) {
	q.calls = append(q.calls, recordedCall{database: database, query: query})
	if len(q.calls) > len(q.replies) {
		return nil, fmt.Errorf("no recorded reply for query %d: %s", len(q.calls), query)
	}
	reply := q.replies[len(q.calls)-1]
	return reply.records, reply.err
}

// recordedConstraints is a [neo4j.ShowConstraints] reading of a database
// written under the default mapping: a primary key, a required property, a scalar type
// constraint, and a relationship-scoped constraint.
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

// TestFetchRemoteState_AnIndexReadFailureIsRuntime: the plan's constraint half
// needs the index rows, since both kinds share one namespace, so an index read
// that fails or returns records the parser refuses fails the command and names
// its step.
func TestFetchRemoteState_AnIndexReadFailureIsRuntime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		index queryReply
		step  string
		cause string
	}{
		{"the index query fails", queryReply{err: errors.New("unsupported in this edition")}, "fetch indexes", "unsupported in this edition"},
		{"an index record has no name", queryReply{records: []map[string]any{{"type": "RANGE"}}}, "parse indexes", "record 0: missing index name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			log := &queryLog{replies: []queryReply{{records: recordedConstraints()}, tt.index}}
			_, err := fetchRemoteState(t.Context(), log.run, "neo4j")
			if code := cli.ExitForError(err); code != cli.ExitRuntime {
				t.Errorf("exit code = %d, want %d: %v", code, cli.ExitRuntime, err)
			}
			if err == nil || !strings.Contains(err.Error(), tt.step) || !strings.Contains(err.Error(), tt.cause) {
				t.Errorf("error %v does not name the step %q and the cause %q", err, tt.step, tt.cause)
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
			_, err := fetchRemoteState(t.Context(), log.run, "neo4j")
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
// the failing cases are read against.
func TestFetchRemoteState_ReadsBothHalvesWhenTheServerAnswers(t *testing.T) {
	t.Parallel()

	indexes := []map[string]any{
		{
			"name": "book_catalog__Book_title_idx", "type": "RANGE", "entityType": "NODE",
			"labelsOrTypes": []any{"book_catalog__Book"}, "properties": []any{"title"}, "state": "ONLINE",
		},
	}
	log := &queryLog{replies: []queryReply{{records: recordedConstraints()}, {records: indexes}}}

	state, err := fetchRemoteState(t.Context(), log.run, "analytics")
	if err != nil {
		t.Fatalf("fetchRemoteState: %v", err)
	}
	for i, call := range log.calls {
		if call.database != "analytics" {
			t.Errorf("query %d ran against database %q, want the one asked for, %q", i, call.database, "analytics")
		}
	}
	if len(state.constraints) != len(recordedConstraints()) || len(state.indexes) != 1 {
		t.Errorf("read %d constraints and %d indexes, want %d and 1",
			len(state.constraints), len(state.indexes), len(recordedConstraints()))
	}
	if log.calls[0].query != neo4j.ShowConstraints {
		t.Errorf("first query = %q, want the constraints projection", log.calls[0].query)
	}
	if log.calls[1].query != neo4j.ShowIndexes {
		t.Errorf("second query = %q, want the indexes projection", log.calls[1].query)
	}
}

// A constraint read that fails, or returns records the parser refuses, is a
// runtime failure naming its step: the diff cannot run without the constraints.
func TestFetchRemoteState_ConstraintFailuresAreRuntime(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		reply queryReply
		step  string
	}{
		"the query fails":      {queryReply{err: errors.New("server gone")}, "fetch constraints"},
		"a record has no name": {queryReply{records: []map[string]any{{"type": "UNIQUENESS"}}}, "parse constraints"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			log := &queryLog{replies: []queryReply{c.reply}}
			_, err := fetchRemoteState(t.Context(), log.run, "neo4j")
			if code := cli.ExitForError(err); code != cli.ExitRuntime {
				t.Errorf("exit code = %d, want %d: %v", code, cli.ExitRuntime, err)
			}
			if err == nil || !strings.Contains(err.Error(), c.step) {
				t.Errorf("error %v does not name the step %q", err, c.step)
			}
			if len(log.calls) != 1 {
				t.Errorf("%d queries ran, want 1: nothing past the failed constraint read", len(log.calls))
			}
		})
	}
}
