package neo4j

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// The two faults of a Timestamp format, in the judge's words after the format.
const (
	notUTF8   = "is not valid UTF-8"
	nameAlone = "holds a zone abbreviation and no numeric offset"
)

// timestampFormats holds one format of every class of text, with the verdict
// written by hand: the fault the judge names, or none.
var timestampFormats = []struct {
	class  string
	format string
	fault  string
}{
	{"empty", "", ""},
	{"a layout", "2006-01-02", ""},
	{"a layout with a zone", "2006-01-02T15:04:05Z07:00", ""},
	{"no layout element", "plain text", ""},
	{"a NUL", "2006\x00", ""},
	{"DEL", "\x7f", ""},
	{"a two-byte character", "é 2006", ""},
	{"a three-byte character", "2006年", ""},
	{"a three-byte character led by E0", "\u0800", ""},
	{"a four-byte character led by F1", "\U00040000", ""},
	{"a four-byte character", "\U0001F600", ""},
	{"the first two-byte character", "\u0080", ""},
	{"the last character before the surrogates", "\ud7ff", ""},
	{"the first character after the surrogates", "\ue000", ""},
	{"the last character", "\U0010ffff", ""},
	{"a byte order mark", "\ufeff2006", ""},
	{"the replacement character, written out", "\ufffd", ""},

	{"a byte no character holds", "\xff", notUTF8},
	{"a continuation byte alone", "\x80", notUTF8},
	{"a two-byte character cut short", "\xc3", notUTF8},
	{"a three-byte character cut short", "\xe2\x82", notUTF8},
	{"a four-byte character cut short", "\xf0\x9f\x98", notUTF8},
	{"an overlong two-byte form", "\xc0\xaf", notUTF8},
	{"an overlong three-byte form", "\xe0\x80\xaf", notUTF8},
	{"an overlong four-byte form", "\xf0\x80\x80\xaf", notUTF8},
	{"a surrogate", "\xed\xa0\x80", notUTF8},
	{"past the last character", "\xf4\x90\x80\x80", notUTF8},
	{"a five-byte form", "\xf8\x88\x80\x80\x80", notUTF8},
	{"a bad byte first", "\xff2006-01-02", notUTF8},
	{"a bad byte in the middle", "2006\xff01", notUTF8},
	{"a bad byte last", "2006-01-02\xff", notUTF8},
	{"a lead byte before ASCII", "\xc3(", notUTF8},

	{"a zone abbreviation alone", "2006-01-02 15:04 MST", nameAlone},
	{"the abbreviation and nothing else", "MST", nameAlone},
	{"RFC 1123", time.RFC1123, nameAlone},
	{"RFC 850", time.RFC850, nameAlone},
	{"RFC 822", time.RFC822, nameAlone},
	{"the Unix date", time.UnixDate, nameAlone},
	{"an abbreviation beside a numeric offset", "2006-01-02 15:04 -0700 MST", ""},
	{"an abbreviation beside Z07:00", "MST Z07:00", ""},
	{"RFC 1123 with a numeric offset", time.RFC1123Z, ""},
	{"a numeric offset alone", "-07", ""},
	{"the abbreviation's letters inside a word, which time still reads", "MSTX 2006", nameAlone},
	{"other capitals", "UTC 2006", ""},
	{"bytes that are not UTF-8 beside an abbreviation", "15:04 MST\xff", notUTF8},
}

// Every door gives one verdict on a Timestamp format, the one the constructor
// recorded: a format that is not UTF-8, or that holds a zone abbreviation and
// no numeric offset, is refused in the judge's words, alone, behind a
// reference and as a List element, and any other is taken.
func TestTimestampFormat_EveryDoorGivesTheConstructorsVerdict(t *testing.T) {
	for _, row := range timestampFormats {
		direct := schema.NewTimestampConstraintFormatted(row.format)
		fault := fmt.Sprintf("timestamp format %q %s", row.format, row.fault)
		for position, c := range map[string]schema.Constraint{
			"itself":            direct,
			"a reference to it": schema.NewAliasConstraint("At", direct),
			"a List of it":      schema.NewListConstraint(direct),
			"a List of a List":  schema.NewListConstraint(schema.NewListConstraint(schema.NewAliasConstraint("At", direct))),
		} {
			answers := map[string]string{
				"CheckConstraint": errAnswer(schema.CheckConstraint(c)),
				"CheckValue":      errAnswer(instance.CheckValue(nil, c)),
			}
			_, err := CoerceParams(nil, ParamTypes{"v": c})
			answers["CoerceParams"] = errAnswer(err)
			for door, got := range answers {
				if refused := got != "ok"; refused != (row.fault != "") || (refused && !strings.Contains(got, fault)) {
					t.Errorf("%s, %s, %s = %s; want refused %v with %s", row.class, position, door, got, row.fault != "", fault)
				}
			}
		}
		if direct.Format() != row.format {
			t.Errorf("%s: Format = %q; want the format as given", row.class, direct.Format())
		}
	}
}
