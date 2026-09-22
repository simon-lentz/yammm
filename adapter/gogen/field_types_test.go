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
// relation omits an unset field, since the parser refuses a null relation.
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

	deeperFirst := marshalString(t, "schema \"d\"\n\n/* Usage\n     car.Drive()\n   drives it. */\ntype Car {\n\tid String primary\n}\n")
	assertHolds(t, deeperFirst, []string{"// Usage\n//\n//\tcar.Drive()\n//\n// drives it.\ntype Car struct {"}, nil)
}

// TestMarshal_EnumConstNamesAreReserved pins that an enum value constant takes
// the shared namespace's suffix when its name is a type's, or a sibling
// value's, so the file declares no name twice.
func TestMarshal_EnumConstNamesAreReserved(t *testing.T) {
	t.Parallel()

	got := marshalString(t, `schema "e"

type Tier = Enum["gold", "silver"]

type TierGold {
	id String primary
	v  Enum["a-b", "a b"]
}
`)
	assertHolds(t, got, []string{
		"type TierGold struct",
		`TierGold2  Tier = "gold"`,
		`TierGoldVAB  TierGoldV = "a-b"`,
		`TierGoldVAB2 TierGoldV = "a b"`,
	}, nil)
}

// TestMarshal_FieldNamesTheTransformMergesAreSuffixed pins that two members
// the identifier transform maps to one Go name are separated in one struct,
// and that a later member whose own name is an earlier one's suffixed name
// takes the next suffix rather than colliding with it.
func TestMarshal_FieldNamesTheTransformMergesAreSuffixed(t *testing.T) {
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
		"Foo1     *string `json:\"foo_1,omitempty\"`",
		"Foo12    *string `json:\"foo1,omitempty\"`",
		"FooBar   *string `json:\"foo_bar,omitempty\"`",
		"FooBar2  *string `json:\"foo__bar,omitempty\"`",
		"FooBar22 *string `json:\"foo_bar2,omitempty\"`",
	}, nil)
}

// TestMarshal_EveryReservedNameIsQualifiedAway pins each identifier the
// generator emits or once emitted: a schema type of that name takes its
// schema-qualified Go name, never the reserved one.
func TestMarshal_EveryReservedNameIsQualifiedAway(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"Graph", "SerializedModel", "SerializedModelEntry", "SerializedSources", "SerializedEntry", "SchemaHash"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := marshalString(t, "schema \"geo\"\n\ntype "+name+" {\n\tid String primary\n}\n")
			assertHolds(t, got, []string{"type Geo" + name + " struct {\n\tID string"}, []string{"type " + name + " struct {\n\tID string"})
		})
	}
}

// TestMarshal_OneSchemaSharingACandidateGenerates pins that two entities of one
// schema mapping to one Go name generate, the second taking the suffix.
func TestMarshal_OneSchemaSharingACandidateGenerates(t *testing.T) {
	t.Parallel()

	got := marshalString(t, "schema \"geo\"\n\ntype Region = String\n\ntype Region {\n\tid String primary\n\tr Region\n}\n")
	assertHolds(t, got, []string{"type GeoRegion2 string", "type GeoRegion struct", "R  *GeoRegion2 "}, nil)
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

// TestMarshal_InlineEnumIsNamedForItsOwnersGoName pins that an inline enum's
// type takes its owner's Go name, which the initialisms shape, not the
// owner's schema name.
func TestMarshal_InlineEnumIsNamedForItsOwnersGoName(t *testing.T) {
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

// TestMarshal_ListEnumNamesAreReservedAfterTheLayouts pins that a List inline
// enum's name and a List DataType's Element name are reserved at emission,
// after the per-layout Timestamp types, so a layout whose base equals one of
// them keeps the base and the enum takes the suffix.
func TestMarshal_ListEnumNamesAreReservedAfterTheLayouts(t *testing.T) {
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
			"type Timestamp2006X struct{ time.Time }",
			"type Timestamp2006X2 string",
			"X  []Timestamp2006X2 `json:\"x,omitzero\"`",
			"T  *Timestamp2006X   `json:\"t,omitempty\"`",
		}, []string{"Timestamp_2006_20_X"})
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
			"type Timestamp2006Element struct{ time.Time }",
			"type Timestamp2006Element2 string",
			"type Timestamp2006 []Timestamp2006Element2",
			"T  *Timestamp2006Element `json:\"t,omitempty\"`",
		}, []string{"Timestamp_2006_20_Element"})
	})
}
