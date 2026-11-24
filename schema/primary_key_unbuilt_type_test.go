package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// TestPrimaryKey_ATypeTheParserCouldNotBuildDrawsOneIssue pins that a
// primary property whose type the schema parser refused draws that refusal
// alone: its kind is not known, so the key rule does not judge it.
func TestPrimaryKey_ATypeTheParserCouldNotBuildDrawsOneIssue(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, "schema \"s\"\ntype T {\n\tid Vector[0] primary\n}\n")
	wantCounts(t, res, map[diag.Code]int{diag.E_INVALID_CONSTRAINT: 1})
}

// TestPrimaryKey_AKeyOfUnknownKindStaysInItsTypesKey pins that such a property
// is still a key: an invariant that reads an association's target key reads
// the composite key the target declares, and draws no issue of its own.
func TestPrimaryKey_AKeyOfUnknownKindStaysInItsTypesKey(t *testing.T) {
	t.Parallel()
	for _, key := range []struct {
		decl string
		code diag.Code
	}{
		{"Vector[0]", diag.E_INVALID_CONSTRAINT},
		{"Nope", diag.E_UNKNOWN_TYPE},
	} {
		res := loadStringErr(t, `schema "s"
type T {
	id `+key.decl+` primary
	k String primary
}
type U {
	id String primary
	--> REL (one) T
	! "x" REL[0] - 1 > 0
}
`)
		wantCounts(t, res, map[diag.Code]int{key.code: 1})
	}
}
