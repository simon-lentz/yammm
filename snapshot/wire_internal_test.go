package snapshot

import (
	"reflect"
	"testing"
)

// Field order in the wire structs is part of the format contract: encoding/json
// serializes in declaration order and the integrity hash covers the exact bytes,
// so reordering a field invalidates every saved .ys hash. The goldens catch a
// reordering only where a fixture happens to carry the field, which leaves an
// omitempty field on an unexercised path unpinned. This names the contract
// directly, per struct, for both wire versions.
func TestWireStructs_FieldOrder(t *testing.T) {
	t.Parallel()

	cases := []struct {
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
		{reflect.TypeFor[typeIDWire](), []string{"schema_path", "name"}},
		{reflect.TypeFor[provenanceWire](), []string{"source_name", "path"}},

		// v2 body structs. Frozen: v2 is a read-only format from v0.12.0, and
		// the decoder still parses documents written under this exact order.
		{reflect.TypeFor[instWire](), []string{
			"key", "type_id,omitempty", "properties", "edges,omitempty",
			"composed,omitempty", "provenance",
		}},
		{reflect.TypeFor[edgeWire](), []string{"target_type", "target_key", "properties"}},
		{reflect.TypeFor[dupWire](), []string{"type", "key", "instance"}},
		{reflect.TypeFor[unresolvedWire](), []string{
			"source_type", "source_key", "relation", "target_type", "target_key",
			"required", "reason", "properties,omitempty",
		}},
		{reflect.TypeFor[diagWire](), []string{"duplicates", "unresolved"}},

		// v3 body structs.
		{reflect.TypeFor[typeTableEntry](), []string{"schema_path", "name", "tag"}},
		{reflect.TypeFor[instWireV3](), []string{
			"key", "type,omitempty", "properties", "edges,omitempty",
			"composed,omitempty", "provenance",
		}},
		{reflect.TypeFor[edgeWireV3](), []string{"target_type", "target_key", "properties"}},
		{reflect.TypeFor[dupWireV3](), []string{
			"type", "key", "instance",
			"parent_type,omitempty", "parent_key,omitempty", "relation,omitempty",
		}},
		{reflect.TypeFor[unresolvedWireV3](), []string{
			"source_type", "source_key", "relation", "target_type", "target_key",
			"required", "reason", "properties,omitempty",
		}},
		{reflect.TypeFor[diagWireV3](), []string{"duplicates", "unresolved"}},
	}

	for _, tc := range cases {
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

// Every wire struct is named above. A new one added without a row here ships
// under a contract nothing checks, which is how five structs reached the tree
// during the v3 bump.
func TestWireStructs_EveryStructIsPinned(t *testing.T) {
	t.Parallel()

	pinned := map[string]bool{
		"headerWire": true, "marshalHeaderWire": true, "typeIDWire": true,
		"provenanceWire": true, "instWire": true, "edgeWire": true,
		"dupWire": true, "unresolvedWire": true, "diagWire": true,
		"typeTableEntry": true, "instWireV3": true, "edgeWireV3": true,
		"dupWireV3": true, "unresolvedWireV3": true, "diagWireV3": true,
	}
	// Sampled through the two document roots, which reach every body struct by
	// reference; a struct no root reaches is not on the wire at all.
	roots := []reflect.Type{
		reflect.TypeFor[headerWire](), reflect.TypeFor[marshalHeaderWire](),
		reflect.TypeFor[instWire](), reflect.TypeFor[diagWire](),
		reflect.TypeFor[instWireV3](), reflect.TypeFor[diagWireV3](),
		reflect.TypeFor[typeTableEntry](),
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
	for _, r := range roots {
		walk(r)
	}

	for name := range seen {
		if !pinned[name] {
			t.Errorf("wire struct %s is reachable on the wire but has no field-order row", name)
		}
	}
}
