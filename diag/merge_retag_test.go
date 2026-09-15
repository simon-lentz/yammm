package diag

import (
	"maps"
	"testing"
)

// TestCollector_MergeRetag_CountsFollowTheDeclarationUnderTruncation merges a
// result that dropped one of its issues through a retag that turns
// E_CONSTRAINT_FAIL into a warning. The merged counts hold the dropped issue at
// its declared severity, and the truncation facts carry over.
func TestCollector_MergeRetag_CountsFollowTheDeclarationUnderTruncation(t *testing.T) {
	t.Parallel()

	src := NewCollector(2)
	src.Collect(NewIssue(Fatal, E_INTERNAL, "internal").Build())
	src.Collect(NewIssue(Error, E_CONSTRAINT_FAIL, "stored").Build())
	src.Collect(NewIssue(Error, E_CONSTRAINT_FAIL, "dropped").Build())
	res := src.Result()
	if res.Len() != 2 || res.DroppedCount() != 1 {
		t.Fatalf("the source result is not truncated as expected: %s", res)
	}

	c := NewCollectorUnlimited()
	c.MergeRetag(res,
		func(sev Severity, code Code) (Severity, bool) {
			if code == E_CONSTRAINT_FAIL {
				return Warning, true
			}
			return sev, true
		},
		func(is Issue) Issue {
			if is.Code() == E_CONSTRAINT_FAIL {
				return NewIssue(Warning, is.Code(), is.Message()).Build()
			}
			return is
		})
	out := c.Result()

	if got, want := out.SeverityCounts(), (SeverityCounts{Fatal: 1, Warnings: 2}); got != want {
		t.Errorf("SeverityCounts = %+v; want %+v", got, want)
	}
	for sev, want := range map[Severity]map[Code]int{
		Fatal:   {E_INTERNAL: 1},
		Error:   {},
		Warning: {E_CONSTRAINT_FAIL: 2},
	} {
		if got := out.CodeCounts(sev); !maps.Equal(got, want) {
			t.Errorf("CodeCounts(%s) = %v; want %v", sev, got, want)
		}
	}
	if !out.LimitReached() || out.DroppedCount() != 1 {
		t.Errorf("LimitReached = %t, DroppedCount = %d; want true, 1", out.LimitReached(), out.DroppedCount())
	}
	if out.Len() != 2 {
		t.Errorf("Len = %d; want 2", out.Len())
	}
	for is := range out.Issues() {
		if is.Code() == E_CONSTRAINT_FAIL && is.Severity() != Warning {
			t.Errorf("stored %s at %s; want Warning", is.Code(), is.Severity())
		}
	}
}

// TestCollector_MergeRetag_DropsWhatTheDeclarationDoesNotKeep keeps only
// E_INTERNAL. An issue of any other code is absent from the stored issues and
// from every count, and one the source dropped does not truncate the merge.
func TestCollector_MergeRetag_DropsWhatTheDeclarationDoesNotKeep(t *testing.T) {
	t.Parallel()

	src := NewCollector(2)
	src.Collect(NewIssue(Fatal, E_INTERNAL, "internal").Build())
	src.Collect(NewIssue(Error, E_CONSTRAINT_FAIL, "not kept").Build())
	src.Collect(NewIssue(Warning, W_ANNOTATION_SHADOWED, "not kept, dropped").Build())
	res := src.Result()
	if res.Len() != 2 || res.DroppedCount() != 1 || res.HasCode(W_ANNOTATION_SHADOWED) {
		t.Fatalf("the source result did not drop its warning as expected: %s", res)
	}

	c := NewCollectorUnlimited()
	c.MergeRetag(res,
		func(sev Severity, code Code) (Severity, bool) { return sev, code == E_INTERNAL },
		func(is Issue) Issue { return is })
	out := c.Result()

	if out.Len() != 1 || !out.HasCode(E_INTERNAL) {
		t.Errorf("stored %s; want only the E_INTERNAL issue", out)
	}
	if got, want := out.SeverityCounts(), (SeverityCounts{Fatal: 1}); got != want {
		t.Errorf("SeverityCounts = %+v; want %+v", got, want)
	}
	for _, sev := range []Severity{Error, Warning} {
		if got := out.CodeCounts(sev); len(got) != 0 {
			t.Errorf("CodeCounts(%s) = %v; want none", sev, got)
		}
	}
	if out.LimitReached() || out.DroppedCount() != 0 {
		t.Errorf("LimitReached = %t, DroppedCount = %d; want false, 0", out.LimitReached(), out.DroppedCount())
	}
}

// TestCollector_MergeRetag_CountsEveryIssueOfACodeAtTheDeclaredSeverity
// retags two same-code issues to each severity. The merged counts hold both at
// that severity, and both are stored there.
func TestCollector_MergeRetag_CountsEveryIssueOfACodeAtTheDeclaredSeverity(t *testing.T) {
	t.Parallel()

	src := NewCollectorUnlimited()
	src.Collect(NewIssue(Error, E_CONSTRAINT_FAIL, "a").Build())
	src.Collect(NewIssue(Error, E_CONSTRAINT_FAIL, "b").Build())
	res := src.Result()

	rows := []struct {
		to   Severity
		want SeverityCounts
	}{
		{Fatal, SeverityCounts{Fatal: 2}},
		{Error, SeverityCounts{Errors: 2}},
		{Warning, SeverityCounts{Warnings: 2}},
		{Info, SeverityCounts{Info: 2}},
		{Hint, SeverityCounts{Hints: 2}},
	}
	for _, row := range rows {
		t.Run(row.to.String(), func(t *testing.T) {
			t.Parallel()
			c := NewCollectorUnlimited()
			c.MergeRetag(res,
				func(Severity, Code) (Severity, bool) { return row.to, true },
				func(is Issue) Issue { return NewIssue(row.to, is.Code(), is.Message()).Build() })
			out := c.Result()

			if got := out.SeverityCounts(); got != row.want {
				t.Errorf("SeverityCounts = %+v; want %+v", got, row.want)
			}
			if got, want := out.CodeCounts(row.to), (map[Code]int{E_CONSTRAINT_FAIL: 2}); !maps.Equal(got, want) {
				t.Errorf("CodeCounts(%s) = %v; want %v", row.to, got, want)
			}
			if out.Len() != 2 {
				t.Errorf("Len = %d; want 2", out.Len())
			}
			for is := range out.Issues() {
				if is.Severity() != row.to {
					t.Errorf("stored %s at %s; want %s", is.Code(), is.Severity(), row.to)
				}
			}
		})
	}
}

// TestCollector_MergeRetag_PanicsWhenTheBuiltIssueIsZeroOrInvalid holds fn to
// returning a valid issue, and names that fault rather than a declaration
// mismatch.
func TestCollector_MergeRetag_PanicsWhenTheBuiltIssueIsZeroOrInvalid(t *testing.T) {
	t.Parallel()

	src := NewCollectorUnlimited()
	src.Collect(NewIssue(Error, E_CONSTRAINT_FAIL, "a").Build())
	res := src.Result()
	toWarning := func(Severity, Code) (Severity, bool) { return Warning, true }

	rows := []struct {
		name string
		fn   func(Issue) Issue
		want string
	}{
		{"a zero issue", func(Issue) Issue { return Issue{} }, "diag.Collector.MergeRetag: zero-value Issue"},
		{"an issue with no message", func(Issue) Issue { return Issue{severity: Warning, code: E_CONSTRAINT_FAIL} }, "diag.Collector.MergeRetag: invalid Issue"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			wantPanic(t, row.want, func() {
				NewCollectorUnlimited().MergeRetag(res, toWarning, row.fn)
			})
		})
	}
}

// TestCollector_MergeRetag_PanicsWhenTheBuiltIssueDisagreesWithTheDeclaration
// holds fn to the severity retag declared and to the issue's own code.
func TestCollector_MergeRetag_PanicsWhenTheBuiltIssueDisagreesWithTheDeclaration(t *testing.T) {
	t.Parallel()

	src := NewCollectorUnlimited()
	src.Collect(NewIssue(Error, E_CONSTRAINT_FAIL, "a").Build())
	res := src.Result()
	toWarning := func(Severity, Code) (Severity, bool) { return Warning, true }

	rows := []struct {
		name string
		fn   func(Issue) Issue
	}{
		{"a severity other than the declared one", func(is Issue) Issue { return is }},
		{"a code other than the issue's", func(is Issue) Issue { return NewIssue(Warning, E_TYPE_MISMATCH, is.Message()).Build() }},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			wantPanic(t, "diag.Collector.MergeRetag: fn built", func() {
				NewCollectorUnlimited().MergeRetag(res, toWarning, row.fn)
			})
		})
	}
}
