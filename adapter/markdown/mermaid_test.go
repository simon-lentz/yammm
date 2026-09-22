package markdown

import (
	"strings"
	"testing"
)

func TestEmitClassDiagram_SingleSchema(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, `schema "fleet"

type Tag = String [1, 20]

type Person {
	id UUID primary
	name String [1, 100] required
	age Integer [0, 150]
}

type Car {
	vin String primary
	color Enum["red", "green"]
	tag Tag
	--> OWNER (one) Person {
		since Date
	}
	*-> WHEELS (one:many) Wheel
}

part type Wheel {
	position Enum["FL", "FR", "RL", "RR"] required
}
`)
	g := newTestGenerator(t, s)
	g.emitClassDiagram(outlineEntry{md: "Class Diagram"})
	want := "## Class Diagram\n" +
		"\n" +
		"```mermaid\n" +
		"classDiagram\n" +
		"    direction TB\n" +
		"    class Person {\n" +
		"        id UUID\n" +
		"        name String\n" +
		"        age Integer\n" +
		"    }\n" +
		"    class Car {\n" +
		"        vin String\n" +
		"        color Enum\n" +
		"        tag Tag\n" +
		"    }\n" +
		"    class Wheel {\n" +
		"        <<Part>>\n" +
		"        position Enum\n" +
		"    }\n" +
		"    Car --> Person : OWNER (one)\n" +
		"    Car *-- Wheel : WHEELS (one#58;many)\n" +
		"```\n"
	if got := g.buf.String(); got != want {
		t.Errorf("diagram = %q, want %q", got, want)
	}
}

// TestEmitClassDiagram_MembersOff pins WithClassMembers(false): classes,
// stereotypes and edges survive; member lines do not. A stereotyped class
// keeps the braced form with the stereotype alone, an unstereotyped one
// declares in the compact form.
func TestEmitClassDiagram_MembersOff(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, `schema "fleet"

abstract type Vehicle {
	vin String primary
	make String required
}

type Car extends Vehicle {
	color String
}

type Truck extends Vehicle {
}
`)
	g := newTestGenerator(t, s)
	g.cfg.classMembers = false
	g.emitClassDiagram(outlineEntry{md: "Class Diagram"})
	want := "## Class Diagram\n" +
		"\n" +
		"```mermaid\n" +
		"classDiagram\n" +
		"    direction TB\n" +
		"    class Vehicle {\n" +
		"        <<Abstract>>\n" +
		"    }\n" +
		"    class Car\n" +
		"    class Truck\n" +
		"    Vehicle <|-- Car\n" +
		"    Vehicle <|-- Truck\n" +
		"```\n"
	if got := g.buf.String(); got != want {
		t.Errorf("diagram = %q, want %q", got, want)
	}
}

func TestEmitClassDiagram_InheritanceAndAbstract(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, `schema "fleet"

abstract type Vehicle {
	vin String primary
	make String required
}

type Car extends Vehicle {
	color String
}

type Truck extends Vehicle {
}
`)
	g := newTestGenerator(t, s)
	g.emitClassDiagram(outlineEntry{md: "Class Diagram"})
	want := "## Class Diagram\n" +
		"\n" +
		"```mermaid\n" +
		"classDiagram\n" +
		"    direction TB\n" +
		"    class Vehicle {\n" +
		"        <<Abstract>>\n" +
		"        vin String\n" +
		"        make String\n" +
		"    }\n" +
		"    class Car {\n" +
		"        color String\n" +
		"    }\n" +
		"    class Truck\n" +
		"    Vehicle <|-- Car\n" +
		"    Vehicle <|-- Truck\n" +
		"```\n"
	if got := g.buf.String(); got != want {
		t.Errorf("diagram = %q, want %q", got, want)
	}
}

func TestEmitClassDiagram_ImportsSanitizedIDs(t *testing.T) {
	t.Parallel()

	s := loadSources(t, map[string][]byte{
		"entry.yammm": []byte(`schema "geo"

import "common.yammm" as common

type City extends common.Located {
	name String primary
}
`),
		"common.yammm": []byte(`schema "common"

type Region {
	id String primary
}

abstract type Located {
	code String required
	--> IN_REGION (one) Region
}
`),
	})
	g := newTestGenerator(t, s)
	g.emitClassDiagram(outlineEntry{md: "Class Diagram"})
	want := "## Class Diagram\n" +
		"\n" +
		mermaidFloorSentence + "\n" +
		"\n" +
		"```mermaid\n" +
		"classDiagram\n" +
		"    direction TB\n" +
		"    class City {\n" +
		"        name String\n" +
		"    }\n" +
		"    class common_Region[\"common.Region\"] {\n" +
		"        id String\n" +
		"    }\n" +
		"    class common_Located[\"common.Located\"] {\n" +
		"        <<Abstract>>\n" +
		"        code String\n" +
		"    }\n" +
		"    common_Located <|-- City\n" +
		"    common_Located --> common_Region : IN_REGION (one)\n" +
		"```\n"
	if got := g.buf.String(); got != want {
		t.Errorf("diagram = %q, want %q", got, want)
	}
}

// TestEmitClassDiagram_FloorSentenceOnlyWhenLabelled pins that the emitter
// states the Mermaid floor exactly when it wrote a labelled class — which a
// schema without imports never does, so such a document keeps rendering on
// Mermaid 9 and its bytes do not move.
func TestEmitClassDiagram_FloorSentenceOnlyWhenLabelled(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, `schema "fleet"

type Car {
	vin String primary
}
`)
	g := newTestGenerator(t, s)
	g.emitClassDiagram(outlineEntry{md: "Class Diagram"})
	if got := g.buf.String(); strings.Contains(got, "Mermaid 10.1.0") {
		t.Errorf("an import-free diagram carries the floor sentence:\n%s", got)
	}
	if mermaidFloorSentence != "This diagram uses Mermaid's labelled class form and needs Mermaid 10.1.0 or later." {
		t.Errorf("floor sentence = %q; the floor is 10.1.0, the first tag whose grammar has classLabel", mermaidFloorSentence)
	}
}

func TestEmitClassDiagram_RegistersAnchorAndClosesFence(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, `schema "people"

type Person {
	id UUID primary
}
`)
	g := newTestGenerator(t, s)
	g.emitClassDiagram(outlineEntry{md: "Class Diagram"})
	if h := g.outline[1]; h.kind != kindClassDiagram || h.anchor != "class-diagram" {
		t.Errorf("outline[1] = %+v, want the class diagram at #class-diagram", h)
	}
	got := g.buf.String()
	if n := strings.Count(got, "```"); n != 2 {
		t.Errorf("fence marker count = %d, want 2 in %q", n, got)
	}
	if !strings.HasSuffix(got, "```\n") {
		t.Errorf("diagram does not end with closing fence: %q", got)
	}
}

// TestCheckDiagram pins the diagram lines the self-check accepts: the forms the
// emitter writes, with entity codes read as Mermaid reads them, and nothing a
// Mermaid lexer reads another way.
func TestCheckDiagram(t *testing.T) {
	t.Parallel()

	const head = "classDiagram\n    direction TB\n"
	for _, tt := range []struct {
		name, body, want string
	}{
		{"emitted forms", head +
			"    class Car {\n        <<Abstract>>\n        vin String\n        tags List\n    }\n" +
			"    class Wheel\n" +
			"    class a_B[\"a#quot;b (x#58;y direction#32;LR)\"]\n" +
			"    Car <|-- Wheel\n" +
			"    Car *-- Wheel : WHEELS (one#58;many)\n" +
			"    Car --> a_B : TO (_)\n", ""},
		{"no header", "    class Car\n", "header lines"},
		{"another diagram type", "flowchart TD\n    direction TB\n    class Car\n", "header lines"},
		{"no direction line", "classDiagram\n    class Car\n    class Wheel\n", "header lines"},
		{"colon in an edge label", head + "    Car *-- Wheel : WHEELS (one:many)\n", "WHEELS (one:many)"},
		{"semicolon in an edge label", head + "    Car --> Wheel : A;B\n", "A;B"},
		{"quote in a class label", head + "    class a_B[\"a\"b\"]\n", `a\"b`},
		{"non-ASCII class id", head + "    class Foo__\u00e9_\n", "Foo__"},
		{"brace in a member line", head + "    class Car {\n        id{x}\n    }\n", "id{x}"},
		{"comment line", head + "    %% note\n", "%% note"},
		{"direction in a class label", head + "    class P_x_[\"P (x direction LR)\"]\n", "direction LR"},
		{"a member named direction", head + "    class P {\n        direction LR\n        direction String\n    }\n", ""},
		{"direction and no keyword in a class label", head + "    class P_x_[\"P (direction String, direction lr)\"]\n", ""},
		{"direction in an edge label", head + "    Car --> Wheel : x direction TB\n", "direction TB"},
		{"direction before a no-break space", head + "    class P_x_[\"P (direction\u00a0LR)\"]\n", "direction"},
		{"directive in a label", head + "    class a_B[\"%%{init: {}}%%\"]\n", "%%{init"},
		{"open class body", head + "    class Car {\n        vin String\n", "body open"},
		{"unknown arrow", head + "    Car ..> Wheel\n", "..>"},
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

// TestMarshal_PropertyNamedDirectionIsAMember pins that a property named
// direction renders as a class member, whatever its type is named. Mermaid
// reads "direction" and a direction keyword as a statement only outside a
// class body, so a member line needs no escape for it.
func TestMarshal_PropertyNamedDirectionIsAMember(t *testing.T) {
	t.Parallel()

	out, err := Marshal(loadSchema(t, `schema "s"

type LR = String[1, 2]

type Arrow {
	id String primary
	direction String
	heading LR
	wind_direction Integer
	--> POINTS (one) Winddirection
}

type Winddirection {
	id String primary
}

type Sign {
	id String primary
	direction LR
}
`))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	doc := string(out)
	for _, want := range []string{
		"        direction String\n",
		"        direction LR\n",
		"        heading LR\n",
		"        wind_direction Integer\n",
		"    class Winddirection {\n",
		"    Arrow --> Winddirection : POINTS (one)\n",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("document has no member line %q:\n%s", want, doc)
		}
	}
}
