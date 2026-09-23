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
		"README.md:6: cites the diagnostic code E_PLURAL_GONE, which is not a registered code",           // the capitals opening a longer word
		"README.md:6: cites the diagnostic code E_BOGUS2,",                                               // capitals and a digit, then lowercase
		"README.md:7: cites the diagnostic code E_OPEN_GONE, which is not a registered code",             // a closing underscore with no asterisk
		"README.md:9: cites the diagnostic code E_AB, which is not a registered code",                    // two characters after the prefix, then lowercase
		"README.md:9: cites the diagnostic code E_ODD, which is not a registered code",                   // an underscore before the lowercase
	} {
		if !r.reports(want) {
			t.Errorf("no report %q; got %v", want, r.msgs)
		}
	}
	if len(r.msgs) != 13 {
		t.Errorf("got %d reports, want 13: %v", len(r.msgs), r.msgs)
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
		"E_K,", "W_F,",    // a capitalized word, E_Known or W_Foo_bar, is no code
		"E_KNOWN_ONE_", // unbalanced emphasis closes on no family
		"E__DOUBLE",    // an underscore where the capital goes
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
	if n.Comments != 8 || n.Markdown != 14 || n.Shell != 0 {
		t.Errorf("counted %+v, want 8 in comments, 14 in Markdown and none in shell", n)
	}
}

// An exclusion shadowed by another that matches the same file is still used,
// and one that matches only a file the gate never reads is stale: the testdata
// and extension filters come first.
func TestAssertCitedCodesExist_MarksEveryExclusionThatMatches(t *testing.T) {
	t.Parallel()
	rules := codeRules()
	rules.Exclude = append(rules.Exclude, "notes/excluded.md", "testdata/*.md", "go.mod")
	r, _ := runCodeGate(t, rules)
	for _, stale := range []string{`exclusion "testdata/*.md" matches no tracked`, `exclusion "go.mod" matches no tracked`} {
		if !r.reports(stale) {
			t.Errorf("no report %q; got %v", stale, r.msgs)
		}
	}
	for _, used := range []string{`exclusion "notes/*.md"`, `exclusion "notes/excluded.md"`} {
		if r.reports(used) {
			t.Errorf("reported %s as stale: %v", used, r.msgs)
		}
	}
}

// A tracked shell script's lines that open on "#" are read; its commands and its
// shebang are not.
//
// Not parallel: it builds its own repository (isolateFromEnclosingRepository).
func TestAssertCitedCodesExist_ReadsShellComments(t *testing.T) {
	isolateFromEnclosingRepository(t)
	dir := t.TempDir()
	script := "#!/usr/bin/env -S bash E_SHEBANG_GONE\n# Reports E_SHELL_GONE and E_KNOWN.\n  # indented: W_INDENTED_GONE\n#! a later line: E_BANG_GONE\n\t# tabbed: E_TABBED_GONE\necho E_COMMAND_GONE # trailing: E_TRAILING_SHELL_GONE\n"
	if err := os.WriteFile(filepath.Join(dir, "hook.sh"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "hook.sh"}} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	r := &recorder{}
	n := doclint.AssertCitedCodesExist(r, dir, doclint.CodeRules{Codes: []string{"E_KNOWN"}})
	for _, want := range []string{
		"hook.sh:2: cites the diagnostic code E_SHELL_GONE,",
		"hook.sh:3: cites the diagnostic code W_INDENTED_GONE,",
		"hook.sh:4: cites the diagnostic code E_BANG_GONE,",
		"hook.sh:5: cites the diagnostic code E_TABBED_GONE,",
	} {
		if !r.reports(want) {
			t.Errorf("no report %q; got %v", want, r.msgs)
		}
	}
	if len(r.msgs) != 4 {
		t.Errorf("got %d reports, want 4: %v", len(r.msgs), r.msgs)
	}
	if n.Shell != 5 {
		t.Errorf("counted %+v, want 5 in shell", n)
	}
}

// A removed name is allowed in the file that records it and nowhere else, and
// one the file stops citing, or a code that is registered, is reported.
func TestAssertCitedCodesExist_AllowsARemovedNameInItsFileOnly(t *testing.T) {
	t.Parallel()
	rules := codeRules()
	rules.Removed = map[string][]string{
		"README.md": {"E_DOC_GONE", "E_VANISHED", "E_UNCITED_GONE", "E_KNOWN"},
	}
	r, _ := runCodeGate(t, rules)
	for _, silent := range []string{"README.md:3: cites the diagnostic code E_DOC_GONE", "removed name E_DOC_GONE is not cited"} {
		if r.reports(silent) {
			t.Errorf("reported %q for a removed name its file cites: %v", silent, r.msgs)
		}
	}
	for _, want := range []string{
		"cited/cited.go:4: cites the diagnostic code E_VANISHED,",
		"removed name E_VANISHED is not cited in README.md",
		"removed name E_UNCITED_GONE is not cited in README.md",
		"removed name E_KNOWN in README.md is a registered code",
	} {
		if !r.reports(want) {
			t.Errorf("no report %q; got %v", want, r.msgs)
		}
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
//
// Not parallel: it builds its own repository (isolateFromEnclosingRepository).
func TestAssertCitedCodesExist_ReportsAFileItCannotRead(t *testing.T) {
	isolateFromEnclosingRepository(t)
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
