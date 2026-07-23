package schema_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// loadNoErr loads a schema that must complete without errors (warnings are
// allowed) and returns both the schema and the result for warning inspection.
// It lives here, with the other shared annotation helpers, because three of the
// annotation test files depend on it.
func loadNoErr(t *testing.T, source string) (*schema.Schema, diag.Result) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), source, "main.yammm")
	if res.HasErrors() {
		t.Fatalf("schema should load without errors, got: %v", res)
	}
	if s == nil {
		t.Fatal("schema returned nil despite no errors")
	}
	return s, res
}

// loadAnnotationImport writes base as base.yammm and main as main.yammm in a
// temp module root and loads main, so a test can exercise annotation behaviour
// across an import boundary. main should import the other as "./base".
func loadAnnotationImport(t *testing.T, base, main string) (*schema.Schema, diag.Result) {
	t.Helper()
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.yammm")
	for path, content := range map[string]string{
		filepath.Join(dir, "base.yammm"): base,
		mainPath:                         main,
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return schema.Load(t.Context(), mainPath, schema.WithModuleRoot(dir))
}

func schemaType(t *testing.T, s *schema.Schema, name string) *schema.Type {
	t.Helper()
	ty, ok := s.Type(name)
	if !ok {
		t.Fatalf("schema has no type %q", name)
	}
	return ty
}

func typeProperty(t *testing.T, ty *schema.Type, prop string) *schema.Property {
	t.Helper()
	p, ok := ty.Property(prop)
	if !ok {
		t.Fatalf("type %q has no property %q", ty.Name(), prop)
	}
	return p
}

// propAnnNames returns the ordered annotation names on a property.
func propAnnNames(p *schema.Property) []string {
	var names []string
	for _, a := range p.AnnotationsSlice() {
		names = append(names, a.Name())
	}
	return names
}

// typeAnnNames returns the ordered names of a type's linearized annotations.
func typeAnnNames(ty *schema.Type) []string {
	var names []string
	for _, a := range ty.AllAnnotationsSlice() {
		names = append(names, a.Name())
	}
	return names
}

// argTexts returns the ordered arg texts of an annotation.
func argTexts(a *schema.Annotation) []string {
	var texts []string
	for _, arg := range a.Args() {
		texts = append(texts, arg.Text())
	}
	return texts
}

func TestAnnotation_Model_PropertyLevelCollected(t *testing.T) {
	t.Parallel()
	s := loadOK(t, `schema "main"
type Document {
	content_hash String primary
	state String @index
}`)
	p := typeProperty(t, schemaType(t, s, "Document"), "state")
	if got, want := propAnnNames(p), []string{"index"}; !slices.Equal(got, want) {
		t.Fatalf("state annotations: got %v, want %v", got, want)
	}
	a, ok := p.Annotation("index")
	if !ok {
		t.Fatal(`p.Annotation("index") not found`)
	}
	if len(a.Args()) != 0 {
		t.Errorf("@index args: got %d, want 0", len(a.Args()))
	}
	if a.Span().IsZero() {
		t.Error("@index span is zero")
	}
}

func TestAnnotation_Model_MultiplePropertyAnnotationsInOrder(t *testing.T) {
	t.Parallel()
	s := loadOK(t, `schema "main"
type Document {
	content_hash String primary
	state String @index @writeOnce
}`)
	p := typeProperty(t, schemaType(t, s, "Document"), "state")
	if got, want := propAnnNames(p), []string{"index", "writeOnce"}; !slices.Equal(got, want) {
		t.Errorf("state annotations: got %v, want %v", got, want)
	}
}

func TestAnnotation_Model_TypeLevelCompositeCollected(t *testing.T) {
	t.Parallel()
	s := loadOK(t, `schema "main"
type Document {
	content_hash String primary
	state String
	published_on Date
	@@index(state, published_on)
}`)
	ty := schemaType(t, s, "Document")
	anns := ty.AllAnnotationsSlice()
	if len(anns) != 1 {
		t.Fatalf("want 1 type annotation, got %d", len(anns))
	}
	if anns[0].Name() != "index" {
		t.Errorf("name: got %q, want index", anns[0].Name())
	}
	if got, want := argTexts(anns[0]), []string{"state", "published_on"}; !slices.Equal(got, want) {
		t.Errorf("args: got %v, want %v", got, want)
	}
}

func TestAnnotation_Model_VectorArgAndDocComment(t *testing.T) {
	t.Parallel()
	s := loadOK(t, `schema "main"
type Document {
	content_hash String primary
	state String
	embedding Vector[768] @vector(cosine)
	/* composite lookup */
	@@index(content_hash, state)
}`)
	ty := schemaType(t, s, "Document")
	v, ok := typeProperty(t, ty, "embedding").Annotation("vector")
	if !ok {
		t.Fatal(`embedding.Annotation("vector") not found`)
	}
	if got, want := argTexts(v), []string{"cosine"}; !slices.Equal(got, want) {
		t.Errorf("@vector args: got %v, want %v", got, want)
	}
	if ty.AllAnnotationsSlice()[0].Documentation() == "" {
		t.Error("@@index doc comment not collected")
	}
}

func TestAnnotation_Hash_ExcludedFromStructuralHash(t *testing.T) {
	t.Parallel()
	// Annotations do not change what instance data is valid, so they are
	// excluded from the structural hash — otherwise adding @index would
	// invalidate every persisted .ys snapshot whose header hash is cross-checked.
	bare := loadOK(t, `schema "main"
type Document {
	content_hash String primary
	state String
	published_on Date
	embedding Vector[768]
	first_seen Timestamp
}`)
	annotated := loadOK(t, `schema "main"
type Document {
	content_hash String primary
	state String @index
	published_on Date @index
	embedding Vector[768] @vector(cosine)
	first_seen Timestamp @writeOnce
	@@index(state, published_on)
}`)
	if got, want := schema.StructuralHash(annotated), schema.StructuralHash(bare); got != want {
		t.Errorf("annotations must not affect StructuralHash:\n annotated=%s\n bare=%s", got, want)
	}
}

// AnnotationArgKind.String renders each kind as a stable lowercase label, so a
// consumer formatting one — or a test failure message — reads a name, not a bare
// integer.
func TestAnnotationArgKind_String(t *testing.T) {
	t.Parallel()
	cases := map[schema.AnnotationArgKind]string{
		schema.ArgUnvalidated: "unvalidated",
		schema.ArgPropertyRef: "property-ref",
		schema.ArgKeyword:     "keyword",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("AnnotationArgKind(%d).String() = %q, want %q", k, got, want)
		}
	}
	if got := schema.AnnotationArgKind(99).String(); got != "unknown" {
		t.Errorf("out-of-range String() = %q, want %q", got, "unknown")
	}
}
