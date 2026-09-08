package instance

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// group4Schema declares a foldable property beside two relations whose field
// names fold, so one row can carry a property error, a property-name collision
// and a relation collision at once.
func group4Schema(t *testing.T) *schema.Schema {
	t.Helper()
	const src = `schema "group4"

type Target {
	id String primary
}

part type Part {
	id String primary
}

type Row {
	id String primary
	count Integer
	--> LINK (_) Target
	*-> PARTS (one:many) Part
}
`
	s, res := schema.LoadString(t.Context(), src, "group4.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

// ctxKey is the value a handler reads back to prove which context reached it.
type ctxKey struct{}

// recordingHandler captures each record's message and the context it was
// handled with, which is the only way to see whether the caller's context
// reached the emit site.
type recordingHandler struct {
	mu      sync.Mutex
	msgs    []string
	ctxVals []any
	onMsg   func(msg string)
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	h.msgs = append(h.msgs, r.Message)
	h.ctxVals = append(h.ctxVals, ctx.Value(ctxKey{}))
	h.mu.Unlock()
	if h.onMsg != nil {
		h.onMsg(r.Message)
	}
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) sawContextValue(want any) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, msg := range h.msgs {
		if msg == "property name normalized" && h.ctxVals[i] == want {
			return true
		}
	}
	return false
}

// TestValidate_NormalizationRecordCarriesTheCallersContext pins the claim
// WithLogger's godoc, the package doc and docs/API.md all make: each record is
// logged with the context passed to Validate. The normalization record was
// emitted through slog.Logger.Debug, which the standard library hard-codes to
// context.Background, so a request id on the caller's context reached every
// record but this one. Emitting it through Debug again turns this red.
func TestValidate_NormalizationRecordCarriesTheCallersContext(t *testing.T) {
	t.Parallel()
	h := &recordingHandler{}
	v := NewValidator(group4Schema(t), WithLogger(slog.New(h)))

	ctx := context.WithValue(t.Context(), ctxKey{}, "req-1")
	// "Count" folds onto "count", which is what emits the record.
	if _, res := v.ValidateOne(ctx, "Row", RawInstance{
		Properties: map[string]any{
			"id": "r1", "Count": int64(1),
			"parts": []any{map[string]any{"id": "p1"}},
		},
	}); res.HasErrors() {
		t.Fatalf("the row does not validate: %s", res)
	}

	if !h.sawContextValue("req-1") {
		t.Errorf("the normalization record did not carry the caller's context; records seen: %v", h.msgs)
	}
}

// TestValidate_EachGateReadsItsOwnPassesErrors pins that an error from one pass
// does not stop a later one. The gates asked HasErrors, so any earlier issue —
// a property error, or the member index's own collision report — silenced every
// pass after it and a row reported one defect per round trip. Returning the
// gates to HasErrors turns these red.
func TestValidate_EachGateReadsItsOwnPassesErrors(t *testing.T) {
	t.Parallel()
	v := NewValidator(group4Schema(t))

	t.Run("a property error beside a relation collision reports both", func(t *testing.T) {
		t.Parallel()
		_, res := v.ValidateOne(t.Context(), "Row", RawInstance{
			Properties: map[string]any{
				"id": "r1", "count": "not a number",
				"Link":  map[string]any{"_target_id": "t1"},
				"LINK":  map[string]any{"_target_id": "t2"},
				"parts": []any{map[string]any{"id": "p1"}},
			},
		})
		if !res.HasCode(ErrTypeMismatch) {
			t.Errorf("the property error is missing: %s", res)
		}
		if !res.HasCode(ErrCaseFoldCollision) {
			t.Errorf("the relation collision is missing: %s", res)
		}
	})

	t.Run("an association collision beside an absent required composition reports both", func(t *testing.T) {
		t.Parallel()
		_, res := v.ValidateOne(t.Context(), "Row", RawInstance{
			Properties: map[string]any{
				"id":   "r1",
				"Link": map[string]any{"_target_id": "t1"},
				"LINK": map[string]any{"_target_id": "t2"},
			},
		})
		if !res.HasCode(ErrCaseFoldCollision) {
			t.Errorf("the association collision is missing: %s", res)
		}
		if !res.HasCode(ErrUnresolvedRequiredComposition) {
			t.Errorf("the absent required composition is missing: %s", res)
		}
	})

	t.Run("the collision message names both keys", func(t *testing.T) {
		t.Parallel()
		_, res := v.ValidateOne(t.Context(), "Row", RawInstance{
			Properties: map[string]any{
				"id":    "r1",
				"Link":  map[string]any{"_target_id": "t1"},
				"LINK":  map[string]any{"_target_id": "t2"},
				"parts": []any{map[string]any{"id": "p1"}},
			},
		})
		var msg string
		for is := range res.Issues() {
			if is.Code() == ErrCaseFoldCollision {
				msg = is.Message()
			}
		}
		if !strings.Contains(msg, "LINK") || !strings.Contains(msg, "Link") {
			t.Errorf("the collision message names one key, not both: %q", msg)
		}
	})
}

// cancelOnNormalize cancels the context the moment the normalization record is
// emitted, which puts the cancellation inside the row rather than between rows.
func cancelOnNormalize(t *testing.T, s *schema.Schema) (*Validator, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	h := &recordingHandler{onMsg: func(msg string) {
		if msg == "property name normalized" {
			cancel()
		}
	}}
	return NewValidator(s, WithLogger(slog.New(h))), ctx
}

// TestValidateOne_CancelledMidRowReturnsTheCancellationAlone pins that
// ValidateOne applies the batch rule. Validate drops a cancelled row's partial
// diagnostics and returns one E_CONTEXT_CANCELLED; ValidateOne returned the
// partial set, so BatchAssembler.addSerial and the snapshot revalidator — its
// two callers — reported a timeout's leftovers as findings about the data.
// Removing ValidateOne's post-row check turns this red.
func TestValidateOne_CancelledMidRowReturnsTheCancellationAlone(t *testing.T) {
	t.Parallel()
	v, ctx := cancelOnNormalize(t, group4Schema(t))

	inst, res := v.ValidateOne(ctx, "Row", RawInstance{
		Properties: map[string]any{"id": "r1", "Count": "not a number"},
	})
	if inst != nil {
		t.Error("a cancelled row yielded an instance")
	}
	if !res.HasCode(diag.E_CONTEXT_CANCELLED) {
		t.Fatalf("the cancellation is missing: %s", res)
	}
	for is := range res.Issues() {
		if is.Code() != diag.E_CONTEXT_CANCELLED {
			t.Errorf("a cancelled row kept a partial diagnostic: %s %s", is.Code(), is.Message())
		}
	}
}

// TestValidateProperties_CancellationRecordedAtEveryExit pins that a row
// cancelled mid-validation records its cancellation whatever else it drew. The
// three gates returned on HasErrors before asking cancelled, so a row that
// errored and was cancelled in the same pass reported only the error and the
// caller could not tell a failed row from an abandoned one.
func TestValidateProperties_CancellationRecordedAtEveryExit(t *testing.T) {
	t.Parallel()
	v, ctx := cancelOnNormalize(t, group4Schema(t))

	_, res := v.Validate(ctx, "Row", []RawInstance{{
		Properties: map[string]any{"id": "r1", "Count": "not a number"},
	}})
	if !res.HasCode(diag.E_CONTEXT_CANCELLED) {
		t.Errorf("a row cancelled while it was erroring did not record the cancellation: %s", res)
	}
}

// TestValidate_NestedBatchCancellationIsReportedOnce pins cancelled's once
// guard across a nested composed batch: a child that cancels must not add a
// second E_CONTEXT_CANCELLED as the parent unwinds through its own exits.
func TestValidate_NestedBatchCancellationIsReportedOnce(t *testing.T) {
	t.Parallel()
	v, ctx := cancelOnNormalize(t, group4Schema(t))

	_, res := v.Validate(ctx, "Row", []RawInstance{{
		Properties: map[string]any{
			"id":    "r1",
			"parts": []any{map[string]any{"id": "p1", "Id": "p2"}},
		},
	}})
	n := 0
	for is := range res.Issues() {
		if is.Code() == diag.E_CONTEXT_CANCELLED {
			n++
		}
	}
	if n > 1 {
		t.Errorf("the cancellation was reported %d times, want at most once: %s", n, res)
	}
}

// TestKeepInternalErrors pins A-370's rule directly. A row's Fatal E_INTERNAL
// survives the cancellation that drops the rest, so a library defect is never
// reported as a deadline. The end-to-end trigger is a panic inside the
// validator that no probe has reached from a loaded schema, so the rule is
// pinned at the function that states it.
func TestKeepInternalErrors(t *testing.T) {
	t.Parallel()
	c := diag.NewCollectorUnlimited()
	c.Collect(diag.NewIssue(diag.Error, ErrTypeMismatch, "a partial finding").Build())
	c.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL, "a library defect").Build())
	c.Collect(diag.NewIssue(diag.Error, ErrMissingRequired, "another partial finding").Build())

	kept := keepInternalErrors(c.Result())

	n := 0
	for is := range kept.Issues() {
		n++
		if is.Code() != diag.E_INTERNAL {
			t.Errorf("a partial finding survived: %s %s", is.Code(), is.Message())
		}
	}
	if n != 1 {
		t.Errorf("kept %d issues, want the one E_INTERNAL", n)
	}

	empty := diag.NewCollectorUnlimited()
	empty.Collect(diag.NewIssue(diag.Error, ErrTypeMismatch, "a partial finding").Build())
	if kept := keepInternalErrors(empty.Result()); kept.HasErrors() {
		t.Errorf("a row with no internal error kept something: %s", kept)
	}
}
