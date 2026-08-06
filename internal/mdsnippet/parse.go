package mdsnippet

import (
	"fmt"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/location"
)

// ParseErrors runs the parser over source and returns the syntax errors it
// reported, as positioned message strings. An empty slice means source parsed
// cleanly.
//
// The filter is syntax-only. A parse also reports constraint findings — an
// inverted bound, an enum with one value — and an AssertParses block is
// allowed to be semantically invalid so long as it parses.
func ParseErrors(source string) []string {
	_, issues := parse.Parse([]byte(source), location.SourceID{})

	var out []string
	for _, iss := range issues {
		if iss.Code().Category() != diag.CategorySyntax {
			continue
		}
		span := iss.Span()
		out = append(out, fmt.Sprintf("line %d:%d %s",
			span.Start.Line, span.Start.Column-1, iss.Message()))
	}
	return out
}
