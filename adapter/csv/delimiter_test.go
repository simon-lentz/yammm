package csv

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// A value holding a comma is the case that tells a tab-split file from a
// comma-split one: read with the wrong delimiter it lands in two columns.
const tabRecord = "e1\tSmith, Ann\t3\n"

func TestParseTyped_WithDelimiterSplitsOnTab(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")

	raws, result := New(WithDelimiter('\t')).ParseTyped(context.Background(),
		location.MustNewSourceID("test://data.tsv"), "Entity",
		strings.NewReader("id\tname\tcount\n"+tabRecord), st)

	if !result.OK() {
		t.Fatalf("parse: %s", result)
	}
	if len(raws) != 1 {
		t.Fatalf("got %d instances, want 1", len(raws))
	}
	want := map[string]any{"id": "e1", "name": "Smith, Ann", "count": int64(3)}
	if got := raws[0].Properties; !reflect.DeepEqual(got, want) {
		t.Errorf("properties = %#v, want %#v", got, want)
	}
}

// ParseWithTypeColumn builds its own reader, so the option has to reach it
// separately from ParseTyped's.
func TestParseWithTypeColumn_WithDelimiterSplitsOnTab(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")

	parsed, result := New(WithTypeColumn("kind"), WithDelimiter('\t')).ParseWithTypeColumn(context.Background(),
		location.MustNewSourceID("test://data.tsv"),
		strings.NewReader("kind\tid\tname\tcount\nEntity\t"+tabRecord),
		func(name string) *schema.Type {
			st, _ := s.Type(name)
			return st
		})

	if !result.OK() {
		t.Fatalf("parse: %s", result)
	}
	if len(parsed["Entity"]) != 1 {
		t.Fatalf("got %d Entity instances, want 1: %v", len(parsed["Entity"]), parsed)
	}
	want := map[string]any{"id": "e1", "name": "Smith, Ann", "count": int64(3)}
	if got := parsed["Entity"][0].Properties; !reflect.DeepEqual(got, want) {
		t.Errorf("properties = %#v, want %#v", got, want)
	}
}

// The writer takes the same delimiter the parser does, so one adapter writes a
// file it reads back. Under a tab delimiter a comma is an ordinary character
// and the cell holding one is written bare.
func TestMarshalSnapshot_WithDelimiterWritesTabsAndReadsBack(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	snap := buildSnapshot(t, s, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "Smith, Ann", "tags": []any{"a", "b"}}},
	})
	a := New(WithDelimiter('\t'))

	out, err := a.MarshalSnapshot(context.Background(), snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text := string(out["Entity"])
	header, row, _ := strings.Cut(text, "\n")
	if strings.Contains(header, ",") || !strings.Contains(header, "\t") {
		t.Errorf("header %q is not tab-delimited", header)
	}
	if !strings.Contains(row, "\tSmith, Ann\t") {
		t.Errorf("row %q does not carry the comma-bearing cell bare between tabs", row)
	}
	if !strings.Contains(row, "\ta|b\t") {
		t.Errorf("row %q does not join the list on the list separator, which the delimiter leaves alone", row)
	}

	raws, result := a.ParseTyped(context.Background(),
		location.MustNewSourceID("test://out.tsv"), "Entity", strings.NewReader(text), st)
	if !result.OK() {
		t.Fatalf("re-parse: %s", result)
	}
	if len(raws) != 1 {
		t.Fatalf("got %d instances back, want 1", len(raws))
	}
	props := raws[0].Properties
	if props["name"] != "Smith, Ann" || !reflect.DeepEqual(props["tags"], []any{"a", "b"}) {
		t.Errorf("read back %#v", props)
	}
}

// WriteSnapshot builds its own csv.Writer per type, so the option has to reach
// it separately from MarshalSnapshot's.
func TestWriteSnapshot_WithDelimiterWritesTabs(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	snap := buildSnapshot(t, s, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "Smith, Ann"}},
	})
	var buf bytes.Buffer

	err := New(WithDelimiter('\t')).WriteSnapshot(context.Background(), func(string) (io.Writer, error) {
		return &buf, nil
	}, snap)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	header, row, _ := strings.Cut(buf.String(), "\n")
	if strings.Contains(header, ",") || !strings.Contains(header, "\t") {
		t.Errorf("header %q is not tab-delimited", header)
	}
	if !strings.Contains(row, "\tSmith, Ann\t") {
		t.Errorf("row %q does not carry the comma-bearing cell bare between tabs", row)
	}
}

// encoding/csv refuses some runes as a delimiter. The option cannot return an
// error, so the refusal must surface wherever a header is read or written: an
// Error diagnostic from either parse entry point and an error from either write
// entry point, never a panic and never a silent default.
func TestWithDelimiter_RefusedDelimiterIsReportedAtUse(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	snap := buildSnapshot(t, s, map[string][]map[string]any{"Entity": {{"id": "e1", "name": "Ann"}}})
	const refusal = "invalid field or comment delimiter"
	id := location.MustNewSourceID("test://data.csv")

	requireRefusal := func(t *testing.T, result diag.Result) {
		t.Helper()
		for issue := range result.Issues() {
			if issue.Code() == E_CSV_CONFIG && strings.Contains(issue.Message(), refusal) {
				if issue.Severity() != diag.Error {
					t.Errorf("the refusal is %v, want Error", issue.Severity())
				}
				return
			}
		}
		t.Errorf("no %s diagnostic carrying the refusal: %s", E_CSV_CONFIG, result)
	}
	requireWriteRefusal := func(t *testing.T, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), refusal) {
			t.Errorf("write error = %v, want one carrying the refusal", err)
		}
	}

	for _, c := range []struct {
		name  string
		delim rune
	}{
		{"NUL", 0},
		{"quote", '"'},
		{"carriage return", '\r'},
		{"line feed", '\n'},
		{"replacement character", utf8.RuneError},
		{"surrogate half", 0xD800},
		{"beyond Unicode", utf8.MaxRune + 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			a := New(WithDelimiter(c.delim), WithTypeColumn("kind"))

			_, result := a.ParseTyped(context.Background(), id, "Entity", strings.NewReader("id\ne1\n"), st)
			requireRefusal(t, result)
			_, result = a.ParseWithTypeColumn(context.Background(), id, strings.NewReader("kind\nEntity\n"),
				func(string) *schema.Type { return st })
			requireRefusal(t, result)

			_, err := a.MarshalSnapshot(context.Background(), snap)
			requireWriteRefusal(t, err)
			err = a.WriteSnapshot(context.Background(), func(string) (io.Writer, error) {
				return io.Discard, nil
			}, snap)
			requireWriteRefusal(t, err)
		})
	}
}
