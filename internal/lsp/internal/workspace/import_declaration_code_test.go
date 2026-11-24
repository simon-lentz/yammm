package workspace

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// TestIsImportDeclarationCode_PartitionsTheImportCodes holds each import code
// to one of the two families: a declaration code softens to a hint in a
// markdown block, and a resolution code keeps its severity.
func TestIsImportDeclarationCode_PartitionsTheImportCodes(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		code diag.Code
		decl bool
	}{
		{diag.E_IMPORT_NOT_ALLOWED, true},
		{diag.E_DUPLICATE_IMPORT, true},
		{diag.E_IMPORT_ALIAS_COLLISION, true},
		{diag.E_INVALID_ALIAS, true},
		{diag.E_IMPORT_RESOLVE, false},
		{diag.E_IMPORT_CYCLE, false},
		{diag.E_PATH_ESCAPE, false},
		{diag.E_SYNTAX, false},
	} {
		if got := isImportDeclarationCode(tt.code.String()); got != tt.decl {
			t.Errorf("isImportDeclarationCode(%s) = %v, want %v", tt.code, got, tt.decl)
		}
	}
}
