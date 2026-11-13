package markdown

import (
	"strings"
	"testing"
)

// marshalSources loads a multi-source schema with entry.yammm as the entry
// point and returns Marshal's document.
func marshalSources(t *testing.T, sources map[string]string) string {
	t.Helper()
	m := make(map[string][]byte, len(sources))
	for name, src := range sources {
		m[name] = []byte(src)
	}
	out, err := Marshal(loadSources(t, m))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(out)
}

// assertLines fails for each want that is not a whole line of doc.
func assertLines(t *testing.T, doc string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains("\n"+doc, "\n"+w+"\n") {
			t.Errorf("document has no line %q:\n%s", w, doc)
		}
	}
}

// assertAbsent fails for each unwanted substring doc holds.
func assertAbsent(t *testing.T, doc string, unwanted ...string) {
	t.Helper()
	for _, u := range unwanted {
		if strings.Contains(doc, u) {
			t.Errorf("document holds %q:\n%s", u, doc)
		}
	}
}

// TestMarshal_ImportedTypeIsNamedByTheEntrysTag pins the display rule: a type
// the entry imports directly is written as the entry's own tag (alias.Name),
// which is the name a data file uses, and a type the entry reaches only
// through another import is written "Name (schema)", never as a tag. Headings,
// links, provenance markers and diagram labels all read the one rule.
func TestMarshal_ImportedTypeIsNamedByTheEntrysTag(t *testing.T) {
	t.Parallel()

	doc := marshalSources(t, map[string]string{
		"entry.yammm": `schema "app"

import "common.yammm" as c

type Order {
	id String primary
	--> BUYER (one) c.Person
}
`,
		"common.yammm": `schema "common"

import "base.yammm" as b

type Person extends b.Named {
	id String primary
}
`,
		"base.yammm": `schema "base"

abstract type Named {
	name String
}
`,
	})

	assertLines(t, doc,
		"### c.Person",
		"### Named (base)",
		"-   `--> BUYER (one)` [c.Person](#cperson)",
		"Extends: [Named (base)](#named-base).",
		"| `name` | `String` | from Named (base) |  |",
		`    class c_Person["c.Person"] {`,
		`    class Named__base_["Named#32;#40;base#41;"] {`,
		"    Order --> c_Person : BUYER (one)",
		"    Named__base_ <|-- c_Person",
	)
	assertAbsent(t, doc, "common.Person", "base.Named", "b.Named")
}

// TestMarshal_ExtendsResolvesInTheDeclaringSchema pins that an extends
// reference is resolved in the schema that declares the type. Here the entry
// and its import each declare a Shared, and T's bare "Shared" names the
// entry's own.
func TestMarshal_ExtendsResolvesInTheDeclaringSchema(t *testing.T) {
	t.Parallel()

	doc := marshalSources(t, map[string]string{
		"entry.yammm": `schema "main"

import "b.yammm" as b

abstract type Shared {
	tag String
}

type T extends b.A, Shared {
	id String primary
}
`,
		"b.yammm": `schema "b"

abstract type Shared {
	z String
}

abstract type A extends Shared {
	y String
}
`,
	})

	assertLines(t, doc,
		"Extends: [b.A](#ba), [Shared](#shared).",
		"    Shared <|-- T",
		"    b_Shared <|-- b_A",
	)
	assertAbsent(t, doc, "b_Shared <|-- T")
}

// TestMarshal_ExtendsLinksAQualifiedAncestorAParentAlsoReaches pins that an
// extends reference written with the entry's tag links to its type's section
// when the type is also an ancestor of another parent: b.Shared is A's parent,
// and T names it again beside b.A.
func TestMarshal_ExtendsLinksAQualifiedAncestorAParentAlsoReaches(t *testing.T) {
	t.Parallel()

	doc := marshalSources(t, map[string]string{
		"entry.yammm": `schema "main"

import "b.yammm" as b

type T extends b.A, b.Shared {
	id String primary
}
`,
		"b.yammm": `schema "b"

abstract type Shared {
	z String
}

abstract type A extends Shared {
	y String
}
`,
	})

	assertLines(t, doc,
		"Extends: [b.A](#ba), [b.Shared](#bshared).",
		"    b_A <|-- T",
		"    b_Shared <|-- T",
	)
}

// TestMarshal_TypeNamedLikeASectionLinksItsOwnHeading pins that a type whose
// heading slugs like a section heading of the document is linked by the
// anchor GitHub gives its own heading: "## Types" comes first and takes
// #types, so the type Types takes #types-1.
func TestMarshal_TypeNamedLikeASectionLinksItsOwnHeading(t *testing.T) {
	t.Parallel()

	out, err := Marshal(loadSchema(t, `schema "main"

type Types {
	id String primary
}

type Other {
	id String primary
	--> LINK (one) Types
}
`))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	doc := string(out)
	assertLines(t, doc, "## Types", "### Types", "-   `--> LINK (one)` [Types](#types-1)")
	assertAbsent(t, doc, "](#types)")
}

// TestMarshal_SchemaNameIsWrittenLiterally pins that a schema name renders as
// written wherever the document shows one: an underscore that could open or
// close emphasis is escaped in the title, a schema heading, a type's display
// and a link, and is an entity code in a diagram label.
func TestMarshal_SchemaNameIsWrittenLiterally(t *testing.T) {
	t.Parallel()

	doc := marshalSources(t, map[string]string{
		"entry.yammm": `schema "app_"

import "mid.yammm" as m

type Car {
	id String primary
	--> AT (one) m.Hub
}
`,
		"mid.yammm": `schema "mid"

import "odd.yammm" as o

type Hub {
	id String primary
	--> OWNER (one) o.Person
}
`,
		"odd.yammm": `schema "co_m__z_"

type Person {
	id String primary
}
`,
	})

	assertLines(t, doc,
		`# Schema app\_`,
		`## Schema co_m\_\_z\_`,
		`### Person (co_m\_\_z\_)`,
		"-   `--> OWNER (one)` [Person (co_m\\_\\_z\\_)](#person-co_m__z_)",
		`    class Person__co_m__z__["Person#32;#40;co_m#95;#95;z#95;#41;"] {`,
	)
}

// TestMarshal_DiagramTextIsMermaidText pins that schema-supplied text reaches
// the class diagram only in forms Mermaid's classDiagram lexer reads: a class
// id breaks every "direction", and a class label writes every character but an
// ASCII letter or digit, a dot, and an underscore between two ASCII letters or
// digits as an entity code. An edge label writes the multiplicity colon as an
// entity, because Mermaid's label token ends at a colon.
func TestMarshal_DiagramTextIsMermaidText(t *testing.T) {
	t.Parallel()

	doc := marshalSources(t, map[string]string{
		"entry.yammm": `schema "app"

import "mid.yammm" as m

type Car {
	id String primary
	--> AT (one) m.Hub
}
`,
		"mid.yammm": `schema "mid"

import "odd.yammm" as o

type Hub {
	id String primary
	--> OWNERS (one:many) o.Person
}
`,
		"odd.yammm": `schema "x_y__direction_LR"

type Person {
	id String primary
}
`,
	})

	assertLines(t, doc,
		"    class Person__x_y__direc_tion_LR_[\"Person#32;#40;x_y#95;#95;direction_LR#41;\"] {",
		"    m_Hub --> Person__x_y__direc_tion_LR_ : OWNERS (one#58;many)",
	)
}

// TestMarshal_MermaidClassIdsAreUnique pins that two types whose displays
// sanitize to one Mermaid id stay two classes: the entry's A_B keeps A_B and
// the imported A.B takes A_B_2, and each edge reaches its own class.
func TestMarshal_MermaidClassIdsAreUnique(t *testing.T) {
	t.Parallel()

	doc := marshalSources(t, map[string]string{
		"entry.yammm": `schema "app"

import "a.yammm" as A

type A_B {
	id String primary
}

type Car {
	id String primary
	--> OWNER (one) A_B
	--> PART (one) A.B
}
`,
		"a.yammm": `schema "a"

type B {
	id String primary
}
`,
	})

	assertLines(t, doc,
		"    class A_B {",
		`    class A_B_2["A.B"] {`,
		"    Car --> A_B : OWNER (one)",
		"    Car --> A_B_2 : PART (one)",
	)
}

// TestMarshal_TypeCellShowsTheConstraintAsDeclared pins that a property's Type
// cell renders the constraint's DSL form byte for byte: a code span doubles no
// backslash, and a value a code span cannot carry exactly takes an
// entity-escaped <code> element.
func TestMarshal_TypeCellShowsTheConstraintAsDeclared(t *testing.T) {
	t.Parallel()

	out, err := Marshal(loadSchema(t, `schema "main"

type T {
	id String primary
	code Pattern["^\\d+$"]
	pipe Pattern["a|b\\|c"]
	kind Enum["a\"b", "x*y*z"]
}
`))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	assertLines(t, string(out),
		"| `code` | `Pattern[\"^\\\\d+$\"]` |  |  |",
		"| `pipe` | <code>Pattern&#91;\"a\\|b&#92;&#92;\\|c\"&#93;</code> |  |  |",
		"| `kind` | `Enum[\"a\\\"b\", \"x*y*z\"]` |  |  |",
	)
}

// TestMarshal_AnchorsAreGitHubs pins an anchor against the HTML GitHub's
// renderer gives Marshal's document: a heading nested in a doc comment's raw
// heading takes no anchor, so <h2><div><h3>Nested</h3>tail</div></h2> before
// "### Nested" gives the ids nestedtail and nested.
func TestMarshal_AnchorsAreGitHubs(t *testing.T) {
	t.Parallel()

	nested := marshalString(t, "schema \"s\"\n/* <h2><div><h3>Nested</h3>tail</div></h2> */\ntype A {\n  id String primary\n  --> TO (one) Nested\n}\ntype Nested {\n  id String primary\n}\n")
	assertLines(t, nested, "-   `--> TO (one)` [Nested](#nested)")
	assertAbsent(t, nested, "#nested-1")
}

// TestMarshal_ATypesOwnDocHeadingTakesTheAnchorAfterIt pins that an outline
// heading the read finds takes its anchor at its place: a type whose doc
// comment holds a heading of the type's own name keeps the anchor its slug
// gives, and the doc comment's heading takes the next.
func TestMarshal_ATypesOwnDocHeadingTakesTheAnchorAfterIt(t *testing.T) {
	t.Parallel()

	doc := marshalString(t, "schema \"s\"\ntype A {\n  id String primary\n  --> TO (one) B\n}\n/* # B */\ntype B {\n  id String primary\n}\n")
	assertLines(t, doc, "-   `--> TO (one)` [B](#b)")
	assertAbsent(t, doc, "(#b-1)")
}

// TestMarshal_AHeadingTheTreeLosesTakesNoAnchorATreeHeadingTakes pins that a
// heading of the generator's that GitHub's tree loses moves no anchor of a
// heading the tree holds: the lost Ob gives up "ob", so o.B, which GitHub
// anchors #ob, is linked there.
func TestMarshal_AHeadingTheTreeLosesTakesNoAnchorATreeHeadingTakes(t *testing.T) {
	t.Parallel()

	doc := marshalSources(t, map[string]string{
		"entry.yammm": "schema \"s\"\n\nimport \"o.yammm\" as o\n\n/* <div>\n<select><option x=\" */\ntype A {\n\tid String primary\n}\n\ntype Ob {\n\tid String primary\n}\n",
		"o.yammm":     "/* <div>\n<i x=\"1\"><input> */\nschema \"o\"\n\ntype B {\n\tid String primary\n\t--> LOOP (one) B\n}\n",
	})
	assertLines(t, doc, "-   `--> LOOP (one)` [o.B](#ob)")
}
