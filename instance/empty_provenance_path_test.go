package instance_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/location"
)

// TestValidate_AnEmptyProvenancePathStartsAtTheRoot holds every diagnostic path
// to the grammar's "$" when the provenance records no path, at both entries. A
// recorded path is the base as given.
func TestValidate_AnEmptyProvenancePathStartsAtTheRoot(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadSrc(t, personCompany))
	raw := func(p path.Path) instance.RawInstance {
		return instance.RawInstance{
			Properties: map[string]any{"id": "p", "name": "n", "zz": 1},
			Provenance: instance.NewProvenance("d.json", p, location.Span{}),
		}
	}
	for _, c := range []struct {
		base path.Path
		want string
	}{
		{"", "$.zz"},
		{path.Root, "$.zz"},
		{path.Root.Index(3), "$[3].zz"},
	} {
		_, one := diagtest.Issues(t, func() (instance.ValidInstance, error) {
			return v.ValidateOne(t.Context(), "Person", raw(c.base))
		})
		if got := mustIssue(t, one, diag.E_UNKNOWN_FIELD).Path(); got != c.want {
			t.Errorf("ValidateOne at base %q: path %q, want %q", c.base, got, c.want)
		}
		_, batch := diagtest.Issues(t, func() ([]instance.ValidInstance, error) {
			return v.Validate(t.Context(), "Person", []instance.RawInstance{raw(c.base)})
		})
		if got := mustIssue(t, batch, diag.E_UNKNOWN_FIELD).Path(); got != c.want {
			t.Errorf("Validate at base %q: path %q, want %q", c.base, got, c.want)
		}
	}
}
