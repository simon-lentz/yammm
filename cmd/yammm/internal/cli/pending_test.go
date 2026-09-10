package cli

import "testing"

// pendingRepairs names each instrument case that fails on the current tree
// and the defect it observes. A listed case must keep failing, so a repair
// turns it red until its entry is deleted; an unlisted failure is a regression.
var pendingRepairs = map[string]string{
	"StagedFiles: a FIFO receives the bytes and stays a FIFO":   "a target no rename can replace is staged",
	"StagedFiles: a basename near the name limit is written":    "the staging name adds to the basename and exceeds the name limit",
	"StagedFiles: a dangling symlink is written through":        "a target no rename can replace is staged",
	"StagedFiles: a looping symlink is refused":                 "a looping link is replaced by a regular file",
	"StagedFiles: a read-only file is refused and kept":         "a read-only file is overwritten",
	"StagedFiles: a symlink survives and its target is written": "the link is replaced by a regular file and its target is left unchanged",
	"StagedFiles: an error names the operator's path":           "the error names the staging file",
	"WriteFile: a FIFO receives the bytes and stays a FIFO":     "the FIFO is replaced by a regular file and its reader receives nothing",
	"WriteFile: a basename near the name limit is written":      "the staging name adds to the basename and exceeds the name limit",
	"WriteFile: a dangling symlink is written through":          "the link is replaced by a regular file and the file it names is never created",
	"WriteFile: a looping symlink is refused":                   "a looping link is replaced by a regular file",
	"WriteFile: a symlink survives and its target is written":   "the link is replaced by a regular file and its target is left unchanged",
	"WriteFile: an error names the operator's path":             "the error names the staging file",
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
