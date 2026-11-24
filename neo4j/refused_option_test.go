package neo4j

import (
	"errors"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema/cypher"
)

// Each call is held to the options it takes: any other is refused before
// anything is generated, whatever its value, by an error that names the first
// such option and the call and carries no diagnostic code.
func TestCalls_RefuseAnOptionTheyDoNotTake(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	snap := holderSnapshot(t, holderValues{})
	s := loadInline(t, "schema \"p\"\n\ntype T {\n\tid String primary\n}\n")
	warnings := WithWarnings(func(diag.Issue) {})

	type call func(opts ...Option) error
	nodes := func(opts ...Option) error {
		qs, err := NodeQueries(ctx, cypher.Config{}, snap, opts...)
		if qs != nil && err != nil {
			t.Error("NodeQueries returned queries beside an error")
		}
		return err
	}
	edges := func(opts ...Option) error {
		qs, err := EdgeQueries(ctx, cypher.Config{}, snap, opts...)
		if qs != nil && err != nil {
			t.Error("EdgeQueries returned queries beside an error")
		}
		return err
	}
	plan := func(opts ...Option) error {
		_, err := Diff(cypher.Config{}, s, nil, nil, opts...)
		return err
	}

	refused := []struct {
		name string
		call call
		opts []Option
		want string
	}{
		{"NodeQueries WithEdgeChunkSize", nodes, []Option{WithEdgeChunkSize(1)}, "neo4j.NodeQueries does not take the WithEdgeChunkSize option"},
		{"NodeQueries WithWarnings", nodes, []Option{warnings}, "neo4j.NodeQueries does not take the WithWarnings option"},
		{"EdgeQueries WithNodeChunkSize", edges, []Option{WithNodeChunkSize(1)}, "neo4j.EdgeQueries does not take the WithNodeChunkSize option"},
		{"EdgeQueries WithWarnings", edges, []Option{warnings}, "neo4j.EdgeQueries does not take the WithWarnings option"},
		{"Diff WithNodeChunkSize", plan, []Option{WithNodeChunkSize(1)}, "neo4j.Diff does not take the WithNodeChunkSize option"},
		{"Diff WithEdgeChunkSize", plan, []Option{WithEdgeChunkSize(1)}, "neo4j.Diff does not take the WithEdgeChunkSize option"},
		{
			"the first refused option is named", plan,
			[]Option{warnings, WithEdgeChunkSize(1), WithNodeChunkSize(1)},
			"neo4j.Diff does not take the WithEdgeChunkSize option",
		},
	}
	for _, row := range refused {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			err := row.call(row.opts...)
			if err == nil || err.Error() != row.want {
				t.Fatalf("err = %v; want %q", err, row.want)
			}
			if _, ok := errors.AsType[*diag.ResultError](err); ok {
				t.Errorf("err = %#v; want an error with no diagnostics", err)
			}
		})
	}

	taken := []struct {
		name string
		call call
		opts []Option
	}{
		{"NodeQueries", nodes, []Option{WithNodeChunkSize(1)}},
		{"EdgeQueries", edges, []Option{WithEdgeChunkSize(1)}},
		{"Diff", plan, []Option{warnings, WithWarnings(nil)}},
	}
	for _, row := range taken {
		t.Run("takes/"+row.name, func(t *testing.T) {
			t.Parallel()
			if err := row.call(row.opts...); err != nil {
				t.Fatalf("err = %v; want nil", err)
			}
		})
	}
}

// Diff sends the constraint generation's warnings to WithWarnings as they are
// found, and its error holds failures alone, whether a plan comes back or not.
func TestDiff_WarningsGoToTheCallbackNeverIntoTheError(t *testing.T) {
	t.Parallel()
	c := cypher.Config{Edition: cypher.Community, NodeKeys: true, TypeConstraints: cypher.NoProperties}
	for name, src := range map[string]string{
		"a plan":           "schema \"p\"\n\ntype T {\n\tid String primary\n\tx String @index\n}\n",
		"an index failure": "schema \"p\"\n\ntype T {\n\tid String primary\n\tmatch String @index\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := loadInline(t, src)
			var got []diag.Code
			_, err := Diff(c, s, nil, nil, WithWarnings(func(is diag.Issue) { got = append(got, is.Code()) }))
			if len(got) != 2 || !slices.Contains(got, cypher.W_NEO4J_NODE_KEY_UNSUPPORTED) || !slices.Contains(got, cypher.W_NEO4J_EDITION_CONSTRAINT_OMITTED) {
				t.Errorf("the callback received %v, want the node-key and the edition warning", got)
			}
			if wantErr := name == "an index failure"; (err != nil) != wantErr || wantErr && !errors.Is(err, cypher.E_NEO4J_INVALID_IDENTIFIER) {
				t.Fatalf("Diff = %v, want an error %t", err, wantErr)
			}
			if re, ok := errors.AsType[*diag.ResultError](err); ok && re.Result.HasWarnings() {
				t.Errorf("the error holds a warning:\n%s", re.Result)
			}
			if _, err := Diff(c, s, nil, nil); err != nil && name == "a plan" {
				t.Errorf("Diff with no callback = %v, want the warnings discarded and a plan", err)
			}
		})
	}
}
