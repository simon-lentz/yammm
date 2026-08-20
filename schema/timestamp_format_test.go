package schema_test

import (
	"strings"
	"testing"

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
		"RFC 3339 with nanoseconds": "2006-01-02T15:04:05.999999999Z07:00",
		"numeric offset and micros": "2006-01-02 15:04:05.999999 -0700",
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
