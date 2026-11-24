package docs_test

import (
	"os"
	"regexp"
	"testing"

	"golang.org/x/mod/modfile"
)

var (
	readmeRequirement = regexp.MustCompile(`Requires Go (\S+) or later\.`)
	readmeBadge       = regexp.MustCompile(`img\.shields\.io/badge/go-([0-9.]+)\+-`)
)

// TestReadme_StatesGoModsGoDirective holds the README's Go requirement, its
// sentence and its badge, to go.mod's go directive. The directive is the
// module's minimum and the toolchain the gate and CI run, so a README that
// names another version states the wrong minimum. Each form must occur exactly
// once, so a rewritten sentence fails here rather than passing unread.
func TestReadme_StatesGoModsGoDirective(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modfile.ParseLax("go.mod", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if mod.Go == nil {
		t.Fatal("go.mod names no go directive")
	}
	want := mod.Go.Version
	readme, err := os.ReadFile(readmeFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, form := range []struct {
		name string
		re   *regexp.Regexp
	}{
		{"requirement sentence", readmeRequirement},
		{"Go version badge", readmeBadge},
	} {
		matches := form.re.FindAllSubmatch(readme, -1)
		if len(matches) != 1 {
			t.Errorf("README.md holds %d of the %s (%s), want 1", len(matches), form.name, form.re)
			continue
		}
		if got := string(matches[0][1]); got != want {
			t.Errorf("README.md's %s names Go %s; go.mod's go directive is %s", form.name, got, want)
		}
	}
}
