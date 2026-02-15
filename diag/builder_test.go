package diag

import (
	"testing"

	"github.com/simon-lentz/yammm/location"
)

func TestNewIssue(t *testing.T) {
	issue := NewIssue(Error, E_SYNTAX, "test message").Build()

	if issue.Severity() != Error {
		t.Errorf("Severity() = %v; want %v", issue.Severity(), Error)
	}
	if issue.Code() != E_SYNTAX {
		t.Errorf("Code() = %v; want %v", issue.Code(), E_SYNTAX)
	}
	if issue.Message() != "test message" {
		t.Errorf("Message() = %q; want %q", issue.Message(), "test message")
	}
	if !issue.IsValid() {
		t.Error("NewIssue should produce valid issue")
	}
}

func TestIssueBuilder_WithSpan(t *testing.T) {
	source := location.MustNewSourceID("test://schema.yammm")
	span := location.Point(source, 10, 5)

	issue := NewIssue(Error, E_SYNTAX, "test").
		WithSpan(span).
		Build()

	if issue.Span() != span {
		t.Errorf("Span() = %v; want %v", issue.Span(), span)
	}
	if !issue.HasSpan() {
		t.Error("HasSpan() = false; want true")
	}
}

func TestIssueBuilder_WithPath(t *testing.T) {
	issue := NewIssue(Error, E_TYPE_MISMATCH, "test").
		WithPath("data.json", "$.items[0].name").
		Build()

	if issue.SourceName() != "data.json" {
		t.Errorf("SourceName() = %q; want %q", issue.SourceName(), "data.json")
	}
	if issue.Path() != "$.items[0].name" {
		t.Errorf("Path() = %q; want %q", issue.Path(), "$.items[0].name")
	}
}

func TestIssueBuilder_WithHint(t *testing.T) {
	issue := NewIssue(Error, E_TYPE_COLLISION, "test").
		WithHint("rename one of the types").
		Build()

	if issue.Hint() != "rename one of the types" {
		t.Errorf("Hint() = %q; want %q", issue.Hint(), "rename one of the types")
	}
}

func TestIssueBuilder_WithRelated(t *testing.T) {
	source := location.MustNewSourceID("test://schema.yammm")
	related1 := location.RelatedInfo{
		Span:    location.Point(source, 5, 1),
		Message: "previous definition here",
	}
	related2 := location.RelatedInfo{
		Span:    location.Point(source, 10, 1),
		Message: "also defined here",
	}

	issue := NewIssue(Error, E_TYPE_COLLISION, "test").
		WithRelated(related1).
		WithRelated(related2).
		Build()

	related := issue.Related()
	if len(related) != 2 {
		t.Fatalf("len(Related()) = %d; want 2", len(related))
	}
	if related[0].Message != "previous definition here" {
		t.Errorf("Related()[0].Message = %q; want %q", related[0].Message, "previous definition here")
	}
	if related[1].Message != "also defined here" {
		t.Errorf("Related()[1].Message = %q; want %q", related[1].Message, "also defined here")
	}
}

func TestIssueBuilder_WithRelated_Variadic(t *testing.T) {
	source := location.MustNewSourceID("test://schema.yammm")
	related := []location.RelatedInfo{
		{Span: location.Point(source, 5, 1), Message: "first"},
		{Span: location.Point(source, 10, 1), Message: "second"},
	}

	issue := NewIssue(Error, E_TYPE_COLLISION, "test").
		WithRelated(related...).
		Build()

	got := issue.Related()
	if len(got) != 2 {
		t.Fatalf("len(Related()) = %d; want 2", len(got))
	}
}

func TestIssueBuilder_WithDetail(t *testing.T) {
	// B3: Test WithDetail single key-value convenience method
	issue := NewIssue(Error, E_TYPE_MISMATCH, "test").
		WithDetail(DetailKeyTypeName, "Person").
		WithDetail(DetailKeyPropertyName, "name").
		Build()

	details := issue.Details()
	if len(details) != 2 {
		t.Fatalf("len(Details()) = %d; want 2", len(details))
	}
	if details[0].Key != DetailKeyTypeName || details[0].Value != "Person" {
		t.Errorf("Details()[0] = %v; want {%q, %q}", details[0], DetailKeyTypeName, "Person")
	}
	if details[1].Key != DetailKeyPropertyName || details[1].Value != "name" {
		t.Errorf("Details()[1] = %v; want {%q, %q}", details[1], DetailKeyPropertyName, "name")
	}
}

func TestIssueBuilder_WithDetails(t *testing.T) {
	issue := NewIssue(Error, E_TYPE_MISMATCH, "test").
		WithDetails(Detail{Key: DetailKeyTypeName, Value: "Person"}).
		WithDetails(Detail{Key: DetailKeyPropertyName, Value: "name"}).
		Build()

	details := issue.Details()
	if len(details) != 2 {
		t.Fatalf("len(Details()) = %d; want 2", len(details))
	}
	if details[0].Key != DetailKeyTypeName || details[0].Value != "Person" {
		t.Errorf("Details()[0] = %v; want {%q, %q}", details[0], DetailKeyTypeName, "Person")
	}
	if details[1].Key != DetailKeyPropertyName || details[1].Value != "name" {
		t.Errorf("Details()[1] = %v; want {%q, %q}", details[1], DetailKeyPropertyName, "name")
	}
}

func TestIssueBuilder_WithDetails_Variadic(t *testing.T) {
	details := TypeProp("Person", "name")

	issue := NewIssue(Error, E_UNKNOWN_PROPERTY, "test").
		WithDetails(details...).
		Build()

	got := issue.Details()
	if len(got) != 2 {
		t.Fatalf("len(Details()) = %d; want 2", len(got))
	}
}

func TestIssueBuilder_WithExpectedGot(t *testing.T) {
	issue := NewIssue(Error, E_TYPE_MISMATCH, "test").
		WithExpectedGot("string", "int").
		Build()

	details := issue.Details()
	if len(details) != 2 {
		t.Fatalf("len(Details()) = %d; want 2", len(details))
	}
	if details[0].Key != DetailKeyExpected || details[0].Value != "string" {
		t.Errorf("Details()[0] = %v; want expected=string", details[0])
	}
	if details[1].Key != DetailKeyGot || details[1].Value != "int" {
		t.Errorf("Details()[1] = %v; want got=int", details[1])
	}
}

func TestIssueBuilder_FluentChaining(t *testing.T) {
	source := location.MustNewSourceID("test://schema.yammm")
	span := location.Point(source, 10, 5)
	related := location.RelatedInfo{
		Span:    location.Point(source, 5, 1),
		Message: "previous definition",
	}

	issue := NewIssue(Error, E_TYPE_COLLISION, `type "Person" already defined`).
		WithSpan(span).
		WithHint("rename one of the types").
		WithRelated(related).
		WithDetails(Detail{Key: DetailKeyTypeName, Value: "Person"}).
		Build()

	// Verify all fields are set
	if !issue.HasSpan() {
		t.Error("issue should have span")
	}
	if issue.Hint() == "" {
		t.Error("issue should have hint")
	}
	if len(issue.Related()) != 1 {
		t.Error("issue should have related info")
	}
	if len(issue.Details()) != 1 {
		t.Error("issue should have details")
	}
	if !issue.IsValid() {
		t.Error("issue should be valid")
	}
}

func TestIssueBuilder_BuildImmutability(t *testing.T) {
	source := location.MustNewSourceID("test://schema.yammm")

	builder := NewIssue(Error, E_TYPE_COLLISION, "test").
		WithRelated(location.RelatedInfo{
			Span:    location.Point(source, 5, 1),
			Message: "original",
		}).
		WithDetails(Detail{Key: DetailKeyTypeName, Value: "original"})

	// Build first issue
	issue1 := builder.Build()

	// Modify builder and build second issue
	builder.WithRelated(location.RelatedInfo{
		Span:    location.Point(source, 10, 1),
		Message: "added",
	})
	builder.WithDetails(Detail{Key: DetailKeyPropertyName, Value: "added"})

	issue2 := builder.Build()

	// issue1 should not be affected by subsequent builder modifications
	if len(issue1.Related()) != 1 {
		t.Errorf("issue1 Related() len = %d; want 1 (builder modifications affected built issue)",
			len(issue1.Related()))
	}
	if len(issue1.Details()) != 1 {
		t.Errorf("issue1 Details() len = %d; want 1 (builder modifications affected built issue)",
			len(issue1.Details()))
	}

	// issue2 should have both
	if len(issue2.Related()) != 2 {
		t.Errorf("issue2 Related() len = %d; want 2", len(issue2.Related()))
	}
	if len(issue2.Details()) != 2 {
		t.Errorf("issue2 Details() len = %d; want 2", len(issue2.Details()))
	}
}

func TestIssueBuilder_BuildDeepCopy(t *testing.T) {
	source := location.MustNewSourceID("test://schema.yammm")

	// Create builder with slices
	builder := NewIssue(Error, E_TYPE_COLLISION, "test").
		WithRelated(location.RelatedInfo{
			Span:    location.Point(source, 5, 1),
			Message: "related",
		}).
		WithDetails(Detail{Key: DetailKeyTypeName, Value: "type"})

	issue := builder.Build()

	// Get slices from built issue
	related := issue.Related()
	details := issue.Details()

	// Modify returned slices
	related[0].Message = "modified"
	details[0].Value = "modified"

	// Original issue should be unchanged
	if issue.Related()[0].Message == "modified" {
		t.Error("modifying Related() return value affected issue")
	}
	if issue.Details()[0].Value == "modified" {
		t.Error("modifying Details() return value affected issue")
	}
}

func TestIssueBuilder_EmptySlices(t *testing.T) {
	issue := NewIssue(Error, E_SYNTAX, "test").Build()

	if issue.Related() != nil {
		t.Error("Related() should be nil when no related info added")
	}
	if issue.Details() != nil {
		t.Error("Details() should be nil when no details added")
	}
}

func TestNewIssue_AllSeverities(t *testing.T) {
	severities := []Severity{Fatal, Error, Warning, Info, Hint}

	for _, sev := range severities {
		t.Run(sev.String(), func(t *testing.T) {
			issue := NewIssue(sev, E_SYNTAX, "test").Build()
			if issue.Severity() != sev {
				t.Errorf("Severity() = %v; want %v", issue.Severity(), sev)
			}
			if !issue.IsValid() {
				t.Error("issue should be valid")
			}
		})
	}
}

// TestNewIssue_PanicOnInvalidSeverity verifies that NewIssue panics when
// given an out-of-range severity value. This enforces's guarantee
// that IssueBuilder produces only valid issues.
func TestNewIssue_PanicOnInvalidSeverity(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewIssue with invalid severity should panic")
		}
	}()

	NewIssue(Severity(255), E_SYNTAX, "test")
}

// TestNewIssue_PanicOnZeroCode verifies that NewIssue panics when
// given a zero Code value. This enforces's guarantee
// that IssueBuilder produces only valid issues.
func TestNewIssue_PanicOnZeroCode(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewIssue with zero code should panic")
		}
	}()

	NewIssue(Error, Code{}, "test")
}

// TestNewIssue_PanicOnEmptyMessage verifies that NewIssue panics when
// given an empty message. This enforces's guarantee
// that IssueBuilder produces only valid issues.
func TestNewIssue_PanicOnEmptyMessage(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewIssue with empty message should panic")
		}
	}()

	NewIssue(Error, E_SYNTAX, "")
}

// TestNewIssue_PanicOnSeverityJustAboveHint verifies the boundary case
// where severity is just above the valid range (Hint + 1 = 5).
func TestNewIssue_PanicOnSeverityJustAboveHint(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewIssue with severity > Hint should panic")
		}
	}()

	NewIssue(Severity(5), E_SYNTAX, "test") // Hint = 4, so 5 is invalid
}

// TestFromIssue_ValidatesInput verifies that FromIssue panics on invalid issues.
func TestFromIssue_ValidatesInput(t *testing.T) {
	t.Run("panics on zero issue", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("FromIssue with zero issue should panic")
			}
		}()
		FromIssue(Issue{})
	})

	t.Run("panics on invalid issue (missing code)", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("FromIssue with invalid issue should panic")
			}
		}()
		// Create an invalid issue by directly constructing it (bypassing builder)
		invalid := Issue{
			severity: Error,
			message:  "test",
			// code is zero - invalid
		}
		FromIssue(invalid)
	})

	t.Run("accepts valid issue", func(t *testing.T) {
		valid := NewIssue(Error, E_SYNTAX, "test message").Build()
		builder := FromIssue(valid)
		if builder == nil {
			t.Error("FromIssue should return non-nil builder for valid issue")
		}
		rebuilt := builder.Build()
		if rebuilt.Message() != "test message" {
			t.Errorf("Message() = %q; want %q", rebuilt.Message(), "test message")
		}
	})
}
