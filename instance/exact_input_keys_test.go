package instance_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/yammmtest"
)

const exactKeysSchema = `schema "k"

type Company {
    id String primary
}

part type Line {
    n String primary
}

type Person {
    id String primary
    name String required
    --> WORKS_AT (one) Company {
        title String required
    }
    *-> LINES (many) Line
}

type Order {
    id String primary
    *-> REQ (one) Line
}
`

// issuesAt renders each issue of res as its code and path, sorted, and fails
// when an issue carries a reason detail: a key in another case is unknown
// and nothing more.
func issuesAt(t *testing.T, res diag.Result) []string {
	t.Helper()
	var out []string
	for is := range res.Issues() {
		out = append(out, is.Code().String()+" "+is.Path())
		if _, ok := detail(is, diag.DetailKeyReason); ok {
			t.Errorf("%s carries a reason detail", is.Code())
		}
	}
	slices.Sort(out)
	return out
}

// A property or relation field spelled in another case names no member: it is
// an unknown field, and the member it was meant for stays absent.
func TestValidate_AKeyInAnotherCaseIsAnUnknownField(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadSrc(t, exactKeysSchema))
	for _, c := range []struct {
		name  string
		props map[string]any
		want  []string
	}{
		{
			"a property",
			map[string]any{"id": "p", "Name": "n"},
			[]string{"E_MISSING_REQUIRED $", "E_UNKNOWN_FIELD $.Name"},
		},
		{
			"an association's field",
			map[string]any{"id": "p", "name": "n", "Works_At": map[string]any{"_target_id": "c", "title": "t"}},
			[]string{"E_UNKNOWN_FIELD $.Works_At"},
		},
		{
			"a composition's field",
			map[string]any{"id": "p", "name": "n", "LINES": []any{map[string]any{"n": "a"}}},
			[]string{"E_UNKNOWN_FIELD $.LINES"},
		},
		// An edge object or child that breaks its own rules shows whether the
		// member was read: an absent member reports none of its breaks.
		{
			"an association's field holding a broken edge",
			map[string]any{"id": "p", "name": "n", "Works_At": map[string]any{"title": "t"}},
			[]string{"E_UNKNOWN_FIELD $.Works_At"},
		},
		{
			"a composition's field holding a broken child",
			map[string]any{"id": "p", "name": "n", "LINES": []any{map[string]any{"bogus": 1}}},
			[]string{"E_UNKNOWN_FIELD $.LINES"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			valid, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: c.props})
			if !valid.IsZero() {
				t.Error("an instance with an unknown field validated")
			}
			yammmtest.Diff(t, c.want, issuesAt(t, res))
		})
	}
}

// In an edge object a foreign-key field or an edge property spelled in another
// case is an unknown edge field, and the member it was meant for stays absent.
func TestValidate_AnEdgeKeyInAnotherCaseIsAnUnknownEdgeField(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadSrc(t, exactKeysSchema))
	for _, c := range []struct {
		name string
		edge map[string]any
		want []string
	}{
		{
			"a foreign-key field",
			map[string]any{"_Target_ID": "c", "title": "t"},
			[]string{"E_MISSING_FK_TARGET $.works_at", "E_UNKNOWN_EDGE_FIELD $.works_at._Target_ID"},
		},
		{
			"an edge property",
			map[string]any{"_target_id": "c", "Title": "t"},
			[]string{"E_MISSING_REQUIRED $.works_at", "E_UNKNOWN_EDGE_FIELD $.works_at.Title"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
				"id": "p", "name": "n", "works_at": c.edge,
			}})
			yammmtest.Diff(t, c.want, issuesAt(t, res))
		})
	}
}

// A required composition spelled in another case is absent, and its absence is
// reported beside the unknown field.
func TestValidate_ARequiredCompositionInAnotherCaseIsAbsent(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadSrc(t, exactKeysSchema))
	_, res := v.ValidateOne(t.Context(), "Order", instance.RawInstance{Properties: map[string]any{
		"id": "o", "REQ": map[string]any{"n": "a"},
	}})
	var got []string
	for is := range res.Issues() {
		got = append(got, is.Code().String()+" "+is.Path())
	}
	slices.Sort(got)
	yammmtest.Diff(t, []string{"E_UNKNOWN_FIELD $.REQ", "E_UNRESOLVED_REQUIRED_COMPOSITION $"}, got)
}

// Under a cap of one issue, the unknown edge field kept is the first in sorted
// order on every run, as on the node path.
func TestValidate_UnknownEdgeFieldsAreSortedUnderACap(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadSrc(t, exactKeysSchema), instance.WithIssueLimit(1))
	for range 20 {
		_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
			"id": "p", "name": "n",
			"works_at": map[string]any{"_target_id": "c", "title": "t", "zz": 1, "yy": 1, "bb": 1, "aa": 1},
		}})
		got := issuesAt(t, res)
		if len(got) != 1 || got[0] != "E_UNKNOWN_EDGE_FIELD $.works_at.aa" {
			t.Fatalf("kept %v, want the first unknown edge field in sorted order", got)
		}
	}
}

// A foreign-key value of the wrong type is reported at the foreign-key field.
func TestValidate_AForeignKeyValueIssueNamesItsField(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadSrc(t, exactKeysSchema))
	_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
		"id": "p", "name": "n", "works_at": map[string]any{"_target_id": 5, "title": "t"},
	}})
	yammmtest.Diff(t, []string{"E_TYPE_MISMATCH $.works_at._target_id"}, issuesAt(t, res))
}
