package snapshot_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// wireTypeTable reads a v3 document's types table.
func wireTypeTable(t *testing.T, data []byte) []struct {
	SchemaPath string `json:"schema_path"`
	Name       string `json:"name"`
	Tag        string `json:"tag"`
} {
	t.Helper()
	var doc struct {
		Types []struct {
			SchemaPath string `json:"schema_path"`
			Name       string `json:"name"`
			Tag        string `json:"tag"`
		} `json:"types"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal types table: %v", err)
	}
	return doc.Types
}

// TestMigration_V2ToV3PreservesTransitiveIdentity is the migration bar: a v2
// document naming a transitively imported type — the shape whose tag form
// cannot say which schema declared it — re-marshals to a v3 document that
// denotes the type exactly. The v2 wire could only say "Part"; the v3 table
// says which Part.
func TestMigration_V2ToV3PreservesTransitiveIdentity(t *testing.T) {
	ctx := context.Background()
	s := loadIdentitySchema(t)

	basePart := mustTypeIDIn(t, s, "base", "Part")
	legacy := marshalDoc(t, ctx, s, graph.SnapshotParts{
		Types: []schema.TypeID{basePart},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			basePart: {{
				TypeName:   "base.Part",
				TypeID:     basePart,
				PrimaryKey: immutable.WrapKey([]any{"bp1"}),
				Properties: immutable.WrapProperties(map[string]any{"name": "bp1"}),
			}},
		},
	})
	if !bytes.Contains(legacy, []byte(`"version":2`)) {
		t.Fatalf("fixture is not a legacy document:\n%s", legacy)
	}

	loaded, res := snapshot.Load(ctx, legacy, s)
	if err := res.Err(); err != nil {
		t.Fatalf("load legacy document: %v", err)
	}

	migrated, res := snapshot.Marshal(ctx, loaded)
	if err := res.Err(); err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !bytes.Contains(migrated, []byte(`"version": 3`)) && !bytes.Contains(migrated, []byte(`"version":3`)) {
		t.Fatalf("re-marshal did not produce a v3 document:\n%s", migrated)
	}

	table := wireTypeTable(t, migrated)
	if len(table) != 1 {
		t.Fatalf("types table holds %d rows, want 1:\n%s", len(table), migrated)
	}
	if table[0].SchemaPath != basePart.SchemaPath().String() {
		t.Errorf("schema_path = %q, want %q — the migration lost the identity the v2 tag could not carry",
			table[0].SchemaPath, basePart.SchemaPath().String())
	}
	if table[0].Name != "Part" {
		t.Errorf("name = %q, want %q", table[0].Name, "Part")
	}

	// The migration is a fixpoint: a v3 document re-marshals to itself.
	reloaded, res := snapshot.Load(ctx, migrated, s)
	if err := res.Err(); err != nil {
		t.Fatalf("load migrated document: %v", err)
	}
	again, res := snapshot.Marshal(ctx, reloaded)
	if err := res.Err(); err != nil {
		t.Fatalf("re-marshal migrated document: %v", err)
	}
	if !bytes.Equal(migrated, again) {
		t.Errorf("a migrated document is not a marshal fixpoint:\nfirst:\n%s\nsecond:\n%s", migrated, again)
	}
}

// TestMigration_V2ToV3FreezesResolvedIdentity pins the half of the migration a
// consumer cannot undo: re-marshalling a legacy document makes the reader's
// identity resolution permanent, because v3 carries no tag form to re-resolve
// from. A wrong resolution would be frozen with it, which is why the legacy
// read path resolves identity-first rather than tag-first.
func TestMigration_V2ToV3FreezesResolvedIdentity(t *testing.T) {
	ctx := context.Background()
	s := loadIdentitySchema(t)

	basePart := mustTypeIDIn(t, s, "base", "Part")
	localPart := mustTypeIDIn(t, s, "", "Part")
	if basePart == localPart {
		t.Fatal("fixture is vacuous: the two Parts share an identity")
	}

	legacy := marshalDoc(t, ctx, s, graph.SnapshotParts{
		Types: []schema.TypeID{basePart},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			basePart: {{
				TypeName:   "base.Part",
				TypeID:     basePart,
				PrimaryKey: immutable.WrapKey([]any{"bp1"}),
				Properties: immutable.WrapProperties(map[string]any{"name": "bp1"}),
			}},
		},
	})

	loaded, res := snapshot.Load(ctx, legacy, s)
	if err := res.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	migrated, res := snapshot.Marshal(ctx, loaded)
	if err := res.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}

	table := wireTypeTable(t, migrated)
	if len(table) != 1 {
		t.Fatalf("types table holds %d rows, want 1", len(table))
	}
	if table[0].SchemaPath == localPart.SchemaPath().String() {
		t.Errorf("the migration bound the imported Part to the local one; the v3 document now names %q permanently",
			table[0].SchemaPath)
	}
}
