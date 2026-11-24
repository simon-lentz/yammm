package instance_test

import (
	"errors"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
)

const personCompany = `schema "p"

type Company {
    id String primary
}

type Person {
    id String primary
    name String
    --> WORKS_AT (one) Company {
        title String
        note String required
    }
}
`

// A present-but-invalid required edge property is a type error, not a missing
// one — the same rule the node-property path applies.
func TestEdgeProperty_PresentButInvalidIsNotMissing(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, personCompany))
	_, err := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
		"id": "p", "works_at": map[string]any{"_target_id": "c1", "note": 42},
	}})
	if !errors.Is(err, diag.E_TYPE_MISMATCH) || errors.Is(err, diag.E_MISSING_REQUIRED) {
		t.Errorf("want E_TYPE_MISMATCH alone, got %s", err)
	}
}

// Only the target's own FK fields are FK fields; any other reserved-prefix key
// is an unknown edge field, exactly the typo the code exists to catch.
func TestEdgeTarget_TypoedFKFieldIsUnknown(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, personCompany))
	_, res := diagtest.Issues(t, func() (instance.ValidInstance, error) {
		return v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
			"id": "p", "works_at": map[string]any{"_target_id": "c1", "_target_bogus": "z", "note": "n"},
		}})
	})
	if got := mustIssue(t, res, diag.E_UNKNOWN_EDGE_FIELD).Path(); got != "$.works_at._target_bogus" {
		t.Errorf("path = %q", got)
	}
	// A typo'd key with no correct key beside it is still unknown, reported
	// beside the missing target rather than hidden behind it.
	_, res = diagtest.Issues(t, func() (instance.ValidInstance, error) {
		return v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
			"id": "p", "works_at": map[string]any{"_target_di": "c1", "note": "n"},
		}})
	})
	if !res.HasCode(diag.E_MISSING_FK_TARGET) || !res.HasCode(diag.E_UNKNOWN_EDGE_FIELD) {
		t.Errorf("want E_MISSING_FK_TARGET beside E_UNKNOWN_EDGE_FIELD, got %s", res)
	}
}

// A null FK component is reported against the target key's underlying kind,
// through any DataType alias the key is declared with.
func TestEdgeTarget_NullFKNamesTheUnderlyingKind(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, `schema "p"

type Code = String

type Company {
    id Code primary
}

type Person {
    id String primary
    --> WORKS_AT (one) Company
}
`))
	_, res := diagtest.Issues(t, func() (instance.ValidInstance, error) {
		return v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
			"id": "p", "works_at": map[string]any{"_target_id": nil},
		}})
	})
	is := mustIssue(t, res, diag.E_TYPE_MISMATCH)
	yammmtest.Diff(t, []string{"string"}, detailValues(is, "expected"))
}
