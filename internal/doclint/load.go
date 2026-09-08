package doclint

import (
	"context"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// Package is one directory's Go files and every name they declare.
//
// The directory rather than the package is the unit, so a name declared in an
// external test package (package foo_test beside package foo) resolves for a
// link written in either.
type Package struct {
	// Dir is the slash-separated path from the walk root, "." for the root.
	Dir string
	// ImportPath is the module path joined with Dir.
	ImportPath string
	// Name is the package clause of the directory's non-test files, falling
	// back to a test file's when a directory holds only tests.
	Name string

	fset  *token.FileSet
	files []*ast.File
	names map[string]struct{}
}

// Module is a parsed module: every package under the walk root, indexed for
// resolution by import path and by package name.
type Module struct {
	Path     string
	Packages []*Package

	byImport map[string]*Package
	byName   map[string][]*Package
}

// Load parses the module's Go files under root, skipping directories that hold
// no source a consumer reads: testdata, node_modules, and anything whose name
// starts with "." or "_". Files that do not parse are an error rather than a
// skip — a gate that quietly drops the file it cannot read reports a clean run
// over nothing.
//
// The file set is the TRACKED tree where root sits in a git work tree, and a
// filesystem walk where git is unavailable. An untracked file is not source the
// module ships, and reading it gave a local run a verdict CI could not
// reproduce.
//
// A build constraint excludes a NON-TEST file only. go doc renders no test
// declaration under any tag, so "outside the published documentation" cannot
// justify dropping a test file — and dropping one silently removes the
// regression anchors this gate exists to check. The constraints are evaluated
// against a PINNED context (see [buildContext]), so the verdict does not depend
// on the machine the gate runs on.
func Load(root string) (*Module, error) {
	modPath, err := modulePath(root)
	if err != nil {
		return nil, err
	}
	m := &Module{
		Path:     modPath,
		byImport: make(map[string]*Package),
		byName:   make(map[string][]*Package),
	}
	fset := token.NewFileSet()
	tracked, hasTracked, err := trackedGoFiles(context.Background(), root)
	if err != nil {
		return nil, err
	}
	if !hasTracked {
		tracked = nil
	}

	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if p != root && skipDir(d.Name()) {
			return fs.SkipDir
		}
		pkg, found, err := loadDir(fset, root, p, tracked)
		if err != nil {
			return err
		}
		if found {
			m.Packages = append(m.Packages, pkg)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", root, err)
	}

	for _, pkg := range m.Packages {
		pkg.ImportPath = modPath
		if pkg.Dir != "." {
			pkg.ImportPath = path.Join(modPath, pkg.Dir)
		}
		m.byImport[pkg.ImportPath] = pkg
		m.byName[pkg.Name] = append(m.byName[pkg.Name], pkg)
	}
	return m, nil
}

func skipDir(name string) bool {
	if name == "testdata" || name == "node_modules" {
		return true
	}
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// modulePath reads the module path from root's go.mod. It is read textually
// rather than through golang.org/x/mod so this package stays standard-library
// only, matching every other gate in the repo.
func modulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("reading go.mod: %w", err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module ")
		if !ok {
			continue
		}
		return strings.TrimSpace(rest), nil
	}
	return "", fmt.Errorf("no module directive in %s", filepath.Join(root, "go.mod"))
}

// buildContext is the context the constraint filter reads. It pins GOOS and
// GOARCH rather than inheriting them: measured, the ambient context excludes
// nine files under darwin/arm64 and linux/amd64 and ten under windows/amd64,
// so an inherited context makes the gate's verdict a property of the machine —
// the local-versus-CI divergence class this repo has closed twice elsewhere.
func buildContext() build.Context {
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH = "linux", "amd64"
	return ctx
}

// isTestFile reports whether name is a Go test file, which no build constraint
// excludes here: go doc renders none of its declarations under any tag.
func isTestFile(name string) bool { return strings.HasSuffix(name, "_test.go") }

// gitTracks reports whether git can list root's tracked files: the binary
// exists and root is inside a work tree. Neither absence is a failure to read
// the module — both mean there is no tracked set — so neither is an error.
func gitTracks(ctx context.Context, root string) bool {
	if _, err := exec.LookPath("git"); err != nil {
		return false
	}
	return exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--is-inside-work-tree").Run() == nil
}

// trackedGoFiles returns every .go file git tracks under root, keyed by its
// path as the walk spells it. The second result is false when there is no
// tracked set to read — git is unavailable, or root is outside a work tree —
// and the caller then walks the filesystem, which is what keeps the gate
// runnable outside a checkout.
func trackedGoFiles(ctx context.Context, root string) (tracked map[string]bool, ok bool, err error) {
	if !gitTracks(ctx, root) {
		return nil, false, nil
	}
	out, err := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z", "--", "*.go").Output()
	if err != nil {
		return nil, false, fmt.Errorf("listing the tracked files of %s: %w", root, err)
	}
	tracked = make(map[string]bool)
	for name := range strings.SplitSeq(string(out), "\x00") {
		if name == "" {
			continue
		}
		tracked[filepath.ToSlash(filepath.Join(root, name))] = true
	}
	return tracked, true, nil
}

// loadDir parses dir's Go files, reporting false when it holds none.
func loadDir(fset *token.FileSet, root, dir string, tracked map[string]bool) (pkg *Package, found bool, err error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, false, fmt.Errorf("reading %s: %w", dir, err)
	}
	bctx := buildContext()
	pkg = &Package{fset: fset, names: make(map[string]struct{})}
	var testName string
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		if tracked != nil && !tracked[filepath.ToSlash(full)] {
			continue
		}
		if !isTestFile(e.Name()) {
			included, err := bctx.MatchFile(dir, e.Name())
			if err != nil {
				return nil, false, fmt.Errorf("reading the build constraints of %s: %w", full, err)
			}
			if !included {
				continue
			}
		}
		f, err := parser.ParseFile(fset, full, nil, parser.ParseComments)
		if err != nil {
			return nil, false, fmt.Errorf("parsing %s: %w", full, err)
		}
		pkg.files = append(pkg.files, f)
		collectNames(f, pkg.names)
		name := f.Name.Name
		if strings.HasSuffix(name, "_test") || strings.HasSuffix(e.Name(), "_test.go") {
			if testName == "" {
				testName = strings.TrimSuffix(name, "_test")
			}
			continue
		}
		pkg.Name = name
	}
	if len(pkg.files) == 0 {
		return nil, false, nil
	}
	if pkg.Name == "" {
		pkg.Name = testName
	}

	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return nil, false, fmt.Errorf("relativizing %s: %w", dir, err)
	}
	pkg.Dir = filepath.ToSlash(rel)
	return pkg, true, nil
}

// collectNames adds every name f declares to names: package-level identifiers
// bare, and struct fields, interface methods, and methods as "Type.Name".
func collectNames(f *ast.File, names map[string]struct{}) {
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil || len(d.Recv.List) == 0 {
				names[d.Name.Name] = struct{}{}
				continue
			}
			if recv := recvTypeName(d.Recv.List[0].Type); recv != "" {
				names[recv+"."+d.Name.Name] = struct{}{}
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					names[s.Name.Name] = struct{}{}
					collectMembers(s.Name.Name, s.Type, names)
				case *ast.ValueSpec:
					for _, id := range s.Names {
						names[id.Name] = struct{}{}
					}
				}
			}
		}
	}
}

// collectMembers adds a struct's fields or an interface's methods as
// "owner.Name". An embedded field contributes the embedded type's own name,
// which is how godoc addresses it.
func collectMembers(owner string, t ast.Expr, names map[string]struct{}) {
	var fields *ast.FieldList
	switch tt := t.(type) {
	case *ast.StructType:
		fields = tt.Fields
	case *ast.InterfaceType:
		fields = tt.Methods
	default:
		return
	}
	if fields == nil {
		return
	}
	for _, f := range fields.List {
		if len(f.Names) == 0 {
			if embedded := recvTypeName(f.Type); embedded != "" {
				names[owner+"."+embedded] = struct{}{}
			}
			continue
		}
		for _, id := range f.Names {
			names[owner+"."+id.Name] = struct{}{}
		}
	}
}

// recvTypeName unwraps a receiver or embedded-field expression to its bare type
// name, dropping pointers and type parameters.
func recvTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return recvTypeName(t.X)
	case *ast.IndexExpr:
		return recvTypeName(t.X)
	case *ast.IndexListExpr:
		return recvTypeName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	default:
		return ""
	}
}

// importsOf maps each import's local name to its path for one file.
func importsOf(f *ast.File) map[string]string {
	out := make(map[string]string, len(f.Imports))
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := path.Base(p)
		if spec.Name != nil {
			if spec.Name.Name == "_" || spec.Name.Name == "." {
				continue
			}
			name = spec.Name.Name
		}
		out[name] = p
	}
	return out
}
