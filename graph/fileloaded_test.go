package graph_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// loadFromDisk loads testdata/composed.yammm through [schema.Load]. It is the
// package's only file-backed schema: every other test builds one from a
// synthetic source ID, whose [schema.TypeID] renders no filesystem path and so
// cannot exercise anything that leaks one.
func loadFromDisk(t *testing.T) *schema.Schema {
	t.Helper()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)
	path, err := filepath.Abs(filepath.Join("testdata", "composed.yammm"))
	if err != nil {
		t.Fatalf("abs testdata path: %v", err)
	}
	s, res := schema.Load(t.Context(), path)
	if res.HasErrors() {
		t.Fatalf("load %s: %s", path, res.String())
	}
	return s
}

// TestFileLoadedSchema_RendersAnAbsolutePath is the control. It asserts the
// property that makes the tests below meaningful: loaded from disk, this
// schema's type identities carry the checkout's path. If this ever fails the
// corpus has stopped being a file-backed one and the guards below prove
// nothing.
func TestFileLoadedSchema_RendersAnAbsolutePath(t *testing.T) {
	t.Parallel()
	s := loadFromDisk(t)
	typ, ok := s.Type("Order")
	if !ok {
		t.Fatal("Order not found")
	}
	if !filepath.IsAbs(typ.ID().SchemaPath().String()) {
		t.Fatalf("schema path %q is not absolute; this corpus is no longer file-backed",
			typ.ID().SchemaPath().String())
	}
}

// TestDuplicateComposedPK_DetailIsAPrimaryKey pins the contract
// [diag.DetailKeyPrimaryKey]'s own godoc states -- "the primary key value".
//
// The duplicate is two Notes sharing a note_id under one Section, so the
// reporting site is reached through the inline builder's recursion at depth
// two. A detail that instead carried a writer's composed address would be
// wrong twice over: rooted at the Section rather than the Order, and bearing
// this checkout's absolute path.
func TestDuplicateComposedPK_DetailIsAPrimaryKey(t *testing.T) {
	t.Parallel()
	s := loadFromDisk(t)
	ctx := t.Context()

	note := func(id string) *instance.ValidInstance {
		return instancetest.VI(
			"Note",
			instancetest.TypeID(mustTypeID(t, s, "Note")),
			instancetest.PK(id),
			instancetest.Props(map[string]any{"note_id": id, "body": "b"}),
		)
	}
	section := instancetest.VI(
		"Section",
		instancetest.TypeID(mustTypeID(t, s, "Section")),
		instancetest.PK("s1"),
		instancetest.Props(map[string]any{"section_id": "s1", "heading": "h"}),
		instancetest.Composed(map[string]immutable.Value{
			"NOTES": immutable.Wrap([]any{note("n1"), note("n1")}),
		}),
	)
	order := instancetest.VI(
		"Order",
		instancetest.TypeID(mustTypeID(t, s, "Order")),
		instancetest.PK("o1"),
		instancetest.Props(map[string]any{"order_id": "o1", "customer": "c"}),
		instancetest.Composed(map[string]immutable.Value{
			"SECTIONS": immutable.Wrap([]any{section}),
		}),
	)

	res := graph.New(s).Add(ctx, order)
	if res.OK() {
		t.Fatal("two sibling Notes sharing a primary key were accepted")
	}

	var got string
	var found bool
	for issue := range res.Issues() {
		if issue.Code() != diag.E_DUPLICATE_COMPOSED_PK {
			continue
		}
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyPrimaryKey {
				got, found = d.Value, true
			}
		}
	}
	if !found {
		t.Fatal("E_DUPLICATE_COMPOSED_PK carried no primary_key detail")
	}
	if want := graph.FormatKey("n1"); got != want {
		t.Errorf("primary_key detail = %q, want the duplicated key %q", got, want)
	}
}

// TestDiagnosticDetails_CarryNoSchemaPath guards the class rather than the one
// site: a detail whose key promises a key, a name or a field must not carry
// the loading machine's filesystem layout. [diag.DetailKeyTypeSchema] is
// exempt by contract -- naming the schema a type came from is what it is for.
func TestDiagnosticDetails_CarryNoSchemaPath(t *testing.T) {
	t.Parallel()
	s := loadFromDisk(t)
	root := filepath.Dir(mustTypeID(t, s, "Order").SchemaPath().String())

	note := func(id string) *instance.ValidInstance {
		return instancetest.VI(
			"Note",
			instancetest.TypeID(mustTypeID(t, s, "Note")),
			instancetest.PK(id),
			instancetest.Props(map[string]any{"note_id": id, "body": "b"}),
		)
	}
	section := instancetest.VI(
		"Section",
		instancetest.TypeID(mustTypeID(t, s, "Section")),
		instancetest.PK("s1"),
		instancetest.Props(map[string]any{"section_id": "s1", "heading": "h"}),
		instancetest.Composed(map[string]immutable.Value{
			"NOTES": immutable.Wrap([]any{note("n1"), note("n1")}),
		}),
	)
	order := instancetest.VI(
		"Order",
		instancetest.TypeID(mustTypeID(t, s, "Order")),
		instancetest.PK("o1"),
		instancetest.Props(map[string]any{"order_id": "o1", "customer": "c"}),
		instancetest.Composed(map[string]immutable.Value{
			"SECTIONS": immutable.Wrap([]any{section}),
		}),
	)

	res := graph.New(s).Add(t.Context(), order)
	for issue := range res.Issues() {
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyTypeSchema {
				continue
			}
			if strings.Contains(d.Value, root) {
				t.Errorf("%s detail %q carries the schema's directory: %q",
					issue.Code(), d.Key, d.Value)
			}
		}
	}
}

// TestFileLoadedSchema_OneCompositionShapes exercises the two shapes this
// corpus had never carried: a (one) slot to a keyed part, and a (one) slot to
// a keyless one. Every other composition here is (_:many) to a keyed part, so
// nothing loaded from disk reached the address a (one) slot writes.
func TestFileLoadedSchema_OneCompositionShapes(t *testing.T) {
	t.Parallel()
	s := loadFromDisk(t)
	ctx := t.Context()

	invoice := instancetest.VI(
		"Invoice",
		instancetest.TypeID(mustTypeID(t, s, "Invoice")),
		instancetest.PK("inv1"),
		instancetest.Props(map[string]any{"invoice_id": "inv1", "amount": "10"}),
	)
	stamp := instancetest.VI(
		"Stamp",
		instancetest.TypeID(mustTypeID(t, s, "Stamp")),
		instancetest.NoKey(),
		instancetest.Props(map[string]any{"mark": "paid"}),
	)
	order := instancetest.VI(
		"Order",
		instancetest.TypeID(mustTypeID(t, s, "Order")),
		instancetest.PK("o1"),
		instancetest.Props(map[string]any{"order_id": "o1", "customer": "c"}),
		instancetest.Composed(map[string]immutable.Value{
			"INVOICE": immutable.Wrap([]any{invoice}),
			"STAMP":   immutable.Wrap([]any{stamp}),
		}),
	)

	g := graph.New(s)
	if res := g.Add(ctx, order); !res.OK() {
		t.Fatalf("a (one) composition to a keyed and a keyless part was refused: %s", res.String())
	}

	roots := g.Snapshot().InstancesOf(mustTypeID(t, s, "Order"))
	if len(roots) != 1 {
		t.Fatalf("got %d Order instances; want 1", len(roots))
	}
	for _, rel := range []string{"INVOICE", "STAMP"} {
		if n := roots[0].ComposedCount(rel); n != 1 {
			t.Errorf("%s holds %d composed children; want 1", rel, n)
		}
	}
}
