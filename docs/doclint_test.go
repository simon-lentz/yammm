package docs_test

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/doclint"
)

// A removed symbol leaves its doc links behind, and nothing in the toolchain
// complains: godoc renders a link to a symbol that no longer exists as an
// ordinary link, and the pinned golangci-lint's documentation rules check
// documentation style, not reference resolution. Two releases running shipped
// documentation advertising API that had been cut — one found by a cleanup pass
// that happened to open the file, one by a consumer trying to upgrade.
//
// This test lives beside the other repo-wide documentation gates rather than in
// internal/doclint, for the same reason those do: it reads the whole module,
// not the package it sits in. The package's own tests prove the checker
// reports what it should; this one points it at the module.
func TestModuleDocLinks(t *testing.T) {
	t.Parallel()
	checked := doclint.AssertNoDanglingLinks(t, "..")
	// The walk is the only thing standing between "no link dangles" and "no
	// link was read", so a shrunken walk is an error rather than a silent pass.
	if checked < 500 {
		t.Errorf("resolved only %d doc links across the module; the walk is not reaching the source", checked)
	}
	t.Logf("resolved %d doc links", checked)
}
