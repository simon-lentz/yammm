package cypher

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// The zero Config is the default mapping.
func TestConfig_TheZeroValueIsTheDefaultMapping(t *testing.T) {
	t.Parallel()

	var c Config
	if c.Edition != Enterprise || c.TypeConstraints != AllProperties || c.NodeKeys || c.LabelPrefix != "" {
		t.Errorf("the zero Config is %+v, want Enterprise, AllProperties, no NODE KEY and no prefix", c)
	}
	s, result := schema.LoadString(t.Context(), "schema \"s\"\n\ntype T {\n\tid String primary\n}\n", "s.yammm")
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	ty, _ := s.Type("T")
	if got := c.Label(ty); got != "s__T" {
		t.Errorf("the zero Config labels %q, want s__T", got)
	}
}
