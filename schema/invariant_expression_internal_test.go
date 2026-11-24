package schema

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/gittree"
	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/location"
)

// TestInvariant_ExpressionIsTheTextAsWritten holds Expression to the author's
// text with comments removed, each gap between two tokens written as one line
// break when it holds one and as one space otherwise, and no gap where the
// source has none.
func TestInvariant_ExpressionIsTheTextAsWritten(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, expr, want string
	}{
		{"line comment", "n > 0 // positive", "n > 0"},
		{"slashes in a string", `s != "a//b"`, `s != "a//b"`},
		{"slashes in a regexp", `s =~ /a\/\/b/`, `s =~ /a\/\/b/`},
		{"multi-line", "n > 0 && // low\n\t\t// between\n\t\tn   <\t10", "n > 0 &&\nn < 10"},
		{"a line break as CR", "n > 0 &&\r\tn < 10", "n > 0 &&\nn < 10"},
		{"two divisions on two lines", "n / 2 > 0 &&\n\t\tn / 3 < 10", "n / 2 > 0 &&\nn / 3 < 10"},
		{"adjacent tokens", "(s->Len)>0&&!(n==1)", "(s->Len)>0&&!(n==1)"},
		{"a call", `s -> TrimPrefix( "x" ) == "y"`, `s -> TrimPrefix( "x" ) == "y"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := "schema \"s\"\ntype T {\n\tid String primary\n\tn Integer\n\ts String\n\t! \"m\" " + tt.expr + "\n}\n"
			s, err := LoadString(t.Context(), src, "s.yammm")
			if err != nil {
				t.Fatalf("load: %s", err)
			}
			typ, _ := s.Type("T")
			for inv := range typ.Invariants() {
				if got := inv.Expression(); got != tt.want {
					t.Errorf("Expression() = %q, want %q", got, tt.want)
				}
			}
		})
	}
}

// TestInvariant_ExpressionParsesToTheSameExpression parses the text of every
// invariant in the repository's tracked .yammm files again and holds it to the
// expression the source parsed to, by the structural hash's encoding.
func TestInvariant_ExpressionParsesToTheSameExpression(t *testing.T) {
	t.Parallel()
	gittree.MarkTrackedFiles()

	cmd := exec.CommandContext(t.Context(), "git", "ls-files", "-z", "--", "*.yammm")
	cmd.Dir = ".."
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	read := 0
	for name := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		src, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		file, _ := parse.Parse(src, location.SourceID{})
		if file == nil {
			continue
		}
		for _, typ := range file.Types {
			for _, inv := range typ.Invariants {
				read++
				again := reparseExpression(inv.Text)
				if again == nil || !sameExpression(inv.Expr, again.Expr) {
					t.Errorf("%s: invariant %q: %q parses to another expression", name, inv.Message, inv.Text)
				}
			}
		}
	}
	if read == 0 {
		t.Fatal("read no invariant")
	}
}

// reparseExpression parses text as the expression of a one-invariant type.
func reparseExpression(text string) *parse.Invariant {
	file, _ := parse.Parse([]byte("schema \"s\"\ntype T {\n\t! \"m\" "+text+"\n}\n"), location.SourceID{})
	if file == nil || len(file.Types) != 1 || len(file.Types[0].Invariants) != 1 {
		return nil
	}
	return file.Types[0].Invariants[0]
}
