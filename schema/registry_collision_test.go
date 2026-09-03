package schema_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// Two files in one closure may declare the same schema name. Registry.Register
// refuses the second, and that refusal is the guarantee the .ys types table and
// StructuralHash are keyed on — so the diagnostic must name the clash, carry
// the declaration it points at, and say which source already holds the name.
//
// TestLoad_SchemaNameCollisionNamesBothSources is also the first test to reach
// the loader's Register failure branch at all.
func TestLoad_SchemaNameCollisionNamesBothSources(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	body := func(prop string) string {
		return `schema "part"

type Part {
    id String primary
    ` + prop + ` String
}
`
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("one.yammm", body("a"))
	write("two.yammm", body("b"))
	write("main.yammm", `schema "app"

import "./one.yammm" as x
import "./two.yammm" as y

type T {
    id String primary
    --> A (one) x.Part
    --> B (one) y.Part
}
`)

	_, res := schema.Load(t.Context(), filepath.Join(dir, "main.yammm"), schema.WithModuleRoot(dir))
	if res.Err() == nil {
		t.Fatal("two schemas declaring one name must not both register")
	}

	var found bool
	for is := range res.Issues() {
		if is.Code().String() != "E_DUPLICATE_TYPE" {
			continue
		}
		found = true
		msg := is.Message()
		if strings.Contains(msg, "register schema:") {
			t.Errorf("the diagnostic leaks the raw Go error: %q", msg)
		}
		if !strings.Contains(msg, `"part"`) {
			t.Errorf("the diagnostic does not name the colliding schema: %q", msg)
		}
		if !strings.Contains(msg, "one.yammm") {
			t.Errorf("the diagnostic does not name the source already holding the name: %q", msg)
		}
		if is.Span().IsZero() {
			t.Error("the diagnostic carries no span; it should point at the schema declaration")
		}
	}
	if !found {
		t.Error("no E_DUPLICATE_TYPE was reported for a schema-name collision")
	}
}
