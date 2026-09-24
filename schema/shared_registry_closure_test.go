package schema_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

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
	if a == nil {
		t.Fatalf("load a: %v", res)
	}
	b := importedSchema(a, "b")
	if b == nil {
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
			if x == nil || res.HasErrors() {
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
			load := func(name string) *schema.Schema {
				t.Helper()
				s, res := schema.Load(t.Context(), filepath.Join(dir, name), schema.WithModuleRoot(dir))
				if s == nil {
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
			for _, s := range []*schema.Schema{sa, sd} {
				if err := reg.Register(s); err != nil {
					t.Fatal(err)
				}
			}
			key := filepath.Join(dir, "x.yammm")
			got, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{key: []byte(x)}, key, dir,
				schema.WithRegistry(reg), schema.WithSourcesOnly(true))
			bound := got != nil && !res.HasErrors()
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

func importedSchema(s *schema.Schema, name string) *schema.Schema {
	for _, imp := range s.ImportsSlice() {
		if sub := imp.Schema(); sub != nil && sub.Name() == name {
			return sub
		}
	}
	return nil
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
	if a == nil {
		t.Fatalf("load a: %s", issueLines(res))
	}
	c := importedSchema(importedSchema(a, "b"), "c")
	if c == nil {
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
	if x == nil || res.HasErrors() {
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
		if s == nil {
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
	if x != nil && !res.HasErrors() {
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
	if r == nil {
		t.Fatalf("load r: %s", issueLines(res))
	}
	writeSchema(t, dir, "b.yammm", b1)
	b, res := schema.Load(t.Context(), filepath.Join(dir, "b.yammm"), schema.WithModuleRoot(dir))
	if b == nil {
		t.Fatalf("load b: %s", issueLines(res))
	}
	if inClosure := importedSchema(r, "b"); inClosure == b {
		t.Fatal("r's closure holds the same compiled schema as the direct registration")
	}

	reg := schema.NewRegistry()
	for _, s := range []*schema.Schema{b, r} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	key := filepath.Join(dir, "x.yammm")
	x, res := schema.LoadSourcesWithEntry(t.Context(),
		map[string][]byte{key: []byte("schema \"x\"\n\nimport \"./b\" as b\n\ntype X {\n\tid String primary\n\t--> TO_B (one) b.B\n}\n")},
		key, dir, schema.WithRegistry(reg), schema.WithSourcesOnly(true))
	if x == nil || res.HasErrors() {
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
	if m == nil {
		t.Fatalf("build m: %s", issueLines(res))
	}
	if m.Sources() != nil {
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
		if r == nil {
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
	if x == nil || res.HasErrors() {
		t.Fatalf("load x: %s", issueLines(res))
	}
	if got := importedSchema(x, "m"); got != m {
		t.Errorf("x binds m as %p, want the one Builder-built schema %p", got, m)
	}
}

// disagreeingOwners loads a and d, each importing ./b, where a's b and d's b
// were compiled from different bytes, and returns them for a shared Registry.
func disagreeingOwners(t *testing.T, dir string) (a, d *schema.Schema) {
	t.Helper()
	const (
		b1 = "schema \"b\"\n\ntype B {\n\tid String primary\n}\n"
		b2 = "schema \"b\"\n\ntype B {\n\tid String primary\n\tname String\n}\n"
	)
	load := func(name string) *schema.Schema {
		t.Helper()
		s, res := schema.Load(t.Context(), filepath.Join(dir, name), schema.WithModuleRoot(dir))
		if s == nil {
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
				for _, s := range []*schema.Schema{sa, sd} {
					if err := reg.Register(s); err != nil {
						t.Fatal(err)
					}
				}
				key := filepath.Join(dir, "x.yammm")
				x, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{key: []byte(row.x)}, key, dir,
					schema.WithRegistry(reg), schema.WithSourcesOnly(sourcesOnly))
				if x != nil && !res.HasErrors() {
					t.Fatalf("x bound b as %p; want the import refused", importedSchema(x, "b"))
				}
				if got := res.CodeCounts(diag.Error)[diag.E_IMPORT_RESOLVE]; got != 1 || errorCount(res) != 1 {
					t.Errorf("E_IMPORT_RESOLVE reported %d times among %d errors, want the one refusal alone: %s", got, errorCount(res), issueLines(res))
				}
				if !strings.Contains(issueLines(res), "two compiles with different bytes") {
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
				for _, s := range []*schema.Schema{sa, sd} {
					if err := reg.Register(s); err != nil {
						t.Fatal(err)
					}
				}
				key := filepath.Join(dir, "x.yammm")
				x, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{key: []byte(row.x)}, key, dir,
					schema.WithRegistry(reg), schema.WithSourcesOnly(sourcesOnly))
				if x != nil && !res.HasErrors() {
					t.Fatal("x bound two closures that hold b compiled from different bytes")
				}
				if got := res.CodeCounts(diag.Error)[diag.E_IMPORT_RESOLVE]; got != 1 || errorCount(res) != 1 {
					t.Errorf("E_IMPORT_RESOLVE reported %d times among %d errors, want the one refusal alone: %s", got, errorCount(res), issueLines(res))
				}
				want := "import " + strconv.Quote(row.refused) + " holds "
				if lines := issueLines(res); !strings.Contains(lines, want) || !strings.Contains(lines, "b.yammm") ||
					!strings.Contains(lines, "compiled from different bytes than the compile this load already holds") {
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
	if a == nil {
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
	if x == nil || res.HasErrors() {
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
	load := func(name string) *schema.Schema {
		t.Helper()
		s, res := schema.Load(t.Context(), filepath.Join(dir, name), schema.WithModuleRoot(dir))
		if s == nil {
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
	for _, s := range []*schema.Schema{sa, sd} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	key := filepath.Join(dir, "x.yammm")
	x, res := schema.LoadSourcesWithEntry(t.Context(),
		map[string][]byte{key: []byte("schema \"x\"\n\nimport \"./a\" as a\nimport \"./d\" as d\n\ntype X {\n\tid String primary\n\t--> TO_A (one) a.A\n\t--> TO_D (one) d.D\n}\n")},
		key, dir, schema.WithRegistry(reg), schema.WithSourcesOnly(true))
	if x == nil || res.HasErrors() {
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
			for _, s := range []*schema.Schema{sa, sd} {
				if err := reg.Register(s); err != nil {
					t.Fatal(err)
				}
			}
			key := filepath.Join(dir, "x.yammm")
			x, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{key: []byte(row.x)}, key, dir,
				schema.WithRegistry(reg), schema.WithSourcesOnly(true))
			if x != nil && !res.HasErrors() {
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
	if a == nil {
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
			if x != nil && !res.HasErrors() {
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
