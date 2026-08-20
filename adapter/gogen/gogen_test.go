package gogen_test

import (
	"bytes"
	"context"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/adapter/gogen"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
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
	// Marshal succeeding already proves the round-trip self-check passed
	// (verifyRoundTrip runs inside Marshal); these assertions pin the shape.
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

// TestMarshal_NotSourceBacked pins the precondition: a Builder-built schema retains
// no source content (nil Sources()), so Marshal must reject it gracefully rather than
// emit an embedded source store it cannot honor — and never panic on the nil Sources().
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
// Where-block PK, and the module-root-relative multi-source embedded store. A
// successful Marshal also proves the multi-source round-trip self-check passed.
func TestMarshal_Imports(t *testing.T) {
	s := loadSchema(t, "imports/main")
	if len(s.ImportsSlice()) == 0 {
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
func TestMarshal_CompositePKWhereBlock(t *testing.T) {
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
	for _, want := range []string{`json:"country"`, `json:"code"`} {
		if !strings.Contains(edge, want) {
			t.Errorf("composite-PK EDGE_ Where block missing %q:\n%s", want, edge)
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

// TestMarshal_RelativeImport is the regression anchor for the round-trip
// self-check's uniform arm. Every other fixture in this corpus imports
// module-style, so a self-check that could not serve a relative import would
// leave the whole corpus green and break generation for a shape the DSL
// supports. A successful Marshal is the proof: both round-trip arms run inside it.
func TestMarshal_RelativeImport(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, "imports/rel_main")
	if len(s.ImportsSlice()) != 1 {
		t.Fatalf("expected imports/rel_main to declare one import, got %d", len(s.ImportsSlice()))
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
// entity named SerializedEntry must be schema-qualified rather than taking the
// emitted const's Go name, which would type-check-fail the whole file.
func TestMarshal_SerializedEntryReserved(t *testing.T) {
	t.Parallel()

	got, err := gogen.Marshal(loadSchema(t, "reserved_name"))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Contains(got, []byte("type GeoSerializedEntry struct")) {
		t.Errorf("expected the schema type to be qualified away from the reserved name:\n%s", got)
	}
	if bytes.Contains(got, []byte("type SerializedEntry struct")) {
		t.Error("the schema type took the reserved SerializedEntry name")
	}
}

func TestMarshal_Golden(t *testing.T) {
	cases := []string{"scalars", "named", "inheritance", "relations", "shared_edge", "edge_datatype", "inherited_edge", "edge_where_collision", "composite_pk", "graph", "graph_collision", "imports/main", "imports/inherit_main", "imports/collision_main", "imports/diamond_main", "imports/rel_main", "reserved_name", "full"}
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

// TestMarshal_ModuleRoot_HermeticReload pins the documented consumer recipe
// for the embedded model: LoadSourcesWithEntry over the root-relative keys
// with module root "." and WithSourcesOnly re-loads the schema from a
// directory containing no .yammm files at all — the embedded map is
// self-contained and the filesystem never participates.
func TestMarshal_ModuleRoot_HermeticReload(t *testing.T) {
	s := loadModrootSchema(t)
	want := schema.StructuralHash(s)

	entrySrc, err := os.ReadFile(filepath.Join("testdata", "modroot", "a", "b", "entry.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	depSrc, err := os.ReadFile(filepath.Join("testdata", "modroot", "lib", "dep.yammm"))
	if err != nil {
		t.Fatal(err)
	}

	t.Chdir(t.TempDir())
	got, res := schema.LoadSourcesWithEntry(context.Background(), map[string][]byte{
		"a/b/entry.yammm": entrySrc,
		"lib/dep.yammm":   depSrc,
	}, "a/b/entry.yammm", ".", schema.WithSourcesOnly())
	if res.HasErrors() {
		t.Fatalf("hermetic re-load: %v", res.Err())
	}
	if h := schema.StructuralHash(got); h != want {
		t.Errorf("hermetic re-load hash mismatch: got %s, want %s", h, want)
	}
}

// TestMarshal_SymlinkedImportDir pins that the embedded-model round-trip
// check is independent of working-directory contents: a schema whose import
// path traverses a symlinked directory (lib -> real) marshals successfully
// even when the cwd is the module root itself, where every embedded key
// resolves to an existing file through the symlink. The re-load must match
// pre-registered keys textually rather than resolving them against disk.
func TestMarshal_SymlinkedImportDir(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{
		filepath.Join(root, "a", "b"),
		filepath.Join(root, "real"),
	} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("real", filepath.Join(root, "lib")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	entry := filepath.Join(root, "a", "b", "entry.yammm")
	entrySrc := []byte("schema \"registry\"\n\nimport \"lib/dep\" as dep\n\ntype Contract {\n\tcontract_id String primary\n\t--> SUPPLIED_BY (one) dep.Vendor\n}\n")
	depSrc := []byte("schema \"deplib\"\n\ntype Vendor {\n\tvendor_id String primary\n}\n")
	if err := os.WriteFile(entry, entrySrc, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "real", "dep.yammm"), depSrc, 0o600); err != nil {
		t.Fatal(err)
	}

	s, res := schema.Load(context.Background(), entry, schema.WithModuleRoot(root))
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	t.Chdir(root) // embedded keys now join the cwd to paths that exist through the symlink
	if _, err := gogen.Marshal(s); err != nil {
		t.Fatalf("Marshal with the module root as cwd: %v", err)
	}
}

// TestMarshal_ModuleRoot_SharedRegistryDeterministic pins that a shared
// Registry does not perturb the embedded keys: a second load of the same
// entry cache-hits the import (the registry returns the first load's
// schema pointer), and Marshal output stays byte-identical — keys derive
// from the entry schema's recorded ModuleRoot, not from how its imports
// were obtained.
func TestMarshal_ModuleRoot_SharedRegistryDeterministic(t *testing.T) {
	registry := schema.NewRegistry()
	first := loadModrootSchema(t, schema.WithRegistry(registry))
	second := loadModrootSchema(t, schema.WithRegistry(registry))

	a, err := gogen.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := gogen.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("shared-registry re-load changed Marshal output")
	}
}

// loadModrootSchema loads the modroot fixture entry with the fixture tree's
// top directory as the module root — the registry-style consumer load shape
// (module root != entry directory).
func loadModrootSchema(t *testing.T, opts ...schema.LoadOption) *schema.Schema {
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
