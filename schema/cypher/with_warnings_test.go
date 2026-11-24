package cypher

import (
	"errors"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// Constraints sends each warning to WithWarnings as it is found, beside a
// failure of a type's property too, and its error holds failures alone. With no callback, or a nil
// one, the warnings are discarded and the call's value and error are the same.
func TestConstraints_WarningsGoToTheCallbackNeverIntoTheError(t *testing.T) {
	t.Parallel()
	c := Config{Edition: Community, NodeKeys: true}
	for name, src := range map[string]string{
		"accepted": "schema \"p\"\n\ntype T {\n\tid String primary\n}\n",
		"refused":  "schema \"p\"\n\ntype T {\n\tid String primary\n\tnested List<List<String>>\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := loadInline(t, src)
			var got []diag.Code
			constraints, err := c.Constraints(s, WithWarnings(func(is diag.Issue) {
				if sev := is.Severity(); sev == diag.Fatal || sev == diag.Error {
					t.Errorf("the callback received a failure: %v", is.Code())
				}
				got = append(got, is.Code())
			}))
			if !slices.Contains(got, W_NEO4J_NODE_KEY_UNSUPPORTED) || !slices.Contains(got, W_NEO4J_EDITION_CONSTRAINT_OMITTED) {
				t.Errorf("the callback received %v, want the node-key and the edition warning", got)
			}
			refused := name == "refused"
			if (err != nil) != refused || refused && (constraints != nil || !errors.Is(err, E_NEO4J_UNSUPPORTED_TYPE)) {
				t.Fatalf("Constraints = %d constraints, %v; want a refusal %t", len(constraints), err, refused)
			}
			if re, ok := errors.AsType[*diag.ResultError](err); ok && re.Result.HasWarnings() {
				t.Errorf("the error holds a warning:\n%s", re.Result)
			}
			for _, opts := range [][]Option{nil, {WithWarnings(nil)}} {
				again, againErr := c.Constraints(s, opts...)
				if len(again) != len(constraints) || errors.Is(againErr, E_NEO4J_UNSUPPORTED_TYPE) != refused {
					t.Errorf("Constraints(%d options) = %d constraints, %v; want the value and error the callback's call returned", len(opts), len(again), againErr)
				}
			}
		})
	}
}
