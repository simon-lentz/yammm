package graph_test

import (
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/schema"
)

const associationShapeSchema = `schema "assoc"

type Company {
	id String primary
}

type Pair {
	a String primary
	b String primary
}

part type Badge {
	code String primary
}

type Person {
	id String primary
	--> EMPLOYER (_:one) Company
	--> MANAGER (one:one) Person
	--> FRIENDS (_:many) Person
	--> PARTNER (_:one) Pair
	*-> BADGES (_:many) Badge
}

type Run {
	at Timestamp primary
	--> NEXT (_:one) Run
}
`

type assocIDs struct{ company, pair, badge, person, run schema.TypeID }

func loadAssociationShapes(t *testing.T) (schema.Schema, assocIDs) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), associationShapeSchema, "assoc.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s, assocIDs{
		company: mustTypeID(t, s, "Company"), pair: mustTypeID(t, s, "Pair"), badge: mustTypeID(t, s, "Badge"),
		person: mustTypeID(t, s, "Person"), run: mustTypeID(t, s, "Run"),
	}
}

// personParts is Person p1 and Company c1, the instances every case's records
// address.
func personParts(ids assocIDs) snapparts.Parts {
	return snapparts.Parts{
		Types: []schema.TypeID{ids.person, ids.company},
		Instances: []snapparts.Instance{
			{TypeID: ids.person, PrimaryKey: immutable.WrapKey([]any{"p1"}), Properties: immutable.WrapProperties(map[string]any{"id": "p1"})},
			{TypeID: ids.company, PrimaryKey: immutable.WrapKey([]any{"c1"}), Properties: immutable.WrapProperties(map[string]any{"id": "c1"})},
		},
	}
}

func unresolvedFromP1(ids assocIDs, relation, reason string, key ...any) snapparts.Unresolved {
	up := snapparts.Unresolved{
		SourceType: ids.person, SourceKey: immutable.WrapKey([]any{"p1"}), Relation: relation,
		Reason: reason,
	}
	if key != nil {
		up.TargetKey = immutable.WrapKey(key)
	}
	return up
}

// TestRebuild_DerivesRequiredFromTheRelation pins that an unresolved record's
// Required is the relation's, as Add derives it: the parts cannot state it.
func TestRebuild_DerivesRequiredFromTheRelation(t *testing.T) {
	t.Parallel()
	s, ids := loadAssociationShapes(t)
	parts := personParts(ids)
	parts.Unresolved = []snapparts.Unresolved{
		unresolvedFromP1(ids, "MANAGER", "absent"),
		unresolvedFromP1(ids, "FRIENDS", "target_missing", "p9"),
	}
	snap := rebuild(s, parts)
	got := map[string]bool{}
	for u := range snap.Unresolved() {
		got[u.Relation()] = u.Required()
	}
	if !got["MANAGER"] || got["FRIENDS"] {
		t.Errorf("Required = %v, want MANAGER required and FRIENDS not", got)
	}
}
