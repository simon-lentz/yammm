package instance_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

const nilContainerSchema = `schema "p"

type T {
	id String primary
	reqTags List<String> required
	optTags List<String>
	reqVec Vector[2] required
	optVec Vector[2]
	stamps List<Timestamp>
	nested List<List<String>>
	name String
}
`

// A nil slice at a required List or Vector is a missing property, whatever
// its element type, and draws E_MISSING_REQUIRED alone.
func TestValidate_ANilSliceAtARequiredListIsMissing(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadT(t, nilContainerSchema))
	for name, props := range map[string]map[string]any{
		"a nil []any list":       {"id": "x", "reqTags": []any(nil), "reqVec": []float64{1, 2}},
		"a nil []string list":    {"id": "x", "reqTags": []string(nil), "reqVec": []float64{1, 2}},
		"a nil []float64 vector": {"id": "x", "reqTags": []any{"a"}, "reqVec": []float64(nil)},
	} {
		_, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: props})
		if got := codes(res); !slices.Equal(got, []string{diag.E_MISSING_REQUIRED.String()}) {
			t.Errorf("%s: got %v, want E_MISSING_REQUIRED alone", name, got)
		}
	}
}

// The validator reads a nil slice or map as encoding/json writes it, null: an
// instance and its JSON form draw the same issues, word for word, and store the
// same properties, so a value no validator judged round-trips to the same verdict.
func TestValidate_ANilContainerAgreesWithItsJSONForm(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadT(t, nilContainerSchema))
	base := func(extra map[string]any) map[string]any {
		props := map[string]any{"id": "x", "reqTags": []any{"a"}, "reqVec": []float64{1, 2}}
		maps.Copy(props, extra)
		return props
	}
	for name, props := range map[string]map[string]any{
		"a required nil list":                      base(map[string]any{"reqTags": []any(nil)}),
		"an optional nil list":                     base(map[string]any{"optTags": []string(nil)}),
		"an optional nil vector":                   base(map[string]any{"optVec": []float64(nil)}),
		"an optional nil timestamp list":           base(map[string]any{"stamps": []any(nil)}),
		"an empty list":                            base(map[string]any{"optTags": []any{}}),
		"a nil map at a list":                      base(map[string]any{"optTags": map[string]any(nil)}),
		"a nil inner list":                         base(map[string]any{"nested": []any{[]any{"a"}, []any(nil)}}),
		"a nil slice at a string":                  base(map[string]any{"name": []string(nil)}),
		"a nil required list and a nil inner list": base(map[string]any{"reqTags": []any(nil), "nested": []any{[]any(nil)}}),
	} {
		encoded, err := json.Marshal(props)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		dec := json.NewDecoder(bytes.NewReader(encoded))
		dec.UseNumber()
		var decoded map[string]any
		if err := dec.Decode(&decoded); err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		goValid, goRes := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: props})
		jsonValid, jsonRes := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: decoded})
		if goRes.String() != jsonRes.String() {
			t.Errorf("%s: the Go value drew\n%s\nits JSON form %s drew\n%s", name, goRes, encoded, jsonRes)
			continue
		}
		if goValid == nil || jsonValid == nil {
			continue
		}
		for _, prop := range []string{"optTags", "optVec", "stamps", "nested", "name"} {
			_, goStored := goValid.Property(prop)
			_, jsonStored := jsonValid.Property(prop)
			if goStored != jsonStored {
				t.Errorf("%s: %s stored = %v from the Go value, %v from its JSON form %s", name, prop, goStored, jsonStored, encoded)
			}
		}
	}
}

// CheckValue accepts a nil slice under a List as a value that holds none, and
// CanonicalValue renders it as nil, never as an empty list.
func TestCanonicalValue_ANilSliceAtAListIsNoEmptyList(t *testing.T) {
	t.Parallel()
	stamps := schema.NewListConstraint(schema.NewTimestampConstraint())
	tags := schema.NewListConstraint(schema.NewStringConstraint())
	for _, c := range []schema.Constraint{stamps, tags} {
		for _, nilSlice := range []any{[]any(nil), []string(nil)} {
			if err := instance.CheckValue(nilSlice, c); err != nil {
				t.Errorf("CheckValue(%#v, %s) = %v, want nil", nilSlice, c, err)
			}
			out, err := instance.CanonicalValue(nilSlice, c)
			if err != nil || out != nil {
				t.Errorf("CanonicalValue(%#v, %s) = %#v, %v; want nil", nilSlice, c, out, err)
			}
		}
	}
}

const nilRelationSchema = `schema "p"

type Co {
	id String primary
}

part type Line {
	id String primary
}

type T {
	id String primary
	--> ONE (_) Co
	--> MANY (many) Co
	--> PROPS (_) Co {
		role String required
		note String
	}
	*-> LINES (many) Line
	*-> HEAD (one) Line
}
`

// A nil slice or map at a relation's value, at an element of a (many)
// association or a composition, at an edge property and at a foreign-key field
// is the null its JSON form holds there: both draw the same issues, word for
// word.
func TestValidate_ANilContainerAtARelationAgreesWithItsJSONForm(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadT(t, nilRelationSchema))
	line := map[string]any{"id": "l"}
	for name, props := range map[string]map[string]any{
		"a nil map at a (one) association":        {"one": map[string]any(nil)},
		"a nil slice at a (one) association":      {"one": []any(nil)},
		"a nil slice at a (many) association":     {"many": []any(nil)},
		"a nil map at a (many) association":       {"many": map[string]any(nil)},
		"a nil map in a (many) association":       {"many": []any{map[string]any(nil)}},
		"a nil slice in a (many) association":     {"many": []any{[]any(nil)}},
		"a nil slice at a foreign-key field":      {"one": map[string]any{"_target_id": []any(nil)}},
		"a nil slice at a required edge property": {"props": map[string]any{"_target_id": "c", "role": []any(nil)}},
		"a nil map at an optional edge property":  {"props": map[string]any{"_target_id": "c", "role": "r", "note": map[string]any(nil)}},
		"a nil slice at a (many) composition":     {"lines": []any(nil)},
		"a nil map in a (many) composition":       {"lines": []any{line, map[string]any(nil)}},
		"a nil slice in a (many) composition":     {"lines": []any{[]any(nil)}},
		"a nil slice at a (one) composition":      {"head": []any(nil)},
	} {
		props["id"] = "x"
		if _, ok := props["head"]; !ok {
			props["head"] = []any{line}
		}
		encoded, err := json.Marshal(props)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("%s: unmarshal: %v", name, err)
		}
		_, goRes := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: props})
		_, jsonRes := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: decoded})
		if goRes.OK() != jsonRes.OK() || goRes.String() != jsonRes.String() {
			t.Errorf("%s: the Go value drew\n%s\nits JSON form %s drew\n%s", name, goRes, encoded, jsonRes)
		}
	}
}

// A nil immutable container holds no value, as immutable.Value.IsNil reads it:
// at a property it is the absent case, and ScalarOf reads it as nil.
func TestValidate_ANilImmutableContainerIsNull(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadT(t, nilContainerSchema))
	base := map[string]any{"id": "x", "reqTags": []any{"a"}, "reqVec": []float64{1, 2}}
	for name, none := range map[string]any{
		"a nil Properties":    immutable.WrapProperties(nil),
		"a nil Key":           immutable.WrapKey(nil),
		"a nil int-keyed Map": immutable.WrapMap(map[int]any(nil)),
	} {
		if held, ok := instance.ScalarOf(none); !ok || held != nil {
			t.Errorf("ScalarOf(%s) = %v, %v; want nil, true", name, held, ok)
		}
		for _, prop := range []string{"optTags", "name"} {
			props := maps.Clone(base)
			props[prop] = none
			nulled := maps.Clone(base)
			nulled[prop] = nil
			_, got := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: props})
			_, want := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: nulled})
			if got.String() != want.String() {
				t.Errorf("%s at %s drew\n%s\nwhere null drew\n%s", name, prop, got, want)
			}
		}
	}
}

// A struct that embeds a nil immutable container is a struct, not the container:
// it holds a value, and draws the issues any other struct draws there.
func TestValidate_AStructEmbeddingANilContainerIsAStruct(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadT(t, nilContainerSchema))
	base := map[string]any{"id": "x", "reqTags": []any{"a"}, "reqVec": []float64{1, 2}}
	for name, embedding := range map[string]any{
		"a nil Slice":      struct{ immutable.Slice }{},
		"a nil Properties": struct{ immutable.Properties }{},
	} {
		if held, ok := instance.ScalarOf(embedding); ok {
			t.Errorf("ScalarOf(a struct embedding %s) = %v, true; want no scalar", name, held)
		}
		for _, prop := range []string{"optTags", "name"} {
			props := maps.Clone(base)
			props[prop] = embedding
			plain := maps.Clone(base)
			plain[prop] = struct{ A int }{}
			_, got := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: props})
			_, want := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: plain})
			if !slices.Equal(codes(got), codes(want)) || !got.HasErrors() {
				t.Errorf("a struct embedding %s at %s drew %v; want %v, as any struct", name, prop, codes(got), codes(want))
			}
		}
	}
}

// An immutable map or slice at a relation reads as the Go map or slice it
// wraps, each entry as the container holds it, as at a property: the two draw
// the same issues and store the same targets and children, and a field no
// reader reads is never walked.
func TestValidate_AnImmutableContainerAtARelationReadsAsWhatItHolds(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadT(t, nilRelationSchema))
	co := map[string]any{"_target_id": "c"}
	line := map[string]any{"id": "l"}
	for name, plain := range map[string]map[string]any{
		"a (one) association":        {"one": co},
		"a (many) association":       {"many": []any{co, map[string]any{"_target_id": "d"}}},
		"an association with props":  {"props": map[string]any{"_target_id": "c", "role": "r"}},
		"a (many) composition":       {"lines": []any{line}},
		"a refused target":           {"one": map[string]any{"_target_id": 5}},
		"a list at a (one) relation": {"one": []any{co}},
		"a map at a (many) relation": {"many": co},
		"a Key at an edge property":  {"props": map[string]any{"_target_id": "c", "role": "r", "note": immutable.WrapKey([]any{"a"})}},
	} {
		plain["id"] = "x"
		plain["head"] = []any{line}
		wrapped := immutable.WrapProperties(plain, immutable.WithClone(true))
		held := make(map[string]any, len(plain))
		for k, val := range wrapped.Range() {
			held[k] = val.Unwrap()
		}
		plainValid, plainRes := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: plain})
		heldValid, heldRes := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: held})
		if plainRes.String() != heldRes.String() {
			t.Errorf("%s: the Go value drew\n%s\nits immutable form drew\n%s", name, plainRes, heldRes)
			continue
		}
		if plainValid == nil || heldValid == nil {
			continue
		}
		for rel, edge := range plainValid.Edges() {
			heldEdge, ok := heldValid.Edge(rel)
			if !ok || len(heldEdge.Targets()) != len(edge.Targets()) {
				t.Errorf("%s: edge %s holds %d targets from the Go value, %v from its immutable form", name, rel, len(edge.Targets()), heldEdge)
			}
		}
		if got, want := maps.Collect(heldValid.Compositions()), maps.Collect(plainValid.Compositions()); len(got) != len(want) {
			t.Errorf("%s: %d compositions from the Go value, %d from its immutable form", name, len(want), len(got))
		}
	}

	props := func(one any) map[string]any {
		return map[string]any{"id": "x", "head": []any{line}, "one": one}
	}
	valid, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: props(immutable.WrapProperties(co))})
	if edge, ok := valid.Edge("ONE"); !res.OK() || !ok || len(edge.Targets()) != 1 {
		t.Errorf("a Properties at a (one) relation drew\n%s\nwant the one target", res)
	}
	for value, want := range map[any]string{time.Time{}: "time.Time", &co: "*map[string]interface {}"} {
		_, res = v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: props(value)})
		if !strings.Contains(res.String(), "expected object, got "+want) {
			t.Errorf("%s at a (one) relation drew\n%s\nwant its type named", want, res)
		}
	}
	cycle := map[int]any{}
	cycle[0] = cycle
	unread := map[string]any{"_target_id": "c", "junk": cycle}
	_, plainRes := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: props(unread)})
	_, heldRes := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: props(immutable.WrapMap(unread))})
	if plainRes.String() != heldRes.String() {
		t.Errorf("a field no reader reads drew\n%s\nfrom the Go value and\n%s\nfrom its immutable form", plainRes, heldRes)
	}
	_, res = v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: props(map[int]any{1: "c"})})
	if !strings.Contains(res.String(), "expected object, got map[int]interface {}") {
		t.Errorf("a map keyed by no string at a (one) relation drew\n%s\nwant its type named", res)
	}
}
