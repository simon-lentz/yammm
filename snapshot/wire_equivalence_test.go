package snapshot_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/snapshot"
)

// The wire contract claims UpdateMetadata(x, newMeta) == Marshal(Load(x)) for
// documents the current Marshal produced. It holds when x carries no created_at
// and newMeta is empty, and the two conditions are not alike: created_at is
// preserved from the document, while metadata comes from the call and replaces
// whatever the document held. This pins the equality where it holds and each
// divergence separately, because the two named wire-format tests pin key order
// and body shape and reach none of them.
func TestWireContract_UpdateMetadataMatchesReMarshal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{"name": "Alice"}))

	t.Run("holds with neither created_at nor metadata", func(t *testing.T) {
		t.Parallel()
		data, result := snapshot.Marshal(ctx, snap)
		if err := result.Err(); err != nil {
			t.Fatalf("marshal: %v", err)
		}

		updated, result := snapshot.UpdateMetadata(ctx, data, nil)
		if err := result.Err(); err != nil {
			t.Fatalf("UpdateMetadata: %v", err)
		}

		loaded, result := snapshot.Load(ctx, data, s)
		if err := result.Err(); err != nil {
			t.Fatalf("load: %v", err)
		}
		remarshalled, result := snapshot.Marshal(ctx, loaded)
		if err := result.Err(); err != nil {
			t.Fatalf("re-marshal: %v", err)
		}

		if !bytes.Equal(updated, remarshalled) {
			t.Errorf("UpdateMetadata and Marshal(Load(x)) differ:\n update: %s\n re-marshal: %s",
				updated, remarshalled)
		}
	})

	t.Run("diverges once the document carries created_at", func(t *testing.T) {
		t.Parallel()
		stamped, result := snapshot.Marshal(ctx, snap,
			snapshot.WithCreatedAt(time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)))
		if err := result.Err(); err != nil {
			t.Fatalf("marshal: %v", err)
		}

		updated, result := snapshot.UpdateMetadata(ctx, stamped, nil)
		if err := result.Err(); err != nil {
			t.Fatalf("UpdateMetadata: %v", err)
		}

		loaded, result := snapshot.Load(ctx, stamped, s)
		if err := result.Err(); err != nil {
			t.Fatalf("load: %v", err)
		}
		remarshalled, result := snapshot.Marshal(ctx, loaded)
		if err := result.Err(); err != nil {
			t.Fatalf("re-marshal: %v", err)
		}

		if bytes.Equal(updated, remarshalled) {
			t.Error("the two agreed on a document carrying created_at — the " +
				"contract's stated condition is stricter than it needs to be")
		}
		if !bytes.Contains(updated, []byte("created_at")) {
			t.Error("UpdateMetadata dropped created_at; it preserves it from the input header")
		}
		if bytes.Contains(remarshalled, []byte("created_at")) {
			t.Error("Marshal reproduced created_at without WithCreatedAt")
		}
	})

	// The document's own metadata is not the condition: UpdateMetadata replaces
	// it with newMeta either way.
	t.Run("holds on a document carrying metadata when newMeta is empty", func(t *testing.T) {
		t.Parallel()
		withMeta, result := snapshot.Marshal(ctx, snap,
			snapshot.WithMetadata(map[string]string{"pipeline": "done"}))
		if err := result.Err(); err != nil {
			t.Fatalf("marshal: %v", err)
		}

		updated, result := snapshot.UpdateMetadata(ctx, withMeta, nil)
		if err := result.Err(); err != nil {
			t.Fatalf("UpdateMetadata: %v", err)
		}
		if bytes.Contains(updated, []byte("metadata")) {
			t.Error("UpdateMetadata kept the input's metadata; it replaces the map wholesale")
		}

		loaded, result := snapshot.Load(ctx, withMeta, s)
		if err := result.Err(); err != nil {
			t.Fatalf("load: %v", err)
		}
		remarshalled, result := snapshot.Marshal(ctx, loaded)
		if err := result.Err(); err != nil {
			t.Fatalf("re-marshal: %v", err)
		}

		if !bytes.Equal(updated, remarshalled) {
			t.Errorf("the two differed on a document carrying metadata, where the "+
				"call passed none:\n update: %s\n re-marshal: %s", updated, remarshalled)
		}
	})

	// The call is the condition: a document carrying no metadata still diverges
	// once newMeta is non-empty.
	t.Run("diverges once newMeta is non-empty", func(t *testing.T) {
		t.Parallel()
		data, result := snapshot.Marshal(ctx, snap)
		if err := result.Err(); err != nil {
			t.Fatalf("marshal: %v", err)
		}

		updated, result := snapshot.UpdateMetadata(ctx, data, map[string]string{"pipeline": "done"})
		if err := result.Err(); err != nil {
			t.Fatalf("UpdateMetadata: %v", err)
		}
		if !bytes.Contains(updated, []byte("metadata")) {
			t.Error("UpdateMetadata did not write newMeta")
		}

		loaded, result := snapshot.Load(ctx, data, s)
		if err := result.Err(); err != nil {
			t.Fatalf("load: %v", err)
		}
		remarshalled, result := snapshot.Marshal(ctx, loaded)
		if err := result.Err(); err != nil {
			t.Fatalf("re-marshal: %v", err)
		}

		if bytes.Equal(updated, remarshalled) {
			t.Error("the two agreed although the call wrote metadata a re-marshal cannot reproduce")
		}
	})
}
