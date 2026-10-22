package neo4j

import (
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// timestampFormats holds one format of every class of text, with the verdict
// written by hand: refused means the bytes are not UTF-8.
var timestampFormats = []struct {
	class   string
	format  string
	refused bool
}{
	{"empty", "", false},
	{"a layout", "2006-01-02", false},
	{"a layout with a zone", "2006-01-02T15:04:05Z07:00", false},
	{"no layout element", "plain text", false},
	{"a NUL", "2006\x00", false},
	{"DEL", "\x7f", false},
	{"a two-byte character", "é 2006", false},
	{"a three-byte character", "2006年", false},
	{"a three-byte character led by E0", "\u0800", false},
	{"a four-byte character led by F1", "\U00040000", false},
	{"a four-byte character", "\U0001F600", false},
	{"the first two-byte character", "\u0080", false},
	{"the last character before the surrogates", "\ud7ff", false},
	{"the first character after the surrogates", "\ue000", false},
	{"the last character", "\U0010ffff", false},
	{"a byte order mark", "\ufeff2006", false},
	{"the replacement character, written out", "\ufffd", false},

	{"a byte no character holds", "\xff", true},
	{"a continuation byte alone", "\x80", true},
	{"a two-byte character cut short", "\xc3", true},
	{"a three-byte character cut short", "\xe2\x82", true},
	{"a four-byte character cut short", "\xf0\x9f\x98", true},
	{"an overlong two-byte form", "\xc0\xaf", true},
	{"an overlong three-byte form", "\xe0\x80\xaf", true},
	{"an overlong four-byte form", "\xf0\x80\x80\xaf", true},
	{"a surrogate", "\xed\xa0\x80", true},
	{"past the last character", "\xf4\x90\x80\x80", true},
	{"a five-byte form", "\xf8\x88\x80\x80\x80", true},
	{"a bad byte first", "\xff2006-01-02", true},
	{"a bad byte in the middle", "2006\xff01", true},
	{"a bad byte last", "2006-01-02\xff", true},
	{"a lead byte before ASCII", "\xc3(", true},
}

// Every door gives one verdict on a Timestamp format, the one the constructor
// recorded: a format that is not UTF-8 is refused in the judge's words, at a
// property, a datatype, a reference and a List element, and any other is taken.
func TestTimestampFormat_EveryDoorGivesTheConstructorsVerdict(t *testing.T) {
	for _, row := range timestampFormats {
		direct := schema.NewTimestampConstraintFormatted(row.format)
		fault := fmt.Sprintf("timestamp format %q is not valid UTF-8", row.format)
		for position, c := range map[string]schema.Constraint{
			"itself":            direct,
			"a reference to it": schema.NewAliasConstraint("At", direct),
			"a List of it":      schema.NewListConstraint(direct),
			"a List of a List":  schema.NewListConstraint(schema.NewListConstraint(schema.NewAliasConstraint("At", direct))),
		} {
			answers := map[string]string{
				"CheckConstraint": errAnswer(schema.CheckConstraint(c)),
				"Build as a property": buildAnswer(func(sb *schema.Builder) *schema.Builder {
					return sb.AddDataType("At", direct).AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).WithProperty("v", c).Done()
				}),
			}
			_, err := Coerce(c, nil)
			answers["Coerce"] = errAnswer(err)
			_, err = CoerceParams(nil, ParamTypes{"v": c})
			answers["CoerceParams"] = errAnswer(err)
			for door, got := range answers {
				if refused := got != "ok"; refused != row.refused || (refused && !strings.Contains(got, fault)) {
					t.Errorf("%s, %s, %s = %s; want refused %v with %s", row.class, position, door, got, row.refused, fault)
				}
			}
		}
		if got := buildAnswer(func(sb *schema.Builder) *schema.Builder {
			return sb.AddDataType("V", direct).AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).Done()
		}); (got != "ok") != row.refused {
			t.Errorf("%s, Build as a datatype = %s; want refused %v", row.class, got, row.refused)
		}
		if direct.Format() != row.format {
			t.Errorf("%s: Format = %q; want the format as given", row.class, direct.Format())
		}
	}
}
