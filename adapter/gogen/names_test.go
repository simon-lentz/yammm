package gogen

import (
	"context"
	"path/filepath"
	"strings"
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
		"type":      "type_",
		"Main":      "main_",
		"main":      "main_",
		"Init":      "init_",
		"in-it":     "init_",
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
	_, err := buildNameTable(s, defaultInitialisms)
	if err == nil {
		t.Fatal("expected a hard collision error for a type and a datatype both named Region")
	}
	if want := `type "Region" and datatype "Region" in schema "geo"`; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name both entities: want %s", err, want)
	}
}

// TestBuildNameTable_BareNamesBeforeQualified pins that every unique candidate
// takes its bare name before any shared candidate is qualified. A qualified
// name another entity already holds takes the table's numeric suffix: b's AFoo
// keeps "AFoo" and a's Foo, whose qualified name is also "AFoo", becomes
// "AFoo2"; b's MainBar keeps "MainBar" and the entry's Bar becomes "MainBar2".
func TestBuildNameTable_BareNamesBeforeQualified(t *testing.T) {
	abs, err := filepath.Abs(filepath.Join("testdata", "imports", "qualified_name_main.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	s, res := schema.Load(context.Background(), abs)
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	nt, err := buildNameTable(s, defaultInitialisms)
	if err != nil {
		t.Fatalf("buildNameTable: %v", err)
	}
	want := map[string]string{
		"main.Bar":    "MainBar2",
		"main.Holder": "Holder",
		"a.Foo":       "AFoo2",
		"a.Bar":       "ABar",
		"b.Foo":       "BFoo",
		"b.AFoo":      "AFoo",
		"b.MainBar":   "MainBar",
	}
	got := map[string]string{}
	for _, sc := range s.Closure() {
		for _, typ := range sc.TypesSlice() {
			name, ok := nt.goType(typ.ID())
			if !ok {
				t.Fatalf("%s.%s has no Go name", sc.Name(), typ.Name())
			}
			got[sc.Name()+"."+typ.Name()] = name
		}
	}
	if len(got) != len(want) {
		t.Errorf("named %d types, want %d: %v", len(got), len(want), got)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
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

// TestBuildNameTable_QualifiedNamePastAReservedName pins that a qualified name
// equal to a reserved one takes the numeric suffix: schema "schema" and schema
// "b" both declare Hash, and the entry's qualified "SchemaHash" is the emitted
// hash constant's name.
func TestBuildNameTable_QualifiedNamePastAReservedName(t *testing.T) {
	s, res := schema.LoadSourcesWithEntry(context.Background(), map[string][]byte{
		"main.yammm": []byte("schema \"schema\"\n\nimport \"b.yammm\" as b\n\ntype Hash {\n\tid String primary\n\t--> HAS_B (one) b.Hash\n}\n"),
		"b.yammm":    []byte("schema \"b\"\n\ntype Hash {\n\tid String primary\n}\n"),
	}, "main.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	nt, err := buildNameTable(s, defaultInitialisms)
	if err != nil {
		t.Fatalf("buildNameTable: %v", err)
	}
	want := map[string]string{"schema": "SchemaHash2", "b": "BHash"}
	for _, sc := range s.Closure() {
		typ, ok := sc.Type("Hash")
		if !ok {
			t.Fatalf("schema %s declares no Hash", sc.Name())
		}
		if got, _ := nt.goType(typ.ID()); got != want[sc.Name()] {
			t.Errorf("%s.Hash = %q, want %q", sc.Name(), got, want[sc.Name()])
		}
	}
}

// TestBuildNameTable_QualifiedNamesAreDeterministic pins the order qualified
// names are assigned in when two shared candidates qualify to one name: a's
// BFoo and a_b's Foo both qualify to "ABFoo", and a's takes it because "BFoo"
// sorts before "Foo". Map iteration order must not decide it, so the table is
// built repeatedly and must name the closure the same way every time.
func TestBuildNameTable_QualifiedNamesAreDeterministic(t *testing.T) {
	src := func(name, typ string) []byte {
		return []byte("schema \"" + name + "\"\n\ntype " + typ + " {\n\tid String primary\n}\n")
	}
	s, res := schema.LoadSourcesWithEntry(context.Background(), map[string][]byte{
		"main.yammm": []byte("schema \"main\"\n\nimport \"a.yammm\" as a\nimport \"d.yammm\" as d\nimport \"ab.yammm\" as ab\nimport \"c.yammm\" as c\n\ntype Holder {\n\tid String primary\n}\n"),
		"a.yammm":    src("a", "BFoo"),
		"d.yammm":    src("d", "BFoo"),
		"ab.yammm":   src("a_b", "Foo"),
		"c.yammm":    src("c", "Foo"),
	}, "main.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	want := map[string]string{
		"main.Holder": "Holder",
		"a.BFoo":      "ABFoo",
		"d.BFoo":      "DBFoo",
		"a_b.Foo":     "ABFoo2",
		"c.Foo":       "CFoo",
	}
	for range 64 {
		nt, err := buildNameTable(s, defaultInitialisms)
		if err != nil {
			t.Fatalf("buildNameTable: %v", err)
		}
		for _, sc := range s.Closure() {
			for _, typ := range sc.TypesSlice() {
				k := sc.Name() + "." + typ.Name()
				if got, _ := nt.goType(typ.ID()); got != want[k] {
					t.Fatalf("%s = %q, want %q", k, got, want[k])
				}
			}
		}
	}
}
