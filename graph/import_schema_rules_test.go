package graph_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// Importing a snapshot into a schema that does not hold its identities.
//
// Each pair below shares one source id, so every identity the first schema
// mints has the schema path the second resolves; what differs is whether the
// second declares the type, or declares it as something that cannot hold what
// the snapshot holds. The import reports the code Graph.Add refuses the same
// fact with: a snapshot the caller binds to another schema is the caller's
// input, not a broken invariant.

func loadEntry(t *testing.T, src string) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), src, "entry.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

func typeIDOf(t *testing.T, s *schema.Schema, name string) schema.TypeID {
	t.Helper()
	typ, ok := s.Type(name)
	if !ok {
		t.Fatalf("type %s not found", name)
	}
	return typ.ID()
}

func rootInstance(t *testing.T, s *schema.Schema, name, key string) *instance.ValidInstance {
	t.Helper()
	return instance.NewValidInstance(name, typeIDOf(t, s, name),
		immutable.WrapKey([]any{key}), immutable.WrapProperties(map[string]any{"id": key}), nil, nil, nil)
}

// requireRefused asserts the import is refused with a nil graph and an issue
// of code whose message holds phrase, and that the seeded assembler refuses
// the same snapshot.
func requireRefused(t *testing.T, s *schema.Schema, snap *graph.Snapshot, code diag.Code, phrase string) {
	t.Helper()
	g, res := graph.NewFromSnapshot(s, snap)
	if g != nil {
		t.Error("a refused import returned a graph")
	}
	found := false
	for issue := range res.Errors() {
		if issue.Code() == code && strings.Contains(issue.Message(), phrase) {
			found = true
		}
	}
	if !found {
		t.Errorf("NewFromSnapshot result = %s, want %s naming %q", res, code, phrase)
	}
	ba, bres := graph.NewBatchAssemblerFromSnapshot(t.Context(), s, snap)
	if ba != nil || !bres.HasErrors() {
		t.Errorf("NewBatchAssemblerFromSnapshot accepted what NewFromSnapshot refuses: %s", bres)
	}
}

// Without the check, the graph held Company, snapshot.Marshal wrote it and
// snapshot.Load refused the document as naming a type the closure lacks.
func TestNewFromSnapshot_RefusesATypeTheImportingSchemaDoesNotDeclare(t *testing.T) {
	t.Parallel()
	with := loadEntry(t, "schema \"entry\"\n\ntype Company {\n\tid String primary\n}\n\ntype Person {\n\tid String primary\n}\n")
	without := loadEntry(t, "schema \"entry\"\n\ntype Person {\n\tid String primary\n}\n")

	src := graph.New(with)
	if r := src.Add(t.Context(), rootInstance(t, with, "Company", "c1")); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	requireRefused(t, without, src.Snapshot(), diag.E_GRAPH_TYPE_NOT_FOUND, "at types entry")
}

func TestNewFromSnapshot_RefusesARootTheImportingSchemaMakesAbstract(t *testing.T) {
	t.Parallel()
	concrete := loadEntry(t, "schema \"entry\"\n\ntype Company {\n\tid String primary\n}\n")
	abstract := loadEntry(t, "schema \"entry\"\n\nabstract type Company {\n\tid String primary\n}\n")

	src := graph.New(concrete)
	if r := src.Add(t.Context(), rootInstance(t, concrete, "Company", "c1")); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	requireRefused(t, abstract, src.Snapshot(), diag.E_GRAPH_ABSTRACT_TYPE, "which is abstract")
}

func TestNewFromSnapshot_RefusesAnUnresolvedTargetTheImportingSchemaDoesNotDeclare(t *testing.T) {
	t.Parallel()
	with := loadEntry(t, "schema \"entry\"\n\ntype Company {\n\tid String primary\n}\n\ntype Person {\n\tid String primary\n\t--> WORKS_AT (one) Company\n}\n")
	without := loadEntry(t, "schema \"entry\"\n\ntype Person {\n\tid String primary\n}\n")

	personID := typeIDOf(t, with, "Person")
	snap, res := graph.RebuildSnapshot(with, graph.SnapshotParts{
		Types: []schema.TypeID{personID},
		Instances: []graph.InstanceParts{{
			TypeID:     personID,
			PrimaryKey: immutable.WrapKey([]any{"p1"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "p1"}),
		}},
		Unresolved: []graph.UnresolvedParts{{
			SourceType: personID,
			SourceKey:  immutable.WrapKey([]any{"p1"}),
			Relation:   "WORKS_AT",
			TargetType: typeIDOf(t, with, "Company"),
			TargetKey:  immutable.WrapKey([]any{"gone"}),
			Reason:     "target_missing",
		}},
	})
	if res.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", res)
	}
	requireRefused(t, without, snap, diag.E_GRAPH_TYPE_NOT_FOUND, "at unresolved target position")
}

func TestNewFromSnapshot_RefusesTwoChildrenInASlotTheImportingSchemaMakesOne(t *testing.T) {
	t.Parallel()
	const tmpl = "schema \"entry\"\n\ntype Order {\n\tid String primary\n\t*-> LINES (%s) Line\n}\n\npart type Line {\n\tsku String\n}\n"
	many := loadEntry(t, strings.Replace(tmpl, "%s", "many", 1))
	one := loadEntry(t, strings.Replace(tmpl, "%s", "one", 1))

	orderID, lineID := typeIDOf(t, many, "Order"), typeIDOf(t, many, "Line")
	line := func(sku string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeID:     lineID,
			Properties: immutable.WrapProperties(map[string]any{"sku": sku}),
		}
	}
	snap, res := graph.RebuildSnapshot(many, graph.SnapshotParts{
		Types: []schema.TypeID{orderID},
		Instances: []graph.InstanceParts{{
			TypeID:     orderID,
			PrimaryKey: immutable.WrapKey([]any{"o1"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "o1"}),
			Composed:   map[string][]graph.InstanceParts{"LINES": {line("a"), line("b")}},
		}},
	})
	if res.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", res)
	}
	requireRefused(t, one, snap, diag.E_DUPLICATE_COMPOSED_PK, "(one) cardinality violated")
}

// Two keys the snapshot's schema holds apart can be one address under the
// importing schema's key constraint. The import installed one and dropped the
// other; it refuses them as RebuildSnapshot refuses two parts at one address.
func TestNewFromSnapshot_RefusesTwoRootsAtOneAddressUnderTheImportingSchema(t *testing.T) {
	t.Parallel()
	asString := loadEntry(t, "schema \"entry\"\n\ntype Run {\n\tat String primary\n}\n")
	asTimestamp := loadEntry(t, "schema \"entry\"\n\ntype Run {\n\tat Timestamp primary\n}\n")

	src := graph.New(asString)
	for _, at := range []string{"2030-01-01T00:00:00Z", "2030-01-01T00:00:00+00:00"} {
		inst := instance.NewValidInstance("Run", typeIDOf(t, asString, "Run"), immutable.WrapKey([]any{at}),
			immutable.WrapProperties(map[string]any{"at": at}), nil, nil, nil)
		if r := src.Add(t.Context(), inst); !r.OK() {
			t.Fatalf("add %s: %s", at, r)
		}
	}
	requireRefused(t, asTimestamp, src.Snapshot(), diag.E_DUPLICATE_PK, "two instances of type")
}

func TestNewFromSnapshot_RefusesARootTheImportingSchemaMakesAPart(t *testing.T) {
	t.Parallel()
	root := loadEntry(t, "schema \"entry\"\n\ntype B {\n\tid String primary\n}\n")
	part := loadEntry(t, "schema \"entry\"\n\npart type B {\n\tid String primary\n}\n\ntype H {\n\tid String primary\n\t*-> BS (many) B\n}\n")
	src := graph.New(root)
	if r := src.Add(t.Context(), rootInstance(t, root, "B", "b1")); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	requireRefused(t, part, src.Snapshot(), diag.E_GRAPH_INVALID_COMPOSITION, "is a part type")
}

// A type the snapshot's schema imports directly and the importing schema
// reaches only through another import cannot hold a root there.
func TestNewFromSnapshot_RefusesARootTheImportingSchemaCannotName(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	base := "schema \"base\"\n\ntype Beacon {\n\tid String primary\n}\n"
	direct, r1 := schema.LoadSourcesWithEntry(ctx, map[string][]byte{
		"entry.yammm": []byte("schema \"entry\"\n\nimport \"base.yammm\" as base\n\ntype E {\n\tid String primary\n}\n"),
		"base.yammm":  []byte(base),
	}, "entry.yammm", ".", schema.WithSourcesOnly(true))
	through, r2 := schema.LoadSourcesWithEntry(ctx, map[string][]byte{
		"entry.yammm": []byte("schema \"entry\"\n\nimport \"mid.yammm\" as mid\n\ntype E {\n\tid String primary\n}\n"),
		"mid.yammm":   []byte("schema \"mid\"\n\nimport \"base.yammm\" as base\n\ntype M {\n\tid String primary\n}\n"),
		"base.yammm":  []byte(base),
	}, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if r1.HasErrors() || r2.HasErrors() {
		t.Fatal(r1, r2)
	}
	b, _ := direct.ImportByAlias("base")
	beacon, _ := b.Schema().Type("Beacon")
	src := graph.New(direct)
	if r := src.Add(ctx, instance.NewValidInstance("base.Beacon", beacon.ID(), immutable.WrapKey([]any{"b1"}),
		immutable.WrapProperties(map[string]any{"id": "b1"}), nil, nil, nil)); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	requireRefused(t, through, src.Snapshot(), diag.E_GRAPH_TYPE_NOT_FOUND, "reachable only through an intermediate import")
}

// A duplicate record's instance is held to the importing schema's names as a
// root is.
func TestNewFromSnapshot_RefusesAnUndeclaredNameOnADuplicate(t *testing.T) {
	t.Parallel()
	with := loadEntry(t, "schema \"entry\"\n\ntype P {\n\tid String primary\n\tnickname String\n}\n")
	without := loadEntry(t, "schema \"entry\"\n\ntype P {\n\tid String primary\n}\n")
	src := graph.New(with)
	p := typeIDOf(t, with, "P")
	src.Add(t.Context(), instance.NewValidInstance("P", p, immutable.WrapKey([]any{"p1"}), immutable.WrapProperties(map[string]any{"id": "p1"}), nil, nil, nil))
	src.Add(t.Context(), instance.NewValidInstance("P", p, immutable.WrapKey([]any{"p1"}), immutable.WrapProperties(map[string]any{"id": "p1", "nickname": "x"}), nil, nil, nil))
	if len(src.Snapshot().Duplicates()) != 1 {
		t.Fatal("fixture is vacuous: no duplicate")
	}
	requireRefused(t, without, src.Snapshot(), diag.E_UNKNOWN_FIELD, `duplicate instance`)
}

// Whether an unresolved record's association is required is the importing
// schema's rule, not the snapshot's.
func TestNewFromSnapshot_DerivesRequiredUnderTheImportingSchema(t *testing.T) {
	t.Parallel()
	optional := loadEntry(t, "schema \"entry\"\n\ntype C {\n\tid String primary\n}\n\ntype P {\n\tid String primary\n\t--> AT (_:one) C\n}\n")
	required := loadEntry(t, "schema \"entry\"\n\ntype C {\n\tid String primary\n}\n\ntype P {\n\tid String primary\n\t--> AT (one:one) C\n}\n")
	p := typeIDOf(t, optional, "P")
	src := graph.New(optional)
	src.Add(t.Context(), instance.NewValidInstance("P", p, immutable.WrapKey([]any{"p1"}), immutable.WrapProperties(map[string]any{"id": "p1"}),
		map[string]*instance.ValidEdgeData{"AT": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{"c9"}), immutable.Properties{}),
		})}, nil, nil))
	if res := src.Check(t.Context()); res.HasErrors() {
		t.Fatalf("control: an optional unresolved record failed Check: %s", res)
	}
	g, res := graph.NewFromSnapshot(required, src.Snapshot())
	if res.HasErrors() {
		t.Fatalf("import: %s", res)
	}
	if !g.Check(t.Context()).HasCode(diag.E_UNRESOLVED_REQUIRED) {
		t.Error("the import kept the snapshot's optional rule for an association the importing schema requires")
	}
}

// A duplicate record's type and key are its instance's: Graph.Add records the
// rejected instance under its own, and the root rule judges the record's type.
func TestRebuildSnapshot_RefusesADuplicateRecordItsInstanceContradicts(t *testing.T) {
	t.Parallel()
	s := loadEntry(t, "schema \"entry\"\n\nabstract type Thing {\n\tid String primary\n}\n\ntype P {\n\tid String primary\n}\n")
	p, thing := typeIDOf(t, s, "P"), typeIDOf(t, s, "Thing")
	p1 := graph.InstanceParts{TypeID: p, PrimaryKey: immutable.WrapKey([]any{"p1"}), Properties: immutable.WrapProperties(map[string]any{"id": "p1"})}
	for name, dp := range map[string]graph.DuplicateParts{
		"another type": {
			Type: p, Key: immutable.WrapKey([]any{"p1"}), ConflictType: p, ConflictKey: immutable.WrapKey([]any{"p1"}),
			Instance: graph.InstanceParts{TypeID: thing, PrimaryKey: immutable.WrapKey([]any{"p1"}), Properties: immutable.WrapProperties(map[string]any{"id": "p1"})},
		},
		"another key": {Type: p, Key: immutable.WrapKey([]any{"p2"}), ConflictType: p, ConflictKey: immutable.WrapKey([]any{"p1"}), Instance: p1},
	} {
		_, res := graph.RebuildSnapshot(s, graph.SnapshotParts{Types: []schema.TypeID{p}, Instances: []graph.InstanceParts{p1}, Duplicates: []graph.DuplicateParts{dp}})
		if !res.HasCode(diag.E_INTERNAL) || !strings.Contains(res.String(), "duplicate record 0 states") {
			t.Errorf("%s: RebuildSnapshot = %s, want the record refused", name, res)
		}
	}
}
