package format

import (
	"os"
	"strings"
	"testing"
)

// Formatting fails closed on syntax and only on syntax. Both directions are
// pinned here because the suite covered only one of them: a source that will
// not parse must not be rewritten, and a source that parses but is
// semantically invalid must still format.
//
// The second direction is the one that can regress silently. The parse result
// carries constraint findings as well as syntax errors, so a filter that
// rejects any diagnostic would refuse to format schemas the released
// formatter has always accepted, and no golden would notice.

const failClosedFixture = "testdata/golden/semantically_invalid.yammm"

func TestTokenStream_FormatsSemanticallyInvalidSource(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(failClosedFixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	got, err := TokenStream(string(src))
	if err != nil {
		t.Fatalf("a syntactically clean source must format, got: %v", err)
	}

	golden, err := os.ReadFile(failClosedFixture + ".golden")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got != string(golden) {
		t.Errorf("formatted output differs from the golden:\n got %q\nwant %q", got, string(golden))
	}

	again, err := TokenStream(got)
	if err != nil {
		t.Fatalf("re-formatting the output must succeed, got: %v", err)
	}
	if again != got {
		t.Error("formatting is not idempotent on this fixture")
	}
}

// The fixture earns its place only while it carries no syntax error and at
// least one semantic one. A future edit that makes it merely well-formed would
// leave the test above passing for the wrong reason.
func TestTokenStream_FailClosedFixtureIsStillSemanticallyInvalid(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(failClosedFixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !strings.Contains(string(src), "Integer[10, 1]") && !strings.Contains(string(src), "Integer[10,1]") {
		t.Error("the fixture no longer carries inverted bounds; it cannot pin the " +
			"semantically-invalid-but-parseable case")
	}
}

func TestTokenStream_RejectsUnparseableSource(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src string }{
		{"unclosed body", "schema \"s\"\ntype T {\n\tname String\n"},
		{"stray bracket", "schema \"s\"\ntype T {\n\tname String]\n}\n"},
		{"no schema header", "type T {\n\tid String primary\n}\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := TokenStream(tt.src); err == nil {
				t.Error("a source that does not parse must not be rewritten")
			}
		})
	}
}
