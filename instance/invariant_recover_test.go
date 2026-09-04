package instance

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

// panicHandler is a slog.Handler whose Handle panics: the evaluator logs
// its trace op from inside EvaluateBool, so the panic lands inside the
// window evaluateInvariants must recover.
type panicHandler struct{}

func (panicHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (panicHandler) Handle(context.Context, slog.Record) error { panic("handler down") }
func (h panicHandler) WithAttrs([]slog.Attr) slog.Handler      { return h }
func (h panicHandler) WithGroup(string) slog.Handler           { return h }

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
	v := NewValidator(s, WithLogger(slog.New(panicHandler{})))
	collector := diag.NewCollectorUnlimited()

	err := v.evaluateInvariants(t.Context(), typ, "Doc", map[string]any{"id": "d"}, nil, nil, collector, nil, path.Root())
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
