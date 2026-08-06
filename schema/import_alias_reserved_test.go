package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// An import whose alias is derived from its path draws E_INVALID_ALIAS when the
// derivation lands on a reserved keyword, and the import is dropped.
//
// This is the only path that reaches the check: a written reserved alias never
// gets here, because the alias position refuses every reserved spelling and the
// import arrives with no alias at all. Deleting the check would leave the whole
// suite green while this source loaded successfully.
func TestImport_DerivedAliasIsReservedKeyword(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, "schema \"main\"\nimport \"type.yammm\"\n")

	counts := codeCounts(res)
	if got := counts[diag.E_INVALID_ALIAS]; got != 1 {
		t.Errorf("E_INVALID_ALIAS count: got %d, want 1: %v", got, res)
	}
	total := 0
	for _, n := range counts {
		total += n
	}
	if total != 1 {
		t.Errorf("diagnostic count: got %d, want exactly 1: %v", total, res)
	}
}

// The dropped import never reaches the loader, so no import-handling diagnostic
// follows the alias refusal.
func TestImport_DerivedAliasIsReservedKeyword_ImportIsDropped(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, "schema \"main\"\nimport \"type.yammm\"\n")

	if res.HasCode(diag.E_IMPORT_NOT_ALLOWED) {
		t.Errorf("a refused alias drops its import, so it must not reach the "+
			"disallowed-imports check: %v", res)
	}
}
