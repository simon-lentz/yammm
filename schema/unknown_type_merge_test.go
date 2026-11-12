package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// A member whose type is not known is reported where the type is written. A
// redeclaration of that member in a subtype, or beside it in a second
// supertype, draws no conflict from it.
func TestLoad_AMemberOfUnknownTypeDrawsNoInheritanceConflict(t *testing.T) {
	t.Parallel()

	const displaced = "type A {\n    id String primary\n}\ntype A = String\n"
	const lib = "import \"./lib\" as lib\n"
	tests := map[string]struct {
		body string
		want map[diag.Code]int
	}{
		"a displaced name, narrowed by the subtype": {
			body: displaced + "type P {\n    id String primary\n    code A\n}\ntype C extends P {\n    code String[1, 3]\n}\n",
			want: map[diag.Code]int{diag.E_DUPLICATE_TYPE: 1},
		},
		"a displaced name in one of two supertypes": {
			body: displaced + "abstract type M {\n    code A\n}\nabstract type N {\n    code String\n}\ntype C extends M, N {\n    id String primary\n}\n",
			want: map[diag.Code]int{diag.E_DUPLICATE_TYPE: 1},
		},
		"a displaced name in the later of two supertypes": {
			body: displaced + "abstract type M {\n    code A\n}\nabstract type N {\n    code String\n}\ntype C extends N, M {\n    id String primary\n}\n",
			want: map[diag.Code]int{diag.E_DUPLICATE_TYPE: 1},
		},
		"a displaced name as a List element": {
			body: displaced + "type P {\n    id String primary\n    codes List<A>\n}\ntype C extends P {\n    codes List<String>[1, 3]\n}\n",
			want: map[diag.Code]int{diag.E_DUPLICATE_TYPE: 1},
		},
		"a displaced name at an edge property": {
			body: displaced + "type B {\n    id String primary\n}\ntype P {\n    id String primary\n    --> LINK (one) B {\n        w A\n    }\n}\ntype C extends P {\n    --> LINK (one) B {\n        w String\n    }\n}\n",
			want: map[diag.Code]int{diag.E_DUPLICATE_TYPE: 1},
		},
		"a displaced name at an edge property of the subtype": {
			body: displaced + "type B {\n    id String primary\n}\ntype P {\n    id String primary\n    --> LINK (one) B {\n        w String\n    }\n}\ntype C extends P {\n    --> LINK (one) B {\n        w A\n    }\n}\n",
			want: map[diag.Code]int{diag.E_DUPLICATE_TYPE: 1},
		},
		"an unknown name": {
			body: "type P {\n    id String primary\n    code Nope\n}\ntype C extends P {\n    code String[1, 3]\n}\n",
			want: map[diag.Code]int{diag.E_UNKNOWN_TYPE: 1},
		},
		"an unknown name in the subtype": {
			body: "type P {\n    id String primary\n    code String\n}\ntype C extends P {\n    code Nope\n}\n",
			want: map[diag.Code]int{diag.E_UNKNOWN_TYPE: 1},
		},
		"an unknown name at an edge property": {
			body: "type B {\n    id String primary\n}\ntype P {\n    id String primary\n    --> LINK (one) B {\n        w Nope\n    }\n}\ntype C extends P {\n    --> LINK (one) B {\n        w String\n    }\n}\n",
			want: map[diag.Code]int{diag.E_UNKNOWN_TYPE: 1},
		},
		"a type the parser could not build": {
			body: "type P {\n    id String primary\n    v Vector[0]\n}\ntype C extends P {\n    v Vector[3]\n}\n",
			want: map[diag.Code]int{diag.E_INVALID_CONSTRAINT: 1},
		},
		"a datatype whose own definition failed": {
			body: "type D = List<Nope>\ntype P {\n    id String primary\n    code D\n}\ntype C extends P {\n    code String\n}\n",
			want: map[diag.Code]int{diag.E_UNKNOWN_TYPE: 1},
		},
		"a datatype that names itself": {
			body: "type D = List<D>\ntype P {\n    id String primary\n    code D\n}\ntype C extends P {\n    code String\n}\n",
			want: map[diag.Code]int{diag.E_INVALID_CONSTRAINT: 1},
		},
		"a name behind an import that failed": {
			body: lib + "type P {\n    id String primary\n    code lib.Code\n}\ntype C extends P {\n    code String[1, 3]\n}\n",
			want: map[diag.Code]int{diag.E_SYNTAX: 1, diag.E_UPSTREAM_FAIL: 1},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res := loadSourcesErr(t, map[string][]byte{
				"main.yammm": []byte("schema \"main\"\n" + tc.body),
				"lib.yammm":  []byte("schema \"lib\"\ntype Code = Nope\n"),
			})
			wantCounts(t, res, tc.want)
		})
	}
}

// Two definitions whose types are known still conflict, and what is not judged
// is one member on the types its definition of unknown type reaches.
func TestLoad_AMemberOfKnownTypeStillConflicts(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body string
		want map[diag.Code]int
	}{
		"a property the subtype widens": {
			body: "type P {\n    id String primary\n    code String[1, 3]\n}\ntype C extends P {\n    code Integer\n}\n",
			want: map[diag.Code]int{diag.E_PROPERTY_CONFLICT: 1},
		},
		"a type the parser reports and still builds": {
			body: "type P {\n    id String primary\n    code String[5, 1]\n}\ntype C extends P {\n    code String[1, 3]\n}\n",
			want: map[diag.Code]int{diag.E_INVALID_CONSTRAINT: 1, diag.E_PROPERTY_CONFLICT: 1},
		},
		"a property beside one that is not judged": {
			body: "abstract type M {\n    code Nope\n    size String\n}\nabstract type N {\n    code String\n    size Integer\n}\n" +
				"type C extends M, N {\n    id String primary\n}\n",
			want: map[diag.Code]int{diag.E_UNKNOWN_TYPE: 1, diag.E_PROPERTY_CONFLICT: 1},
		},
		"a relation beside one that is not judged": {
			body: "type B {\n    id String primary\n}\ntype B2 {\n    id String primary\n}\n" +
				"abstract type M {\n    --> LINK (one) Nope\n    --> OTHER (one) B\n}\n" +
				"abstract type N {\n    --> LINK (one) B\n    --> OTHER (one) B2\n}\n" +
				"type C extends M, N {\n    id String primary\n}\n",
			want: map[diag.Code]int{diag.E_UNKNOWN_TYPE: 1, diag.E_RELATION_COLLISION: 1},
		},
		"a supertype's own shadow warning beside a definition of unknown type": {
			body: "abstract type Base {\n    code String @index\n}\nabstract type P extends Base {\n    code String[1, 3]\n}\n" +
				"abstract type Q {\n    code Nope\n}\ntype C extends P, Q {\n    id String primary\n}\n",
			want: map[diag.Code]int{diag.E_UNKNOWN_TYPE: 1, diag.W_ANNOTATION_SHADOWED: 1},
		},
		"an edge property the subtype retypes": {
			body: "type B {\n    id String primary\n}\ntype P {\n    id String primary\n    --> LINK (one) B {\n        w Integer\n    }\n}\ntype C extends P {\n    --> LINK (one) B {\n        w String\n    }\n}\n",
			want: map[diag.Code]int{diag.E_RELATION_COLLISION: 1},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res := loadSourcesErr(t, map[string][]byte{"main.yammm": []byte("schema \"main\"\n" + tc.body)})
			wantCounts(t, res, tc.want)
		})
	}
}

// A member that reaches a type with one definition of unknown type is not
// judged on that type, whatever the order of the extends clause: no conflict
// is drawn from that definition, and none between the definitions it stands
// beside.
func TestLoad_AMemberWithADefinitionOfUnknownTypeIsNotJudgedInAnyOrder(t *testing.T) {
	t.Parallel()

	const target = "type B {\n    id String primary\n}\ntype B2 {\n    id String primary\n}\n"
	tests := map[string]string{
		"a property": "abstract type M {\n    code Nope\n}\nabstract type N {\n    code String\n}\n" +
			"abstract type O {\n    code Integer\n}\n",
		"an edge property": target + "abstract type M {\n    --> LINK (one) B {\n        w Nope\n    }\n}\n" +
			"abstract type N {\n    --> LINK (one) B {\n        w String\n    }\n}\n" +
			"abstract type O {\n    --> LINK (one) B {\n        w Integer\n    }\n}\n",
		"an edge property beside two relations that differ in target": target +
			"abstract type M {\n    --> LINK (one) B {\n        w Nope\n    }\n}\n" +
			"abstract type N {\n    --> LINK (one) B\n}\nabstract type O {\n    --> LINK (one) B2\n}\n",
		"a relation target": target + "abstract type M {\n    --> LINK (one) Nope\n}\n" +
			"abstract type N {\n    --> LINK (one) B\n}\nabstract type O {\n    --> LINK (one) B2\n}\n",
	}
	orders := []string{"M, N, O", "M, O, N", "N, M, O", "N, O, M", "O, M, N", "O, N, M"}
	for name, supertypes := range tests {
		for _, order := range orders {
			t.Run(name+"/"+order, func(t *testing.T) {
				t.Parallel()
				res := loadSourcesErr(t, map[string][]byte{"main.yammm": []byte(
					"schema \"main\"\n" + supertypes + "type C extends " + order + " {\n    id String primary\n}\n")})
				wantCounts(t, res, map[diag.Code]int{diag.E_UNKNOWN_TYPE: 1})
			})
		}
	}
}

// The rule holds at every type the definition of unknown type reaches, and
// for a difference no type decides.
func TestLoad_ADefinitionOfUnknownTypeIsNotJudgedAtASubtype(t *testing.T) {
	t.Parallel()

	const supertypes = "abstract type N {\n    code String\n}\nabstract type O {\n    code Integer\n}\n"
	tests := map[string]string{
		"the declaration of a type between": supertypes +
			"type C extends N, O {\n    id String primary\n    code Nope\n}\ntype G extends C {\n}\n",
		"a supertype's supertype": "abstract type M {\n    code Nope\n}\n" + supertypes +
			"abstract type K extends N, M {\n}\ntype G extends K, O {\n    id String primary\n}\n",
		"another edge property of the relation": "type B {\n    id String primary\n}\n" +
			"type P {\n    id String primary\n    --> LINK (one) B {\n        a Nope\n        b String\n    }\n}\n" +
			"type C extends P {\n    --> LINK (one) B {\n        a String\n        b Integer\n    }\n}\n",
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res := loadSourcesErr(t, map[string][]byte{"main.yammm": []byte("schema \"main\"\n" + body)})
			wantCounts(t, res, map[diag.Code]int{diag.E_UNKNOWN_TYPE: 1})
		})
	}
}

// The merge runs on a member that is not judged as on any other, and only its
// conflict is withheld. So a redeclaration that conflicts with what it inherits
// decides nothing about the annotations of the member, as one that draws the
// conflict decides nothing: no shadow warning, and no dropped annotation for a
// subtype.
func TestLoad_ARedeclarationRefusedUnreportedDecidesNoAnnotation(t *testing.T) {
	t.Parallel()

	const base = "abstract type Base {\n    id String primary\n    code String @index\n}\n"
	tests := map[string]string{
		"the redeclaration is of unknown type": base + "type C extends Base {\n    code Nope\n}\n",
		"the inherited definition is of unknown type": "abstract type Base {\n    id String primary\n" +
			"    code Nope @index\n}\ntype C extends Base {\n    code String\n}\n",
		"a subtype beside a supertype that keeps the annotation": base + "abstract type L extends Base {\n}\n" +
			"abstract type R extends Base {\n    code Nope\n}\ntype D extends L, R {\n}\n",
		"a subtype, the supertypes in the other order": base + "abstract type L extends Base {\n}\n" +
			"abstract type R extends Base {\n    code Nope\n}\ntype D extends R, L {\n}\n",
		"a full-text annotation": "abstract type Base {\n    id String primary\n    code String @fulltext\n}\n" +
			"type C extends Base {\n    code Nope\n}\n",
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res := loadSourcesErr(t, map[string][]byte{"main.yammm": []byte("schema \"main\"\n" + body)})
			wantCounts(t, res, map[diag.Code]int{diag.E_UNKNOWN_TYPE: 1})
		})
	}
}

// A redeclaration that reads the same unresolved name as the definition it
// inherits agrees with it. Its annotations are judged as for any property: the
// annotation it leaves out draws the shadow warning, and a subtype beside a
// supertype that keeps the annotation draws the disagreement.
func TestLoad_ARedeclarationOfOneUnknownTypeKeepsItsAnnotationIssues(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body string
		want map[diag.Code]int
	}{
		"the shadow warning": {
			body: "abstract type Base {\n    id String primary\n    code Nope @index\n}\n" +
				"type C extends Base {\n    code Nope\n}\n",
			want: map[diag.Code]int{diag.E_UNKNOWN_TYPE: 2, diag.W_ANNOTATION_SHADOWED: 1},
		},
		"the disagreement on a subtype": {
			body: "abstract type Base {\n    code Nope @writeOnce\n}\nabstract type L extends Base {\n}\n" +
				"abstract type R extends Base {\n    code Nope\n}\npart type D extends L, R {\n}\n",
			want: map[diag.Code]int{diag.E_UNKNOWN_TYPE: 2, diag.W_ANNOTATION_SHADOWED: 1, diag.E_INVALID_ANNOTATION: 1},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res := loadSourcesErr(t, map[string][]byte{"main.yammm": []byte("schema \"main\"\n" + tc.body)})
			wantCounts(t, res, tc.want)
		})
	}
}
