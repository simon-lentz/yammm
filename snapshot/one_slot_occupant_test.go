package snapshot_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// oneSlotReaderSchema declares a (one) ADDRESS composition beside a (_:many)
// LINES, so the reader's cardinality guard has both the shape it refuses and
// the shape it must leave alone.
func oneSlotReaderSchema(t *testing.T) schema.Schema {
	t.Helper()
	const src = `schema "one_slot_reader"

type Order {
	id String primary
	*-> ADDRESS (one) Address
	*-> LINES (_:many) Line
}

part type Address {
	street String primary
}

part type Line {
	sku String primary
}
`
	s, err := schema.LoadString(t.Context(), src, "one_slot_reader.yammm")
	if err != nil {
		t.Fatalf("load: %s", err)
	}
	return s
}

// oneSlotDocument writes a document whose (one) ADDRESS slot holds a
// sole occupant, and returns it beside a copy carrying a second one.
//
// The second occupant is spliced into the composed array rather than written:
// every writer refuses the shape, and a schema whose cardinality differs is
// refused at the schema-hash gate before the structural walk runs, so a
// foreign document is the only way to present it to the reader. The splice is
// byte-level because the reader requires yammm_snapshot to be the object's
// first key, which a JSON round trip through a Go map does not preserve.
func oneSlotDocument(t *testing.T, s schema.Schema) (sole, twoOccupants []byte) {
	t.Helper()
	order := instancetest.Valid(t, s, "Order", map[string]any{
		"id":      "o1",
		"address": []any{map[string]any{"street": "first"}},
	})

	g := graph.New(s)
	if err := g.Add(t.Context(), order); err != nil {
		t.Fatalf("add: %s", err)
	}
	data, err := snapshot.Marshal(t.Context(), g.Snapshot())
	if err != nil {
		t.Fatalf("marshal: %s", err)
	}

	occupant, ok := soleArrayElement(data, `"ADDRESS":[`)
	if !ok {
		t.Fatal("the composed ADDRESS array does not hold exactly one occupant")
	}
	second := strings.NewReplacer(`"first"`, `"second"`).Replace(occupant)
	at := bytes.Index(data, []byte(occupant)) + len(occupant)
	spliced := append(append(append([]byte{}, data[:at]...), ","+second...), data[at:]...)
	return data, rehashDocument(t, spliced)
}

// soleArrayElement returns the single JSON object of the array opening at
// marker, matched by brace depth so a nested array or object does not end it.
// It reports false when the array holds anything but one object.
func soleArrayElement(data []byte, marker string) (string, bool) {
	at := bytes.Index(data, []byte(marker))
	if at < 0 {
		return "", false
	}
	body := data[at+len(marker):]
	depth := 0
	for i, b := range body {
		switch b {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				// The element ends here; the array must end immediately after.
				return string(body[:i+1]), i+1 < len(body) && body[i+1] == ']'
			}
			if depth < 0 {
				return "", false
			}
		}
	}
	return "", false
}

// TestLoad_RefusesASecondOccupantInAOneSlot pins the reader's half of the
// (one) composed key's premise. The composed key segment carries no
// discriminating element for a (one) hop because such a slot holds exactly one
// child, and nothing on the load path enforced that: a document carrying two
// occupants loaded clean and the adapter then minted one byte-identical
// _composed_key for both. Removing the guard turns this red.
func TestLoad_RefusesASecondOccupantInAOneSlot(t *testing.T) {
	t.Parallel()
	s := oneSlotReaderSchema(t)
	sole, twoOccupants := oneSlotDocument(t, s)

	// The control runs first: a sole occupant must still load, or the guard
	// would pass by refusing everything.
	if _, err := snapshot.Unmarshal(t.Context(), sole, s); err != nil {
		t.Fatalf("a sole occupant of a (one) composition was refused: %s", err)
	}

	_, res := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
		return snapshot.Unmarshal(t.Context(), twoOccupants, s, snapshot.WithWarnings(warn))
	})
	if diagtest.Err(res) == nil {
		t.Fatal("two occupants of a (one) composition loaded clean")
	}
	if !hasCode(res, diag.E_DUPLICATE_COMPOSED_PK) {
		t.Errorf("want E_DUPLICATE_COMPOSED_PK; got %s", res)
	}
	// Verify reads the same walk, so it must agree.
	if !hasCode(diagtest.CaptureErr(t, func(warn func(diag.Issue)) error {
		return snapshot.Verify(t.Context(), twoOccupants, s, snapshot.WithWarnings(warn))
	}), diag.E_DUPLICATE_COMPOSED_PK) {
		t.Error("Verify did not refuse the shape Load refuses")
	}
}
