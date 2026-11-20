package snapshot_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/snapshot"
)

// Each function that takes a context panics on a nil one, naming itself.
// ScanDir panics when called, before its sequence runs.
func TestNilContext_EachPanicNamesTheCall(t *testing.T) {
	t.Parallel()
	s, doc := duplicateEdgeDoc(t)
	loaded, res := snapshot.Unmarshal(t.Context(), doc, s)
	if res.HasErrors() {
		t.Fatal(res)
	}
	var nilCtx context.Context
	dir := t.TempDir()
	for want, call := range map[string]func(){
		"snapshot.Unmarshal: nil context":                 func() { _, _ = snapshot.Unmarshal(nilCtx, doc, s) },
		"snapshot.Verify: nil context":                    func() { snapshot.Verify(nilCtx, doc, s) },
		"snapshot.Marshal: nil context":                   func() { _, _ = snapshot.Marshal(nilCtx, loaded) },
		"snapshot.UpdateMetadata: nil context":            func() { _, _ = snapshot.UpdateMetadata(nilCtx, doc, nil) },
		"snapshot.UpdateMetadataOrReMarshal: nil context": func() { _, _ = snapshot.UpdateMetadataOrReMarshal(nilCtx, doc, nil, s) },
		"snapshot.Stat: nil context":                      func() { _, _ = snapshot.Stat(nilCtx, doc) },
		"snapshot.ReadHeader: nil context":                func() { _, _ = snapshot.ReadHeader(nilCtx, bytes.NewReader(doc)) },
		"snapshot.ScanDir: nil context":                   func() { _ = snapshot.ScanDir(nilCtx, dir) },
	} {
		if got := yammmtest.AssertPanics(t, call); got != want {
			t.Errorf("panic = %v, want %q", got, want)
		}
	}
}
