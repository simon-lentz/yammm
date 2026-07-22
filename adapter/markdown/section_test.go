package markdown

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/expr"
)

// loadSchema parses src as a single-source schema and fails the test on any
// load error.
func loadSchema(t *testing.T, src string) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(context.Background(), src, "test.yammm")
	if err := res.Err(); err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	return s
}

// loadSources loads a multi-source schema with entry.yammm as the entry
// point and fails the test on any load error.
func loadSources(t *testing.T, sources map[string][]byte) *schema.Schema {
	t.Helper()
	s, res := schema.LoadSourcesWithEntry(context.Background(), sources, "entry.yammm", ".", schema.WithSourcesOnly())
	if err := res.Err(); err != nil {
		t.Fatalf("LoadSourcesWithEntry: %v", err)
	}
	return s
}

// newTestGenerator builds a generator, failing the test on an anchor-collision
// error (the corpus fixtures never collide).
func newTestGenerator(t *testing.T, s *schema.Schema) *generator {
	t.Helper()
	g, err := newGenerator(s)
	if err != nil {
		t.Fatalf("newGenerator: %v", err)
	}
	return g
}

// sectionFor renders the named type's section and returns it.
func sectionFor(t *testing.T, s *schema.Schema, typeName string) string {
	t.Helper()
	g := newTestGenerator(t, s)
	target := findType(t, g, typeName)
	g.emitTypeSection(target)
	return g.buf.String()
}

// findType locates a type anywhere in the generator's closure by bare name.
func findType(t *testing.T, g *generator, name string) *schema.Type {
	t.Helper()
	for _, sch := range g.closure {
		if typ, ok := sch.Type(name); ok {
			return typ
		}
	}
	t.Fatalf("type %q not found in closure", name)
	return nil
}

func TestEmitTypeSection_PropertyTable(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, `schema "people"

type Person {
	id UUID primary
	/* Full legal name. */
	name String [1, 100] required
	age Integer [0, 150]
}
`)
	got := sectionFor(t, s, "Person")
	want := "### Person\n" +
		"\n" +
		"| Property | Type | Modifiers | Description |\n" +
		"| --- | --- | --- | --- |\n" +
		"| `id` | `UUID` | primary |  |\n" +
		"| `name` | `String[1, 100]` | required | Full legal name. |\n" +
		"| `age` | `Integer[0, 150]` |  |  |\n"
	if got != want {
		t.Errorf("section = %q, want %q", got, want)
	}
}

func TestEmitTypeSection_TypeDocumentation(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, `schema "people"

/* A person in the registry. */
type Person {
	id UUID primary
}
`)
	got := sectionFor(t, s, "Person")
	want := "### Person\n" +
		"\n" +
		"A person in the registry.\n" +
		"\n" +
		"| Property | Type | Modifiers | Description |\n" +
		"| --- | --- | --- | --- |\n" +
		"| `id` | `UUID` | primary |  |\n"
	if got != want {
		t.Errorf("section = %q, want %q", got, want)
	}
}

func TestEmitTypeSection_InheritedRowsAndExtendsBadge(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, `schema "fleet"

abstract type Vehicle {
	vin String primary
	make String required
}

type Car extends Vehicle {
	color Enum["red", "green"] required
}
`)
	got := sectionFor(t, s, "Car")
	want := "### Car\n" +
		"\n" +
		"Extends: [Vehicle](#vehicle).\n" +
		"\n" +
		"| Property | Type | Modifiers | Description |\n" +
		"| --- | --- | --- | --- |\n" +
		"| `color` | `Enum[\"red\", \"green\"]` | required |  |\n" +
		"| `vin` | `String` | primary, from Vehicle |  |\n" +
		"| `make` | `String` | required, from Vehicle |  |\n"
	if got != want {
		t.Errorf("section = %q, want %q", got, want)
	}
}

func TestEmitTypeSection_AbstractBadge(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, `schema "fleet"

abstract type Vehicle {
	vin String primary
}

type Car extends Vehicle {
	color String
}
`)
	got := sectionFor(t, s, "Vehicle")
	want := "### Vehicle\n" +
		"\n" +
		"*Abstract type* — no direct instances.\n" +
		"\n" +
		"| Property | Type | Modifiers | Description |\n" +
		"| --- | --- | --- | --- |\n" +
		"| `vin` | `String` | primary |  |\n"
	if got != want {
		t.Errorf("section = %q, want %q", got, want)
	}
}

func TestEmitTypeSection_PartBadgeAndCompositionBullet(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, `schema "fleet"

part type Wheel {
	position Enum["FL", "FR", "RL", "RR"] required
}

type Car {
	vin String primary
	*-> WHEELS (one:many) Wheel
}
`)
	wheel := sectionFor(t, s, "Wheel")
	wantWheel := "### Wheel\n" +
		"\n" +
		"*Part type* — instances exist only as composed children.\n" +
		"\n" +
		"| Property | Type | Modifiers | Description |\n" +
		"| --- | --- | --- | --- |\n" +
		"| `position` | `Enum[\"FL\", \"FR\", \"RL\", \"RR\"]` | required |  |\n"
	if wheel != wantWheel {
		t.Errorf("Wheel section = %q, want %q", wheel, wantWheel)
	}

	car := sectionFor(t, s, "Car")
	wantCar := "### Car\n" +
		"\n" +
		"| Property | Type | Modifiers | Description |\n" +
		"| --- | --- | --- | --- |\n" +
		"| `vin` | `String` | primary |  |\n" +
		"\n" +
		"**Compositions**\n" +
		"\n" +
		"- `*-> WHEELS (one:many)` [Wheel](#wheel)\n"
	if car != wantCar {
		t.Errorf("Car section = %q, want %q", car, wantCar)
	}
}

func TestEmitTypeSection_AssociationBulletWithDoc(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, `schema "fleet"

type Person {
	id UUID primary
}

type Car {
	vin String primary
	/* Current owner. */
	--> OWNER (one) Person
}
`)
	got := sectionFor(t, s, "Car")
	want := "### Car\n" +
		"\n" +
		"| Property | Type | Modifiers | Description |\n" +
		"| --- | --- | --- | --- |\n" +
		"| `vin` | `String` | primary |  |\n" +
		"\n" +
		"**Associations**\n" +
		"\n" +
		"- `--> OWNER (one)` [Person](#person)\n" +
		"\n" +
		"  Current owner.\n"
	if got != want {
		t.Errorf("section = %q, want %q", got, want)
	}
}

func TestEmitTypeSection_EdgePropertySubTable(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, `schema "geo"

type State {
	code String primary
}

type County {
	fips String primary
	--> IN_STATE (one) State {
		/* Assignment date. */
		since Date required
		note String
	}
}
`)
	got := sectionFor(t, s, "County")
	want := "### County\n" +
		"\n" +
		"| Property | Type | Modifiers | Description |\n" +
		"| --- | --- | --- | --- |\n" +
		"| `fips` | `String` | primary |  |\n" +
		"\n" +
		"**Associations**\n" +
		"\n" +
		"- `--> IN_STATE (one)` [State](#state)\n" +
		"\n" +
		"  | Property | Type | Modifiers | Description |\n" +
		"  | --- | --- | --- | --- |\n" +
		"  | `since` | `Date` | required | Assignment date. |\n" +
		"  | `note` | `String` |  |  |\n"
	if got != want {
		t.Errorf("section = %q, want %q", got, want)
	}
}

func TestEmitTypeSection_Invariants(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, `schema "people"

type Person {
	id UUID primary
	age Integer [0, 150]

	! "adults only" age >= 18
	! "age in bounds" age >= 0 &&
		age <= 150
}
`)
	got := sectionFor(t, s, "Person")
	want := "### Person\n" +
		"\n" +
		"| Property | Type | Modifiers | Description |\n" +
		"| --- | --- | --- | --- |\n" +
		"| `id` | `UUID` | primary |  |\n" +
		"| `age` | `Integer[0, 150]` |  |  |\n" +
		"\n" +
		"**Invariants**\n" +
		"\n" +
		"- \"adults only\"\n" +
		"\n" +
		"  ```yammm\n" +
		"  ! \"adults only\" age >= 18\n" +
		"  ```\n" +
		"\n" +
		"- \"age in bounds\"\n" +
		"\n" +
		"  ```yammm\n" +
		"  ! \"age in bounds\" age >= 0 &&\n" +
		"  age <= 150\n" +
		"  ```\n"
	if got != want {
		t.Errorf("section = %q, want %q", got, want)
	}
}

func TestEmitTypeSection_InvariantDocStrippedFromFence(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, `schema "people"

type Person {
	id UUID primary
	age Integer

	/* Enforced at intake. */
	! "adults only" age >= 18
}
`)
	got := sectionFor(t, s, "Person")
	wantBlock := "**Invariants**\n" +
		"\n" +
		"- \"adults only\"\n" +
		"\n" +
		"  Enforced at intake.\n" +
		"\n" +
		"  ```yammm\n" +
		"  ! \"adults only\" age >= 18\n" +
		"  ```\n"
	if !strings.Contains(got, wantBlock) {
		t.Errorf("section %q does not contain invariant block %q", got, wantBlock)
	}
	if strings.Contains(got, "Enforced at intake. */") || strings.Contains(got, "/*") {
		t.Errorf("fence retains doc-comment text: %q", got)
	}
}

func TestEmitTypeSection_BuilderInvariantDegradesToMessage(t *testing.T) {
	t.Parallel()

	s, res := schema.NewBuilder().WithName("built").
		AddType("Thing").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithInvariant("always true", expr.NewLiteral(true), "").
		Done().
		Build()
	if err := res.Err(); err != nil {
		t.Fatalf("Build: %v", err)
	}

	got := sectionFor(t, s, "Thing")
	want := "### Thing\n" +
		"\n" +
		"| Property | Type | Modifiers | Description |\n" +
		"| --- | --- | --- | --- |\n" +
		"| `id` | `String` | primary |  |\n" +
		"\n" +
		"**Invariants**\n" +
		"\n" +
		"- \"always true\"\n"
	if got != want {
		t.Errorf("section = %q, want %q", got, want)
	}
}

func TestEmitTypeSection_CrossSchemaTargetsAndInheritance(t *testing.T) {
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

	got := sectionFor(t, s, "City")
	want := "### City\n" +
		"\n" +
		"Extends: [common.Located](#commonlocated).\n" +
		"\n" +
		"| Property | Type | Modifiers | Description |\n" +
		"| --- | --- | --- | --- |\n" +
		"| `name` | `String` | primary |  |\n" +
		"| `code` | `String` | required, from common.Located |  |\n" +
		"\n" +
		"**Associations**\n" +
		"\n" +
		"- `--> IN_REGION (one)` [common.Region](#commonregion) — from common.Located\n"
	if got != want {
		t.Errorf("section = %q, want %q", got, want)
	}
}

func TestEmitTypeSection_InheritedRelationProvenance(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, `schema "fleet"

type Person {
	id UUID primary
}

part type Wheel {
	position Enum["FL", "FR", "RL", "RR"] required
}

abstract type Vehicle {
	vin String primary
	--> OWNER (one) Person
	*-> WHEELS (one:many) Wheel
}

type Car extends Vehicle {
	color String
	--> DEALER (one) Person
}
`)
	got := sectionFor(t, s, "Car")
	// Own relations (DEALER) carry no marker; inherited relations (OWNER,
	// WHEELS) name the declaring ancestor, exactly as the property table's
	// "from <Owner>" marks inherited rows.
	want := "### Car\n" +
		"\n" +
		"Extends: [Vehicle](#vehicle).\n" +
		"\n" +
		"| Property | Type | Modifiers | Description |\n" +
		"| --- | --- | --- | --- |\n" +
		"| `color` | `String` |  |  |\n" +
		"| `vin` | `String` | primary, from Vehicle |  |\n" +
		"\n" +
		"**Associations**\n" +
		"\n" +
		"- `--> DEALER (one)` [Person](#person)\n" +
		"\n" +
		"- `--> OWNER (one)` [Person](#person) — from Vehicle\n" +
		"\n" +
		"**Compositions**\n" +
		"\n" +
		"- `*-> WHEELS (one:many)` [Wheel](#wheel) — from Vehicle\n"
	if got != want {
		t.Errorf("section = %q, want %q", got, want)
	}
}

func TestEmitTypeSection_InheritedInvariantProvenance(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, `schema "people"

abstract type Base {
	id UUID primary
	age Integer

	! "adults only" age >= 18
}

type Person extends Base {
	score Integer

	! "positive score" score >= 0
}
`)
	got := sectionFor(t, s, "Person")
	// The type's own invariant carries no marker; the inherited one names its
	// declaring ancestor.
	wantInv := "**Invariants**\n" +
		"\n" +
		"- \"positive score\"\n" +
		"\n" +
		"  ```yammm\n" +
		"  ! \"positive score\" score >= 0\n" +
		"  ```\n" +
		"\n" +
		"- \"adults only\" — from Base\n" +
		"\n" +
		"  ```yammm\n" +
		"  ! \"adults only\" age >= 18\n" +
		"  ```\n"
	if !strings.Contains(got, wantInv) {
		t.Errorf("section %q does not contain expected invariant block %q", got, wantInv)
	}
}

func TestEmitTypeSection_RegistersHeadingAnchor(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, `schema "people"

type Person {
	id UUID primary
}
`)
	g := newTestGenerator(t, s)
	g.emitTypeSection(findType(t, g, "Person"))
	if !g.anchors["person"] {
		t.Errorf("anchors = %v, want %q registered", g.anchors, "person")
	}
}
