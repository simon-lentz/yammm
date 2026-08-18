package doclint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
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

// Load parses every Go file under root, skipping directories that hold no
// source a consumer reads: testdata, node_modules, and anything whose name
// starts with "." or "_". Files that do not parse are an error rather than a
// skip — a gate that quietly drops the file it cannot read reports a clean run
// over nothing.
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
		pkg, found, err := loadDir(fset, root, p)
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

// loadDir parses dir's Go files, reporting false when it holds none.
func loadDir(fset *token.FileSet, root, dir string) (pkg *Package, found bool, err error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, false, fmt.Errorf("reading %s: %w", dir, err)
	}
	pkg = &Package{fset: fset, names: make(map[string]struct{})}
	var testName string
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		full := filepath.Join(dir, e.Name())
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
