package gogen_test

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
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

// TestMarshal_TemporalTypes pins the shape a Date or custom-layout Timestamp
// takes in every position: a generated struct embedding time.Time with a
// JSON codec that speaks the stored string form, so a value adapter/json
// wrote decodes into the generated field.
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
		"type Timestamp20060102150405 struct{ time.Time }",
		"type Timestamp20060102T150405000000000Z0700 struct{ time.Time }",
		"type Day struct{ time.Time }",
		"func (v Day) MarshalJSON() ([]byte, error)",
		"type Wall struct{ time.Time }",
		"func (v Wall) MarshalJSON() ([]byte, error)",
		"type Stamp struct{ time.Time }",
		"Installed      Date ",
		"Decommissioned *Date ",
		"CreatedAt      time.Time ",
		"SeenWall       *Timestamp20060102150405 ",
		"SeenAt         Timestamp20060102T150405000000000Z0700 ",
		"Days           []Date ",
		"Walls          []Timestamp20060102150405 ",
		"At Timestamp20060102150405 ",
		"On *Date ",
		"func marshalTemporal(t time.Time, layout string) ([]byte, error)",
		"func unmarshalTemporal(b []byte, layout string, dst *time.Time) error",
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if bytes.Contains(got, []byte("func (v Stamp) MarshalJSON")) {
		t.Error("a DataType over the default Timestamp layout must not carry its own codec; time.Time's is the stored form")
	}
	if n := bytes.Count(got, []byte("type Timestamp20060102150405 struct")); n != 1 {
		t.Errorf("one layout must yield one type, got %d declarations", n)
	}
}

// TestMarshal_TemporalEdge pins the two positions emitField does not reach
// through an owning type: a Date primary key in a Where block, and a
// custom-layout Timestamp on an edge property.
func TestMarshal_TemporalEdge(t *testing.T) {
	got, err := gogen.Marshal(loadSchema(t, "temporal_edge"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type EDGE_Log_on_Day struct {\n\tTargetOn Date                    `json:\"_target_on\"`\n\tAt       Timestamp20060102150405 `json:\"at\"`\n}",
		"type Date struct{ time.Time }",
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// TestMarshal_TemporalInsideList pins that a temporal position reached only
// inside a List generates what it names — the Date or layout type, or the time
// import — under a List DataType and a nested List property, and that a List
// of a temporal DataType names that DataType at every depth, so no Date or
// layout type is generated for it.
func TestMarshal_TemporalInsideList(t *testing.T) {
	t.Parallel()

	const wall = `type Wall = Timestamp["2006-01-02 15:04:05"]` + "\n"
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
		"list datatype over a layout datatype": {
			body:   wall + "type Walls = List<Wall>\ntype Doc {\n\tid String primary\n}\n",
			want:   []string{"type Walls []Wall", "type Wall struct{ time.Time }"},
			absent: []string{"type Timestamp20060102150405 struct"},
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
		"nested list property over a layout datatype": {
			body:   wall + "type Doc {\n\tid String primary\n\tshifts List<List<Wall>>\n}\n",
			want:   []string{"Shifts [][]Wall ", "type Wall struct{ time.Time }"},
			absent: []string{"type Timestamp20060102150405 struct"},
		},
		"list datatype over timestamp": {
			body: "type Stamps = List<Timestamp>\ntype Doc {\n\tid String primary\n}\n",
			want: []string{"type Stamps []time.Time", "import \"time\""},
		},
		"list datatype over a timestamp datatype": {
			body: "type At = Timestamp\ntype Ats = List<At>\ntype Doc {\n\tid String primary\n}\n",
			want: []string{"type Ats []At", "type At struct{ time.Time }"},
		},
		"layout datatype property": {
			body:   "type Clock = Timestamp[\"15:04\"]\ntype Doc {\n\tid String primary\n\tc Clock\n}\n",
			want:   []string{"C  *Clock ", "type Clock struct{ time.Time }"},
			absent: []string{"type Timestamp1504 struct"},
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

// TestMarshal_LayoutNameReservedBeforeInlineEnum pins the shared namespace's
// order: a per-layout type takes its name before an inline enum that derives
// the same one, so the enum takes the suffix.
func TestMarshal_LayoutNameReservedBeforeInlineEnum(t *testing.T) {
	t.Parallel()

	s, res := schema.LoadString(context.Background(), `schema "order"

type TimestampMon {
	id  String primary
	jan Enum["a", "b"]
	at  Timestamp["Mon Jan"]
}
`, "order.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{
		"type TimestampMonJan struct{ time.Time }",
		"type TimestampMonJan2 string",
		"TimestampMonJan2 `json:\"jan,omitempty\"`",
		"TimestampMonJan  `json:\"at,omitempty\"`",
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// TestMarshal_TemporalOnlyInAnImport pins that registration walks the whole
// closure: the entry declares no temporal position, and the one import declares
// a List DataType over Date and a nested List property over a layout, so each
// is the only position that registers its type.
func TestMarshal_TemporalOnlyInAnImport(t *testing.T) {
	t.Parallel()

	got, err := gogen.Marshal(loadSchema(t, "imports/temporal_main"))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{
		"type Date struct{ time.Time }",
		"type Days []Date",
		"type Timestamp1504MST struct{ time.Time }",
		"On [][]Timestamp1504MST ",
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// TestMarshal_CollidingLayoutsAreNamedByTheirLayoutAlone pins the names of
// layouts that share a letters-and-digits base: each takes its layout-exact
// name, the same for any declaration order, and the bare base is emitted for
// neither. A layout alone takes the bare base, and no name one run gives a
// layout is given to a different layout by the other runs.
func TestMarshal_CollidingLayoutsAreNamedByTheirLayoutAlone(t *testing.T) {
	t.Parallel()

	gen := func(t *testing.T, layouts ...string) map[string]string {
		t.Helper()
		return layoutNames(t, "", layouts...)
	}

	const dashed, bare, underscored = "2006-01-02", "20060102", "2006_01_02"
	all := gen(t, dashed, bare, underscored)
	for name, want := range map[string]string{
		"Timestamp_2006_2D_01_2D_02": dashed,
		"Timestamp_20060102":         bare,
		"Timestamp_2006_5F_01_5F_02": underscored,
	} {
		if all[name] != want {
			t.Errorf("%s names layout %q, want %q (all: %v)", name, all[name], want, all)
		}
	}
	for _, alone := range []string{dashed, bare, underscored} {
		one := gen(t, alone)
		if one["Timestamp20060102"] != alone {
			t.Errorf("a lone %q is named %v, want Timestamp20060102", alone, one)
		}
		for name, layout := range all {
			if other, ok := one[name]; ok && other != layout {
				t.Errorf("%s names %q beside the other layouts and %q alone", name, layout, other)
			}
		}
	}
	for range 16 {
		if again := gen(t, underscored, dashed, bare); !maps.Equal(again, all) {
			t.Fatalf("names depend on declaration order: %v, then %v", all, again)
		}
	}
}

// TestMarshal_LayoutNameNeverPassesToAnotherLayout pins that a layout whose
// bare base anything else claims — another layout emission names, or a
// declared type — takes its layout-exact name, so declaring or adding
// something never hands a generated name from one layout to another. The
// exact name tells an invalid UTF-8 byte from U+FFFD. A
// temporal DataType is its own carrier and names no layout type, so its layout
// claims nothing.
func TestMarshal_LayoutNameNeverPassesToAnotherLayout(t *testing.T) {
	t.Parallel()

	const declared = "type Timestamp20060102 {\n\tid String primary\n}\n"
	before := layoutNames(t, declared, "2006-01-022")
	after := layoutNames(t, declared, "2006-01-022", "2006-01-02")
	for name, layout := range before {
		if other, ok := after[name]; ok && other != layout {
			t.Errorf("%s names %q, and %q once another layout is added", name, layout, other)
		}
	}
	if before["Timestamp200601022"] != "2006-01-022" || after["Timestamp200601022"] != "2006-01-022" {
		t.Errorf("a layout whose base nothing else claims is named %v, then %v; want Timestamp200601022 both times", before, after)
	}
	if got := after["Timestamp_2006_2D_01_2D_02"]; got != "2006-01-02" {
		t.Errorf("a layout whose base a declared type holds is named %v, want its exact name", after)
	}

	invalid := layoutNames(t, "", "a\xffb", "a\uFFFDb")
	if invalid["Timestamp_a_xFF_b"] != "a\xffb" || invalid["Timestamp_a_FFFD_b"] != "a\uFFFDb" {
		t.Errorf("an invalid byte and U+FFFD are not told apart: %v", invalid)
	}

	carrier := layoutNames(t, "type Wall = Timestamp[\"2006-01-02\"]\n", "20060102")
	if got := carrier["Timestamp20060102"]; got != "20060102" {
		t.Errorf("a DataType's layout claimed the bare base: %v", carrier)
	}
}

// layoutNames generates a schema holding decls and one Doc property per
// layout, and returns each generated layout type's name mapped to its layout.
func layoutNames(t *testing.T, decls string, layouts ...string) map[string]string {
	t.Helper()
	var b strings.Builder
	b.WriteString("schema \"bases\"\n\n" + decls + "\ntype Doc {\n\tid String primary\n")
	for i, l := range layouts {
		fmt.Fprintf(&b, "\tp%d Timestamp[%s]\n", i, strconv.Quote(l))
	}
	b.WriteString("}\n")
	s, res := schema.LoadString(context.Background(), b.String(), "bases.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	byName := map[string]string{}
	for _, m := range layoutDecl.FindAllStringSubmatch(string(got), -1) {
		layout, err := strconv.Unquote(m[2])
		if err != nil {
			t.Fatal(err)
		}
		byName[m[1]] = layout
	}
	return byName
}

// layoutDecl matches a generated layout type's doc comment and declaration.
var layoutDecl = regexp.MustCompile(`// (\w+) is exchanged as a JSON string in the layout ("[^"]*")\.\ntype \w+ struct\{ time\.Time \}`)

// TestMarshal_GraphKeysAreAddressableTags pins the Graph aggregate's json keys
// as the names adapter/json writes: bare for the entry schema's types and
// alias-qualified for a direct import. A type the entry schema reaches only
// through another import has no such name and no Graph field, though its
// struct is still emitted; and an entry type that shares its name with one
// keeps its bare key rather than its schema-qualified Go name.
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
			absent:  []string{"LeftbaseNode", "RightbaseNode"},
			structs: []string{"LeftbaseNode", "RightbaseNode"},
		},
		"imports/entry_shadow_main": {
			keys:    []string{"Node", "mid.Mid"},
			absent:  []string{"EmainNode", "EleafNode"},
			structs: []string{"EmainNode", "EleafNode"},
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

// TestMarshal_PerLayoutNameYieldsToSchemaType pins the precedence between a
// schema-declared name and a synthesized per-layout name: the schema keeps
// the bare identifier and the layout takes its layout-exact name.
func TestMarshal_PerLayoutNameYieldsToSchemaType(t *testing.T) {
	s, res := schema.LoadString(context.Background(),
		"schema \"clash\"\n\ntype Timestamp20060102150405 {\n\tid String primary\n\tat Timestamp[\"2006-01-02 15:04:05\"]\n}", "clash.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type Timestamp20060102150405 struct {\n",
		"type Timestamp_2006_2D_01_2D_02_20_15_3A_04_3A_05 struct{ time.Time }",
		"At *Timestamp_2006_2D_01_2D_02_20_15_3A_04_3A_05 ",
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
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
