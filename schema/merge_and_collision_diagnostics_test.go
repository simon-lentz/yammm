package schema_test

import (
	"fmt"
	"iter"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func issuesOfSource(t *testing.T, src string) []diag.Issue {
	t.Helper()
	_, res := schema.LoadString(t.Context(), src, "m.yammm")
	return slices.Collect(res.Issues())
}

func TestLoad_ARelationNameTwoParentsCarryCollides(t *testing.T) {
	t.Parallel()
	for name, parents := range map[string]string{
		"an association first": "abstract type P1 {\n\t--> FOO (one) X\n}\n\nabstract type P2 {\n\t--> OTHER (one) X\n\t*-> FOO (one) L\n}\n",
		"a composition second": "abstract type P1 {\n\t--> FOO (one) X\n\t*-> OTHER (one) L\n}\n\nabstract type P2 {\n\t*-> FOO (one) L\n}\n",
	} {
		src := "schema \"c\"\n\ntype X {\n\tid String primary\n}\n\npart type L {\n\tn String\n}\n\n" + parents + "\ntype K extends P1, P2 {\n\tid String primary\n}\n"
		issues := issuesOfSource(t, src)
		if !slices.ContainsFunc(issues, func(i diag.Issue) bool { return i.Code() == diag.E_RELATION_COLLISION }) {
			t.Errorf("%s: issues = %v, want E_RELATION_COLLISION", name, issues)
		}
	}
}

func TestLoad_AnOwnRelationCollidesAtItsOwnLine(t *testing.T) {
	t.Parallel()
	issues := issuesOfSource(t, "schema \"c\"\n\ntype X {\n\tid String primary\n}\n\nabstract type P {\n\tfoo String\n}\n\ntype K extends P {\n\tid String primary\n\t--> FOO (one) X\n}\n")
	if len(issues) != 1 || issues[0].Span().Start.Line != 13 {
		t.Fatalf("issues = %v, want one at line 13", issues)
	}
	if related := issues[0].Related(); len(related) != 1 {
		t.Errorf("related = %v, want the inherited property alone", related)
	}
}

func TestLoad_APartTypesOwnAssociationDrawsOneError(t *testing.T) {
	t.Parallel()
	issues := issuesOfSource(t, "schema \"c\"\n\ntype X {\n\tid String primary\n}\n\npart type L {\n\tn String\n\t--> FOO (one) X\n}\n")
	if len(issues) != 1 || issues[0].Code() != diag.E_INVALID_ASSOCIATION_TARGET || !strings.Contains(issues[0].Message(), "cannot declare") {
		t.Errorf("issues = %v, want one refusal of the declaration", issues)
	}
}

func TestLoad_ARivalReachedTwiceIsRelatedOnce(t *testing.T) {
	t.Parallel()
	const relTargets = "type A1 {\n\tid String primary\n}\n\ntype B1 {\n\tid String primary\n}\n\n"
	const rivals = relTargets + "abstract type S {\n\t--> FOO (one) A1\n}\n\nabstract type R {\n\t--> FOO (one) B1\n}\n\nabstract type X1 extends R {\n}\n\n"
	for name, src := range map[string]string{
		"a property origin": "schema \"o\"\n\nabstract type S {\n\tname Integer\n}\n\nabstract type R {\n\tname Boolean\n}\n\nabstract type X1 extends R {\n}\n\nabstract type X2 {\n\tname Boolean\n}\n\ntype T extends S, X1, X2 {\n\tid String primary\n}\n",
		"a relation rival":  "schema \"r\"\n\n" + rivals + "type T extends S, X1 {\n\tid String primary\n}\n",
		"two routes":        "schema \"t\"\n\n" + rivals + "abstract type X2 extends R {\n}\n\ntype T extends S, X1, X2 {\n\tid String primary\n}\n",
	} {
		issues := issuesOfSource(t, src)
		if len(issues) == 0 {
			t.Fatalf("%s: no issue", name)
		}
		for _, issue := range issues {
			seen := map[string]bool{}
			for _, r := range issue.Related() {
				key := fmt.Sprint(r.Span, r.Message)
				if seen[key] {
					t.Errorf("%s: %s relates %q twice", name, issue.Code(), r.Message)
				}
				seen[key] = true
			}
		}
	}
}

func TestLoad_ARedundantIndexNamesTheFirstEmitter(t *testing.T) {
	t.Parallel()
	issues := issuesOfSource(t, "schema \"i\"\n\nabstract type Base {\n\tid String primary @index\n}\n\ntype Zed extends Base {\n}\n\ntype Yak extends Base {\n}\n")
	if len(issues) != 1 || !strings.Contains(issues[0].Message(), `"Zed"`) {
		t.Errorf("issues = %v, want one naming Zed", issues)
	}
}

func TestLoad_AnInvariantOverAnUnknownTargetDrawsOnlyTheRootCause(t *testing.T) {
	t.Parallel()
	issues := issuesOfSource(t, "schema \"u\"\n\ntype K {\n\tid String primary\n\t*-> ONE (one) Missing\n\t! \"m\" ONE + 1 > 0\n}\n")
	if len(issues) != 1 || issues[0].Code() != diag.E_UNKNOWN_TYPE {
		t.Errorf("issues = %v, want E_UNKNOWN_TYPE alone", issues)
	}
}

// The part type's ancestor is reached only through a member no source imports, so its
// schema is not in the load's own table.
func TestLoad_APartInheritsAnAssociationThroughAnAncestorItNeverImports(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg := schema.NewRegistry()
	sources := map[string][]byte{
		filepath.Join(dir, "c.yammm"): []byte("schema \"c\"\n\nabstract type C {\n\textra String\n}\n"),
		filepath.Join(dir, "b.yammm"): []byte("schema \"b\"\n\nimport \"./c\" as c\n\ntype W {\n\tid String primary\n}\n\nabstract type B extends c.C {\n\t--> TO_W (one) W\n}\n"),
	}
	if _, res := schema.LoadSourcesWithEntry(t.Context(), sources, filepath.Join(dir, "b.yammm"), dir, schema.WithRegistry(reg)); res.HasErrors() {
		t.Fatal(res)
	}
	key := filepath.Join(dir, "x.yammm")
	x := map[string][]byte{key: []byte("schema \"x\"\n\nimport \"./b\" as b\n\npart type X extends b.B {\n\tmine String\n}\n")}
	_, res := schema.LoadSourcesWithEntry(t.Context(), x, key, dir, schema.WithRegistry(reg), schema.WithSourcesOnly(true))
	var codes []string
	for issue := range res.Issues() {
		codes = append(codes, issue.Code().String())
	}
	yammmtest.Diff(t, []string{diag.E_INVALID_ASSOCIATION_TARGET.String()}, codes)
}

func TestBuilder_TwoImportsThatShareASourcelessMemberBuild(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	memberID := location.MustSourceIDFromPath(filepath.Join(dir, "m.yammm"))
	member, res := schema.NewBuilder().WithName("m").WithSourceID(memberID).
		AddType("M").WithPrimaryKey("id", schema.NewStringConstraint()).Done().Build()
	if res.HasErrors() {
		t.Fatal(res)
	}
	building := schema.NewRegistry()
	if err := building.Register(member); err != nil {
		t.Fatal(err)
	}
	shared := schema.NewRegistry()
	ids := map[string]location.SourceID{}
	for _, name := range []string{"r1", "r2"} {
		ids[name] = location.MustSourceIDFromPath(filepath.Join(dir, name+".yammm"))
		route, res := schema.NewBuilder().WithName(name).WithSourceID(ids[name]).WithRegistry(building).
			WithImportResolver(func(p string) (location.SourceID, bool) { return memberID, p == "./m" }).
			AddImport("./m", "m").AddType(strings.ToUpper(name)).WithPrimaryKey("id", schema.NewStringConstraint()).Done().Build()
		if res.HasErrors() {
			t.Fatal(res)
		}
		if err := shared.Register(route); err != nil {
			t.Fatal(err)
		}
	}
	top, res := schema.NewBuilder().WithName("top").WithSourceID(location.MustNewSourceID("test://top.yammm")).WithRegistry(shared).
		WithImportResolver(func(p string) (location.SourceID, bool) {
			id, ok := ids[strings.TrimPrefix(p, "./")]
			return id, ok
		}).
		AddImport("./r1", "a").AddImport("./r2", "b").AddType("K").WithPrimaryKey("id", schema.NewStringConstraint()).Done().Build()
	if res.HasErrors() || top.IsZero() {
		t.Errorf("Build = %v", res)
	}
}

// A walk that visits a shared import once per route takes 2^depth steps, so at this
// depth a regression never ends and the package's timeout reports it.
const deepDiamondDepth = 44

func deepDiamond(dir string) map[string][]byte {
	sources := map[string][]byte{}
	for i := range deepDiamondDepth {
		for _, side := range []string{"a", "b"} {
			name := fmt.Sprintf("%s%d", side, i)
			var sb strings.Builder
			fmt.Fprintf(&sb, "schema %q\n\n", name)
			if i+1 < deepDiamondDepth {
				fmt.Fprintf(&sb, "import \"./a%d\" as x\nimport \"./b%d\" as y\n\n", i+1, i+1)
			}
			fmt.Fprintf(&sb, "type T%s {\n\tid String primary\n}\n", name)
			sources[filepath.Join(dir, name+".yammm")] = []byte(sb.String())
		}
	}
	return sources
}

func TestDeepDiamond_EveryWalkVisitsAMemberOnce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg := schema.NewRegistry()
	top, res := schema.LoadSourcesWithEntry(t.Context(), deepDiamond(dir), filepath.Join(dir, "a0.yammm"), dir, schema.WithRegistry(reg))
	if res.HasErrors() {
		t.Fatal(res)
	}
	built, res := schema.NewBuilder().WithName("dtop").WithSourceID(location.MustNewSourceID("test://dtop.yammm")).WithRegistry(reg).
		WithImportResolver(func(string) (location.SourceID, bool) { return top.SourceID(), true }).
		AddImport("./a0", "t").AddType("K").WithPrimaryKey("id", schema.NewStringConstraint()).Done().Build()
	if res.HasErrors() || built.IsZero() {
		t.Fatal(res)
	}
	key := filepath.Join(dir, "next.yammm")
	entry := map[string][]byte{key: []byte("schema \"next\"\n\nimport \"./a0\" as t\n\ntype K {\n\tid String primary\n}\n")}
	if _, res := schema.LoadSourcesWithEntry(t.Context(), entry, key, dir, schema.WithRegistry(reg), schema.WithSourcesOnly(true)); res.HasErrors() {
		t.Fatal(res)
	}
}

func breakAfterTheFirst[T any](t *testing.T, name string, seq iter.Seq[T]) {
	t.Helper()
	var n int
	for range seq {
		n++
		break
	}
	if n != 1 {
		t.Errorf("%s yields %d before the break, want 1: the fixture must hold two", name, n)
	}
}

func TestRecordIterators_AnEarlyBreakStopsEachOne(t *testing.T) {
	t.Parallel()
	const src = `schema "b"

abstract type P1 {
	p1 String @index
	p1b String
	! "p1" p1 != ""
	! "p1b" p1b != ""
	--> A1 (one) X
	--> A1B (one) X
	*-> C1 (one) L
	*-> C1B (one) L
	@@index(p1, p1b)
	@@fulltext(p1, p1b)
}

abstract type P2 extends P1 {
}

part type L {
	n String
}

type X {
	id String primary
}

type K extends P1, P2 {
	id String primary
	note String
	! "k" id != ""
	! "kb" note != ""
	--> A2 (one) X {
		e1 String
		e2 String
	}
	--> A2B (one) X
	*-> C2 (one) L
	*-> C2B (one) L
	@@index(id, note)
	@@fulltext(note, id)
}

type Sub1 extends K {
}

type Sub2 extends K {
}
`
	s, res := schema.LoadString(t.Context(), src, "b.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	k, ok := s.Type("K")
	if !ok {
		t.Fatal("no type K")
	}
	breakAfterTheFirst(t, "Properties", k.Properties())
	breakAfterTheFirst(t, "AllProperties", k.AllProperties())
	breakAfterTheFirst(t, "Associations", k.Associations())
	breakAfterTheFirst(t, "AllAssociations", k.AllAssociations())
	breakAfterTheFirst(t, "Compositions", k.Compositions())
	breakAfterTheFirst(t, "AllCompositions", k.AllCompositions())
	breakAfterTheFirst(t, "Invariants", k.Invariants())
	breakAfterTheFirst(t, "AllInvariants", k.AllInvariants())
	breakAfterTheFirst(t, "AllAnnotations", k.AllAnnotations())
	breakAfterTheFirst(t, "Inherits", k.Inherits())
	breakAfterTheFirst(t, "SuperTypes", k.SuperTypes())
	breakAfterTheFirst(t, "SubTypes", k.SubTypes())
	breakAfterTheFirst(t, "Annotations", k.Annotations())
	rel, ok := k.Relation("A2")
	if !ok {
		t.Fatal("no relation A2")
	}
	breakAfterTheFirst(t, "Relation.Properties", rel.Properties())
}

func TestSchema_ImportsHoldDeclarationOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	key := filepath.Join(dir, "main.yammm")
	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		key:                           []byte("schema \"main\"\n\nimport \"./z\" as z\nimport \"./a\" as a\n\ntype M {\n\tid String primary\n}\n"),
		filepath.Join(dir, "a.yammm"): []byte("schema \"a\"\n\ntype A {\n\tid String primary\n}\n"),
		filepath.Join(dir, "z.yammm"): []byte("schema \"z\"\n\ntype Z {\n\tid String primary\n}\n"),
	}, key, dir)
	if res.HasErrors() {
		t.Fatal(res)
	}
	var aliases []string
	for imp := range s.Imports() {
		aliases = append(aliases, imp.Alias())
	}
	yammmtest.Diff(t, []string{"z", "a"}, aliases)
}
