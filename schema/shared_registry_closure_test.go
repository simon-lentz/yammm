package schema_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// TestLoadSourcesWithEntry_BindsARegisteredClosureInAnyImportOrder holds a load
// that shares a Registry to one answer for a schema the registry holds only
// inside another schema's import closure: the compiled schema is bound, whether
// the load imports the closure's owner first or the member itself, and its
// source joins the load's sources either way.
func TestLoadSourcesWithEntry_BindsARegisteredClosureInAnyImportOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("c.yammm", "schema \"c\"\n\ntype C {\n\tid String primary\n}\n")
	write("b.yammm", "schema \"b\"\n\nimport \"c\" as c\n\ntype B {\n\tid String primary\n\t--> TO_C (one) c.C\n}\n")
	write("a.yammm", "schema \"a\"\n\nimport \"./b\" as b\n\ntype A {\n\tid String primary\n\t--> TO_B (one) b.B\n}\n")

	a, res := schema.Load(t.Context(), filepath.Join(dir, "a.yammm"), schema.WithModuleRoot(dir))
	if a.IsZero() {
		t.Fatalf("load a: %v", res)
	}
	b := importedSchema(a, "b")
	if b.IsZero() {
		t.Fatal("a binds no schema named b")
	}

	const (
		aThenB = "schema \"x\"\n\nimport \"./a\" as a\nimport \"./b\" as b\n\ntype X {\n\tid String primary\n\t--> TO_A (one) a.A\n\t--> TO_B (one) b.B\n}\n"
		bThenA = "schema \"x\"\n\nimport \"./b\" as b\nimport \"./a\" as a\n\ntype X {\n\tid String primary\n\t--> TO_A (one) a.A\n\t--> TO_B (one) b.B\n}\n"
	)
	rows := []struct {
		name string
		x    string
		opts []schema.LoadOption
	}{
		{"sources only, the owner imported first", aThenB, []schema.LoadOption{schema.WithSourcesOnly(true)}},
		{"sources only, the member imported first", bThenA, []schema.LoadOption{schema.WithSourcesOnly(true)}},
		{"from disk, the owner imported first", aThenB, nil},
		{"from disk, the member imported first", bThenA, nil},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			reg := schema.NewRegistry()
			if err := reg.Register(a); err != nil {
				t.Fatal(err)
			}
			key := filepath.Join(dir, "x.yammm")
			opts := append([]schema.LoadOption{schema.WithRegistry(reg)}, row.opts...)
			x, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{key: []byte(row.x)}, key, dir, opts...)
			if x.IsZero() || res.HasErrors() {
				t.Fatalf("load x: %s", issueLines(res))
			}
			if got := importedSchema(x, "b"); got != b {
				t.Errorf("x binds b as %p, want the registered closure's compiled schema %p", got, b)
			}
			if _, ok := x.Sources().ContentBySource(b.SourceID()); !ok {
				t.Errorf("x's sources do not hold b (%s)", b.SourceID())
			}
		})
	}
}

// TestLoadSourcesWithEntry_BindsAClosureMemberOnlyWhenItsCompilesAgree holds a
// shared Registry whose import closures hold one source twice. Two compiles of
// the same bytes are one schema to bind. Two compiles of different bytes name no
// schema a load can choose between, so an import of that source is refused.
func TestLoadSourcesWithEntry_BindsAClosureMemberOnlyWhenItsCompilesAgree(t *testing.T) {
	t.Parallel()

	const (
		b1 = "schema \"b\"\n\ntype B {\n\tid String primary\n}\n"
		b2 = "schema \"b\"\n\ntype B {\n\tid String primary\n\tname String\n}\n"
		a  = "schema \"a\"\n\nimport \"./b\" as b\n\ntype A {\n\tid String primary\n\t--> TO_B (one) b.B\n}\n"
		d  = "schema \"d\"\n\nimport \"./b\" as b\n\ntype D {\n\tid String primary\n\t--> TO_B (one) b.B\n}\n"
		x  = "schema \"x\"\n\nimport \"./b\" as b\n\ntype X {\n\tid String primary\n\t--> TO_B (one) b.B\n}\n"
	)
	rows := []struct {
		name     string
		bForD    string
		wantBind bool
	}{
		{"two compiles of the same bytes", b1, true},
		{"two compiles of different bytes", b2, false},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			write := func(name, body string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			load := func(name string) schema.Schema {
				t.Helper()
				s, res := schema.Load(t.Context(), filepath.Join(dir, name), schema.WithModuleRoot(dir))
				if s.IsZero() {
					t.Fatalf("load %s: %s", name, issueLines(res))
				}
				return s
			}
			write("b.yammm", b1)
			write("a.yammm", a)
			sa := load("a.yammm")
			write("b.yammm", row.bForD)
			write("d.yammm", d)
			sd := load("d.yammm")

			reg := schema.NewRegistry()
			for _, s := range []schema.Schema{sa, sd} {
				if err := reg.Register(s); err != nil {
					t.Fatal(err)
				}
			}
			key := filepath.Join(dir, "x.yammm")
			got, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{key: []byte(x)}, key, dir,
				schema.WithRegistry(reg), schema.WithSourcesOnly(true))
			bound := !got.IsZero() && !res.HasErrors()
			if bound != row.wantBind {
				t.Errorf("x loaded = %v, want %v: %s", bound, row.wantBind, issueLines(res))
			}
			if !row.wantBind {
				return
			}
			// Registry.All orders the walk by SourceID, so a.yammm's compile is
			// the one two agreeing compiles resolve to.
			if want, from := importedSchema(sa, "b"), importedSchema(sd, "b"); importedSchema(got, "b") != want {
				t.Errorf("x binds b as %p, want a's compile %p rather than d's %p", importedSchema(got, "b"), want, from)
			}
		})
	}
}

func importedSchema(s schema.Schema, name string) schema.Schema {
	for imp := range s.Imports() {
		if sub := imp.Schema(); !sub.IsZero() && sub.Name() == name {
			return sub
		}
	}
	return schema.Schema{}
}

func issueLines(res diag.Result) string {
	var lines []string
	for issue := range res.Issues() {
		lines = append(lines, issue.Code().String()+": "+issue.Message())
	}
	return strings.Join(lines, "; ")
}

// TestLoadSourcesWithEntry_BindsAClosureMemberTwoImportsDeep holds a load that
// shares a Registry to the compiled schema for a source the registry reaches
// only through two import hops. The registry holds one root; the source is its
// import's import.
func TestLoadSourcesWithEntry_BindsAClosureMemberTwoImportsDeep(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSchema(t, dir, "c.yammm", "schema \"c\"\n\ntype C {\n\tid String primary\n}\n")
	writeSchema(t, dir, "b.yammm", "schema \"b\"\n\nimport \"./c\" as c\n\ntype B {\n\tid String primary\n\t--> TO_C (one) c.C\n}\n")
	writeSchema(t, dir, "a.yammm", "schema \"a\"\n\nimport \"./b\" as b\n\ntype A {\n\tid String primary\n\t--> TO_B (one) b.B\n}\n")

	a, res := schema.Load(t.Context(), filepath.Join(dir, "a.yammm"), schema.WithModuleRoot(dir))
	if a.IsZero() {
		t.Fatalf("load a: %s", issueLines(res))
	}
	c := importedSchema(importedSchema(a, "b"), "c")
	if c.IsZero() {
		t.Fatal("a's closure holds no schema named c")
	}

	reg := schema.NewRegistry()
	if err := reg.Register(a); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "x.yammm")
	x, res := schema.LoadSourcesWithEntry(t.Context(),
		map[string][]byte{key: []byte("schema \"x\"\n\nimport \"./c\" as c\n\ntype X {\n\tid String primary\n\t--> TO_C (one) c.C\n}\n")},
		key, dir, schema.WithRegistry(reg), schema.WithSourcesOnly(true))
	if x.IsZero() || res.HasErrors() {
		t.Fatalf("load x: %s", issueLines(res))
	}
	if got := importedSchema(x, "c"); got != c {
		t.Errorf("x binds c as %p, want the closure's compiled schema %p", got, c)
	}
}

// TestLoadSourcesWithEntry_KeepsARefusedClosureMemberRefused holds a Registry
// whose roots compile one source three times, two from one spelling and one from
// another. The disagreement refuses the source, and a root walked after the
// refusal leaves it refused.
func TestLoadSourcesWithEntry_KeepsARefusedClosureMemberRefused(t *testing.T) {
	t.Parallel()

	const (
		b1   = "schema \"b\"\n\ntype B {\n\tid String primary\n}\n"
		b2   = "schema \"b\"\n\ntype B {\n\tid String primary\n\tname String\n}\n"
		root = "schema \"%s\"\n\nimport \"./b\" as b\n\ntype %s {\n\tid String primary\n\t--> TO_B (one) b.B\n}\n"
	)
	dir := t.TempDir()
	reg := schema.NewRegistry()
	// r2 sorts between r1 and r3, so the walk meets the disagreeing compile
	// second and revisits the refused entry at r3.
	for _, row := range []struct{ name, b string }{{"r1", b1}, {"r2", b2}, {"r3", b1}} {
		writeSchema(t, dir, "b.yammm", row.b)
		writeSchema(t, dir, row.name+".yammm", fmt.Sprintf(root, row.name, strings.ToUpper(row.name)))
		s, res := schema.Load(t.Context(), filepath.Join(dir, row.name+".yammm"), schema.WithModuleRoot(dir))
		if s.IsZero() {
			t.Fatalf("load %s: %s", row.name, issueLines(res))
		}
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
	}

	key := filepath.Join(dir, "x.yammm")
	x, res := schema.LoadSourcesWithEntry(t.Context(),
		map[string][]byte{key: []byte("schema \"x\"\n\nimport \"./b\" as b\n\ntype X {\n\tid String primary\n\t--> TO_B (one) b.B\n}\n")},
		key, dir, schema.WithRegistry(reg), schema.WithSourcesOnly(true))
	if !x.IsZero() && !res.HasErrors() {
		t.Errorf("x bound a source three registered closures disagree on, want it read like any other import")
	}
}

// TestLoadSourcesWithEntry_PrefersADirectlyRegisteredSchemaToAClosureMember
// holds one SourceID the Registry holds twice — as a registered schema and,
// compiled from other bytes, inside another registered schema's closure — to the
// registered schema.
func TestLoadSourcesWithEntry_PrefersADirectlyRegisteredSchemaToAClosureMember(t *testing.T) {
	t.Parallel()

	const (
		b1 = "schema \"b\"\n\ntype B {\n\tid String primary\n}\n"
		b2 = "schema \"b\"\n\ntype B {\n\tid String primary\n\tname String\n}\n"
	)
	dir := t.TempDir()
	writeSchema(t, dir, "b.yammm", b2)
	writeSchema(t, dir, "r.yammm", "schema \"r\"\n\nimport \"./b\" as b\n\ntype R {\n\tid String primary\n\t--> TO_B (one) b.B\n}\n")
	r, res := schema.Load(t.Context(), filepath.Join(dir, "r.yammm"), schema.WithModuleRoot(dir))
	if r.IsZero() {
		t.Fatalf("load r: %s", issueLines(res))
	}
	writeSchema(t, dir, "b.yammm", b1)
	b, res := schema.Load(t.Context(), filepath.Join(dir, "b.yammm"), schema.WithModuleRoot(dir))
	if b.IsZero() {
		t.Fatalf("load b: %s", issueLines(res))
	}
	if inClosure := importedSchema(r, "b"); inClosure == b {
		t.Fatal("r's closure holds the same compiled schema as the direct registration")
	}

	reg := schema.NewRegistry()
	for _, s := range []schema.Schema{b, r} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	key := filepath.Join(dir, "x.yammm")
	x, res := schema.LoadSourcesWithEntry(t.Context(),
		map[string][]byte{key: []byte("schema \"x\"\n\nimport \"./b\" as b\n\ntype X {\n\tid String primary\n\t--> TO_B (one) b.B\n}\n")},
		key, dir, schema.WithRegistry(reg), schema.WithSourcesOnly(true))
	if x.IsZero() || res.HasErrors() {
		t.Fatalf("load x: %s", issueLines(res))
	}
	if got := importedSchema(x, "b"); got != b {
		t.Errorf("x binds b as %p, want the registered schema %p rather than the closure's", got, b)
	}
}

func writeSchema(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestLoadSourcesWithEntry_BindsASourcelessClosureMemberReachedTwice holds a
// Builder-built schema, which carries no sources, to itself when two registered
// closures hold it. Sources decide whether two compiles of one SourceID agree,
// and a schema with none agrees with itself alone.
func TestLoadSourcesWithEntry_BindsASourcelessClosureMemberReachedTwice(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSchema(t, dir, "m.yammm", "schema \"m\"\n\ntype M {\n\tid String primary\n}\n")
	mID := location.MustSourceIDFromPath(filepath.Join(dir, "m.yammm"))
	m, res := schema.NewBuilder().WithName("m").WithSourceID(mID).
		AddType("M").WithPrimaryKey("id", schema.NewStringConstraint()).Done().
		Build()
	if m.IsZero() {
		t.Fatalf("build m: %s", issueLines(res))
	}
	if !m.Sources().IsZero() {
		t.Fatal("a Builder-built schema carries sources, so this test no longer probes the sourceless case")
	}

	// m is wired into each root from a registry the load never sees, so the load
	// reaches m only by walking a registered closure.
	building := schema.NewRegistry()
	if err := building.Register(m); err != nil {
		t.Fatal(err)
	}
	shared := schema.NewRegistry()
	for _, name := range []string{"r1", "r2"} {
		writeSchema(t, dir, name+".yammm", "schema \""+name+"\"\n")
		r, res := schema.NewBuilder().WithName(name).
			WithSourceID(location.MustSourceIDFromPath(filepath.Join(dir, name+".yammm"))).
			WithRegistry(building).
			WithImportResolver(func(path string) (location.SourceID, bool) {
				return mID, path == "./m"
			}).
			AddImport("./m", "m").
			AddType(strings.ToUpper(name)).WithPrimaryKey("id", schema.NewStringConstraint()).Done().
			Build()
		if r.IsZero() {
			t.Fatalf("build %s: %s", name, issueLines(res))
		}
		if importedSchema(r, "m") != m {
			t.Fatalf("%s does not hold m in its closure", name)
		}
		if err := shared.Register(r); err != nil {
			t.Fatal(err)
		}
	}

	key := filepath.Join(dir, "x.yammm")
	x, res := schema.LoadSourcesWithEntry(t.Context(),
		map[string][]byte{key: []byte("schema \"x\"\n\nimport \"./m\" as m\n\ntype X {\n\tid String primary\n\t--> TO_M (one) m.M\n}\n")},
		key, dir, schema.WithRegistry(shared), schema.WithSourcesOnly(true))
	if x.IsZero() || res.HasErrors() {
		t.Fatalf("load x: %s", issueLines(res))
	}
	if got := importedSchema(x, "m"); got != m {
		t.Errorf("x binds m as %p, want the one Builder-built schema %p", got, m)
	}
}

// disagreeingOwners loads a and d, each importing ./b, where a's b and d's b
// were compiled from different bytes, and returns them for a shared Registry.
func disagreeingOwners(t *testing.T, dir string) (a, d schema.Schema) {
	t.Helper()
	const (
		b1 = "schema \"b\"\n\ntype B {\n\tid String primary\n}\n"
		b2 = "schema \"b\"\n\ntype B {\n\tid String primary\n\tname String\n}\n"
	)
	load := func(name string) schema.Schema {
		t.Helper()
		s, res := schema.Load(t.Context(), filepath.Join(dir, name), schema.WithModuleRoot(dir))
		if s.IsZero() {
			t.Fatalf("load %s: %s", name, issueLines(res))
		}
		return s
	}
	writeSchema(t, dir, "b.yammm", b1)
	writeSchema(t, dir, "a.yammm", "schema \"a\"\n\nimport \"./b\" as b\n\ntype A {\n\tid String primary\n\t--> TO_B (one) b.B\n}\n")
	a = load("a.yammm")
	writeSchema(t, dir, "b.yammm", b2)
	writeSchema(t, dir, "d.yammm", "schema \"d\"\n\nimport \"./b\" as b\n\ntype D {\n\tid String primary\n\t--> TO_B (one) b.B\n}\n")
	d = load("d.yammm")
	return a, d
}

// TestLoadSourcesWithEntry_RefusesADisagreeingMemberInEitherImportOrder holds
// a source two registered closures compiled from different bytes refused,
// whether the load imports its owner before it or after it, and whether the
// load is restricted to its own sources or reads from disk. Importing the owner
// first used to bind the owner's compile, and reading from disk after the owner
// compiled the source a third time.
func TestLoadSourcesWithEntry_RefusesADisagreeingMemberInEitherImportOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sa, sd := disagreeingOwners(t, dir)
	rows := []struct{ name, x string }{
		{"the owner first", "schema \"x\"\n\nimport \"./a\" as a\nimport \"./b\" as b\n\ntype X {\n\tid String primary\n\t--> TO_A (one) a.A\n\t--> TO_B (one) b.B\n}\n"},
		{"the member first", "schema \"x\"\n\nimport \"./b\" as b\nimport \"./a\" as a\n\ntype X {\n\tid String primary\n\t--> TO_A (one) a.A\n\t--> TO_B (one) b.B\n}\n"},
	}
	for _, row := range rows {
		for _, sourcesOnly := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s, sources only %v", row.name, sourcesOnly), func(t *testing.T) {
				t.Parallel()
				reg := schema.NewRegistry()
				for _, s := range []schema.Schema{sa, sd} {
					if err := reg.Register(s); err != nil {
						t.Fatal(err)
					}
				}
				key := filepath.Join(dir, "x.yammm")
				x, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{key: []byte(row.x)}, key, dir,
					schema.WithRegistry(reg), schema.WithSourcesOnly(sourcesOnly))
				if !x.IsZero() && !res.HasErrors() {
					t.Fatalf("x bound b as %p; want the import refused", importedSchema(x, "b"))
				}
				if got := res.CodeCounts(diag.Error)[diag.E_IMPORT_RESOLVE]; got != 1 || errorCount(res) != 1 {
					t.Errorf("E_IMPORT_RESOLVE reported %d times among %d errors, want the one refusal alone: %s", got, errorCount(res), issueLines(res))
				}
				if !strings.Contains(issueLines(res), "two different compiles") {
					t.Errorf("the refusal does not name the disagreement: %s", issueLines(res))
				}
				assertResolutionShape(t, res)
			})
		}
	}
}

// TestLoadSourcesWithEntry_RefusesTheSecondOfTwoOwnersThatDisagree holds a load
// that imports two registered schemas whose closures hold one source compiled
// from different bytes, and never that source itself, to a refusal of the second
// import: binding both would give one SourceID two types.
func TestLoadSourcesWithEntry_RefusesTheSecondOfTwoOwnersThatDisagree(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sa, sd := disagreeingOwners(t, dir)
	rows := []struct{ name, x, refused string }{
		{"a then d", "schema \"x\"\n\nimport \"./a\" as a\nimport \"./d\" as d\n\ntype X {\n\tid String primary\n\t--> TO_A (one) a.A\n\t--> TO_D (one) d.D\n}\n", "./d"},
		{"d then a", "schema \"x\"\n\nimport \"./d\" as d\nimport \"./a\" as a\n\ntype X {\n\tid String primary\n\t--> TO_A (one) a.A\n\t--> TO_D (one) d.D\n}\n", "./a"},
	}
	for _, row := range rows {
		for _, sourcesOnly := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s, sources only %v", row.name, sourcesOnly), func(t *testing.T) {
				t.Parallel()
				reg := schema.NewRegistry()
				for _, s := range []schema.Schema{sa, sd} {
					if err := reg.Register(s); err != nil {
						t.Fatal(err)
					}
				}
				key := filepath.Join(dir, "x.yammm")
				x, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{key: []byte(row.x)}, key, dir,
					schema.WithRegistry(reg), schema.WithSourcesOnly(sourcesOnly))
				if !x.IsZero() && !res.HasErrors() {
					t.Fatal("x bound two closures that hold b compiled from different bytes")
				}
				if got := res.CodeCounts(diag.Error)[diag.E_IMPORT_RESOLVE]; got != 1 || errorCount(res) != 1 {
					t.Errorf("E_IMPORT_RESOLVE reported %d times among %d errors, want the one refusal alone: %s", got, errorCount(res), issueLines(res))
				}
				want := "import " + strconv.Quote(row.refused) + " holds "
				if lines := issueLines(res); !strings.Contains(lines, want) || !strings.Contains(lines, "b.yammm") ||
					!strings.Contains(lines, "as a different compile than the one this load already holds") {
					t.Errorf("the refusal does not name the second import %q, the member b.yammm and the disagreement: %s", row.refused, lines)
				}
				assertResolutionShape(t, res)
			})
		}
	}
}

// TestLoadSourcesWithEntry_InheritsThroughAMemberItNeverImports holds a load
// that imports b, a member of a registered closure, where b.B extends c.C and c
// is a member the load never imports, to X extends b.B carrying c.C's property.
// The bound closure's own links reach c; the load needs no entry of its own for it.
func TestLoadSourcesWithEntry_InheritsThroughAMemberItNeverImports(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSchema(t, dir, "c.yammm", "schema \"c\"\n\ntype C {\n\tid String primary\n\textra String\n}\n")
	writeSchema(t, dir, "b.yammm", "schema \"b\"\n\nimport \"./c\" as c\n\ntype B extends c.C {\n\tname String\n}\n")
	writeSchema(t, dir, "a.yammm", "schema \"a\"\n\nimport \"./b\" as b\n\ntype A {\n\tid String primary\n\t--> TO_B (one) b.B\n}\n")
	a, res := schema.Load(t.Context(), filepath.Join(dir, "a.yammm"), schema.WithModuleRoot(dir))
	if a.IsZero() {
		t.Fatalf("load a: %s", issueLines(res))
	}
	reg := schema.NewRegistry()
	if err := reg.Register(a); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "x.yammm")
	x, res := schema.LoadSourcesWithEntry(t.Context(),
		map[string][]byte{key: []byte("schema \"x\"\n\nimport \"./b\" as b\n\ntype X extends b.B {\n\tmine String\n}\n")},
		key, dir, schema.WithRegistry(reg), schema.WithSourcesOnly(true))
	if x.IsZero() || res.HasErrors() {
		t.Fatalf("load x: %s", issueLines(res))
	}
	xt, ok := x.Type("X")
	if !ok {
		t.Fatal("x declares no type X")
	}
	got := map[string]bool{}
	for p := range xt.AllProperties() {
		got[p.Name()] = true
	}
	for _, want := range []string{"id", "extra", "name", "mine"} {
		if !got[want] {
			t.Errorf("X lacks the inherited property %q; it has %v", want, got)
		}
	}
}

func errorCount(res diag.Result) int {
	n := 0
	for _, c := range res.CodeCounts(diag.Error) {
		n += c
	}
	return n
}

// TestLoadSourcesWithEntry_BindsTwoOwnersThatAgree holds a load that imports
// two registered schemas whose closures hold one source compiled twice from the
// same bytes to both bindings: two compiles of one content are one source.
func TestLoadSourcesWithEntry_BindsTwoOwnersThatAgree(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	load := func(name string) schema.Schema {
		t.Helper()
		s, res := schema.Load(t.Context(), filepath.Join(dir, name), schema.WithModuleRoot(dir))
		if s.IsZero() {
			t.Fatalf("load %s: %s", name, issueLines(res))
		}
		return s
	}
	writeSchema(t, dir, "b.yammm", "schema \"b\"\n\ntype B {\n\tid String primary\n}\n")
	writeSchema(t, dir, "a.yammm", "schema \"a\"\n\nimport \"./b\" as b\n\ntype A {\n\tid String primary\n\t--> TO_B (one) b.B\n}\n")
	writeSchema(t, dir, "d.yammm", "schema \"d\"\n\nimport \"./b\" as b\n\ntype D {\n\tid String primary\n\t--> TO_B (one) b.B\n}\n")
	sa, sd := load("a.yammm"), load("d.yammm")
	if importedSchema(sa, "b") == importedSchema(sd, "b") {
		t.Fatal("the two loads share one compile of b, so this test no longer probes two compiles")
	}
	reg := schema.NewRegistry()
	for _, s := range []schema.Schema{sa, sd} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	key := filepath.Join(dir, "x.yammm")
	x, res := schema.LoadSourcesWithEntry(t.Context(),
		map[string][]byte{key: []byte("schema \"x\"\n\nimport \"./a\" as a\nimport \"./d\" as d\n\ntype X {\n\tid String primary\n\t--> TO_A (one) a.A\n\t--> TO_D (one) d.D\n}\n")},
		key, dir, schema.WithRegistry(reg), schema.WithSourcesOnly(true))
	if x.IsZero() || res.HasErrors() {
		t.Fatalf("load x: %s", issueLines(res))
	}
	if importedSchema(x, "a") != sa || importedSchema(x, "d") != sd {
		t.Error("x does not bind both registered owners")
	}
}

// TestLoadSourcesWithEntry_RefusesADisagreeingOwnerUnderEveryAlias holds a
// refused owner refused again under a second alias, the second declaration
// drawing a duplicate as a compile failure's does, and the same for a refused
// member.
func TestLoadSourcesWithEntry_RefusesADisagreeingOwnerUnderEveryAlias(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sa, sd := disagreeingOwners(t, dir)
	rows := []struct{ name, x string }{
		{"the owner twice", "schema \"x\"\n\nimport \"./a\" as a\nimport \"./d\" as d\nimport \"./d\" as d2\n\ntype X {\n\tid String primary\n\t--> TO_A (one) a.A\n}\n"},
		{"the member twice", "schema \"x\"\n\nimport \"./b\" as b\nimport \"./b\" as b2\n\ntype X {\n\tid String primary\n}\n"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			reg := schema.NewRegistry()
			for _, s := range []schema.Schema{sa, sd} {
				if err := reg.Register(s); err != nil {
					t.Fatal(err)
				}
			}
			key := filepath.Join(dir, "x.yammm")
			x, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{key: []byte(row.x)}, key, dir,
				schema.WithRegistry(reg), schema.WithSourcesOnly(true))
			if !x.IsZero() && !res.HasErrors() {
				t.Fatal("x loaded with a refused schema under a second alias")
			}
			counts := res.CodeCounts(diag.Error)
			if counts[diag.E_IMPORT_RESOLVE] != 2 || counts[diag.E_DUPLICATE_IMPORT] != 1 {
				t.Errorf("want one refusal per declaration and one duplicate, got %v: %s", counts, issueLines(res))
			}
		})
	}
}

// TestLoadSourcesWithEntry_RefusesAnImportWhoseSourceThisLoadHoldsDifferently
// holds a load that already holds bytes for a source — as its entry, or handed
// in by the caller — to a refusal of an import whose registered closure compiled
// that source from other bytes: the registry assumes its files do not change,
// and the disagreement is reported at the import, whether or not the entry was
// ever registered.
func TestLoadSourcesWithEntry_RefusesAnImportWhoseSourceThisLoadHoldsDifferently(t *testing.T) {
	t.Parallel()

	const (
		b1 = "schema \"b\"\n\ntype B {\n\tid String primary\n}\n"
		b2 = "schema \"b\"\n\nimport \"./a\" as a\n\ntype B {\n\tid String primary\n\tname String\n\t--> TO_A (one) a.A\n}\n"
		b3 = "schema \"b\"\n\ntype B {\n\tid String primary\n\tname String\n}\n"
	)
	dir := t.TempDir()
	writeSchema(t, dir, "b.yammm", b1)
	writeSchema(t, dir, "a.yammm", "schema \"a\"\n\nimport \"./b\" as b\n\ntype A {\n\tid String primary\n\t--> TO_B (one) b.B\n}\n")
	a, res := schema.Load(t.Context(), filepath.Join(dir, "a.yammm"), schema.WithModuleRoot(dir))
	if a.IsZero() {
		t.Fatalf("load a: %s", issueLines(res))
	}
	bKey, xKey := filepath.Join(dir, "b.yammm"), filepath.Join(dir, "x.yammm")
	rows := []struct {
		name    string
		sources map[string][]byte
		entry   string
	}{
		{"the entry itself", map[string][]byte{bKey: []byte(b2)}, bKey},
		{"a source the caller handed in", map[string][]byte{
			bKey: []byte(b3),
			xKey: []byte("schema \"x\"\n\nimport \"./b\" as b\n\ntype X {\n\tid String primary\n\t--> TO_B (one) b.B\n}\n"),
		}, xKey},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			reg := schema.NewRegistry()
			if err := reg.Register(a); err != nil {
				t.Fatal(err)
			}
			x, res := schema.LoadSourcesWithEntry(t.Context(), row.sources, row.entry, dir,
				schema.WithRegistry(reg), schema.WithSourcesOnly(true))
			if !x.IsZero() && !res.HasErrors() {
				t.Fatal("the load bound a compile of b beside other bytes for b")
			}
			if got := res.CodeCounts(diag.Error)[diag.E_LOAD_SOURCE_CHANGED]; got != 1 {
				t.Errorf("E_LOAD_SOURCE_CHANGED reported %d times, want 1: %s", got, issueLines(res))
			}
			if lines := issueLines(res); !strings.Contains(lines, "b.yammm") ||
				!strings.Contains(lines, "from different bytes than this load holds for it") {
				t.Errorf("the refusal does not name b.yammm and the disagreement: %s", lines)
			}
		})
	}
}

// TestLoadSourcesWithEntry_APartInheritsAnAssociationPastAMemberItNeverImports
// holds a part type that extends b.B, where b.B extends c.C and c is reachable
// only inside a registered closure, so one of the part's ancestors does not
// resolve in this load. The part still draws its one refusal.
func TestLoadSourcesWithEntry_APartInheritsAnAssociationPastAMemberItNeverImports(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSchema(t, dir, "c.yammm", "schema \"c\"\n\ntype C {\n\tid String primary\n}\n")
	writeSchema(t, dir, "b.yammm", "schema \"b\"\n\nimport \"./c\" as c\n\ntype Other {\n\tid String primary\n}\n\ntype B extends c.C {\n\t--> TO_OTHER (one) Other\n}\n")
	writeSchema(t, dir, "a.yammm", "schema \"a\"\n\nimport \"./b\" as b\n\ntype A {\n\tid String primary\n\t--> TO_B (one) b.B\n}\n")
	a, res := schema.Load(t.Context(), filepath.Join(dir, "a.yammm"), schema.WithModuleRoot(dir))
	if a.IsZero() {
		t.Fatalf("load a: %s", issueLines(res))
	}
	reg := schema.NewRegistry()
	if err := reg.Register(a); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "x.yammm")
	x, res := schema.LoadSourcesWithEntry(t.Context(),
		map[string][]byte{key: []byte("schema \"x\"\n\nimport \"./b\" as b\n\npart type X extends b.B {\n\tmine String\n}\n")},
		key, dir, schema.WithRegistry(reg), schema.WithSourcesOnly(true))
	if !x.IsZero() {
		t.Fatal("a part type carrying an association loaded")
	}
	if n := countCode(res, diag.E_INVALID_ASSOCIATION_TARGET); n != 1 {
		t.Errorf("E_INVALID_ASSOCIATION_TARGET drawn %d times, want 1: %s", n, issueLines(res))
	}
}

// builtAt builds a sourceless schema under the identity of the file name in dir,
// with one concrete type.
func builtAt(t *testing.T, dir, file, schemaName, typeName string) schema.Schema {
	t.Helper()
	writeSchema(t, dir, file, "schema \""+schemaName+"\"\n")
	s, res := schema.NewBuilder().WithName(schemaName).
		WithSourceID(location.MustSourceIDFromPath(filepath.Join(dir, file))).
		AddType(typeName).WithPrimaryKey("id", schema.NewStringConstraint()).Done().
		Build()
	if s.IsZero() {
		t.Fatalf("build %s: %s", schemaName, issueLines(res))
	}
	return s
}

// TestLoadSourcesWithEntry_RefusesARegisteredCompileOfItsOwnSource holds a load whose
// source a registered Builder-built compile also names, reached as the import itself,
// as a member of its closure, or as a member of a closure the load binds. A text that
// imports itself is a cycle. A text that does not holds no cycle, and the registered
// compile, which carries no sources, is a changed source. So is an import the load
// holds bytes for.
func TestLoadSourcesWithEntry_RefusesARegisteredCompileOfItsOwnSource(t *testing.T) {
	t.Parallel()

	built := func(t *testing.T, dir string) (direct, holder *schema.Registry) {
		t.Helper()
		a := builtAt(t, dir, "a.yammm", "a", "Old")
		direct = schema.NewRegistry()
		if err := direct.Register(a); err != nil {
			t.Fatal(err)
		}
		writeSchema(t, dir, "b.yammm", "schema \"b\"\n")
		b, res := schema.NewBuilder().WithName("b").
			WithSourceID(location.MustSourceIDFromPath(filepath.Join(dir, "b.yammm"))).
			WithRegistry(direct).
			WithImportResolver(func(path string) (location.SourceID, bool) { return a.SourceID(), path == "./a" }).
			AddImport("./a", "a").
			AddType("R").WithPrimaryKey("id", schema.NewStringConstraint()).Done().
			Build()
		if b.IsZero() {
			t.Fatal(issueLines(res))
		}
		holder = schema.NewRegistry()
		if err := holder.Register(b); err != nil {
			t.Fatal(err)
		}
		return direct, holder
	}
	const selfImport = "schema \"a\"\n\nimport \"./a\" as self\n\ntype T extends self.Old {\n\tid String primary\n}\n"
	const holderImport = "schema \"a\"\n\nimport \"./b\" as b\n\ntype T {\n\tid String primary\n}\n"

	for _, c := range []struct {
		name   string
		source string
		extra  map[string][]byte
		pick   func(direct, holder *schema.Registry) *schema.Registry
		want   diag.Code
	}{
		{"the text imports itself and the registry holds the compile", selfImport, nil, func(d, _ *schema.Registry) *schema.Registry { return d }, diag.E_IMPORT_CYCLE},
		{"the text imports itself and a registered closure holds the compile", selfImport, nil, func(_, h *schema.Registry) *schema.Registry { return h }, diag.E_IMPORT_CYCLE},
		{"the imported schema's closure holds the compile", holderImport, nil, func(_, h *schema.Registry) *schema.Registry { return h }, diag.E_LOAD_SOURCE_CHANGED},
		{
			"the load holds bytes for the imported compile", holderImport,
			map[string][]byte{"b.yammm": []byte("schema \"b\"\n")},
			func(_, h *schema.Registry) *schema.Registry { return h }, diag.E_LOAD_SOURCE_CHANGED,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			direct, holder := built(t, dir)
			key := filepath.Join(dir, "a.yammm")
			sources := map[string][]byte{key: []byte(c.source)}
			for name, content := range c.extra {
				sources[filepath.Join(dir, name)] = content
			}
			s, res := schema.LoadSourcesWithEntry(t.Context(), sources,
				key, dir, schema.WithRegistry(c.pick(direct, holder)), schema.WithSourcesOnly(true))
			if !s.IsZero() {
				t.Fatalf("the load returned a schema: %s", issueLines(res))
			}
			found := 0
			for issue := range res.Issues() {
				if issue.Code() == c.want {
					found++
				}
			}
			if found != 1 {
				t.Errorf("%s reported %d times, want 1: %s", c.want, found, issueLines(res))
			}
			if c.want == diag.E_LOAD_SOURCE_CHANGED && !strings.Contains(issueLines(res), "with no sources, and this load holds bytes for it") {
				t.Errorf("the refusal does not say the compile carries no sources: %s", issueLines(res))
			}
		})
	}
}

// TestLoadSourcesWithEntry_OriginOfACopyOfACopyIsTheDeclaredProperty holds a merge
// whose survivor is itself a merged copy: the declarations are in c, a member of a
// registered closure the load never imports, so the walk reaches b.M's copy first
// and Y's annotation copies it again. Origin still names the declaration in c.
func TestLoadSourcesWithEntry_OriginOfACopyOfACopyIsTheDeclaredProperty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSchema(t, dir, "c.yammm", "schema \"c\"\n\nabstract type N1 {\n\tid String primary\n\tx Timestamp\n}\n\nabstract type N2 {\n\tx Timestamp @writeOnce\n}\n")
	writeSchema(t, dir, "b.yammm", "schema \"b\"\n\nimport \"./c\" as c\n\nabstract type M extends c.N1, c.N2 {\n\ty String\n}\n")
	writeSchema(t, dir, "a.yammm", "schema \"a\"\n\nimport \"./b\" as b\n\ntype A extends b.M {\n\tz String\n}\n")
	a, res := schema.Load(t.Context(), filepath.Join(dir, "a.yammm"), schema.WithModuleRoot(dir))
	if a.IsZero() {
		t.Fatalf("load a: %s", issueLines(res))
	}
	reg := schema.NewRegistry()
	if err := reg.Register(a); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "x.yammm")
	x, res := schema.LoadSourcesWithEntry(t.Context(),
		map[string][]byte{key: []byte("schema \"x\"\n\nimport \"./b\" as b\n\nabstract type Y {\n\tx Timestamp @index\n}\n\ntype T extends b.M, Y {\n\tz String\n}\n")},
		key, dir, schema.WithRegistry(reg), schema.WithSourcesOnly(true))
	if x.IsZero() || res.HasErrors() {
		t.Fatalf("load x: %s", issueLines(res))
	}

	property := func(s schema.Schema, typeName string) schema.Property {
		t.Helper()
		typ, ok := s.Type(typeName)
		if !ok {
			t.Fatalf("%s declares no type %s", s.Name(), typeName)
		}
		p, ok := typ.Property("x")
		if !ok {
			t.Fatalf("%s holds no property x", typeName)
		}
		return p
	}
	b := importedSchema(a, "b")
	declared := property(importedSchema(b, "c"), "N1")
	copied := property(b, "M")
	merged := property(x, "T")
	if copied == declared || merged == copied {
		t.Fatal("each merge should carry its own copy, so this test no longer holds a copy of a copy")
	}
	if merged.Origin() != declared {
		t.Error("Origin() on a copy of a copy should return the declared property, not the copy it was merged from")
	}
}

// TestLoadSourcesWithEntry_WalksARegisteredDiamondChainOncePerSchema holds a load
// to one visit per schema when it reads a shared Registry's closures for an import
// the registry does not hold. A chain of diamonds has 2^depth import paths and
// 2*depth schemas, so a walk by path would not finish.
func TestLoadSourcesWithEntry_WalksARegisteredDiamondChainOncePerSchema(t *testing.T) {
	t.Parallel()

	const depth = 64
	dir := t.TempDir()
	writeSchema(t, dir, "l0.yammm", "schema \"l0\"\n\ntype T {\n\tid String primary\n}\n")
	writeSchema(t, dir, "r0.yammm", "schema \"r0\"\n\ntype T {\n\tid String primary\n}\n")
	for i := 1; i <= depth; i++ {
		for _, side := range []string{"l", "r"} {
			writeSchema(t, dir, fmt.Sprintf("%s%d.yammm", side, i), fmt.Sprintf(
				"schema \"%s%d\"\n\nimport \"./l%d\" as l\nimport \"./r%d\" as r\n\ntype T {\n\tid String primary\n}\n",
				side, i, i-1, i-1))
		}
	}
	writeSchema(t, dir, "fresh.yammm", "schema \"fresh\"\n\ntype T {\n\tid String primary\n}\n")

	reg := schema.NewRegistry()
	top, res := schema.Load(t.Context(), filepath.Join(dir, fmt.Sprintf("l%d.yammm", depth)), schema.WithModuleRoot(dir), schema.WithRegistry(reg))
	if top.IsZero() {
		t.Fatalf("load the chain: %s", issueLines(res))
	}

	done := make(chan diag.Result, 1)
	go func() {
		key := filepath.Join(dir, "x.yammm")
		_, res := schema.LoadSourcesWithEntry(context.Background(),
			map[string][]byte{key: []byte("schema \"x\"\n\nimport \"./fresh\" as f\n\ntype T {\n\tid String primary\n}\n")},
			key, dir, schema.WithRegistry(reg))
		done <- res
	}()
	select {
	case res := <-done:
		if res.HasErrors() {
			t.Errorf("load x: %s", issueLines(res))
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the load did not finish walking a registered chain of diamonds")
	}
}
