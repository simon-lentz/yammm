package doclint_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/internal/doclint"
	"github.com/simon-lentz/yammm/internal/gittree"
)

// A copy placed inside a repository that tracks none of it is a copy: the
// gates read its files on disk, as they read the module cache's copy of a
// release, and not the repository's empty listing under it.
func TestAssertProcessFreeNames_ReadsACopyInsideARepositoryThatTracksNoneOfIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gittree.WriteFile(t, dir, "top.txt", []byte("top\n"), 0o600)
	gittree.Index(t, dir, "top.txt")
	root := filepath.Join(dir, "copy")
	gittree.WriteFile(t, root, "go.mod", []byte("module example.com/copy\n\ngo 1.26\n"), 0o600)
	gittree.WriteFile(t, root, "copy_test.go", []byte("package copy\n\nimport \"testing\"\n\nfunc TestCopy(t *testing.T) {}\n"), 0o600)

	r := &recorder{}
	paths, tests := doclint.AssertProcessFreeNames(r, root)
	if paths != 2 || tests != 1 {
		t.Errorf("read %d paths and %d tests in the copy, want 2 and 1; reports %v", paths, tests, r.msgs)
	}
}

// A .git that names no repository is a repository git refuses, and the gate
// reports it rather than reading the files on disk as a copy.
func TestAssertProcessFreeNames_ReportsARepositoryGitRefuses(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gittree.WriteFile(t, root, ".git", []byte("gitdir: "+filepath.Join(root, "missing")+"\n"), 0o600)
	gittree.WriteFile(t, root, "go.mod", []byte("module example.com/refused\n\ngo 1.26\n"), 0o600)

	r := &recorder{}
	if paths, _ := doclint.AssertProcessFreeNames(r, root); paths != 0 {
		t.Errorf("read %d paths under a refused repository, want none", paths)
	}
	if !r.reports("listing the paths of " + root) {
		t.Errorf("no report of the refused repository; got %v", r.msgs)
	}
}

// A root that is a link is read at its target.
func TestAssertCitedCodesExist_ReadsARootThatIsALink(t *testing.T) {
	t.Parallel()
	target := t.TempDir()
	gittree.WriteFile(t, target, "docs/a.md", []byte("E_GONE\n"), 0o600)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("this host makes no symbolic link: %v", err)
	}

	r := &recorder{}
	n := doclint.AssertCitedCodesExist(r, link, doclint.CodeRules{Codes: []string{"E_KNOWN"}})
	if n.Markdown != 1 || !r.reports("docs/a.md:1: cites the diagnostic code E_GONE") {
		t.Errorf("read %d names through the link, want the one in docs/a.md; reports %v", n.Markdown, r.msgs)
	}
}

// Markdown is a .md or .markdown file in any letter case, in both gates that
// read it.
func TestMarkdownGates_ReadEveryMarkdownSpelling(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"a.markdown", "B.MD", "c.Markdown"} {
		gittree.WriteFile(t, root, name, []byte("E_GONE\n\n`yammm check --absent`\n"), 0o600)
	}
	gittree.WriteFile(t, root, "d.mdx", []byte("E_GONE\n\n`yammm check --absent`\n"), 0o600)

	r := &recorder{}
	if n := doclint.AssertCitedCodesExist(r, root, doclint.CodeRules{Codes: []string{"E_KNOWN"}}); n.Markdown != 3 {
		t.Errorf("the code gate read %d names in Markdown, want 3; reports %v", n.Markdown, r.msgs)
	}
	r = &recorder{}
	if n := doclint.AssertInvocationsExist(r, root, invocationRules()); n != 3 {
		t.Errorf("the invocation gate read %d invocations, want 3; reports %v", n, r.msgs)
	}
}
