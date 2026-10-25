package snapshot_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/snapshot"
)

// The record index is the revalidator's alone, so a read without the option
// pays nothing for it. The fixture's p2 is its one root with such records.
func TestRead_OnlyARevalidatingReadFilesRecords(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	if n := snapshot.FiledRecords([]byte(doc), s); n != 0 {
		t.Errorf("a read without revalidation filed records under %d roots, want 0", n)
	}
	if n := snapshot.FiledRecords([]byte(doc), s, snapshot.WithRevalidation(diag.Warning)); n != 1 {
		t.Errorf("a revalidating read filed records under %d roots, want 1", n)
	}
}
