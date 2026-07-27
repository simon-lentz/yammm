package neo4j

import (
	"strings"
	"testing"
)

// Every comparison against a column the SERVER fills has to fold case. The
// columns are canonically upper-case today, so none of this is reachable
// against a current server — which is the point: an unfolded comparison fails
// silently and only under a server that changes, so the fold is pinned here
// rather than left to be discovered the next time one does.
//
// The two commands that read these columns must fold them IDENTICALLY. A
// stricter test in one of them is the dangerous shape: `neo4j diff` would keep
// working while `neo4j introspect` quietly produced a different answer from the
// same rows.

// lowerCased is the mutation these tests apply: a server returning the same
// value in another case. Applied to whole records rather than one field, so a
// site that folds one column and not its neighbour is still caught.
func lowerCased(rc RemoteConstraint) RemoteConstraint {
	rc.Type = strings.ToLower(rc.Type)
	rc.EntityType = strings.ToLower(rc.EntityType)
	rc.PropertyType = strings.ToLower(rc.PropertyType)
	return rc
}

func TestDiffConstraints_FoldsServerColumnCase(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Constraint{
		{Kind: ConstraintUnique, Label: "test__Entity", Properties: []string{"id"}},
		{
			Kind: ConstraintType, Label: "test__Entity", Properties: []string{"tags"},
			TypeExpr: "LIST<STRING NOT NULL>",
		},
	}
	actual := []RemoteConstraint{
		lowerCased(RemoteConstraint{
			Name: "u", Type: "UNIQUENESS", EntityType: "NODE",
			LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"},
		}),
		lowerCased(RemoteConstraint{
			Name: "t", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE",
			LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"tags"},
			PropertyType: "LIST<STRING NOT NULL>",
		}),
	}

	result := a.DiffConstraints(desired, actual, testOwned())

	if len(result.Match) != 2 {
		t.Errorf("Match = %d; want 2 — a case difference in a server column is not drift", len(result.Match))
	}
	for _, d := range result.Drift {
		t.Errorf("drift on a lower-cased but otherwise identical constraint: %s", d.Reason)
	}
	if result.Excluded != 0 {
		t.Errorf("Excluded = %d; want 0 — lower-cased entityType/type must not make a constraint undeclarable", result.Excluded)
	}
	if n := len(result.Create) + len(result.Drop) + len(result.Unverified); n != 0 {
		t.Errorf("Create+Drop+Unverified = %d; want 0", n)
	}
}

// The TYPE-constraint comparison is the one that reads propertyType, and it
// compares against a string the ADAPTER emits rather than one it looks up, so a
// fold there is easy to leave out. A genuine mismatch must still be drift.
func TestDiffConstraints_TypeExprFoldDoesNotHideRealDrift(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Constraint{{
		Kind: ConstraintType, Label: "test__Entity", Properties: []string{"n"}, TypeExpr: "INTEGER",
	}}
	actual := []RemoteConstraint{{
		Name: "t", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE",
		LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"n"},
		PropertyType: "string",
	}}

	result := a.DiffConstraints(desired, actual, testOwned())

	if len(result.Drift) != 1 {
		t.Fatalf("Drift = %d; want 1 — INTEGER vs STRING is a real type mismatch whatever the case", len(result.Drift))
	}
	if len(result.Match) != 0 {
		t.Errorf("Match = %d; want 0", len(result.Match))
	}
}

// InferSchema reads the same two columns and must reach the same conclusions.
// Its failure mode is silent: an unrecognised entityType skips the constraint
// entirely, and an unrecognised propertyType leaves the property on its String
// fallback, so both produce a plausible-looking schema that is wrong.
func TestInferSchema_FoldsServerColumnCase(t *testing.T) {
	t.Parallel()
	a := New()

	constraints := []RemoteConstraint{
		lowerCased(RemoteConstraint{
			Name: "u", Type: "UNIQUENESS", EntityType: "NODE",
			LabelsOrTypes: []string{"app__Person"}, Properties: []string{"id"},
		}),
		lowerCased(RemoteConstraint{
			Name: "t", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE",
			LabelsOrTypes: []string{"app__Person"}, Properties: []string{"age"},
			PropertyType: "INTEGER",
		}),
	}

	src, err := a.InferSchema(constraints, nil, "app")
	if err != nil {
		t.Fatalf("InferSchema: %v", err)
	}

	// A skipped entityType would drop the type and its key entirely.
	if !strings.Contains(src, "primary") {
		t.Errorf("lower-cased entityType lost the primary key:\n%s", src)
	}
	// A skipped propertyType would leave age on the String fallback.
	if !strings.Contains(src, "age Integer") {
		t.Errorf("lower-cased propertyType did not resolve to Integer:\n%s", src)
	}
}
