package schema_test

import (
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// loadWithFormat loads a one-property schema declaring the given timestamp
// layout and returns the load result.
func loadWithFormat(t *testing.T, layout string) diag.Result {
	t.Helper()
	src := "schema \"tsfmt\"\n\ntype T {\n\tid String primary\n\tat Timestamp[\"" + layout + "\"]\n}\n"
	_, result := schema.LoadString(t.Context(), src, "tsfmt.yammm")
	if result.HasErrors() {
		t.Fatalf("schema declaring %q did not load: %s", layout, result)
	}
	return result
}

// TestTimestampFormat_LossyLayoutWarns covers both halves. A layout that drops
// the zone, the fraction, or both cannot reproduce an instant, and values
// stored through it lose that part permanently. A layout carrying both is the
// positive control: without it the test cannot tell the check from a
// hard-coded warning.
func TestTimestampFormat_LossyLayoutWarns(t *testing.T) {
	t.Parallel()

	lossy := map[string]string{
		"no zone and no fraction": "2006-01-02 15:04:05",
		"no zone":                 "2006-01-02 15:04:05.000",
		"no fraction":             "2006-01-02T15:04:05Z07:00",
		"date only":               "2006-01-02",
		"micros":                  "2006-01-02 15:04:05.999999 -0700",
	}
	for name, layout := range lossy {
		t.Run("lossy/"+name, func(t *testing.T) {
			t.Parallel()
			result := loadWithFormat(t, layout)
			if !result.HasCode(diag.W_TIMESTAMP_LOSSY_FORMAT) {
				t.Errorf("layout %q drew no lossy-format warning: %s", layout, result)
			}
			const hint = "declare Timestamp without a format to store RFC 3339 with nanoseconds, which loses none of these"
			for issue := range result.Issues() {
				if issue.Code() == diag.W_TIMESTAMP_LOSSY_FORMAT && issue.Hint() != hint {
					t.Errorf("layout %q: the warning's hint is %q; want %q", layout, issue.Hint(), hint)
				}
			}
		})
	}

	lossless := map[string]string{
		"RFC 3339 with nanoseconds": "2006-01-02T15:04:05.999999999Z07:00",
		"numeric offset and nanos":  "2006-01-02 15:04:05.999999999 -0700",
	}
	for name, layout := range lossless {
		t.Run("lossless/"+name, func(t *testing.T) {
			t.Parallel()
			result := loadWithFormat(t, layout)
			if result.HasCode(diag.W_TIMESTAMP_LOSSY_FORMAT) {
				t.Errorf("layout %q carries a zone and a fraction but drew the warning: %s", layout, result)
			}
		})
	}
}

// TestTimestampFormat_BareTimestampIsSilent is the second control: the warning
// belongs to a declared layout, and the default RFC 3339 rendering with
// nanoseconds loses no field the warning names.
func TestTimestampFormat_BareTimestampIsSilent(t *testing.T) {
	t.Parallel()
	const src = "schema \"tsfmt\"\n\ntype T {\n\tid String primary\n\tat Timestamp\n}\n"
	_, result := schema.LoadString(t.Context(), src, "tsfmt.yammm")
	if result.HasCode(diag.W_TIMESTAMP_LOSSY_FORMAT) {
		t.Errorf("a bare Timestamp drew the lossy-format warning: %s", result)
	}
}

// TestTimestampFormat_WarningAnchorsOnTheLayout pins where the diagnostic
// lands. The author is told at the point of declaration, so the span has to
// cover the format literal rather than the property or the whole constraint.
func TestTimestampFormat_WarningAnchorsOnTheLayout(t *testing.T) {
	t.Parallel()
	const layout = "2006-01-02 15:04:05"
	src := "schema \"tsfmt\"\n\ntype T {\n\tid String primary\n\tat Timestamp[\"" + layout + "\"]\n}\n"
	_, result := schema.LoadString(t.Context(), src, "tsfmt.yammm")

	var found bool
	for issue := range result.Issues() {
		if issue.Code() != diag.W_TIMESTAMP_LOSSY_FORMAT {
			continue
		}
		found = true
		if issue.Severity() != diag.Warning {
			t.Errorf("severity = %s, want warning", issue.Severity())
		}
		span := issue.Span()
		line := strings.Split(src, "\n")[span.Start.Line-1]
		quoted := line[span.Start.Column-1 : span.End.Column-1]
		if quoted != `"`+layout+`"` {
			t.Errorf("span covers %q, want the format literal", quoted)
		}
	}
	if !found {
		t.Fatalf("no lossy-format warning to inspect: %s", result)
	}
}

// refusedFormatPositions are the three places a source declares a Timestamp
// format, each with %s where the quoted format goes.
var refusedFormatPositions = map[string]string{
	"a property":     "type T {\n\tid String primary\n\tat Timestamp[%s]\n}\n",
	"a datatype":     "type At = Timestamp[%s]\n\ntype T {\n\tid String primary\n\tat At\n}\n",
	"a List element": "type T {\n\tid String primary\n\tat List<Timestamp[%s]>\n}\n",
}

// assertFormatRefused loads layout at each position and asserts one
// E_INVALID_CONSTRAINT error with the message want, on the format literal, and
// no lossy-format warning beside it.
func assertFormatRefused(t *testing.T, layout, want string) {
	t.Helper()
	literal := `"` + layout + `"`
	for position, body := range refusedFormatPositions {
		src := "schema \"tsfmt\"\n\n" + strings.Replace(body, "%s", literal, 1)
		_, result := schema.LoadString(t.Context(), src, "tsfmt.yammm")
		if !result.HasErrors() {
			t.Errorf("%s, %s: the schema loaded: %s", layout, position, result)
			continue
		}
		var faults int
		for issue := range result.Issues() {
			if issue.Code() == diag.W_TIMESTAMP_LOSSY_FORMAT {
				t.Errorf("%s, %s: a lossy-format warning joins the refusal: %s", layout, position, issue.Message())
			}
			if issue.Code() != diag.E_INVALID_CONSTRAINT {
				continue
			}
			faults++
			if issue.Severity() != diag.Error || issue.Message() != want {
				t.Errorf("%s, %s: %s %q; want the error %q", layout, position, issue.Severity(), issue.Message(), want)
			}
			span := issue.Span()
			line := strings.Split(src, "\n")[span.Start.Line-1]
			if quoted := line[span.Start.Column-1 : span.End.Column-1]; quoted != literal {
				t.Errorf("%s, %s: span covers %q, want the format literal", layout, position, quoted)
			}
		}
		if faults != 1 {
			t.Errorf("%s, %s: %d E_INVALID_CONSTRAINT issues, want 1: %s", layout, position, faults, result)
		}
	}
}

// A layout that holds a zone abbreviation is refused, with a numeric offset
// beside the abbreviation or with none: an abbreviation names no one offset,
// and package time reads the text UTC there as UTC whatever offset stands
// beside it.
func TestTimestampFormat_AZoneAbbreviationIsRefused(t *testing.T) {
	t.Parallel()
	for _, layout := range []string{
		"2006-01-02 15:04:05 MST", "15:04 MST", "MST", time.RFC1123, time.RFC850, time.RFC822, time.UnixDate,
		"2006-01-02 15:04:05 -0700 MST", "MST Z07:00", "15:04 -07 MST", "15:04 MST Z07",
		"2006-01-02 15:04:05.999999999 -0700 MST",
	} {
		assertFormatRefused(t, layout, "timestamp format \""+layout+"\" holds a zone abbreviation, which names no one offset; state the zone as a numeric offset such as Z07:00")
	}
}

// Package time reads the text UTC at an abbreviation as UTC and drops the
// numeric offset beside it, so a layout that holds both stores one instant
// and reads back another. The refusal of such a layout rests on that.
func TestTimestampFormat_AnAbbreviationBesideAnOffsetReadsBackAnotherInstant(t *testing.T) {
	t.Parallel()
	const layout = "2006-01-02 15:04:05 -0700 MST"
	assertFormatRefused(t, layout, "timestamp format \""+layout+"\" holds a zone abbreviation, which names no one offset; state the zone as a numeric offset such as Z07:00")
	stored := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.FixedZone("UTC", 60*60))
	back, err := time.ParseInLocation(layout, stored.Format(layout), time.UTC)
	if err != nil {
		t.Fatalf("package time does not read %q back: %v", stored.Format(layout), err)
	}
	if back.Equal(stored) {
		t.Errorf("%q reads back as the instant it was written from; the refusal rests on package time reading another", stored.Format(layout))
	}
}

// A layout that does not read back the text it writes is refused, and the
// message shows the instant and the text, whole when it runs past the 32 bytes
// a value's text is cut to. A layout that reads one probe instant and not the
// other is refused by the one it does not read.
func TestTimestampFormat_ALayoutThatDoesNotReadItsOwnTextIsRefused(t *testing.T) {
	t.Parallel()
	const first, second = "2026-08-19T13:34:56.789123456+05:30", "1950-11-23T00:07:08.1-09:30"
	for _, row := range []struct{ layout, instant, text string }{
		{"2006-12-02 15:04:05", first, "2026-819-19 13:34:56"},
		{"2006-12 15:04:05", first, "2026-819 13:34:56"},
		{"12", first, "819"},
		{"2006120215", first, "20268191913"},
		{"34", first, "134"},
		{"52", second, "823"},
		{"2006-01-02T15:04:05.0000000000Z07:00", first, "2026-08-19T13:34:56.789123456+05:30"},
	} {
		assertFormatRefused(t, row.layout, "timestamp format \""+row.layout+"\" does not read back the text it writes: it writes "+row.instant+" as \""+row.text+"\"")
	}
}

// An empty format is refused where it is written, as an empty Enum value is:
// the constraint with no format is written with no brackets.
func TestTimestampFormat_AnEmptyFormatIsRefused(t *testing.T) {
	t.Parallel()
	assertFormatRefused(t, "", "timestamp format cannot be empty; declare Timestamp with no format to store RFC 3339 text")
}

// A refused format is still the constraint the property declares, so a child
// that redeclares a parent's Timestamp under one draws the conflict beside the
// refusal.
func TestTimestampFormat_ARefusedFormatIsStillTheDeclaredConstraint(t *testing.T) {
	t.Parallel()
	src := "schema \"tsfmt\"\n\nabstract type P {\n\tid String primary\n\tat Timestamp\n}\n\ntype C extends P {\n\tat Timestamp[\"15:04 MST\"]\n}\n"
	_, result := schema.LoadString(t.Context(), src, "tsfmt.yammm")
	for _, code := range []diag.Code{diag.E_INVALID_CONSTRAINT, diag.E_PROPERTY_CONFLICT} {
		if !result.HasCode(code) {
			t.Errorf("no %s: %s", code, result)
		}
	}
}

// The controls of the refusals: a layout that holds a numeric offset or no zone
// loads, and so does one that holds no layout element. The letters MST after PM
// are no abbreviation, as package time reads a layout.
func TestTimestampFormat_ALayoutWithNoZoneAbbreviationLoads(t *testing.T) {
	t.Parallel()
	for _, layout := range []string{
		time.RFC1123Z, time.RFC3339Nano, "2006-01-02 15:04:05", "UTC 2006", "plain text",
		"-07", "3:04PMST", "PMST", "1 2",
	} {
		if result := loadWithFormat(t, layout); result.HasCode(diag.E_INVALID_CONSTRAINT) {
			t.Errorf("layout %q is refused: %s", layout, result)
		}
	}
}
