package snapshot_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/snapshot"
)

// The wire contract.
//
// The wire carries type identity once, in a types table, and every other
// position references a row by index. These tests drive the shapes that
// reference goes wrong in — two entries claiming one row, a position that
// carries no index at all, and a row whose schema no longer exists.

// wireTypeTable reads a v3 document's types table.
func wireTypeTable(t *testing.T, data []byte) []struct {
	SchemaPath string `json:"schema_path"`
	Name       string `json:"name"`
} {
	t.Helper()
	var doc struct {
		Types []struct {
			SchemaPath string `json:"schema_path"`
			Name       string `json:"name"`
		} `json:"types"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal types table: %v", err)
	}
	return doc.Types
}

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

// TestWireV3_DuplicateGroupEntryIsReported pins the structural rule that
// replaced the parallel-array length check: the instances section holds at
// most one entry per table row, so two entries claiming one row cannot be
// read as one group.
func TestWireV3_DuplicateGroupEntryIsReported(t *testing.T) {
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
	edited := append(append([]byte{}, data[:idx]...), append([]byte(`,{"type":1,"items":[]}`), data[idx:]...)...)

	_, res := snapshot.Load(ctx, edited, s, snapshot.WithSkipIntegrityCheck())
	if !hasCode(res, diag.E_SNAPSHOT_MALFORMED) {
		t.Errorf("two instances entries referencing one table row loaded without %s: %v",
			diag.E_SNAPSHOT_MALFORMED, res)
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

// TestWireV3_TableIsOrderedByIdentity pins the table's ordering. Two types
// can share a bare name, so ordering on a rendered name is not a total order
// over the rows; ordering on the identity is.
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

	var sameName int
	for _, row := range table {
		if row.Name == table[0].Name {
			sameName++
		}
	}
	if sameName < 2 {
		t.Fatalf("fixture is vacuous: no two rows share a name:\n%s", data)
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

// TestWireV3_UnresolvableSchemaPathIsReported pins the strict half of table
// resolution: a document was written by a writer that resolved every
// identity, so a row whose schema path no longer resolves means the schema
// moved, and silent rebinding by bare name is the one wrong answer. The
// document is refused with an Error the consumer can act on.
func TestWireV3_UnresolvableSchemaPathIsReported(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	data := v3Doc(t)

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
	if !hasCode(res, diag.E_SNAPSHOT_UNKNOWN_TYPE) {
		t.Errorf("a moved schema path loaded without %s: %v", diag.E_SNAPSHOT_UNKNOWN_TYPE, res)
	}
	if !res.HasErrors() || loaded != nil {
		t.Errorf("a row the closure does not declare must refuse the document, not rebind it: %v", res)
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

// TestWireV3_TableCarriesUnresolvedTargetTypes pins that a type denoted only
// as an unresolved edge's target still takes a table row; it holds no
// instances, so it takes no instances entry.
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

// The four-key rule.
//
// The wire contract fixes four top-level keys. The reader used to normalise an
// absent instances or diagnostics section into an empty one, so a truncated
// document loaded as a valid empty snapshot. Presence is the signal — an
// instances entry present with an empty items array means the snapshot holds
// the type and no instance of it — so absence has to be malformed for presence
// to mean anything. A null section is that same absence dressed as presence,
// and every other wire position already refuses null.

// sectionRange returns the byte range of one top-level section, from the comma
// that precedes its key through the end of its value. It exploits the fixed key
// order — instances runs to the diagnostics key, diagnostics runs to the
// document's final brace — and fails loudly when the shape it assumes moves.
func sectionRange(t *testing.T, data []byte, key string) (int, int) {
	t.Helper()
	anchor := fmt.Sprintf(`,%q:`, key)
	start := bytes.Index(data, []byte(anchor))
	if start < 0 {
		t.Fatalf("fixture shape changed; %s not found in:\n%s", anchor, data)
	}
	switch key {
	case "instances":
		end := bytes.Index(data, []byte(`,"diagnostics":`))
		if end < start {
			t.Fatalf("fixture shape changed; diagnostics does not follow instances in:\n%s", data)
		}
		return start, end
	case "diagnostics":
		if !bytes.HasSuffix(data, []byte("}")) {
			t.Fatalf("fixture shape changed; document does not end with a brace:\n%s", data)
		}
		return start, len(data) - 1
	default:
		t.Fatalf("sectionRange does not handle %q", key)
		return 0, 0
	}
}

// dropSection removes one top-level section. Spliced rather than
// re-marshalled: top-level key order is part of the wire contract and a map
// round trip loses it.
func dropSection(t *testing.T, data []byte, key string) []byte {
	t.Helper()
	start, end := sectionRange(t, data, key)
	return append(append([]byte{}, data[:start]...), data[end:]...)
}

// nullSection replaces one top-level section's value with null.
func nullSection(t *testing.T, data []byte, key string) []byte {
	t.Helper()
	start, end := sectionRange(t, data, key)
	out := append([]byte{}, data[:start]...)
	out = append(out, fmt.Sprintf(`,%q:null`, key)...)
	return append(out, data[end:]...)
}

// TestWireV3_AbsentSectionIsMalformed drives all three read surfaces against a
// document missing a section, because the surfaces reach decodeSections by two
// different routes: Load and Verify through the pipeline, Info on its own.
func TestWireV3_AbsentSectionIsMalformed(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	data := v3Doc(t)

	// The reason is asserted, not only the code. Absence and a null value both
	// draw E_SNAPSHOT_MALFORMED, so a test that reads the code alone cannot tell
	// the two guards apart and passes when the reader reports the wrong one.
	cases := []struct {
		name       string
		section    string
		null       bool
		wantReason string
	}{
		{"instances absent", "instances", false, "instances key not found in document"},
		{"instances null", "instances", true, "instances section is null"},
		{"diagnostics absent", "diagnostics", false, "diagnostics key not found in document"},
		{"diagnostics null", "diagnostics", true, "diagnostics section is null"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			edited := dropSection(t, data, tc.section)
			if tc.null {
				edited = nullSection(t, data, tc.section)
			}
			if bytes.Equal(edited, data) {
				t.Fatalf("edit is vacuous: the document is unchanged")
			}

			loaded, res := snapshot.Load(ctx, edited, s, snapshot.WithSkipIntegrityCheck())
			if !hasMalformedReason(res, tc.wantReason) {
				t.Errorf("Load did not report %q: %v", tc.wantReason, res)
			}
			if loaded != nil {
				t.Errorf("Load returned a snapshot beside a malformed document")
			}

			vres := snapshot.Verify(ctx, edited, s, snapshot.WithSkipIntegrityCheck())
			if !hasMalformedReason(vres, tc.wantReason) {
				t.Errorf("Verify did not report %q: %v", tc.wantReason, vres)
			}

			info, ires := snapshot.Info(ctx, edited)
			if !hasMalformedReason(ires, tc.wantReason) {
				t.Errorf("Info did not report %q: %v", tc.wantReason, ires)
			}
			if info != nil {
				t.Errorf("Info returned a summary beside a malformed document")
			}
		})
	}
}

// hasMalformedReason reports whether the result carries E_SNAPSHOT_MALFORMED
// naming the given reason.
func hasMalformedReason(result diag.Result, reason string) bool {
	for issue := range result.Issues() {
		if issue.Code() == diag.E_SNAPSHOT_MALFORMED && strings.Contains(issue.Message(), reason) {
			return true
		}
	}
	return false
}

// TestWireV3_EmptySectionsAreNotAbsence is the positive half of the rule. An
// empty snapshot writes both sections present and empty, and every read surface
// accepts it — so the refusal above is about absence, not about emptiness.
func TestWireV3_EmptySectionsAreNotAbsence(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithComposition(t)

	data, res := snapshot.Marshal(ctx, buildSnapshot(t, s))
	if res.HasErrors() {
		t.Fatalf("marshal: %v", res)
	}
	if !bytes.Contains(data, []byte(`"instances":[]`)) {
		t.Fatalf("fixture is vacuous: an empty snapshot did not write an empty instances section:\n%s", data)
	}
	if !bytes.Contains(data, []byte(`"diagnostics":`)) {
		t.Fatalf("fixture is vacuous: no diagnostics section:\n%s", data)
	}

	if loaded, lres := snapshot.Load(ctx, data, s); lres.HasErrors() || loaded == nil {
		t.Errorf("Load refused a document whose sections are present and empty: %v", lres)
	}
	if vres := snapshot.Verify(ctx, data, s); vres.HasErrors() {
		t.Errorf("Verify refused a document whose sections are present and empty: %v", vres)
	}
	if info, ires := snapshot.Info(ctx, data); ires.HasErrors() || info == nil {
		t.Errorf("Info refused a document whose sections are present and empty: %v", ires)
	}
}
