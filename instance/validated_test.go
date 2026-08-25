package instance_test

import (
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

const validatedTokenSchema = `schema "att"

type Parent {
	id String primary
	*-> ITEMS (many) Item
}

part type Item {
	id String primary
}
`

// TestNewValidInstance_CannotMintTheToken pins the forge boundary: the
// exported constructor asserts the caller's claim and cannot set the
// validated bit. Only the validator's unexported constructor proves.
func TestNewValidInstance_CannotMintTheToken(t *testing.T) {
	t.Parallel()

	vi := instance.NewValidInstance(
		"Parent", schema.TypeID{}, immutable.Key{}, immutable.Properties{},
		nil, nil, nil,
	)
	if vi.Validated() {
		t.Fatal("NewValidInstance minted the validated token; only the validator may set it")
	}
}

// TestValidateOne_SetsValidatedOnRootAndComposedChildren pins that every
// validator exit carries the token, composed children included — they
// funnel through the same constructor.
func TestValidateOne_SetsValidatedOnRootAndComposedChildren(t *testing.T) {
	t.Parallel()

	s, res := schema.LoadString(t.Context(), validatedTokenSchema, "att.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	v := instance.NewValidator(s)

	vi, result := v.ValidateOne(t.Context(), "Parent", instance.RawInstance{Properties: map[string]any{
		"id":    "p1",
		"items": []any{map[string]any{"id": "c1"}},
	}})
	if result.HasErrors() {
		t.Fatalf("ValidateOne: %s", result.String())
	}
	if !vi.Validated() {
		t.Fatal("validator output does not report Validated()")
	}

	children := 0
	for _, comp := range vi.Compositions() {
		slice, ok := comp.Unwrap().(immutable.Slice)
		if !ok {
			t.Fatalf("composition value is %T, want immutable.Slice", comp.Unwrap())
		}
		for i := range slice.Len() {
			child, ok := slice.Get(i).Unwrap().(*instance.ValidInstance)
			if !ok {
				t.Fatalf("composed child is %T, want *instance.ValidInstance", slice.Get(i).Unwrap())
			}
			if !child.Validated() {
				t.Errorf("composed child %d does not report Validated()", i)
			}
			children++
		}
	}
	if children != 1 {
		t.Fatalf("walked %d composed children, want 1", children)
	}
}
