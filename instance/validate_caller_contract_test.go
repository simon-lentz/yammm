package instance_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

func loadT(t *testing.T, src string) schema.Schema {
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

const nullRuleSchema = `schema "p"

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
`

// A nil slice or a nil map is the absent value at every null-rule site, as an
// interface nil is.
func TestValidateOne_ANilSliceOrMapIsAbsentAtEveryNullRuleSite(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadT(t, nullRuleSchema))
	t.Run("optional nil slice is dropped", func(t *testing.T) {
		t.Parallel()
		for _, nilTags := range []any{[]any(nil), []string(nil)} {
			vi, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": "m", "tags": nilTags}})
			if res.Err() != nil {
				t.Fatalf("%T: refused: %v", nilTags, res.Err())
			}
			if _, ok := vi.Property("tags"); ok {
				t.Errorf("%T: a nil slice was stored", nilTags)
			}
		}
	})
	t.Run("edge value nil map and nil slice are the null an explicit null is", func(t *testing.T) {
		t.Parallel()
		_, oneControl := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": "m", "at": nil}})
		_, one := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": "m", "at": map[string]any(nil)}})
		_, manyControl := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": "m", "lines": nil}})
		_, many := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": "m", "lines": []any(nil)}})
		for name, pair := range map[string][2]diag.Result{"a nil map at a (one) association": {oneControl, one}, "a nil slice at a (many) composition": {manyControl, many}} {
			if !slices.Equal(codes(pair[0]), codes(pair[1])) || !pair[1].HasCode(diag.E_EDGE_SHAPE_MISMATCH) {
				t.Errorf("%s drew %v, an explicit null %v; want the same E_EDGE_SHAPE_MISMATCH", name, codes(pair[1]), codes(pair[0]))
			}
		}
	})
	t.Run("FK component nil slice is a null component", func(t *testing.T) {
		t.Parallel()
		_, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": "m", "at": map[string]any{"_target_id": []string(nil)}}})
		if !strings.Contains(res.String(), `FK field "_target_id": expected string, got null`) {
			t.Errorf("a nil slice at an FK component was not reported as null: %s", res)
		}
	})
}

// A nil pointer is a value of no form at every null-rule site, where an
// interface nil is the absent value: each site refuses it by its Go type and
// none reads it as null or as missing. The edge-property and composition sites
// are [TestValidateOne_APointerIsNoValueAtAnEdgePropertyOrAComposition]'s.
func TestValidateOne_ANilPointerIsNoAbsentValueAtAnyNullRuleSite(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadT(t, nullRuleSchema))
	var nilStr *string
	t.Run("optional property", func(t *testing.T) {
		t.Parallel()
		vi, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": "m", "nick": nilStr}})
		if !slices.Equal(codes(res), []string{diag.E_TYPE_MISMATCH.String()}) || !vi.IsZero() {
			t.Errorf("want E_TYPE_MISMATCH alone and no instance, got %v", codes(res))
		}
		if !strings.Contains(res.String(), "expected string, got *string") {
			t.Errorf("the refusal does not name the pointer: %s", res)
		}
	})
	t.Run("required property", func(t *testing.T) {
		t.Parallel()
		_, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": nilStr}})
		if !slices.Equal(codes(res), []string{diag.E_TYPE_MISMATCH.String()}) {
			t.Errorf("want E_TYPE_MISMATCH alone, got %v", codes(res))
		}
	})
	t.Run("edge value", func(t *testing.T) {
		t.Parallel()
		var nilEdge *map[string]any
		_, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": "m", "at": nilEdge}})
		if !res.HasCode(diag.E_EDGE_SHAPE_MISMATCH) || !strings.Contains(res.String(), "got *map[string]interface {}") || strings.Contains(res.String(), "got null") {
			t.Errorf("want E_EDGE_SHAPE_MISMATCH naming the pointer, got %s", res)
		}
	})
	t.Run("FK component", func(t *testing.T) {
		t.Parallel()
		_, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": "m", "at": map[string]any{"_target_id": nilStr}}})
		if res.Err() == nil || strings.Contains(res.String(), "got null") || !strings.Contains(res.String(), "*string") {
			t.Errorf("want a refusal naming the pointer, got %s", res)
		}
	})
}

// A pointer is a value of no form at an edge property, a composition's value
// and a composed child too, nil or not: each site refuses it by its Go type, and
// none reads a nil one as null or as missing.
func TestValidateOne_APointerIsNoValueAtAnEdgePropertyOrAComposition(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadT(t, `schema "p"

part type Line {
	id String primary
}

type Co {
	id String primary
}

type T {
	id String primary
	--> AT (one) Co {
		note String
	}
	*-> LINES (many) Line
}
`))
	text := "x"
	var (
		nilText  *string
		nilLines *[]any
		nilLine  *map[string]any
	)
	lines := []any{map[string]any{"id": "l"}}
	line := map[string]any{"id": "l"}
	for name, c := range map[string]struct {
		props map[string]any
		code  diag.Code
		want  string
	}{
		"a nil pointer at an edge property":   {map[string]any{"at": map[string]any{"_target_id": "c", "note": nilText}}, diag.E_TYPE_MISMATCH, `edge property "note": expected string, got *string`},
		"a pointer at an edge property":       {map[string]any{"at": map[string]any{"_target_id": "c", "note": &text}}, diag.E_TYPE_MISMATCH, `edge property "note": expected string, got *string`},
		"a nil pointer at a composition":      {map[string]any{"lines": nilLines}, diag.E_EDGE_SHAPE_MISMATCH, `expected array, got *[]interface {}`},
		"a pointer at a composition":          {map[string]any{"lines": &lines}, diag.E_EDGE_SHAPE_MISMATCH, `expected array, got *[]interface {}`},
		"a nil pointer at a composed child":   {map[string]any{"lines": []any{nilLine}}, diag.E_EDGE_SHAPE_MISMATCH, `composition child must be an object, got *map[string]interface {}`},
		"a pointer at a composed child":       {map[string]any{"lines": []any{&line}}, diag.E_EDGE_SHAPE_MISMATCH, `composition child must be an object, got *map[string]interface {}`},
		"a pointer at a one-edge's value":     {map[string]any{"at": &line}, diag.E_EDGE_SHAPE_MISMATCH, `expected object, got *map[string]interface {}`},
		"a nil pointer at a one-edge's value": {map[string]any{"at": nilLine}, diag.E_EDGE_SHAPE_MISMATCH, `expected object, got *map[string]interface {}`},
	} {
		c.props["id"] = "x"
		vi, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: c.props})
		if !slices.Equal(codes(res), []string{c.code.String()}) || !vi.IsZero() {
			t.Errorf("%s: want %s alone and no instance, got %v", name, c.code, codes(res))
		}
		if !strings.Contains(res.String(), c.want) || strings.Contains(res.String(), "null") {
			t.Errorf("%s: %s; want %q and no null", name, res, c.want)
		}
	}
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
// the name before it answers a nil or empty batch, as docs/API.md states.
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

// An abstract or a part type's name cannot hold a root, and an empty batch is
// refused under it as a row would be, with no instance index: the batch names
// no row. Without it a writer's empty group under such a name read back OK.
func TestValidate_AnAbstractOrPartNameFailsOnAnEmptyBatch(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\nabstract type A {\n\tid String primary\n}\n\npart type P {\n\tid String primary\n}\n\ntype T {\n\tid String primary\n\t*-> PS (many) P\n}\n")
	v := instance.NewValidator(s)
	for _, c := range []struct {
		name string
		code diag.Code
	}{{"A", diag.E_ABSTRACT_TYPE}, {"P", diag.E_PART_TYPE_DIRECT}} {
		for batch, raws := range map[string][]instance.RawInstance{"nil": nil, "empty": {}} {
			valids, res := v.Validate(t.Context(), c.name, raws)
			if valids != nil || !res.HasCode(c.code) {
				t.Errorf("%s, %s batch: valids=%v codes=%v; want nil and %s", c.name, batch, valids, codes(res), c.code)
			}
			for issue := range res.Issues() {
				for _, d := range issue.Details() {
					if d.Key == diag.DetailKeyInstanceIndex {
						t.Errorf("%s, %s batch: the refusal names instance %s, but the batch names none", c.name, batch, d.Value)
					}
				}
			}
		}
	}
	if valids, res := v.Validate(t.Context(), "T", []instance.RawInstance{}); valids == nil || res.Err() != nil {
		t.Errorf("control: a root type and an empty batch is empty, OK; got %v, %v", valids, res.Err())
	}
}

// A row's refusal under an abstract name carries the row's own span.
func TestValidate_AnAbstractRowRefusalCarriesItsSpan(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"a\"\n\nabstract type A {\n\tid String primary\n}\n")
	span := location.PointWithByte(location.NewSourceID("rows.json"), 7, 3, -1)
	raw := instance.RawInstance{Properties: map[string]any{"id": "x"}, Provenance: location.NewProvenance("rows.json", path.Root(), span)}
	_, res := instance.NewValidator(s).Validate(t.Context(), "A", []instance.RawInstance{raw})
	if !res.HasCode(diag.E_ABSTRACT_TYPE) {
		t.Fatalf("Validate = %s, want E_ABSTRACT_TYPE", res)
	}
	for issue := range res.Issues() {
		if issue.Span() != span {
			t.Errorf("the refusal's span is %v, want the row's %v", issue.Span(), span)
		}
	}
}

// A batch-wide resolution error names the source and no row: the type name,
// not raws[0], is at fault.
func TestValidate_BatchWideErrorBlamesNoRow(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\ntype T {\n\tid String primary\n}\n")
	v := instance.NewValidator(s)
	prov := location.NewProvenance("rows.json", path.Root().Index(7), location.Span{})
	valids, res := v.Validate(t.Context(), "NoSuchType", []instance.RawInstance{{Properties: map[string]any{"id": "x"}, Provenance: prov}})
	if !res.HasCode(instance.ErrTypeNotFound) {
		t.Fatalf("want E_TYPE_NOT_FOUND, got %v", codes(res))
	}
	if valids != nil {
		t.Errorf("a batch that did not run returned %v, want a nil slice", valids)
	}
	for is := range res.Issues() {
		for _, d := range is.Details() {
			if d.Key == diag.DetailKeyInstanceIndex {
				t.Errorf("a batch-wide error carries instance_index %q", d.Value)
			}
		}
		if is.SourceName() != "rows.json" {
			t.Errorf("the source name was dropped: %q", is.SourceName())
		}
		if strings.Contains(is.Path(), "[7]") {
			t.Errorf("a batch-wide error was blamed on row 7: path %q", is.Path())
		}
	}
}

// A nil ValidInstance — the documented shape of a failed entry in Validate's
// slice — answers its accessors as ValidEdgeData does, instead of panicking.
func TestValidInstance_ZeroHandleIsSafe(t *testing.T) {
	t.Parallel()
	var vi instance.ValidInstance
	if vi.TypeName() != "" || !vi.Provenance().IsZero() {
		t.Error("a nil instance must answer zero values")
	}
	if vi.Type() != (schema.Type{}) || vi.PrimaryKey().Len() != 0 || vi.Properties().Len() != 0 {
		t.Error("a nil instance must answer zero identity, key and properties")
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

// A json.Number — what an encoding/json decode with UseNumber emits for every
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
