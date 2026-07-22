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
