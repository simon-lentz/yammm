package diag

import (
	"log/slog"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

func TestIssue_Accessors(t *testing.T) {
	source := location.MustNewSourceID("test://schema.yammm")
	span := location.Point(source, 10, 5)
	related := []location.RelatedInfo{
		{Span: location.Point(source, 5, 1), Message: "previous definition here"},
	}
	details := []Detail{
		{Key: DetailKeyTypeName, Value: "Person"},
	}

	issue := Issue{
		span:       span,
		sourceName: "data.json",
		path:       "$.items[0].name",
		severity:   Error,
		code:       E_TYPE_COLLISION,
		message:    "type collision detected",
		hint:       "rename one of the types",
		related:    related,
		details:    details,
	}

	if got := issue.Severity(); got != Error {
		t.Errorf("Severity() = %v; want %v", got, Error)
	}
	if got := issue.Code(); got != E_TYPE_COLLISION {
		t.Errorf("Code() = %v; want %v", got, E_TYPE_COLLISION)
	}
	if got := issue.Message(); got != "type collision detected" {
		t.Errorf("Message() = %q; want %q", got, "type collision detected")
	}
	if got := issue.Span(); got != span {
		t.Errorf("Span() = %v; want %v", got, span)
	}
	if got := issue.SourceName(); got != "data.json" {
		t.Errorf("SourceName() = %q; want %q", got, "data.json")
	}
	if got := issue.Path(); got != "$.items[0].name" {
		t.Errorf("Path() = %q; want %q", got, "$.items[0].name")
	}
	if got := issue.Hint(); got != "rename one of the types" {
		t.Errorf("Hint() = %q; want %q", got, "rename one of the types")
	}
}

func TestIssue_HasSpan(t *testing.T) {
	source := location.MustNewSourceID("test://schema.yammm")

	tests := []struct {
		name  string
		issue Issue
		want  bool
	}{
		{
			name:  "zero issue",
			issue: Issue{},
			want:  false,
		},
		{
			name: "issue with span",
			issue: Issue{
				span:     location.Point(source, 1, 1),
				severity: Error,
				code:     E_SYNTAX,
				message:  "test",
			},
			want: true,
		},
		{
			name: "issue without span",
			issue: Issue{
				sourceName: "data.json",
				path:       "$.x",
				severity:   Error,
				code:       E_TYPE_MISMATCH,
				message:    "test",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.issue.HasSpan(); got != tt.want {
				t.Errorf("HasSpan() = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestIssue_IsZero(t *testing.T) {
	source := location.MustNewSourceID("test://schema.yammm")

	tests := []struct {
		name  string
		issue Issue
		want  bool
	}{
		{
			name:  "zero value",
			issue: Issue{},
			want:  true,
		},
		{
			name: "only code set",
			issue: Issue{
				code: E_SYNTAX,
			},
			want: false,
		},
		{
			name: "only message set",
			issue: Issue{
				message: "test",
			},
			want: false,
		},
		{
			name: "only span set",
			issue: Issue{
				span: location.Point(source, 1, 1),
			},
			want: false,
		},
		{
			name: "only sourceName set",
			issue: Issue{
				sourceName: "data.json",
			},
			want: false,
		},
		{
			name: "only path set",
			issue: Issue{
				path: "$.x",
			},
			want: false,
		},
		{
			name: "full issue",
			issue: Issue{
				span:     location.Point(source, 1, 1),
				severity: Error,
				code:     E_SYNTAX,
				message:  "test",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.issue.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestIssue_IsValid(t *testing.T) {
	tests := []struct {
		name  string
		issue Issue
		want  bool
	}{
		{
			name:  "zero value",
			issue: Issue{},
			want:  false,
		},
		{
			name: "only code set",
			issue: Issue{
				code: E_SYNTAX,
			},
			want: false,
		},
		{
			name: "only message set",
			issue: Issue{
				message: "test",
			},
			want: false,
		},
		{
			name: "code and message set",
			issue: Issue{
				code:    E_SYNTAX,
				message: "test",
			},
			want: true,
		},
		{
			name: "full issue",
			issue: Issue{
				severity: Error,
				code:     E_SYNTAX,
				message:  "test",
			},
			want: true,
		},
		{
			name: "invalid severity (255)",
			issue: Issue{
				severity: Severity(255),
				code:     E_SYNTAX,
				message:  "test",
			},
			want: false,
		},
		{
			name: "invalid severity (6)",
			issue: Issue{
				severity: Severity(6),
				code:     E_SYNTAX,
				message:  "test",
			},
			want: false,
		},
		{
			name: "highest valid severity (Hint)",
			issue: Issue{
				severity: Hint,
				code:     E_SYNTAX,
				message:  "test",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.issue.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestIssue_ProvenanceClassification(t *testing.T) {
	source := location.MustNewSourceID("test://schema.yammm")
	span := location.Point(source, 1, 1)

	tests := []struct {
		name           string
		issue          Issue
		wantSchemaOnly bool
		wantInstOnly   bool
		wantHybrid     bool
	}{
		{
			name:           "zero issue",
			issue:          Issue{},
			wantSchemaOnly: false,
			wantInstOnly:   false,
			wantHybrid:     false,
		},
		{
			name: "schema only (span, no path)",
			issue: Issue{
				span:     span,
				severity: Error,
				code:     E_SYNTAX,
				message:  "test",
			},
			wantSchemaOnly: true,
			wantInstOnly:   false,
			wantHybrid:     false,
		},
		{
			name: "instance only (path, no span)",
			issue: Issue{
				sourceName: "data.json",
				path:       "$.x",
				severity:   Error,
				code:       E_TYPE_MISMATCH,
				message:    "test",
			},
			wantSchemaOnly: false,
			wantInstOnly:   true,
			wantHybrid:     false,
		},
		{
			name: "hybrid (both span and path)",
			issue: Issue{
				span:       span,
				sourceName: "data.json",
				path:       "$.x",
				severity:   Error,
				code:       E_TYPE_MISMATCH,
				message:    "test",
			},
			wantSchemaOnly: false,
			wantInstOnly:   false,
			wantHybrid:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.issue.IsSchemaOnly(); got != tt.wantSchemaOnly {
				t.Errorf("IsSchemaOnly() = %v; want %v", got, tt.wantSchemaOnly)
			}
			if got := tt.issue.IsInstanceOnly(); got != tt.wantInstOnly {
				t.Errorf("IsInstanceOnly() = %v; want %v", got, tt.wantInstOnly)
			}
			if got := tt.issue.IsHybrid(); got != tt.wantHybrid {
				t.Errorf("IsHybrid() = %v; want %v", got, tt.wantHybrid)
			}
		})
	}
}

func TestIssue_Related_DefensiveCopy(t *testing.T) {
	source := location.MustNewSourceID("test://schema.yammm")
	original := []location.RelatedInfo{
		{Span: location.Point(source, 5, 1), Message: "original"},
	}

	issue := Issue{
		severity: Error,
		code:     E_SYNTAX,
		message:  "test",
		related:  original,
	}

	// Get a copy and modify it
	copy1 := issue.Related()
	copy1[0].Message = "modified"

	// Get another copy - should still have original value
	copy2 := issue.Related()
	if copy2[0].Message != "original" {
		t.Errorf("Related() returned reference, not copy; got %q, want %q",
			copy2[0].Message, "original")
	}

	// Original should be unchanged
	if original[0].Message != "original" {
		t.Error("original slice was modified")
	}
}

func TestIssue_Related_NilForEmpty(t *testing.T) {
	issue := Issue{
		severity: Error,
		code:     E_SYNTAX,
		message:  "test",
	}

	if got := issue.Related(); got != nil {
		t.Errorf("Related() = %v; want nil for empty", got)
	}
}

func TestIssue_Details_DefensiveCopy(t *testing.T) {
	original := []Detail{
		{Key: DetailKeyTypeName, Value: "original"},
	}

	issue := Issue{
		severity: Error,
		code:     E_SYNTAX,
		message:  "test",
		details:  original,
	}

	// Get a copy and modify it
	copy1 := issue.Details()
	copy1[0].Value = "modified"

	// Get another copy - should still have original value
	copy2 := issue.Details()
	if copy2[0].Value != "original" {
		t.Errorf("Details() returned reference, not copy; got %q, want %q",
			copy2[0].Value, "original")
	}

	// Original should be unchanged
	if original[0].Value != "original" {
		t.Error("original slice was modified")
	}
}

func TestIssue_Details_NilForEmpty(t *testing.T) {
	issue := Issue{
		severity: Error,
		code:     E_SYNTAX,
		message:  "test",
	}

	if got := issue.Details(); got != nil {
		t.Errorf("Details() = %v; want nil for empty", got)
	}
}

func TestIssue_Clone(t *testing.T) {
	source := location.MustNewSourceID("test://schema.yammm")
	original := Issue{
		span:       location.Point(source, 10, 5),
		sourceName: "data.json",
		path:       "$.x",
		severity:   Error,
		code:       E_TYPE_COLLISION,
		message:    "original message",
		hint:       "original hint",
		related: []location.RelatedInfo{
			{Span: location.Point(source, 5, 1), Message: "related"},
		},
		details: []Detail{
			{Key: DetailKeyTypeName, Value: "Person"},
		},
	}

	clone := original.Clone()

	// Verify all fields are equal
	if clone.Severity() != original.Severity() {
		t.Error("Clone severity mismatch")
	}
	if clone.Code() != original.Code() {
		t.Error("Clone code mismatch")
	}
	if clone.Message() != original.Message() {
		t.Error("Clone message mismatch")
	}
	if clone.Span() != original.Span() {
		t.Error("Clone span mismatch")
	}
	if clone.SourceName() != original.SourceName() {
		t.Error("Clone sourceName mismatch")
	}
	if clone.Path() != original.Path() {
		t.Error("Clone path mismatch")
	}
	if clone.Hint() != original.Hint() {
		t.Error("Clone hint mismatch")
	}

	// Verify slices are independent
	cloneRelated := clone.Related()
	originalRelated := original.Related()
	if len(cloneRelated) != len(originalRelated) {
		t.Error("Clone related length mismatch")
	}

	// Modify clone's internal slices (if we could access them)
	// Since we can't, modify via Clone's returned slices
	cloneRelated[0].Message = "modified"
	if original.Related()[0].Message == "modified" {
		t.Error("Clone's related slice shares backing array with original")
	}

	cloneDetails := clone.Details()
	cloneDetails[0].Value = "modified"
	if original.Details()[0].Value == "modified" {
		t.Error("Clone's details slice shares backing array with original")
	}
}

func TestIssue_Clone_EmptySlices(t *testing.T) {
	original := Issue{
		severity: Error,
		code:     E_SYNTAX,
		message:  "test",
	}

	clone := original.Clone()

	if clone.Related() != nil {
		t.Error("Clone of issue with no related should have nil related")
	}
	if clone.Details() != nil {
		t.Error("Clone of issue with no details should have nil details")
	}
}

// ---- Issue.LogValue ----
//
// Paired with ContextualError.LogValue but tested here so the per-issue
// attribute-shape pins live next to the surface they describe. The map
// helper (issueLogMap) shares the shape; covering the slog form covers
// both.

func logValueAttrs(t *testing.T, v slog.Value) []slog.Attr {
	t.Helper()
	if v.Kind() != slog.KindGroup {
		t.Fatalf("expected group value, got kind %v", v.Kind())
	}
	return v.Group()
}

func issueLogAttr(t *testing.T, v slog.Value, key string) (slog.Value, bool) {
	t.Helper()
	for _, a := range logValueAttrs(t, v) {
		if a.Key == key {
			return a.Value, true
		}
	}
	return slog.Value{}, false
}

func TestIssue_LogValue_MinimalIssue(t *testing.T) {
	// severity + message + code only; span/path/hint/details omitted.
	issue := NewIssue(Error, E_SYNTAX, "bare issue").Build()
	val := issue.LogValue()
	attrs := logValueAttrs(t, val)

	// Required keys.
	sv, ok := issueLogAttr(t, val, "severity")
	if !ok || sv.String() != "error" {
		t.Errorf("severity = %v (ok=%v), want \"error\"", sv.String(), ok)
	}
	mv, ok := issueLogAttr(t, val, "message")
	if !ok || mv.String() != "bare issue" {
		t.Errorf("message = %v (ok=%v), want \"bare issue\"", mv.String(), ok)
	}
	cv, ok := issueLogAttr(t, val, "code")
	if !ok || cv.String() != E_SYNTAX.String() {
		t.Errorf("code = %v (ok=%v)", cv.String(), ok)
	}

	// Omitted keys.
	for _, key := range []string{"path", "location", "hint", "details"} {
		if _, found := issueLogAttr(t, val, key); found {
			t.Errorf("attribute %q present on minimal issue, want omitted", key)
		}
	}

	if len(attrs) != 3 {
		t.Errorf("attr count = %d, want 3 (severity, message, code)", len(attrs))
	}
}

func TestIssue_LogValue_FullIssue(t *testing.T) {
	source := location.MustNewSourceID("test://schema.yammm")
	issue := NewIssue(Error, E_TYPE_COLLISION, "full issue").
		WithSpan(location.Point(source, 7, 3)).
		WithPath("data.json", "$.Widget[0].name").
		WithHint("rename it").
		WithDetails(
			Detail{Key: DetailKeyTypeName, Value: "Widget"},
			Detail{Key: DetailKeyPropertyName, Value: "name"},
		).
		Build()
	val := issue.LogValue()

	if sv, _ := issueLogAttr(t, val, "severity"); sv.String() != "error" {
		t.Errorf("severity = %q, want error", sv.String())
	}
	if mv, _ := issueLogAttr(t, val, "message"); mv.String() != "full issue" {
		t.Errorf("message = %q", mv.String())
	}
	if cv, _ := issueLogAttr(t, val, "code"); cv.String() != E_TYPE_COLLISION.String() {
		t.Errorf("code = %q", cv.String())
	}
	if pv, _ := issueLogAttr(t, val, "path"); pv.String() != "$.Widget[0].name" {
		t.Errorf("path = %q", pv.String())
	}
	if hv, _ := issueLogAttr(t, val, "hint"); hv.String() != "rename it" {
		t.Errorf("hint = %q", hv.String())
	}

	loc, ok := issueLogAttr(t, val, "location")
	if !ok || loc.Kind() != slog.KindGroup {
		t.Fatalf("location group missing or wrong kind: %v", loc.Kind())
	}
	srcVal, _ := issueLogAttr(t, loc, "source")
	if srcVal.String() != source.String() {
		t.Errorf("location.source = %q, want %q", srcVal.String(), source.String())
	}
	lineVal, _ := issueLogAttr(t, loc, "line")
	if lineVal.Int64() != 7 {
		t.Errorf("location.line = %d, want 7", lineVal.Int64())
	}
	colVal, _ := issueLogAttr(t, loc, "column")
	if colVal.Int64() != 3 {
		t.Errorf("location.column = %d, want 3", colVal.Int64())
	}

	det, ok := issueLogAttr(t, val, "details")
	if !ok || det.Kind() != slog.KindGroup {
		t.Fatalf("details group missing or wrong kind: %v", det.Kind())
	}
	typeVal, _ := issueLogAttr(t, det, DetailKeyTypeName)
	if typeVal.String() != "Widget" {
		t.Errorf("details.type = %q, want Widget", typeVal.String())
	}
	propVal, _ := issueLogAttr(t, det, DetailKeyPropertyName)
	if propVal.String() != "name" {
		t.Errorf("details.property = %q, want name", propVal.String())
	}
}

func TestIssue_LogValue_OmitsEmptyFields(t *testing.T) {
	// Instance-only issue (path but no span): assert path is present,
	// location is absent. Hint left empty.
	issue := NewIssue(Warning, E_TYPE_COLLISION, "instance-only").
		WithPath("source", "$.items[0]").
		Build()
	val := issue.LogValue()
	if _, ok := issueLogAttr(t, val, "path"); !ok {
		t.Error("instance-only issue missing path attribute")
	}
	if _, ok := issueLogAttr(t, val, "location"); ok {
		t.Error("instance-only issue emitted location group, want omitted")
	}
	if _, ok := issueLogAttr(t, val, "hint"); ok {
		t.Error("issue without hint emitted hint attribute, want omitted")
	}
	if _, ok := issueLogAttr(t, val, "details"); ok {
		t.Error("issue without details emitted details group, want omitted")
	}
}

func TestIssue_LogValue_SchemaOnly_OmitsPath(t *testing.T) {
	source := location.MustNewSourceID("test://schema.yammm")
	issue := NewIssue(Error, E_TYPE_COLLISION, "schema-only").
		WithSpan(location.Point(source, 1, 1)).
		Build()
	val := issue.LogValue()
	if _, ok := issueLogAttr(t, val, "location"); !ok {
		t.Error("schema-only issue missing location group")
	}
	if _, ok := issueLogAttr(t, val, "path"); ok {
		t.Error("schema-only issue emitted path attribute, want omitted")
	}
}
