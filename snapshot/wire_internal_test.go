package snapshot

import (
	"reflect"
	"testing"
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
