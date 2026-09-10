package main

import "testing"

// pendingRepairs names each instrument case that fails on the current tree
// and the defect it observes. A listed case must keep failing, so a repair
// turns it red until its entry is deleted; an unlisted failure is a regression.
var pendingRepairs = map[string]string{
	"snapshot info: a directory entry carries the diagnostic wire":                     "the entry carries a three-field issue projection under issues, with no truncation state",
	"snapshot info: a warned directory row names its warning":                          "the row is marked warn and names no code",
	"snapshot info: absent metadata renders as an object, --format json":               "a nil map marshals as null",
	"snapshot info: absent metadata renders as an object, --header-only --format json": "a nil map marshals as null",
	"snapshot info: header-only text reports the file size":                            "the text renderer reads the library struct, which carries no file size",
}

// checkRepairState reports a case against pendingRepairs. failure is empty
// when the case passed.
func checkRepairState(t *testing.T, key, failure string) {
	t.Helper()
	defect, pinned := pendingRepairs[key]
	switch {
	case pinned && failure == "":
		t.Errorf("%s: the pinned defect (%s) no longer reproduces; delete its pendingRepairs entry", key, defect)
	case pinned:
		t.Logf("%s: pinned defect reproduces (%s): %s", key, defect, failure)
	case failure != "":
		t.Error(failure)
	}
}
