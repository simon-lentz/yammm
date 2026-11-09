package format

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// formatText is [Source] over a string, the form this package's tests compare.
func formatText(text string) (string, error) {
	out, err := Source([]byte(text))
	return string(out), err
}

// TestSource_ReturnsAFreshSliceAndLeavesItsInput pins go/format.Source's
// contract: the result is the canonical text in a slice of its own, whether or
// not the input was already canonical, and the input's bytes are not written.
func TestSource_ReturnsAFreshSliceAndLeavesItsInput(t *testing.T) {
	t.Parallel()
	const canonical = "schema \"s\"\n\ntype T {\n\tname String\n}\n"
	for _, tt := range []struct {
		name string
		in   string
	}{
		{"unformatted", "schema \"s\"\ntype T {\n    name   String   \n}"},
		{"canonical", canonical},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := []byte(tt.in)
			out, err := Source(in)
			if err != nil {
				t.Fatalf("Source: %v", err)
			}
			if string(out) != canonical {
				t.Fatalf("Source = %q, want %q", out, canonical)
			}
			if string(in) != tt.in {
				t.Fatalf("Source wrote its input: %q, want %q", in, tt.in)
			}
			for i := range out {
				out[i] = 'x'
			}
			if string(in) != tt.in {
				t.Errorf("Source's result shares its input's bytes: writing it made the input %q", in)
			}
		})
	}
}

// TestSource_ReturnsNoBytesWithAnError pins that a source that does not parse
// yields a nil result, so a caller that writes what it gets writes nothing.
func TestSource_ReturnsNoBytesWithAnError(t *testing.T) {
	t.Parallel()
	out, err := Source([]byte("schema \"s\"\ntype T {\n"))
	if _, ok := errors.AsType[*SyntaxError](err); !ok {
		t.Fatalf("Source error = %v, want a *SyntaxError", err)
	}
	if out != nil {
		t.Errorf("Source = %q with an error, want nil", out)
	}
}

// TestSource_IsTheStringFormatter pins that Source gives the text formatter's
// answer, its error included: an empty source, a marked CRLF one, and syntax
// errors behind a byte order mark and a CRLF, whose positions count the text the
// formatter was given.
func TestSource_IsTheStringFormatter(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"",
		"schema \"s\"\n",
		"\ufeffschema \"s\"\r\ntype T {\r\n  a Integer\r\n}\r\n",
		"\ufeffschema \"s\" x\n",
		"schema \"s\"\r\ntype T {\r\n",
	} {
		want, wantErr := tokenStream(src, rewrite)
		got, err := Source([]byte(src))
		if !bytes.Equal(got, []byte(want)) || !reflect.DeepEqual(err, wantErr) {
			t.Errorf("Source(%q) = %q, %#v; the text formatter gives %q, %#v", src, got, err, want, wantErr)
		}
		if wantErr != nil && got != nil {
			t.Errorf("Source(%q) = %q with an error, want nil", src, got)
		}
	}
}

// TestSource_WrapsPastTheDocumentedWidth pins the width docs/API.md states for
// line wrapping: a line wraps past 100 display cells, where the leading tab
// counts 4 and an East Asian wide or fullwidth rune counts 2.
func TestSource_WrapsPastTheDocumentedWidth(t *testing.T) {
	t.Parallel()
	// The property line is "\tstatus Enum["a", "<value>"]": 24 cells around the value.
	line := func(value string) string { return "\tstatus Enum[\"a\", \"" + value + "\"]" }
	for _, tt := range []struct {
		name  string
		value string
		wraps bool
	}{
		{"100 cells", strings.Repeat("x", 76), false},
		{"101 cells", strings.Repeat("x", 77), true},
		{"100 cells with a wide rune", "日" + strings.Repeat("x", 74), false},
		{"101 cells with a wide rune", "日" + strings.Repeat("x", 75), true},
		{"100 cells with a fullwidth rune", "Ａ" + strings.Repeat("x", 74), false},
		{"101 cells with a fullwidth rune", "Ａ" + strings.Repeat("x", 75), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := "schema \"s\"\n\ntype T {\n" + line(tt.value) + "\n}\n"
			got, err := formatText(src)
			if err != nil {
				t.Fatalf("Source: %v", err)
			}
			if wrapped := !strings.Contains(got, line(tt.value)+"\n"); wrapped != tt.wraps {
				t.Errorf("wrapped = %v, want %v:\n%s", wrapped, tt.wraps, got)
			}
		})
	}
}
