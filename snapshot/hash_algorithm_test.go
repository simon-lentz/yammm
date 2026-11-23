package snapshot_test

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// TestLoad_RefusesAnOldHashAlgorithm pins the v0.15.0 severity split: a
// body-reading surface refuses a document whose schema identity it cannot
// check, and a header-only read stays classifiable for dispatch. The old
// algorithm is the current one minus one, so the test follows every bump:
// a document written under the previous algorithm draws this signal — and NOT
// E_SNAPSHOT_INCOMPATIBLE_SCHEMA, because a version mismatch skips the
// hash comparison — which is what a consumer's cutover routes on.
func TestLoad_RefusesAnOldHashAlgorithm(t *testing.T) {
	t.Parallel()
	s, data := attWireFixture(t)

	current := fmt.Appendf(nil, `"schema_hash_algorithm":%d`, schema.StructuralHashVersion)
	previous := fmt.Appendf(nil, `"schema_hash_algorithm":%d`, schema.StructuralHashVersion-1)
	old := bytes.Replace(data, current, previous, 1)
	if bytes.Equal(old, data) {
		t.Fatalf("fixture does not carry algorithm %d", schema.StructuralHashVersion)
	}

	_, err := snapshot.Unmarshal(t.Context(), old, s, snapshot.WithIntegrityCheck(false))
	if err == nil || !errors.Is(err, diag.E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM) {
		t.Fatalf("Load accepted a previous-algorithm document: %v", err)
	}
	if errors.Is(err, diag.E_SNAPSHOT_INCOMPATIBLE_SCHEMA) {
		t.Fatal("a version mismatch must skip the hash comparison, not report it as incompatible")
	}
	if err := snapshot.Verify(t.Context(), old, s, snapshot.WithIntegrityCheck(false)); err == nil || !errors.Is(err, diag.E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM) {
		t.Fatalf("Verify accepted a previous-algorithm document: %v", err)
	}

	header, err := snapshot.ReadHeader(t.Context(), bytes.NewReader(old))
	if err != nil {
		t.Fatalf("a header-only read must stay classifiable: %v", err)
	}
	if got, want := header.SchemaHashAlgorithm, schema.StructuralHashVersion-1; got != want {
		t.Fatalf("a header-only read names algorithm %d, want the old %d", got, want)
	}
	if header.SchemaHashMatches(s) {
		t.Fatal("a header under an old algorithm must not match any schema")
	}
}
