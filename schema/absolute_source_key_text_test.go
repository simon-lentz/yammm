package schema_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

const absoluteStart = "it starts as a file-backed source's path does: with a slash, two backslashes, or a drive letter, a colon and a separator"

// A source key shaped like an absolute path is refused under a synthetic root
// with the starts that make it so, and is not advised to take a scheme, which
// would only make it another relative key.
func TestLoadSources_AnAbsoluteKeyIsRefusedWithoutSchemeAdvice(t *testing.T) {
	t.Parallel()
	source := []byte("schema \"s\"\n\ntype A {\n\tid String primary\n}\n")
	for _, key := range []string{"/abs.yammm", "C:/abs.yammm"} {
		_, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{key: source}, key, "",
			schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://app"))
		if !res.HasErrors() {
			t.Errorf("key %q loaded", key)
			continue
		}
		msg := firstError(res)
		if !strings.Contains(msg, absoluteStart) || strings.Contains(msg, "scheme prefix") {
			t.Errorf("key %q: %s", key, msg)
		}
	}
}

// The builder reports a synthetic source identifier shaped like an absolute
// path with the same sentence.
func TestBuilder_AnAbsoluteSyntheticSourceIDStatesTheStarts(t *testing.T) {
	t.Parallel()
	_, res := schema.NewBuilder().
		WithName("s").
		WithSourceID(location.NewSourceID("/abs/s.yammm")).
		AddType("A").
		WithProperty("id", schema.NewStringConstraint()).
		Done().
		Build()
	found := false
	for issue := range res.Issues() {
		if issue.Code() != diag.E_INVALID_SYNTHETIC_ID {
			continue
		}
		found = true
		if msg := issue.Message(); !strings.HasSuffix(msg, absoluteStart) || strings.Contains(msg, "scheme prefix") {
			t.Errorf("the issue reads %s", msg)
		}
	}
	if !found {
		t.Errorf("no E_INVALID_SYNTHETIC_ID: %s", res)
	}
}
