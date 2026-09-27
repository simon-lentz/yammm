package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/adapter/csv"
	adapterjson "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// commandFuncs parses the package's production files and calls visit with
// each function declaration and the file set that positions it.
func commandFuncs(t *testing.T, visit func(fset *token.FileSet, fn *ast.FuncDecl)) {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
				visit(fset, fn)
			}
		}
	}
}

// isSelector reports whether n is pkg.name.
func isSelector(n ast.Node, pkg, name string) bool {
	sel, ok := n.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == pkg
}

// TestExitCodes_NamedOnlyWhereNoResultDecides pins that a diag.Result becomes
// an exit code through cli.ExitForResult and nowhere else: a hard-coded
// ExitValidation after a failed result exits 1 on an I/O code the exit contract
// answers with 3, and a hard-coded ExitRuntime exits 3 on a refusal of the
// input. A literal code is refused as a name is. The sites below answer an
// outcome that is not a result: fmt --check
// finding an unformatted file, and neo4j diff finding drift or a comparison
// that could not run.
func TestExitCodes_NamedOnlyWhereNoResultDecides(t *testing.T) {
	allowed := map[string]map[string]int{
		"ExitValidation": {"fmtPath": 1, "neo4jDiffExit": 1},
		"ExitRuntime":    {"neo4jDiffExit": 1},
	}
	got := map[string]map[string]int{"ExitValidation": {}, "ExitRuntime": {}}
	commandFuncs(t, func(fset *token.FileSet, fn *ast.FuncDecl) {
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if lit, ok := n.(*ast.CompositeLit); ok && isSelector(lit.Type, "cli", "ExitError") {
				for _, elt := range lit.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						if _, literal := kv.Value.(*ast.BasicLit); literal {
							t.Errorf("%s: %s builds a cli.ExitError from a literal code; answer a diag.Result with cli.ExitForResult",
								fset.Position(kv.Pos()), fn.Name.Name)
						}
					}
				}
			}
			for code := range allowed {
				if !isSelector(n, "cli", code) {
					continue
				}
				got[code][fn.Name.Name]++
				if got[code][fn.Name.Name] > allowed[code][fn.Name.Name] {
					t.Errorf("%s: %s names cli.%s; answer a diag.Result with cli.ExitForResult",
						fset.Position(n.Pos()), fn.Name.Name, code)
				}
			}
			return true
		})
	})
	for code, fns := range allowed {
		for fn, want := range fns {
			if got[code][fn] != want {
				t.Errorf("%s names cli.%s %d times, want %d: the allowance is stale", fn, code, got[code][fn], want)
			}
		}
	}
}

// TestExitCodes_EveryReturnedErrorIsClassified pins that a command classifies
// each failure where it returns it. An error that is not a cli.ExitError exits
// 3, a failure the command did not classify, so a usage refusal returned as a
// bare fmt.Errorf or errors.New would exit 3 where the exit table says 2.
func TestExitCodes_EveryReturnedErrorIsClassified(t *testing.T) {
	commandFuncs(t, func(fset *token.FileSet, fn *ast.FuncDecl) {
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			ret, ok := n.(*ast.ReturnStmt)
			if !ok {
				return true
			}
			for _, res := range ret.Results {
				call, ok := res.(*ast.CallExpr)
				if !ok {
					continue
				}
				if isSelector(call.Fun, "fmt", "Errorf") || isSelector(call.Fun, "errors", "New") {
					t.Errorf("%s: %s returns an unclassified error; return cli.Usagef, cli.Runtimef or cli.Validationf",
						fset.Position(call.Pos()), fn.Name.Name)
				}
			}
			return true
		})
	})
}

// TestExitCodes_CobraRefusalsAreUsage pins that a refusal cobra makes before
// any command runs exits 2 on every command in newRootCmd's tree: an unknown
// flag, an unknown subcommand, and an operand count the command's validator
// refuses. A failure a command returns unclassified exits 3, so these reach 2
// only through execute.
func TestExitCodes_CobraRefusalsAreUsage(t *testing.T) {
	t.Parallel()
	var walk func(c *cobra.Command)
	var lines [][]string
	walk = func(c *cobra.Command) {
		path := strings.Fields(c.CommandPath())[1:]
		lines = append(lines, append(slices.Clone(path), "--no-such-flag"))
		if c.HasSubCommands() {
			lines = append(lines, append(slices.Clone(path), "no-such-command"))
		}
		many := slices.Repeat([]string{"x"}, 20)
		if c.Args != nil && c.Args(c, many) != nil {
			lines = append(lines, append(slices.Clone(path), many...))
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(newRootCmd("test"))
	for _, args := range lines {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			if code, _, stderr := executeCmdOutput(t, args...); code != cli.ExitUsage {
				t.Errorf("exit %d, want %d: %s", code, cli.ExitUsage, stderr)
			}
		})
	}
}

// TestWriterFailure_ARefusalOfTheDataExitsValidation pins export's writer rule:
// a value the format cannot represent is a refusal of the input (1), wrapped
// or not, and any other writer failure is outside the input (3).
func TestWriterFailure_ARefusalOfTheDataExitsValidation(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		err  error
		want int
	}{
		{"csv's refusal", csv.ErrUnrepresentable, cli.ExitValidation},
		{"json's refusal, wrapped", fmt.Errorf("marshal: %w", adapterjson.ErrUnrepresentable), cli.ExitValidation},
		{"any other writer failure", errors.New("disk full"), cli.ExitRuntime},
		{"csv's setting refusal", csv.ErrConfig, cli.ExitRuntime},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := cli.ExitForError(writerFailure("marshal", c.err)); got != c.want {
				t.Errorf("writerFailure(%v) exits %d, want %d", c.err, got, c.want)
			}
		})
	}
}
