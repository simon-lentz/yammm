package snapshot_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/snapshot"
)

// TestUpdateMetadataOrReMarshal_FallbackKeepsCreatedAtBytes pins that the
// fallback carries a foreign header's created_at byte-for-byte, as the fast
// path does: fractional seconds and a non-UTC offset survive Load + Marshal.
func TestUpdateMetadataOrReMarshal_FallbackKeepsCreatedAtBytes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))
	stamped := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	data, res := snapshot.Marshal(ctx, snap, snapshot.WithCreatedAt(stamped))
	if res.HasErrors() {
		t.Fatalf("marshal: %v", res)
	}

	// A foreign writer's stamp: sub-second precision and an offset.
	const foreign = "2026-08-17T14:00:00.123456789+02:00"
	foreignDoc := bytes.Replace(data,
		[]byte(`"created_at":"2026-08-17T12:00:00Z"`),
		[]byte(`"created_at":"`+foreign+`"`), 1)
	if bytes.Equal(foreignDoc, data) {
		t.Fatal("fixture shape changed; created_at not found")
	}
	foreignDoc = rehashDocument(t, foreignDoc)

	// The same JSON-insignificant space the non-Marshal-shape test uses, so
	// the fast path refuses the document while every read path accepts it.
	spaced := bytes.Replace(foreignDoc, []byte(`},"types":`), []byte(`} ,"types":`), 1)
	if bytes.Equal(spaced, foreignDoc) {
		t.Fatal("fixture shape changed; header boundary not found")
	}
	spaced = rehashDocument(t, spaced)
	if _, fastRes := snapshot.UpdateMetadata(ctx, spaced, nil); !hasCode(fastRes, diag.E_UPDATE_METADATA_BODY_OFFSET) {
		t.Fatalf("the fast path accepted a non-Marshal shape: %v", fastRes)
	}

	out, outRes := snapshot.UpdateMetadataOrReMarshal(ctx, spaced, map[string]string{"stage": "fallback"}, s)
	if outRes.HasErrors() {
		t.Fatalf("the fallback failed: %v", outRes)
	}
	if !hasCode(outRes, diag.W_UPDATE_METADATA_FALLBACK) {
		t.Fatalf("no W_UPDATE_METADATA_FALLBACK warning: %v", outRes)
	}
	if hasCode(outRes, diag.W_SNAPSHOT_VALUE_DROPPED) {
		t.Errorf("the fallback reported dropping a created_at it could carry: %v", outRes)
	}
	if !bytes.Contains(out, []byte(`"created_at":"`+foreign+`"`)) {
		hdr, _ := snapshot.HeaderOnly(ctx, out)
		t.Errorf("created_at after the fallback = %q, want %q byte-for-byte", hdr.CreatedAt, foreign)
	}
}

// TestUpdateMetadataOrReMarshal_FallbackMarksACreatedAtItCannotCarry drives
// the one W_SNAPSHOT_VALUE_DROPPED site: the fallback carries a created_at only
// once it parses as RFC 3339, so it writes none for this one and says so.
func TestUpdateMetadataOrReMarshal_FallbackMarksACreatedAtItCannotCarry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))
	data, res := snapshot.Marshal(ctx, snap, snapshot.WithCreatedAt(time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)))
	if res.HasErrors() {
		t.Fatalf("marshal: %v", res)
	}
	doc := bytes.Replace(data, []byte(`"created_at":"2026-08-17T12:00:00Z"`), []byte(`"created_at":"yesterday"`), 1)
	doc = bytes.Replace(doc, []byte(`},"types":`), []byte(`} ,"types":`), 1)
	if bytes.Equal(doc, data) {
		t.Fatal("fixture shape changed")
	}
	doc = rehashDocument(t, doc)

	out, outRes := snapshot.UpdateMetadataOrReMarshal(ctx, doc, map[string]string{"stage": "fallback"}, s)
	if outRes.HasErrors() {
		t.Fatalf("the fallback failed: %v", outRes)
	}
	if !hasCode(outRes, diag.W_UPDATE_METADATA_FALLBACK) || !hasCode(outRes, diag.W_SNAPSHOT_VALUE_DROPPED) {
		t.Errorf("want the fallback and the dropped created_at marked: %v", outRes)
	}
	if bytes.Contains(out, []byte("yesterday")) {
		t.Errorf("the fallback carried a created_at Marshal cannot write: %s", out)
	}
}
