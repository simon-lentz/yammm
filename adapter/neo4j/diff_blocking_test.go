package neo4j

import (
	"context"
	"testing"
)

// blockingProbe runs DiffConstraints for one desired UNIQUE constraint on
// (probe__Doc, title) against a single remote index.
func blockingProbe(t *testing.T, ri RemoteIndex) *ConstraintDiffResult {
	t.Helper()
	desired := []Constraint{{
		Name:       "probe__Doc_title_unique",
		Kind:       ConstraintUnique,
		Label:      "probe__Doc",
		Properties: []string{"title"},
	}}
	return New().DiffConstraints(desired, nil, ownedSet("probe__Doc"), ri)
}

// TestDiffConstraints_OnlyARangeNodeIndexBlocks pins the rule measured against
// both server generations: a uniqueness constraint backs itself with a RANGE
// index and the server refuses only a duplicate of that one. Every other index
// kind, and any relationship index, is an object it is created beside.
func TestDiffConstraints_OnlyARangeNodeIndexBlocks(t *testing.T) {
	t.Parallel()
	base := RemoteIndex{
		Name:          "other_idx",
		EntityType:    "NODE",
		LabelsOrTypes: []string{"probe__Doc"},
		Properties:    []string{"title"},
		State:         "ONLINE",
	}

	for _, tc := range []struct {
		name      string
		mutate    func(RemoteIndex) RemoteIndex
		wantDrift bool
	}{
		{"RANGE node index blocks", func(r RemoteIndex) RemoteIndex { r.Type = "RANGE"; return r }, true},
		{"an unreported type is read permissively", func(r RemoteIndex) RemoteIndex { r.Type = ""; return r }, true},
		{"TEXT does not block", func(r RemoteIndex) RemoteIndex { r.Type = "TEXT"; return r }, false},
		{"POINT does not block", func(r RemoteIndex) RemoteIndex { r.Type = "POINT"; return r }, false},
		{"FULLTEXT does not block", func(r RemoteIndex) RemoteIndex { r.Type = "FULLTEXT"; return r }, false},
		{"VECTOR does not block", func(r RemoteIndex) RemoteIndex { r.Type = "VECTOR"; return r }, false},
		{
			"a RELATIONSHIP index spelled like the label does not block",
			func(r RemoteIndex) RemoteIndex { r.Type = "RANGE"; r.EntityType = "RELATIONSHIP"; return r },
			false,
		},
		{
			"a multi-label index serves no single-label constraint",
			func(r RemoteIndex) RemoteIndex {
				r.Type = "RANGE"
				r.LabelsOrTypes = []string{"probe__Doc", "probe__Other"}
				return r
			},
			false,
		},
		{
			"a backing index is not an independent blocker",
			func(r RemoteIndex) RemoteIndex { r.Type = "RANGE"; r.OwningConstraint = "some_constraint"; return r },
			false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := blockingProbe(t, tc.mutate(base))
			gotDrift := len(plan.Drift) > 0
			if gotDrift != tc.wantDrift {
				t.Errorf("Drift=%d Create=%d; want blocking=%v",
					len(plan.Drift), len(plan.Create), tc.wantDrift)
			}
			if !tc.wantDrift && len(plan.Create) != 1 {
				t.Errorf("Create=%d, want the constraint planned for creation", len(plan.Create))
			}
		})
	}
}

// TestDiffConstraints_FulltextOnASolePrimaryKeyConverges drives the schema
// shape docs/SPEC.md blesses through the adapter's own emitters, because that
// is the path on which the two of them contradicted each other: the index
// emitter produced a FULLTEXT index for `title String primary @fulltext` and
// the constraint diff then reported the UNIQUE constraint it emits for the same
// property as blocked by it, forever.
func TestDiffConstraints_FulltextOnASolePrimaryKeyConverges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	a, s, _, _ := setupWrite(t, "fulltext_pk.yammm")

	indexes, ir := a.IndexesStructured(ctx, s)
	if ir.HasErrors() {
		t.Fatalf("IndexesStructured: %s", ir.String())
	}
	constraints, cr := a.ConstraintsStructured(ctx, s)
	if cr.HasErrors() {
		t.Fatalf("ConstraintsStructured: %s", cr.String())
	}

	var remote []RemoteIndex
	var sawFulltext bool
	for _, idx := range indexes {
		typ := indexKindToRemoteType(idx.Kind)
		if typ == "FULLTEXT" {
			sawFulltext = true
		}
		remote = append(remote, RemoteIndex{
			Name: idx.Name, Type: typ, EntityType: "NODE",
			LabelsOrTypes: []string{idx.Label}, Properties: idx.Properties, State: "ONLINE",
		})
	}
	if !sawFulltext {
		t.Fatal("the fixture emitted no FULLTEXT index, so it cannot exercise the collision")
	}

	plan := a.DiffConstraints(constraints, nil, a.OwnedLabels(ctx, s), remote...)
	for _, d := range plan.Drift {
		t.Errorf("a schema the SPEC blesses reports permanent drift: %s -> %s", d.Desired.Name, d.Reason)
	}
	if len(plan.Create) != len(constraints) {
		t.Errorf("Create=%d, want all %d constraints planned", len(plan.Create), len(constraints))
	}
}
