package scripttest

import (
	"errors"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const raceskipPath = fixtureModule + "/internal/raceskip"

// ratioFloorTest measures a ratio the race detector distorts, so its plain run
// through raceskip.Skip is the only run that gates it.
const ratioFloorTest = "TestUpdateMetadataRatioFloor"

// TestRaceSkips_GoThroughRaceskip holds every tracked Go file outside
// internal/raceskip to skipping under the race detector through raceskip.Skip.
// scripts/test.sh runs a test again without -race only when raceskip.Skip
// skipped it, so a skip built another way never runs at all.
func TestRaceSkips_GoThroughRaceskip(t *testing.T) {
	t.Parallel()
	cmd := exec.CommandContext(t.Context(), "git", "ls-files", "-z", "--", "*.go")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}

	fset := token.NewFileSet()
	scanned := 0
	ratioFloorFound := false
	for rel := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if rel == "" || strings.HasPrefix(rel, "internal/raceskip/") || slices.Contains(strings.Split(rel, "/"), "testdata") {
			continue
		}
		src, err := os.ReadFile(fromRoot(rel))
		if errors.Is(err, fs.ErrNotExist) {
			// The index still lists a file the working tree has deleted.
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Errorf("parse %s: %v", rel, err)
			continue
		}
		scanned++
		if expr := buildConstraint(file); expr != nil && mentionsTag(expr, "race") {
			t.Errorf("%s is built by the race tag; skip through raceskip.Skip instead", rel)
		}
		name := importName(file, raceskipPath)
		switch name {
		case "":
		case ".":
			t.Errorf("%s dot-imports raceskip; import it by name so a read of Enabled is visible", rel)
		default:
			ast.Inspect(file, func(n ast.Node) bool {
				if sel, ok := selectorOf(n, name); ok && sel == "Enabled" {
					t.Errorf("%s reads raceskip.Enabled; skip through raceskip.Skip instead", fset.Position(n.Pos()))
				}
				return true
			})
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != ratioFloorTest || fn.Body == nil {
				continue
			}
			ratioFloorFound = true
			if !callsSkip(fn.Body, name) {
				t.Errorf("%s in %s does not call raceskip.Skip, so no run without -race gates it", ratioFloorTest, rel)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("no tracked Go file was scanned")
	}
	if !ratioFloorFound {
		t.Errorf("no tracked Go file declares %s", ratioFloorTest)
	}
}

// selectorOf returns the selected name when n is pkg.Name for the given package name.
func selectorOf(n ast.Node, pkg string) (string, bool) {
	sel, ok := n.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || id.Name != pkg {
		return "", false
	}
	return sel.Sel.Name, true
}

// callsSkip reports whether body calls raceskip.Skip through the package name pkg.
func callsSkip(body *ast.BlockStmt, pkg string) bool {
	if pkg == "" || pkg == "." {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := selectorOf(call.Fun, pkg); ok && sel == "Skip" {
				found = true
			}
		}
		return !found
	})
	return found
}

// buildConstraint returns the file's //go:build expression, or else the
// conjunction of its // +build lines, which the go command still honours.
func buildConstraint(file *ast.File) constraint.Expr {
	var plus constraint.Expr
	for _, group := range file.Comments {
		if group.Pos() >= file.Package {
			break
		}
		for _, c := range group.List {
			expr, err := constraint.Parse(c.Text)
			switch {
			case err != nil:
			case constraint.IsGoBuild(c.Text):
				return expr
			case plus == nil:
				plus = expr
			default:
				plus = &constraint.AndExpr{X: plus, Y: expr}
			}
		}
	}
	return plus
}

func mentionsTag(expr constraint.Expr, tag string) bool {
	switch e := expr.(type) {
	case *constraint.TagExpr:
		return e.Tag == tag
	case *constraint.NotExpr:
		return mentionsTag(e.X, tag)
	case *constraint.AndExpr:
		return mentionsTag(e.X, tag) || mentionsTag(e.Y, tag)
	case *constraint.OrExpr:
		return mentionsTag(e.X, tag) || mentionsTag(e.Y, tag)
	}
	return false
}

// importName returns the name the file refers to path by, or "" when the file
// does not import it.
func importName(file *ast.File, path string) string {
	for _, spec := range file.Imports {
		if p, err := strconv.Unquote(spec.Path.Value); err != nil || p != path {
			continue
		}
		if spec.Name != nil {
			return spec.Name.Name
		}
		return path[strings.LastIndexByte(path, '/')+1:]
	}
	return ""
}
