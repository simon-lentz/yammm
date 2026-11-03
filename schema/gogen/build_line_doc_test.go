package gogen_test

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMarshal_ADocHoldingABuildLineStaysInEveryBuild pins that a schema doc
// line Go would read as a "+build" constraint neither leaves the generated
// file out of a build nor loses its text. go/build decides inclusion as the go
// command does, go/parser reads the declaration's doc comment, and go vet runs
// the checks a consumer's go test runs, so no judge is the generator's own.
func TestMarshal_ADocHoldingABuildLineStaysInEveryBuild(t *testing.T) {
	const body = "\tid String primary\n\tname String\n"
	cases := map[string]struct {
		src, holder, text string
	}{
		"type doc": {
			"schema \"g\"\n\n/* +build ignore */\ntype Car {\n" + body + "}\n",
			"Car", "+build ignore",
		},
		"property doc": {
			"schema \"g\"\n\ntype Car {\n\tid String primary\n\t/* +build ignore */\n\tname String\n}\n",
			"Name", "+build ignore",
		},
		"a later line of a doc": {
			"schema \"g\"\n\n/* Car is a car.\n   +build linux,!linux */\ntype Car {\n" + body + "}\n",
			"Car", "Car is a car.\n+build linux,!linux",
		},
		"a bare +build line": {
			"schema \"g\"\n\n/* +build */\ntype Car {\n" + body + "}\n",
			"Car", "+build",
		},
		"a //go:build line alone stays a line comment": {
			"schema \"g\"\n\n/* Car.\n   //go:build ignore */\ntype Car {\n" + body + "}\n",
			"Car", "Car.\n//go:build ignore",
		},
		"a property's +build line beside a build line": {
			"schema \"g\"\n\ntype Car {\n\tid String primary\n\t/* +build linux\n\t   // +build ignore */\n\tname String\n}\n",
			"Name", "+build linux\n// +build ignore",
		},
		"a type's +build line beside a //go:build line": {
			"schema \"g\"\n\n/* Car.\n   +build linux\n   //go:build ignore */\ntype Car {\n" + body + "}\n",
			"Car", "Car.\n+build linux\n//go:build ignore",
		},
		"a type's +build line beside a // +build line": {
			"schema \"g\"\n\n/* +build linux\n   // +build ignore */\ntype Car {\n" + body + "}\n",
			"Car", "+build linux\n// +build ignore",
		},
		"a line vet reads as a //go:build line written with a space": {
			"schema \"g\"\n\n/* Car.\n   go:build x //go:build y */\ntype Car {\n" + body + "}\n",
			"Car", "Car.\ngo:build x //go:build y",
		},
	}
	module := t.TempDir()
	if err := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module probe\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pkg := 0
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			out := marshalString(t, tc.src)
			pkg++
			dir := filepath.Join(module, fmt.Sprintf("p%d", pkg))
			if err := os.Mkdir(dir, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "car.go"), []byte(out), 0o600); err != nil {
				t.Fatal(err)
			}
			if match, err := build.Default.MatchFile(dir, "car.go"); err != nil || !match {
				t.Errorf("go/build leaves the generated file out (match %v, %v):\n%s", match, err, out)
			}
			f, err := parser.ParseFile(token.NewFileSet(), "car.go", out, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			for _, group := range f.Comments {
				for _, c := range group.List {
					if constraint.IsPlusBuild(c.Text) || constraint.IsGoBuild(c.Text) {
						t.Errorf("the output holds the build line %q", c.Text)
					}
				}
			}
			if got := docOf(f, tc.holder); unindented(got) != tc.text {
				t.Errorf("%s's doc = %q, want %q", tc.holder, got, tc.text)
			}
		})
	}

	vet := exec.CommandContext(t.Context(), "go", "vet", "./...")
	vet.Dir = module
	vet.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	if out, err := vet.CombinedOutput(); err != nil {
		t.Errorf("go vet over the generated files: %v\n%s", err, out)
	}

	out := marshalString(t, "schema \"g\"\n\n/* Car is a car.\n   +buildx is no build line */\ntype Car {\n"+body+"}\n")
	assertHolds(t, out, []string{"// Car is a car.\n// +buildx is no build line\n"}, []string{"/*\nCar is a car."})
	out = marshalString(t, "schema \"g\"\n\n/* Car.\n   //go:build ignore\n   +build linux */\ntype Car {\n"+body+"}\n")
	assertHolds(t, out, []string{"// Car.\n// //go:build ignore\n/*+build linux*/\n"}, nil)
}

// unindented returns doc's lines without the indentation gofmt gives a block
// comment inside a struct, and without its trailing line break.
func unindented(doc string) string {
	lines := strings.Split(strings.TrimSpace(doc), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.Join(lines, "\n")
}

// docOf returns the doc comment text of the type or field named name.
func docOf(f *ast.File, name string) string {
	var doc string
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.GenDecl:
			for _, spec := range x.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.Name == name && x.Doc != nil {
					doc = x.Doc.Text()
				}
			}
		case *ast.Field:
			for _, id := range x.Names {
				if id.Name == name && x.Doc != nil {
					doc = x.Doc.Text()
				}
			}
		}
		return true
	})
	return doc
}
