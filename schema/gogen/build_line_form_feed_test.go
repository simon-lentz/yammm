package gogen_test

import (
	"fmt"
	"go/build"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMarshal_ABuildLineHoldingAFormFeedIsGofmtFormatted pins the generated
// comment for a doc line that Go would read as a build line and that holds a
// form feed. go/printer counts a form feed in a comment as a line end, so a
// "/* */" comment holding one draws the next comment, or the field it
// documents, onto its own line; the form feed is written as a space there.
// gofmt, go/build, go/parser and go vet judge each file, and a form feed in a
// line that is no build line stays as written.
func TestMarshal_ABuildLineHoldingAFormFeedIsGofmtFormatted(t *testing.T) {
	const body = "\tid String primary\n\tname String\n"
	typeDoc := func(content string) string {
		return "schema \"g\"\n\n/* " + content + " */\ntype Car {\n" + body + "}\n"
	}
	propertyDoc := func(content string) string {
		return "schema \"g\"\n\ntype Car {\n\tid String primary\n\t/* " + content + " */\n\tname String\n}\n"
	}
	cases := map[string]struct {
		src, holder string
		// written is a run of the generated file, from the comment group into
		// the declaration it documents, as written, and text the doc go/ast
		// reads from it, each line without its indentation.
		written, text string
	}{
		"a type's build line, then a line": {
			typeDoc("\f+build ignore\n   tail."), "Car",
			"/* +build ignore*/\n// tail.\ntype Car struct {", "+build ignore\ntail.",
		},
		"a type's build line, last": {
			typeDoc("Car.\n   \f+build ignore"), "Car",
			"// Car.\n/* +build ignore*/\ntype Car struct {", "Car.\n+build ignore",
		},
		"a property's build line, then a line": {
			propertyDoc("\f+build ignore\n\t   tail."), "Name",
			"\t/* +build ignore*/\n\t// tail.\n\tName ", "+build ignore\ntail.",
		},
		"a property's build line, last": {
			propertyDoc("Name.\n\t   \f+build ignore"), "Name",
			"\t// Name.\n\t/* +build ignore*/\n\tName ", "Name.\n+build ignore",
		},
		"a property's build line, alone, the form feed inside": {
			propertyDoc("+build a\fb"), "Name",
			"\t/*+build a b*/\n\tName ", "+build a b",
		},
		"two form feeds": {
			propertyDoc("+build a\f\fb\n\t   tail."), "Name",
			"\t/*+build a  b*/\n\t// tail.\n\tName ", "+build a  b\ntail.",
		},
		"a line vet reads as a //go:build line written with a space": {
			typeDoc("Car.\n   go:build x\f//go:build y\n   tail."), "Car",
			"// Car.\n/*go:build x //go:build y*/\n// tail.\ntype Car struct {", "Car.\ngo:build x //go:build y\ntail.",
		},
		"two build lines, each holding a form feed": {
			propertyDoc("\f+build a\n\t   +build\fb\n\t   tail."), "Name",
			"\t/* +build a*/\n\t/*+build b*/\n\t// tail.\n\tName ", "+build a\n+build b\ntail.",
		},
		"a type's line that is no build line keeps its form feed": {
			typeDoc("a\fb\n   +build ignore\n   \fc\n   tail."), "Car",
			"// a\fb\n/*+build ignore*/\n// \fc\n// tail.\ntype Car struct {", "a\fb\n+build ignore\nc\ntail.",
		},
		"a property's line that is no build line keeps its form feed": {
			propertyDoc("a\fb\n\t   +build ignore\n\t   c\fd"), "Name",
			"\t// a\fb\n\t/*+build ignore*/\n\t// c\fd\n\tName ", "a\fb\n+build ignore\nc\fd",
		},
		"a vertical tab in a build line stays": {
			propertyDoc("+build a\vb\n\t   tail."), "Name",
			"\t/*+build a\vb*/\n\t// tail.\n\tName ", "+build a\vb\ntail.",
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
			if !strings.Contains(out, tc.written) {
				t.Errorf("the generated file does not hold %q:\n%q", tc.written, out)
			}
			assertGofmtLeaves(t, out)
			if match, err := build.Default.MatchFile(dir, "car.go"); err != nil || !match {
				t.Errorf("go/build leaves the generated file out (match %v, %v)", match, err)
			}
			f, err := parser.ParseFile(token.NewFileSet(), "car.go", out, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			if got := unindented(docOf(f, tc.holder)); got != tc.text {
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
}

// assertGofmtLeaves fails unless gofmt writes src back byte for byte.
func assertGofmtLeaves(t *testing.T, src string) {
	t.Helper()
	formatted, err := format.Source([]byte(src))
	if err != nil {
		t.Fatalf("gofmt: %v", err)
	}
	if string(formatted) != src {
		t.Errorf("gofmt rewrites the generated file:\n got %q\nwant %q", formatted, src)
	}
}
