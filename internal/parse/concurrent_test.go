package parse

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
)

// TestParse_ConcurrentCallsShareTheParsersSafely exercises the shared parser
// values that replace per-parse construction. It means nothing without -race,
// which is why the package gate runs with it.
func TestParse_ConcurrentCallsShareTheParsersSafely(t *testing.T) {
	sources := []string{
		smokeSource,
		"schema \"a\"\ntype A {\n\tid String primary\n}\n",
		"schema \"b\"\ntype B {\n\tid String primary\n\t! \"m\" id != nil\n}\n",
		"schema \"c\"\ntype C {\n\tbroken @\n}\n",
		"not a schema at all",
		"",
	}

	var mu sync.Mutex
	first := make(map[int]string, len(sources))
	yammmtest.RunConcurrent(8, 25, func() {
		for i, src := range sources {
			file, issues := Parse([]byte(src), location.NewSourceID("c.yammm"))
			got := render(file, issues)
			mu.Lock()
			if want, seen := first[i]; seen && want != got {
				t.Errorf("source %d parsed differently under concurrency:\n got %s\nwant %s", i, got, want)
			} else if !seen {
				first[i] = got
			}
			mu.Unlock()
		}
	})
}

// TestLex_ConcurrentCallsShareTheDefinitionSafely covers the other entry point
// onto the same shared state.
func TestLex_ConcurrentCallsShareTheDefinitionSafely(t *testing.T) {
	want := len(Lex(smokeSource))
	var mu sync.Mutex
	yammmtest.RunConcurrent(8, 25, func() {
		got := len(Lex(smokeSource))
		mu.Lock()
		defer mu.Unlock()
		if got != want {
			t.Errorf("token count = %d, want %d", got, want)
		}
	})
}

// TestRender_DiscriminatesOnSpans keeps the fingerprint above from going
// vacuous. The two sources below agree on every name and on the issue count
// and differ only in where things sit, which is the shape a shared derivation
// corrupts into.
func TestRender_DiscriminatesOnSpans(t *testing.T) {
	a := "schema \"a\"\ntype A {\n\tid String primary\n}\n"
	b := "schema \"a\"\ntype A {\n\n\tid String primary\n}\n"
	fa, ia := Parse([]byte(a), location.NewSourceID("c.yammm"))
	fb, ib := Parse([]byte(b), location.NewSourceID("c.yammm"))
	if len(ia) != 0 || len(ib) != 0 {
		t.Fatalf("fixtures must parse clean: %v / %v", ia, ib)
	}
	if render(fa, ia) == render(fb, ib) {
		t.Error("fingerprint is span-blind: two parses differing only in position render alike")
	}
}

// render reduces a parse to a comparable summary, so a concurrency defect
// shows up as a difference rather than as a data race alone. Spans and
// messages are included because a shared derivation corrupts those while
// leaving names and issue counts intact.
func render(f *File, issues []diag.Issue) string {
	var out strings.Builder
	writeSpan := func(s location.Span) {
		fmt.Fprintf(&out, "@%d-%d/%d:%d", s.Start.Byte, s.End.Byte, s.Start.Line, s.Start.Column)
	}
	out.WriteString(f.Name)
	writeSpan(f.NameSpan)
	out.WriteByte('|')
	for _, ty := range f.Types {
		out.WriteString(ty.Name)
		writeSpan(ty.Span)
		out.WriteByte(':')
		for _, p := range ty.Properties {
			out.WriteString(p.Name)
			writeSpan(p.Span)
			out.WriteByte(',')
		}
		out.WriteByte(';')
	}
	for _, dt := range f.DataTypes {
		out.WriteString(dt.Name)
		writeSpan(dt.Span)
		out.WriteByte('.')
	}
	out.WriteByte('|')
	for _, iss := range issues {
		out.WriteString(iss.Code().String())
		out.WriteByte('=')
		out.WriteString(iss.Message())
		writeSpan(iss.Span())
		out.WriteByte(';')
	}
	return out.String()
}
