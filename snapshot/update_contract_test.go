package snapshot_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// headerDoc builds a minimal well-formed document whose header carries the
// given version and features literal. The body is the shape Marshal produces,
// so UpdateMetadata's body-offset resolution succeeds and the test isolates
// the header validation it is written for.
func headerDoc(s *schema.Schema, version int, featuresJSON string) []byte {
	return fmt.Appendf(nil,
		`{"yammm_snapshot":{"version":%d,"schema_name":"test","schema_source":"test://test.yammm","schema_hash":%q,"schema_hash_algorithm":%d,"integrity_hash":"","features":%s},"types":[{"schema_path":"test://test.yammm","name":"Person"}],"instances":[{"type":0,"items":[{"key":["p1"],"properties":{"id":"p1","name":"Alice"},"provenance":null}]}],"diagnostics":{"duplicates":[],"unresolved":[]}}`,
		version, schema.StructuralHash(s), schema.StructuralHashVersion, featuresJSON)
}

// headerVersion reads the version field back out of a document.
func headerVersion(t *testing.T, data []byte) int {
	t.Helper()
	var doc struct {
		Header struct {
			Version int `json:"version"`
		} `json:"yammm_snapshot"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal output document: %v", err)
	}
	return doc.Header.Version
}

// TestUpdateMetadata_PreservesInputVersion pins that the metadata fast path
// writes the version it read through its parameterized header builder, so a
// future version bump cannot silently relabel a body written under the
// older format.
func TestUpdateMetadata_PreservesInputVersion(t *testing.T) {
	ctx := context.Background()
	s := testSchema(t)

	data := headerDoc(s, 3, `[]`)

	out, res := snapshot.UpdateMetadata(ctx, data, map[string]string{"phase": "link"})
	if res.HasErrors() {
		t.Fatalf("UpdateMetadata on a readable document: %v", res)
	}
	if got := headerVersion(t, out); got != 3 {
		t.Errorf("output version = %d, want 3 — the fast path must write the version it read", got)
	}
}

// TestUpdateMetadata_RefusesUnsupportedVersion pins that the fast path honours
// the version rejection decodeHeader collects, on both sides of the accept
// range: v2 documents are no longer readable, and decodeHeader returns a nil
// error for an out-of-range version, so a caller that tests only the error
// accepts a document no read path would.
func TestUpdateMetadata_RefusesUnsupportedVersion(t *testing.T) {
	ctx := context.Background()
	s := testSchema(t)

	for _, version := range []int{2, 99} {
		data := headerDoc(s, version, `[]`)
		_, res := snapshot.UpdateMetadata(ctx, data, map[string]string{"phase": "link"})
		if !res.HasErrors() {
			t.Fatalf("UpdateMetadata accepted version %d", version)
		}
		if !hasCode(res, diag.E_SNAPSHOT_UNSUPPORTED_VERSION) {
			t.Errorf("version %d: want %s, got %v", version, diag.E_SNAPSHOT_UNSUPPORTED_VERSION, res)
		}
	}
}

// TestUpdateMetadata_RefusesNullFeatures pins the second shape the collector
// check newly refuses. Marshal always emits a non-nil array, so a null features
// field is a document no read path accepts.
func TestUpdateMetadata_RefusesNullFeatures(t *testing.T) {
	ctx := context.Background()
	s := testSchema(t)
	data := headerDoc(s, 3, `null`)

	_, res := snapshot.UpdateMetadata(ctx, data, map[string]string{"phase": "link"})
	if !res.HasErrors() {
		t.Fatal("UpdateMetadata accepted a null features field")
	}
	if !hasCode(res, diag.E_SNAPSHOT_MALFORMED) {
		t.Errorf("want %s, got %v", diag.E_SNAPSHOT_MALFORMED, res)
	}
}

// TestUpdateMetadata_RefusesUnrecognizedFeature pins the third shape the
// collector check newly refuses.
func TestUpdateMetadata_RefusesUnrecognizedFeature(t *testing.T) {
	ctx := context.Background()
	s := testSchema(t)
	data := headerDoc(s, 3, `["nonexistent_feature"]`)

	_, res := snapshot.UpdateMetadata(ctx, data, map[string]string{"phase": "link"})
	if !res.HasErrors() {
		t.Fatal("UpdateMetadata accepted an unrecognized feature")
	}
	if !hasCode(res, diag.E_SNAPSHOT_UNSUPPORTED_FEATURE) {
		t.Errorf("want %s, got %v", diag.E_SNAPSHOT_UNSUPPORTED_FEATURE, res)
	}
}

// TestUpdateMetadata_HashAlgorithmWarningStillPasses guards the boundary of the
// collector check: the hash-algorithm mismatch is a Warning and must keep
// passing, or the check would refuse documents every read path accepts.
func TestUpdateMetadata_RefusesUnknownHashAlgorithm(t *testing.T) {
	// Inverted at v0.15.0: UpdateMetadata re-signs a body it does not read,
	// so a document whose schema identity cannot be checked is refused
	// rather than relabelled. Header-only reads stay non-fatal instead.
	ctx := context.Background()
	s := testSchema(t)
	data := fmt.Appendf(nil,
		`{"yammm_snapshot":{"version":3,"schema_name":"test","schema_source":"test://test.yammm","schema_hash":%q,"schema_hash_algorithm":99,"integrity_hash":"","features":[]},"types":[{"schema_path":"test://test.yammm","name":"Person"}],"instances":[{"type":0,"items":[{"key":["p1"],"properties":{"id":"p1","name":"Alice"},"provenance":null}]}],"diagnostics":{"duplicates":[],"unresolved":[]}}`,
		schema.StructuralHash(s))

	out, res := snapshot.UpdateMetadata(ctx, data, map[string]string{"phase": "link"})
	if !res.HasCode(diag.E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM) || !res.HasErrors() {
		t.Errorf("an unrecognized hash algorithm must refuse the relabel with an Error: %v", res)
	}
	if out != nil {
		t.Error("a refused relabel returned output bytes")
	}

	header, hres := snapshot.HeaderOnly(ctx, data)
	if hres.HasErrors() {
		t.Errorf("a header-only read must stay classifiable: %v", hres)
	}
	if header == nil || header.SchemaHashMatches(s) {
		t.Error("a header under an unknown algorithm must not match any schema")
	}
}
