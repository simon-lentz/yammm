package instance_test

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

func loadSrc(t *testing.T, src string) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), src, "p.yammm")
	if !res.OK() {
		t.Fatalf("load: %s", res)
	}
	return s
}

func issueWithCode(res diag.Result, code diag.Code) (diag.Issue, bool) {
	for is := range res.Issues() {
		if is.Code() == code {
			return is, true
		}
	}
	return diag.Issue{}, false
}

func mustIssue(t *testing.T, res diag.Result, code diag.Code) diag.Issue {
	t.Helper()
	is, ok := issueWithCode(res, code)
	if !ok {
		t.Fatalf("want %s, got %s", code, res)
	}
	return is
}

func detailValues(is diag.Issue, key string) []string {
	var out []string
	for _, d := range is.Details() {
		if d.Key == key {
			out = append(out, d.Value)
		}
	}
	return out
}

func kids(val any) []*instance.ValidInstance {
	var out []*instance.ValidInstance
	switch c := val.(type) {
	case *instance.ValidInstance:
		out = append(out, c)
	case []*instance.ValidInstance:
		out = append(out, c...)
	case immutable.Slice:
		for e := range c.Iter() {
			out = append(out, kids(e.Unwrap())...)
		}
	}
	return out
}

func composedChildren(inst *instance.ValidInstance) []*instance.ValidInstance {
	var out []*instance.ValidInstance
	for _, val := range inst.Compositions() {
		out = append(out, kids(val.Unwrap())...)
	}
	return out
}

// A diagnostic's path names the token in the INPUT document; the schema
// spelling is a detail, not the address.
func TestDiagnosticPath_NamesTheInputKey(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, `schema "p"

type Person {
    id String primary
    age Integer
}
`))
	_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{"id": "p", "Age": "x"}})
	if got := mustIssue(t, res, instance.ErrTypeMismatch).Path(); got != "$.Age" {
		t.Errorf("path = %q, want $.Age", got)
	}
}

const docSrc = `schema "p"

part type Line {
    n String primary
    qty Integer
}

type Doc {
    id String primary
    *-> LINES (one:many) Line
}
`

// A composed child's diagnostic path is anchored on the input key the caller
// wrote, and the relation's schema spelling rides in the details.
func TestComposedChildDiagnosticPath_NamesTheInputKey(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, docSrc))
	_, res := v.ValidateOne(t.Context(), "Doc", instance.RawInstance{Properties: map[string]any{
		"id": "d", "Lines": []any{map[string]any{"n": "a", "qty": "x"}},
	}})
	if got := mustIssue(t, res, instance.ErrTypeMismatch).Path(); got != "$.Lines[0].qty" {
		t.Errorf("path = %q, want $.Lines[0].qty", got)
	}
}

// A composed child's stored provenance is the parent's, extended by the input
// key and position; a parent with no provenance yields children with none.
func TestComposedChildProvenance_FollowsTheParent(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, docSrc))
	props := func() map[string]any {
		return map[string]any{"id": "d", "lines": []any{map[string]any{"n": "a", "qty": 1}}}
	}

	prov := location.NewProvenance("data.json", path.Root().Key("Doc").Index(0), location.Span{})
	inst, res := v.ValidateOne(t.Context(), "Doc", instance.RawInstance{Properties: props(), Provenance: prov})
	if !res.OK() {
		t.Fatal(res)
	}
	for _, c := range composedChildren(inst) {
		if c.Provenance() == nil {
			t.Fatal("child provenance is nil under a parent that has one")
		}
		if got := c.Provenance().SourceName(); got != "data.json" {
			t.Errorf("source = %q", got)
		}
		if got := c.Provenance().Path().String(); got != "$.Doc[0].lines[0]" {
			t.Errorf("path = %q, want $.Doc[0].lines[0]", got)
		}
	}

	inst, res = v.ValidateOne(t.Context(), "Doc", instance.RawInstance{Properties: props()})
	if !res.OK() {
		t.Fatal(res)
	}
	for _, c := range composedChildren(inst) {
		if c.Provenance() != nil {
			t.Errorf("a parent without provenance yields a child with %v", c.Provenance())
		}
	}
}

// An absent required composition is reported at the object that lacks it.
func TestAbsentComposition_PathIsTheParentObject(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, docSrc))
	_, res := v.ValidateOne(t.Context(), "Doc", instance.RawInstance{Properties: map[string]any{"id": "d"}})
	if got := mustIssue(t, res, instance.ErrUnresolvedRequiredComposition).Path(); got != "$" {
		t.Errorf("path = %q, want $", got)
	}
}

// Every Fatal the validator raises on its own path carries the provenance it
// was handed, so a cancelled batch still names the row it stopped on.
func TestFatal_CarriesProvenance(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, docSrc))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	prov := location.NewProvenance("data.json", path.Root().Key("Doc").Index(3), location.Span{})
	_, res := v.ValidateOne(ctx, "Doc", instance.RawInstance{Properties: map[string]any{"id": "d"}, Provenance: prov})
	is := mustIssue(t, res, diag.E_CONTEXT_CANCELLED)
	if is.SourceName() != "data.json" || is.Path() != "$.Doc[3]" {
		t.Errorf("source=%q path=%q, want data.json $.Doc[3]", is.SourceName(), is.Path())
	}
}

// The provenance a caller supplies reaches every diagnostic about the
// instance: the object-level ones at the object, the span where one exists.
// A batch-wide failure is about no instance: it keeps the source, at its
// root, with no span.
func TestProvenance_ReachesEveryDiagnostic(t *testing.T) {
	src := location.NewSourceID("data.json")
	span := location.Span{Source: src, Start: location.Position{Line: 7, Column: 3, Byte: 40}, End: location.Position{Line: 7, Column: 9, Byte: 46}}
	prov := location.NewProvenance("data.json", path.Root().Key("Doc").Index(2), span)
	v := instance.NewValidator(loadSrc(t, docSrc))

	t.Run("missing_required_property", func(t *testing.T) {
		_, res := v.ValidateOne(t.Context(), "Doc", instance.RawInstance{Properties: map[string]any{
			"lines": []any{map[string]any{"n": "a", "qty": 1}},
		}, Provenance: prov})
		is := mustIssue(t, res, instance.ErrMissingRequired)
		if is.Path() != "$.Doc[2]" || is.SourceName() != "data.json" || is.Span() != span {
			t.Errorf("path=%q source=%q span=%v", is.Path(), is.SourceName(), is.Span())
		}
	})
	// A nil primary-key property is a missing required property: a primary
	// key is required by definition, so no separate code exists for it.
	t.Run("nil_primary_key_is_missing_required", func(t *testing.T) {
		_, res := v.ValidateOne(t.Context(), "Doc", instance.RawInstance{Properties: map[string]any{
			"id": nil, "lines": []any{map[string]any{"n": "a", "qty": 1}},
		}, Provenance: prov})
		is := mustIssue(t, res, instance.ErrMissingRequired)
		if is.Path() != "$.Doc[2]" || is.Span() != span {
			t.Errorf("path=%q span=%v", is.Path(), is.Span())
		}
	})
	t.Run("type_not_found", func(t *testing.T) {
		_, res := v.ValidateOne(t.Context(), "Nope", instance.RawInstance{Properties: map[string]any{"id": "d"}, Provenance: prov})
		is := mustIssue(t, res, instance.ErrTypeNotFound)
		if is.Path() != "$.Doc[2]" || is.SourceName() != "data.json" || is.Span() != span {
			t.Errorf("path=%q source=%q span=%v", is.Path(), is.SourceName(), is.Span())
		}
	})
	t.Run("composition_not_found", func(t *testing.T) {
		_, res := v.ValidateForComposition(t.Context(), "Doc", "NOPE", []instance.RawInstance{{Provenance: prov}})
		is := mustIssue(t, res, instance.ErrCompositionNotFound)
		if is.Path() != "$" || is.SourceName() != "data.json" || is.Span() != (location.Span{}) {
			t.Errorf("path=%q source=%q span=%v, want the batch's source at its root with no span", is.Path(), is.SourceName(), is.Span())
		}
	})
}

// A leading dot is not an empty import alias: ".Person" is a name the schema
// does not have, reported as such.
func TestValidateOne_LeadingDotIsNotAnAlias(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, docSrc))
	_, res := v.ValidateOne(t.Context(), ".Doc", instance.RawInstance{Properties: map[string]any{"id": "d"}})
	is := mustIssue(t, res, instance.ErrTypeNotFound)
	if want := `type ".Doc" not found`; is.Message() != want {
		t.Errorf("message = %q, want %q", is.Message(), want)
	}
}
