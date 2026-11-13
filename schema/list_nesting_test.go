package schema_test

import (
	"fmt"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/schema"
)

// listChain declares datatypes L1 to Ln, each a List of the one before it, so
// Ln nests n Lists and no declaration writes more than one.
func listChain(n int) string {
	var b strings.Builder
	b.WriteString("schema \"s\"\n\ntype L1 = List<String>\n")
	for i := 2; i <= n; i++ {
		fmt.Fprintf(&b, "type L%d = List<L%d>\n", i, i-1)
	}
	return b.String()
}

func nestingIssues(t *testing.T, src string) []diag.Issue {
	t.Helper()
	_, res := schema.LoadString(t.Context(), src, "s.yammm")
	var out []diag.Issue
	for issue := range res.Issues() {
		if strings.Contains(issue.Message(), "List nests deeper") {
			out = append(out, issue)
			continue
		}
		t.Errorf("an issue of another kind: %s %s", issue.Code(), issue.Message())
	}
	return out
}

// The bound on a List's nesting counts the Lists of each datatype the List
// names, so a chain of datatypes reaches it as one written type does.
func TestLoad_ListNestingCountsThroughDatatypes(t *testing.T) {
	t.Parallel()
	limit := parse.MaxNesting
	holder := func(elem string) string { return "type T {\n\tid String primary\n\tdeep " + elem + "\n}\n" }

	t.Run("a datatype at the bound loads", func(t *testing.T) {
		t.Parallel()
		src := listChain(limit) + holder(fmt.Sprintf("L%d", limit))
		if got := nestingIssues(t, src); len(got) != 0 {
			t.Fatalf("a List of %d levels drew %d issues", limit, len(got))
		}
	})
	t.Run("a datatype past the bound is one issue at its declaration", func(t *testing.T) {
		t.Parallel()
		src := listChain(limit+1) + holder(fmt.Sprintf("L%d", limit+1))
		got := nestingIssues(t, src)
		if len(got) != 1 {
			t.Fatalf("want one issue, got %d", len(got))
		}
		if got[0].Code() != diag.E_INVALID_CONSTRAINT {
			t.Errorf("code = %s, want E_INVALID_CONSTRAINT", got[0].Code())
		}
		if line := got[0].Span().Start.Line; line != limit+3 {
			t.Errorf("the issue is at line %d, want the declaration of L%d at line %d", line, limit+1, limit+3)
		}
	})
	t.Run("a chain three times the bound is one issue at the first datatype past it", func(t *testing.T) {
		t.Parallel()
		members := fmt.Sprintf("L%d\n\tmid L%d\n\tlist List<L%d>", 3*limit, limit+limit/2, 2*limit+limit/2)
		for name, src := range map[string]string{
			"declared from the element out": listChain(3*limit) + holder(members),
			"used before it is declared":    "schema \"s\"\n\n" + holder(members) + strings.TrimPrefix(listChain(3*limit), "schema \"s\"\n\n"),
		} {
			got := nestingIssues(t, src)
			if len(got) != 1 {
				t.Fatalf("%s: want one issue, got %d", name, len(got))
			}
			if text := lineOf(src, got[0].Span().Start.Line); !strings.HasPrefix(text, fmt.Sprintf("type L%d =", limit+1)) {
				t.Errorf("%s: the issue is at %q, want the declaration of L%d", name, text, limit+1)
			}
		}
	})
	t.Run("a property past the bound is one issue at the property", func(t *testing.T) {
		t.Parallel()
		src := listChain(limit) + holder(fmt.Sprintf("List<L%d>", limit))
		got := nestingIssues(t, src)
		if len(got) != 1 {
			t.Fatalf("want one issue, got %d", len(got))
		}
		if got[0].Code() != diag.E_INVALID_CONSTRAINT {
			t.Errorf("code = %s, want E_INVALID_CONSTRAINT", got[0].Code())
		}
		if line := got[0].Span().Start.Line; line != limit+5 {
			t.Errorf("the issue is at line %d, want the property at line %d", line, limit+5)
		}
	})
}

// A chain of datatypes resolves with no call nested per datatype: one declared
// from its longest datatype down loads under a stack that a nested call per
// datatype passes.
func TestLoad_ADatatypeChainDeclaredLongestFirstTakesNoStackPerDatatype(t *testing.T) {
	// The stack limit is the process's, so the test runs alone.
	defer debug.SetMaxStack(debug.SetMaxStack(8 << 20))
	const links = 12_000
	var b strings.Builder
	b.WriteString("schema \"s\"\n\n")
	for i := links; i > 1; i-- {
		fmt.Fprintf(&b, "type L%d = List<L%d>\n", i, i-1)
	}
	fmt.Fprintf(&b, "type L1 = List<String>\n\ntype T {\n\tid String primary\n\tdeep L%d\n}\n", links)
	issues := nestingIssues(t, b.String())
	if len(issues) != 1 || issues[0].Code() != diag.E_INVALID_CONSTRAINT {
		t.Fatalf("got %d issues, want one E_INVALID_CONSTRAINT", len(issues))
	}
	if got, want := issues[0].Span().Start.Line, links-parse.MaxNesting+2; got != want {
		t.Errorf("the issue is on line %d, want line %d, the first datatype past the bound", got, want)
	}
}
