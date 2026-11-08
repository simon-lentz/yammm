package graph_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
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

func loadEntry(t *testing.T, src string) schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), src, "entry.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

func typeIDOf(t *testing.T, s schema.Schema, name string) schema.TypeID {
	t.Helper()
	typ, ok := s.Type(name)
	if !ok {
		t.Fatalf("type %s not found", name)
	}
	return typ.ID()
}

func rootInstance(t *testing.T, s schema.Schema, name, key string) instance.ValidInstance {
	t.Helper()
	return instancetest.Valid(t, s, name, map[string]any{"id": key})
}

// requireRefused asserts the import is refused with a nil graph and an issue
// of code whose message holds phrase.
func requireRefused(t *testing.T, s schema.Schema, snap graph.Snapshot, code diag.Code, phrase string) {
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

// The types entry and each root carry the one identity the importing schema
// drops, which is one break.
func TestNewFromSnapshot_RefusesATypeTheImportingSchemaDoesNotDeclareOnce(t *testing.T) {
	t.Parallel()
	with := loadEntry(t, "schema \"entry\"\n\ntype Company {\n\tid String primary\n}\n\ntype Person {\n\tid String primary\n}\n")
	without := loadEntry(t, "schema \"entry\"\n\ntype Person {\n\tid String primary\n}\n")

	src := graph.New(with)
	for _, key := range []string{"c1", "c2"} {
		if r := src.Add(t.Context(), rootInstance(t, with, "Company", key)); !r.OK() {
			t.Fatalf("add: %s", r)
		}
	}
	_, res := graph.NewFromSnapshot(without, src.Snapshot())
	if n := res.Len(); n != 1 || !res.HasCode(diag.E_GRAPH_TYPE_NOT_FOUND) {
		t.Errorf("want one E_GRAPH_TYPE_NOT_FOUND, got %d issues: %s", n, res)
	}
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

// A record's target is its association's declared target, so an importing
// schema that drops the association is what leaves a record with no target: the
// import refuses it with the code Add gives data under an undeclared association.
func TestNewFromSnapshot_RefusesAnUnresolvedRecordUnderAnAssociationTheImportingSchemaDrops(t *testing.T) {
	t.Parallel()
	with := loadEntry(t, "schema \"entry\"\n\ntype Company {\n\tid String primary\n}\n\ntype Person {\n\tid String primary\n\t--> WORKS_AT (one) Company\n}\n")
	without := loadEntry(t, "schema \"entry\"\n\ntype Person {\n\tid String primary\n}\n")

	personID := typeIDOf(t, with, "Person")
	snap := rebuild(with, snapparts.Parts{
		Types: []schema.TypeID{personID},
		Instances: []snapparts.Instance{{
			TypeID:     personID,
			PrimaryKey: immutable.WrapKey([]any{"p1"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "p1"}),
		}},
		Unresolved: []snapparts.Unresolved{{
			SourceType: personID,
			SourceKey:  immutable.WrapKey([]any{"p1"}),
			Relation:   "WORKS_AT",
			TargetKey:  immutable.WrapKey([]any{"gone"}),
			Reason:     "target_missing",
		}},
	})
	requireRefused(t, without, snap, diag.E_GRAPH_UNKNOWN_RELATION, `under "WORKS_AT", which its type does not declare as an association`)
}

// A name the schema declares is shown whole, however long: only a name the
// data writes is cut.
func TestNewFromSnapshot_NamesADeclaredAssociationWhole(t *testing.T) {
	t.Parallel()
	long := "WORKS_AT_" + strings.Repeat("A", 31)
	const tmpl = "schema \"entry\"\n\ntype Company {\n%s}\n\ntype Person {\n\tid String primary\n\t--> %s (one) Company\n}\n"
	two := loadEntry(t, fmt.Sprintf(tmpl, "\ta String primary\n\tb String primary\n", long))
	one := loadEntry(t, fmt.Sprintf(tmpl, "\tid String primary\n", long))

	personID := typeIDOf(t, two, "Person")
	snap := rebuild(two, snapparts.Parts{
		Types: []schema.TypeID{personID},
		Instances: []snapparts.Instance{{
			TypeID:     personID,
			PrimaryKey: immutable.WrapKey([]any{"p1"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "p1"}),
		}},
		Unresolved: []snapparts.Unresolved{{
			SourceType: personID,
			SourceKey:  immutable.WrapKey([]any{"p1"}),
			Relation:   long,
			TargetKey:  immutable.WrapKey([]any{"x", "y"}),
			Reason:     "target_missing",
		}},
	})
	requireRefused(t, one, snap, diag.E_GRAPH_INVALID_PK, `under "`+long+`" carries a 2-part target key; the target declares 1`)
}

func TestNewFromSnapshot_RefusesTwoChildrenInASlotTheImportingSchemaMakesOne(t *testing.T) {
	t.Parallel()
	const tmpl = "schema \"entry\"\n\ntype Order {\n\tid String primary\n\t*-> LINES (%s) Line\n}\n\npart type Line {\n\tsku String\n}\n"
	many := loadEntry(t, strings.Replace(tmpl, "%s", "many", 1))
	one := loadEntry(t, strings.Replace(tmpl, "%s", "one", 1))

	orderID, lineID := typeIDOf(t, many, "Order"), typeIDOf(t, many, "Line")
	line := func(sku string) snapparts.Instance {
		return snapparts.Instance{
			TypeID:     lineID,
			Properties: immutable.WrapProperties(map[string]any{"sku": sku}),
		}
	}
	snap := rebuild(many, snapparts.Parts{
		Types: []schema.TypeID{orderID},
		Instances: []snapparts.Instance{{
			TypeID:     orderID,
			PrimaryKey: immutable.WrapKey([]any{"o1"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "o1"}),
			Composed:   map[string][]snapparts.Instance{"LINES": {line("a"), line("b")}},
		}},
	})
	requireRefused(t, one, snap, diag.E_DUPLICATE_COMPOSED_PK, `Order[["o1"]] composition "LINES": (one) cardinality violated, got 2 children`)
}

// Two keys the snapshot's schema holds apart can be one address under the
// importing schema's key constraint. The import installed one and dropped the
// other; it refuses them as the reader refuses two roots at one address.
func TestNewFromSnapshot_RefusesTwoRootsAtOneAddressUnderTheImportingSchema(t *testing.T) {
	t.Parallel()
	asString := loadEntry(t, "schema \"entry\"\n\ntype Run {\n\tat String primary\n}\n")
	asTimestamp := loadEntry(t, "schema \"entry\"\n\ntype Run {\n\tat Timestamp primary\n}\n")

	src := graph.New(asString)
	for _, at := range []string{"2030-01-01T00:00:00Z", "2030-01-01T00:00:00+00:00"} {
		inst := instancetest.Valid(t, asString, "Run", map[string]any{"at": at})
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
// reaches only through another import holds its root there by identity.
func TestNewFromSnapshot_KeepsARootTheImportingSchemaCannotName(t *testing.T) {
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
	src := graph.New(direct)
	if r := src.Add(ctx, instancetest.Valid(t, direct, "base.Beacon", map[string]any{"id": "b1"})); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	g, res := graph.NewFromSnapshot(through, src.Snapshot())
	if g == nil || !res.OK() {
		t.Fatalf("NewFromSnapshot: %s", res)
	}
	if n := len(slices.Collect(g.Snapshot().AllInstances())); n != 1 {
		t.Errorf("the imported graph holds %d roots, want 1", n)
	}
}

// A duplicate record's instance is held to the importing schema's names as a
// root is.
func TestNewFromSnapshot_RefusesAnUndeclaredNameOnADuplicate(t *testing.T) {
	t.Parallel()
	with := loadEntry(t, "schema \"entry\"\n\ntype P {\n\tid String primary\n\tnickname String\n}\n")
	without := loadEntry(t, "schema \"entry\"\n\ntype P {\n\tid String primary\n}\n")
	src := graph.New(with)
	src.Add(t.Context(), instancetest.Valid(t, with, "P", map[string]any{"id": "p1"}))
	src.Add(t.Context(), instancetest.Valid(t, with, "P", map[string]any{"id": "p1", "nickname": "x"}))
	if len(slices.Collect(src.Snapshot().Duplicates())) != 1 {
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
	src := graph.New(optional)
	src.Add(t.Context(), instancetest.Valid(t, optional, "P", map[string]any{"id": "p1", "at": map[string]any{"_target_id": "c9"}}))
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

func TestNewFromSnapshot_RefusesARootTheImportingSchemaGivesNoKey(t *testing.T) {
	t.Parallel()
	keyed := loadEntry(t, "schema \"entry\"\n\ntype Company {\n\tid String primary\n}\n")
	keyless := loadEntry(t, "schema \"entry\"\n\npart type Company {\n\tid String\n}\n\ntype H {\n\tid String primary\n\t*-> CS (many) Company\n}\n")
	src := graph.New(keyed)
	if r := src.Add(t.Context(), rootInstance(t, keyed, "Company", "c1")); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	requireRefused(t, keyless, src.Snapshot(), diag.E_GRAPH_MISSING_PK, "declares no primary key")
}

// The importing schema can declare a key of another arity than the stored one, a
// key where the stored part has none, and no key where the stored part has one.
func TestNewFromSnapshot_RefusesAStoredKeyOfAnotherArity(t *testing.T) {
	t.Parallel()
	const order = "schema \"entry\"\n\ntype Order {\n\tid String primary\n\t*-> LINES (many) Line\n}\n\npart type Line {\n\tsku String%s\n}\n"
	for _, c := range []struct {
		name, built, bound string
		typeName           string
		raw                map[string]any
		phrase             string
	}{
		{
			name:     "two components under one key property",
			built:    "schema \"entry\"\n\ntype P {\n\ta String primary\n\tb String primary\n}\n",
			bound:    "schema \"entry\"\n\ntype P {\n\ta String primary\n\tb String\n}\n",
			typeName: "P",
			raw:      map[string]any{"a": "x", "b": "y"},
			phrase:   "primary key has 2 components; type declares 1",
		},
		{
			name:     "a keyless part the importing schema gives a key",
			built:    fmt.Sprintf(order, ""),
			bound:    fmt.Sprintf(order, " primary"),
			typeName: "Order",
			raw:      map[string]any{"id": "o1", "LINES": []any{map[string]any{"sku": "a"}}},
			phrase:   "primary key is empty",
		},
		{
			name:     "a keyed part the importing schema makes keyless",
			built:    fmt.Sprintf(order, " primary"),
			bound:    fmt.Sprintf(order, ""),
			typeName: "Order",
			raw:      map[string]any{"id": "o1", "LINES": []any{map[string]any{"sku": "a"}}},
			phrase:   `composed child of type Line at ["a"]: its type declares no primary key, so it stores none`,
		},
		{
			name:     "a keyed part under a parent type the importing schema lacks",
			built:    fmt.Sprintf(order, " primary"),
			bound:    "schema \"entry\"\n\npart type Line {\n\tsku String\n}\n",
			typeName: "Order",
			raw:      map[string]any{"id": "o1", "LINES": []any{map[string]any{"sku": "a"}}},
			phrase:   `composed child of type Line at ["a"]: its type declares no primary key, so it stores none`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			snap := snapshotOf(t, loadEntry(t, c.built), c.typeName, c.raw)
			requireRefused(t, loadEntry(t, c.bound), snap, diag.E_GRAPH_INVALID_PK, c.phrase)
		})
	}
}

// An identity of a schema outside the importing closure is named with its schema
// path, so the refusal says which schema's type it is.
func TestNewFromSnapshot_NamesAnIdentityOfAnotherSchemaByItsPath(t *testing.T) {
	t.Parallel()
	const src = "schema \"entry\"\n\ntype P {\n\tid String primary\n}\n"
	other, res := schema.LoadString(t.Context(), src, "other.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	snap := snapshotOf(t, other, "P", map[string]any{"id": "p1"})
	requireRefused(t, loadEntry(t, src), snap, diag.E_GRAPH_TYPE_NOT_FOUND, "other.yammm")
}

// The identity rule walks below an instance whose own identity the importing
// schema drops, so a child's identity is one break of its own.
func TestNewFromSnapshot_WalksBelowAnIdentityTheImportingSchemaDrops(t *testing.T) {
	t.Parallel()
	built := loadEntry(t, "schema \"entry\"\n\ntype Order {\n\tid String primary\n\t*-> ITEMS (_:many) Item\n}\n\npart type Item {\n\tcode String primary\n}\n")
	bound := loadEntry(t, "schema \"entry\"\n\ntype Keep {\n\tid String primary\n}\n")
	snap := snapshotOf(t, built, "Order", map[string]any{"id": "o1", "ITEMS": []any{map[string]any{"code": "a"}}})
	requireRefused(t, bound, snap, diag.E_GRAPH_TYPE_NOT_FOUND, "at composed child position")
}

// stampedChildSchema declares a keyed part whose key is of the given kind.
func stampedChildSchema(t *testing.T, kind string) schema.Schema {
	t.Helper()
	return loadEntry(t, fmt.Sprintf("schema \"entry\"\n\ntype Run {\n\tid String primary\n\t*-> STAMPS (_:many) Stamp\n}\n\npart type Stamp {\n\tat %s primary\n}\n", kind))
}

// Two keys the snapshot's schema holds apart can be one key under the importing
// schema's constraint, and a (many) slot holds one child at a key.
func TestNewFromSnapshot_RefusesTwoSiblingsAtOneKeyUnderTheImportingSchema(t *testing.T) {
	t.Parallel()
	snap := snapshotOf(t, stampedChildSchema(t, "String"), "Run", map[string]any{"id": "r1", "STAMPS": []any{
		map[string]any{"at": canonInstant}, map[string]any{"at": rawInstant},
	}})
	requireRefused(t, stampedChildSchema(t, "Timestamp"), snap, diag.E_DUPLICATE_COMPOSED_PK, "holds children 0 and 1 at key")
}

// A record the key rule refuses holds no address, whichever stands first: a child
// repeats no sibling's key, and a root is no second root. Each input draws the key
// rule's issue alone.
func TestNewFromSnapshot_ARefusedKeyHoldsNoAddress(t *testing.T) {
	t.Parallel()
	const later = "2030-01-01T00:00:00Z"
	for _, c := range []struct {
		name             string
		firstAt, lastAt  string
		firstKey, lastKy string
	}{
		{"the first refused", later, canonInstant, rawInstant, canonInstant},
		{"the last refused", canonInstant, later, rawInstant, canonInstant},
	} {
		t.Run("children, "+c.name, func(t *testing.T) {
			t.Parallel()
			built, bound := stampedChildSchema(t, "String"), stampedChildSchema(t, "Timestamp")
			run, stamp := typeIDOf(t, built, "Run"), typeIDOf(t, built, "Stamp")
			child := func(key, at string) snapparts.Instance {
				return snapparts.Instance{TypeID: stamp, PrimaryKey: immutable.WrapKey([]any{key}), Properties: immutable.WrapProperties(map[string]any{"at": at})}
			}
			snap := rebuild(built, snapparts.Parts{Instances: []snapparts.Instance{{
				TypeID: run, PrimaryKey: immutable.WrapKey([]any{"r1"}), Properties: immutable.WrapProperties(map[string]any{"id": "r1"}),
				Composed: map[string][]snapparts.Instance{"STAMPS": {child(c.firstKey, c.firstAt), child(c.lastKy, c.lastAt)}},
			}}})
			_, res := graph.NewFromSnapshot(bound, snap)
			if failures(res) != 1 || !res.HasCode(diag.E_GRAPH_INVALID_PK) {
				t.Errorf("%s, want one E_GRAPH_INVALID_PK", res)
			}
		})
		t.Run("roots, "+c.name, func(t *testing.T) {
			t.Parallel()
			built, bound := instantKeySchema(t, "String"), instantKeySchema(t, "Timestamp")
			run := typeIDOf(t, built, "Run")
			root := func(key, at string) snapparts.Instance {
				return snapparts.Instance{TypeID: run, PrimaryKey: immutable.WrapKey([]any{key}), Properties: immutable.WrapProperties(map[string]any{"at": at})}
			}
			snap := rebuild(built, snapparts.Parts{Instances: []snapparts.Instance{root(c.firstKey, c.firstAt), root(c.lastKy, c.lastAt)}})
			_, res := graph.NewFromSnapshot(bound, snap)
			if failures(res) != 1 || !res.HasCode(diag.E_GRAPH_INVALID_PK) {
				t.Errorf("%s, want one E_GRAPH_INVALID_PK", res)
			}
		})
	}
}

// Keyless parts have no key to repeat, so a (many) slot holds any number of them.
func TestNewFromSnapshot_KeepsKeylessSiblings(t *testing.T) {
	t.Parallel()
	const src = "schema \"entry\"\n\ntype Order {\n\tid String primary\n\t*-> LINES (many) Line\n}\n\npart type Line {\n\tsku String\n}\n"
	built, bound := loadEntry(t, src), loadEntry(t, src)
	snap := snapshotOf(t, built, "Order", map[string]any{"id": "o1", "LINES": []any{map[string]any{"sku": "a"}, map[string]any{"sku": "a"}}})
	order := slices.Collect(mustImport(t, bound, snap).Snapshot().InstancesOf(typeIDOf(t, bound, "Order")))[0]
	if n := order.ComposedCount("LINES"); n != 2 {
		t.Errorf("%d children, want 2", n)
	}
}

// The import re-keys a composed child under the importing schema, as it does a
// root.
func TestNewFromSnapshot_RekeysAComposedChild(t *testing.T) {
	t.Parallel()
	bound := stampedChildSchema(t, "Timestamp")
	snap := snapshotOf(t, stampedChildSchema(t, "String"), "Run", map[string]any{"id": "r1", "STAMPS": []any{map[string]any{"at": rawInstant}}})
	root := slices.Collect(mustImport(t, bound, snap).Snapshot().InstancesOf(typeIDOf(t, bound, "Run")))[0]
	child := slices.Collect(root.Composed("STAMPS"))[0]
	if got, want := child.PrimaryKey().String(), graph.FormatKey(canonInstant); got != want {
		t.Errorf("child key = %s, want %s", got, want)
	}
}

// A key component and the property the importing schema makes its key are held
// to one kind: the integer 1 and the float 1.0 render alike as the key [1], and
// a float is never an integer.
func TestNewFromSnapshot_AKeyComponentAgreesWithItsPropertyByKind(t *testing.T) {
	t.Parallel()
	built := loadEntry(t, "schema \"entry\"\n\ntype T {\n\tid String primary\n\tcode Float\n}\n")
	bound := loadEntry(t, "schema \"entry\"\n\ntype T {\n\tid String\n\tcode String primary\n}\n")
	id := typeIDOf(t, built, "T")
	snapOf := func(code json.Number) graph.Snapshot {
		return snappartstest.Loaded(t, built, snapparts.Parts{Instances: []snapparts.Instance{{
			TypeID: id, PrimaryKey: immutable.WrapKey([]any{json.Number("1")}),
			Properties: immutable.WrapProperties(map[string]any{"id": json.Number("1"), "code": code}),
		}}})
	}
	requireRefused(t, bound, snapOf("1.0"), diag.E_GRAPH_INVALID_PK, `primary key component 0 disagrees with property "code" ([1] vs [1.0])`)
	requireRefused(t, bound, snapOf("1"), diag.E_GRAPH_INVALID_PK, `primary key component 0 disagrees with property "code" ([1] vs [1.0])`)
}

// A disagreement's message marks each whole float in a list as a float, as it
// marks a float at the top: [1.0, 2.5] is no list of integers.
func TestNewFromSnapshot_ADisagreementMarksAFloatInAList(t *testing.T) {
	t.Parallel()
	built := loadEntry(t, "schema \"entry\"\n\ntype T {\n\tid String primary\n\tcode List<Float>\n}\n")
	bound := loadEntry(t, "schema \"entry\"\n\ntype T {\n\tid String\n\tcode String primary\n}\n")
	snap := snappartstest.Loaded(t, built, snapparts.Parts{Instances: []snapparts.Instance{{
		TypeID: typeIDOf(t, built, "T"), PrimaryKey: immutable.WrapKey([]any{"x"}),
		Properties: immutable.WrapProperties(map[string]any{"id": "x", "code": []any{json.Number("1"), json.Number("2.5")}}),
	}}})
	requireRefused(t, bound, snap, diag.E_GRAPH_INVALID_PK, `primary key component 0 disagrees with property "code" (["x"] vs [[1.0,2.5]])`)
}
