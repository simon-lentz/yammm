package instance_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

func detail(is diag.Issue, key string) (string, bool) {
	for _, d := range is.Details() {
		if d.Key == key {
			return d.Value, true
		}
	}
	return "", false
}

func countCode(res diag.Result, code diag.Code) int {
	n := 0
	for is := range res.Issues() {
		if is.Code() == code {
			n++
		}
	}
	return n
}

// One member index for every input key: a key that folds onto a relation
// field an exact key already claimed is unknown, as it is for a property —
// it is not silently a relation field.
func TestUnknownField_FoldingOntoAClaimedRelationFieldIsUnknown(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadSrc(t, personCompany))
	_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
		"id": "p", "name": "n",
		"works_at": map[string]any{"_target_id": "c1", "note": "n"},
		"Works_At": map[string]any{"_target_id": "c2", "note": "n"},
	}})
	is := mustIssue(t, res, instance.ErrUnknownField)
	if reason, _ := detail(is, diag.DetailKeyReason); reason != "case_fold_shadowed" {
		t.Errorf("reason = %q, want case_fold_shadowed", reason)
	}
	if is.Path() != "$.Works_At" {
		t.Errorf("path = %q", is.Path())
	}
}

// An absent required composition is reported beside a sibling composition's
// own error: the gate is the relation's state, never the collector's.
func TestComposition_AbsentRequiredIsReportedBesideASiblingsError(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\npart type Line {\n\tid String primary\n}\n\npart type Note {\n\tid String primary\n}\n\ntype Doc {\n\tid String primary\n\t*-> LINES (many) Line\n\t*-> NOTES (one:many) Note\n}\n")
	v := instance.NewValidator(s)
	_, res := v.ValidateOne(t.Context(), "Doc", instance.RawInstance{Properties: map[string]any{
		"id": "d", "lines": []any{map[string]any{"id": 5}}, // a child with a type error; NOTES absent
	}})
	if !res.HasCode(instance.ErrTypeMismatch) {
		t.Fatalf("control: want the child's E_TYPE_MISMATCH, got %v", codes(res))
	}
	if !res.HasCode(instance.ErrUnresolvedRequiredComposition) {
		t.Errorf("the absent required NOTES was suppressed by LINES' error: %v", codes(res))
	}
}

// cancelOnNormalize is a slog handler that cancels a context the first time
// the validator logs a property-name normalization: give one row a folded key
// and the cancellation lands inside that row, past the batch's loop head.
type cancelOnNormalize struct {
	cancel context.CancelFunc
}

func (cancelOnNormalize) Enabled(context.Context, slog.Level) bool { return true }
func (h cancelOnNormalize) Handle(_ context.Context, r slog.Record) error {
	if r.Message == "property name normalized" {
		h.cancel()
	}
	return nil
}
func (h cancelOnNormalize) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h cancelOnNormalize) WithGroup(string) slog.Handler      { return h }

// One cancellation rule per batch: a batch that did not complete returns nil
// and exactly one E_CONTEXT_CANCELLED, stamped with the row it stopped on.
func TestValidate_CancellationMidBatchIsReportedOnceOnItsRow(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\ntype T {\n\tid String primary\n}\n")
	raws := []instance.RawInstance{
		{Properties: map[string]any{"id": "a"}},
		{Properties: map[string]any{"ID": "b"}}, // folds, so row 1 logs the normalization
		{Properties: map[string]any{"id": "c"}},
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	v := instance.NewValidator(s, instance.WithLogger(slog.New(cancelOnNormalize{cancel: cancel})))
	valids, res := v.Validate(ctx, "T", raws)
	if valids != nil {
		t.Errorf("a cancelled batch returned a slice of %d", len(valids))
	}
	if n := countCode(res, diag.E_CONTEXT_CANCELLED); n != 1 {
		t.Fatalf("%d E_CONTEXT_CANCELLED issues, want exactly 1: %v", n, res)
	}
	is := mustIssue(t, res, diag.E_CONTEXT_CANCELLED)
	if idx, ok := detail(is, diag.DetailKeyInstanceIndex); !ok || idx != "1" {
		t.Errorf("instance_index = %q, %v; want the row it stopped on, 1", idx, ok)
	}
}

func TestValidateForComposition_CancellationMidBatchIsReportedOnceOnItsRow(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\npart type Line {\n\tid String primary\n}\n\ntype T {\n\tid String primary\n\t*-> LINES (many) Line\n}\n")
	raws := []instance.RawInstance{{Properties: map[string]any{"id": "a"}}, {Properties: map[string]any{"ID": "b"}}, {Properties: map[string]any{"id": "c"}}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	v := instance.NewValidator(s, instance.WithLogger(slog.New(cancelOnNormalize{cancel: cancel})))
	valids, res := v.ValidateForComposition(ctx, "T", "LINES", raws)
	if valids != nil {
		t.Errorf("a cancelled batch returned a slice of %d", len(valids))
	}
	if n := countCode(res, diag.E_CONTEXT_CANCELLED); n != 1 {
		t.Fatalf("%d E_CONTEXT_CANCELLED issues, want exactly 1: %v", n, res)
	}
	if idx, ok := detail(mustIssue(t, res, diag.E_CONTEXT_CANCELLED), diag.DetailKeyInstanceIndex); !ok || idx != "1" {
		t.Errorf("instance_index = %q, %v; want 1", idx, ok)
	}
}

// A cancellation before the first row is batch-wide: it is anchored on the
// batch's source at its root and names no row.
func TestValidate_CancellationBeforeTheFirstRowIsBatchWide(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\ntype T {\n\tid String primary\n}\n")
	v := instance.NewValidator(s)
	prov := location.NewProvenance("rows.json", path.Root().Index(0), location.Span{})
	raws := []instance.RawInstance{{Properties: map[string]any{"id": "a"}, Provenance: prov}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	valids, res := v.Validate(ctx, "T", raws)
	if valids != nil {
		t.Errorf("valids = %v, want nil", valids)
	}
	is := mustIssue(t, res, diag.E_CONTEXT_CANCELLED)
	if _, stamped := detail(is, diag.DetailKeyInstanceIndex); stamped {
		t.Error("a batch-wide cancellation was blamed on a row")
	}
	if is.SourceName() != "rows.json" || strings.Contains(is.Path(), "[") {
		t.Errorf("source=%q path=%q; want the batch's source at its root", is.SourceName(), is.Path())
	}
}

// A shape mismatch on a nested composition carries the relation pair — name
// and field — for the innermost relation only, so the outer composition
// does not stack a second field under the same key.
func TestComposition_ShapeMismatchCarriesTheRelationPairOnce(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\npart type Item {\n\tid String primary\n}\n\npart type Line {\n\tid String primary\n\t*-> ITEM (one) Item\n}\n\ntype Doc {\n\tid String primary\n\t*-> LINES (many) Line\n}\n")
	v := instance.NewValidator(s)
	_, res := v.ValidateOne(t.Context(), "Doc", instance.RawInstance{Properties: map[string]any{
		"id": "d", "lines": []any{map[string]any{"id": "l", "item": 5}},
	}})
	is := mustIssue(t, res, instance.ErrEdgeShapeMismatch)
	if rel, _ := detail(is, diag.DetailKeyRelationName); rel != "ITEM" {
		t.Errorf("relation = %q, want the innermost ITEM", rel)
	}
	var fields []string
	for _, d := range is.Details() {
		if d.Key == diag.DetailKeyJSONField {
			fields = append(fields, d.Value)
		}
	}
	if len(fields) != 1 || fields[0] != "item" {
		t.Errorf("json_field details = %v, want exactly [item]", fields)
	}
}

// NewValidEdgeData clones its slice, so the edge is immutable by
// construction whatever the caller does with its targets afterwards.
func TestNewValidEdgeData_DoesNotAliasTheCallerSlice(t *testing.T) {
	t.Parallel()
	targets := []instance.ValidEdgeTarget{instance.NewValidEdgeTarget(immutable.WrapKey([]any{"c1"}), immutable.Properties{})}
	edge := instance.NewValidEdgeData(targets)
	targets[0] = instance.NewValidEdgeTarget(immutable.WrapKey([]any{"c9"}), immutable.Properties{})
	if got := edge.Targets()[0].TargetKey().String(); !strings.Contains(got, "c1") {
		t.Errorf("the edge saw the caller's later mutation: %s", got)
	}
}

// Edge accepts a relation's DSL name and its field name alike, the rule every
// name-taking surface of this package follows.
func TestValidInstance_EdgeAcceptsEitherSpelling(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadSrc(t, personCompany))
	inst, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
		"id": "p", "name": "n", "works_at": map[string]any{"_target_id": "c1", "note": "n"},
	}})
	if inst == nil {
		t.Fatal(res)
	}
	for _, name := range []string{"WORKS_AT", "works_at"} {
		if _, ok := inst.Edge(name); !ok {
			t.Errorf("Edge(%q) not found", name)
		}
	}
}

// CanonicalValue returns the input beside an error, so a caller that heals
// what it can and passes through what it cannot may use the value directly.
func TestCanonicalValue_ReturnsTheInputBesideAnError(t *testing.T) {
	t.Parallel()
	got, err := instance.CanonicalValue("x", schema.NewIntegerConstraint())
	if err == nil || got != "x" {
		t.Errorf("CanonicalValue(\"x\", Integer) = %#v, %v; want the input beside an error", got, err)
	}
}

// The collision message lists its candidates in sorted order, and the unknown
// keys of an edge object are reported in sorted order, run after run.
func TestDiagnosticOrder_IsSortedAndStable(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadSrc(t, personCompany))
	for range 5 {
		_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
			"id": "p", "nAme": "x", "NAME": "y", "Name": "z",
		}})
		if msg := mustIssue(t, res, instance.ErrCaseFoldCollision).Message(); !strings.Contains(msg, "[NAME Name nAme]") {
			t.Errorf("collision candidates not sorted: %s", msg)
		}
		// A property collision ends the instance before its edges are read,
		// so the edge keys are ordered on a clean instance.
		_, res = v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
			"id": "p", "name": "n",
			"works_at": map[string]any{"_target_id": "c1", "note": "n", "zeta": 1, "alpha": 2},
		}})
		var unknown []string
		for is := range res.Issues() {
			if is.Code() == instance.ErrUnknownEdgeField {
				unknown = append(unknown, is.Path())
			}
		}
		if strings.Join(unknown, ",") != "$.works_at.alpha,$.works_at.zeta" {
			t.Errorf("unknown edge keys not in sorted order: %v", unknown)
		}
	}
}

// A null foreign-key value is reported with got=null, so a structured
// consumer reads the fact the message states.
func TestEdgeTarget_NullFKCarriesGotNull(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadSrc(t, personCompany))
	_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
		"id": "p", "name": "n", "works_at": map[string]any{"_target_id": nil, "note": "n"},
	}})
	is := mustIssue(t, res, instance.ErrTypeMismatch)
	if got, _ := detail(is, diag.DetailKeyGot); got != "null" {
		t.Errorf("got detail = %q, want null", got)
	}
}
