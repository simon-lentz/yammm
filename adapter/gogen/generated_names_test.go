package gogen_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"maps"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/adapter/gogen"
	"github.com/simon-lentz/yammm/adapter/jschema"
	"github.com/simon-lentz/yammm/schema"
)

// generateSources loads sources with entry "main.yammm" and generates them.
func generateSources(t *testing.T, sources map[string]string) (*schema.Schema, []byte) {
	t.Helper()
	files := make(map[string][]byte, len(sources))
	for k, v := range sources {
		files[k] = []byte(v)
	}
	s, res := schema.LoadSourcesWithEntry(context.Background(), files, "main.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return s, got
}

// topLevelDecls maps every type and const the generated file declares to its
// printed declaration, a struct's fields sorted, since they follow declaration
// order.
func topLevelDecls(t *testing.T, src []byte) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "gen.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	decls := map[string]string{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || (gd.Tok != token.TYPE && gd.Tok != token.CONST) {
			continue
		}
		for _, spec := range gd.Specs {
			var buf bytes.Buffer
			if err := printer.Fprint(&buf, fset, spec); err != nil {
				t.Fatal(err)
			}
			switch sp := spec.(type) {
			case *ast.TypeSpec:
				lines := strings.Split(buf.String(), "\n")
				if _, isStruct := sp.Type.(*ast.StructType); isStruct && len(lines) > 2 {
					slices.Sort(lines[1 : len(lines)-1])
				}
				decls[sp.Name.Name] = strings.Join(lines, "\n")
			case *ast.ValueSpec:
				for _, n := range sp.Names {
					decls[n.Name] = buf.String()
				}
			}
		}
	}
	return decls
}

// declSignatures maps every generated type and value constant to what it
// denotes, independent of any generated name: a struct by its sorted json
// keys, an enum type by its sorted values, a value constant by its enum's
// signature and its value, a layout type by its layout, any other type "type".
func declSignatures(t *testing.T, src []byte) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "gen.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	values := map[string][]string{} // enum type name -> its values
	consts := map[string][2]string{}
	layouts := map[string]string{} // unexported layout const -> layout
	sigs := map[string]string{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			switch sp := spec.(type) {
			case *ast.ValueSpec:
				if gd.Tok != token.CONST || len(sp.Values) != 1 {
					continue
				}
				lit, ok := sp.Values[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				v, _ := strconv.Unquote(lit.Value)
				if sp.Type == nil {
					layouts[sp.Names[0].Name] = v
					continue
				}
				typ := sp.Type.(*ast.Ident).Name
				values[typ] = append(values[typ], v)
				consts[sp.Names[0].Name] = [2]string{typ, v}
			case *ast.TypeSpec:
				st, ok := sp.Type.(*ast.StructType)
				if !ok {
					continue
				}
				var keys []string
				for _, fld := range st.Fields.List {
					if fld.Tag == nil {
						continue
					}
					tag, _ := strconv.Unquote(fld.Tag.Value)
					key, _, _ := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(tag, `json:"`), `"`), ",")
					keys = append(keys, key)
				}
				slices.Sort(keys)
				sigs[sp.Name.Name] = "struct " + strings.Join(keys, " ")
			}
		}
	}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			sp := spec.(*ast.TypeSpec)
			if _, isStruct := sigs[sp.Name.Name]; isStruct && sigs[sp.Name.Name] != "struct " {
				continue
			}
			name := sp.Name.Name
			if layout, ok := layouts[strings.ToLower(name[:1])+name[1:]+"Layout"]; ok {
				sigs[name] = "layout " + layout
				continue
			}
			if vs, ok := values[name]; ok {
				sorted := slices.Sorted(slices.Values(vs))
				sigs[name] = "enum " + strings.Join(sorted, " ")
				continue
			}
			sigs[name] = "type"
		}
	}
	for name, c := range consts {
		sigs[name] = "value " + c[1] + " of " + sigs[c[0]]
	}
	return sigs
}

// collisionSources gives every name family two claimants of one bare
// spelling, from a type and a data type to a layout and a type. Every struct
// whose fields carry json tags holds a key no other struct holds, so its json
// keys identify it.
func collisionSources() map[string]string {
	return map[string]string{
		"main.yammm": `schema "geo"

import "other.yammm" as other

type Region = String
type Tags = List<Enum["a-b", "a_b", "c"]>

type Region {
	region_id String primary
	kind Enum["big", "small"]
	r Region
}

type Url {
	url_id String primary
	--> LINK (one) Region {
		url_note String
		kind Enum["x", "y"]
	}
}

type URL {
	uri_id String primary
	--> LINK (one) Region {
		uri_note String
		kind Enum["p", "q"]
	}
}

type TagsElement {
	te_id String primary
	tags Tags
}

type RegionKind {
	rk_id String primary
}

type Graph {
	graph_id String primary
}

type Stamp {
	stamp_id String primary
	at Timestamp["2006-01-02 15:04"]
	--> TO (one) other.Region
}

type Timestamp200601021504 {
	ts_id String primary
}
`,
		"other.yammm": `schema "other"

type Region {
	other_region_id String primary
}
`,
	}
}

// TestMarshal_AContestedNameTakesEveryClaimantsExactSpelling pins the rule for
// every name family: an entity keeps its bare spelling only as its sole
// claimant, and otherwise takes its exact spelling, which no bare spelling and
// no other entity holds.
func TestMarshal_AContestedNameTakesEveryClaimantsExactSpelling(t *testing.T) {
	t.Parallel()

	_, got := generateSources(t, collisionSources())
	decls := topLevelDecls(t, got)
	for _, want := range []string{
		"Type_geo__Region", "DataType_geo__Region", "Type_other__Region",
		"Type_geo__Url", "Type_geo__URL",
		"Association_geo__Url__LINK", "Association_geo__URL__LINK",
		"AssociationEnum_geo__Url__LINK__kind", "AssociationEnum_geo__URL__LINK__kind",
		"Type_geo__Graph",
		"Enum_geo__Region__kind", "Type_geo__RegionKind",
		"Element_geo__Tags", "Type_geo__TagsElement",
		"Const_Element_geo__Tags__a_2D_b", "Const_Element_geo__Tags__a_5F_b",
		"Timestamp_2006_2D_01_2D_02_20_15_3A_04", "Type_geo__Timestamp200601021504",
		// Sole claimants keep their bare spellings.
		"Tags", "Stamp", "EDGE_Stamp_to_Region", "TagsElementC", "RegionKindBig",
		"EDGE_URL_link_RegionKindX",
		// The reserved aggregate keeps its name.
		"Graph",
	} {
		if _, ok := decls[want]; !ok {
			t.Errorf("no declaration %s among %v", want, slices.Sorted(maps.Keys(decls)))
		}
	}
	for _, absent := range []string{
		"Region", "GeoRegion", "GeoRegion2", "URL", "GeoURL", "GeoURL2", "GeoGraph",
		"RegionKind", "RegionKind2", "TagsElement", "TagsElement2", "TagsElementAB", "TagsElementAB2",
		"EDGE_URL_link_Region", "EDGE_URL_link_Region2", "Timestamp200601021504",
	} {
		if _, ok := decls[absent]; ok {
			t.Errorf("declaration %s holds a contested or suffixed spelling", absent)
		}
	}
}

// TestMarshal_NamesDependOnTheClaimsNotTheirOrder pins that reordering the
// declarations, which keeps the schema's hash, gives every entity the same
// name and every name the same declaration.
func TestMarshal_NamesDependOnTheClaimsNotTheirOrder(t *testing.T) {
	t.Parallel()

	base := collisionSources()
	s1, first := generateSources(t, base)
	blocks := strings.Split(strings.TrimSuffix(base["main.yammm"], "\n"), "\n\n")
	head, body := blocks[:2], blocks[2:]
	slices.Reverse(body)
	reordered := map[string]string{
		"main.yammm":  strings.Join(append(slices.Clone(head), body...), "\n\n") + "\n",
		"other.yammm": base["other.yammm"],
	}
	// The two data types swap as well.
	reordered["main.yammm"] = strings.Replace(reordered["main.yammm"],
		"type Region = String\ntype Tags = List<Enum[\"a-b\", \"a_b\", \"c\"]>",
		"type Tags = List<Enum[\"a-b\", \"a_b\", \"c\"]>\ntype Region = String", 1)
	s2, second := generateSources(t, reordered)
	if h1, h2 := schema.StructuralHash(s1), schema.StructuralHash(s2); h1 != h2 {
		t.Fatalf("the reorder moved the hash: %s, %s", h1, h2)
	}
	if bytes.Equal(first, second) {
		t.Fatal("the reorder changed nothing the file emits in order; the test reorders nothing")
	}
	if d1, d2 := topLevelDecls(t, first), topLevelDecls(t, second); !reflect.DeepEqual(d1, d2) {
		for name, decl := range d1 {
			if d2[name] != decl {
				t.Errorf("%s declares\n%s\nthen\n%s", name, decl, d2[name])
			}
		}
		for name := range d2 {
			if _, ok := d1[name]; !ok {
				t.Errorf("%s is declared only after the reorder", name)
			}
		}
	}
}

// TestMarshal_ADeclarationAddedNeverRebindsAName pins that for a schema S and
// one that adds declarations to it, every name both files declare denotes the
// same entity in both: an addition can move an entity to its exact spelling,
// never hand its name to another.
func TestMarshal_ADeclarationAddedNeverRebindsAName(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ before, added string }{
		"a type whose name a qualification once took": {
			before: "type Url {\n\turl_id String primary\n}\n\ntype URL {\n\turi_id String primary\n}\n",
			added:  "type GeoURL {\n\tgeo_url_id String primary\n}\n",
		},
		"a type that shares an inline enum's name": {
			before: "type Person {\n\tperson_id String primary\n\tstatus Enum[\"on\", \"off\"]\n}\n",
			added:  "type PersonStatus {\n\tps_id String primary\n}\n",
		},
		"a value whose name another value shares": {
			before: "type Level = Enum[\"a_b\", \"x\"]\n\ntype Doc {\n\tdoc_id String primary\n\tl Level\n}\n",
			added:  "type Other = Enum[\"q\", \"r\"]\n\ntype LevelAB {\n\tlab_id String primary\n}\n",
		},
		"a layout whose base a type claims": {
			before: "type Doc {\n\tdoc_id String primary\n\tat Timestamp[\"2006-01-02\"]\n}\n",
			added:  "type Timestamp20060102 {\n\tt_id String primary\n}\n",
		},
		"an association whose name another claims": {
			before: "type C {\n\tc_id String primary\n}\n\ntype Url {\n\turl_id String primary\n\t--> LINK (one) C\n}\n",
			added:  "type URL {\n\turi_id String primary\n\t--> LINK (one) C\n}\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			const head = "schema \"geo\"\n\n"
			_, before := generateSources(t, map[string]string{"main.yammm": head + tc.before})
			_, after := generateSources(t, map[string]string{"main.yammm": head + tc.before + "\n" + tc.added})
			sb, sa := declSignatures(t, before), declSignatures(t, after)
			delete(sb, "Graph") // the aggregate, whose fields every added type moves
			for n, sig := range sb {
				if other, ok := sa[n]; ok && other != sig {
					t.Errorf("%s denotes %q, and %q once the declaration is added", n, sig, other)
				}
			}
		})
	}
}

// TestMarshal_ContestedFieldsTakeExactSpellings pins the rule inside a struct:
// two fields whose bare Go spellings meet both take "Field_" and their wire
// key, whichever is declared first, and an edge key field is no exception.
func TestMarshal_ContestedFieldsTakeExactSpellings(t *testing.T) {
	t.Parallel()

	got := strings.Join(strings.Fields(marshalString(t, `schema "f"

type Place {
	id String primary
}

type Doc {
	doc_id String primary
	foo_1 String
	foo1 String
	--> AT (one) Place {
		target_id String
		note String
	}
}
`)), " ")
	assertHolds(t, got, []string{
		"Field_foo_1 *string `json:\"foo_1,omitempty\"`",
		"Field_foo1 *string `json:\"foo1,omitempty\"`",
		"Field__target_id string `json:\"_target_id\"`",
		"Field_target_id *string `json:\"target_id,omitempty\"`",
		"Note *string `json:\"note,omitempty\"`",
	}, []string{"Foo12", "TargetID2"})
}

// TestMarshal_ADerivedPackageNameIsNeverPredeclared pins that a package name
// derived from the schema name never shadows a predeclared identifier, which
// an importing file could then no longer use unaliased.
func TestMarshal_ADerivedPackageNameIsNeverPredeclared(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{"nil": "nil_", "String": "string_", "Error": "error_", "any": "any_", "len": "len_", "geo": "geo"} {
		got := marshalString(t, "schema \""+name+"\"\n\ntype T {\n\tid String primary\n}\n")
		if !strings.Contains(got, "\npackage "+want+"\n") {
			t.Errorf("schema %q generates a package clause other than %q:\n%s", name, want, got)
		}
	}
}

// TestMarshal_AnEmptyPackageNameIsRefused pins that WithPackageName("") is an
// explicit name that is not an identifier, refused as any other is, and that
// CheckPackageName refuses it as Marshal does.
func TestMarshal_AnEmptyPackageNameIsRefused(t *testing.T) {
	t.Parallel()

	s, res := schema.LoadString(context.Background(), "schema \"stamps\"\n\ntype T {\n\tid String primary\n}\n", "p.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	if _, err := gogen.Marshal(s, gogen.WithPackageName("")); !errors.Is(err, gogen.ErrInvalidPackageName) {
		t.Errorf("WithPackageName(\"\") = %v, want ErrInvalidPackageName", err)
	}
	if err := gogen.CheckPackageName(""); !errors.Is(err, gogen.ErrInvalidPackageName) {
		t.Errorf("CheckPackageName(\"\") = %v, want ErrInvalidPackageName", err)
	}
}

// goStructs maps every struct whose first field carries a json tag to its
// fields, each wire key to such a struct its type names, or "" for any other.
func goStructs(t *testing.T, src []byte) map[string]map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "gen.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	structs := map[string]*ast.StructType{}
	ast.Inspect(f, func(n ast.Node) bool {
		if sp, ok := n.(*ast.TypeSpec); ok {
			if st, ok := sp.Type.(*ast.StructType); ok && len(st.Fields.List) > 0 && st.Fields.List[0].Tag != nil {
				structs[sp.Name.Name] = st
			}
		}
		return true
	})
	out := map[string]map[string]string{}
	for name, st := range structs {
		fields := map[string]string{}
		for _, fld := range st.Fields.List {
			tag, _ := strconv.Unquote(fld.Tag.Value)
			key, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
			typ := fld.Type
			for {
				switch x := typ.(type) {
				case *ast.StarExpr:
					typ = x.X
					continue
				case *ast.ArrayType:
					typ = x.Elt
					continue
				}
				break
			}
			if id, ok := typ.(*ast.Ident); ok && structs[id.Name] != nil {
				fields[key] = id.Name
			} else {
				fields[key] = ""
			}
		}
		out[name] = fields
	}
	return out
}

// defRef returns the $defs key a JSON Schema fragment references, directly or
// as its array items.
func defRef(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var frag struct {
		Ref   string `json:"$ref"`
		Items struct {
			Ref string `json:"$ref"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &frag); err != nil {
		t.Fatal(err)
	}
	ref := frag.Ref
	if ref == "" {
		ref = frag.Items.Ref
	}
	pointer, err := url.PathUnescape(strings.TrimPrefix(ref, "#/$defs/"))
	if err != nil || !strings.HasPrefix(ref, "#/$defs/") {
		return ""
	}
	return strings.NewReplacer("~1", "/", "~0", "~").Replace(pointer)
}

// TestMarshal_EveryReferenceBindsTheEntityJSchemaBinds checks gogen's names
// against a second implementation of the same binding, adapter/jschema's
// $defs keys: from each Graph field, every struct a field's type names holds
// the wire keys of the def jschema's matching property references, at every
// depth.
func TestMarshal_EveryReferenceBindsTheEntityJSchemaBinds(t *testing.T) {
	t.Parallel()

	s, src := generateSources(t, collisionSources())
	doc, err := jschema.Marshal(s)
	if err != nil {
		t.Fatalf("jschema.Marshal: %v", err)
	}
	var js struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Defs       map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(doc, &js); err != nil {
		t.Fatal(err)
	}
	structs := goStructs(t, src)
	type binding struct{ goName, defKey, path string }
	var queue []binding
	for key, goName := range structs["Graph"] {
		queue = append(queue, binding{goName, defRef(t, js.Properties[key]), key})
	}
	seen := map[string]bool{}
	for len(queue) > 0 {
		b := queue[0]
		queue = queue[1:]
		if seen[b.goName] {
			continue
		}
		seen[b.goName] = true
		var def struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(js.Defs[b.defKey], &def); err != nil || def.Properties == nil {
			t.Errorf("%s: jschema binds %q, which is no object def", b.path, b.defKey)
			continue
		}
		fields := structs[b.goName]
		if got, want := slices.Sorted(maps.Keys(fields)), slices.Sorted(maps.Keys(def.Properties)); !slices.Equal(got, want) {
			t.Errorf("%s: gogen's %s holds %v, jschema's %s holds %v", b.path, b.goName, got, b.defKey, want)
			continue
		}
		for key, target := range fields {
			if target != "" {
				queue = append(queue, binding{target, defRef(t, def.Properties[key]), b.path + "." + key})
			}
		}
	}
	if len(seen) != len(structs)-1 {
		t.Errorf("the walk reached %d of the %d structs beside Graph", len(seen), len(structs)-1)
	}
}
