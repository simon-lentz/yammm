package mdsnippet

import (
	"context"
	"strings"

	"github.com/simon-lentz/yammm/schema"
)

// TB is the slice of [testing.TB] these gates use, which *testing.T satisfies.
//
// It is an interface of this package's own rather than testing.TB itself
// because testing.TB is deliberately unimplementable outside the standard
// library, and a gate whose failure path is unreachable from a test is a gate
// that can silently stop failing. This one is stubbed in this package's test to
// prove each Assert actually reports when its condition is violated.
type TB interface {
	Helper()
	Errorf(format string, args ...any)
}

// Each Assert function reports and continues (Errorf), per the repo's test
// helper conventions, and returns how many blocks it actually checked. The
// count is returned rather than asserted internally because a corpus may
// legitimately contain none of a given genre; where a corpus is REQUIRED to
// exercise a gate, the caller asserts the count, keeping that expectation
// visible in the test body.

// AssertParses checks that every block claiming to be real yammm code parses.
// [TagInvalid] blocks are excluded: failing to parse is one of the ways they
// are allowed to be wrong.
func AssertParses(t TB, blocks []Block) (checked int) {
	t.Helper()
	for _, b := range blocks {
		if b.Tag == TagInvalid {
			continue
		}
		if b.SkipReason() != "" {
			continue
		}
		wrapped := b.WrapForParsing()
		if errs := ParseErrors(wrapped); len(errs) > 0 {
			t.Errorf("%s failed to parse:\n  source:\n%s  errors:\n    %s",
				b.Ref(), Indent(wrapped), strings.Join(errs, "\n    "))
		}
		checked++
	}
	return checked
}

// AssertLoads checks that every [TagSchema] block compiles clean, not merely
// parses.
func AssertLoads(t TB, blocks []Block) (checked int) {
	t.Helper()
	for _, b := range blocks {
		if b.Tag != TagSchema {
			continue
		}
		wrapped := b.WrapForLoading()
		_, result := schema.LoadString(context.Background(), wrapped, "snippet.yammm")
		if err := result.Err(); err != nil {
			t.Errorf("%s does not load:\n%v\n  source:\n%s", b.Ref(), err, Indent(wrapped))
		}
		checked++
	}
	return checked
}

// AssertInvalid checks that every [TagInvalid] block genuinely fails to load —
// by syntax error or by semantic one, either is a legitimate way to be wrong.
//
// This is the direction a documentation corpus cannot self-correct. An example
// marked WRONG that a loosened loader has quietly made valid still reads as a
// warning, so the document goes on teaching a lesson that stopped being true,
// and no conventional test notices because nothing was ever asserted about it.
func AssertInvalid(t TB, blocks []Block) (checked int) {
	t.Helper()
	for _, b := range blocks {
		if b.Tag != TagInvalid {
			continue
		}
		wrapped := b.WrapForLoading()
		_, result := schema.LoadString(context.Background(), wrapped, "snippet.yammm")
		if result.Err() == nil {
			t.Errorf("%s is tagged %s but loads CLEAN — either the example is no "+
				"longer wrong (retag it, and check the prose around it still holds) "+
				"or the loader stopped rejecting it:\n  source:\n%s",
				b.Ref(), TagInvalid, Indent(wrapped))
		}
		checked++
	}
	return checked
}

// AssertDocumentedDiagnostics checks that a block naming a diagnostic code in
// its own comment actually produces it, whatever its fence tag.
//
// This is the direction that rots silently: prose saying "this is reported as
// E_INVALID_ANNOTATION" stays readable and convincing long after the loader
// stopped reporting it, or started reporting something else.
func AssertDocumentedDiagnostics(t TB, blocks []Block) (checked int) {
	t.Helper()
	for _, b := range blocks {
		if b.SkipReason() != "" {
			continue
		}
		want, ok := b.DiagnosticCode()
		if !ok {
			continue
		}
		checked++

		// Matched by STRING rather than by constructing a diag.Code: NewCode
		// panics on a name already registered, which every real code is.
		_, result := schema.LoadString(context.Background(), b.WrapForLoading(), "snippet.yammm")
		var got []string
		found := false
		for iss := range result.Issues() {
			code := iss.Code().String()
			got = append(got, code)
			if code == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s documents %s but the loader reported [%s]",
				b.Ref(), want, strings.Join(got, " "))
		}
	}
	return checked
}
