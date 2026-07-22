package completion

import (
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/schema"
)

func annDoc(text string) *docstate.Snapshot {
	return &docstate.Snapshot{Text: text}
}

func completionLabels(items []protocol.CompletionItem) []string {
	labels := make([]string, len(items))
	for i, it := range items {
		labels[i] = it.Label
	}
	return labels
}

func TestDetectContext_Annotation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want Context
	}{
		{"property @ start", "schema \"m\"\ntype T {\n\tstate String @", AnnotationName},
		{"property @ midword", "schema \"m\"\ntype T {\n\tstate String @ind", AnnotationName},
		{"type @@ start", "schema \"m\"\ntype T {\n\t@@", AnnotationName},
		{"type @@ midword", "schema \"m\"\ntype T {\n\t@@ind", AnnotationName},
		{"vector args open", "schema \"m\"\ntype T {\n\te Vector[8] @vector(", AnnotationArgs},
		{"vector args midword", "schema \"m\"\ntype T {\n\te Vector[8] @vector(cos", AnnotationArgs},
		{"vector args open with trailing space", "schema \"m\"\ntype T {\n\te Vector[8] @vector( ", AnnotationArgs},
		{"closed args fall through to type body", "schema \"m\"\ntype T {\n\te Vector[8] @vector(cosine)", TypeBody},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// The cursor sits at end of the last line; char is that line's byte length.
			lines := strings.Split(tt.text, "\n")
			line := len(lines) - 1
			char := len(lines[line])
			if got := DetectContext(annDoc(tt.text), line, char); got != tt.want {
				t.Errorf("DetectContext(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestAnnotationNameCompletions_Property(t *testing.T) {
	t.Parallel()
	labels := completionLabels(AnnotationNameCompletions(schema.PlacementProperty))
	for _, want := range []string{"index", "vector", "writeOnce"} {
		if !slices.Contains(labels, want) {
			t.Errorf("property annotation completions missing %q; got %v", want, labels)
		}
	}
}

func TestAnnotationNameCompletions_Type(t *testing.T) {
	t.Parallel()
	labels := completionLabels(AnnotationNameCompletions(schema.PlacementType))
	if !slices.Contains(labels, "index") {
		t.Errorf("type annotation completions should include index; got %v", labels)
	}
	if slices.Contains(labels, "writeOnce") {
		t.Errorf("type annotation completions should not include property-only writeOnce; got %v", labels)
	}
}

func TestAnnotationArgCompletions_Vector(t *testing.T) {
	t.Parallel()
	labels := completionLabels(AnnotationArgCompletions("vector"))
	for _, want := range []string{"cosine", "euclidean"} {
		if !slices.Contains(labels, want) {
			t.Errorf("@vector arg completions missing %q; got %v", want, labels)
		}
	}
	if got := AnnotationArgCompletions("index"); len(got) != 0 {
		t.Errorf("non-@vector annotation should offer no keyword completions, got %v", completionLabels(got))
	}
}
