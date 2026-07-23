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
			if got, _ := DetectContext(annDoc(tt.text), line, char); got != tt.want {
				t.Errorf("DetectContext(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

// TestDetectContext_AnnotationNotInString pins that a whitespace-preceded '@'
// inside a string literal is not treated as an annotation head: completion must
// not offer annotation names while the cursor is inside a string value.
func TestDetectContext_AnnotationNotInString(t *testing.T) {
	t.Parallel()
	text := "schema \"m\"\ntype T {\n\temail Pattern[\" @tag"
	lines := strings.Split(text, "\n")
	line := len(lines) - 1
	char := len(lines[line])
	if got, _ := DetectContext(annDoc(text), line, char); got == AnnotationName || got == AnnotationArgs {
		t.Errorf("annotation context must not fire inside a string literal, got %v", got)
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

// annDocWithState returns a snapshot carrying the per-line brace and
// block-comment state the server recomputes on every change. The
// comment-aware detectors read it, so a test that exercises a multi-line
// comment must supply it.
func annDocWithState(text string) *docstate.Snapshot {
	depths, inComment := docstate.ComputeBraceDepths(text)
	return &docstate.Snapshot{
		Text:      text,
		Version:   1,
		LineState: &docstate.LineState{Version: 1, BraceDepth: depths, InBlockComment: inComment},
	}
}

// An '@' on a continuation line of a block comment is comment text, not an
// annotation head: the scan of the current line alone reads clean, so the
// detector must consult the block-comment state carried into the line. Hover's
// sibling detector already did.
func TestDetectContext_AnnotationNotInMultiLineComment(t *testing.T) {
	t.Parallel()
	text := "schema \"m\"\ntype T {\n\t/* notes\n\t   @ind"
	lines := strings.Split(text, "\n")
	line := len(lines) - 1
	char := len(lines[line])
	if got, _ := DetectContext(annDocWithState(text), line, char); got == AnnotationName || got == AnnotationArgs {
		t.Errorf("annotation context must not fire inside a block comment, got %v", got)
	}
}

// yammm's WS is on the hidden channel, so an annotation needs no whitespace
// before it: `state String@index` is valid and loads clean. Anchoring only on a
// whitespace-preceded '@' left every such annotation without completions, and
// made two adjacent annotations parse into one garbage name.
func TestDetectContext_AnnotationWithoutLeadingWhitespace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		text      string
		want      Context
		wantName  string
		wantPlace schema.AnnotationPlacement
	}{
		{
			"property annotation flush against the datatype",
			"schema \"m\"\ntype T {\n\tstate String@ind",
			AnnotationName, "ind", schema.PlacementProperty,
		},
		{
			"argument list flush against the datatype",
			"schema \"m\"\ntype T {\n\te Vector[8]@vector(cos",
			AnnotationArgs, "vector", schema.PlacementProperty,
		},
		{
			"second of two adjacent annotations",
			"schema \"m\"\ntype T {\n\tstate String @index@vector(cos",
			AnnotationArgs, "vector", schema.PlacementProperty,
		},
		{
			"type annotation flush against the datatype keeps its placement",
			"schema \"m\"\ntype T {\n\tstate String@@ind",
			AnnotationName, "ind", schema.PlacementType,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lines := strings.Split(tt.text, "\n")
			line := len(lines) - 1
			char := len(lines[line])
			got, ann := DetectContext(annDoc(tt.text), line, char)
			if got != tt.want {
				t.Errorf("DetectContext(%q) = %v, want %v", lines[line], got, tt.want)
			}
			if ann.name != tt.wantName {
				t.Errorf("annotation name = %q, want %q", ann.name, tt.wantName)
			}
			if ann.placement != tt.wantPlace {
				t.Errorf("annotation placement = %v, want %v", ann.placement, tt.wantPlace)
			}
		})
	}
}
