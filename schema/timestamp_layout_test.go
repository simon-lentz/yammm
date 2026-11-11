package schema_test

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// lossyFormatMessages declares each layout on its own property and returns the
// W_TIMESTAMP_LOSSY_FORMAT message drawn by each layout that drew one. It loads
// the layouts a batch at a time, since a load stores a bounded count of issues.
func lossyFormatMessages(t *testing.T, layouts []string) map[string]string {
	t.Helper()
	const batch = 64
	got := make(map[string]string)
	for chunk := range slices.Chunk(layouts, batch) {
		var src strings.Builder
		src.WriteString("schema \"tsfmt\"\n\ntype T {\n\tid String primary\n")
		for i, layout := range chunk {
			fmt.Fprintf(&src, "\tp%d Timestamp[%q]\n", i, layout)
		}
		src.WriteString("}\n")
		_, result := schema.LoadString(t.Context(), src.String(), "tsfmt.yammm")
		if result.HasErrors() {
			t.Fatalf("the schema did not load: %s", result)
		}
		for issue := range result.Issues() {
			if issue.Code() != diag.W_TIMESTAMP_LOSSY_FORMAT {
				continue
			}
			quoted, err := strconv.QuotedPrefix(strings.TrimPrefix(issue.Message(), "timestamp format "))
			if err != nil {
				t.Fatalf("the message %q names no layout", issue.Message())
			}
			layout, _ := strconv.Unquote(quoted)
			if _, twice := got[layout]; twice {
				t.Fatalf("layout %q drew two warnings", layout)
			}
			got[layout] = issue.Message()
		}
	}
	return got
}

func lossMessage(layout string, lost ...string) string {
	list := lost[0]
	if len(lost) > 1 {
		list = strings.Join(lost[:len(lost)-1], ", ") + " and " + lost[len(lost)-1]
	}
	return fmt.Sprintf("timestamp format %q cannot represent an instant: a value stored through it loses %s", layout, list)
}

// A layout that keeps one instant whole can still lose a field of another, and
// each message names the fields the layout loses. A field one instant keeps and
// the other loses is lost: the last five layouts each read one field, the
// year, the month, the day, the minute and the second, back from one instant alone.
func TestTimestampFormat_NamesEveryFieldTheLayoutLoses(t *testing.T) {
	t.Parallel()
	everyField := []string{"the year", "the month", "the day", "the hour", "the minute", "the second", "the fractional second", "the UTC offset"}
	rows := map[string]string{
		"320062":                                  lossMessage("320062", everyField...),
		"x3__2":                                   lossMessage("x3__2", everyField...),
		"32":                                      lossMessage("32", everyField...),
		"3064":                                    lossMessage("3064", everyField...),
		"3065":                                    lossMessage("3065", everyField...),
		"2006-01-02T15:04:05.000Z07:00":           lossMessage("2006-01-02T15:04:05.000Z07:00", "the fractional second past 3 digits"),
		"2006-01-02T15:04:05.000000Z07:00":        lossMessage("2006-01-02T15:04:05.000000Z07:00", "the fractional second past 6 digits"),
		"2006-01-02T15:04:05.0Z07:00":             lossMessage("2006-01-02T15:04:05.0Z07:00", "the fractional second past 1 digit"),
		"2006-01-02 03:04:05.000000000Z07:00":     lossMessage("2006-01-02 03:04:05.000000000Z07:00", "whether the hour is before or after noon"),
		"06-01-02T15:04:05.000000000Z07:00":       lossMessage("06-01-02T15:04:05.000000000Z07:00", "the century of the year"),
		"2006-01-02T15:04:05.000000000Z07":        lossMessage("2006-01-02T15:04:05.000000000Z07", "the minutes of the UTC offset"),
		"15:04:05.000000000Z07:00":                lossMessage("15:04:05.000000000Z07:00", "the year", "the month", "the day"),
		"2006-01-02T15:04:05Z07:00":               lossMessage("2006-01-02T15:04:05Z07:00", "the fractional second"),
		"2006-01-02 15:04:05.999999999":           lossMessage("2006-01-02 15:04:05.999999999", "the UTC offset"),
		"2006-01-02":                              lossMessage("2006-01-02", "the hour", "the minute", "the second", "the fractional second", "the UTC offset"),
		"plain text":                              lossMessage("plain text", "the year", "the month", "the day", "the hour", "the minute", "the second", "the fractional second", "the UTC offset"),
		"2006-01-02T15:04:05.999999999Z07:00":     "",
		"2006-01-02 03:04:05.000000000 PM -07:00": "",
		"Jan _2 2006 15:04:05.000000000 -0700":    "",
	}
	layouts := make([]string, 0, len(rows))
	for layout := range rows {
		layouts = append(layouts, layout)
	}
	got := lossyFormatMessages(t, layouts)
	for layout, want := range rows {
		if got[layout] != want {
			t.Errorf("layout %q:\n got  %q\n want %q", layout, got[layout], want)
		}
	}
}

// layoutPart is one field's directive and what a value written through it
// loses, read from the directive alone.
type layoutPart struct {
	directive string
	lost      []string
}

// The warning and the layout's directives agree on what a layout loses: for
// every layout built from Go's year, month, day, hour, minute, second,
// fraction and offset directives, the schema loads, the message names what the
// directives drop, and a layout that drops nothing draws none.
func TestTimestampFormat_WarningAgreesWithTheLayoutsDirectives(t *testing.T) {
	t.Parallel()
	years := []layoutPart{{"2006", nil}, {"06", []string{"the century of the year"}}, {"", []string{"the year"}}}
	months := []layoutPart{{"01", nil}, {"Jan", nil}, {"January", nil}, {"", []string{"the month"}}}
	days := []layoutPart{{"02", nil}, {"_2", nil}, {"", []string{"the day"}}}
	hours := []layoutPart{{"15", nil}, {"03PM", nil}, {"03", []string{"whether the hour is before or after noon"}}, {"", []string{"the hour"}}}
	minutes := []layoutPart{{"04", nil}, {"", []string{"the minute"}}}
	seconds := []layoutPart{
		{"05.000000000", nil},
		{"05.999999999", nil},
		{"05.000000", []string{"the fractional second past 6 digits"}},
		{"05.999", []string{"the fractional second past 3 digits"}},
		{"05.0", []string{"the fractional second past 1 digit"}},
		{"05", []string{"the fractional second"}},
		{"", []string{"the second", "the fractional second"}},
	}
	offsets := []layoutPart{
		{"Z07:00", nil},
		{"-0700", nil},
		{"-07:00", nil},
		{"Z0700", nil},
		{"-07:00:00", nil},
		{"Z07", []string{"the minutes of the UTC offset"}},
		{"", []string{"the UTC offset"}},
	}
	want := make(map[string]string)
	var layouts []string
	for _, y := range years {
		for _, mo := range months {
			for _, d := range days {
				for _, h := range hours {
					for _, mi := range minutes {
						for _, s := range seconds {
							for _, z := range offsets {
								var directives, lost []string
								for _, p := range []layoutPart{y, mo, d, h, mi, s, z} {
									if p.directive != "" {
										directives = append(directives, p.directive)
									}
									lost = append(lost, p.lost...)
								}
								if len(directives) == 0 {
									continue
								}
								layout := strings.Join(directives, " ")
								layouts = append(layouts, layout)
								if len(lost) > 0 {
									want[layout] = lossMessage(layout, lost...)
								}
							}
						}
					}
				}
			}
		}
	}
	got := lossyFormatMessages(t, layouts)
	wrong := 0
	for _, layout := range layouts {
		if got[layout] != want[layout] {
			wrong++
			if wrong <= 10 {
				t.Errorf("layout %q:\n got  %q\n want %q", layout, got[layout], want[layout])
			}
		}
	}
	if wrong > 0 {
		t.Errorf("%d of %d layouts disagree", wrong, len(layouts))
	}
}
