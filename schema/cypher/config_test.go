package cypher

import "testing"

// The zero Config is the default mapping.
func TestConfig_TheZeroValueIsTheDefaultMapping(t *testing.T) {
	t.Parallel()

	var c Config
	if c.Edition != Enterprise || c.TypeConstraints != AllProperties || c.NodeKeys || c.LabelPrefix != "" {
		t.Errorf("the zero Config is %+v, want Enterprise, AllProperties, no NODE KEY and no prefix", c)
	}
	if got := c.labels().Compose("s", "T"); got != "s__T" {
		t.Errorf("the zero Config composes %q, want s__T", got)
	}
}
