package graph_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/schema"
)

// A required association whose key holds null reaches Check as the absent case,
// the issue the association left out draws, its message and its reason alike.
func TestCheck_ARequiredAssociationSetToNullIsAbsent(t *testing.T) {
	t.Parallel()
	s, err := schema.LoadString(t.Context(), `schema "n"

type Co {
	id String primary
}

type T {
	id String primary
	--> ONE (one) Co
	--> MANY (one:many) Co
}
`, "n.yammm")
	if err != nil {
		t.Fatal(err)
	}
	check := func(props map[string]any) diag.Result {
		t.Helper()
		vi, err := instance.NewValidator(s).ValidateOne(t.Context(), "T", instance.RawInstance{Properties: props})
		if err != nil {
			t.Fatalf("validation refused %v: %s", props, err)
		}
		g := graph.New(s)
		if err := g.Add(t.Context(), vi); err != nil {
			t.Fatalf("Add refused %v: %s", props, err)
		}
		return diagtest.Failures(t, g.Check(t.Context()))
	}
	reasons := func(res diag.Result) []string {
		t.Helper()
		var out []string
		for is := range res.Issues() {
			if is.Code() != diag.E_UNRESOLVED_REQUIRED {
				t.Errorf("unexpected issue %s: %s", is.Code(), is.Message())
				continue
			}
			for _, d := range is.Details() {
				if d.Key == diag.DetailKeyReason {
					out = append(out, d.Value)
				}
			}
		}
		return out
	}
	want := check(map[string]any{"id": "t"})
	if r := reasons(want); !slices.Equal(r, []string{"absent", "absent"}) {
		t.Fatalf("the associations left out draw reasons %v, want two absent", r)
	}
	for name, props := range map[string]map[string]any{
		"null":                  {"id": "t", "one": nil, "many": nil},
		"nil map and nil slice": {"id": "t", "one": map[string]any(nil), "many": []any(nil)},
		"null one, absent many": {"id": "t", "one": nil},
		"absent one, null many": {"id": "t", "many": nil},
	} {
		got := check(props)
		if got.String() != want.String() {
			t.Errorf("%s drew\n%s\nwant what the associations left out draw\n%s", name, got, want)
		}
		if r := reasons(got); !slices.Equal(r, []string{"absent", "absent"}) {
			t.Errorf("%s draws reasons %v, want two absent", name, r)
		}
	}
}
