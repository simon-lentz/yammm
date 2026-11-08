package graph_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// employerSchema has a Person naming its Company through an EMPLOYER
// association, (one) when required is set and (_:one) otherwise.
func employerSchema(t *testing.T, required bool) schema.Schema {
	t.Helper()
	multiplicity := "_:one"
	if required {
		multiplicity = "one"
	}
	s, result := schema.LoadString(t.Context(), `schema "employer"

type Company {
	id String primary
	name String required
}

type Person {
	id String primary
	name String required
	--> EMPLOYER (`+multiplicity+`) Company
}
`, "employer.yammm")
	if result.HasErrors() {
		t.Fatalf("schema: %s", result)
	}
	return s
}

// employerRecord is one raw record of employerSchema: a type name and its properties.
type employerRecord struct {
	typeName string
	props    map[string]any
}

func company(id string) employerRecord {
	return employerRecord{"Company", map[string]any{"id": id, "name": id}}
}

func person(id string) employerRecord {
	return employerRecord{"Person", map[string]any{"id": id, "name": id}}
}

func personAt(id, employer string) employerRecord {
	return employerRecord{"Person", map[string]any{"id": id, "name": id, "employer": map[string]any{"_target_id": employer}}}
}

// addRecords validates each record and adds it to g, returning the result of
// the last add; it fails the test on a validation error or an earlier refusal.
func addRecords(t *testing.T, g *graph.Graph, s schema.Schema, records ...employerRecord) diag.Result {
	t.Helper()
	v := instance.NewValidator(s)
	var res diag.Result
	for i, rec := range records {
		valid, vRes := v.ValidateOne(t.Context(), rec.typeName, instance.RawInstance{Properties: rec.props})
		if vRes.HasErrors() || valid.IsZero() {
			t.Fatalf("validate %s %v: %s", rec.typeName, rec.props["id"], vRes)
		}
		res = g.Add(t.Context(), valid)
		if res.HasErrors() && i < len(records)-1 {
			t.Fatalf("add %s %v: %s", rec.typeName, rec.props["id"], res)
		}
	}
	return res
}

// seedOf adds each record to a new graph and returns its snapshot with no
// Check, the shape of a mid-batch checkpoint.
func seedOf(t *testing.T, s schema.Schema, records ...employerRecord) graph.Snapshot {
	t.Helper()
	g := graph.New(s)
	if len(records) > 0 {
		if res := addRecords(t, g, s, records...); res.HasErrors() {
			t.Fatalf("seed: %s", res)
		}
	}
	return g.Snapshot()
}

func count[T any](seq func(func(T) bool)) int {
	return len(slices.Collect(seq))
}

// An add to a seeded graph resolves against the seeded roots, and the seed's
// snapshot does not change.
func TestNewFromSnapshot_AnAddResolvesAgainstTheSeed(t *testing.T) {
	t.Parallel()
	s := employerSchema(t, false)
	seed := seedOf(t, s, company("acme"), person("alice"))

	g := mustImport(t, s, seed)
	if res := addRecords(t, g, s, personAt("bob", "acme")); res.HasErrors() {
		t.Fatalf("add bob: %s", res)
	}
	if res := g.Check(t.Context()); res.HasErrors() {
		t.Fatalf("Check: %s", res)
	}
	snap := g.Snapshot()
	if n := count(snap.InstancesOf(mustTypeID(t, s, "Person"))); n != 2 {
		t.Errorf("%d persons, want the seeded alice and the added bob", n)
	}
	edges := slices.Collect(snap.Edges())
	if len(edges) != 1 || edges[0].Relation() != "EMPLOYER" ||
		edges[0].Source().PrimaryKey().String() != `["bob"]` || edges[0].Target().PrimaryKey().String() != `["acme"]` {
		t.Errorf(`edges %v, want one EMPLOYER ["bob"]->["acme"]`, edges)
	}
	if n := count(seed.InstancesOf(mustTypeID(t, s, "Person"))); n != 1 {
		t.Errorf("the seed holds %d persons after the add, want 1", n)
	}
	if n := count(seed.Edges()); n != 0 {
		t.Errorf("the seed holds %d edges after the add, want 0", n)
	}
}

// An add supplies the target of a required record the seed left unresolved, so
// the record becomes an edge and Check passes.
func TestNewFromSnapshot_AnAddResolvesASeededRecord(t *testing.T) {
	t.Parallel()
	s := employerSchema(t, true)
	seed := seedOf(t, s, personAt("alice", "acme"))
	if n := count(seed.Unresolved()); n != 1 {
		t.Fatalf("the seed holds %d unresolved records, want 1", n)
	}

	g := mustImport(t, s, seed)
	if res := addRecords(t, g, s, company("acme")); res.HasErrors() {
		t.Fatalf("add acme: %s", res)
	}
	if res := g.Check(t.Context()); res.HasErrors() {
		t.Fatalf("Check: %s; the add supplied the missing target", res)
	}
	snap := g.Snapshot()
	if count(snap.Edges()) != 1 || count(snap.Unresolved()) != 0 {
		t.Errorf("%d edges and %d unresolved records, want 1 and 0", count(snap.Edges()), count(snap.Unresolved()))
	}
}

// A required record the seed left unresolved fails Check when no add supplies
// its target: Check judges seeded and added state alike.
func TestNewFromSnapshot_ASeededRecordFailsCheck(t *testing.T) {
	t.Parallel()
	s := employerSchema(t, true)
	g := mustImport(t, s, seedOf(t, s, personAt("alice", "missing")))
	if res := g.Check(t.Context()); !res.HasCode(diag.E_UNRESOLVED_REQUIRED) {
		t.Errorf("Check = %s, want E_UNRESOLVED_REQUIRED from the seeded record", res)
	}
	if n := count(g.Snapshot().Unresolved()); n != 1 {
		t.Errorf("%d unresolved records, want 1", n)
	}
}

// An add at a seeded root's key is a duplicate: the call's result holds
// E_DUPLICATE_PK, the snapshot records the duplicate, and Check does not judge
// it.
func TestNewFromSnapshot_AnAddAtASeededKeyIsADuplicate(t *testing.T) {
	t.Parallel()
	s := employerSchema(t, false)
	g := mustImport(t, s, seedOf(t, s, person("alice")))

	res := addRecords(t, g, s, employerRecord{"Person", map[string]any{"id": "alice", "name": "Alice the Second"}})
	if !res.HasCode(diag.E_DUPLICATE_PK) {
		t.Fatalf("add at a seeded key = %s, want E_DUPLICATE_PK", res)
	}
	if res := g.Check(t.Context()); res.HasErrors() {
		t.Fatalf("Check: %s; a duplicate is no Check finding", res)
	}
	snap := g.Snapshot()
	if count(snap.InstancesOf(mustTypeID(t, s, "Person"))) != 1 || count(snap.Duplicates()) != 1 {
		t.Errorf("%d persons and %d duplicates, want the seeded alice and one duplicate",
			count(snap.InstancesOf(mustTypeID(t, s, "Person"))), count(snap.Duplicates()))
	}
}

// A batch written to the wire and loaded seeds a graph that completes the
// batch, and that graph's snapshot writes in turn.
func TestNewFromSnapshot_ALoadedSnapshotSeedsAResume(t *testing.T) {
	t.Parallel()
	s := employerSchema(t, false)
	ctx := t.Context()
	first := graph.New(s)
	if res := addRecords(t, first, s, company("acme"), personAt("alice", "acme")); res.HasErrors() {
		t.Fatalf("first run: %s", res)
	}
	data, res := snapshot.Marshal(ctx, first.Snapshot())
	if res.HasErrors() {
		t.Fatalf("Marshal: %s", res)
	}
	loaded, res := snapshot.Load(ctx, data, s)
	if res.HasErrors() {
		t.Fatalf("Load: %s", res)
	}

	g := mustImport(t, s, loaded)
	if res := addRecords(t, g, s, personAt("bob", "acme")); res.HasErrors() {
		t.Fatalf("second run: %s", res)
	}
	if res := g.Check(ctx); res.HasErrors() {
		t.Fatalf("Check: %s", res)
	}
	snap := g.Snapshot()
	if count(snap.InstancesOf(mustTypeID(t, s, "Person"))) != 2 || count(snap.Edges()) != 2 {
		t.Errorf("%d persons and %d edges, want 2 and 2: alice's edge survives the round trip and bob's resolves against the loaded acme",
			count(snap.InstancesOf(mustTypeID(t, s, "Person"))), count(snap.Edges()))
	}
	if _, res := snapshot.Marshal(ctx, snap); res.HasErrors() {
		t.Errorf("Marshal of the resumed batch: %s", res)
	}
}
