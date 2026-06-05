package gogen_test

import (
	"bytes"
	"context"
	"flag"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/adapter/gogen"
	"github.com/simon-lentz/yammm/schema"
)

var update = flag.Bool("update", false, "update golden files")

// checkGolden compares got against testdata/<name>.go.golden, updating it under -update.
func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	golden := filepath.Join("testdata", name+".go.golden")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil { //nolint:gosec // golden test fixture, not sensitive
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("output mismatch for %s.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestSerializedModel(t *testing.T) {
	s := loadSchema(t, "scalars")
	// Marshal succeeding already proves the round-trip self-check passed
	// (verifyRoundTrip runs inside Marshal); these assertions pin the shape.
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("var SerializedModel =")) {
		t.Error("missing SerializedModel")
	}
	if !bytes.Contains(got, []byte(schema.StructuralHash(s))) {
		t.Error("SchemaHash does not match schema.StructuralHash")
	}
}

// TestMarshal_NotSourceBacked pins the precondition: a Builder-built schema retains
// no source content (nil Sources()), so Marshal must reject it gracefully rather than
// emit a SerializedModel it cannot honor — and never panic on the nil Sources().
func TestMarshal_NotSourceBacked(t *testing.T) {
	s, res := schema.NewBuilder().
		WithName("geo").
		AddType("County").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		Build()
	if res.HasErrors() {
		t.Fatalf("build: %v", res.Err())
	}
	if _, err := gogen.Marshal(s); err == nil {
		t.Fatal("expected an error for a non-source-backed (Builder) schema")
	} else if !strings.Contains(err.Error(), "not source-backed") {
		t.Errorf("expected a 'not source-backed' error, got: %v", err)
	}
}

// TestMarshal_Imports exercises a real imported (multi-source) schema end-to-end:
// closure flatten, cross-schema reference + naming, the faithful cross-schema
// Where-block PK, and the module-root-relative multi-source SerializedModel. A
// successful Marshal also proves the multi-source round-trip self-check passed.
func TestMarshal_Imports(t *testing.T) {
	s := loadSchema(t, "imports/main")
	if s.ImportCount() == 0 {
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
		"var SerializedModel = map[string]string{",
		`"common.yammm":`,
		`"main.yammm":`,
		`const SerializedModelEntry = "main.yammm"`,
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

// TestMarshal_CrossSchemaCollision pins the cross-schema name-collision QUALIFICATION
// SUCCESS path (the complement of the hard-error path in names_test.go): two schemas in
// the closure both declare a type Region, so neither can take the bare Go name "Region"
// — the name table schema-qualifies them (GeoRegion / CommonRegion), and the qualified
// name flows through to the EDGE_ target and the Graph aggregate.
func TestMarshal_CrossSchemaCollision(t *testing.T) {
	s := loadSchema(t, "imports/collision_main")
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type GeoRegion struct",                          // entry-schema Region, qualified
		"type CommonRegion struct",                       // imported Region, qualified
		"type EDGE_County_in_region_CommonRegion struct", // edge target uses the qualified name
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("output missing %q", want)
		}
	}
	// Neither Region may keep the bare, ambiguous Go name.
	if bytes.Contains(got, []byte("type Region struct")) {
		t.Error("a colliding Region kept the bare Go name; expected schema-qualification")
	}
}

// TestMarshal_DiamondImport pins the closure dedup-by-SourceID: the entry imports left
// and right, both of which import the same base schema. The shared base type must be
// emitted EXACTLY ONCE (a double-walk would duplicate the declaration and fail the
// writer's go/types pass), and SerializedModel must carry all four sources.
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
		`const SerializedModelEntry = "diamond_main.yammm"`,
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("output missing %q", want)
		}
	}
}

// TestMarshal_EdgeWhereKeyCollision pins the fix for an edge property named "where"
// colliding with the synthesized Where block's JSON key. Two struct fields sharing a
// JSON key make encoding/json drop BOTH at marshal time, and go/types does not catch
// duplicate struct tags — so the block's wire key must fall back to its unique Go field
// name (the lossy-collision strategy emitGraph uses), while the edge property's own
// "where" key stays canonical.
func TestMarshal_EdgeWhereKeyCollision(t *testing.T) {
	s := loadSchema(t, "edge_where_collision")
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(got, []byte("`json:\"where\"`")); n != 1 {
		t.Errorf("want exactly one `json:\"where\"` (the edge property); got %d:\n%s", n, got)
	}
	if !bytes.Contains(got, []byte("`json:\"Where2\"`")) {
		t.Errorf("Where block did not fall back to a unique wire key on collision:\n%s", got)
	}
}

// TestMarshal_AssociationTargetNoPrimaryKey pins that gogen refuses an association whose
// target type has no primary key: its EDGE_ Where block would be empty (no way to identify
// the target node), so the generated edge could not round-trip the graph. A PK-less type is
// accepted at both schema-load and per-node instance validation, so gogen is the layer that
// must reject it — consistent with its error-don't-emit-broken-Go contract.
func TestMarshal_AssociationTargetNoPrimaryKey(t *testing.T) {
	src := "schema \"geo\"\n\ntype Tag {\n\tlabel String required\n}\n\ntype Doc {\n\tid String primary\n\t--> TAGGED (many) Tag\n}\n"
	s, res := schema.LoadString(context.Background(), src, "pkless.yammm")
	if res.HasErrors() {
		t.Fatalf("load (expected to succeed — the loader does not require a primary key): %v", res.Err())
	}
	_, err := gogen.Marshal(s)
	if err == nil {
		t.Fatal("expected an error: association targets a type with no primary key")
	}
	if !strings.Contains(err.Error(), "no primary key") {
		t.Errorf("expected a 'no primary key' error, got: %v", err)
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
	s := loadSchema(t, "full")
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
}

func TestMarshal_Golden(t *testing.T) {
	cases := []string{"scalars", "named", "inheritance", "relations", "shared_edge", "edge_datatype", "inherited_edge", "edge_where_collision", "graph", "graph_collision", "imports/main", "imports/inherit_main", "imports/collision_main", "imports/diamond_main", "full"}
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

// loadSchema loads a testdata schema, failing on any diagnostic error.
func loadSchema(t *testing.T, name string) *schema.Schema {
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
