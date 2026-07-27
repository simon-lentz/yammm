package instance

import (
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// Internal tests for the deferred-symbolization capture path. They live in
// package instance because one of the two symbolization sites is not reachable
// through the public API — see TestEdgeStateFor_ShapeMismatch_NamesFirstUse.

// pcTestSchema is the in-package fixture for these tests. The external suite's
// personSchema is not visible from package instance, and these tests need
// unexported access.
func pcTestSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
		WithName("pctest").
		AddType("Address").
		AsPart().
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		WithRelation("knows", schema.LocalTypeRef("Person", location.Span{}), false, true).
		WithComposition("addresses", schema.LocalTypeRef("Address", location.Span{}), false, true).
		Done().
		Build()
	if result.HasErrors() {
		t.Fatalf("pcTestSchema: %s", result)
	}
	return s
}

func TestSymbolizePC_ZeroIsEmpty(t *testing.T) {
	t.Parallel()
	if got := symbolizePC(0); got != "" {
		t.Errorf("symbolizePC(0) = %q, want \"\" — a failed capture must render no locator at all", got)
	}
}

// TestSymbolizePC_MatchesEagerResolution is the correctness proof for deferring
// symbolization: a PC resolved later must name exactly the line an immediate
// resolution would have named. Both are taken from the SAME call site, so any
// disagreement is the deferral's fault.
//
// It also guards the pc-1 adjustment. runtime.Callers records return addresses;
// runtime.CallersFrames maps one back to the calling instruction and
// runtime.FuncForPC does not, so a switch to the latter would show up here as an
// off-by-one rather than passing silently.
func TestSymbolizePC_MatchesEagerResolution(t *testing.T) {
	t.Parallel()
	var pcs [1]uintptr
	if runtime.Callers(1, pcs[:]) == 0 { // 1 = this frame, so File:Line is the line above
		t.Fatal("runtime.Callers returned no frames")
	}

	frame, _ := runtime.CallersFrames(pcs[:1]).Next()
	eager := frame.File + ":" + strconv.Itoa(frame.Line)

	if got := symbolizePC(pcs[0]); got != eager {
		t.Errorf("deferred symbolization = %q, eager = %q", got, eager)
	}
	if got := symbolizePC(pcs[0]); !strings.Contains(got, "schema_builder_pc_test.go:") {
		t.Errorf("symbolizePC = %q, want a locator in this file", got)
	}
}

// TestCapturePC_SucceedsFromABuilderMethod pins that capturePC's frame skip is
// satisfiable at its real call depth: a non-zero PC that resolves to this file.
// The public-API equivalents (TestSchemaBuilder_CallerLocator_SameFile /
// _CrossFile in schema_builder_test.go) pin the exact line and the cross-file
// attribution; this one guards the primitive itself.
func TestCapturePC_SucceedsFromABuilderMethod(t *testing.T) {
	t.Parallel()
	s := pcTestSchema(t)
	b, err := BuilderFor(s, "Person")
	if err != nil {
		t.Fatalf("BuilderFor: %v", err)
	}

	b.Property("nope", 1)
	if len(b.errors) != 1 {
		t.Fatalf("recorded %d errors, want 1", len(b.errors))
	}
	if b.errors[0].callerPC == 0 {
		t.Fatal("capturePC returned the zero PC from a real builder call")
	}
	if got := symbolizePC(b.errors[0].callerPC); !strings.Contains(got, "schema_builder_pc_test.go:") {
		t.Errorf("locator = %q, want a line in this file", got)
	}
}

// TestEdgeStateFor_ShapeMismatch_NamesFirstUse covers the ONE symbolization site
// that is not [buildError.Error] — edgeStateFor formats the first use's locator
// into a detail string when a later call pins a different shape.
//
// It is exercised directly because the branch is UNREACHABLE through the public
// API: addEdge rejects EdgeTo on a relation that declares edge properties and
// EdgeToWith on one that does not, and HasProperties is fixed per relation, so
// only one shape can ever reach edgeStateFor for a given relation.
// TestSchemaBuilder_Cardinality_OneMixingEdgeShapes records that observation
// from the outside. The branch is kept as defence for a future third
// edge-producing entry point; without this test the deferral inside it would
// ship unverified.
func TestEdgeStateFor_ShapeMismatch_NamesFirstUse(t *testing.T) {
	t.Parallel()
	s := pcTestSchema(t)
	b, err := BuilderFor(s, "Person")
	if err != nil {
		t.Fatalf("BuilderFor: %v", err)
	}

	rel, ok := b.resolveRelation("knows")
	if !ok {
		t.Fatal("fixture must declare a 'knows' relation")
	}

	// Captured with runtime.Callers directly, not capturePC: capturePC's skip of
	// 3 is calibrated for user → builder method → capturePC, so calling it from a
	// test resolves to testing.go rather than here. edgeStateFor takes whatever
	// PC it is handed, so a PC pointing at this file is what makes the locator
	// assertion below mean something.
	var pcs [1]uintptr
	if runtime.Callers(1, pcs[:]) == 0 {
		t.Fatal("runtime.Callers returned no frames")
	}
	firstPC := pcs[0]

	if b.edgeStateFor(rel, shapeTo, firstPC) == nil {
		t.Fatal("first call must pin the shape and return state")
	}
	if b.edgeStateFor(rel, shapeToWith, firstPC) != nil {
		t.Error("mismatched shape must return nil")
	}

	if len(b.errors) != 1 {
		t.Fatalf("recorded %d errors, want 1", len(b.errors))
	}
	detail := b.errors[0].detail
	if !strings.Contains(detail, "cannot mix EdgeTo and EdgeToWith") {
		t.Errorf("detail = %q, want the mix message", detail)
	}
	if want := symbolizePC(firstPC); !strings.Contains(detail, want) {
		t.Errorf("detail = %q, want it to name the first use at %q — the locator must "+
			"survive being stored as a PC and symbolized here", detail, want)
	}
	if !strings.Contains(detail, "schema_builder_pc_test.go:") {
		t.Errorf("detail = %q, want a locator in this file", detail)
	}
}

// TestComposed_ChildLocators_AreParallelToChildren pins that callerPCs stays
// index-aligned with children, so a failing child's error names the Composed
// call that added THAT child.
//
// The correspondence had no coverage before: reversing the index survives every
// other test in the package, because the existing composition tests add a single
// child and any index resolves to the same PC. Two children added from two
// distinct source lines is the smallest shape that can tell them apart.
func TestComposed_ChildLocators_AreParallelToChildren(t *testing.T) {
	t.Parallel()
	s := pcTestSchema(t)
	parent, err := BuilderFor(s, "Person")
	if err != nil {
		t.Fatalf("BuilderFor: %v", err)
	}
	good, err := BuilderFor(s, "Address")
	if err != nil {
		t.Fatalf("BuilderFor(Address): %v", err)
	}
	bad, err := BuilderFor(s, "Address")
	if err != nil {
		t.Fatalf("BuilderFor(Address): %v", err)
	}
	good.Property("id", "a1")
	bad.Property("id", "a2").Property("nope", "x") // fails at the child's Build

	parent.Property("id", "p1").Property("name", "Alice")

	// Each Composed call is preceded by its own runtime.Caller(0), so the
	// expected line is always "the line after this one" — no offset arithmetic
	// that a later edit to the test body could silently invalidate.
	_, _, goodMark, ok := runtime.Caller(0)
	parent.Composed("addresses", good)
	goodLine := goodMark + 1

	_, _, badMark, ok2 := runtime.Caller(0)
	parent.Composed("addresses", bad)
	badLine := badMark + 1

	if !ok || !ok2 {
		t.Fatal("runtime.Caller failed")
	}
	if _, err = parent.Build(); err == nil {
		t.Fatal("expected the failing child to surface")
	}
	msg := err.Error()

	want := "schema_builder_pc_test.go:" + strconv.Itoa(badLine)
	if !strings.Contains(msg, want) {
		t.Errorf("error = %q,\nwant the locator of the Composed call that added the FAILING child (%s)", msg, want)
	}
	notWant := "schema_builder_pc_test.go:" + strconv.Itoa(goodLine)
	if strings.Contains(msg, notWant) {
		t.Errorf("error = %q,\nnames the Composed call for the SUCCEEDING child (%s); the "+
			"callerPCs slice is no longer index-aligned with children", msg, notWant)
	}
}

// TestSchemaBuilder_SuccessPath_IsAllocationFree is the ratchet for this change.
// The whole point of storing a PC is that a successful builder call allocates
// nothing for a locator it will never render; re-introducing eager symbolization
// would restore three allocations per call and pass every other test in the
// package.
func TestSchemaBuilder_SuccessPath_IsAllocationFree(t *testing.T) {
	s := pcTestSchema(t)
	b, err := BuilderFor(s, "Person")
	if err != nil {
		t.Fatalf("BuilderFor: %v", err)
	}
	b.Property("name", "warm") // warm the property map so growth is not measured

	if got := testing.AllocsPerRun(200, func() {
		b.Property("name", "Alice")
	}); got != 0 {
		t.Errorf("a successful Property call allocated %v times per run, want 0", got)
	}
}

// TestBuildError_Error_OmitsLocatorForZeroPC pins the degradation contract: a
// capture that failed renders the message with no locator and no stray prefix,
// rather than a bogus one.
func TestBuildError_Error_OmitsLocatorForZeroPC(t *testing.T) {
	t.Parallel()
	e := &buildError{kind: kindUnknownProperty, typ: "Person", target: "nope"}
	const want = `Person: unknown property "nope"`
	if got := e.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if strings.HasPrefix(e.Error(), ":") {
		t.Error("a zero PC must not leave a bare colon prefix")
	}
}
