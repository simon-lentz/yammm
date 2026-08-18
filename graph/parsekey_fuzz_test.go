package graph_test

import (
	"testing"

	"github.com/simon-lentz/yammm/graph"
)

// FuzzParseKey drives the parser with arbitrary input, asserting the two
// properties that hold whatever comes in: a parse never panics, and whatever it
// accepts re-renders to a string it accepts identically. The second is the
// round-trip law from the side that matters — a key read out of a document and
// written back must be the same key.
func FuzzParseKey(f *testing.F) {
	// Keys FormatKey produces, so the seeds start on the law's own domain.
	for _, values := range [][]any{
		{"ABC123"},
		{"us", int64(12345)},
		{int64(9007199254740993)},
		{3.25},
		{true, false},
		{nil},
		{"quote\"brace{bracket["},
		{"café 🙂"},
		{},
	} {
		f.Add(graph.FormatKey(values...))
	}
	// Shapes the parser must reject rather than crash on.
	for _, s := range []string{
		"", "[", "]", "null", "{}", `"x"`, "5",
		`["a"] trailing`, `[["nested"]]`, `[{"o":1}]`,
		`[1e999]`, `[99999999999999999999]`, "[\x00]", "[\xff]",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		values, err := graph.ParseKey(s)
		if err != nil {
			return
		}
		rendered := graph.FormatKey(values...)
		reparsed, err := graph.ParseKey(rendered)
		if err != nil {
			t.Fatalf("graph.ParseKey(%q) accepted, but its rendering %q did not: %v", s, rendered, err)
		}
		if graph.FormatKey(reparsed...) != rendered {
			t.Fatalf("graph.FormatKey is not stable across a second parse of %q: %q then %q",
				s, rendered, graph.FormatKey(reparsed...))
		}
	})
}
