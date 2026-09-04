package instance

import (
	"errors"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

// A panic anywhere on the invariant path — the scope build included, which
// runs only for a type that declares an invariant — is recovered into an
// InternalError of KindInvariantPanic carrying its stack, and the caller
// reports it as Fatal E_INTERNAL. A composed slot holding a nil child is the
// panic source: no validated instance carries one, so the recover is the only
// guard between the panic and the process.
func TestEvaluateInvariants_RecoversAPanicAsInternalError(t *testing.T) {
	s, res := schema.LoadString(t.Context(), `schema "p"

part type Line {
    n String primary
}

type Doc {
    id String primary
    *-> LINES (many) Line

    ! "m" LINES -> Len >= 0
}
`, "p.yammm")
	if !res.OK() {
		t.Fatal(res)
	}
	typ, _ := s.Type("Doc")
	v := NewValidator(s)
	collector := diag.NewCollectorUnlimited()
	composed := map[string]immutable.Value{"LINES": immutable.Wrap([]*ValidInstance{nil})}

	err := v.evaluateInvariants(t.Context(), typ, "Doc", map[string]any{"id": "d"}, nil, composed, collector, nil, path.Root())
	var internalErr *InternalError
	if !errors.As(err, &internalErr) {
		t.Fatalf("err = %v, want *InternalError", err)
	}
	if internalErr.Kind != KindInvariantPanic || internalErr.Stack == "" || !errors.Is(err, ErrInternalFailure) {
		t.Errorf("kind=%v has-stack=%v is-internal=%v", internalErr.Kind, internalErr.Stack != "", errors.Is(err, ErrInternalFailure))
	}
	if collector.HasErrors() {
		t.Errorf("the panic was reported as an ordinary diagnostic: %s", collector.Result())
	}
}
