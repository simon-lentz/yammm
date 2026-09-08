package snapshot_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// respellAfter rewrites the first occurrence of from that follows marker, so
// one address in a document moves while every other rendering of the same
// value stays as written.
func respellAfter(t *testing.T, data []byte, marker, from, to string) []byte {
	t.Helper()
	at := bytes.Index(data, []byte(marker))
	if at < 0 {
		t.Fatalf("respellAfter: %q not in the document", marker)
	}
	rel := bytes.Index(data[at:], []byte(from))
	if rel < 0 {
		t.Fatalf("respellAfter: %q not found after %q", from, marker)
	}
	i := at + rel
	out := append([]byte{}, data[:i]...)
	out = append(out, to...)
	return append(out, data[i+len(from):]...)
}

// TestLoad_DuplicateRecordKeyAgreesAcrossSpellings pins that a duplicate record
// whose stated key and carried instance key spell one instant two ways is not
// malformed. Every other address in validateDiagnostics moved to the canonical
// form and this equality check stayed on the document's raw text, so a foreign
// writer that spelled the two differently was refused a document whose
// addresses agree. Comparing the raw spellings again turns this red.
func TestLoad_DuplicateRecordKeyAgreesAcrossSpellings(t *testing.T) {
	t.Parallel()
	s := residueRecordSchema(t)
	sensorType, _ := s.Type("Sensor")
	readingType, _ := s.Type("Reading")

	reading := func() *instance.ValidInstance {
		return instance.NewValidInstance("Reading", readingType.ID(),
			immutable.WrapKey([]any{residueCanonStamp}),
			immutable.WrapProperties(map[string]any{"taken_at": residueCanonStamp}),
			nil, nil, nil)
	}
	sensor := instance.NewValidInstance("Sensor", sensorType.ID(),
		immutable.WrapKey([]any{residueCanonStamp}),
		immutable.WrapProperties(map[string]any{"observed_at": residueCanonStamp}),
		nil, map[string]immutable.Value{"READINGS": immutable.Wrap([]any{reading()})}, nil)

	g := graph.New(s)
	if r := g.Add(t.Context(), sensor); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	if r := g.AddComposed(t.Context(), sensorType.ID(), graph.FormatKey(residueCanonStamp),
		"READINGS", reading()); r.OK() {
		t.Fatal("a duplicate sibling was accepted")
	}

	data, mr := snapshot.Marshal(t.Context(), g.Snapshot())
	if mr.HasErrors() {
		t.Fatalf("marshal: %s", mr)
	}
	// Only the duplicate record's STATED key moves, so it disagrees as text
	// with the instance it carries and names the same value.
	doc := rehashDocument(t, respellAfter(t, data, `"duplicates"`, residueCanonStamp, residueRawStamp))

	if _, res := snapshot.Load(t.Context(), doc, s); hasCode(res, diag.E_SNAPSHOT_MALFORMED) {
		t.Errorf("two spellings of one instant were called a key disagreement: %s", res)
	}
}

// group3ReaderSchema declares a (one) ADDRESS composition beside a (_:many)
// LINES, so the reader's cardinality guard has both the shape it refuses and
// the shape it must leave alone.
func group3ReaderSchema(t *testing.T) *schema.Schema {
	t.Helper()
	const src = `schema "group3_reader"

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
	s, res := schema.LoadString(t.Context(), src, "group3_reader.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

// group3OneSlotDocument writes a document whose (one) ADDRESS slot holds a
// sole occupant, and returns it beside a copy carrying a second one.
//
// The second occupant is spliced into the composed array rather than written:
// every writer refuses the shape, and a schema whose cardinality differs is
// refused at the schema-hash gate before the structural walk runs, so a
// foreign document is the only way to present it to the reader. The splice is
// byte-level because the reader requires yammm_snapshot to be the object's
// first key, which a JSON round trip through a Go map does not preserve.
func group3OneSlotDocument(t *testing.T, s *schema.Schema) (sole, twoOccupants []byte) {
	t.Helper()
	orderType, _ := s.Type("Order")
	addressType, _ := s.Type("Address")

	order := instance.NewValidInstance("Order", orderType.ID(),
		immutable.WrapKey([]any{"o1"}),
		immutable.WrapProperties(map[string]any{"id": "o1"}), nil,
		map[string]immutable.Value{"ADDRESS": immutable.Wrap([]any{
			instance.NewValidInstance("Address", addressType.ID(), immutable.WrapKey([]any{"first"}),
				immutable.WrapProperties(map[string]any{"street": "first"}), nil, nil, nil),
		})}, nil)

	g := graph.New(s)
	if r := g.Add(t.Context(), order); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	data, mr := snapshot.Marshal(t.Context(), g.Snapshot())
	if mr.HasErrors() {
		t.Fatalf("marshal: %s", mr)
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
	s := group3ReaderSchema(t)
	sole, twoOccupants := group3OneSlotDocument(t, s)

	// The control runs first: a sole occupant must still load, or the guard
	// would pass by refusing everything.
	if _, res := snapshot.Load(t.Context(), sole, s); res.HasErrors() {
		t.Fatalf("a sole occupant of a (one) composition was refused: %s", res)
	}

	_, res := snapshot.Load(t.Context(), twoOccupants, s)
	if !res.HasErrors() {
		t.Fatal("two occupants of a (one) composition loaded clean")
	}
	if !hasCode(res, diag.E_DUPLICATE_COMPOSED_PK) {
		t.Errorf("want E_DUPLICATE_COMPOSED_PK; got %s", res)
	}
	// Verify reads the same walk, so it must agree.
	if !hasCode(snapshot.Verify(t.Context(), twoOccupants, s), diag.E_DUPLICATE_COMPOSED_PK) {
		t.Error("Verify did not refuse the shape Load refuses")
	}
}
