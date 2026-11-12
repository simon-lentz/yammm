package schema_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// A source that writes a format after Timestamp does not load: the bracket
// draws E_SYNTAX at each position a Timestamp is written. It is the one
// diagnostic here, where no later check reads what the parser drops with the
// bracket.
func TestLoad_TimestampTakesNoFormat(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]string{
		"a property":      "schema \"s\"\ntype T {\n\tid String primary\n\tt Timestamp[\"2006-01-02\"]\n}\n",
		"RFC 3339 itself": "schema \"s\"\ntype T {\n\tid String primary\n\tt Timestamp[\"2006-01-02T15:04:05Z07:00\"]\n}\n",
		"a datatype":      "schema \"s\"\ntype At = Timestamp[\"2006-01-02\"]\ntype T {\n\tid String primary\n}\n",
		"a List element":  "schema \"s\"\ntype T {\n\tid String primary\n\tl List<Timestamp[\"2006-01-02\"]>\n}\n",
		"an edge property": "schema \"s\"\ntype B {\n\tid String primary\n}\ntype T {\n\tid String primary\n" +
			"\t--> R (one) B {\n\t\tsince Timestamp[\"2006-01-02\"]\n\t}\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, res := schema.LoadString(t.Context(), src, "s.yammm")
			if !res.HasErrors() {
				t.Fatalf("the source loads: %v", res)
			}
			var got []string
			for iss := range res.Issues() {
				got = append(got, iss.Code().String()+" "+iss.Message())
			}
			if len(got) != 1 || !strings.HasPrefix(got[0], diag.E_SYNTAX.String()+` unexpected token "["`) {
				t.Errorf("issues = %q, want one E_SYNTAX on the bracket", got)
			}
		})
	}
}

// A Timestamp is one constraint: the loader's, the constructor's and the zero
// value are equal, print alike, and CheckConstraint accepts each.
func TestTimestampConstraint_IsOneValue(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), "schema \"s\"\ntype T {\n\tid String primary\n\tat Timestamp required\n}\n", "s.yammm")
	if res.Len() != 0 {
		t.Fatalf("load: %v", res)
	}
	typ, _ := s.Type("T")
	at, ok := typ.Property("at")
	if !ok {
		t.Fatal("T declares no at")
	}
	loaded := at.Constraint()
	for name, c := range map[string]schema.Constraint{
		"the loader's":      loaded,
		"the constructor's": schema.NewTimestampConstraint(),
		"the zero value":    schema.TimestampConstraint{},
	} {
		if c != schema.NewTimestampConstraint() || !c.Equal(loaded) || !loaded.Equal(c) {
			t.Errorf("%s Timestamp is %#v, want the one Timestamp", name, c)
		}
		if c.String() != "Timestamp" || c.Kind() != schema.KindTimestamp {
			t.Errorf("%s Timestamp prints %q of kind %v", name, c.String(), c.Kind())
		}
		if err := schema.CheckConstraint(c); err != nil {
			t.Errorf("CheckConstraint refuses %s Timestamp: %v", name, err)
		}
	}
}

// A schema that holds a Timestamp keeps the structural hash it had while a
// Timestamp could hold a format and it declared none.
func TestStructuralHash_ATimestampKeepsItsHash(t *testing.T) {
	t.Parallel()
	const src = "schema \"s\"\n\ntype At = Timestamp\n\ntype T {\n\tid String primary\n\tat Timestamp required\n\twhen At\n\tall List<Timestamp>\n}\n"
	s, res := schema.LoadString(t.Context(), src, "s.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res)
	}
	const want = "sha256:0f60e7065c0ec92a21519eb993fea36c46378f53912bc079e3dc8e49dcfcf02a"
	if got := schema.StructuralHash(s); got != want {
		t.Errorf("StructuralHash = %s, want %s", got, want)
	}
}
