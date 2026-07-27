package main

import (
	"bytes"
	"strings"
	"testing"

	adaptern4j "github.com/simon-lentz/yammm/adapter/neo4j"
)

// A remote object that parsed a name but no type means the introspection
// projection did not deliver its type column. The parsers deliberately tolerate
// that — they accept records from any source, including a caller projecting
// fewer columns — so the diff command is what has to notice, because it is the
// only place that knows the full projection was requested.
//
// The cost of missing it is silence, not an error: an object with no type is
// undeclarable, so it drops out of the comparison and the remaining objects
// still print a confident plan. These tests pin the detection, not the message.

func TestUntypedRemoteObjects_CountsOnlyMissingTypes(t *testing.T) {
	t.Parallel()

	constraints := []adaptern4j.RemoteConstraint{
		{Name: "ok", Type: "UNIQUENESS"},
		{Name: "missing"},                         // the column did not arrive
		{Name: "also-missing", Type: ""},          // explicit, same thing
		{Name: "unrecognised", Type: "SOMETHING"}, // a value change, NOT this failure
	}

	got := untypedRemoteObjects(len(constraints), func(i int) string { return constraints[i].Type })
	if got != 2 {
		t.Errorf("untypedRemoteObjects = %d; want 2 — only an ABSENT type counts, an unrecognised one is a different failure", got)
	}
}

func TestUntypedRemoteObjects_HealthyRecordsCountZero(t *testing.T) {
	t.Parallel()

	// Every type string a current server reports for an index, including the
	// kinds this schema can never declare. None is a projection failure.
	indexes := []adaptern4j.RemoteIndex{
		{Name: "a", Type: "RANGE"},
		{Name: "b", Type: "VECTOR"},
		{Name: "c", Type: "TEXT"},
		{Name: "d", Type: "POINT"},
		{Name: "e", Type: "FULLTEXT"},
		{Name: "f", Type: "LOOKUP"},
	}

	if got := untypedRemoteObjects(len(indexes), func(i int) string { return indexes[i].Type }); got != 0 {
		t.Errorf("untypedRemoteObjects = %d; want 0 — an undeclarable kind is not an unreadable projection", got)
	}
}

// An empty result set is the normal state of a database yammm has not
// provisioned yet, and must not read as a broken projection.
func TestUntypedRemoteObjects_EmptyResultIsNotAFailure(t *testing.T) {
	t.Parallel()

	if got := untypedRemoteObjects(0, func(int) string { return "" }); got != 0 {
		t.Errorf("untypedRemoteObjects on an empty set = %d; want 0", got)
	}
}

// The message has to name the query, because the fix is to look at what that
// projection asks for against the server actually running.
func TestReportUnreadableProjection_NamesTheQueryAndTheCost(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	reportUnreadableProjection(&buf, 2, 5, "constraint", "SHOW CONSTRAINTS")

	for _, want := range []string{"2 of 5", "constraint", "SHOW CONSTRAINTS", "type", "unclassifiable"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("stderr does not mention %q:\n%s", want, buf.String())
		}
	}
}
