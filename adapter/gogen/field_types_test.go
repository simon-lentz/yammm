package gogen_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/adapter/gogen"
	"github.com/simon-lentz/yammm/schema"
)

// marshalString loads src as one schema and generates it.
func marshalString(t *testing.T, src string) string {
	t.Helper()
	s, res := schema.LoadString(context.Background(), src, "gen.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(got)
}

// assertHolds fails for every want the output lacks and every absent it holds.
func assertHolds(t *testing.T, got string, want, absent []string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("output missing %q:\n%s", w, got)
		}
	}
	for _, a := range absent {
		if strings.Contains(got, a) {
			t.Errorf("output holds %q:\n%s", a, got)
		}
	}
}

// TestMarshal_ListOfADataTypeKeepsItsNameAtEveryDepth pins that a List whose
// innermost element names a DataType renders that DataType's Go name however
// deep the List nests, in a property and in a DataType's own constraint, and
// in a List of a List DataType.
func TestMarshal_ListOfADataTypeKeepsItsNameAtEveryDepth(t *testing.T) {
	t.Parallel()

	got := marshalString(t, `schema "n"

type Code = String[1, 8]
type Codes = List<Code>
type Grid = List<List<Code>>

type Doc {
	id     String primary
	flat   List<Code>
	nested List<List<Code>>
	deep   List<List<List<Code>>>
	cs     Codes
	rows   List<Codes>
}
`)
	assertHolds(t, got, []string{
		"type Codes []Code\n",
		"type Grid [][]Code\n",
		"Flat   []Code ",
		"Nested [][]Code ",
		"Deep   [][][]Code ",
		"Cs     Codes ",
		"Rows   []Codes ",
	}, []string{"[]string"})
}

// TestMarshal_ListOfAnImportedDataTypeKeepsItsName pins that a List DataType
// whose element names a data type through an import alias resolves it in the
// schema that declares the List.
func TestMarshal_ListOfAnImportedDataTypeKeepsItsName(t *testing.T) {
	t.Parallel()

	s, res := schema.LoadSourcesWithEntry(context.Background(), map[string][]byte{
		"main.yammm": []byte("schema \"main\"\n\nimport \"dep.yammm\" as dep\n\ntype Codes = List<List<dep.Code>>\n\ntype Doc {\n\tid String primary\n\tcs Codes\n}\n"),
		"dep.yammm":  []byte("schema \"dep\"\n\ntype Code = String[1, 8]\n"),
	}, "main.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	assertHolds(t, string(got), []string{"type Codes [][]Code\n", "type Code string\n"}, nil)
}

// TestMarshal_EdgePropertyInlineEnumIsNamed pins that an enum declared inline
// on an edge property takes its own named type and value constants, as the
// same constraint on a type property does, named for the EDGE_ struct.
func TestMarshal_EdgePropertyInlineEnumIsNamed(t *testing.T) {
	t.Parallel()

	got := marshalString(t, `schema "p"

type Company {
	id String primary
}

type Person {
	id   String primary
	rank Enum["junior", "senior"]
	--> WORKS_AT (one) Company {
		role  Enum["ic", "manager"] required
		level Enum["l1", "l2"]
	}
}
`)
	assertHolds(t, got, []string{
		"type PersonRank string",
		"type EDGE_Person_works_at_CompanyRole string",
		`EDGE_Person_works_at_CompanyRoleIc      EDGE_Person_works_at_CompanyRole = "ic"`,
		`EDGE_Person_works_at_CompanyRoleManager EDGE_Person_works_at_CompanyRole = "manager"`,
		"type EDGE_Person_works_at_CompanyLevel string",
		"Role     EDGE_Person_works_at_CompanyRole ",
		"Level    *EDGE_Person_works_at_CompanyLevel ",
	}, []string{"Role     string"})
}

// TestMarshal_JSONTagsOmitOnlyWhatIsAbsent pins each field's json option: an
// optional pointer omits nil, an optional slice omits only nil so a present
// empty list survives a re-encode, a required property carries none, and every
// relation omits an unset field, since instance validation refuses a null
// relation.
func TestMarshal_JSONTagsOmitOnlyWhatIsAbsent(t *testing.T) {
	t.Parallel()

	got := marshalString(t, `schema "tags"

part type Part {
	name String primary
}

type Doc {
	id    String primary
	note  String
	req   List<String> required
	tags  List<String>
	vec   Vector[2]
	*-> PARTS (one) Part
	*-> MORE (many) Part
	--> LINK (one) Doc
	--> MAYBE Doc
	--> MANY (many) Doc
}
`)
	assertHolds(t, got, []string{
		"`json:\"id\"`",
		"`json:\"note,omitempty\"`",
		"`json:\"req\"`",
		"`json:\"tags,omitzero\"`",
		"`json:\"vec,omitzero\"`",
		"Parts []*Part              `json:\"parts,omitempty\"`",
		"More  []*Part              `json:\"more,omitempty\"`",
		"Link  *EDGE_Doc_link_Doc   `json:\"link,omitempty\"`",
		"Maybe *EDGE_Doc_maybe_Doc  `json:\"maybe,omitempty\"`",
		"Many  []*EDGE_Doc_many_Doc `json:\"many,omitempty\"`",
		"TargetID string `json:\"_target_id\"`",
	}, nil)
}

// TestMarshal_DocCommentContinuationLinesAreDedented pins that the indentation
// a block comment's continuation lines share is removed, so the doc-comment
// formatter reads them as prose, and that a line indented deeper keeps the
// difference.
func TestMarshal_DocCommentContinuationLinesAreDedented(t *testing.T) {
	t.Parallel()

	got := marshalString(t, "schema \"d\"\n\n/* Vehicle management schema\n   Defines cars, dealers, and their relationships */\ntype Car {\n\t/* Line one\n\t   line two\n\n\t   line four */\n\tid String primary\n}\n")
	assertHolds(t, got, []string{
		"// Vehicle management schema\n// Defines cars, dealers, and their relationships\ntype Car struct {",
		"\t// Line one\n\t// line two\n\t//\n\t// line four\n\tID string",
	}, []string{"//\tDefines"})

	nested := marshalString(t, "schema \"d\"\n\n/* Usage\n   Call it as\n     car.Drive()\n   and stop. */\ntype Car {\n\tid String primary\n}\n")
	assertHolds(t, nested, []string{"// Usage\n// Call it as\n//\n//\tcar.Drive()\n//\n// and stop.\ntype Car struct {"}, nil)

	crOnly := marshalString(t, "schema \"d\"\n\n/* Usage\r   Call it as\r   more */\ntype Car {\n\tid String primary\n}\n")
	assertHolds(t, crOnly, []string{"// Usage\n// Call it as\n// more\ntype Car struct {"}, nil)

	deeper := marshalString(t, "schema \"d\"\n\n/* Usage\n   Call it as\n      car.Drive() */\ntype Car {\n\tid String primary\n}\n")
	assertHolds(t, deeper, []string{"// Usage\n// Call it as\n//\n//\tcar.Drive()\ntype Car struct {"}, nil)

	deeperFirst := marshalString(t, "schema \"d\"\n\n/* Usage\n     car.Drive()\n   drives it. */\ntype Car {\n\tid String primary\n}\n")
	assertHolds(t, deeperFirst, []string{"// Usage\n//\n//\tcar.Drive()\n//\n// drives it.\ntype Car struct {"}, nil)
}

// TestMarshal_AContestedEnumConstTakesItsExactSpelling pins that an enum value
// constant whose bare spelling a type's, or a sibling value's, also claims
// takes its exact spelling, as every other claimant does.
func TestMarshal_AContestedEnumConstTakesItsExactSpelling(t *testing.T) {
	t.Parallel()

	got := marshalString(t, `schema "e"

type Tier = Enum["gold", "silver"]

type TierGold {
	id String primary
	v  Enum["a-b", "a b"]
}
`)
	assertHolds(t, got, []string{
		"type Type_e__TierGold struct",
		`Const_DataType_e__Tier__gold Tier = "gold"`,
		`TierSilver                   Tier = "silver"`,
		`Const_Enum_e__TierGold__v__a_2D_b TierGoldV = "a-b"`,
		`Const_Enum_e__TierGold__v__a_20_b TierGoldV = "a b"`,
	}, []string{"TierGold2", "TierGoldVAB"})
}

// TestMarshal_FieldNamesTheTransformMergesTakeTheirWireKeys pins that members
// the identifier transform maps to one Go name each take "Field_" and their
// wire key, and that no member takes a name another member's spelling
// suggests: foo_bar2 keeps its bare FooBar2 beside foo_bar and foo__bar.
func TestMarshal_FieldNamesTheTransformMergesTakeTheirWireKeys(t *testing.T) {
	t.Parallel()

	got := marshalString(t, `schema "f"

type Doc {
	id       String primary
	foo_1    String
	foo1     String
	foo_bar  String
	foo__bar String
	foo_bar2 String
}
`)
	assertHolds(t, got, []string{
		"Field_foo_1    *string `json:\"foo_1,omitempty\"`",
		"Field_foo1     *string `json:\"foo1,omitempty\"`",
		"Field_foo_bar  *string `json:\"foo_bar,omitempty\"`",
		"Field_foo__bar *string `json:\"foo__bar,omitempty\"`",
		"FooBar2        *string `json:\"foo_bar2,omitempty\"`",
	}, []string{"Foo12", "FooBar22"})
}

// TestMarshal_EveryReservedNameIsAClaimant pins each identifier the generator
// emits or once emitted but Date, a DSL keyword: a schema type of that name
// takes its exact spelling, never the reserved one.
func TestMarshal_EveryReservedNameIsAClaimant(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"Graph", "SerializedModel", "SerializedModelEntry", "SerializedSources", "SerializedEntry", "SchemaHash"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := marshalString(t, "schema \"geo\"\n\ntype "+name+" {\n\tid String primary\n}\n")
			assertHolds(t, got, []string{"type Type_geo__" + name + " struct {\n\tID string"}, []string{"type " + name + " struct {\n\tID string"})
		})
	}
}

// TestMarshal_OneSchemaSharingACandidateGenerates pins that two entities of one
// schema mapping to one Go name generate, each under its exact spelling.
func TestMarshal_OneSchemaSharingACandidateGenerates(t *testing.T) {
	t.Parallel()

	got := marshalString(t, "schema \"geo\"\n\ntype Region = String\n\ntype Region {\n\tid String primary\n\tr Region\n}\n")
	assertHolds(t, got, []string{"type DataType_geo__Region string", "type Type_geo__Region struct", "R  *DataType_geo__Region "}, nil)
}

// TestMarshal_WithInitialismsIsScopedToItsCall pins that an injected acronym
// changes only the call it is passed to: a later call without it, and calls
// running concurrently, derive names from the default set alone.
func TestMarshal_WithInitialismsIsScopedToItsCall(t *testing.T) {
	s := loadSchema(t, "initialisms")
	if got, err := gogen.Marshal(s, gogen.WithInitialisms("JWT")); err != nil || !bytes.Contains(got, []byte("JWTToken string")) {
		t.Fatalf("WithInitialisms(JWT) = %v, want JWTToken", err)
	}
	if got, err := gogen.Marshal(s); err != nil || !bytes.Contains(got, []byte("JwtToken string")) {
		t.Errorf("a later Marshal without the option = %v, want JwtToken:\n%s", err, got)
	}

	done := make(chan error)
	for _, extra := range []string{"JWT", "TOKEN"} {
		go func() {
			_, err := gogen.Marshal(s, gogen.WithInitialisms(extra))
			done <- err
		}()
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
	if got, err := gogen.Marshal(s); err != nil || !bytes.Contains(got, []byte("JwtToken string")) {
		t.Errorf("Marshal after concurrent WithInitialisms calls = %v, want JwtToken:\n%s", err, got)
	}
}

// TestMarshal_ImportedListDataTypeResolvesInItsOwnSchema pins that a List
// DataType's element is resolved in the schema declaring the List, not in the
// entry: the imported Codes names its own schema's Code.
func TestMarshal_ImportedListDataTypeResolvesInItsOwnSchema(t *testing.T) {
	t.Parallel()

	s, res := schema.LoadSourcesWithEntry(context.Background(), map[string][]byte{
		"main.yammm": []byte("schema \"main\"\n\nimport \"dep.yammm\" as dep\n\ntype Doc {\n\tid String primary\n\tcs dep.Codes\n}\n"),
		"dep.yammm":  []byte("schema \"dep\"\n\ntype Code = String[1, 8]\ntype Codes = List<List<Code>>\n"),
	}, "main.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	assertHolds(t, string(got), []string{"type Codes [][]Code\n"}, nil)
}

// TestMarshal_EdgeInlineEnumsAreNamedPerAssociation pins that two associations
// of one owner to one target, each with an inline enum edge property of one
// name, take one named type each.
func TestMarshal_EdgeInlineEnumsAreNamedPerAssociation(t *testing.T) {
	t.Parallel()

	got := marshalString(t, `schema "p"

type Company {
	id String primary
}

type Person {
	id String primary
	--> WORKS_AT (one) Company {
		role Enum["ic", "manager"]
	}
	--> ADVISES (one) Company {
		role Enum["lead", "member"]
	}
}
`)
	assertHolds(t, got, []string{
		"type EDGE_Person_works_at_CompanyRole string",
		`EDGE_Person_works_at_CompanyRoleIc      EDGE_Person_works_at_CompanyRole = "ic"`,
		"type EDGE_Person_advises_CompanyRole string",
		`EDGE_Person_advises_CompanyRoleLead   EDGE_Person_advises_CompanyRole = "lead"`,
	}, nil)
}

// TestMarshal_AnInheritedInlineEnumIsNamedPerOwner pins that an inline enum
// a type inherits is its own named type, as the declaring type's is, with its
// own constants.
func TestMarshal_AnInheritedInlineEnumIsNamedPerOwner(t *testing.T) {
	t.Parallel()

	got := marshalString(t, `schema "h"

abstract type Base {
	kind Enum["a", "b"]
}

type Child extends Base {
	id String primary
}
`)
	assertHolds(t, got, []string{
		"type BaseKind string",
		`BaseKindA BaseKind = "a"`,
		"type ChildKind string",
		`ChildKindA ChildKind = "a"`,
		"Kind *ChildKind ",
	}, nil)
}

// TestMarshal_InlineEnumIsNamedForItsOwnersBareSpelling pins that an inline
// enum's bare spelling starts with its owner's, which the initialisms shape,
// not with the owner's name as written.
func TestMarshal_InlineEnumIsNamedForItsOwnersBareSpelling(t *testing.T) {
	t.Parallel()

	got := marshalString(t, "schema \"k\"\n\ntype Api_key {\n\tid String primary\n\tkind Enum[\"a\", \"b\"]\n}\n")
	assertHolds(t, got, []string{"type APIKeyKind string", "Kind *APIKeyKind "}, []string{"Api_keyKind"})
}

// TestMarshal_ListInlineEnumIsNamedAtEveryDepth pins that an enum declared
// inline inside a List takes the <Owner><Field> type a scalar inline enum
// takes, at every depth, and that a List DataType's inline element is
// <DataType>Element; a List of a named Enum keeps the DataType's name.
func TestMarshal_ListInlineEnumIsNamedAtEveryDepth(t *testing.T) {
	t.Parallel()

	got := marshalString(t, `schema "p"

type Tags = List<Enum["red", "green"]>
type Tone = Enum["warm", "cool"]

type Post {
	id     String primary
	labels List<Enum["draft", "final"]>
	matrix List<List<Enum["yes", "no"]>>
	tones  List<Tone>
	tags   Tags
}
`)
	assertHolds(t, got, []string{
		"type TagsElement string",
		`TagsElementRed   TagsElement = "red"`,
		"type Tags []TagsElement",
		"type PostLabels string",
		`PostLabelsDraft PostLabels = "draft"`,
		"type PostMatrix string",
		"Labels []PostLabels   `json:\"labels,omitzero\"`",
		"Matrix [][]PostMatrix `json:\"matrix,omitzero\"`",
		"Tones  []Tone         `json:\"tones,omitzero\"`",
		"Tags   Tags           `json:\"tags,omitzero\"`",
	}, []string{"[]string", "[][]string"})
}

// TestMarshal_ListEnumNamesAreClaimantsBesideTheLayouts pins that a List inline
// enum's name and a List DataType's Element name claim their bare spellings as
// a per-layout type does, so a layout whose base equals one of them and the
// enum both take their exact spellings.
func TestMarshal_ListEnumNamesAreClaimantsBesideTheLayouts(t *testing.T) {
	t.Parallel()

	t.Run("a List inline enum on a property", func(t *testing.T) {
		t.Parallel()
		got := marshalString(t, `schema "p"

type Timestamp2006 {
	id String primary
	x  List<Enum["a", "b"]>
	t  Timestamp["2006 X"]
}
`)
		assertHolds(t, got, []string{
			"type Timestamp_2006_20_X struct{ time.Time }",
			"type Enum_p__Timestamp2006__x string",
			"X  []Enum_p__Timestamp2006__x `json:\"x,omitzero\"`",
			"T  *Timestamp_2006_20_X       `json:\"t,omitempty\"`",
		}, []string{"Timestamp2006X "})
	})

	t.Run("a List DataType's inline element", func(t *testing.T) {
		t.Parallel()
		got := marshalString(t, `schema "p"

type Timestamp2006 = List<Enum["a", "b"]>

type Row {
	id String primary
	v  Timestamp2006
	t  Timestamp["2006 Element"]
}
`)
		assertHolds(t, got, []string{
			"type Timestamp_2006_20_Element struct{ time.Time }",
			"type Element_p__Timestamp2006 string",
			"type Timestamp2006 []Element_p__Timestamp2006",
			"T  *Timestamp_2006_20_Element `json:\"t,omitempty\"`",
		}, []string{"Timestamp2006Element "})
	})
}
