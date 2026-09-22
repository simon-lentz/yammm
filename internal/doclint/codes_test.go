package doclint_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/internal/doclint"
)

const codeFixture = "testdata/codefixture"

func codeRules() doclint.CodeRules {
	return doclint.CodeRules{
		Codes:        []string{"E_KNOWN", "E_KNOWN_ONE", "W_KNOWN_WARNING"},
		Exclude:      []string{"notes/*.md"},
		Placeholders: []string{"E_EXAMPLE_CODE"},
	}
}

func runCodeGate(t *testing.T, rules doclint.CodeRules) (*recorder, doclint.CodeCitations) {
	t.Helper()
	r := &recorder{}
	return r, doclint.AssertCitedCodesExist(r, codeFixture, rules)
}

func TestAssertCitedCodesExist_ReportsEveryUnregisteredName(t *testing.T) {
	t.Parallel()
	r, _ := runCodeGate(t, codeRules())
	for _, want := range []string{
		"cited/cited.go:4: cites the diagnostic code E_VANISHED,",                                        // package doc
		"cited/cited.go:4: cites the diagnostic code E_GONE_, which is the prefix of no registered code", // a family no code starts
		"cited/cited.go:9: cites the diagnostic code E_TRAILING_GONE,",                                   // an inline comment
		"cited/tagged.go:5: cites the diagnostic code E_TAGGED_GONE,",                                    // behind a build constraint
		"README.md:3: cites the diagnostic code E_DOC_GONE, which is not a registered code",              // Markdown
		"README.md:4: cites the diagnostic code E_EM_GONE,",                                              // in emphasis
		"README.md:4: cites the diagnostic code E_STRONG_GONE,",                                          // in strong emphasis
		"README.md:5: cites the diagnostic code E_ESCAPED_GONE,",                                         // escaped
	} {
		if !r.reports(want) {
			t.Errorf("no report %q; got %v", want, r.msgs)
		}
	}
	if len(r.msgs) != 8 {
		t.Errorf("got %d reports, want 8: %v", len(r.msgs), r.msgs)
	}
}

func TestAssertCitedCodesExist_ReadsRegisteredNamesSilently(t *testing.T) {
	t.Parallel()
	r, _ := runCodeGate(t, codeRules())
	for _, silent := range []string{
		"E_KNOWN,", "W_KNOWN_WARNING", "E_KNOWN_,", // registered, qualified, a family
		"E_IN_A_STRING",   // a Go string literal
		"E_EMBEDDED",      // inside a longer word
		"E_1ST",           // no capital after the prefix
		"E_EXCLUDED_GONE", // an excluded path
		"E_TESTDATA_GONE", // under testdata
		"E_EXAMPLE_CODE",  // a placeholder
	} {
		if r.reports(silent) {
			t.Errorf("reported %s: %v", silent, r.msgs)
		}
	}
}

// The counts let a caller hold a floor, so a walk that reads nothing fails.
func TestAssertCitedCodesExist_CountsWhatItRead(t *testing.T) {
	t.Parallel()
	_, n := runCodeGate(t, codeRules())
	if n.Comments != 8 || n.Markdown != 7 {
		t.Errorf("counted %+v, want 8 in comments and 7 in Markdown", n)
	}
}

func TestAssertCitedCodesExist_ReportsStaleRules(t *testing.T) {
	t.Parallel()
	rules := codeRules()
	rules.Exclude = append(rules.Exclude, "gone/*.md")
	rules.Placeholders = append(rules.Placeholders, "E_UNCITED", "E_KNOWN")
	r, _ := runCodeGate(t, rules)
	for _, want := range []string{
		`exclusion "gone/*.md" matches no tracked`,
		"placeholder E_UNCITED is cited nowhere",
		"placeholder E_KNOWN is a registered code",
	} {
		if !r.reports(want) {
			t.Errorf("no report %q; got %v", want, r.msgs)
		}
	}
}

func TestAssertCitedCodesExist_MissingRootIsReported(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	n := doclint.AssertCitedCodesExist(r, "testdata/does-not-exist", codeRules())
	if n != (doclint.CodeCitations{}) {
		t.Errorf("counted %+v over a missing root; want none", n)
	}
	if len(r.msgs) == 0 {
		t.Error("a missing root passed silently")
	}
}

func TestAssertCitedCodesExist_RefusesAMalformedExclusion(t *testing.T) {
	t.Parallel()
	rules := codeRules()
	rules.Exclude = append(rules.Exclude, "notes/[")
	r, n := runCodeGate(t, rules)
	if !r.reports(`exclusion "notes/[" is not a valid pattern`) {
		t.Errorf("a malformed exclusion was not reported: %v", r.msgs)
	}
	if n != (doclint.CodeCitations{}) {
		t.Errorf("counted %+v under a malformed exclusion; want nothing read", n)
	}
}

// A tracked file the gate cannot read or parse is reported, never skipped.
func TestAssertCitedCodesExist_ReportsAFileItCannotRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("broken.go", "package broken\n\nfunc {\n")
	write("gone.md", "E_KNOWN\n")
	for _, args := range [][]string{{"init", "-q"}, {"add", "broken.go", "gone.md"}} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.Remove(filepath.Join(dir, "gone.md")); err != nil {
		t.Fatal(err)
	}
	r := &recorder{}
	doclint.AssertCitedCodesExist(r, dir, doclint.CodeRules{Codes: []string{"E_KNOWN"}})
	for _, want := range []string{"parsing " + filepath.Join(dir, "broken.go"), "reading " + filepath.Join(dir, "gone.md")} {
		if !r.reports(want) {
			t.Errorf("no report %q; got %v", want, r.msgs)
		}
	}
}
