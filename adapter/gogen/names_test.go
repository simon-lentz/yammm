package gogen

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

func TestGoIdent(t *testing.T) {
	// goExportedIdent applies the merged initialism set; defaultInitialisms is
	// gogen's golint base (domain acronyms would arrive via WithInitialisms).
	cases := map[string]string{
		"fips":     "Fips",
		"in_state": "InState",
		"id":       "ID",
		"base_url": "BaseURL",
		"http_api": "HTTPAPI", // both in the full golint set
		// Digit-leading input (e.g. an arbitrary schema name used as a collision
		// qualifier) must still yield an EXPORTED identifier: the transform produces
		// "_2020Census" (unexported), so goExportedIdent prefixes "X".
		"2020census": "X_2020Census",
		"":           "X",
	}
	for in, want := range cases {
		if got := goExportedIdent(in, defaultInitialisms); got != want {
			t.Errorf("goExportedIdent(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGoPackageName(t *testing.T) {
	cases := map[string]string{
		"municipal": "municipal",
		"Geo Data":  "geodata",
		"123schema": "schema",
		"":          "schema",
	}
	for in, want := range cases {
		if got := goPackageName(in); got != want {
			t.Errorf("goPackageName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBuildNameTable_TypeDataTypeCollision pins the cross-kind collision path: a type
// and a datatype sharing a name both LOAD (separate indexTypes/indexDataTypes), so the
// name table must catch the resulting Go clash. Same-schema, schema-qualification
// cannot separate them, so it is a hard error. (Loadability verified against source.)
func TestBuildNameTable_TypeDataTypeCollision(t *testing.T) {
	s, res := schema.LoadString(context.Background(),
		"schema \"geo\"\n\ntype Region = String\n\ntype Region {\n\tid String primary\n}", "collide.yammm")
	if res.HasErrors() {
		t.Fatalf("load (expected to succeed — loader permits the overlap): %v", res.Err())
	}
	if _, err := buildNameTable(s, defaultInitialisms); err == nil {
		t.Fatal("expected a hard collision error for a type and a datatype both named Region")
	}
}

// TestBuildNameTable_ReservedNameQualified pins the reserved-name path: a schema type
// named "Graph" must be qualified away from the emitted Graph aggregate, not silently
// shadow it.
func TestBuildNameTable_ReservedNameQualified(t *testing.T) {
	s, res := schema.LoadString(context.Background(),
		"schema \"geo\"\n\ntype Graph {\n\tid String primary\n}", "g.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	nt, err := buildNameTable(s, defaultInitialisms)
	if err != nil {
		t.Fatalf("buildNameTable: %v", err)
	}
	gt, _ := s.Type("Graph")
	name, ok := nt.goType(gt.ID())
	if !ok || name == "Graph" {
		t.Errorf("type Graph must be qualified away from the reserved aggregate name, got %q (ok=%v)", name, ok)
	}
}

// TestLayoutTypeBase pins the per-layout type name: every letter and digit
// of the layout, nothing else, behind a fixed prefix — so the name depends
// on the layout alone and an unrelated schema edit cannot move it.
func TestLayoutTypeBase(t *testing.T) {
	cases := map[string]string{
		"2006-01-02 15:04:05":        "Timestamp20060102150405",
		"2006-01-02T15:04:05Z07:00":  "Timestamp20060102T150405Z0700",
		"Jan _2, 2006":               "TimestampJan22006",
		"--":                         "Timestamp",
		"02 Jänner 2006":             "Timestamp02Jänner2006",
		"2006-01-02T15:04:05.000000": "Timestamp20060102T150405000000",
	}
	for in, want := range cases {
		if got := layoutTypeBase(in); got != want {
			t.Errorf("layoutTypeBase(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBuildNameTable_DateReserved pins that the emitted Date type's name is
// taken before any schema entity is assigned. Date is a DSL keyword, so no
// schema can claim it; the reservation is what keeps the invariant true
// should that ever change.
func TestBuildNameTable_DateReserved(t *testing.T) {
	s, res := schema.LoadString(context.Background(),
		"schema \"geo\"\n\ntype County {\n\tid String primary\n}", "d.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	nt, err := buildNameTable(s, defaultInitialisms)
	if err != nil {
		t.Fatalf("buildNameTable: %v", err)
	}
	if !nt.taken[dateGoName] {
		t.Errorf("%q is not reserved in the name table", dateGoName)
	}
}
