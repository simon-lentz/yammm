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
		"an abbreviation beside a numeric offset, no fraction": "2006-01-02 15:04:05 -0700 MST",
	}
	for name, layout := range lossy {
		t.Run("lossy/"+name, func(t *testing.T) {
			t.Parallel()
			result := loadWithFormat(t, layout)
			if !result.HasCode(diag.W_TIMESTAMP_LOSSY_FORMAT) {
				t.Errorf("layout %q drew no lossy-format warning: %s", layout, result)
			}
		})
	}

	lossless := map[string]string{
		"RFC 3339 with nanoseconds":                               "2006-01-02T15:04:05.999999999Z07:00",
		"numeric offset and micros":                               "2006-01-02 15:04:05.999999 -0700",
		"an abbreviation beside a numeric offset and nanoseconds": "2006-01-02 15:04:05.999999999 -0700 MST",
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
// nanoseconds round-trips exactly.
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

// A layout that holds a zone abbreviation and no numeric offset is refused: an
// abbreviation names no one offset, so text in the layout names no one instant.
// The one error lands on the format literal, at a property, a datatype and a
// List element, and no lossy-format warning joins it.
func TestTimestampFormat_AZoneAbbreviationAloneIsRefused(t *testing.T) {
	t.Parallel()
	positions := map[string]string{
		"a property":     "type T {\n\tid String primary\n\tat Timestamp[%s]\n}\n",
		"a datatype":     "type At = Timestamp[%s]\n\ntype T {\n\tid String primary\n\tat At\n}\n",
		"a List element": "type T {\n\tid String primary\n\tat List<Timestamp[%s]>\n}\n",
	}
	for _, layout := range []string{"2006-01-02 15:04:05 MST", "15:04 MST", "MST", time.RFC1123, time.RFC850, time.RFC822, time.UnixDate} {
		literal := `"` + layout + `"`
		want := "timestamp format " + literal + " holds a zone abbreviation and no numeric offset"
		for position, body := range positions {
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
				if issue.Severity() != diag.Error || !strings.HasPrefix(issue.Message(), want) {
					t.Errorf("%s, %s: %s %q; want an error that starts %q", layout, position, issue.Severity(), issue.Message(), want)
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

// The controls of the refusal: a layout that holds a numeric offset, beside an
// abbreviation or alone, and a layout that holds no zone, each load. An offset in
// whole hours beside the abbreviation is an offset, and the letters MST after PM
// are no abbreviation, as package time reads a layout.
func TestTimestampFormat_AZoneAbbreviationBesideAnOffsetLoads(t *testing.T) {
	t.Parallel()
	for _, layout := range []string{
		"2006-01-02 15:04:05 -0700 MST", "MST Z07:00", time.RFC1123Z, time.RFC3339Nano,
		"2006-01-02 15:04:05", "UTC 2006", "plain text",
		"15:04 -07 MST", "15:04 MST Z07", "3:04PMST", "PMST",
	} {
		if result := loadWithFormat(t, layout); result.HasCode(diag.E_INVALID_CONSTRAINT) {
			t.Errorf("layout %q is refused: %s", layout, result)
		}
	}
}
