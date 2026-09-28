package markdown

import (
	"regexp"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// diagramIDs matches the class ids a class line or an edge line of the diagram
// names.
var diagramIDs = regexp.MustCompile(`(?m)^    (?:class ([A-Za-z0-9_]+)|([A-Za-z0-9_]+) (?:<\|--|-->|\*--) ([A-Za-z0-9_]+))`)

// TestMarshal_NoClassIDHoldsDirection pins that no class id holds
// "direction": Mermaid reads a line ending in "direction", the line break and
// a next line starting LR as a direction statement, which here would turn the
// diagram left to right and drop both edges. Each such type takes the
// labelled form, so its name still reads as written.
func TestMarshal_NoClassIDHoldsDirection(t *testing.T) {
	t.Parallel()

	doc := string(mustMarshal(t, loadTestdata(t, "mermaid/fixtures/redirection")))
	for _, m := range diagramIDs.FindAllStringSubmatch(doc, -1) {
		for _, id := range m[1:] {
			if strings.Contains(id, "direction") {
				t.Errorf("class id %q holds direction:\n%s", id, doc)
			}
		}
	}
	assertLines(t, doc,
		`    class Redirec_tion["Redirection"] {`,
		"    Base <|-- Redirec_tion",
		"    LRUCache --> Redirec_tion : TARGET (one)",
	)
}

// TestMarshal_EntityCodesNeverSpellDirection pins that a schema named
// "dir:ction TB" generates: its label codes the colon and the space, so no
// reading of it holds a direction statement. The self-check's reading of the
// codes themselves is TestCheckDiagram_ReadsTheDiagramAsMermaidDoes's.
func TestMarshal_EntityCodesNeverSpellDirection(t *testing.T) {
	t.Parallel()

	doc := string(mustMarshal(t, loadTestdata(t, "mermaid/fixtures/direction_label/main")))
	assertLines(t, doc, `    class Beacon__dir_ction_TB_["Beacon#32;#40;dir#58;ction#32;TB#41;"] {`)
}

// TestMarshal_ClassLabelHoldsNoSyntax pins that a class label writes every
// character but an ASCII letter or digit, a dot, and an underscore between two
// ASCII letters or digits as an entity code: Mermaid 11 reads a label as Markdown, and Mermaid's render
// rewrites the sequences it uses as entity placeholders.
func TestMarshal_ClassLabelHoldsNoSyntax(t *testing.T) {
	t.Parallel()

	doc := string(mustMarshal(t, loadTestdata(t, "mermaid/fixtures/label_text/main")))
	assertLines(t, doc, `    class Mark___b___c___d__y____x____1___i__amp__direc_tion_LR_["Mark#32;#40;#42;b#42;#32;#95;c#95;#32;#92;d#32;#36;y#36;#32;#64258;#176;x#182;#223;#32;#35;1#59;#32;#60;i#62;#38;amp#59;#32;direction#32;LR#41;"] {`)
}

// TestCheckDiagram_ReadsTheDiagramAsMermaidDoes pins two rules of the check's
// model of Mermaid: an entity code is replaced by Mermaid's own placeholder,
// which holds no white space and joins no letters around it, and a direction statement is read across a line
// break outside a class body, but not inside one.
func TestCheckDiagram_ReadsTheDiagramAsMermaidDoes(t *testing.T) {
	t.Parallel()

	const head = "classDiagram\n    direction TB\n"
	for _, tt := range []struct {
		name, body, want string
	}{
		{"a code inside direction", head + "    class B__dir_ction_TB_[\"Beacon (dir#58;ction TB)\"]\n", ""},
		{"a named code inside direction", head + "    class B[\"dir#quot;ction TB\"]\n", ""},
		{"an id ending in direction before a keyword line", head + "    class Redirection\n    LRUCache --> Redirection : TARGET (one)\n", "class Redirection"},
		{"an edge ending in direction before a keyword line", head + "    Base <|-- Redirection\n    LRUCache --> Base : TARGET (one)\n", "Base <|-- Redirection"},
		{"white space across lines", head + "    class Redirection\n\n    BTree --> X : T (one)\n", "class Redirection"},
		{"a keyword line after direction in a body", head + "    class A {\n        redirection\n        LR String\n    }\n", ""},
		{"a lower-case keyword", head + "    class Redirection\n    lrucache --> Redirection : TARGET (one)\n", ""},
		{"a named code inside direction", head + "    class B[\"x (direc#tion; TB)\"]\n", ""},
		{"a statement on a later line", head + "    class A\n    class Redirection\n    LRUCache --> Redirection : T (one)\n", "class Redirection"},
		{"the line quoted as written", head + "    class A\n    class B[\"x #58; direction LR\"]\n", "#58;"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := checkDiagram(tt.body)
			switch {
			case tt.want == "" && err != nil:
				t.Errorf("checkDiagram = %v, want nil", err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Errorf("checkDiagram = %v, want an error naming %q", err, tt.want)
			}
		})
	}
}

// mustMarshal returns Marshal's document for s, failing the test when Marshal
// refuses it.
func mustMarshal(t *testing.T, s *schema.Schema) []byte {
	t.Helper()
	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal = %v, want nil", err)
	}
	return out
}

// TestCheckDiagram_ReadsEveryJavaScriptSpace pins that the direction rule reads
// each character JavaScript's \s matches, which is how Mermaid's lexer reads
// white space: ECMAScript's WhiteSpace and LineTerminator sets, written out
// here rather than read from jsSpace, on a line after the first. A line feed
// is the break between lines, which TestCheckDiagram_ReadsTheDiagramAsMermaidDoes
// reads.
func TestCheckDiagram_ReadsEveryJavaScriptSpace(t *testing.T) {
	t.Parallel()

	for _, r := range []rune{'\u0009', '\u000b', '\u000d', '\u000c', '\u0020', '\u00a0', '\u1680', '\u2000', '\u2001', '\u2002', '\u2003', '\u2004', '\u2005', '\u2006', '\u2007', '\u2008', '\u2009', '\u200a', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff'} {
		body := "classDiagram\n    direction TB\n    class A\n    class B[\"x direction" + string(r) + "LR\"]\n"
		if err := checkDiagram(body); err == nil {
			t.Errorf("checkDiagram accepts direction and %U before LR", r)
		}
	}
}
