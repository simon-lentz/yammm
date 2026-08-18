package snapshot

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// wireStructCases pins every wire struct's field order and tags —
// encoding/json serializes in declaration order and the integrity hash
// covers the exact bytes, so a reordering invalidates every saved .ys hash.
// Goldens catch it only where a fixture carries the field; this names it.
var wireStructCases = []struct {
	typ  reflect.Type
	tags []string
}{
	{reflect.TypeFor[headerWire](), []string{
		"version", "schema_name", "schema_source", "schema_hash",
		"schema_hash_algorithm", "integrity_hash", "features",
		"created_at,omitempty", "metadata,omitempty",
	}},
	{reflect.TypeFor[marshalHeaderWire](), []string{
		"schema_name", "schema_source", "schema_hash", "integrity_hash",
		"features", "created_at,omitempty", "metadata,omitempty",
	}},
	{reflect.TypeFor[provenanceWire](), []string{"source_name", "path"}},

	// Body structs.
	{reflect.TypeFor[typeTableEntry](), []string{"schema_path", "name"}},
	{reflect.TypeFor[instanceGroupWire](), []string{"type", "items"}},
	{reflect.TypeFor[instWire](), []string{
		"key", "type,omitempty", "properties", "edges,omitempty",
		"composed,omitempty", "provenance",
	}},
	{reflect.TypeFor[edgeWire](), []string{"target_type", "target_key", "properties"}},
	{reflect.TypeFor[conflictWire](), []string{"type", "key"}},
	{reflect.TypeFor[dupWire](), []string{
		"type", "key", "instance", "conflict",
		"parent_type,omitempty", "parent_key,omitempty", "relation,omitempty",
	}},
	{reflect.TypeFor[unresolvedWire](), []string{
		"source_type", "source_key", "relation", "target_type", "target_key",
		"required", "reason", "properties,omitempty",
	}},
	{reflect.TypeFor[diagWire](), []string{"duplicates", "unresolved"}},
}

// wireRoots are the types Marshal serializes directly: the two header forms
// and the three body sections. Every other wire struct must be reachable
// from one of these, or it is not on the wire at all.
var wireRoots = []reflect.Type{
	reflect.TypeFor[headerWire](), reflect.TypeFor[marshalHeaderWire](),
	reflect.TypeFor[typeTableEntry](), reflect.TypeFor[instanceGroupWire](),
	reflect.TypeFor[diagWire](),
}

func TestWireStructs_FieldOrder(t *testing.T) {
	t.Parallel()

	for _, tc := range wireStructCases {
		t.Run(tc.typ.Name(), func(t *testing.T) {
			t.Parallel()
			got := make([]string, tc.typ.NumField())
			for i := range got {
				got[i] = tc.typ.Field(i).Tag.Get("json")
			}
			if !reflect.DeepEqual(got, tc.tags) {
				t.Errorf("%s field order changed — every saved .ys integrity hash depends on it\n got:  %q\n want: %q",
					tc.typ.Name(), got, tc.tags)
			}
		})
	}
}

// TestWireStructs_EveryStructIsPinned holds the case inventory and the wire
// closed over each other: every struct reachable from the document roots
// must have a field-order row, and every row must be reachable — so a
// stale row and an unpinned struct both fail, from one inventory.
func TestWireStructs_EveryStructIsPinned(t *testing.T) {
	t.Parallel()

	pinned := make(map[string]bool, len(wireStructCases))
	for _, tc := range wireStructCases {
		pinned[tc.typ.Name()] = true
	}

	seen := make(map[string]bool)
	var walk func(reflect.Type)
	walk = func(rt reflect.Type) {
		for rt.Kind() == reflect.Pointer || rt.Kind() == reflect.Slice || rt.Kind() == reflect.Map {
			if rt.Kind() == reflect.Map {
				rt = rt.Elem()
				continue
			}
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || rt.PkgPath() != reflect.TypeFor[headerWire]().PkgPath() {
			return
		}
		if seen[rt.Name()] {
			return
		}
		seen[rt.Name()] = true
		for f := range rt.Fields() {
			walk(f.Type)
		}
	}
	for _, r := range wireRoots {
		walk(r)
	}

	for name := range seen {
		if !pinned[name] {
			t.Errorf("wire struct %s is reachable on the wire but has no field-order row", name)
		}
	}
	for name := range pinned {
		if !seen[name] {
			t.Errorf("field-order row %s pins a struct no document root reaches — stale row or missing root", name)
		}
	}
}

// deepSchema composes without a natural floor, so a tree can exceed the
// reader's nesting limit.
const deepSchema = `schema "deep"

type Trunk {
	id String primary
	*-> KIDS (many) Node
}

part type Node {
	id String primary
	*-> KIDS (many) Node
}
`

// nestedParts builds a chain of composed Node parts depth levels deep.
func nestedParts(nodeID schema.TypeID, depth int) []graph.InstanceParts {
	if depth == 0 {
		return nil
	}
	node := graph.InstanceParts{
		TypeName:   "Node",
		TypeID:     nodeID,
		PrimaryKey: immutable.WrapKey([]any{fmt.Sprintf("n%d", depth)}),
		Properties: immutable.WrapProperties(map[string]any{"id": fmt.Sprintf("n%d", depth)}),
	}
	if kids := nestedParts(nodeID, depth-1); kids != nil {
		node.Composed = map[string][]graph.InstanceParts{"KIDS": kids}
	}
	return []graph.InstanceParts{node}
}

// TestMarshal_RefusesNestingBeyondTheReadersLimit pins the writer against the
// reader's bound. The reader refuses composed nesting past maxComposedDepth and
// nothing upstream bounds it, so an unbounded writer could produce a document
// Load and Verify reject whole. Marshal refuses to write it instead. In-package
// because the bound the two sides must share is unexported.
func TestMarshal_RefusesNestingBeyondTheReadersLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.LoadString(t.Context(), deepSchema, "deep.yammm")
	if res.HasErrors() {
		t.Fatalf("load deep schema: %s", res)
	}
	trunk, ok := s.Type("Trunk")
	if !ok {
		t.Fatal("Trunk missing")
	}
	node, ok := s.Type("Node")
	if !ok {
		t.Fatal("Node missing")
	}

	build := func(chain int) *graph.Snapshot {
		t.Helper()
		root := graph.InstanceParts{
			TypeName:   "Trunk",
			TypeID:     trunk.ID(),
			PrimaryKey: immutable.WrapKey([]any{"t1"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "t1"}),
			Composed:   map[string][]graph.InstanceParts{"KIDS": nestedParts(node.ID(), chain)},
		}
		built, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
			Types:     []schema.TypeID{trunk.ID(), node.ID()},
			Instances: map[schema.TypeID][]graph.InstanceParts{trunk.ID(): {root}},
		})
		if res.HasErrors() {
			t.Fatalf("assembling a %d-deep chain: %s", chain, res)
		}
		return built
	}

	// The control: the deepest tree the reader accepts must still marshal and
	// load, or the writer's bound is off by one in the strict direction.
	data, res := Marshal(ctx, build(maxComposedDepth))
	if err := res.Err(); err != nil {
		t.Fatalf("Marshal refused a tree at the reader's limit: %v", err)
	}
	if _, loadRes := Load(ctx, data, s); loadRes.HasErrors() {
		t.Fatalf("Load refused a tree at its own limit: %v", loadRes)
	}

	// One level deeper is a document no read path accepts, and the writer must
	// name the bound the way the reader names it — same code, same severity —
	// or a caller cannot tell a snapshot too deep to write from a corrupt
	// writer state.
	_, deepRes := Marshal(ctx, build(maxComposedDepth+1))
	if !deepRes.HasErrors() {
		t.Fatal("Marshal wrote a tree nested past the reader's limit")
	}
	var found bool
	for issue := range deepRes.Issues() {
		if issue.Code() != diag.E_SNAPSHOT_DEPTH_EXCEEDED {
			continue
		}
		found = true
		if issue.Severity() != diag.Error {
			t.Errorf("Marshal reported the depth bound at %s, the reader reports it at %s",
				issue.Severity(), diag.Error)
		}
		// The payload, not only the code: a consumer branches on the detail and
		// reads the message, and both can be wrong with the code right.
		details := map[string]string{}
		for _, d := range issue.Details() {
			details[d.Key] = d.Value
		}
		if got := details[diag.DetailKeyDepth]; got != strconv.Itoa(maxComposedDepth+1) {
			t.Errorf("depth detail = %q, want %q", got, strconv.Itoa(maxComposedDepth+1))
		}
		if got := details[diag.DetailKeyTypeName]; got != node.ID().String() {
			t.Errorf("type-name detail = %q, want the identity %q", got, node.ID())
		}
		if !strings.Contains(issue.Message(), "composed nesting depth") {
			t.Errorf("message does not name the bound it reports: %q", issue.Message())
		}
	}
	if !found {
		t.Errorf("Marshal did not report %s for a tree past the bound: %v",
			diag.E_SNAPSHOT_DEPTH_EXCEEDED, deepRes)
	}
}
