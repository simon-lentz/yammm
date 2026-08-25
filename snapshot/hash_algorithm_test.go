package snapshot_test

import (
	"bytes"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/snapshot"
)

// TestLoad_RefusesAnOldHashAlgorithm pins the v0.15.0 severity split: a
// body-reading surface refuses a document whose schema identity it cannot
// check, and a header-only read stays classifiable for dispatch.
func TestLoad_RefusesAnOldHashAlgorithm(t *testing.T) {
	t.Parallel()
	s, data := attWireFixture(t)

	old := bytes.Replace(data,
		[]byte(`"schema_hash_algorithm":2`), []byte(`"schema_hash_algorithm":1`), 1)
	if bytes.Equal(old, data) {
		t.Fatal("fixture does not carry algorithm 2")
	}

	if _, res := snapshot.Load(t.Context(), old, s, snapshot.WithSkipIntegrityCheck()); !res.HasErrors() || !res.HasCode(diag.E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM) {
		t.Fatalf("Load accepted an algorithm-1 document: %v", res)
	}
	if res := snapshot.Verify(t.Context(), old, s, snapshot.WithSkipIntegrityCheck()); !res.HasErrors() || !res.HasCode(diag.E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM) {
		t.Fatalf("Verify accepted an algorithm-1 document: %v", res)
	}

	header, hres := snapshot.HeaderOnly(t.Context(), old)
	if hres.HasErrors() {
		t.Fatalf("a header-only read must stay classifiable: %v", hres)
	}
	if header.SchemaHashMatches(s) {
		t.Fatal("a header under an old algorithm must not match any schema")
	}
}
