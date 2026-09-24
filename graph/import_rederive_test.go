package graph_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot/snapshottest"
)

// rederiveSchema is S and each S′: the key constraint, which KNOWS's ref edge
// property shares, and the multiplicities of WORKS_AT and AUDITS vary.
const rederiveSchema = `schema "rederive"

type Company {
	id %s primary
	name String
}

type Employee {
	id String primary
	--> WORKS_AT %s Company
	--> AUDITS %s Company
	--> KNOWS (_:many) Company {
		since Integer
		ref %s
	}
}
`

const (
	companyOne     = "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"
	companyTwo     = "b1ffcd88-8d1a-4ef8-bb6d-6bb9bd380a22"
	companyMissing = "c2aade77-7e2b-4ef8-bb6d-6bb9bd380a33"
)

type rederiveShape struct{ key, worksAt, audits string }

func loadRederive(t *testing.T, sh rederiveShape) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), fmt.Sprintf(rederiveSchema, sh.key, sh.worksAt, sh.audits, sh.key), "rederive.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema %+v: %s", sh, res)
	}
	return s
}

type rederiveRow struct {
	typeName string
	props    map[string]any
}

// rederiveData is D, in the order both graphs add it; e2 precedes the companies
// it names, and an upper-case key or ref names a company only under a UUID key.
func rederiveData() []rederiveRow {
	ref := func(key string) map[string]any { return map[string]any{"_target_id": key} }
	// No list under WORKS_AT is empty: S records nothing for one, so the import
	// derives "absent" where Add under a required WORKS_AT records "empty".
	// No two company keys agree only under a UUID key: Add records a duplicate,
	// and the import refuses the collision, having no order to pick a survivor by.
	// No association changes its target: the import refuses an edge or a
	// target_missing record under one, where Add under S′ resolves its key anew.
	return []rederiveRow{
		{"Employee", map[string]any{
			"id": "e1", "works_at": ref(companyOne), "audits": []any{ref(companyOne)},
			"knows": []any{
				map[string]any{"_target_id": companyOne, "since": 2020, "ref": strings.ToUpper(companyTwo)},
				map[string]any{"_target_id": strings.ToUpper(companyMissing), "since": 2021, "ref": strings.ToUpper(companyOne)},
			},
		}},
		{"Employee", map[string]any{"id": "e2", "audits": []any{ref(strings.ToUpper(companyTwo))}}},
		{"Company", map[string]any{"id": companyOne, "name": "one"}},
		{"Company", map[string]any{"id": companyTwo, "name": "two"}},
		{"Company", map[string]any{"id": companyOne, "name": "one again"}},
		{"Employee", map[string]any{"id": "e3", "works_at": ref(companyMissing)}},
		{"Employee", map[string]any{
			"id": "e4", "works_at": ref(strings.ToUpper(companyOne)), "audits": []any{},
			"knows": []any{map[string]any{"_target_id": strings.ToUpper(companyOne), "since": 2022, "ref": strings.ToUpper(companyTwo)}},
		}},
	}
}

// addAll validates D under s and adds it through Graph.Add. The row Add records
// as a duplicate draws E_DUPLICATE_PK, and only that row may.
func addAll(t *testing.T, s *schema.Schema) *graph.Snapshot {
	t.Helper()
	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, row := range rederiveData() {
		vi, res := v.ValidateOne(t.Context(), row.typeName, instance.RawInstance{Properties: row.props})
		if res.HasErrors() {
			t.Fatalf("validate %s %v: %s", row.typeName, row.props["id"], res)
		}
		if res := g.Add(t.Context(), vi); res.HasErrors() && (row.props["name"] != "one again" || !res.HasCode(diag.E_DUPLICATE_PK)) {
			t.Fatalf("Add %s %v: %s", row.typeName, row.props["id"], res)
		}
	}
	return g.Snapshot()
}

// A snapshot of D built under S and imported under S′ equals D validated under
// S′ and added through Graph.Add. A key whose canonical form changes resolves a
// target_missing record into an edge, and an association turning required or
// optional gains or loses its absent and empty records.
func TestNewFromSnapshot_DerivesTheRecordsAddDerivesUnderTheImportingSchema(t *testing.T) {
	t.Parallel()
	base := rederiveShape{key: "String", worksAt: "(_:one)", audits: "(one:many)"}
	for _, c := range []struct {
		name  string
		shape rederiveShape
	}{
		{"the same schema text, loaded again", base},
		{"a String key becomes a UUID", rederiveShape{key: "UUID", worksAt: "(_:one)", audits: "(one:many)"}},
		{"the required and optional associations swap", rederiveShape{key: "String", worksAt: "(one)", audits: "(_:many)"}},
		{"both at once", rederiveShape{key: "UUID", worksAt: "(one)", audits: "(_:many)"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			built := addAll(t, loadRederive(t, base))
			sPrime := loadRederive(t, c.shape)
			imported, res := graph.NewFromSnapshot(sPrime, built)
			if res.HasErrors() {
				t.Fatalf("NewFromSnapshot: %s", res)
			}
			snapshottest.DiffSnapshots(t, addAll(t, sPrime), imported.Snapshot())
		})
	}
}

// The fixture is not vacuous: each change of schema moves a record, so an
// import that installed the snapshot's records verbatim would differ.
func TestNewFromSnapshot_RederiveFixtureMovesARecordUnderEachChange(t *testing.T) {
	t.Parallel()
	base := rederiveShape{key: "String", worksAt: "(_:one)", audits: "(one:many)"}
	built := addAll(t, loadRederive(t, base))
	count := func(snap *graph.Snapshot) (edges, records int) {
		return len(snap.Edges()), len(snap.Unresolved())
	}
	be, br := count(built)
	for _, sh := range []rederiveShape{
		{key: "UUID", worksAt: "(_:one)", audits: "(one:many)"},
		{key: "String", worksAt: "(one)", audits: "(_:many)"},
	} {
		e, r := count(addAll(t, loadRederive(t, sh)))
		if e == be && r == br {
			t.Errorf("under %+v Add derives %d edges and %d records, as under S: the change moves nothing", sh, e, r)
		}
	}
}

// The snapshot's own schema installs the records as they stand, without a
// walk, and yields the same snapshot.
func TestNewFromSnapshot_TheSnapshotsOwnSchemaInstallsItsRecords(t *testing.T) {
	t.Parallel()
	s := loadRederive(t, rederiveShape{key: "String", worksAt: "(_:one)", audits: "(one:many)"})
	built := addAll(t, s)
	imported, res := graph.NewFromSnapshot(s, built)
	if res.HasErrors() {
		t.Fatalf("NewFromSnapshot: %s", res)
	}
	snapshottest.DiffSnapshots(t, built, imported.Snapshot())
}

// A duplicate record holds to its conflict under S′ as under S. A (one) slot's
// duplicate collides with the sole occupant at any key; under a (many) slot the
// same record collides with no sibling, and Add under S′ attaches both children,
// so the import refuses it under the code Add refuses a composition with.
func TestNewFromSnapshot_RefusesADuplicateWithNoConflictUnderTheImportingSchema(t *testing.T) {
	t.Parallel()
	const orders = `schema "orders"

type Order {
	id String primary
	*-> MAIN %s Part
}

part type Part {
	code String primary
}
`
	load := func(multiplicity string) *schema.Schema {
		s, res := schema.LoadString(t.Context(), fmt.Sprintf(orders, multiplicity), "orders.yammm")
		if res.HasErrors() {
			t.Fatalf("load schema: %s", res)
		}
		return s
	}
	s := load("(_:one)")
	v := instance.NewValidator(s)
	order, res := v.ValidateOne(t.Context(), "Order", instance.RawInstance{Properties: map[string]any{
		"id": "o1", "main": []any{map[string]any{"code": "p1"}},
	}})
	if res.HasErrors() {
		t.Fatalf("validate: %s", res)
	}
	part, res := v.ValidateForComposition(t.Context(), "Order", "MAIN", []instance.RawInstance{{Properties: map[string]any{"code": "p2"}}})
	if res.HasErrors() {
		t.Fatalf("validate part: %s", res)
	}
	g := graph.New(s)
	mustOK(t, g.Add(t.Context(), order))
	if res := g.AddComposed(t.Context(), mustTypeID(t, s, "Order"), `["o1"]`, "MAIN", part[0]); !res.HasErrors() {
		t.Fatal("a second child of a (one) slot was attached")
	}
	snap := g.Snapshot()
	if len(snap.Duplicates()) != 1 {
		t.Fatalf("want the one duplicate record, got %d", len(snap.Duplicates()))
	}

	imported, res := graph.NewFromSnapshot(load("(many)"), snap)
	if imported != nil {
		t.Error("a duplicate with no conflict under the importing schema was installed")
	}
	for issue := range res.Issues() {
		if issue.Severity() == diag.Error && issue.Code() == diag.E_GRAPH_INVALID_COMPOSITION &&
			strings.Contains(issue.Message(), `holds no child of the composition "MAIN" at its key`) {
			return
		}
	}
	t.Errorf("want Error E_GRAPH_INVALID_COMPOSITION naming the slot, got: %s", res)
}

// A composed duplicate's parent type need not resolve under the importing
// schema: the types entries refuse the parent's type, and the duplicate is not
// judged against a composition the schema cannot name.
func TestNewFromSnapshot_RefusesADuplicateWhoseParentTypeTheSchemaDrops(t *testing.T) {
	t.Parallel()
	load := func(src string) *schema.Schema {
		s, res := schema.LoadString(t.Context(), src, "orders.yammm")
		if res.HasErrors() {
			t.Fatalf("load schema: %s", res)
		}
		return s
	}
	const part = `
part type Part {
	code String primary
}
`
	s := load(`schema "orders"

type Order {
	id String primary
	*-> MAIN (_:one) Part
}
` + part)
	v := instance.NewValidator(s)
	order, res := v.ValidateOne(t.Context(), "Order", instance.RawInstance{Properties: map[string]any{
		"id": "o1", "main": []any{map[string]any{"code": "p1"}},
	}})
	if res.HasErrors() {
		t.Fatalf("validate: %s", res)
	}
	second, res := v.ValidateForComposition(t.Context(), "Order", "MAIN", []instance.RawInstance{{Properties: map[string]any{"code": "p2"}}})
	if res.HasErrors() {
		t.Fatalf("validate part: %s", res)
	}
	g := graph.New(s)
	mustOK(t, g.Add(t.Context(), order))
	g.AddComposed(t.Context(), mustTypeID(t, s, "Order"), `["o1"]`, "MAIN", second[0])
	snap := g.Snapshot()
	if len(snap.Duplicates()) != 1 {
		t.Fatalf("want the one duplicate record, got %d", len(snap.Duplicates()))
	}

	imported, res := graph.NewFromSnapshot(load(`schema "orders"
`+part), snap)
	if imported != nil {
		t.Error("a snapshot whose root type the importing schema drops was installed")
	}
	if !res.HasCode(diag.E_GRAPH_TYPE_NOT_FOUND) || res.HasCode(diag.E_GRAPH_INVALID_COMPOSITION) {
		t.Errorf("want the dropped root type's refusal and no judgement of its slot, got: %s", res)
	}
}

// people is a schema whose Person has the association assoc, or none.
func people(t *testing.T, assoc string) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), `schema "people"

type Company {
	id String primary
}

type Office {
	id String primary
}

type Person {
	id String primary
	`+assoc+`
}
`, "people.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	return s
}

// An absent record holds no data, so an association the importing schema drops
// leaves nothing of it: Graph.Add under that schema, given the same data,
// records nothing.
func TestNewFromSnapshot_DropsAnAbsentRecordUnderADroppedAssociation(t *testing.T) {
	t.Parallel()
	add := func(s *schema.Schema) *graph.Snapshot {
		t.Helper()
		vi, res := instance.NewValidator(s).ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{"id": "p1"}})
		if res.HasErrors() {
			t.Fatalf("validate: %s", res)
		}
		g := graph.New(s)
		mustOK(t, g.Add(t.Context(), vi))
		return g.Snapshot()
	}
	built := add(people(t, "--> WORKS_AT (one) Company"))
	if n := len(built.Unresolved()); n != 1 {
		t.Fatalf("want the one absent record, got %d", n)
	}
	sPrime := people(t, "")
	imported, res := graph.NewFromSnapshot(sPrime, built)
	if res.HasErrors() {
		t.Fatalf("NewFromSnapshot: %s", res)
	}
	snapshottest.DiffSnapshots(t, add(sPrime), imported.Snapshot())
}

// A target_missing record names a target as an edge does, so an association
// the importing schema retargets refuses it as it refuses the edge.
func TestNewFromSnapshot_RefusesATargetMissingRecordUnderARetargetedAssociation(t *testing.T) {
	t.Parallel()
	s := people(t, "--> WORKS_AT (one) Company")
	vi, res := instance.NewValidator(s).ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
		"id": "p1", "works_at": map[string]any{"_target_id": "c9"},
	}})
	if res.HasErrors() {
		t.Fatalf("validate: %s", res)
	}
	g := graph.New(s)
	mustOK(t, g.Add(t.Context(), vi))
	imported, res := graph.NewFromSnapshot(people(t, "--> WORKS_AT (one) Office"), g.Snapshot())
	if imported != nil {
		t.Error("a record naming a Company under an association that declares Office was installed")
	}
	for issue := range res.Issues() {
		if issue.Code() == diag.E_GRAPH_UNKNOWN_RELATION && strings.Contains(issue.Message(), "the association declares") {
			return
		}
	}
	t.Errorf("want E_GRAPH_UNKNOWN_RELATION naming the declared target, got: %s", res)
}

// A duplicate whose own type the importing schema cannot resolve is the
// identity rule's to refuse; its slot is not judged as well.
func TestNewFromSnapshot_RefusesADuplicateOfATypeTheSchemaDropsOnce(t *testing.T) {
	t.Parallel()
	load := func(part string) *schema.Schema {
		s, res := schema.LoadString(t.Context(), `schema "orders"

type Order {
	id String primary
	*-> MAIN (_:one) `+part+`
}

part type `+part+` {
	code String primary
}
`, "orders.yammm")
		if res.HasErrors() {
			t.Fatalf("load schema: %s", res)
		}
		return s
	}
	s := load("Part")
	v := instance.NewValidator(s)
	order, res := v.ValidateOne(t.Context(), "Order", instance.RawInstance{Properties: map[string]any{
		"id": "o1", "main": []any{map[string]any{"code": "p1"}},
	}})
	if res.HasErrors() {
		t.Fatalf("validate: %s", res)
	}
	second, res := v.ValidateForComposition(t.Context(), "Order", "MAIN", []instance.RawInstance{{Properties: map[string]any{"code": "p2"}}})
	if res.HasErrors() {
		t.Fatalf("validate part: %s", res)
	}
	g := graph.New(s)
	mustOK(t, g.Add(t.Context(), order))
	g.AddComposed(t.Context(), mustTypeID(t, s, "Order"), `["o1"]`, "MAIN", second[0])

	imported, res := graph.NewFromSnapshot(load("Piece"), g.Snapshot())
	if imported != nil {
		t.Error("a snapshot of a part type the importing schema drops was installed")
	}
	if !res.HasCode(diag.E_GRAPH_TYPE_NOT_FOUND) || res.HasCode(diag.E_GRAPH_INVALID_COMPOSITION) {
		t.Errorf("want the identity refusal alone, got: %s", res)
	}
}
