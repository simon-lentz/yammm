package instance_test

import (
	"testing"

	"github.com/simon-lentz/yammm/instance"
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

// One collision is one diagnostic: the colliding inputs are accounted for by
// the collision and are not also reported as unknown fields.
func TestCaseFoldCollision_IsOneDiagnostic(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, personCompany))
	_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
		"id": "p", "Name": "x", "NAME": "y",
	}})
	if res.Len() != 1 {
		t.Errorf("want one diagnostic, got %s", res)
	}
	if got := mustIssue(t, res, instance.ErrCaseFoldCollision).Path(); got != "$" {
		t.Errorf("path = %q, want $", got)
	}
}

// The rule buildPropertyMapping states holds on the edge path too: an exact
// match is claimed first and never collides; the variant is then unknown and
// says which property shadowed it.
func TestEdgeProperty_ExactMatchIsNeverACollision(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, personCompany))
	_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
		"id": "p", "works_at": map[string]any{"_target_id": "c1", "note": "n", "title": "eng", "TITLE": "eng2"},
	}})
	if res.HasCode(instance.ErrCaseFoldCollision) {
		t.Errorf("an exact match collided: %s", res)
	}
	is := mustIssue(t, res, instance.ErrUnknownEdgeField)
	yammmtest.Diff(t, []string{"case_fold_shadowed"}, detailValues(is, "reason"))
	if is.Path() != "$.works_at.TITLE" {
		t.Errorf("path = %q", is.Path())
	}
}

// A present-but-invalid required edge property is a type error, not a missing
// one — the same rule the node-property path applies.
func TestEdgeProperty_PresentButInvalidIsNotMissing(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, personCompany))
	_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
		"id": "p", "works_at": map[string]any{"_target_id": "c1", "note": 42},
	}})
	if !res.HasCode(instance.ErrTypeMismatch) || res.HasCode(instance.ErrMissingRequired) {
		t.Errorf("want E_TYPE_MISMATCH alone, got %s", res)
	}
}

// Only the target's own FK fields are FK fields; any other reserved-prefix key
// is an unknown edge field, exactly the typo the code exists to catch.
func TestEdgeTarget_TypoedFKFieldIsUnknown(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, personCompany))
	_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
		"id": "p", "works_at": map[string]any{"_target_id": "c1", "_target_bogus": "z", "note": "n"},
	}})
	if got := mustIssue(t, res, instance.ErrUnknownEdgeField).Path(); got != "$.works_at._target_bogus" {
		t.Errorf("path = %q", got)
	}
}

// FK fields fold under the default mode like every other input key; the
// reserved prefix is itself reserved case-insensitively.
func TestEdgeTarget_FKFieldFoldsUnderNonStrict(t *testing.T) {
	s := loadSrc(t, personCompany)
	raw := func() instance.RawInstance {
		return instance.RawInstance{Properties: map[string]any{
			"id": "p", "works_at": map[string]any{"_Target_ID": "c1", "note": "n"},
		}}
	}
	if _, res := instance.NewValidator(s).ValidateOne(t.Context(), "Person", raw()); !res.OK() {
		t.Errorf("non-strict: %s", res)
	}
	if _, res := instance.NewValidator(s, instance.WithStrictPropertyNames(true)).ValidateOne(t.Context(), "Person", raw()); !res.HasCode(instance.ErrMissingFKTarget) {
		t.Errorf("strict: want E_MISSING_FK_TARGET, got %s", res)
	}
}

// A composition-field collision is the whole of what is wrong; the slot is not
// also reported absent.
func TestCompositionCollision_IsOneDiagnostic(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, docSrc))
	_, res := v.ValidateOne(t.Context(), "Doc", instance.RawInstance{Properties: map[string]any{
		"id": "d", "Lines": []any{map[string]any{"n": "a"}}, "LINES": []any{map[string]any{"n": "b"}},
	}})
	if !res.HasCode(instance.ErrCaseFoldCollision) || res.HasCode(instance.ErrUnresolvedRequiredComposition) {
		t.Errorf("want the collision alone, got %s", res)
	}
}

// Schema identifiers are ASCII, so the fold is ASCII: a key carrying a
// non-ASCII letter matches no schema name however it lowercases.
func TestFold_NeverMatchesANonASCIIKey(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, `schema "p"

type T {
    id String primary
    key String
}
`))
	_, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{
		"id": "t", "\u212Aey": "v", // KELVIN SIGN + "ey"; strings.ToLower gives "key"
	}})
	if !res.HasCode(instance.ErrUnknownField) {
		t.Errorf("want E_UNKNOWN_FIELD, got %s", res)
	}
}

// A collision between two edge-property keys is anchored on the edge object
// they both belong to, not on whichever key iteration reached second.
func TestEdgePropertyCollision_PathIsTheTargetObject(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, personCompany))
	for range 10 {
		_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
			"id": "p", "works_at": map[string]any{"_target_id": "c1", "note": "n", "Title": "a", "TITLE": "b"},
		}})
		if got := mustIssue(t, res, instance.ErrCaseFoldCollision).Path(); got != "$.works_at" {
			t.Fatalf("path = %q, want $.works_at", got)
		}
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
	_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
		"id": "p", "works_at": map[string]any{"_target_id": nil},
	}})
	is := mustIssue(t, res, instance.ErrTypeMismatch)
	yammmtest.Diff(t, []string{"string"}, detailValues(is, "expected"))
}
