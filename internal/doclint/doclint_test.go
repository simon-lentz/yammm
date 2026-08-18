package doclint_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/doclint"
)

// recorder is a [doclint.TB] that captures failures instead of reporting them,
// so the gate's failure path can be exercised. A gate is only worth having if
// it fails when it should, and every rule below is checked in both directions.
type recorder struct{ msgs []string }

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

func (r *recorder) reports(substr string) bool {
	return slices.ContainsFunc(r.msgs, func(m string) bool {
		return strings.Contains(m, substr)
	})
}

// fixture is a module of its own so the gate runs its real entry point, go.mod
// read included, rather than a path that only tests exercise.
const fixture = "testdata/fixture"

func runGate(t *testing.T) (*recorder, int) {
	t.Helper()
	r := &recorder{}
	return r, doclint.AssertNoDanglingLinks(r, fixture)
}

func TestAssertNoDanglingLinks_CleanPackageIsSilent(t *testing.T) {
	t.Parallel()
	r, checked := runGate(t)
	for _, m := range r.msgs {
		if strings.Contains(m, "/clean/") {
			t.Errorf("clean fixture reported: %s", m)
		}
	}
	if checked == 0 {
		t.Error("the gate resolved no links, so it asserts nothing")
	}
}

// One case per shape a link can take, so no single resolution path can regress
// unnoticed.
func TestAssertNoDanglingLinks_ReportsEveryDanglingShape(t *testing.T) {
	t.Parallel()
	r, _ := runGate(t)
	for _, want := range []string{
		"[Removed]",            // package-level name
		"[Gadget.Vanished]",    // member of a declared type
		"[TestGadget_Deleted]", // regression anchor whose test is gone
		"clean.Absent",         // qualified at an in-module package
	} {
		if !r.reports(want) {
			t.Errorf("no report naming %s; got %v", want, r.msgs)
		}
	}
}

// The directory, not the package, is the resolution unit: the anchor lives in
// package clean's doc and the test it names lives in package clean_test.
func TestAssertNoDanglingLinks_ResolvesAcrossTestFiles(t *testing.T) {
	t.Parallel()
	r, _ := runGate(t)
	if r.reports("TestWidget_Builds") {
		t.Error("an anchor naming a live test in an external test package was reported")
	}
}

// godoc renders a bracketed lowercase name as literal text rather than
// linkifying it, so it is out of this gate's scope by construction.
func TestAssertNoDanglingLinks_IgnoresLowercaseNames(t *testing.T) {
	t.Parallel()
	r, _ := runGate(t)
	if r.reports("[helper]") {
		t.Error("a bracketed lowercase name was treated as a link")
	}
}

// The file's own import wins over a same-named in-module package. Without
// import-first, json.Marshal would be looked for in the fixture json package,
// which does not have it, and reported.
func TestAssertNoDanglingLinks_ResolvesPackageNamesAgainstImportsFirst(t *testing.T) {
	t.Parallel()
	r, _ := runGate(t)
	if r.reports("json.Marshal") {
		t.Error("json.Marshal resolved against the fixture package instead of the file's encoding/json import")
	}
}

// With no import to go on, a unique in-module package of that name is the
// fallback. Without it the text would not become a link at all, and the missing
// symbol would go unreported.
func TestAssertNoDanglingLinks_FallsBackToAUniqueInModulePackageName(t *testing.T) {
	t.Parallel()
	r, _ := runGate(t)
	if !r.reports("json.Absent") {
		t.Errorf("the unique-in-module-name fallback did not resolve json; got %v", r.msgs)
	}
}

// A whole-package link carries no symbol, so finding the package is the whole
// of resolving it.
func TestAssertNoDanglingLinks_AcceptsWholePackageLinks(t *testing.T) {
	t.Parallel()
	r, _ := runGate(t)
	if r.reports("[doclintfixture/rotten]") {
		t.Error("a link naming a package that exists was reported")
	}
}

func TestAssertNoDanglingLinks_MissingRootIsReported(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	if checked := doclint.AssertNoDanglingLinks(r, "testdata/does-not-exist"); checked != 0 {
		t.Errorf("checked = %d over a missing root; want 0", checked)
	}
	if len(r.msgs) == 0 {
		t.Error("a missing root passed silently")
	}
}
