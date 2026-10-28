package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// A CSV cell that does not coerce draws its E_CSV_COERCE alone on `check` and
// `load`: the validator reports nothing more for it, and the document is
// refused with one issue per cell.
func TestCheck_ACSVCellThatDoesNotCoerceDrawsOneIssue(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	schemaPath, dataPath := filepath.Join(dir, "c.yammm"), filepath.Join(dir, "Person.csv")
	if err := os.WriteFile(schemaPath, []byte("schema \"c\"\ntype Person {\n\tid String primary\n\tn Integer\n\tborn Date\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath, []byte("id,n,born\np1,abc,2020-01-01\np2,5,xyz\np3,6,2020-01-02\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"check", "load"} {
		code, stderr := executeCmdStderr(t, command, "--type", "Person", schemaPath, dataPath)
		if code != cli.ExitValidation {
			t.Errorf("%s exited %d, want %d", command, code, cli.ExitValidation)
		}
		if n, coerce := strings.Count(stderr, "error["), strings.Count(stderr, "error[E_CSV_COERCE]"); n != 2 || coerce != 2 {
			t.Errorf("%s drew %d errors, %d of them E_CSV_COERCE; want one E_CSV_COERCE per cell:\n%s", command, n, coerce, stderr)
		}
	}
}
