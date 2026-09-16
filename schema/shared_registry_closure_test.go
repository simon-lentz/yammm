package schema_test

import (
	"fmt"
	"os"
	"path/filepath"
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
// schema a load can choose between, so an import of that source is read, and a
// load restricted to its own sources refuses it.
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
