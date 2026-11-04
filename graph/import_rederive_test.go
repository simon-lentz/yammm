package graph_test

import (
	"fmt"
	"slices"
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

func loadRederive(t *testing.T, sh rederiveShape) schema.Schema {
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
func addAll(t *testing.T, s schema.Schema) graph.Snapshot {
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
	count := func(snap graph.Snapshot) (edges, records int) {
		return len(slices.Collect(snap.Edges())), len(slices.Collect(snap.Unresolved()))
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

// people is a schema whose Person has the association assoc, or none.
func people(t *testing.T, assoc string) schema.Schema {
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
	add := func(s schema.Schema) graph.Snapshot {
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
	if n := len(slices.Collect(built.Unresolved())); n != 1 {
		t.Fatalf("want the one absent record, got %d", n)
	}
	sPrime := people(t, "")
	imported, res := graph.NewFromSnapshot(sPrime, built)
	if res.HasErrors() {
		t.Fatalf("NewFromSnapshot: %s", res)
	}
	snapshottest.DiffSnapshots(t, add(sPrime), imported.Snapshot())
}

// An "absent" record under a (one) association is what Graph.Add records for
// data that names no target there, so an import under another load of the
// schema keeps it.
func TestNewFromSnapshot_KeepsAnAbsentRecordUnderAOneAssociation(t *testing.T) {
	t.Parallel()
	add := func(s schema.Schema) graph.Snapshot {
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
	if records := slices.Collect(built.Unresolved()); len(records) != 1 || records[0].Reason() != "absent" {
		t.Fatalf("want the one absent record, got %v", records)
	}
	sPrime := people(t, "--> WORKS_AT (one) Company")
	imported, res := graph.NewFromSnapshot(sPrime, built)
	if res.HasErrors() {
		t.Fatalf("NewFromSnapshot: %s", res)
	}
	snapshottest.DiffSnapshots(t, add(sPrime), imported.Snapshot())
}

// An "empty" record stands for an empty list, which the validator refuses at a
// (one) association, so an import under a schema that makes the association
// (one) refuses the record, required or not.
func TestNewFromSnapshot_RefusesAnEmptyRecordUnderAOneAssociation(t *testing.T) {
	t.Parallel()
	props := map[string]any{"id": "p1", "works_at": []any{}}
	s := people(t, "--> WORKS_AT (one:many) Company")
	vi, res := instance.NewValidator(s).ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: props})
	if res.HasErrors() {
		t.Fatalf("validate: %s", res)
	}
	g := graph.New(s)
	mustOK(t, g.Add(t.Context(), vi))
	built := g.Snapshot()
	if records := slices.Collect(built.Unresolved()); len(records) != 1 || records[0].Reason() != "empty" {
		t.Fatalf("want the one empty record, got %v", records)
	}
	for _, assoc := range []string{"--> WORKS_AT (one) Company", "--> WORKS_AT (_:one) Company"} {
		t.Run(assoc, func(t *testing.T) {
			t.Parallel()
			sPrime := people(t, assoc)
			if _, res := instance.NewValidator(sPrime).ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: props}); !res.HasCode(diag.E_EDGE_SHAPE_MISMATCH) {
				t.Fatalf("the validator under %q accepts the data: %s", assoc, res)
			}
			imported, res := graph.NewFromSnapshot(sPrime, built)
			if imported != nil {
				t.Error("an empty record under a (one) association was installed")
			}
			var got []string
			for issue := range res.Issues() {
				got = append(got, issue.Code().String()+": "+issue.Message())
			}
			want := `E_EDGE_SHAPE_MISMATCH: NewFromSnapshot: unresolved record of Person[["p1"]] under "WORKS_AT" states reason "empty", where Graph.Add records "empty" only under a (many) association`
			if len(got) != 1 || !strings.HasPrefix(got[0], want) {
				t.Errorf("issues = %q; want one starting %q", got, want)
			}
		})
	}
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

// An edge or a target_missing record whose target type the importing schema
// drops is one identity refusal: the association rule leaves an identity that
// does not resolve to the identity rule, which judges the edge's target through
// the instance that carries it and the record's target where it is named.
func TestNewFromSnapshot_ATargetTypeTheSchemaDropsIsTheIdentityRulesAlone(t *testing.T) {
	t.Parallel()
	load := func(target string) schema.Schema {
		s, res := schema.LoadString(t.Context(), `schema "links"

type Source {
	id String primary
	--> TO (many) `+target+`
}

type `+target+` {
	tid String primary
}
`, "links.yammm")
		if res.HasErrors() {
			t.Fatalf("load schema: %s", res)
		}
		return s
	}
	s := load("Target")
	v := instance.NewValidator(s)
	for _, c := range []struct {
		name  string
		held  bool
		where string
	}{
		{"an edge", true, "at types entry"},
		{"a target_missing record", false, "at unresolved target position"},
	} {
		g := graph.New(s)
		if c.held {
			target, res := v.ValidateOne(t.Context(), "Target", instance.RawInstance{Properties: map[string]any{"tid": "t1"}})
			if res.HasErrors() {
				t.Fatalf("validate target: %s", res)
			}
			mustOK(t, g.Add(t.Context(), target))
		}
		source, res := v.ValidateOne(t.Context(), "Source", instance.RawInstance{Properties: map[string]any{
			"id": "s1", "to": []any{map[string]any{"_target_tid": "t1"}},
		}})
		if res.HasErrors() {
			t.Fatalf("validate source: %s", res)
		}
		mustOK(t, g.Add(t.Context(), source))

		_, res = graph.NewFromSnapshot(load("Other"), g.Snapshot())
		if !res.HasCode(diag.E_GRAPH_TYPE_NOT_FOUND) || res.HasCode(diag.E_GRAPH_UNKNOWN_RELATION) {
			t.Errorf("%s: want the identity refusal alone, got: %s", c.name, res)
		}
		if !strings.Contains(res.String(), c.where) {
			t.Errorf("%s: no refusal stands %s: %s", c.name, c.where, res)
		}
	}
}
