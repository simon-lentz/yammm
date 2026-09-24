package graph_test

import (
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// A root instance carries one identity, and every reader of a rebuilt snapshot
// answers by it: the group it is filed under, the types the snapshot reports,
// and the name it renders.

const rootIdentityEntry = `schema "entry"

type Anchor {
	id String primary
}

type Site {
	id String primary
}
`

func TestRebuildSnapshot_FilesARootUnderItsOwnIdentity(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), rootIdentityEntry, "entry.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	anchor := mustTypeID(t, s, "Anchor")
	site := mustTypeID(t, s, "Site")

	// Types names Anchor alone; the Site root still joins the reported types.
	snap, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{anchor},
		Instances: []graph.InstanceParts{{
			TypeID:     site,
			PrimaryKey: immutable.WrapKey([]any{"x1"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "x1"}),
		}},
	})
	if res.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", res)
	}
	if n := len(snap.InstancesOf(anchor)); n != 0 {
		t.Errorf("InstancesOf(Anchor) = %d, want 0", n)
	}
	got := snap.InstancesOf(site)
	if len(got) != 1 || got[0].TypeID() != site || got[0].TypeName() != "Site" {
		t.Fatalf("InstancesOf(Site) = %v, want the one Site instance named Site", got)
	}
	if types := snap.Types(); len(types) != 2 || types[0] != anchor || types[1] != site {
		t.Errorf("Types() = %v, want [Anchor Site]", types)
	}
	n := 0
	for range snap.AllInstances() {
		n++
	}
	if n != 1 {
		t.Errorf("AllInstances yields %d, want 1", n)
	}
}

const (
	aliasBase = `schema "base"

type Basin {
	id String primary
}
`
	aliasEntryB = `schema "entry"

import "base.yammm" as b
`
	aliasEntryX = `schema "entry"

import "base.yammm" as x
`
)

func loadAliased(t *testing.T, entry, alias string) (*schema.Schema, schema.TypeID) {
	t.Helper()
	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(entry),
		"base.yammm":  []byte(aliasBase),
	}, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	basin, ok := s.ResolveType(schema.NewTypeRef(alias, "Basin", location.Span{}))
	if !ok {
		t.Fatal("Basin did not resolve through the entry schema")
	}
	return s, basin.ID()
}

func TestRebuildSnapshot_RendersARootsNameFromItsIdentity(t *testing.T) {
	t.Parallel()
	s, basin := loadAliased(t, aliasEntryB, "b")
	snap, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{basin},
		Instances: []graph.InstanceParts{{
			TypeID:     basin,
			PrimaryKey: immutable.WrapKey([]any{"b1"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "b1"}),
		}},
	})
	if res.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", res)
	}
	if got := snap.InstancesOf(basin)[0].TypeName(); got != "b.Basin" {
		t.Errorf("TypeName() = %q, want the alias-qualified b.Basin", got)
	}
}

// The snapshot's names were rendered under its own schema's alias; the graph
// that imports it renders them under its own.
func TestNewFromSnapshot_RendersNamesUnderTheImportingSchema(t *testing.T) {
	t.Parallel()
	sb, basin := loadAliased(t, aliasEntryB, "b")
	sx, basinX := loadAliased(t, aliasEntryX, "x")
	if basin != basinX {
		t.Fatalf("the two entries resolve Basin to %s and %s, want one identity", basin, basinX)
	}
	snap, res := graph.RebuildSnapshot(sb, graph.SnapshotParts{
		Types: []schema.TypeID{basin},
		Instances: []graph.InstanceParts{{
			TypeID:     basin,
			PrimaryKey: immutable.WrapKey([]any{"b1"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "b1"}),
		}},
	})
	if res.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", res)
	}
	got := mustImport(t, sx, snap).Snapshot().InstancesOf(basin)[0].TypeName()
	if got != "x.Basin" {
		t.Errorf("imported TypeName() = %q, want x.Basin, the importing schema's rendering", got)
	}
}
