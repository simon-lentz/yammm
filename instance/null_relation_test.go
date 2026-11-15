package instance_test

import (
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
)

const nullRelationSchema = `schema "n"

part type Part {
	id String primary
}

type Co {
	id String primary
}

type T {
	id String primary
	--> OPT_ONE (_:one) Co
	--> REQ_ONE (one) Co
	--> OPT_MANY (many) Co
	--> REQ_MANY (one:many) Co
	*-> C_OPT_ONE (_:one) Part
	*-> C_REQ_ONE (one) Part
	*-> C_OPT_MANY (many) Part
	*-> C_REQ_MANY (one:many) Part
}
`

// A relation whose key holds no value draws what the same instance draws with
// the key left out, at every relation kind, optionality and multiplicity: the
// issues, their order and paths, and the instance's edges and compositions.
func TestValidateOne_ANullRelationIsTheRelationLeftOut(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadT(t, nullRelationSchema))
	fields := map[string]bool{
		"opt_one": false, "req_one": false, "opt_many": true, "req_many": true,
		"c_opt_one": true, "c_req_one": true, "c_opt_many": true, "c_req_many": true,
	}
	base := func() map[string]any {
		return map[string]any{
			"id":         "t",
			"req_one":    map[string]any{"_target_id": "c"},
			"req_many":   []any{map[string]any{"_target_id": "c"}},
			"c_req_one":  []any{map[string]any{"id": "p1"}},
			"c_req_many": []any{map[string]any{"id": "p2"}},
		}
	}
	for _, field := range slices.Sorted(maps.Keys(fields)) {
		absent := []any{nil}
		if fields[field] {
			absent = append(absent, []any(nil), []map[string]any(nil), immutable.Slice{})
		} else {
			absent = append(absent, map[string]any(nil), immutable.Properties{})
		}
		left := base()
		delete(left, field)
		wantVI, want := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: left})
		for _, null := range absent {
			props := base()
			props[field] = null
			gotVI, got := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: props})
			if g, w := issueLines(got), issueLines(want); !slices.Equal(g, w) {
				t.Errorf("%s = %#v drew\n%q\nwant what the key left out draws\n%q", field, null, g, w)
			}
			if gotVI.IsZero() != wantVI.IsZero() {
				t.Errorf("%s = %#v: instance zero %v, want %v", field, null, gotVI.IsZero(), wantVI.IsZero())
				continue
			}
			if gotVI.IsZero() {
				continue
			}
			if g, w := slices.Sorted(keysOf(gotVI.Edges())), slices.Sorted(keysOf(wantVI.Edges())); !slices.Equal(g, w) {
				t.Errorf("%s = %#v: edges %v, want %v", field, null, g, w)
			}
			if g, w := composedRelations(gotVI), composedRelations(wantVI); !slices.Equal(g, w) {
				t.Errorf("%s = %#v: compositions %v, want %v", field, null, g, w)
			}
		}
	}
}

// A null optional relation is dropped and a null required composition draws the
// required error, each as the relation left out does, with no shape error.
func TestValidateOne_ANullRelationDrawsNoShapeError(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadT(t, nullRelationSchema))
	vi, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{
		"id": "t", "opt_one": nil, "req_one": nil, "opt_many": nil, "req_many": nil,
		"c_opt_one": nil, "c_opt_many": nil, "c_req_one": nil, "c_req_many": nil,
	}})
	want := []string{
		instance.ErrUnresolvedRequiredComposition.String(),
		instance.ErrUnresolvedRequiredComposition.String(),
	}
	if got := codes(res); !slices.Equal(got, want) {
		t.Fatalf("codes %v, want %v: %s", got, want, res)
	}
	if !vi.IsZero() {
		t.Errorf("an instance missing a required composition was accepted")
	}
	vi, res = v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{
		"id": "t", "opt_one": nil, "req_one": nil, "opt_many": nil, "req_many": nil,
		"c_opt_one": nil, "c_opt_many": nil,
		"c_req_one": []any{map[string]any{"id": "p1"}}, "c_req_many": []any{map[string]any{"id": "p2"}},
	}})
	if res.Err() != nil || vi.IsZero() {
		t.Fatalf("null associations and null optional compositions were refused: %s", res)
	}
	if edges := slices.Collect(keysOf(vi.Edges())); len(edges) != 0 {
		t.Errorf("a null association was stored: %v", edges)
	}
	if comps := composedRelations(vi); !slices.Equal(comps, []string{"C_REQ_MANY", "C_REQ_ONE"}) {
		t.Errorf("compositions %v, want the two required ones alone", comps)
	}
}

// composedRelations returns the sorted names of the relations vi composes at
// least one child under.
func composedRelations(vi instance.ValidInstance) []string {
	var names []string
	for rel := range vi.Type().AllCompositions() {
		for range vi.Composed(rel.Name()) {
			names = append(names, rel.Name())
			break
		}
	}
	slices.Sort(names)
	return names
}

func keysOf[V any](seq func(func(string, V) bool)) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range seq {
			if !yield(k) {
				return
			}
		}
	}
}

// issueLines renders each issue of res, in order, as its severity, code, path,
// message and details, the parts a caller reads.
func issueLines(res diag.Result) []string {
	var out []string
	for is := range res.Issues() {
		out = append(out, fmt.Sprintf("%v %s %s %s %v", is.Severity(), is.Code(), is.Path(), is.Message(), is.Details()))
	}
	return out
}
