package gogen_test

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/adapter/gogen"
	"github.com/simon-lentz/yammm/adapter/jschema"
	"github.com/simon-lentz/yammm/schema"
)

// TestMarshal_GraphKeysMatchTheJSONSchemaEnvelope holds gogen's Graph keys to
// adapter/jschema's envelope keys over both packages' fixtures: the two
// generators describe one document, so they must name one set of top-level
// keys, and every key must be a tag the entry schema resolves to a concrete
// type.
func TestMarshal_GraphKeysMatchTheJSONSchemaEnvelope(t *testing.T) {
	var files []string
	for _, pattern := range []string{
		"testdata/*.yammm", "testdata/*/*.yammm",
		"../jschema/testdata/*.yammm", "../jschema/testdata/*/*.yammm",
	} {
		m, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	if len(files) < 40 {
		t.Fatalf("found %d fixtures; the globs no longer reach both corpora", len(files))
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
		for _, sc := range s.Closure() {
			for _, typ := range sc.TypesSlice() {
				if !typ.IsAbstract() && !typ.IsPart() && !schema.Addressable(s, typ.ID()) {
					unnameable++
				}
			}
		}

		t.Run(f, func(t *testing.T) {
			src, err := gogen.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := jschema.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			graph := graphKeys(t, src)
			envelope := envelopeKeys(t, doc)
			if !slices.Equal(graph, envelope) {
				t.Errorf("Graph keys %q, envelope keys %q", graph, envelope)
			}
			for _, k := range graph {
				typ, ok := s.ResolveTypeName(k)
				if !ok || typ.IsAbstract() || typ.IsPart() {
					t.Errorf("Graph key %q does not resolve to a concrete type", k)
				}
			}
		})
	}
	// Without a concrete type the entry schema cannot name, the two
	// generators could agree by both keying every type.
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

// envelopeKeys returns the sorted top-level property names of a JSON Schema
// document.
func envelopeKeys(t *testing.T, doc []byte) []string {
	t.Helper()
	var envelope struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(doc, &envelope); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(envelope.Properties))
	for k := range envelope.Properties {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
