package gogen_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/gogen"
)

// TestMarshal_GraphKeysAreTheTagsTheEntrySchemaAddresses holds gogen's Graph
// keys to the type tags a JSON instance document is read by: one key per
// concrete, non-part type the entry schema can name, spelled as
// [schema.AddressableTag] spells it, and each key a tag the entry schema
// resolves to a concrete type.
func TestMarshal_GraphKeysAreTheTagsTheEntrySchemaAddresses(t *testing.T) {
	var files []string
	for _, pattern := range []string{"testdata/*.yammm", "testdata/*/*.yammm"} {
		m, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	if len(files) < 40 {
		t.Fatalf("found %d fixtures; the globs no longer reach the corpus", len(files))
	}

	unnameable := 0
	for _, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil {
			t.Fatal(err)
		}
		s, res := schema.Load(context.Background(), abs)
		if res.HasErrors() {
			t.Fatalf("load %s: %v", f, res.Err())
		}
		var want []string
		for sc := range s.Closure() {
			for typ := range sc.Types() {
				if typ.IsAbstract() || typ.IsPart() {
					continue
				}
				if tag, ok := schema.AddressableTag(s, typ.ID()); ok {
					want = append(want, tag)
				} else {
					unnameable++
				}
			}
		}
		slices.Sort(want)

		t.Run(f, func(t *testing.T) {
			src, err := gogen.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			graph := graphKeys(t, src)
			if !slices.Equal(graph, want) {
				t.Errorf("Graph keys %q, want the entry schema's tags %q", graph, want)
			}
			for _, k := range graph {
				typ, ok := s.ResolveTypeName(k)
				if !ok || typ.IsAbstract() || typ.IsPart() {
					t.Errorf("Graph key %q does not resolve to a concrete type", k)
				}
			}
		})
	}
	// Without a concrete type the entry schema cannot name, keying every type
	// would pass.
	if unnameable == 0 {
		t.Fatal("no fixture holds a concrete type its entry schema cannot name")
	}
}

// graphKeys returns the sorted json keys of the Graph struct in src.
func graphKeys(t *testing.T, src []byte) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "gen.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok || spec.Name.Name != "Graph" {
			return true
		}
		st, ok := spec.Type.(*ast.StructType)
		if !ok {
			t.Fatalf("Graph is %T, not a struct", spec.Type)
		}
		found = true
		for _, field := range st.Fields.List {
			if field.Tag == nil {
				t.Fatalf("Graph field %v carries no tag", field.Names)
			}
			tag, err := strconv.Unquote(field.Tag.Value)
			if err != nil {
				t.Fatal(err)
			}
			name, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
			keys = append(keys, name)
		}
		return false
	})
	if !found {
		t.Fatal("no Graph struct in the generated source")
	}
	slices.Sort(keys)
	return keys
}
