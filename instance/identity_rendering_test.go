package instance_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

func loadClosure(t *testing.T) schema.Schema {
	t.Helper()
	m := map[string][]byte{
		"entry.yammm": []byte(`schema "geo"

import "common.yammm" as common

type Anchor {
    id String primary
}
`),
		"common.yammm": []byte(`schema "common"

import "parts.yammm" as parts

type Region {
    id String primary
    name String required
    *-> CELLS (many) parts.Cell
}
`),
		"parts.yammm": []byte(`schema "parts"

part type Cell {
    id String primary
}
`),
	}
	s, err := schema.LoadSourcesWithEntry(t.Context(), m, "entry.yammm", schema.WithSourcesOnly(true), schema.WithModuleRoot("."))
	if err != nil {
		t.Fatalf("load: %s", err)
	}
	return s
}

// Every rendering of a type identity in this package is the entry-relative
// form graph and snapshot render — schema.TagForm — never the declaring
// schema's alias.
func TestComposedChild_TypeNameIsTheEntryRelativeForm(t *testing.T) {
	s := loadClosure(t)
	inst, err := instance.NewValidator(s).ValidateOne(t.Context(), "common.Region", instance.RawInstance{Properties: map[string]any{
		"id": "r", "name": "n", "cells": []any{map[string]any{"id": "c1"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	children := composedChildren(inst)
	if len(children) != 1 {
		t.Fatalf("got %d children, want 1", len(children))
	}
	if want, got := schema.TagForm(s, children[0].Type().ID()), children[0].TypeName(); got != want {
		t.Errorf("TypeName = %q, want TagForm %q", got, want)
	}
}

func TestDiagnosticTypeName_IsTheEntryRelativeForm(t *testing.T) {
	s := loadClosure(t)
	_, res := diagtest.Issues(t, func() (instance.ValidInstance, error) {
		return instance.NewValidator(s).ValidateOne(t.Context(), "common.Region", instance.RawInstance{Properties: map[string]any{"id": "r"}})
	})
	yammmtest.Diff(t, []string{"common.Region"}, detailValues(mustIssue(t, res, diag.E_MISSING_REQUIRED), diag.DetailKeyTypeName))
}
