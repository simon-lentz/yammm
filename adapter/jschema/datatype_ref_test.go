package jschema

import (
	"encoding/json"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// defFragment returns the compacted JSON of one member of a decoded
// document, found by walking keys from the root.
func defFragment(t *testing.T, doc map[string]any, path ...string) string {
	t.Helper()
	var cur any = doc
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("%v: %q is not under an object", path, k)
		}
		if cur, ok = m[k]; !ok {
			t.Fatalf("%v: no member %q", path, k)
		}
	}
	b, err := json.Marshal(cur)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMarshal_DataTypeIsARefAtEveryListDepth(t *testing.T) {
	src := `schema "p"

type FipsCode = String[5, 5]
type Codes = List<FipsCode>
type Strs = List<String>

type Doc {
	id String primary
	flat List<FipsCode>
	nested List<List<FipsCode>>[1, _]
	codes Codes
	nestedCodes List<List<Codes>>
	strs List<Strs>
}
`
	s := loadFixture(t, src, "test://datatype_ref.yammm")
	out, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeDoc(t, out)
	cases := []struct {
		path []string
		want string
	}{
		{[]string{"$defs", "Doc", "properties", "flat"}, `{"items":{"$ref":"#/$defs/FipsCode"},"type":"array"}`},
		{[]string{"$defs", "Doc", "properties", "nested"}, `{"items":{"items":{"$ref":"#/$defs/FipsCode"},"type":"array"},"minItems":1,"type":"array"}`},
		{[]string{"$defs", "Doc", "properties", "codes"}, `{"$ref":"#/$defs/Codes"}`},
		{[]string{"$defs", "Doc", "properties", "nestedCodes"}, `{"items":{"items":{"$ref":"#/$defs/Codes"},"type":"array"},"type":"array"}`},
		{[]string{"$defs", "Doc", "properties", "strs"}, `{"items":{"$ref":"#/$defs/Strs"},"type":"array"}`},
		{[]string{"$defs", "Codes"}, `{"items":{"$ref":"#/$defs/FipsCode"},"type":"array"}`},
		{[]string{"$defs", "Strs"}, `{"items":{"type":"string"},"type":"array"}`},
	}
	for _, tc := range cases {
		if got := defFragment(t, doc, tc.path...); got != tc.want {
			t.Errorf("%v = %s\nwant %s", tc.path, got, tc.want)
		}
	}

	compiled := compileEmitted(t, s)
	good := []byte(`{"Doc": [{"id": "d", "nested": [["12345"]], "codes": ["12345"], "nestedCodes": [[["12345"]]]}]}`)
	if err := validateEmitted(t, compiled, good); err != nil {
		t.Errorf("a valid file fails the emitted schema: %v", err)
	}
	for _, bad := range []string{
		`{"Doc": [{"id": "d", "nested": [["1234"]]}]}`,
		`{"Doc": [{"id": "d", "codes": ["1234"]}]}`,
		`{"Doc": [{"id": "d", "nestedCodes": [[["1234"]]]}]}`,
	} {
		if validateEmitted(t, compiled, []byte(bad)) == nil {
			t.Errorf("the emitted schema accepts %s", bad)
		}
		if len(yammmErrors(t, s, []byte(bad))) == 0 {
			t.Errorf("yammm accepts %s", bad)
		}
	}
}

// A Builder-built property carries no DataTypeRef; its datatype is named by
// the constraint alone.
func TestMarshal_BuilderDataTypePropertyIsARef(t *testing.T) {
	s, res := schema.NewBuilder().WithName("b").
		AddDataType("Code", schema.StringLenBetween(1, 3)).
		AddDataType("Codes", schema.NewListConstraint(schema.NewAliasConstraint("Code", nil))).
		AddType("Doc").WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("c", schema.NewAliasConstraint("Code", nil)).
		WithProperty("cs", schema.NewListConstraint(schema.NewAliasConstraint("Code", nil))).
		WithProperty("all", schema.NewAliasConstraint("Codes", nil)).Done().Build()
	if res.HasErrors() {
		t.Fatal(res.Err())
	}
	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	doc := decodeDoc(t, out)
	for path, want := range map[string]string{
		"c":   `{"$ref":"#/$defs/Code"}`,
		"cs":  `{"items":{"$ref":"#/$defs/Code"},"type":"array"}`,
		"all": `{"$ref":"#/$defs/Codes"}`,
	} {
		if got := defFragment(t, doc, "$defs", "Doc", "properties", path); got != want {
			t.Errorf("%s = %s, want %s", path, got, want)
		}
	}
	if got, want := defFragment(t, doc, "$defs", "Codes"), `{"items":{"$ref":"#/$defs/Code"},"type":"array"}`; got != want {
		t.Errorf("Codes = %s, want %s", got, want)
	}
	compileEmitted(t, s)
}

// A datatype from an imported schema is named "alias.Name" and resolves in
// the schema that declares the reference, at every position.
func TestMarshal_ImportedDataTypeIsARef(t *testing.T) {
	s := loadMulti(t, map[string]string{
		"main.yammm": `schema "geo"

import "common.yammm" as common

type Fipses = List<common.Fips>

type County {
	fips common.Fips primary
	all List<common.Fips>
	nested List<List<common.Fips>>
	named Fipses
}
`,
		"common.yammm": `schema "common"

type Fips = String[5, 5]
`,
	})
	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	doc := decodeDoc(t, out)
	cases := []struct {
		path []string
		want string
	}{
		{[]string{"$defs", "County", "properties", "fips"}, `{"$ref":"#/$defs/Fips"}`},
		{[]string{"$defs", "County", "properties", "all"}, `{"items":{"$ref":"#/$defs/Fips"},"type":"array"}`},
		{[]string{"$defs", "County", "properties", "nested"}, `{"items":{"items":{"$ref":"#/$defs/Fips"},"type":"array"},"type":"array"}`},
		{[]string{"$defs", "Fipses"}, `{"items":{"$ref":"#/$defs/Fips"},"type":"array"}`},
	}
	for _, tc := range cases {
		if got := defFragment(t, doc, tc.path...); got != tc.want {
			t.Errorf("%v = %s\nwant %s", tc.path, got, tc.want)
		}
	}
	compileEmitted(t, s)
}
