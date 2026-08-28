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
	g.emitClassDiagram()
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
		"    Car *-- Wheel : WHEELS (one:many)\n" +
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
	g.classMembers = false
	g.emitClassDiagram()
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
	g.emitClassDiagram()
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
	g.emitClassDiagram()
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
	g.emitClassDiagram()
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
	g.emitClassDiagram()
	if !g.anchors["class-diagram"] {
		t.Errorf("anchors = %v, want %q registered", g.anchors, "class-diagram")
	}
	got := g.buf.String()
	if n := strings.Count(got, "```"); n != 2 {
		t.Errorf("fence marker count = %d, want 2 in %q", n, got)
	}
	if !strings.HasSuffix(got, "```\n") {
		t.Errorf("diagram does not end with closing fence: %q", got)
	}
}
