package schema_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

const doesNotPrint = "holds a character that does not print"

// Every door of this package that mints a synthetic identity refuses a
// character that does not print: a root, a source key, an import key, a
// LoadString name and a Builder's source ID.
func TestSyntheticIdentity_EveryDoorRefusesACharacterThatDoesNotPrint(t *testing.T) {
	t.Parallel()
	source := []byte("schema \"s\"\n\ntype A {\n\tid String primary\n}\n")
	for _, bad := range []string{"\u202e", "\u2066", "\u200e", "\x00", "\n"} {
		if _, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{"a" + bad + ".yammm": source}, "a"+bad+".yammm", "",
			schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://app")); !res.HasErrors() || !strings.Contains(firstError(res), doesNotPrint) {
			t.Errorf("key %q: %s", bad, firstError(res))
		}
		if _, msg := loadUnderRoot(t, "embedded://a"+bad+"b"); !strings.Contains(msg, "invalid synthetic root") {
			t.Errorf("root %q: %s", bad, msg)
		}
		if _, res := schema.LoadString(t.Context(), string(source), "s"+bad+".yammm"); !res.HasErrors() || !strings.Contains(firstError(res), doesNotPrint) {
			t.Errorf("LoadString name %q: %s", bad, firstError(res))
		}
		if _, err := schema.SyntheticImportKey("main.yammm", "./a"+bad+"b"); !errors.Is(err, location.ErrUnprintableSourceID) {
			t.Errorf("SyntheticImportKey %q = %v", bad, err)
		}
		_, res := schema.NewBuilder().WithName("s").WithSourceID(location.NewSourceID("test://a"+bad+"b")).
			AddType("A").WithPrimaryKey("id", schema.NewStringConstraint()).Done().Build()
		if !hasCode(res, diag.E_INVALID_SYNTHETIC_ID) {
			t.Errorf("Builder source ID %q: %s", bad, res.String())
		}
	}
}
