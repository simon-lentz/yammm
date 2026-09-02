package yammmtest

import (
	"os"
	"testing"
)

// ModuleRootFinder is the shape of the module-root discovery walk.
//
// The finder is a parameter rather than a direct call so this package does not
// import schema: schema depends on diag, internal/parse and internal/source,
// and in-package tests in all three import this package. Importing schema here
// would be an import cycle in those test binaries.
type ModuleRootFinder func(dir string) (root string, found bool, err error)

// RequireNoModuleRoot fails the test when a module-root marker sits above
// either directory a disk-loading test resolves paths from, naming the marker
// it found.
//
// Discovery makes every disk load sensitive to the filesystem ABOVE its
// fixtures: a marker anywhere on the ancestor chain silently widens the load's
// sandbox and moves the root a test asserts. Both chains are walked because
// the corpus creates trees under the temp root while `go test` runs with the
// package directory as the working directory, and a marker on either would
// change a result without changing a fixture.
//
// A test that deliberately plants a marker calls this BEFORE planting it, or
// not at all.
func RequireNoModuleRoot(tb testing.TB, find ModuleRootFinder) {
	tb.Helper()

	chains := map[string]string{"temp directory": tb.TempDir()}
	if wd, err := os.Getwd(); err == nil {
		chains["package directory"] = wd
	}

	for name, dir := range chains {
		root, found, err := find(dir)
		switch {
		case err != nil:
			tb.Fatalf("a malformed module-root marker sits above this test's %s (%s): %v; "+
				"remove it — discovery would fail every disk load here", name, dir, err)
		case found:
			tb.Fatalf("a module-root marker in %s sits above this test's %s (%s); "+
				"remove it — it widens every disk load's sandbox and moves the root this test asserts",
				root, name, dir)
		}
	}
}
