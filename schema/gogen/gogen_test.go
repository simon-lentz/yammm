package gogen_test

import (
	"bytes"
	"context"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/gogen"
)

// checkGolden compares got against testdata/<name>.go.golden via the shared
// yammmtest flow (one repo-wide -update flag; the .go.golden suffix marks the
// content as generated Go source).
func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	yammmtest.Golden(t, name+".go", got)
}

func TestMarshal_EmbedsTheSourceStore(t *testing.T) {
	s := loadSchema(t, "scalars")
	// Marshal returns only what finish returns, and finish re-loads the store
	// (TestFinish_RefusesAnEmbeddedStoreThatDoesNotReload); these pin the shape.
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("var serializedSources = map[string]string{")) {
		t.Error("missing the embedded source store")
	}
	if !bytes.Contains(got, []byte(schema.StructuralHash(s))) {
		t.Error("SchemaHash does not match schema.StructuralHash")
	}
}

// TestMarshal_Imports exercises a real imported (multi-source) schema end-to-end:
// closure flatten, cross-schema reference + naming, the faithful cross-schema
// Where-block PK, and the module-root-relative multi-source embedded store. A
// successful Marshal also proves the multi-source round-trip self-check passed.
func TestMarshal_Imports(t *testing.T) {
	s := loadSchema(t, "imports/main")
	if len(slices.Collect(s.Imports())) == 0 {
		t.Fatal("expected imports/main to declare an import")
	}
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type Region struct",
		"type County struct",
		"type EDGE_County_in_region_Region struct",
		"Code RegionCode", // faithful cross-schema Where-block PK (not Code string)
		"var serializedSources = map[string]string{",
		`"common.yammm":`,
		`"main.yammm":`,
		`const SerializedEntry = "main.yammm"`,
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("output missing %q", want)
		}
	}
	// No absolute generation-machine path may leak into the embedded keys/entry —
	// the keys must be module-root-relative so the .go output is reproducible.
	absDir, _ := filepath.Abs(filepath.Join("testdata", "imports"))
	if bytes.Contains(got, []byte(absDir)) {
		t.Errorf("absolute path %q leaked into output (keys must be module-root-relative)", absDir)
	}
}

// TestMarshal_CompositePKWhereBlock pins gogen's composite-primary-key edge handling:
// an association targeting a type with multiple `primary` fields (Region: country +
// code) renders an EDGE_ Where block with one field per key component, so the generated
// edge can address its target by the full composite key. Where blocks are emitted after
// the node structs, so slicing from the EDGE_ struct excludes Region's own country/code
// fields — the assertion can only be satisfied by the Where block itself.
func TestMarshal_CompositePKTargetFields(t *testing.T) {
	s := loadSchema(t, "composite_pk")
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	src := string(got)
	start := strings.Index(src, "type EDGE_Sensor_located_in_Region struct {")
	if start < 0 {
		t.Fatalf("missing EDGE_Sensor_located_in_Region struct:\n%s", src)
	}
	edge := src[start:]
	if end := strings.Index(edge, "\n}\n"); end >= 0 {
		edge = edge[:end]
	}
	for _, want := range []string{`json:"_target_country"`, `json:"_target_code"`} {
		if !strings.Contains(edge, want) {
			t.Errorf("composite-PK EDGE_ struct missing flattened %q:\n%s", want, edge)
		}
	}
}

// TestMarshal_CrossSchemaInheritance is the regression for declaring-schema
// resolution of inherited members: City extends an IMPORTED abstract type, so its
// inherited DataType property (region -> RegionCode, not string) and inherited
// composition (HAS_MARKER -> []*Marker, not a generation error) must resolve against
// the parent's schema (common), not the inheritor's.
func TestMarshal_CrossSchemaInheritance(t *testing.T) {
	s := loadSchema(t, "imports/inherit_main")
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type City struct",
		"type Located struct",
		"type Marker struct",
		"Region", "RegionCode", // inherited DataType property kept its named type
		"HasMarker", "[]*Marker", // inherited composition resolved
		`json:"region"`,
		`json:"has_marker,omitempty"`,
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("output missing %q", want)
		}
	}
	// The bug this guards against would degrade region to string; assert the named
	// type survived (the byte-exact golden pins the exact layout).
	if bytes.Contains(got, []byte("Region    string")) {
		t.Error("inherited DataType property degraded to string (cross-schema resolution failed)")
	}
}

// TestMarshal_CrossSchemaCollision pins a cross-schema name collision: two schemas in
// the closure both declare a type Region, so neither can take the bare Go name "Region"
// and each takes its exact spelling (Type_geo__Region / Type_common__Region), which the
// Graph aggregate names. The EDGE_ struct's bare spelling is built from the target's
// name, and nothing else claims it.
func TestMarshal_CrossSchemaCollision(t *testing.T) {
	s := loadSchema(t, "imports/collision_main")
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type Type_geo__Region struct",
		"type Type_common__Region struct",
		"type EDGE_County_in_region_Region struct",
		"InRegion *EDGE_County_in_region_Region ",
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("output missing %q", want)
		}
	}
	// Neither Region may keep the bare, ambiguous Go name.
	if bytes.Contains(got, []byte("type Region struct")) {
		t.Error("a colliding Region kept the bare Go name; expected its exact spelling")
	}
}

// TestMarshal_DiamondImport pins the closure dedup-by-SourceID: the entry imports left
// and right, both of which import the same base schema. The shared base type must be
// emitted EXACTLY ONCE (a double-walk would duplicate the declaration and fail the
// writer's go/types pass), and the embedded store must carry all four sources.
func TestMarshal_DiamondImport(t *testing.T) {
	s := loadSchema(t, "imports/diamond_main")
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(got, []byte("type Shared struct")); n != 1 {
		t.Errorf("diamond-shared base emitted %d times, want exactly 1", n)
	}
	for _, want := range []string{
		`"diamond_base.yammm":`,
		`"diamond_left.yammm":`,
		`"diamond_right.yammm":`,
		`const SerializedEntry = "diamond_main.yammm"`,
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("output missing %q", want)
		}
	}
}

// TestMarshal_EdgeWhereKeyCollision pins that an edge property named "where" keeps
// its wire key. Two struct fields sharing a JSON key make encoding/json drop both,
// and go/types does not catch duplicate struct tags. The _target_ fields flatten
// beside the properties, and the underscore rule keeps the namespaces apart.
func TestMarshal_EdgeWhereKeyCollision(t *testing.T) {
	s := loadSchema(t, "edge_where_collision")
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(got, []byte("`json:\"where\"`")); n != 1 {
		t.Errorf("want exactly one `json:\"where\"` (the edge property); got %d:\n%s", n, got)
	}
	if bytes.Contains(got, []byte("`json:\"Where2\"`")) {
		t.Errorf("the deleted clash fallback re-appeared:\n%s", got)
	}
	if !bytes.Contains(got, []byte("`json:\"_target_")) {
		t.Errorf("EDGE_ struct carries no flattened _target_ field:\n%s", got)
	}
}

// TestMarshal_Initialisms exercises the WithInitialisms injection end-to-end: jwt is NOT
// in gogen's default golint set, so by default jwt_token -> JwtToken; injecting "JWT"
// upper-cases it wholesale to JWTToken. This is the consumer-vocabulary path that keeps
// domain acronyms at the call site, never in yammm.
func TestMarshal_Initialisms(t *testing.T) {
	s := loadSchema(t, "initialisms")

	def, err := gogen.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(def, []byte("JwtToken string")) {
		t.Errorf("default set: expected JwtToken, got:\n%s", def)
	}

	got, err := gogen.Marshal(s, gogen.WithInitialisms("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("JWTToken string")) {
		t.Errorf("WithInitialisms(JWT): expected JWTToken, got:\n%s", got)
	}
	// The golden is the WithInitialisms output, so it locks the injected-acronym shape.
	checkGolden(t, "initialisms", got)
}

// TestMarshal_TypeChecks is defense beyond the writer's internal go/types pass: it
// type-checks the comprehensive output against the REAL time package via
// importer.Default() (the test always runs with a Go toolchain present), complementing
// Marshal's hermetic timeImporter stub by confirming the output type-checks against the
// actual time, not only the stub's opaque Time.
func TestMarshal_TypeChecks(t *testing.T) {
	for _, name := range []string{"full", "temporal", "temporal_edge", "temporal_list"} {
		t.Run(name, func(t *testing.T) {
			s := loadSchema(t, name)
			got, err := gogen.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "gen.go", got, parser.AllErrors)
			if err != nil {
				t.Fatalf("generated source does not parse: %v\n%s", err, got)
			}
			conf := types.Config{Importer: importer.Default()}
			if _, err := conf.Check(f.Name.Name, fset, []*ast.File{f}, nil); err != nil {
				t.Fatalf("generated source does not type-check: %v\n%s", err, got)
			}
		})
	}
}

// TestMarshal_TemporalTypes pins the shape a Date and a Timestamp take in every
// position: a Date is a generated struct embedding time.Time with a JSON codec
// that speaks YYYY-MM-DD, and a Timestamp is time.Time, whose own codec speaks
// the stored text.
func TestMarshal_TemporalTypes(t *testing.T) {
	got, err := gogen.Marshal(loadSchema(t, "temporal"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"import (\n\t\"encoding/json\"\n\t\"time\"\n)",
		"type Date struct{ time.Time }",
		"func (v Date) MarshalJSON() ([]byte, error)",
		"func (v *Date) UnmarshalJSON(b []byte) error",
		"type Day struct{ time.Time }",
		"func (v Day) MarshalJSON() ([]byte, error) { return marshalDate(v.Time) }",
		"func (v *Day) UnmarshalJSON(b []byte) error { return unmarshalDate(b, &v.Time) }",
		"type Stamp struct{ time.Time }",
		"Installed      Date ",
		"Decommissioned *Date ",
		"CreatedAt      time.Time ",
		"SeenAt         *time.Time ",
		"Days           []Date ",
		"Stamps         []time.Time ",
		"At time.Time ",
		"On *Date ",
		"Since    *time.Time ",
		"func marshalDate(t time.Time) ([]byte, error)",
		"func unmarshalDate(b []byte, dst *time.Time) error",
		"t.Format(time.DateOnly)",
		"time.Parse(time.DateOnly, s)",
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	for _, absent := range []string{
		"func (v Stamp) MarshalJSON", // time.Time's codec is the stored text
		"Layout",
		"layout",
		"type Timestamp",
	} {
		if bytes.Contains(got, []byte(absent)) {
			t.Errorf("output holds %q:\n%s", absent, got)
		}
	}
}

// TestMarshal_TemporalEdge pins the two positions emitField does not reach
// through an owning type: a Date primary key in a Where block, and a
// Timestamp on an edge property.
func TestMarshal_TemporalEdge(t *testing.T) {
	got, err := gogen.Marshal(loadSchema(t, "temporal_edge"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type EDGE_Log_on_Day struct {\n\tTargetOn Date      `json:\"_target_on\"`\n\tAt       time.Time `json:\"at\"`\n}",
		"type Date struct{ time.Time }",
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// TestMarshal_TemporalInsideList pins that a temporal position reached only
// inside a List generates what it names — the Date type or the time import —
// under a List DataType and a nested List property, and that a List of a
// temporal DataType names that DataType at every depth, so no Date type is
// generated for it.
func TestMarshal_TemporalInsideList(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		body         string
		want, absent []string
	}{
		"list datatype over date": {
			body: "type Days = List<Date>\ntype Doc {\n\tid String primary\n\td Days\n}\n",
			want: []string{"type Days []Date", "type Date struct{ time.Time }"},
		},
		"list datatype over a date datatype": {
			body:   "type Day = Date\ntype Dates = List<Day>\ntype Doc {\n\tid String primary\n}\n",
			want:   []string{"type Dates []Day", "type Day struct{ time.Time }"},
			absent: []string{"type Date struct"},
		},
		"nested list property over date": {
			body: "type Doc {\n\tid String primary\n\tgrid List<List<Date>>\n}\n",
			want: []string{"Grid [][]Date ", "type Date struct{ time.Time }"},
		},
		"nested list property over a date datatype": {
			body:   "type Day = Date\ntype Doc {\n\tid String primary\n\tgrid List<List<Day>>\n}\n",
			want:   []string{"Grid [][]Day ", "type Day struct{ time.Time }"},
			absent: []string{"type Date struct"},
		},
		"nested list property over a timestamp datatype": {
			body:   "type At = Timestamp\ntype Doc {\n\tid String primary\n\tshifts List<List<At>>\n}\n",
			want:   []string{"Shifts [][]At ", "type At struct{ time.Time }"},
			absent: []string{"func (v At) MarshalJSON", "encoding/json"},
		},
		"nested list property over timestamp": {
			body:   "type Doc {\n\tid String primary\n\tshifts List<List<Timestamp>>\n}\n",
			want:   []string{"Shifts [][]time.Time ", "import \"time\""},
			absent: []string{"encoding/json"},
		},
		"list datatype over timestamp": {
			body: "type Stamps = List<Timestamp>\ntype Doc {\n\tid String primary\n}\n",
			want: []string{"type Stamps []time.Time", "import \"time\""},
		},
		"list datatype over a timestamp datatype": {
			body: "type At = Timestamp\ntype Ats = List<At>\ntype Doc {\n\tid String primary\n}\n",
			want: []string{"type Ats []At", "type At struct{ time.Time }"},
		},
		"timestamp datatype property": {
			body:   "type At = Timestamp\ntype Doc {\n\tid String primary\n\tc At\n}\n",
			want:   []string{"C  *At ", "type At struct{ time.Time }"},
			absent: []string{"func (v At) MarshalJSON", "encoding/json"},
		},
		"list property over a date datatype": {
			body:   "type Day = Date\ntype Doc {\n\tid String primary\n\tdays List<Day>\n}\n",
			want:   []string{"Days []Day ", "type Day struct{ time.Time }"},
			absent: []string{"type Date struct"},
		},
		"date datatype property": {
			body:   "type Day = Date\ntype Doc {\n\tid String primary\n\td Day\n}\n",
			want:   []string{"D  *Day ", "type Day struct{ time.Time }"},
			absent: []string{"type Date struct"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, res := schema.LoadString(context.Background(), "schema \"cal\"\n\n"+tc.body, "cal.yammm")
			if res.HasErrors() {
				t.Fatalf("load: %v", res.Err())
			}
			got, err := gogen.Marshal(s)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			for _, want := range tc.want {
				if !bytes.Contains(got, []byte(want)) {
					t.Errorf("output missing %q:\n%s", want, got)
				}
			}
			for _, absent := range tc.absent {
				if bytes.Contains(got, []byte(absent)) {
					t.Errorf("output holds %q:\n%s", absent, got)
				}
			}
		})
	}
}

// TestMarshal_TemporalOnlyInAnImport pins that registration walks the whole
// closure: the entry declares no temporal position, and the one import declares
// a List DataType over Date and a nested List property over Timestamp.
func TestMarshal_TemporalOnlyInAnImport(t *testing.T) {
	t.Parallel()

	got, err := gogen.Marshal(loadSchema(t, "imports/temporal_main"))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{
		"type Date struct{ time.Time }",
		"type Days []Date",
		"On [][]time.Time ",
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// TestMarshal_GraphKeysAreAddressableTags pins the Graph aggregate's json keys
// as the type tags an instance document is keyed by: bare for the entry schema's types and
// alias-qualified for a direct import. A type the entry schema reaches only
// through another import has no such name and no Graph field, though its
// struct is still emitted; and an entry type that shares its name with one
// keeps its bare key rather than its exact Go spelling.
func TestMarshal_GraphKeysAreAddressableTags(t *testing.T) {
	cases := map[string]struct{ keys, absent, structs []string }{
		"imports/main": {
			keys:   []string{"County", "common.Region"},
			absent: []string{"county"},
		},
		"graph_collision": {
			keys: []string{"ABCDef", "AbcDef"},
		},
		"imports/tagform_collision_main": {
			keys:    []string{"Main", "left.Left", "right.Right"},
			absent:  []string{"Type_leftbase__Node", "Type_rightbase__Node"},
			structs: []string{"Type_leftbase__Node", "Type_rightbase__Node"},
		},
		"imports/entry_shadow_main": {
			keys:    []string{"Node", "mid.Mid"},
			absent:  []string{"Type_emain__Node", "Type_eleaf__Node"},
			structs: []string{"Type_emain__Node", "Type_eleaf__Node"},
		},
		"imports/diamond_main": {
			keys:    []string{"Top", "left.Left", "right.Right"},
			absent:  []string{"Shared"},
			structs: []string{"Shared"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := gogen.Marshal(loadSchema(t, name))
			if err != nil {
				t.Fatal(err)
			}
			keys := graphKeys(t, got)
			for _, want := range tc.keys {
				if !slices.Contains(keys, want) {
					t.Errorf("Graph keys %q lack %q", keys, want)
				}
			}
			for _, bad := range tc.absent {
				if slices.Contains(keys, bad) {
					t.Errorf("Graph keys %q hold %q", keys, bad)
				}
			}
			for _, name := range tc.structs {
				if !bytes.Contains(got, []byte("type "+name+" struct {")) {
					t.Errorf("output lacks the struct %s:\n%s", name, got)
				}
			}
		})
	}
}

// TestMarshal_UniformSerializedSources pins the shape a consumer that handles
// more than one generated package depends on: one re-load surface, emitted
// identically whatever the source count, over one unexported store.
func TestMarshal_UniformSerializedSources(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		entryKey string
	}{
		{name: "scalars", entryKey: "scalars.yammm"},
		{name: "imports/main", entryKey: "main.yammm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := gogen.Marshal(loadSchema(t, tc.name))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			for _, w := range []string{
				"var serializedSources = map[string]string{",
				"const SerializedEntry = " + strconv.Quote(tc.entryKey),
				"func SerializedSources() map[string][]byte {",
				"m := make(map[string][]byte, len(serializedSources))",
			} {
				if !bytes.Contains(got, []byte(w)) {
					t.Errorf("output missing %q", w)
				}
			}
			// The schema source is embedded once, in one store.
			if n := bytes.Count(got, []byte("var serializedSources =")); n != 1 {
				t.Errorf("the source store is declared %d times, want 1", n)
			}
			// The retired two-shape emission leaves nothing behind.
			for _, gone := range []string{"SerializedModel", "SerializedModelEntry"} {
				if bytes.Contains(got, []byte(gone)) {
					t.Errorf("retired identifier %s still emitted", gone)
				}
			}
		})
	}
}

// TestMarshal_RelativeImport pins that a relative import generates, keyed
// beside its entry. Where a key follows the import text rather than the file's
// identity is pinned by TestMarshal_SymlinkedImportDirKeysByImportText.
func TestMarshal_RelativeImport(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, "imports/rel_main")
	if len(slices.Collect(s.Imports())) != 1 {
		t.Fatalf("expected imports/rel_main to declare one import, got %d", len(slices.Collect(s.Imports())))
	}
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{
		"type Site struct",
		"type Zone struct",
		`"rel_dep.yammm":`,
		`const SerializedEntry = "rel_main.yammm"`,
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("output missing %q", want)
		}
	}
}

// TestMarshal_SerializedEntryReserved pins the reservedNames addition: a schema
// entity named SerializedEntry takes its exact spelling rather than the emitted
// const's Go name, which would type-check-fail the whole file.
func TestMarshal_SerializedEntryReserved(t *testing.T) {
	t.Parallel()

	got, err := gogen.Marshal(loadSchema(t, "reserved_name"))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Contains(got, []byte("type Type_geo__SerializedEntry struct")) {
		t.Errorf("expected the schema type to take its exact spelling:\n%s", got)
	}
	if bytes.Contains(got, []byte("type SerializedEntry struct")) {
		t.Error("the schema type took the reserved SerializedEntry name")
	}
}

func TestMarshal_Golden(t *testing.T) {
	cases := []string{"scalars", "named", "inheritance", "relations", "shared_edge", "edge_datatype", "inherited_edge", "edge_where_collision", "composite_pk", "graph", "graph_collision", "imports/main", "imports/inherit_main", "imports/collision_main", "imports/diamond_main", "imports/rel_main", "imports/tagform_collision_main", "imports/qualified_name_main", "imports/temporal_main", "reserved_name", "full", "temporal", "temporal_edge", "temporal_list", "inline_enum_list"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			s := loadSchema(t, name)
			got, err := gogen.Marshal(s)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			checkGolden(t, name, got)
		})
	}
}

// TestMarshal_ModuleRoot pins the module-root-aware embedded-source keys for a
// registry-style layout: the module root is an ANCESTOR of the entry's directory
// (root/a/b/entry.yammm importing root/lib/dep.yammm by module-style path), so
// entry-directory-relative keys would be "../"-shaped and would not match the
// import statement on re-load. Keys must be root-relative and "../"-free.
func TestMarshal_ModuleRoot(t *testing.T) {
	s := loadModrootSchema(t)
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type Contract struct",
		"type Vendor struct",
		`"a/b/entry.yammm":`,
		`"lib/dep.yammm":`,
		`const SerializedEntry = "a/b/entry.yammm"`,
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("output missing %q", want)
		}
	}
	if bytes.Contains(got, []byte("../")) {
		t.Error(`output contains "../" — keys must be module-root-relative, not entry-dir-relative`)
	}
	absRoot, _ := filepath.Abs(filepath.Join("testdata", "modroot"))
	if bytes.Contains(got, []byte(absRoot)) {
		t.Errorf("absolute path %q leaked into output", absRoot)
	}
	checkGolden(t, "modroot/entry", got)
}

// TestMarshal_ModuleRoot_CwdIndependent pins the fix for the silent disk
// fallback: Marshal's round-trip self-check must succeed (and produce
// identical bytes) regardless of the process working directory. Before the
// module-root-aware keys, the multi-source round-trip only resolved with cwd
// at the consumer's module root — from anywhere else it failed with
// E_IMPORT_RESOLVE.
func TestMarshal_ModuleRoot_CwdIndependent(t *testing.T) {
	s := loadModrootSchema(t) // resolve fixture paths before changing cwd
	want, err := gogen.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal from a foreign cwd: %v", err)
	}
	if !bytes.Equal(want, got) {
		t.Error("Marshal output differs across working directories")
	}
}

// loadModrootSchema loads the modroot fixture entry with the fixture tree's
// top directory as the module root (module root != entry directory).
func loadModrootSchema(t *testing.T, opts ...schema.LoadOption) schema.Schema {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("testdata", "modroot"))
	if err != nil {
		t.Fatalf("abs root: %v", err)
	}
	entry := filepath.Join(root, "a", "b", "entry.yammm")
	s, res := schema.Load(context.Background(), entry, append([]schema.LoadOption{schema.WithModuleRoot(root)}, opts...)...)
	if res.HasErrors() {
		t.Fatalf("load modroot entry: %v", res.Err())
	}
	return s
}

// loadSchema loads a testdata schema, failing on any diagnostic error.
func loadSchema(t *testing.T, name string) schema.Schema {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", name+".yammm"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	s, res := schema.Load(context.Background(), path)
	if res.HasErrors() {
		t.Fatalf("load %s: %v", name, res.Err())
	}
	return s
}

func TestMarshal_Header(t *testing.T) {
	s := loadSchema(t, "marker")
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.HasPrefix(got, []byte("// Code generated by yammm. DO NOT EDIT.")) {
		t.Errorf("missing generated header:\n%s", got)
	}
	if !bytes.Contains(got, []byte("package marker")) {
		t.Errorf("missing package decl:\n%s", got)
	}
}
