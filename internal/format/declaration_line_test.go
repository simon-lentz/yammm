package format

import (
	"strings"
	"testing"
)

// TestSource_EachDeclarationBeginsALine pins that a file-scope declaration or a
// type-body member written on another's line is moved to a line of its own, and
// that the output is a fixed point. A type left on the schema header's line took
// the blank line phase 2 inserts after the header into its own wrapped enum on a
// second pass, and two members on one line left phase 4's columns unaligned.
func TestSource_EachDeclarationBeginsALine(t *testing.T) {
	t.Parallel()
	long := func(c string, n int) string { return `"` + strings.Repeat(c, n) + `"` }
	for _, tt := range []struct {
		name, in, want string
	}{
		{
			"a wrapped enum on the header's line",
			`schema "s" type A = Enum[` + long("a", 30) + `, ` + long("b", 39) + `, ` + long("c", 33) + "]\n",
			"schema \"s\"\n\ntype A = Enum[\n\t" + long("a", 30) + ",\n\t" + long("b", 39) + ",\n\t" + long("c", 33) + ",\n]\n",
		},
		{
			"two aliases on the header's line",
			"schema \"s\" type A = Integer type Bb = String\n",
			"schema \"s\"\n\ntype A  = Integer\ntype Bb = String\n",
		},
		{
			"an import and a type on the header's line",
			"schema \"s\" import \"./x\" as x type T {}\n",
			"schema \"s\"\n\nimport \"./x\" as x\n\ntype T {\n}\n",
		},
		{
			"a documented type after an alias",
			"schema \"s\"\n\ntype A = Integer /** doc */ abstract type T {}\n",
			"schema \"s\"\n\ntype A = Integer\n/** doc */\nabstract type T {\n}\n",
		},
		{
			"a multi-line doc comment on the header's line",
			"schema \"s\" /* a\n b */ type A = Integer\n",
			"schema \"s\"\n\n/* a\n b */\ntype A = Integer\n",
		},
		{
			"a multi-line doc comment on the last import's line",
			"schema \"s\"\n\nimport \"./x\" as x /* a\nb */ type A = Integer\n",
			"schema \"s\"\n\nimport \"./x\" as x\n\n/* a\nb */\ntype A = Integer\n",
		},
		{
			"a line comment after a declaration",
			"schema \"s\"\n\ntype A = Integer // c\ntype B = String\n",
			"schema \"s\"\n\ntype A = Integer // c\ntype B = String\n",
		},
		{
			"two properties on one line",
			"schema \"s\"\n\ntype T {\n\ta Integer bb String\n\tc Integer\n}\n",
			"schema \"s\"\n\ntype T {\n\ta  Integer\n\tbb String\n\tc  Integer\n}\n",
		},
		{
			"a property and a relation with an edge property",
			"schema\"0\"type A{a A-->A(one)A{a A b B}}",
			"schema \"0\"\n\ntype A {\n\ta A\n\t--> A (one) A {\n\t\ta A\n\t\tb B\n\t}\n}\n",
		},
		{
			"a type annotation after a property",
			"schema \"s\"\n\ntype T {\n\ta Integer @@index(a)\n}\n",
			"schema \"s\"\n\ntype T {\n\ta Integer\n\t@@index(a)\n}\n",
		},
		{
			"an annotated property, a documented composition, a type annotation and an invariant",
			"schema \"s\"\n\ntype T {\n\ta Integer @index --> R (one) T /** d */ *-> P (many) T\n\t@@index(a) ! \"m\" a > 0 b String\n}\n",
			"schema \"s\"\n\ntype T {\n\ta Integer @index\n\t--> R (one) T  /** d */\n\t*-> P (many) T\n\t@@index(a)\n\t! \"m\" a > 0\n\tb String\n}\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := formatText(tt.in)
			if err != nil {
				t.Fatalf("Source: %v", err)
			}
			if got != tt.want {
				t.Errorf("Source =\n%s\nwant\n%s", got, tt.want)
			}
			again, err := formatText(got)
			if err != nil {
				t.Fatalf("Source on its own output: %v", err)
			}
			if again != got {
				t.Errorf("Source is not a fixed point: a second pass gives\n%s\nfrom\n%s", again, got)
			}
		})
	}
}
