package csv

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// One name, two schemas, on the CSV surface.
//
// Every other test in this package loads a single schema, so nothing here
// distinguished a per-type output map keyed by the ADDRESSABLE TAG from one keyed
// by the bare type name, or a schema-type lookup by identity from one by name. A
// local type beside a directly imported one of the same name separates the two:
// the tags differ, the bare names do not, and the two declare different
// properties.

const multiEntrySource = `schema "entry"

import "base.yammm" as base

type Beacon {
	id String primary
	power Float
}
`

const multiBaseSource = `schema "base"

type Beacon {
	id String primary
	depth Float
}
`

// sameNameSnapshot holds one instance of each Beacon.
func sameNameSnapshot(t *testing.T) (*graph.Snapshot, *schema.Schema) {
	t.Helper()
	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(multiEntrySource),
		"base.yammm":  []byte(multiBaseSource),
	}, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load multi-schema fixture: %s", res)
	}
	local, ok := s.Type("Beacon")
	if !ok {
		t.Fatal("local Beacon not found")
	}
	base, ok := s.ImportByAlias("base")
	if !ok || base.Schema() == nil {
		t.Fatal("import alias base did not resolve")
	}
	imported, ok := base.Schema().Type("Beacon")
	if !ok {
		t.Fatal("base.Beacon not found")
	}
	if local.ID().Name() != imported.ID().Name() {
		t.Fatalf("fixture is vacuous: the two Beacons have different names (%q, %q)",
			local.ID().Name(), imported.ID().Name())
	}
	localTag, _ := schema.AddressableTag(s, local.ID())
	importedTag, _ := schema.AddressableTag(s, imported.ID())
	if localTag == importedTag {
		t.Fatalf("fixture is vacuous: both Beacons address as %q", localTag)
	}

	snap, rres := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{local.ID(), imported.ID()},
		Instances: []graph.InstanceParts{
			{
				TypeID:     local.ID(),
				PrimaryKey: immutable.WrapKey([]any{"l1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "l1", "power": float64(1)}),
			},
			{
				TypeID:     imported.ID(),
				PrimaryKey: immutable.WrapKey([]any{"i1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "i1", "depth": float64(2)}),
			},
		},
	})
	if rres.HasErrors() {
		t.Fatalf("assembling: %s", rres)
	}
	return snap, s
}

// TestMarshalSnapshot_KeysByTheAddressableTag pins the output map's key. Keyed by
// the bare name the two Beacons collide and one silently replaces the other.
//
// Mutation: keying the map by typeID.Name() turns this red.
func TestMarshalSnapshot_KeysByTheAddressableTag(t *testing.T) {
	t.Parallel()
	snap, _ := sameNameSnapshot(t)

	out, err := New().MarshalSnapshot(context.Background(), snap)
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d output files, want 2 — the two Beacons shared a key: %v", len(out), keysOf(out))
	}
	for _, want := range []string{"Beacon", "base.Beacon"} {
		if _, ok := out[want]; !ok {
			t.Errorf("no output for %q; got %v", want, keysOf(out))
		}
	}
}

// TestMarshalSnapshot_ResolvesTheSchemaTypeByIdentity pins the type lookup. Each
// Beacon declares a different Float property, so a lookup by NAME hands one
// instance the other type's columns.
//
// Mutation: resolving the schema type with ResolveTypeName over the bare name
// turns this red.
func TestMarshalSnapshot_ResolvesTheSchemaTypeByIdentity(t *testing.T) {
	t.Parallel()
	snap, _ := sameNameSnapshot(t)

	out, err := New().MarshalSnapshot(context.Background(), snap)
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}
	local, ok := out["Beacon"]
	if !ok {
		t.Fatalf("no output for the local Beacon; got %v", keysOf(out))
	}
	imported, ok := out["base.Beacon"]
	if !ok {
		t.Fatalf("no output for the imported Beacon; got %v", keysOf(out))
	}

	localHeader := firstLine(string(local))
	importedHeader := firstLine(string(imported))
	if !strings.Contains(localHeader, "power") || strings.Contains(localHeader, "depth") {
		t.Errorf("the local Beacon's header is %q; it declares power, not depth", localHeader)
	}
	if !strings.Contains(importedHeader, "depth") || strings.Contains(importedHeader, "power") {
		t.Errorf("the imported Beacon's header is %q; it declares depth, not power", importedHeader)
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimRight(line, "\r")
}
