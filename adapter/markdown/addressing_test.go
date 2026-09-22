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
		"- `--> BUYER (one)` [c.Person](#cperson)",
		"Extends: [Named (base)](#named-base).",
		"| `name` | `String` | from Named (base) |  |",
		`    class c_Person["c.Person"] {`,
		`    class Named__base_["Named (base)"] {`,
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
	assertLines(t, doc, "## Types", "### Types", "- `--> LINK (one)` [Types](#types-1)")
	assertAbsent(t, doc, "](#types)")
}

// TestMarshal_SchemaNameIsWrittenLiterally pins that a schema name renders as
// written wherever the document shows one: Markdown syntax in it is escaped in
// the title, a schema heading, a type's display and a link, and a double quote
// in a diagram label is Mermaid's #quot;.
func TestMarshal_SchemaNameIsWrittenLiterally(t *testing.T) {
	t.Parallel()

	doc := marshalSources(t, map[string]string{
		"entry.yammm": `schema "app*"

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
		"odd.yammm": `schema "co\"m [z](w)*"

type Person {
	id String primary
}
`,
	})

	assertLines(t, doc,
		`# Schema app\*`,
		`## Schema co"m \[z\](w)\*`,
		`### Person (co"m \[z\](w)\*)`,
		"- `--> OWNER (one)` [Person (co\"m \\[z\\](w)\\*)](#person-com-zw)",
		`    class Person__co_m__z__w___["Person (co#quot;m [z](w)*)"] {`,
	)
}

// TestMarshal_DiagramTextIsMermaidText pins that schema-supplied text reaches
// the class diagram only in forms Mermaid's classDiagram lexer reads: a class
// id is ASCII letters, digits and underscores, since Mermaid's \w is ASCII and
// its table of other letters is partial, and a class label writes each
// character Mermaid reads as syntax, a directive or an entity as an entity
// code, as it does the white space in "direction LR", which Mermaid reads as a
// direction statement anywhere on a line outside a class body. An edge label
// writes the
// multiplicity colon as an entity, because Mermaid's label token ends at a
// colon.
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
		"odd.yammm": `schema "x\"y:z;#35;%%{init}%%<b>&é٣ direction LR"

type Person {
	id String primary
}
`,
	})

	assertLines(t, doc,
		"    class Person__x_y_z__35____init____b_____direction_LR_[\"Person (x#quot;y#58;z#59;#35;35#59;#37;#37;{init}#37;#37;#60;b#62;#38;é٣ direction#32;LR)\"] {",
		"    m_Hub --> Person__x_y_z__35____init____b_____direction_LR_ : OWNERS (one#58;many)",
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

// TestMarshal_SchemaNameLineBreakStaysOnOneLine pins that a schema name
// holding a line break — legal in the DSL's string literal — is written with
// the break as its escape, so a heading, a link and a diagram label each stay
// on one line and the link's anchor is the one the heading takes.
func TestMarshal_SchemaNameLineBreakStaysOnOneLine(t *testing.T) {
	t.Parallel()

	doc := marshalSources(t, map[string]string{
		"entry.yammm": `schema "a\nb"

import "common.yammm" as c

type Order extends c.Person {
	id String primary
}
`,
		"common.yammm": `schema "common"

import "base.yammm" as b

type Person extends b.Named {
	pid String
}
`,
		"base.yammm": `schema "ba\nse"

type Named {
	name String primary
}
`,
	})

	assertLines(t, doc,
		`# Schema a\\nb`,
		`## Schema ba\\nse`,
		`### Named (ba\\nse)`,
		`Extends: [Named (ba\\nse)](#named-banse).`,
		"| `name` | `String` | primary, from Named (ba\\\\nse) |  |",
		`    class Named__ba_nse_["Named (ba\nse)"] {`,
	)
}

// TestMarshal_SchemaNameTrailingSpaceReadsAsWritten pins that a schema name
// ending in a space generates, and its heading keeps the space: an ATX heading
// drops trailing spaces, so the generator writes them as character references.
func TestMarshal_SchemaNameTrailingSpaceReadsAsWritten(t *testing.T) {
	t.Parallel()

	doc := marshalSources(t, map[string]string{
		"entry.yammm": `schema "app "

import "mid.yammm" as m

type Car extends m.Hub {
	id String primary
}
`,
		"mid.yammm": `schema "mid"

import "base.yammm" as b

abstract type Hub extends b.P {
	note String
}
`,
		"base.yammm": `schema "base  "

abstract type P {
	tag String
}
`,
	})

	assertLines(t, doc, "# Schema app&#32;", "## Schema base&#32;&#32;")
}

// TestMarshal_PipeInASchemaNameKeepsTheRow pins that a schema name holding a
// pipe, written in a property row's provenance marker, is escaped so the row
// keeps its four cells.
func TestMarshal_PipeInASchemaNameKeepsTheRow(t *testing.T) {
	t.Parallel()

	doc := marshalSources(t, map[string]string{
		"entry.yammm": `schema "app"

import "mid.yammm" as m

type Order extends m.Mid {
	id String primary
}
`,
		"mid.yammm": `schema "mid"

import "base.yammm" as b

abstract type Mid extends b.P {
	note String
}
`,
		"base.yammm": `schema "a|b"

abstract type P {
	tag String
}
`,
	})

	assertLines(t, doc, "| `tag` | `String` | from P (a\\|b) |  |")
}
