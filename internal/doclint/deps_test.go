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
	checked, _ := doclint.AssertDependencyLines(r, depFixture)
	return r, checked
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

// A row naming an import the directory does not have is the shape the gate
// exists to refuse.
func TestAssertDependencyLines_ReportsANameTheDirectoryDoesNotImport(t *testing.T) {
	t.Parallel()
	r, _ := runDepGate(t)
	if !r.reports("names other, which that directory does not import") {
		t.Errorf("the extra name was not reported: %v", r.msgs)
	}
}

func TestAssertDependencyLines_ReportsAnImportTheLineDoesNotName(t *testing.T) {
	t.Parallel()
	r, _ := runDepGate(t)
	if !r.reports("imports other, which its dependency row does not name") {
		t.Errorf("the unnamed import was not reported: %v", r.msgs)
	}
}

// A directory whose only Go file is doc.go documents a family's edges. Its rows
// name OTHER directories and are read against those directories' imports — the
// skip that exempted the whole file is deleted, because a row's subject decides
// what it is compared against and the doc's own import set never did.
func TestAssertDependencyLines_ReadsAFamilyTableRowByRow(t *testing.T) {
	t.Parallel()
	r, _ := runDepGate(t)
	if !r.reports("the extra row names nothing-of-the-sort") {
		t.Errorf("a family table's false row was not read: %v", r.msgs)
	}
	for _, m := range r.msgs {
		if strings.Contains(m, "familyonly") && strings.Contains(m, "the correct row names leaf") {
			t.Errorf("a family table's TRUE row was reported: %s", m)
		}
	}
}

// A heading the gate cannot read is an error: it is prose where the module
// states a machine-checked claim everywhere else.
func TestAssertDependencyLines_ReportsAHeadingWithNoRow(t *testing.T) {
	t.Parallel()
	r, _ := runDepGate(t)
	if !r.reports("a # Dependencies heading with no") {
		t.Errorf("a heading carrying no readable row was not reported: %v", r.msgs)
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

// A package with no dependency block asserts nothing and must not be counted,
// or the driver's every-heading-was-read check would measure the walk rather
// than the claims. leaf carries no doc.go, so it states nothing at all.
func TestAssertDependencyLines_CountsOnlyDocsThatCarryARow(t *testing.T) {
	t.Parallel()
	r, checked := runDepGate(t)
	if r.reports("/leaf/") {
		t.Errorf("a package with no dependency block was reported: %v", r.msgs)
	}
	// correct, extra, absent, wrapped, prose, plus the family table's rows and
	// the arrowless heading's zero — the family rows are why this is not five.
	if checked < 5 {
		t.Errorf("checked %d rows, want at least the five self-rows", checked)
	}
}

func TestAssertDependencyLines_MissingRootIsReported(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	if checked, _ := doclint.AssertDependencyLines(r, "testdata/does-not-exist"); checked != 0 {
		t.Errorf("checked %d lines under a root that does not exist", checked)
	}
	if len(r.msgs) == 0 {
		t.Error("a missing root was not reported")
	}
}
