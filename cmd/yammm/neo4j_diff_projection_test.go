package main

import (
	"bytes"
	"strings"
	"testing"

	adaptern4j "github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
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
// projection asks for against the server actually running. The exit code has to
// be a runtime failure, because the comparison did not run.
func TestUnreadableProjection_NamesTheQueryAndTheCost(t *testing.T) {
	t.Parallel()

	err := unreadableProjection(2, 5, "constraint", "SHOW CONSTRAINTS")

	if got := cli.ExitForError(err); got != cli.ExitRuntime {
		t.Errorf("exit code = %d; want %d — a comparison that never ran must not report success", got, cli.ExitRuntime)
	}
	for _, want := range []string{"2 of 5", "constraint", "SHOW CONSTRAINTS", "type", "unclassifiable"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message does not mention %q:\n%s", want, err.Error())
		}
	}
}

// Each line of the message is reported on its own, so the explanation is not
// indented under a prefix that belongs to the first line.
func TestUnreadableProjection_ReportsEachLineSeparately(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cli.ReportError(&buf, unreadableProjection(2, 5, "index", "SHOW INDEXES"))

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d line(s), want 2:\n%s", len(lines), buf.String())
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "error: ") {
			t.Errorf("line does not carry the error prefix: %q", line)
		}
	}
}
