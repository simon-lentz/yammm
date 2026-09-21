package gogen_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/adapter/gogen"
	"github.com/simon-lentz/yammm/schema"
)

// TestMarshal_WithPackageNameRefusesAnInvalidName pins that a name no package
// clause can hold is refused as the caller's input, with ErrInvalidPackageName
// and the option named, before any source is generated.
func TestMarshal_WithPackageNameRefusesAnInvalidName(t *testing.T) {
	s := loadSchema(t, "scalars")
	for _, name := range []string{"my-model", "type", "2models", "_", "go.pkg", "a b"} {
		t.Run(name, func(t *testing.T) {
			got, err := gogen.Marshal(s, gogen.WithPackageName(name))
			if !errors.Is(err, gogen.ErrInvalidPackageName) {
				t.Fatalf("Marshal(WithPackageName(%q)) error = %v, want ErrInvalidPackageName", name, err)
			}
			if got != nil {
				t.Errorf("Marshal returned %d bytes with its error", len(got))
			}
			if !strings.HasPrefix(err.Error(), "gogen: invalid package name: ") {
				t.Errorf("error %q does not lead with the sentinel's text", err)
			}
			if want := "WithPackageName(" + `"` + name + `"` + ")"; !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not name %s", err, want)
			}
		})
	}
}

// TestMarshal_WithPackageNameAdmitsAValidName pins the other side of the
// rule: an explicit "main" is legal, since a caller may generate into a
// program's own directory, and so is any identifier that is not a keyword and
// not "_".
func TestMarshal_WithPackageNameAdmitsAValidName(t *testing.T) {
	s := loadSchema(t, "scalars")
	for _, name := range []string{"main", "init", "gen", "Model", "model_v2", "ñame", "_x", "string"} {
		t.Run(name, func(t *testing.T) {
			got, err := gogen.Marshal(s, gogen.WithPackageName(name))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(got, []byte("\npackage "+name+"\n")) {
				t.Errorf("output does not declare package %s", name)
			}
		})
	}
}

// TestMarshal_DerivedPackageNameIsNeverMain pins that a schema named "Main"
// derives "main_": a file of declarations alone cannot build as package main.
func TestMarshal_DerivedPackageNameIsNeverMain(t *testing.T) {
	s, res := schema.LoadString(context.Background(),
		"schema \"Main\"\n\ntype County {\n\tid String primary\n}\n", "main.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("\npackage main_\n")) {
		t.Errorf("a schema named Main does not derive package main_:\n%s", got[:min(len(got), 200)])
	}
}
