package csv

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// A type column holding a CR LF names a column no header holds, since
// encoding/csv reads a quoted CR LF back as LF; the name is refused as
// configuration before a byte is read, not reported as missing from the header.
func TestParseWithTypeColumn_ANameNoHeaderCanHoldIsRefused(t *testing.T) {
	t.Parallel()
	r := &countingReader{r: strings.NewReader("\"kind\r\nx\",id\nEntity,e1\n")}
	byType, res := New(WithTypeColumn("kind\r\nx")).ParseWithTypeColumn(t.Context(),
		location.MustNewSourceID("test://t.csv"), r, func(string) *schema.Type { return nil })
	if !res.HasCode(E_CSV_CONFIG) || res.Len() != 1 {
		t.Errorf("got %s, want one E_CSV_CONFIG", res)
	}
	if issue, ok := issueContaining(res, "CR LF"); !ok || issue.Span().Start.Line != 0 {
		t.Errorf("the refusal does not name the CR LF, or carries a span: %s", res)
	}
	if len(byType) != 0 || r.reads != 0 {
		t.Errorf("%d types parsed after %d reads, want none", len(byType), r.reads)
	}
}

// A lone CR or LF in a quoted header name reads back as written, so a type
// column named with one is found.
func TestParseWithTypeColumn_ALoneCROrLFNameIsFound(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"kind\rx", "kind\nx"} {
		in := `"` + name + "\",id\nEntity,e1\n"
		byType, res := New(WithTypeColumn(name)).ParseWithTypeColumn(t.Context(),
			location.MustNewSourceID("test://t.csv"), strings.NewReader(in), func(string) *schema.Type { return nil })
		if !res.OK() || len(byType["Entity"]) != 1 {
			t.Errorf("type column %q: %d rows, %s", name, len(byType["Entity"]), res)
		}
	}
}
