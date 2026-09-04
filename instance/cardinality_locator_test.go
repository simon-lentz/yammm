package instance_test

import (
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/instance"
)

// A cardinality mismatch names the call site that caused it. Every other
// builder error carried the caller's file:line and this one did not, because a
// mismatch is detected in Build — after every call has returned — so there is
// no "current" call to attribute it to. Build's own godoc promises the locator
// unconditionally.
//
// The attributed call is the FIRST EXCESS one: the call that made a
// single-valued relation multi-valued is the one the caller has to delete.
//
// Mutation: clearing edgeState.excessCallerPC, or reverting the literal to omit
// callerPC, turns this red — symbolizePC returns "" for a zero PC and the
// message renders with no prefix.
func TestCardinalityLocator_NamesTheFirstExcessCall(t *testing.T) {
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	if err != nil {
		t.Fatalf("BuilderFor: %v", err)
	}

	b.Property("id", "p1").Property("name", "Alice")
	b.EdgeTo("WORKS_AT", "c1")

	_, _, captureLine, ok := runtime.Caller(0)
	mustCaller(t, ok)
	b.EdgeTo("WORKS_AT", "c2") // DO NOT MOVE: the offset below counts from captureLine.
	excessLine := captureLine + 2

	b.EdgeTo("WORKS_AT", "c3")

	_, err = b.Build()
	if err == nil {
		t.Fatal("three targets on a single-valued relation must fail")
	}
	if !strings.Contains(err.Error(), "cardinality mismatch") {
		t.Fatalf("wrong error: %v", err)
	}

	want := "schema_builder_test.go:"
	if strings.Contains(err.Error(), want) {
		t.Errorf("the locator names the wrong file: %v", err)
	}
	wantLine := "cardinality_locator_test.go:" + strconv.Itoa(excessLine)
	if !strings.Contains(err.Error(), wantLine) {
		t.Errorf("error = %v, want it to name the first excess call at %s", err, wantLine)
	}
	// Every excess call is an error at its own call: three targets are two
	// errors, so Build's count is true and names the second beside the first.
	if !strings.Contains(err.Error(), "this call adds target 2") || !strings.Contains(err.Error(), "and 1 more") {
		t.Errorf("error = %v, want the first excess call's own wording and a count of one more", err)
	}
}

// A single target on a single-valued relation still builds clean, so the
// locator field cannot be reported when there is nothing to report.
func TestCardinalityLocator_OneTargetStillBuilds(t *testing.T) {
	s := personSchema(t)
	b, err := instance.BuilderFor(s, "Person")
	if err != nil {
		t.Fatalf("BuilderFor: %v", err)
	}

	if _, err := b.Property("id", "p1").Property("name", "Alice").
		EdgeTo("WORKS_AT", "c1").Build(); err != nil {
		t.Errorf("a single target must build: %v", err)
	}
}

// mustCaller keeps the runtime.Caller check to a single line so the DO NOT MOVE
// offset above stays at +2, matching the convention the other locator tests use.
func mustCaller(t *testing.T, ok bool) {
	t.Helper()
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
}
