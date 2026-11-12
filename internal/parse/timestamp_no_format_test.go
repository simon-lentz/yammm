package parse

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// A bracket after Timestamp is no production, at a property, an alias, a List
// element and an edge property alike, as it is none after Date.
func TestParse_TimestampTakesNoFormat(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name, src, want string
	}{
		{"a property", "schema \"s\"\ntype T {\n\tid String primary\n\tt Timestamp[\"2006-01-02\"]\n}\n", `unexpected token "[" in a type body`},
		{"a property, a space before the bracket", "schema \"s\"\ntype T {\n\tid String primary\n\tt Timestamp [\"2006-01-02\"] required\n}\n", `unexpected token "[" in a type body`},
		{"RFC 3339's own layout", "schema \"s\"\ntype T {\n\tid String primary\n\tt Timestamp[\"2006-01-02T15:04:05Z07:00\"]\n}\n", `unexpected token "[" in a type body`},
		{"an empty bracket", "schema \"s\"\ntype T {\n\tid String primary\n\tt Timestamp[]\n}\n", `unexpected token "[" in a type body`},
		{"an empty layout", "schema \"s\"\ntype T {\n\tid String primary\n\tt Timestamp[\"\"]\n}\n", `unexpected token "[" in a type body`},
		{"bounds in the bracket", "schema \"s\"\ntype T {\n\tid String primary\n\tt Timestamp[1, 3]\n}\n", `unexpected token "[" in a type body`},
		{"a number in the bracket", "schema \"s\"\ntype T {\n\tid String primary\n\tt Timestamp[5]\n}\n", `unexpected token "[" in a type body`},
		{"two layouts in the bracket", "schema \"s\"\ntype T {\n\tid String primary\n\tt Timestamp[\"2006\", \"01\"]\n}\n", `unexpected token "[" in a type body`},
		{"an alias", "schema \"s\"\ntype Stamp = Timestamp[\"2006\"]\ntype T {\n\tid String primary\n}\n", `unexpected token "[" in a declaration`},
		{"a List element", "schema \"s\"\ntype T {\n\tid String primary\n\tl List<Timestamp[\"2006\"]>\n}\n", `unexpected token "[" in a type body`},
		{"an edge property", "schema \"s\"\ntype T {\n\tid String primary\n\t--> R B { since Timestamp[\"2006\"] }\n}\n", `unexpected token "[" in a type body`},
		{"a Date, the control", "schema \"s\"\ntype T {\n\tid String primary\n\td Date[\"2006-01-02\"]\n}\n", `unexpected token "[" in a type body`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			_, issues := Parse([]byte(row.src), location.NewSourceID("s.yammm"))
			if len(issues) != 1 {
				t.Fatalf("got %d diagnostics, want 1: %v", len(issues), issues)
			}
			iss := issues[0]
			if iss.Code() != diag.E_SYNTAX || iss.Severity() != diag.Error {
				t.Errorf("got %s at %v, want E_SYNTAX at error", iss.Code(), iss.Severity())
			}
			if iss.Message() != row.want {
				t.Errorf("message = %q, want %q", iss.Message(), row.want)
			}
			sp := iss.Span()
			if covered := row.src[sp.Start.Byte:sp.End.Byte]; covered != "[" {
				t.Errorf("anchored on %q, want the bracket", covered)
			}
		})
	}
}

// A bare Timestamp carries its kind and nothing else.
func TestParse_TimestampIsBare(t *testing.T) {
	t.Parallel()
	src := "schema \"s\"\ntype T {\n\tid String primary\n\tt Timestamp required\n}\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	c := file.Types[0].Properties[1].Constraint
	if c == nil || c.Kind != ConstraintTimestamp {
		t.Fatalf("constraint = %+v, want a Timestamp", c)
	}
	if got := src[c.Span.Start.Byte:c.Span.End.Byte]; got != "Timestamp" {
		t.Errorf("span covers %q, want the keyword", got)
	}
}
