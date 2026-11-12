package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// duplicate is one E_DUPLICATE_TYPE issue: where it stands, its message, and
// where its related span places the first declaration, each as line:column.
type duplicate struct {
	at      string
	message string
	first   string
}

// duplicatesOf returns res's E_DUPLICATE_TYPE issues and fails for an issue of
// another code.
func duplicatesOf(t *testing.T, res diag.Result) []duplicate {
	t.Helper()
	var got []duplicate
	for issue := range res.Issues() {
		if issue.Code() != diag.E_DUPLICATE_TYPE {
			t.Errorf("unexpected issue: %v", issue)
			continue
		}
		related := issue.Related()
		if len(related) != 1 || related[0].Message != "first defined here" {
			t.Errorf("issue at %s: related = %v, want one span, \"first defined here\"", issue.Span().Start, related)
			continue
		}
		got = append(got, duplicate{issue.Span().Start.String(), issue.Message(), related[0].Span.Start.String()})
	}
	return got
}

func wantDuplicates(t *testing.T, source string, want []duplicate) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), source, "s.yammm")
	if !s.IsZero() {
		t.Errorf("the schema loads; want it refused")
	}
	got := duplicatesOf(t, res)
	if len(got) != len(want) {
		t.Fatalf("got %d E_DUPLICATE_TYPE issues, want %d\n%v", len(got), len(want), res)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("issue %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

const (
	typeAfterDatatype = `type "A" is defined multiple times; the name is first defined as a datatype`
	datatypeAfterType = `datatype "A" is defined multiple times; the name is first defined as a type`
)

func TestLoad_ATypeAndADatatypeShareOneNamespace(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		source string
		want   []duplicate
	}{
		"the type stands first": {
			source: `schema "s"
type A {
    id String primary
}
type A = String
`,
			want: []duplicate{{"5:1", datatypeAfterType, "2:1"}},
		},
		"the datatype stands first": {
			source: `schema "s"
type A = String
type A {
    id String primary
}
`,
			want: []duplicate{{"3:1", typeAfterDatatype, "2:1"}},
		},
		"every later declaration names the first": {
			source: `schema "s"
type A = String
type A {
    id String primary
}
type A = Integer
type A {
    id UUID primary
}
`,
			want: []duplicate{
				{"3:1", typeAfterDatatype, "2:1"},
				{"6:1", `datatype "A" is defined multiple times`, "2:1"},
				{"7:1", typeAfterDatatype, "2:1"},
			},
		},
		"a reference to the first declaration draws nothing": {
			source: `schema "s"
type A = String[1, 5]
type A {
    id String primary
}
type T {
    id String primary
    code A
}
`,
			want: []duplicate{{"3:1", typeAfterDatatype, "2:1"}},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			wantDuplicates(t, tc.source, tc.want)
		})
	}
}

// The second datatype is not the one a reference reads: an Integer at the key
// would be refused.
func TestLoad_ADuplicateOfOneKindNamesItsFirstDeclaration(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		source string
		want   []duplicate
	}{
		"two types": {
			source: `schema "s"
type A {
    id String primary
}
type A {
    id UUID primary
}
`,
			want: []duplicate{{"5:1", `type "A" is defined multiple times`, "2:1"}},
		},
		"two datatypes": {
			source: `schema "s"
type M = String
type M = Integer
type T {
    id M primary
}
`,
			want: []duplicate{{"3:1", `datatype "M" is defined multiple times`, "2:1"}},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			wantDuplicates(t, tc.source, tc.want)
		})
	}
}

// A reference that reads a refused declaration reports nothing more: the
// duplicate is the one break.
func TestLoad_AReferenceToARefusedDeclarationDrawsOnlyTheDuplicate(t *testing.T) {
	t.Parallel()

	const refusedType = `schema "s"
type A = String
type A {
    id String primary
}
`
	const refusedDatatype = `schema "s"
type A {
    id String primary
}
type A = List<String>
`
	tests := map[string]struct {
		source string
		want   []duplicate
	}{
		"an association target": {
			source: refusedType + "type T {\n    id String primary\n    --> LINK (one) A\n}\n",
			want:   []duplicate{{"3:1", typeAfterDatatype, "2:1"}},
		},
		"a composition target": {
			source: refusedType + "type T {\n    id String primary\n    *-> PARTS (many) A\n}\n",
			want:   []duplicate{{"3:1", typeAfterDatatype, "2:1"}},
		},
		"an extends clause": {
			source: refusedType + "type T extends A {\n    name String\n}\n",
			want:   []duplicate{{"3:1", typeAfterDatatype, "2:1"}},
		},
		"a property type": {
			source: refusedDatatype + "type T {\n    id String primary\n    code A\n}\n",
			want:   []duplicate{{"5:1", datatypeAfterType, "2:1"}},
		},
		"a primary key's type": {
			source: refusedDatatype + "type T {\n    id A primary\n}\n",
			want:   []duplicate{{"5:1", datatypeAfterType, "2:1"}},
		},
		"an edge property type": {
			source: refusedDatatype + "type T {\n    id String primary\n    --> LINK (one) T {\n        w A\n    }\n}\n",
			want:   []duplicate{{"5:1", datatypeAfterType, "2:1"}},
		},
		"a list element": {
			source: refusedDatatype + "type Codes = List<A>\n",
			want:   []duplicate{{"5:1", datatypeAfterType, "2:1"}},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			wantDuplicates(t, tc.source, tc.want)
		})
	}
}

// A name that no declaration of the wanted kind holds is unknown where that
// kind is read: a name of the other kind alone, a name declared twice as the
// other kind, and a qualified name, which a local declaration never answers.
func TestLoad_ANameOfTheOtherKindIsUnknown(t *testing.T) {
	t.Parallel()

	const (
		pair    = "type A = String\ntype A {\n    id String primary\n}\n"
		libOnly = "import \"./lib\" as lib\n"
	)
	unknown := map[diag.Code]int{diag.E_UNKNOWN_TYPE: 1}
	afterDuplicate := map[diag.Code]int{diag.E_DUPLICATE_TYPE: 1, diag.E_UNKNOWN_TYPE: 1}
	tests := map[string]struct {
		body string
		want map[diag.Code]int
	}{
		"a datatype as an association target": {
			body: "type A = String\ntype T {\n    id String primary\n    --> LINK (one) A\n}\n",
			want: unknown,
		},
		"a datatype in an extends clause": {
			body: "type A = String\ntype T extends A {\n    id String primary\n}\n",
			want: unknown,
		},
		"a type as a property type": {
			body: "type A {\n    id String primary\n}\ntype T {\n    id String primary\n    code A\n}\n",
			want: unknown,
		},
		"a type as a list element": {
			body: "type A {\n    id String primary\n}\ntype Codes = List<A>\n",
			want: unknown,
		},
		"a datatype declared twice, as an association target": {
			body: "type M = String\ntype M = Integer\ntype T {\n    id String primary\n    --> LINK (one) M\n}\n",
			want: afterDuplicate,
		},
		"a datatype declared twice, in an extends clause": {
			body: "type M = String\ntype M = Integer\ntype T extends M {\n    id String primary\n}\n",
			want: afterDuplicate,
		},
		"a type declared twice, as a property type": {
			body: "type P {\n    id String primary\n}\ntype P {\n    id String primary\n}\ntype T {\n    id String primary\n    p P\n}\n",
			want: afterDuplicate,
		},
		"a qualified association target beside a refused local type": {
			body: libOnly + pair + "type T {\n    id String primary\n    --> LINK (one) lib.A\n}\n",
			want: afterDuplicate,
		},
		"a qualified supertype beside a refused local type": {
			body: libOnly + pair + "type T extends lib.A {\n    id String primary\n}\n",
			want: afterDuplicate,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res := loadSourcesErr(t, map[string][]byte{
				"main.yammm": []byte("schema \"main\"\n" + tc.body),
				"lib.yammm":  []byte("schema \"lib\"\ntype Other {\n    id String primary\n}\n"),
			})
			wantCounts(t, res, tc.want)
		})
	}
}

// An imported name is another schema's: a qualified reference names it, so it
// never meets a local declaration.
func TestLoad_AnImportedNameIsOutsideTheLocalNamespace(t *testing.T) {
	t.Parallel()

	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./lib" as lib
type Code {
    id String primary
    code lib.Code
    --> OWNER (one) lib.Region
}
type Region = String
`),
		"lib.yammm": []byte(`schema "lib"
type Code = String[1, 5]
type Region {
    id String primary
}
`),
	}, "main.yammm", t.TempDir())
	if s.IsZero() || res.Len() != 0 {
		t.Fatalf("a local type and an imported datatype of one name must load together\n%v", res)
	}
}

func TestLoad_AnImportAliasCollisionNamesTheLocalKind(t *testing.T) {
	t.Parallel()

	const collision = `import alias "Geo" collides with local `
	tests := map[string]struct {
		local string
		kind  string
		want  map[diag.Code]int
	}{
		"a type": {
			local: "type Geo {\n    id String primary\n}",
			kind:  "type",
			want:  map[diag.Code]int{diag.E_IMPORT_ALIAS_COLLISION: 1},
		},
		"a datatype": {
			local: "type Geo = String",
			kind:  "datatype",
			want:  map[diag.Code]int{diag.E_IMPORT_ALIAS_COLLISION: 1},
		},
		"a datatype and then a type: the first declaration's kind": {
			local: "type Geo = String\ntype Geo {\n    id String primary\n}",
			kind:  "datatype",
			want:  map[diag.Code]int{diag.E_IMPORT_ALIAS_COLLISION: 1, diag.E_DUPLICATE_TYPE: 1},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res := loadSourcesErr(t, map[string][]byte{
				"main.yammm": []byte("schema \"main\"\nimport \"./geo\" as Geo\n" + tc.local + "\n"),
				"geo.yammm":  []byte(`schema "geo"`),
			})
			wantCounts(t, res, tc.want)
			for issue := range res.Issues() {
				if issue.Code() != diag.E_IMPORT_ALIAS_COLLISION {
					continue
				}
				if want := collision + tc.kind + " name"; issue.Message() != want {
					t.Errorf("got %q, want %q", issue.Message(), want)
				}
			}
		})
	}
}

// A model built by hand gives every declaration the zero span. The first
// declaration of a name is then the one visited first: a type before a
// datatype, and otherwise the model's order.
func TestComplete_DeclarationsAtOneSpanKeepTheFirstVisited(t *testing.T) {
	t.Parallel()

	key := []*schema.TestPropertyDecl{{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true}}
	shared := &schema.TestDataTypeDecl{Name: "A", Constraint: schema.NewStringConstraint()}
	tests := map[string]struct {
		model *schema.TestModel
		want  string
	}{
		"a type and a datatype": {
			model: &schema.TestModel{
				Name:      "test",
				Types:     []*schema.TestTypeDecl{{Name: "A", Properties: key}},
				DataTypes: []*schema.TestDataTypeDecl{{Name: "A", Constraint: schema.NewStringConstraint()}},
			},
			want: datatypeAfterType,
		},
		"two types, the first abstract and with no key": {
			model: &schema.TestModel{
				Name:  "test",
				Types: []*schema.TestTypeDecl{{Name: "A", IsAbstract: true}, {Name: "A"}},
			},
			want: `type "A" is defined multiple times`,
		},
		"one datatype declaration listed twice": {
			model: &schema.TestModel{
				Name:      "test",
				DataTypes: []*schema.TestDataTypeDecl{shared, shared},
			},
			want: `datatype "A" is defined multiple times`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, res := buildAndComplete(t, tc.model)
			if !s.IsZero() {
				t.Errorf("the model completes; want it refused")
			}
			got := duplicatesOf(t, res)
			if len(got) != 1 || got[0].message != tc.want {
				t.Errorf("got %+v, want one issue %q\n%v", got, tc.want, res)
			}
		})
	}
}
