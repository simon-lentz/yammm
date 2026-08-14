package snapshot_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/snapshot"
)

// The v3 wire contract.
//
// v3 carries type identity once, in a types table, and every other position
// references a row by index. These tests drive the shapes that reference goes
// wrong in — a section that is not parallel to the table, a position that
// carries no index at all, and a row whose schema no longer exists.

// v3Doc marshals a small composed snapshot and returns the v3 bytes.
func v3Doc(t *testing.T) []byte {
	t.Helper()
	ctx := context.Background()
	data, res := snapshot.Marshal(ctx, composedPKCollision(t))
	if res.HasErrors() {
		t.Fatalf("marshal: %v", res)
	}
	return data
}

// TestWireV3_SectionLengthMismatchIsReported pins the check that replaced the
// name-keyed types/instances cross-check. The two sections denote the same
// rows, so a length difference means the document cannot be read positionally.
func TestWireV3_SectionLengthMismatchIsReported(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	data := v3Doc(t)

	// Spliced rather than re-marshalled: the top-level key order is part of the
	// wire contract, and a map round trip loses it.
	const boundary = `],"diagnostics":`
	idx := bytes.Index(data, []byte(boundary))
	if idx < 0 {
		t.Fatalf("fixture shape changed; %s not found in:\n%s", boundary, data)
	}
	edited := append(append([]byte{}, data[:idx]...), append([]byte(",[]"), data[idx:]...)...)

	_, res := snapshot.Load(ctx, edited, s, snapshot.WithSkipIntegrityCheck())
	if !hasCode(res, diag.E_SNAPSHOT_TYPE_MISMATCH) {
		t.Errorf("an instances section longer than the types table loaded without %s: %v",
			diag.E_SNAPSHOT_TYPE_MISMATCH, res)
	}
}

// TestWireV3_ComposedChildWithoutTypeIsReported pins the one position a v3
// writer must never leave to inference. A root instance takes its type from its
// position; a composed child has no position to take one from, so a child
// carrying no index is malformed rather than recoverable.
func TestWireV3_ComposedChildWithoutTypeIsReported(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	data := v3Doc(t)

	compact := new(bytes.Buffer)
	if err := json.Compact(compact, data); err != nil {
		t.Fatalf("compact: %v", err)
	}
	body := compact.String()
	const anchor = `"CHILDREN":[{"key":["c1"],"type":`
	idx := strings.Index(body, anchor)
	if idx < 0 {
		t.Fatalf("fixture shape changed; %s not found in:\n%s", anchor, body)
	}
	end := strings.Index(body[idx+len(anchor):], ",")
	if end < 0 {
		t.Fatalf("fixture shape changed; no field terminator after %s", anchor)
	}
	stripped := body[:idx+len(`"CHILDREN":[{"key":["c1"],`)] + body[idx+len(anchor)+end+1:]

	_, res := snapshot.Load(ctx, []byte(stripped), s, snapshot.WithSkipIntegrityCheck())
	if !hasCode(res, diag.E_SNAPSHOT_MALFORMED) {
		t.Errorf("a composed child carrying no type index loaded without %s: %v",
			diag.E_SNAPSHOT_MALFORMED, res)
	}
}

// TestWireV3_TableIsOrderedByIdentity pins the table's ordering. Two types can
// render the same tag, so ordering on the tag is not a total order over the
// rows; ordering on the identity is, and the instances section is positional
// against it.
func TestWireV3_TableIsOrderedByIdentity(t *testing.T) {
	ctx := context.Background()
	s := loadIdentitySchema(t)

	built, _, _ := collidingBeacons(t, s)
	data, res := snapshot.Marshal(ctx, built)
	if res.HasErrors() {
		t.Fatalf("marshal: %v", res)
	}

	table := wireTypeTable(t, data)
	if len(table) < 2 {
		t.Fatalf("fixture is vacuous: table holds %d rows, want at least 2", len(table))
	}

	var sameTag int
	for _, row := range table {
		if row.Tag == table[0].Tag {
			sameTag++
		}
	}
	if sameTag < 2 {
		t.Fatalf("fixture is vacuous: no two rows share a tag:\n%s", data)
	}

	for i := 1; i < len(table); i++ {
		prev := table[i-1].SchemaPath + ":" + table[i-1].Name
		cur := table[i].SchemaPath + ":" + table[i].Name
		if prev >= cur {
			t.Errorf("table row %d (%s) does not follow row %d (%s) in identity order",
				i, cur, i-1, prev)
		}
	}

	// A comparator that ties on the name leaves the tie to map iteration order,
	// so repeated marshals of the same snapshot stop agreeing.
	for range 8 {
		again, res := snapshot.Marshal(ctx, built)
		if res.HasErrors() {
			t.Fatalf("marshal: %v", res)
		}
		if !bytes.Equal(data, again) {
			t.Fatalf("Marshal is not deterministic for two types rendering one tag:\nfirst:\n%s\nlater:\n%s", data, again)
		}
	}
}

// TestWireV3_UnresolvableSchemaPathFallsBackToName pins the shape a document
// takes after its schema moved: the persisted path resolves to nothing, but the
// name still names a type. Discarding both would yield an identity that cannot
// be written back at all.
func TestWireV3_UnresolvableSchemaPathFallsBackToName(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	data := v3Doc(t)

	// Parent, not Child: a composed-only type holds no root instances, so its
	// survival is not observable through Snapshot.Types.
	const rooted = "Parent"
	row := -1
	for i, e := range wireTypeTable(t, data) {
		if e.Name == rooted {
			row = i
		}
	}
	if row < 0 {
		t.Fatalf("fixture carries no %s row:\n%s", rooted, data)
	}

	moved := bytes.Replace(data,
		[]byte(`{"schema_path":"`+wireTypeTable(t, data)[row].SchemaPath+`","name":"`+rooted+`"`),
		[]byte(`{"schema_path":"/nonexistent/moved.yammm","name":"`+rooted+`"`), 1)
	if bytes.Equal(moved, data) {
		t.Fatalf("fixture shape changed; %s row not found in:\n%s", rooted, data)
	}

	loaded, res := snapshot.Load(ctx, moved, s, snapshot.WithSkipIntegrityCheck())
	if res.HasErrors() {
		t.Fatalf("a moved schema path must fall back to the name: %v", res)
	}
	if loaded == nil {
		t.Fatal("no snapshot returned")
	}

	var found bool
	for _, id := range loaded.Types() {
		if id.Name() == rooted {
			found = true
			if id.SchemaPath().String() == "/nonexistent/moved.yammm" {
				t.Errorf("the fallback kept the unresolvable path instead of binding the name")
			}
		}
	}
	if !found {
		t.Errorf("type %q did not survive the fallback; loaded types: %v", rooted, loaded.Types())
	}
}

// TestWireV3_UnknownTableRowIsReported pins the other half: a row whose path and
// name both resolve to nothing is reported rather than silently bound.
func TestWireV3_UnknownTableRowIsReported(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	data := v3Doc(t)

	table := wireTypeTable(t, data)
	if len(table) == 0 {
		t.Fatal("fixture carries no types table")
	}
	ghosted := bytes.Replace(data,
		[]byte(`"name":"`+table[0].Name+`"`),
		[]byte(`"name":"NoSuchTypeAnywhere"`), 1)
	if bytes.Equal(ghosted, data) {
		t.Fatalf("fixture shape changed; name not found in:\n%s", data)
	}

	loaded, res := snapshot.Load(ctx, ghosted, s, snapshot.WithSkipIntegrityCheck())
	if !hasCode(res, diag.E_SNAPSHOT_UNKNOWN_TYPE) {
		t.Errorf("a types table row naming nothing loaded without %s: %v",
			diag.E_SNAPSHOT_UNKNOWN_TYPE, res)
	}
	if !res.HasErrors() || loaded != nil {
		t.Errorf("a row that denotes no type must refuse the document, not annotate it: %v", res)
	}
}

// TestWireV3_TableCarriesUnresolvedTargetTypes pins that a type denoted only as
// an unresolved edge's target still takes a table row. Its instances row is
// empty, which is what keeps the two sections parallel.
func TestWireV3_TableCarriesUnresolvedTargetTypes(t *testing.T) {
	ctx := context.Background()
	s := testSchema(t)

	snap := buildSnapshot(t, s,
		mustValidInstanceWithEdge(t, s, "Person", []any{"p1"},
			map[string]any{"name": "Alice"}, "EMPLOYER", [][]any{{"c-missing"}}))
	if len(snap.Unresolved()) == 0 {
		t.Fatal("fixture is vacuous: no unresolved edge")
	}

	data, res := snapshot.Marshal(ctx, snap)
	if res.HasErrors() {
		t.Fatalf("marshal: %v", res)
	}

	var company bool
	for _, row := range wireTypeTable(t, data) {
		if row.Name == "Company" {
			company = true
		}
	}
	if !company {
		t.Errorf("the unresolved edge's target type took no table row:\n%s", data)
	}

	if _, res := snapshot.Load(ctx, data, s); res.HasErrors() {
		t.Errorf("the document does not read back: %v", res)
	}
}
