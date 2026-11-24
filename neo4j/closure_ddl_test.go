package neo4j

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/cypher"
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

func loadClosureDDLSchema(t *testing.T) schema.Schema {
	t.Helper()
	s, err := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(closureDDLEntry),
		"base.yammm":  []byte(closureDDLBase),
	}, "entry.yammm", schema.WithSourcesOnly(true), schema.WithModuleRoot("."))
	if err != nil {
		t.Fatalf("load schema: %s", err)
	}
	return s
}

// An imported type the write path creates nodes for must also get its DDL and
// its ownership. The write path's shape walks the import closure and gives base.Basin a
// label, so NodeQueries writes Basin nodes; a constraint walk over the
// entry schema's own types alone left those nodes with no uniqueness
// constraint, no index, and no entry in OwnedLabels — which also made them
// invisible to the diff, so nothing would ever report the gap.
func TestClosureDDL_ImportedTypeGetsConstraintsIndexesAndOwnership(t *testing.T) {
	t.Parallel()
	s := loadClosureDDLSchema(t)
	a := cypher.Config{}

	shape, err := shapeFor(a, s)
	if shape == nil {
		t.Fatalf("shapeFor: %s", err)
	}
	var basinLabel string
	for id, ns := range shape.types {
		if strings.HasSuffix(id.String(), ":Basin") {
			basinLabel = ns.label
		}
	}
	if basinLabel == "" {
		t.Fatal("shapeFor gave the imported type no label; the premise of this test is gone")
	}

	constraints, cres := constraintStatements(t, a, s)
	if diagtest.Err(cres) != nil {
		t.Fatalf("Constraints: %s", cres)
	}
	if !mentionsLabel(constraints, basinLabel) {
		t.Errorf("no constraint mentions the imported type's label %q; the write path creates its nodes and nothing constrains them\ngot: %v",
			basinLabel, constraints)
	}

	indexes, ires := indexStatements(t, a, s)
	if diagtest.Err(ires) != nil {
		t.Fatalf("Indexes: %s", ires)
	}
	if !mentionsLabel(indexes, basinLabel) {
		t.Errorf("no index mentions the imported type's label %q, though its region property is @index\ngot: %v",
			basinLabel, indexes)
	}

	if owned := a.OwnedLabels(s); !owned.Contains(basinLabel) {
		t.Errorf("OwnedLabels does not contain %q; the diff cannot see a label this schema writes", basinLabel)
	}
}

// The emit set and the own set are one set. Every label any emitter produces is
// a label OwnedLabels claims, so the diff can never plan a DROP for a label the
// emitter still writes.
func TestClosureDDL_EmitSetAndOwnSetAgree(t *testing.T) {
	t.Parallel()
	s := loadClosureDDLSchema(t)
	a := cypher.Config{}

	owned := a.OwnedLabels(s)
	shape, err := shapeFor(a, s)
	if shape == nil {
		t.Fatalf("shapeFor: %s", err)
	}
	for id, ns := range shape.types {
		if !owned.Contains(ns.label) {
			t.Errorf("type %s is written under label %q, which OwnedLabels does not claim", id, ns.label)
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
