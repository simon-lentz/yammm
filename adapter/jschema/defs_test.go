package jschema

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

const fleetFixture = `schema "fleet"

type StateCode = String [2, 2]

type State {
	code StateCode primary
	name String required
}

type Car {
	vin String primary
	zips List <StateCode>
	--> REGISTERED_IN (one) State {
		since Integer required
		tag StateCode
	}
	*-> WHEELS (one:many) Wheel
}

part type Wheel {
	position String required
}
`

func loadFixture(t *testing.T, src, name string) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), src, name)
	if res.HasErrors() {
		t.Fatalf("load %s: %v", name, res.Err())
	}
	return s
}

func loadMulti(t *testing.T, sources map[string]string, entry string) *schema.Schema {
	t.Helper()
	m := make(map[string][]byte, len(sources))
	for k, v := range sources {
		m[k] = []byte(v)
	}
	s, res := schema.LoadSourcesWithEntry(t.Context(), m, entry, ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load multi: %v", res.Err())
	}
	return s
}

func mustType(t *testing.T, s *schema.Schema, name string) *schema.Type {
	t.Helper()
	typ, ok := s.Type(name)
	if !ok {
		t.Fatalf("type %q missing", name)
	}
	return typ
}

func mustDefName(t *testing.T, table *defsTable, typ *schema.Type) string {
	t.Helper()
	name, ok := table.defName(typ.ID())
	if !ok {
		t.Fatalf("no def name for type %q", typ.Name())
	}
	return name
}

func TestBuildDefsTable_SingleSchema(t *testing.T) {
	s := loadFixture(t, fleetFixture, "test://fleet.yammm")
	table, err := buildDefsTable(s)
	if err != nil {
		t.Fatalf("buildDefsTable: %v", err)
	}

	// Unique raw names stay bare.
	for _, want := range []string{"State", "Car", "Wheel"} {
		if got := mustDefName(t, table, mustType(t, s, want)); got != want {
			t.Errorf("type %s: def name %q, want bare", want, got)
		}
	}
	dt, ok := s.DataType("StateCode")
	if !ok {
		t.Fatal("StateCode datatype missing")
	}
	if name, ok := table.dataTypeDefName(dt); !ok || name != "StateCode" {
		t.Errorf("datatype def name %q ok=%v, want bare StateCode", name, ok)
	}

	// Emission order is declaration order.
	var typeNames []string
	for _, typ := range table.orderedTypes {
		typeNames = append(typeNames, typ.Name())
	}
	if got, want := strings.Join(typeNames, ","), "State,Car,Wheel"; got != want {
		t.Errorf("orderedTypes %s, want %s", got, want)
	}

	// EDGE_ registration: owner-qualified key from collision-resolved def
	// names plus the relation's JSON field name.
	car := mustType(t, s, "Car")
	assocs := car.AssociationsSlice()
	if len(assocs) != 1 {
		t.Fatalf("expected 1 association on Car, got %d", len(assocs))
	}
	rel := assocs[0]
	edgeName, ok := table.edgeDefName(rel)
	if !ok || edgeName != "EDGE_Car_registered_in_State" {
		t.Errorf("edge def name %q ok=%v, want EDGE_Car_registered_in_State", edgeName, ok)
	}
	if len(table.orderedEdges) != 1 {
		t.Fatalf("expected 1 ordered edge, got %d", len(table.orderedEdges))
	}
	if table.orderedEdges[0].target.Name() != "State" {
		t.Errorf("edge target %q, want State", table.orderedEdges[0].target.Name())
	}
}

func TestBuildDefsTable_DataTypePropertyRegistration(t *testing.T) {
	s := loadFixture(t, fleetFixture, "test://fleet.yammm")
	table, err := buildDefsTable(s)
	if err != nil {
		t.Fatalf("buildDefsTable: %v", err)
	}

	state := mustType(t, s, "State")
	car := mustType(t, s, "Car")
	rel := car.AssociationsSlice()[0]

	edgeProp := func(name string) *schema.Property {
		p, ok := rel.Property(name)
		if !ok {
			t.Fatalf("edge property %q missing", name)
		}
		return p
	}

	// DataType-typed members register: a type property, a list element, and
	// an edge property. Non-DataType members miss.
	registered := []*schema.Property{
		prop(t, state, "code"), // direct alias
		prop(t, car, "zips"),   // list-of-alias element
		edgeProp("tag"),        // alias edge property
	}
	for _, p := range registered {
		if name, ok := table.dtPropName(p); !ok || name != "StateCode" {
			t.Errorf("property %q: dtPropName %q ok=%v, want StateCode", p.Name(), name, ok)
		}
	}
	for _, p := range []*schema.Property{prop(t, car, "vin"), edgeProp("since")} {
		if _, ok := table.dtPropName(p); ok {
			t.Errorf("property %q should not be registered", p.Name())
		}
	}

	// End-to-end with the Task 3 mapper: the table's lookup satisfies dtRef.
	got, err := schemaForProperty(prop(t, car, "zips"), table.dtPropName)
	if err != nil {
		t.Fatalf("schemaForProperty: %v", err)
	}
	want := `{"type": "array", "items": {"$ref": "#/$defs/StateCode"}}`
	if g, w := normalize(t, got), normalizeWant(t, want); g != w {
		t.Errorf("got  %s\nwant %s", g, w)
	}
}

func TestBuildDefsTable_CrossSchemaCollisionQualifies(t *testing.T) {
	s := loadMulti(t, map[string]string{
		"main.yammm": `schema "geo"

import "common.yammm" as common

type Region {
	id String primary
}

type County {
	fips String primary
	--> IN_REGION (one) common.Region
}
`,
		"common.yammm": `schema "common"

type Region {
	code String primary
	name String required
}
`,
	}, "main.yammm")

	table, err := buildDefsTable(s)
	if err != nil {
		t.Fatalf("buildDefsTable: %v", err)
	}

	// Both Region claimants qualify; the unique County stays bare.
	if got := mustDefName(t, table, mustType(t, s, "Region")); got != "geo.Region" {
		t.Errorf("entry-schema Region: %q, want geo.Region", got)
	}
	if got := mustDefName(t, table, mustType(t, s, "County")); got != "County" {
		t.Errorf("County: %q, want bare", got)
	}
	var qualifiedImported bool
	for _, typ := range table.orderedTypes {
		if typ.SchemaName() == "common" && typ.Name() == "Region" {
			if got := mustDefName(t, table, typ); got != "common.Region" {
				t.Errorf("imported Region: %q, want common.Region", got)
			}
			qualifiedImported = true
		}
	}
	if !qualifiedImported {
		t.Error("imported common.Region not in orderedTypes")
	}

	// The edge key uses the qualified target name.
	county := mustType(t, s, "County")
	edgeName, ok := table.edgeDefName(county.AssociationsSlice()[0])
	if !ok || edgeName != "EDGE_County_in_region_common.Region" {
		t.Errorf("edge def name %q ok=%v, want EDGE_County_in_region_common.Region", edgeName, ok)
	}
}

func TestBuildDefsTable_CrossSchemaInheritedMembers(t *testing.T) {
	s := loadMulti(t, map[string]string{
		"main.yammm": `schema "geo"

import "common.yammm" as common

type City extends common.Located {
	name String primary
}
`,
		"common.yammm": `schema "common"

type RegionCode = String [5, 5]

part type Marker {
	id UUID primary
}

abstract type Located {
	region RegionCode required
	*-> HAS_MARKER (many) Marker
}
`,
	}, "main.yammm")

	table, err := buildDefsTable(s)
	if err != nil {
		t.Fatalf("buildDefsTable: %v", err)
	}

	// The inherited property's pointer (registered while walking its
	// DECLARING schema) resolves to the datatype defined there.
	city := mustType(t, s, "City")
	var region *schema.Property
	for _, p := range city.AllPropertiesSlice() {
		if p.Name() == "region" {
			region = p
		}
	}
	if region == nil {
		t.Fatal("inherited property region missing from City")
	}
	if name, ok := table.dtPropName(region); !ok || name != "RegionCode" {
		t.Errorf("inherited region property: dtPropName %q ok=%v, want RegionCode", name, ok)
	}

	// Abstract types are registered in the table (lookup by TargetID must
	// always succeed); whether they are EMITTED is the emitter's concern.
	var located *schema.Type
	for _, typ := range table.orderedTypes {
		if typ.Name() == "Located" {
			located = typ
		}
	}
	if located == nil {
		t.Fatal("Located missing from orderedTypes")
	}
	if got := mustDefName(t, table, located); got != "Located" {
		t.Errorf("Located def name %q, want bare", got)
	}
	// The composition target resolves through the table by TargetID.
	comps := city.AllCompositionsSlice()
	if len(comps) != 1 {
		t.Fatalf("expected 1 inherited composition, got %d", len(comps))
	}
	if name, ok := table.defName(comps[0].TargetID()); !ok || name != "Marker" {
		t.Errorf("composition target def %q ok=%v, want Marker", name, ok)
	}
}

func TestBuildDefsTable_InheritedAssociationSharesDeclaringEdge(t *testing.T) {
	src := `schema "geo"

type Org {
	id UUID primary
}

abstract type Member {
	id UUID primary
	--> BELONGS_TO (one) Org
}

type Person extends Member {
	name String required
}
`
	s := loadFixture(t, src, "test://inherited_edge.yammm")
	table, err := buildDefsTable(s)
	if err != nil {
		t.Fatalf("buildDefsTable: %v", err)
	}

	member := mustType(t, s, "Member")
	person := mustType(t, s, "Person")
	declared := member.AssociationsSlice()[0]
	inherited := person.AllAssociationsSlice()[0]

	declaredName, ok1 := table.edgeDefName(declared)
	inheritedName, ok2 := table.edgeDefName(inherited)
	if !ok1 || !ok2 {
		t.Fatalf("edge lookups failed: declared=%v inherited=%v", ok1, ok2)
	}
	if declaredName != inheritedName {
		t.Errorf("inherited association has its own edge def (%q vs %q); must share the declaring owner's", inheritedName, declaredName)
	}
	if declaredName != "EDGE_Member_belongs_to_Org" {
		t.Errorf("edge def name %q, want EDGE_Member_belongs_to_Org", declaredName)
	}
	// One EDGE_ entry total: declared once, by its owner.
	if len(table.orderedEdges) != 1 {
		t.Errorf("expected 1 ordered edge, got %d", len(table.orderedEdges))
	}
}

func TestBuildDefsTable_SameSchemaTypeDataTypeCollisionErrors(t *testing.T) {
	src := `schema "geo"

type Region = String [2, 2]

type Region {
	id String primary
}
`
	s := loadFixture(t, src, "test://type_dt_collision.yammm")
	if _, err := buildDefsTable(s); err == nil {
		t.Error("a type and datatype sharing a name in one schema cannot be separated by qualification; buildDefsTable must error")
	} else if !strings.Contains(err.Error(), "rename") {
		t.Errorf("collision error should instruct a rename, got: %v", err)
	}
}

func TestBuildDefsTable_EdgeKeyCollisionWithTypeErrors(t *testing.T) {
	src := `schema "fleet"

type EDGE_Car_owner_Person {
	id String primary
}

type Person {
	id String primary
}

type Car {
	vin String primary
	--> OWNER (one) Person
}
`
	s := loadFixture(t, src, "test://edge_key_collision.yammm")
	if _, err := buildDefsTable(s); err == nil {
		t.Error("a type whose name equals a generated EDGE_ key must be a hard error")
	}
}

func TestRefTo_JSONPointerEscaping(t *testing.T) {
	// Qualified def keys embed schema names, which are unconstrained string
	// literals — "/" and "~" must escape per RFC 6901.
	cases := []struct{ in, want string }{
		{"Person", `{"$ref": "#/$defs/Person"}`},
		{"common.Region", `{"$ref": "#/$defs/common.Region"}`},
		{"a/b.Region", `{"$ref": "#/$defs/a~1b.Region"}`},
		{"odd~name.X", `{"$ref": "#/$defs/odd~0name.X"}`},
	}
	for _, tc := range cases {
		if g, w := normalize(t, refTo(tc.in)), normalizeWant(t, tc.want); g != w {
			t.Errorf("refTo(%q): got %s want %s", tc.in, g, w)
		}
	}
}
