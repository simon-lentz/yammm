package value_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// A UUID's text of 38 bytes is a UUID between braces and nothing else.
// uuid.Parse reads any 38 bytes that hold a UUID after the first byte and
// before the last, so the checker, the stored form and the coercion each
// refuse what it alone would take.
func TestCheck_AUUIDOf38BytesIsOneBetweenBraces(t *testing.T) {
	t.Parallel()
	const id = "0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b"
	c := schema.NewUUIDConstraint()
	for _, text := range []string{
		"x" + id + "y", "{" + id + "x", "x" + id + "}", "}" + id + "{",
		"[" + id + "]", "(" + id + ")", " " + id + " ", `"` + id + `"`,
	} {
		if _, ok := value.ParseUUID(text); ok {
			t.Errorf("ParseUUID(%q) read a UUID", text)
		}
		err := value.Check(text, c)
		var checkErr *value.CheckError
		if !errors.As(err, &checkErr) || checkErr.Kind != value.KindConstraintFail || !strings.HasPrefix(err.Error(), "invalid UUID: ") {
			t.Errorf("Check(%q) = %v; want the constraint failure of an invalid UUID", text, err)
		}
		if got, err := value.Canonical(text, c); err == nil || got != text {
			t.Errorf("Canonical(%q) = %#v, %v; want the text back beside an error", text, got, err)
		}
		if got, err := value.Coerce(text, c); err == nil {
			t.Errorf("Coerce(%q) = %#v; want an error", text, got)
		}
	}
	for _, text := range []string{
		id, "{" + id + "}", strings.ToUpper(id), "{" + strings.ToUpper(id) + "}",
		"urn:uuid:" + id, "URN:UUID:" + id, strings.ReplaceAll(id, "-", ""),
	} {
		if u, ok := value.ParseUUID(text); !ok || u.String() != id {
			t.Errorf("ParseUUID(%q) = %s, %v; want %s", text, u, ok, id)
		}
		if err := value.Check(text, c); err != nil {
			t.Errorf("Check(%q) = %v; want nil", text, err)
		}
		if got, err := value.Canonical(text, c); err != nil || got != id {
			t.Errorf("Canonical(%q) = %#v, %v; want %q", text, got, err, id)
		}
	}
}
