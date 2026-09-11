package location

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestConstructors_AgreeOnARealFile holds the three file-backed constructors to
// one identity for one existing file. SourceIDFromAbsolutePath promises
// equality with SourceIDFromPath for a path without symlinks, and
// CanonicalizePathForSourceID promises to match SourceIDFromPath; every file
// sits under a symlink-resolved directory, so no symlink separates them.
func TestConstructors_AgreeOnARealFile(t *testing.T) {
	t.Parallel()

	// knownDisagreements names the rows that disagree today; a listed row that
	// starts agreeing fails, so the repair must remove its entry.
	knownDisagreements := map[string]string{}
	if runtime.GOOS != "windows" {
		knownDisagreements = map[string]string{
			"backslash in a file name":   "two constructors rewrite a backslash to a separator, one keeps it",
			"dot-dot beside a backslash": "the rewrite runs after cleaning, so one result keeps a .. segment",
			"double slash prefix":        "SourceIDFromAbsolutePath refuses a leading // that the others clean to /",
		}
	}

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)

	rows := []struct {
		name string
		// rel is the entry created under root, spelled with the host separator.
		rel string
		// dir creates rel as a directory rather than a file.
		dir bool
		// input derives the path handed to the constructors from the created one.
		input func(created string) string
		// unixOnly marks a row whose spelling names nothing distinct on Windows.
		unixOnly bool
	}{
		{name: "plain file", rel: "plain.yammm"},
		{name: "backslash in a file name", rel: `a\b.yammm`},
		{name: "dot-dot beside a backslash", rel: `d\..\c.yammm`},
		{name: "trailing separator", rel: "dir", dir: true, input: func(p string) string { return p + sep }},
		{name: "double slash prefix", rel: "dslash.yammm", input: func(p string) string { return "/" + p }, unixOnly: true},
		{name: "decomposed name", rel: "cafe\u0301.yammm"},
	}

	names := make(map[string]bool, len(rows))
	for _, row := range rows {
		names[row.name] = true
	}
	for name := range knownDisagreements {
		if !names[name] {
			t.Errorf("knownDisagreements names %q, which is no row", name)
		}
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			if row.unixOnly && runtime.GOOS == "windows" {
				t.Skip("the spelling names nothing distinct on Windows")
			}

			created := root + sep + row.rel
			if err := os.MkdirAll(filepath.Dir(created), 0o750); err != nil {
				t.Fatal(err)
			}
			if row.dir {
				if err := os.Mkdir(created, 0o750); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(created, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			input := created
			if row.input != nil {
				input = row.input(created)
			}

			detail, agree := constructorAgreement(input)
			reason, known := knownDisagreements[row.name]
			switch {
			case agree && known:
				t.Errorf("the constructors now agree on %q (%s); remove the row from knownDisagreements, which said: %s", input, detail, reason)
			case !agree && !known:
				t.Errorf("the constructors disagree on %q: %s", input, detail)
			}
		})
	}
}

// constructorAgreement runs the three file-backed constructors over one path
// and reports whether they produced one identity, with every result.
func constructorAgreement(p string) (string, bool) {
	fromPath, errPath := SourceIDFromPath(p)
	fromAbs, errAbs := SourceIDFromAbsolutePath(p)
	forKey, errKey := CanonicalizePathForSourceID(p)
	detail := fmt.Sprintf("SourceIDFromPath=%q (err %v), SourceIDFromAbsolutePath=%q (err %v), CanonicalizePathForSourceID=%q (err %v)",
		fromPath.String(), errPath, fromAbs.String(), errAbs, forKey, errKey)
	if errPath != nil || errAbs != nil || errKey != nil {
		return detail, false
	}
	return detail, fromPath.String() == fromAbs.String() && fromAbs.String() == forKey
}
