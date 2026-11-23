package schema_test

import (
	"errors"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/schema"
)

// @writeOnce is rejected on every property of a part type — the SPEC's
// eligibility rule is a property of the HOLDER, so it must hold wherever the
// property was declared. An own-only walk sees the declared case and never the
// inherited one, which loaded clean.
func TestAnnotation_WriteOnce_RejectedOnPartTypeWhereverDeclared(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"declared on the part type": `schema "s"

type Car {
	vin String primary
	*-> WHEEL (one) Wheel
}

part type Wheel {
	pos String @writeOnce
}
`,
		"inherited by the part type": `schema "s"

type Car {
	vin String primary
	*-> WHEEL (one) Wheel
}

abstract type Base {
	pos String @writeOnce
}

part type Wheel extends Base {
	extra String
}
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := schema.LoadString(t.Context(), src, "part.yammm")
			if !errors.Is(err, diag.E_INVALID_ANNOTATION_TARGET) {
				t.Fatalf("@writeOnce on a part type's property must draw E_INVALID_ANNOTATION_TARGET; got %v", err)
			}
		})
	}
}

// The inclusive walk must not re-report an inherited @writeOnce on a NON-part
// subtype: the annotation is legal there, and the own-body walk already
// validated it at its declaring type.
func TestAnnotation_WriteOnce_InheritedByConcreteSubtypeIsLegal(t *testing.T) {
	t.Parallel()
	_, err := schema.LoadString(t.Context(), `schema "s"

type Base {
	id String primary
	state String @writeOnce
}

type Leaf extends Base {
	extra String
}
`, "leaf.yammm")
	if err != nil {
		t.Fatalf("an inherited @writeOnce on a concrete subtype is legal; got %v", err)
	}
}

// A @writeOnce declared on the part type itself is reported once, by the
// own-body walk; the holder walk must skip own declarations rather than
// report the same defect a second time.
func TestAnnotation_WriteOnce_DeclaredOnPartTypeReportsOnce(t *testing.T) {
	t.Parallel()
	_, res := diagtest.Capture(t, func(warn func(diag.Issue)) (schema.Schema, error) {
		return schema.LoadString(t.Context(), `schema "s"

type Car {
	vin String primary
	*-> WHEEL (one) Wheel
}

part type Wheel {
	pos String @writeOnce
}
`, "part.yammm", schema.WithWarnings(warn))
	})
	var n int
	for iss := range res.Issues() {
		if iss.Code() == diag.E_INVALID_ANNOTATION_TARGET {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("E_INVALID_ANNOTATION_TARGET reported %d times, want exactly 1: %v", n, res)
	}
}
