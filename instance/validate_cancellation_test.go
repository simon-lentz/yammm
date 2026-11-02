package instance

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// cancellationSchema declares a foldable property beside two relations whose field
// names fold, so one row can carry a property error, a property-name collision
// and a relation collision at once.
func cancellationSchema(t *testing.T) schema.Schema {
	t.Helper()
	const src = `schema "cancellation"

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
	s, res := schema.LoadString(t.Context(), src, "cancellation.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

// TestValidate_EachGateReadsItsOwnPassesErrors pins that an error from one pass
// does not stop a later one. The gates asked HasErrors, so any earlier issue —
// a property error, or the member index's own collision report — silenced every
// pass after it and a row reported one defect per round trip. Returning the
// gates to HasErrors turns these red.
func TestValidate_EachGateReadsItsOwnPassesErrors(t *testing.T) {
	t.Parallel()
	v := NewValidator(cancellationSchema(t))

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

// TestValidateOne_CancelledMidRowReturnsTheCancellationAlone pins that
// ValidateOne applies the batch rule. Validate drops a cancelled row's partial
// diagnostics and returns one E_CONTEXT_CANCELLED; ValidateOne returned the
// partial set, so BatchAssembler.addSerial and the snapshot revalidator — its
// two callers — reported a timeout's leftovers as findings about the data.
// Removing ValidateOne's post-row check turns this red.
func TestValidateOne_CancelledMidRowReturnsTheCancellationAlone(t *testing.T) {
	t.Parallel()
	v := NewValidator(cancellationSchema(t))
	raw := RawInstance{Properties: map[string]any{"id": "r1", "Count": "not a number"}}
	total := yammmtest.CountChecks(t.Context(), func(ctx context.Context) { v.ValidateOne(ctx, "Row", raw) })

	for n := range total {
		ctx := yammmtest.NewCheckContext(t.Context(), n+1)
		inst, res := v.ValidateOne(ctx, "Row", raw)
		if !inst.IsZero() {
			t.Errorf("check %d: a cancelled row yielded an instance", n+1)
		}
		if !res.HasCode(diag.E_CONTEXT_CANCELLED) {
			t.Fatalf("check %d: the cancellation is missing: %s", n+1, res)
		}
		for is := range res.Issues() {
			if is.Code() != diag.E_CONTEXT_CANCELLED {
				t.Errorf("check %d: a cancelled row kept a partial diagnostic: %s %s", n+1, is.Code(), is.Message())
			}
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
	v := NewValidator(cancellationSchema(t))
	raws := []RawInstance{{Properties: map[string]any{"id": "r1", "Count": "not a number"}}}
	total := yammmtest.CountChecks(t.Context(), func(ctx context.Context) { v.Validate(ctx, "Row", raws) })

	for n := range total {
		_, res := v.Validate(yammmtest.NewCheckContext(t.Context(), n+1), "Row", raws)
		if !res.HasCode(diag.E_CONTEXT_CANCELLED) {
			t.Errorf("check %d: a row cancelled while it was erroring did not record the cancellation: %s", n+1, res)
		}
	}
}

// A child that cancels must not add a second E_CONTEXT_CANCELLED as the parent
// unwinds through its own exits.
func TestValidate_NestedBatchCancellationIsReportedOnce(t *testing.T) {
	t.Parallel()
	v := NewValidator(cancellationSchema(t))
	raws := []RawInstance{{
		Properties: map[string]any{
			"id":    "r1",
			"parts": []any{map[string]any{"id": "p1", "Id": "p2"}},
		},
	}}
	total := yammmtest.CountChecks(t.Context(), func(ctx context.Context) { v.Validate(ctx, "Row", raws) })

	for at := range total {
		_, res := v.Validate(yammmtest.NewCheckContext(t.Context(), at+1), "Row", raws)
		n := 0
		for is := range res.Issues() {
			if is.Code() == diag.E_CONTEXT_CANCELLED {
				n++
			}
		}
		if n != 1 {
			t.Errorf("check %d: the cancellation was reported %d times, want once: %s", at+1, n, res)
		}
	}
}

// panicsAtCheck is a context that panics at its at-th Err call and reports
// [context.Canceled] from then on.
type panicsAtCheck struct {
	context.Context //nolint:containedctx // a decorator holds the context it wraps
	at, seen        int64
}

func (c *panicsAtCheck) Err() error {
	c.seen++
	if c.seen == c.at {
		panic("a check panics")
	}
	if c.seen > c.at {
		return context.Canceled
	}
	return nil
}

// validateOneRecovering is ValidateOne on ctx, and reports false when a panic
// left it.
func validateOneRecovering(ctx context.Context, v *Validator, typeName string, raw RawInstance) (res diag.Result, returned bool) {
	defer func() {
		if recover() != nil {
			returned = false
		}
	}()
	_, res = v.ValidateOne(ctx, typeName, raw)
	return res, true
}

// A panic in a row's invariant is a library defect, and it reaches the caller
// beside the cancellation that follows it, stamped with the row it stopped on.
func TestValidate_ARowsInternalErrorOutlivesTheCancellation(t *testing.T) {
	t.Parallel()
	s, loaded := schema.LoadString(t.Context(), `schema "defect"

type Row {
	id String primary
	! "named" id != "bad"
}
`, "defect.yammm")
	if loaded.HasErrors() {
		t.Fatalf("load: %s", loaded)
	}
	v := NewValidator(s)
	raws := []RawInstance{{Properties: map[string]any{"id": "a"}}}
	total := yammmtest.CountChecks(t.Context(), func(ctx context.Context) { v.Validate(ctx, "Row", raws) })

	defects := 0
	for at := range total {
		func() {
			defer func() { _ = recover() }()
			valid, res := v.Validate(&panicsAtCheck{Context: t.Context(), at: at + 1}, "Row", raws)
			if !res.HasCode(diag.E_INTERNAL) {
				return
			}
			defects++
			if valid != nil {
				t.Errorf("check %d: a cancelled batch returned instances: %v", at+1, valid)
			}
			if !res.HasCode(diag.E_CONTEXT_CANCELLED) {
				t.Errorf("check %d: the cancellation is missing beside the defect: %s", at+1, res)
			}
		}()
	}
	if defects == 0 {
		t.Error("no check drew an E_INTERNAL")
	}
}

// A panic in a composed child's invariant is a library defect, and it reaches the
// caller beside the cancellation that follows it, for each child of the batch.
func TestValidateOne_AComposedChildsInternalErrorOutlivesTheCancellation(t *testing.T) {
	t.Parallel()
	s, loaded := schema.LoadString(t.Context(), `schema "defect"

part type Line {
	id String primary
	! "named" id != "bad"
}

type Order {
	id String primary
	*-> LINES (_:many) Line
}
`, "defect.yammm")
	if loaded.HasErrors() {
		t.Fatalf("load: %s", loaded)
	}
	v := NewValidator(s)
	raw := RawInstance{Properties: map[string]any{
		"id":    "o1",
		"lines": []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}},
	}}
	total := yammmtest.CountChecks(t.Context(), func(ctx context.Context) { v.ValidateOne(ctx, "Order", raw) })

	defects := make(map[string]bool)
	for at := range total {
		res, returned := validateOneRecovering(&panicsAtCheck{Context: t.Context(), at: at + 1}, v, "Order", raw)
		if !returned {
			continue
		}
		for is := range res.Issues() {
			if is.Code() != diag.E_INTERNAL {
				continue
			}
			defects[is.Path()] = true
			if !res.HasCode(diag.E_CONTEXT_CANCELLED) {
				t.Errorf("check %d: the cancellation is missing beside the defect: %s", at+1, res)
			}
		}
	}
	for _, path := range []string{"$.lines[0]", "$.lines[1]"} {
		if !defects[path] {
			t.Errorf("no check drew an E_INTERNAL at %s; drew %v", path, defects)
		}
	}
}

// TestKeepInternalErrors pins the rule directly. A row's Fatal E_INTERNAL
// survives the cancellation that drops the rest, so a library defect is never
// reported as a deadline.
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

// TestKeepInternalErrors_CountsTheInternalErrorsItsInputDropped pins that the
// kept result counts every E_INTERNAL the row saw, including one its own
// capped collector could not store: two defects through a one-slot collector
// read as two, one of them dropped.
func TestKeepInternalErrors_CountsTheInternalErrorsItsInputDropped(t *testing.T) {
	t.Parallel()
	c := diag.NewCollector(1)
	c.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL, "a library defect").Build())
	c.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL, "a second library defect").Build())

	kept := keepInternalErrors(c.Result())

	if got := kept.SeverityCounts().Fatal; got != 2 {
		t.Errorf("SeverityCounts().Fatal = %d, want 2", got)
	}
	if got := kept.CodeCounts(diag.Fatal)[diag.E_INTERNAL]; got != 2 {
		t.Errorf("CodeCounts(Fatal)[E_INTERNAL] = %d, want 2", got)
	}
	if got := kept.Len(); got != 1 {
		t.Errorf("Len() = %d, want the one stored E_INTERNAL", got)
	}
	if !kept.LimitReached() {
		t.Error("LimitReached() = false, want true")
	}
	if got := kept.DroppedCount(); got != 1 {
		t.Errorf("DroppedCount() = %d, want 1", got)
	}
}
