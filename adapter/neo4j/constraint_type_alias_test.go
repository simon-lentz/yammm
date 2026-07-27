package neo4j

import (
	"strings"
	"testing"
)

// Neo4j spells the node uniqueness constraint type differently across server
// generations — 5.x reports UNIQUENESS, 2026.x reports NODE_PROPERTY_UNIQUENESS
// — and this adapter supports both. Every comparison against a server-reported
// type therefore has to fold the two together. These tests pin that at each
// site: matching a remote constraint could rot silently into "the schema's own
// constraint is drift to be dropped", which is worse than a hard failure
// because the plan looks actionable.

// uniquenessSpellings is what real Neo4j servers report for a node uniqueness
// constraint: 5.x reports the first, 2026.x the second. Both were read from
// SHOW CONSTRAINTS on a running server of each generation.
//
// Written as LITERALS on purpose. Deriving them from
// remoteConstraintTypeAliases would make every test below agree with whatever
// that table happens to contain — including an empty one — and the regression
// they exist to catch is precisely a spelling missing from it. This list is a
// claim about the servers, not about the code.
var uniquenessSpellings = []string{"UNIQUENESS", "NODE_PROPERTY_UNIQUENESS"}

func TestCanonicalRemoteConstraintType(t *testing.T) {
	t.Parallel()
	canonical := constraintKindToRemoteType(ConstraintUnique)

	for _, spelling := range uniquenessSpellings {
		if got := canonicalRemoteConstraintType(spelling); got != canonical {
			t.Errorf("canonicalRemoteConstraintType(%q) = %q; want %q — a spelling a real server reports must fold onto the one the desired side embeds",
				spelling, got, canonical)
		}
		// Case is folded too: the column is canonically upper-case, but a
		// difference must not hide a declarable constraint.
		lower := strings.ToLower(spelling)
		if got := canonicalRemoteConstraintType(lower); got != canonical {
			t.Errorf("canonicalRemoteConstraintType(%q) = %q; want %q", lower, got, canonical)
		}
	}

	// The other three kinds are already spelled canonically and must pass
	// through untouched rather than acquiring an alias by accident.
	for _, kind := range allConstraintKinds {
		if kind == ConstraintUnique {
			continue
		}
		remote := constraintKindToRemoteType(kind)
		if got := canonicalRemoteConstraintType(remote); got != remote {
			t.Errorf("canonicalRemoteConstraintType(%q) = %q; want it unchanged", remote, got)
		}
	}

	// A relationship spelling must NOT fold onto a node kind: admitting one
	// would decide ownership against LabelsOrTypes holding a relationship type
	// rather than a label.
	for _, relType := range []string{"RELATIONSHIP_UNIQUENESS", "RELATIONSHIP_PROPERTY_UNIQUENESS"} {
		if got := canonicalRemoteConstraintType(relType); got == canonical {
			t.Errorf("canonicalRemoteConstraintType(%q) = %q; a relationship constraint must not fold onto a node kind", relType, got)
		}
	}
}

func TestDiffConstraints_UniquenessSpellingsAllMatch(t *testing.T) {
	t.Parallel()

	for _, spelling := range uniquenessSpellings {
		t.Run(spelling, func(t *testing.T) {
			t.Parallel()
			a := New()

			desired := []Constraint{{
				Kind: ConstraintUnique, Label: "test__Entity", Properties: []string{"id"},
			}}
			actual := []RemoteConstraint{{
				Name: "test__Entity_id_unique", Type: spelling, EntityType: "NODE",
				LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"},
			}}

			result := a.DiffConstraints(desired, actual, testOwned())

			// The specific misbehaviour this guards: an unrecognised spelling
			// makes the remote constraint undeclarable, so it is Excluded rather
			// than paired, and the desired constraint then finds its name taken
			// and reports as drift telling the operator to drop a constraint that
			// is correct.
			if len(result.Match) != 1 {
				t.Errorf("Match = %d; want 1 — remote %q should realise the declaration", len(result.Match), spelling)
			}
			for _, unwanted := range []struct {
				name string
				n    int
			}{
				{"Drift", len(result.Drift)},
				{"Create", len(result.Create)},
				{"Drop", len(result.Drop)},
				{"Unverified", len(result.Unverified)},
				{"Excluded", result.Excluded},
			} {
				if unwanted.n != 0 {
					t.Errorf("%s = %d; want 0 for remote %q", unwanted.name, unwanted.n, spelling)
				}
			}
			for _, d := range result.Drift {
				t.Logf("  drift: %s", d.Reason)
			}
		})
	}
}

func TestInferSchema_UniquenessSpellingsYieldPrimaryKey(t *testing.T) {
	t.Parallel()

	for _, spelling := range uniquenessSpellings {
		t.Run(spelling, func(t *testing.T) {
			t.Parallel()
			a := New()

			constraints := []RemoteConstraint{{
				Name: "c1", Type: spelling, EntityType: "NODE",
				LabelsOrTypes: []string{"app__Person"}, Properties: []string{"id"},
			}}

			src, err := a.InferSchema(constraints, nil, "app")
			if err != nil {
				t.Fatalf("InferSchema: %v", err)
			}

			// An unrecognised spelling marks no primary key, and the scaffold
			// then fails to load on E_NO_PRIMARY_KEY — a schema the tool itself
			// produced and cannot read back.
			if !strings.Contains(src, "primary") {
				t.Errorf("remote %q produced a scaffold with no primary key:\n%s", spelling, src)
			}
		})
	}
}
