package instance_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

func loadT(t *testing.T, src string) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), src, "p.yammm")
	if res.Err() != nil {
		t.Fatal(res.Err())
	}
	return s
}

func codes(res diag.Result) []string {
	var out []string
	for is := range res.Issues() {
		out = append(out, is.Code().String())
	}
	return out
}

// A typed nil pointer — the shape adapter/gogen emits for an absent optional
// scalar — is the absent value at every null-rule site, exactly as an
// interface nil is. A typed nil slice or map stays an empty container.
func TestTypedNilPointer_IsAbsent(t *testing.T) {
	t.Parallel()
	s := loadT(t, `schema "p"

part type Line {
	id String primary
	note String
}

type Co {
	id String primary
}

type T {
	id String primary
	nick String
	must String required
	tags List<String>
	--> AT (one) Co
	*-> LINES (many) Line
}
`)
	v := instance.NewValidator(s)
	var nilStr *string
	t.Run("optional pointer is dropped", func(t *testing.T) {
		t.Parallel()
		vi, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": "m", "nick": nilStr}})
		if res.Err() != nil {
			t.Fatalf("refused: %v", res.Err())
		}
		if _, ok := vi.Property("nick"); ok {
			t.Error("an absent optional was stored")
		}
	})
	t.Run("required pointer is missing, not a type mismatch", func(t *testing.T) {
		t.Parallel()
		_, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": nilStr}})
		if !res.HasCode(diag.E_MISSING_REQUIRED) || res.HasCode(diag.E_TYPE_MISMATCH) {
			t.Errorf("want E_MISSING_REQUIRED alone, got %v", codes(res))
		}
	})
	t.Run("typed nil slice is an empty list", func(t *testing.T) {
		t.Parallel()
		var nilTags []any
		vi, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": "m", "tags": nilTags}})
		if res.Err() != nil {
			t.Fatalf("refused: %v", res.Err())
		}
		tv, ok := vi.Property("tags")
		sl, isList := tv.Slice()
		if !ok || !isList || sl.Len() != 0 {
			t.Errorf("a typed nil slice must be stored as an empty list: stored=%v list=%v", ok, isList)
		}
	})
	t.Run("edge value pointer is the null an explicit null is", func(t *testing.T) {
		t.Parallel()
		var nilEdge *map[string]any
		_, control := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": "m", "at": nil}})
		_, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": "m", "at": nilEdge}})
		if !control.HasCode(diag.E_EDGE_SHAPE_MISMATCH) || !res.HasCode(diag.E_EDGE_SHAPE_MISMATCH) {
			t.Errorf("explicit null drew %v, typed nil pointer drew %v; want E_EDGE_SHAPE_MISMATCH from both", codes(control), codes(res))
		}
	})
	t.Run("FK component pointer is a null component", func(t *testing.T) {
		t.Parallel()
		_, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": "m", "at": map[string]any{"_target_id": nilStr}}})
		found := false
		for is := range res.Issues() {
			if strings.Contains(is.Message(), "got null") {
				found = true
			}
		}
		if !found {
			t.Errorf("a typed nil FK component was not reported as null: %v", res.Err())
		}
	})
}

// A cancelled batch returns no partial slice: the documented "one entry per
// input instance" holds for a completed batch, and a cancelled one returns
// nil beside the cancellation, as the pre-loop check does.
func TestValidate_CancellationReturnsNoPartialSlice(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\ntype T {\n\tid String primary\n}\n")
	v := instance.NewValidator(s)
	raws := make([]instance.RawInstance, 5)
	for i := range raws {
		raws[i] = instance.RawInstance{Properties: map[string]any{"id": string(rune('a' + i))}}
	}
	ctx, cancel := context.WithCancel(t.Context())
	// Cancel after the first instance validates: the second ctx.Err() check trips.
	valids, res := v.Validate(ctx, "T", raws)
	if res.Err() != nil || len(valids) != 5 {
		t.Fatalf("control: %d valids, %v", len(valids), res.Err())
	}
	cancel()
	valids, res = v.Validate(ctx, "T", raws)
	if valids != nil {
		t.Errorf("a cancelled batch returned a partial slice of %d", len(valids))
	}
	if !res.HasCode(diag.E_CONTEXT_CANCELLED) {
		t.Errorf("want E_CANCELLED, got %v", codes(res))
	}
}

// An unknown type name is a failure whatever the batch: Validate resolves
// the name before it answers a nil or empty batch, as ValidateForComposition
// does, and as docs/API.md states.
func TestValidate_UnknownTypeFailsOnAnEmptyBatch(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\ntype T {\n\tid String primary\n}\n")
	v := instance.NewValidator(s)
	for name, raws := range map[string][]instance.RawInstance{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			valids, res := v.Validate(t.Context(), "Nope", raws)
			if valids != nil || !res.HasCode(instance.ErrTypeNotFound) {
				t.Errorf("valids=%v codes=%v; want nil and E_INSTANCE_TYPE_NOT_FOUND", valids, codes(res))
			}
		})
	}
	if valids, res := v.Validate(t.Context(), "T", nil); valids != nil || res.Err() != nil {
		t.Errorf("control: a known type and a nil batch is nil, OK; got %v, %v", valids, res.Err())
	}
}

// A cancelled composition batch returns no partial slice either: the
// one-entry-per-input contract is ValidateForComposition's as it is Validate's.
func TestValidateForComposition_CancellationReturnsNoPartialSlice(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\npart type Line {\n\tid String primary\n}\n\ntype T {\n\tid String primary\n\t*-> LINES (many) Line\n}\n")
	v := instance.NewValidator(s)
	raws := []instance.RawInstance{{Properties: map[string]any{"id": "a"}}, {Properties: map[string]any{"id": "b"}}}
	ctx, cancel := context.WithCancel(t.Context())
	if valids, res := v.ValidateForComposition(ctx, "T", "LINES", raws); res.Err() != nil || len(valids) != 2 {
		t.Fatalf("control: %d valids, %v", len(valids), res.Err())
	}
	cancel()
	if valids, res := v.ValidateForComposition(ctx, "T", "LINES", raws); valids != nil || !res.HasCode(diag.E_CONTEXT_CANCELLED) {
		t.Errorf("a cancelled composition batch returned %d valids with %v", len(valids), codes(res))
	}
}

// A batch-wide resolution error names the source and no row: the type name,
// not raws[0], is at fault.
func TestValidate_BatchWideErrorBlamesNoRow(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\ntype T {\n\tid String primary\n}\n")
	v := instance.NewValidator(s)
	prov := location.NewProvenance("rows.json", path.Root().Index(7), location.Span{})
	_, res := v.Validate(t.Context(), "NoSuchType", []instance.RawInstance{{Properties: map[string]any{"id": "x"}, Provenance: prov}})
	if !res.HasCode(instance.ErrTypeNotFound) {
		t.Fatalf("want E_TYPE_NOT_FOUND, got %v", codes(res))
	}
	for is := range res.Issues() {
		if is.SourceName() != "rows.json" {
			t.Errorf("the source name was dropped: %q", is.SourceName())
		}
		if strings.Contains(is.Path(), "[7]") {
			t.Errorf("a batch-wide error was blamed on row 7: path %q", is.Path())
		}
	}
}

// NewValidInstance does not alias the caller's maps: the godoc promises
// immutability, so a caller that reuses its map cannot change the instance.
func TestNewValidInstance_DoesNotAliasCallerMaps(t *testing.T) {
	t.Parallel()
	edges := map[string]*instance.ValidEdgeData{"AT": instance.NewValidEdgeData(nil)}
	composed := map[string]immutable.Value{}
	vi := instance.NewValidInstance("T", schema.TypeID{}, immutable.Key{}, immutable.Properties{}, edges, composed, nil)
	delete(edges, "AT")
	if _, ok := vi.Edge("AT"); !ok {
		t.Error("deleting from the caller's map removed the instance's edge")
	}
}

// A nil *ValidInstance — the documented shape of a failed entry in Validate's
// slice — answers its accessors as ValidEdgeData does, instead of panicking.
func TestValidInstance_NilReceiverIsSafe(t *testing.T) {
	t.Parallel()
	var vi *instance.ValidInstance
	if vi.TypeName() != "" || vi.Validated() || vi.Provenance() != nil {
		t.Error("a nil instance must answer zero values")
	}
	if _, ok := vi.Property("x"); ok {
		t.Error("a nil instance has no properties")
	}
	if _, ok := vi.Edge("x"); ok {
		t.Error("a nil instance has no edges")
	}
	n := 0
	for range vi.Edges() {
		n++
	}
	for range vi.Compositions() {
		n++
	}
	if n != 0 {
		t.Error("a nil instance iterates nothing")
	}
}

// A grandchild's diagnostic carries the relation that reached it once — the
// innermost — not one entry per nesting level under the same keys.
func TestComposition_RelationDetailIsInnermostOnly(t *testing.T) {
	t.Parallel()
	s := loadT(t, `schema "p"

part type Room {
	id String primary
}

part type Addr {
	id String primary
	*-> ROOMS (one:many) Room
}

type P {
	id String primary
	*-> ADDRS (one:many) Addr
}
`)
	v := instance.NewValidator(s)
	_, res := v.ValidateOne(t.Context(), "P", instance.RawInstance{Properties: map[string]any{
		"id": "p", "addrs": []any{map[string]any{"id": "a", "rooms": []any{map[string]any{"id": "r", "bogus": 1}}}},
	}})
	if !res.HasCode(diag.E_UNKNOWN_FIELD) {
		t.Fatalf("want E_UNKNOWN_FIELD on the grandchild, got %v", codes(res))
	}
	for is := range res.Issues() {
		if is.Code() != diag.E_UNKNOWN_FIELD {
			continue
		}
		rel := []string{}
		for _, d := range is.Details() {
			if d.Key == diag.DetailKeyRelationName {
				rel = append(rel, d.Value)
			}
		}
		if len(rel) != 1 || rel[0] != "ROOMS" {
			t.Errorf("want one relation detail naming ROOMS, got %v", rel)
		}
	}
}

// An inherited invariant is evaluated on a subtype's instance, as SPEC's
// invariant merging states; a subtype instance that violates its parent's
// rule fails.
func TestInheritedInvariant_IsEvaluated(t *testing.T) {
	t.Parallel()
	s := loadT(t, `schema "p"

abstract type Base {
	id String primary
	n Integer
	! "n_positive" n > 0
}

type Sub extends Base {
	x Integer
}
`)
	v := instance.NewValidator(s)
	_, res := v.ValidateOne(t.Context(), "Sub", instance.RawInstance{Properties: map[string]any{"id": "s", "n": int64(-1)}})
	if !res.HasCode(diag.E_INVARIANT_FAIL) {
		t.Errorf("the inherited invariant did not run: %v", codes(res))
	}
}

// The validator's logger reaches the evaluator: an invariant evaluation
// emits the evaluator's trace op under it.
func TestWithLogger_ReachesTheEvaluator(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\ntype T {\n\tid String primary\n\tn Integer\n\t! \"pos\" n > 0\n}\n")
	h := yammmtest.NewRecordHandler(slog.LevelDebug)
	v := instance.NewValidator(s, instance.WithLogger(slog.New(h)))
	if _, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "n": int64(1)}}); res.Err() != nil {
		t.Fatal(res.Err())
	}
	if !yammmtest.HasAttr(h.Records(), "op", "yammm.eval.expr") {
		t.Error("the evaluator's trace op was never logged: the validator's logger does not reach it")
	}
}

// A json.Number — what adapter/json's UseNumber decode emits for every
// number — is a number at a String property, not a string of digits.
func TestValidateOne_JSONNumberIsNotAString(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\ntype T {\n\tid String primary\n\ts String\n\tn Integer\n}\n")
	v := instance.NewValidator(s)
	_, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "s": json.Number("4200")}})
	if !res.HasCode(diag.E_TYPE_MISMATCH) {
		t.Errorf("json.Number at a String property: want E_TYPE_MISMATCH, got %v", codes(res))
	}
	vi, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "n": json.Number("42")}})
	if res.Err() != nil {
		t.Fatalf("control: json.Number at an Integer property refused: %v", res.Err())
	}
	if n, _ := vi.Property("n"); n.Unwrap() != int64(42) {
		t.Errorf("stored %#v, want int64(42)", n.Unwrap())
	}
}
