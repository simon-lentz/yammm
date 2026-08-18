package doclint

import "fmt"

// TB is the slice of testing.TB this gate uses, which *testing.T satisfies.
//
// It is an interface of this package's own rather than testing.TB itself
// because testing.TB is deliberately unimplementable outside the standard
// library, and a gate whose failure path is unreachable from a test is a gate
// that can silently stop failing.
type TB interface {
	Helper()
	Errorf(format string, args ...any)
}

// AssertNoDanglingLinks reports every doc link under root that names nothing,
// and returns how many links it resolved.
//
// The count is returned rather than asserted internally so the caller keeps its
// own floor visible: a walk that reaches no source resolves no links and would
// otherwise pass.
func AssertNoDanglingLinks(t TB, root string) (checked int) {
	t.Helper()
	m, err := Load(root)
	if err != nil {
		t.Errorf("loading %s: %v", root, err)
		return 0
	}
	dangling, checked := m.Dangling()
	for _, l := range dangling {
		t.Errorf("%s: doc link [%s] resolves to nothing%s", l.Pos, l.Text, suffix(l))
	}
	return checked
}

// suffix names the package a link was resolved against, when it is not the
// referencing one — without it a report on a qualified link says nothing about
// where the name was looked for.
func suffix(l Link) string {
	if l.ImportPath == "" {
		return ""
	}
	return fmt.Sprintf(" (looked for %q in %s)", key(l), l.ImportPath)
}
