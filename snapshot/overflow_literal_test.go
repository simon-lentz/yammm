package snapshot_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// TestUnmarshal_DuplicatesHoldingNumbersPastEveryFloatLoad holds the reader to
// the order it sorts duplicates by: a number literal no finite float64 holds
// stays the literal it is written as, and two duplicates holding such
// literals, or one and a float, still order, so the document loads and writes
// back byte for byte.
func TestUnmarshal_DuplicatesHoldingNumbersPastEveryFloatLoad(t *testing.T) {
	t.Parallel()
	s, err := schema.LoadString(t.Context(), `schema "ov"

type T {
	id String primary
	x Float
}
`, "ov.yammm")
	if err != nil {
		t.Fatalf("schema: %s", err)
	}
	doc := readerDoc(t, s,
		map[string]any{"$type": "T", "id": "a", "x": 1.0},
		map[string]any{"$type": "T", "id": "a", "x": 7.0},
		map[string]any{"$type": "T", "id": "a", "x": 8.0},
		map[string]any{"$type": "T", "id": "a", "x": 9.0},
	)
	doc = editOnce(t, doc, `"x":7.0`, `"x":1e400`)
	doc = editOnce(t, doc, `"x":8.0`, `"x":-2e400`)
	data := rehashDocument(t, []byte(doc))

	snap, err := snapshot.Unmarshal(context.Background(), data, s)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	out, err := snapshot.Marshal(context.Background(), snap)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	again, err := snapshot.Unmarshal(context.Background(), out, s)
	if err != nil {
		t.Fatalf("Unmarshal of the written document: %v", err)
	}
	back, err := snapshot.Marshal(context.Background(), again)
	if err != nil {
		t.Fatalf("Marshal of the reloaded snapshot: %v", err)
	}
	if !bytes.Equal(out, back) {
		t.Errorf("the document does not write back byte for byte:\n%s\n%s", out, back)
	}
}
