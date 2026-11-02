package instance

import (
	"context"
	"errors"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

// panicContext panics on its first Err call, which evaluateInvariants makes
// inside the window it must recover.
type panicContext struct{ context.Context } //nolint:containedctx // a decorator holds the context it wraps

func (panicContext) Err() error { panic("context down") }

func TestEvaluateInvariants_RecoversAPanicAsInternalError(t *testing.T) {
	s, res := schema.LoadString(t.Context(), `schema "p"

type Doc {
    id String primary

    ! "m" id != ""
}
`, "p.yammm")
	if !res.OK() {
		t.Fatal(res)
	}
	typ, _ := s.Type("Doc")
	v := NewValidator(s)
	collector := diag.NewCollectorUnlimited()

	inst := newValidInstance("Doc", typ, immutable.Key{}, immutable.WrapProperties(map[string]any{"id": "d"}), nil, nil, location.Provenance{})
	err := v.evaluateInvariants(panicContext{t.Context()}, typ, "Doc", inst, collector, location.Provenance{}, path.Root())
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
