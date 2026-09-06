package doclint_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/doclint"
)

// depFixture is a module of its own so the gate runs its real entry point,
// go.mod read included, rather than a path that only tests exercise.
const depFixture = "testdata/depfixture"

func runDepGate(t *testing.T) (*recorder, int) {
	t.Helper()
	r := &recorder{}
	return r, doclint.AssertDependencyLines(r, depFixture)
}

func TestAssertDependencyLines_AgreeingLinesAreSilent(t *testing.T) {
	t.Parallel()
	r, checked := runDepGate(t)
	for _, quiet := range []string{"correct", "wrapped", "prose"} {
		if r.reports("/" + quiet + "/") {
			t.Errorf("%s reported: %v", quiet, r.msgs)
		}
	}
	if checked == 0 {
		t.Error("the gate checked no dependency line, so it asserts nothing")
	}
}

// A line naming a package the directory does not import is the shape that had
// been wrong at three consecutive trees.
func TestAssertDependencyLines_ReportsANameTheDirectoryDoesNotImport(t *testing.T) {
	t.Parallel()
	r, _ := runDepGate(t)
	if !r.reports("names other, which the directory does not import") {
		t.Errorf("the extra name was not reported: %v", r.msgs)
	}
}

func TestAssertDependencyLines_ReportsAnImportTheLineDoesNotName(t *testing.T) {
	t.Parallel()
	r, _ := runDepGate(t)
	if !r.reports("imports other, which the dependency line does not name") {
		t.Errorf("the unnamed import was not reported: %v", r.msgs)
	}
}

// A directory whose only Go file is doc.go documents a family's edges, so its
// lines name other directories and go/build reports it importing nothing.
func TestAssertDependencyLines_SkipsADirectoryHoldingOnlyDocGo(t *testing.T) {
	t.Parallel()
	r, _ := runDepGate(t)
	if r.reports("/familyonly/") {
		t.Errorf("the family listing was read as its own: %v", r.msgs)
	}
}

// A wrapped list is one list. Reading only its first line would report every
// continuation entry as an unnamed import.
func TestAssertDependencyLines_ReadsAWrappedListWhole(t *testing.T) {
	t.Parallel()
	r, _ := runDepGate(t)
	for _, m := range r.msgs {
		if strings.Contains(m, "/wrapped/") {
			t.Errorf("a continuation entry was not read: %s", m)
		}
	}
}

// The paragraph under a wrapped block is prose, not list entries.
func TestAssertDependencyLines_StopsAtTheEndOfTheBlock(t *testing.T) {
	t.Parallel()
	r, _ := runDepGate(t)
	for _, m := range r.msgs {
		if strings.Contains(m, "paragraph") || strings.Contains(m, "must") {
			t.Errorf("prose after the block was read as a list entry: %s", m)
		}
	}
}

// A package doc with no dependency block asserts nothing and must not be
// counted, or the floor in the module-wide driver would measure the walk
// rather than the claims.
func TestAssertDependencyLines_CountsOnlyDocsThatCarryALine(t *testing.T) {
	t.Parallel()
	r, checked := runDepGate(t)
	if r.reports("/noline/") {
		t.Errorf("a doc with no dependency block was reported: %v", r.msgs)
	}
	if want := 5; checked != want {
		t.Errorf("checked %d lines, want %d (correct, extra, absent, wrapped, prose)", checked, want)
	}
}

func TestAssertDependencyLines_MissingRootIsReported(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	if checked := doclint.AssertDependencyLines(r, "testdata/does-not-exist"); checked != 0 {
		t.Errorf("checked %d lines under a root that does not exist", checked)
	}
	if len(r.msgs) == 0 {
		t.Error("a missing root was not reported")
	}
}
