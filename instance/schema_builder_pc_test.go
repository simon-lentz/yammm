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
