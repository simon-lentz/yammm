package neo4j

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

const (
	closureDDLBase = `schema "base"

type Basin {
	basin_id String primary
	region String required @index
}
`
	closureDDLEntry = `schema "entry"

import "./base" as base

type Station {
	station_id String primary
	--> SITS_IN (one) base.Basin
}
`
)

func loadClosureDDLSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(closureDDLEntry),
		"base.yammm":  []byte(closureDDLBase),
	}, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	return s
}

// An imported type the write path creates nodes for must also get its DDL and
// its ownership. ShapeForSchema walks the import closure and gives base.Basin a
// label, so BatchNodeQueries writes Basin nodes; a constraint walk over the
// entry schema's own types alone left those nodes with no uniqueness
// constraint, no index, and no entry in OwnedLabels — which also made them
// invisible to the diff, so nothing would ever report the gap.
func TestClosureDDL_ImportedTypeGetsConstraintsIndexesAndOwnership(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadClosureDDLSchema(t)
	a := New()

	shape, sres := a.ShapeForSchema(ctx, s)
	if shape == nil {
		t.Fatalf("ShapeForSchema: %s", sres)
	}
	var basinLabel string
	for id, ns := range shape.Types {
		if strings.HasSuffix(id.String(), ":Basin") {
			basinLabel = ns.Label
		}
	}
	if basinLabel == "" {
		t.Fatal("ShapeForSchema gave the imported type no label; the premise of this test is gone")
	}

	constraints, cres := a.ConstraintsForSchema(ctx, s)
	if cres.HasErrors() {
		t.Fatalf("ConstraintsForSchema: %s", cres)
	}
	if !mentionsLabel(constraints, basinLabel) {
		t.Errorf("no constraint mentions the imported type's label %q; the write path creates its nodes and nothing constrains them\ngot: %v",
			basinLabel, constraints)
	}

	indexes, ires := a.IndexesForSchema(ctx, s)
	if ires.HasErrors() {
		t.Fatalf("IndexesForSchema: %s", ires)
	}
	if !mentionsLabel(indexes, basinLabel) {
		t.Errorf("no index mentions the imported type's label %q, though its region property is @index\ngot: %v",
			basinLabel, indexes)
	}

	if owned := a.OwnedLabels(ctx, s); !owned.Contains(basinLabel) {
		t.Errorf("OwnedLabels does not contain %q; the diff cannot see a label this schema writes", basinLabel)
	}
}

// The emit set and the own set are one set. Every label any emitter produces is
// a label OwnedLabels claims, so the diff can never plan a DROP for a label the
// emitter still writes.
func TestClosureDDL_EmitSetAndOwnSetAgree(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadClosureDDLSchema(t)
	a := New()

	owned := a.OwnedLabels(ctx, s)
	shape, sres := a.ShapeForSchema(ctx, s)
	if shape == nil {
		t.Fatalf("ShapeForSchema: %s", sres)
	}
	for id, ns := range shape.Types {
		if !owned.Contains(ns.Label) {
			t.Errorf("type %s is written under label %q, which OwnedLabels does not claim", id, ns.Label)
		}
	}
}

func mentionsLabel(statements []string, label string) bool {
	for _, stmt := range statements {
		if strings.Contains(stmt, label) {
			return true
		}
	}
	return false
}
