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
		`{"yammm_snapshot":{"version":%d,"schema_name":"test","schema_source":"test://test.yammm","schema_hash":%q,"schema_hash_algorithm":1,"integrity_hash":"","features":%s},"types":["Person"],"instances":{"Person":[{"key":["p1"],"properties":{"id":"p1","name":"Alice"},"provenance":null}]},"diagnostics":{"duplicates":[],"unresolved":[]}}`,
		version, schema.StructuralHash(s), featuresJSON)
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
// writes back the version it read. UpdateMetadata reuses the body verbatim, so
// stamping the writer's current version onto a body of an older format
// produces a document that lies about its own shape.
func TestUpdateMetadata_PreservesInputVersion(t *testing.T) {
	ctx := context.Background()
	s := testSchema(t)

	const readableOlderVersion = 2
	data := headerDoc(s, readableOlderVersion, `[]`)

	out, res := snapshot.UpdateMetadata(ctx, data, map[string]string{"phase": "link"})
	if res.HasErrors() {
		t.Fatalf("UpdateMetadata on a readable document: %v", res)
	}
	if got := headerVersion(t, out); got != readableOlderVersion {
		t.Errorf("output version = %d, want %d — the fast path must not relabel a body it did not migrate", got, readableOlderVersion)
	}
}

// TestUpdateMetadata_RefusesUnsupportedVersion pins that the fast path honours
// the version rejection decodeHeader collects. decodeHeader returns a nil error
// for an out-of-range version and reports it on the collector, so a caller that
// tests only the error accepts a document no read path would.
func TestUpdateMetadata_RefusesUnsupportedVersion(t *testing.T) {
	ctx := context.Background()
	s := testSchema(t)
	data := headerDoc(s, 99, `[]`)

	_, res := snapshot.UpdateMetadata(ctx, data, map[string]string{"phase": "link"})
	if !res.HasErrors() {
		t.Fatal("UpdateMetadata accepted an out-of-range version")
	}
	if !hasCode(res, diag.E_SNAPSHOT_UNSUPPORTED_VERSION) {
		t.Errorf("want %s, got %v", diag.E_SNAPSHOT_UNSUPPORTED_VERSION, res)
	}
}

// TestUpdateMetadata_RefusesNullFeatures pins the second shape the collector
// check newly refuses. Marshal always emits a non-nil array, so a null features
// field is a document no read path accepts.
func TestUpdateMetadata_RefusesNullFeatures(t *testing.T) {
	ctx := context.Background()
	s := testSchema(t)
	data := headerDoc(s, 2, `null`)

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
	data := headerDoc(s, 2, `["nonexistent_feature"]`)

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
func TestUpdateMetadata_HashAlgorithmWarningStillPasses(t *testing.T) {
	ctx := context.Background()
	s := testSchema(t)
	data := fmt.Appendf(nil,
		`{"yammm_snapshot":{"version":2,"schema_name":"test","schema_source":"test://test.yammm","schema_hash":%q,"schema_hash_algorithm":99,"integrity_hash":"","features":[]},"types":["Person"],"instances":{"Person":[{"key":["p1"],"properties":{"id":"p1","name":"Alice"},"provenance":null}]},"diagnostics":{"duplicates":[],"unresolved":[]}}`,
		schema.StructuralHash(s))

	if _, res := snapshot.UpdateMetadata(ctx, data, map[string]string{"phase": "link"}); res.HasErrors() {
		t.Errorf("an unrecognized hash algorithm is a Warning and must not refuse the fast path: %v", res)
	}
}
