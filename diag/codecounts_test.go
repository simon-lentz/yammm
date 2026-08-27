package diag

import "testing"

// CodeCounts is the seen-based sibling of HasCode: it counts every issue the
// collector saw, at the severity it carried, so a code dropped past the limit
// still reads under the severity it arrived with.

func TestResult_CodeCounts_SeenBasedUnderTruncation(t *testing.T) {
	c := NewCollector(1)
	c.Collect(NewIssue(Warning, W_ANNOTATION_SHADOWED, "stored").Build())
	c.Collect(NewIssue(Warning, W_SNAPSHOT_VALUE_NONCONFORMING, "dropped").Build())
	r := c.Result()

	if r.HasCode(W_SNAPSHOT_VALUE_NONCONFORMING) {
		t.Fatal("setup: the second code must have been dropped past the limit")
	}
	if got := r.CodeCounts(Warning)[W_SNAPSHOT_VALUE_NONCONFORMING]; got != 1 {
		t.Errorf("CodeCounts(Warning)[dropped code] = %d; want 1", got)
	}
	if got := r.CodeCounts(Warning)[W_ANNOTATION_SHADOWED]; got != 1 {
		t.Errorf("CodeCounts(Warning)[stored code] = %d; want 1", got)
	}
	if got := r.CodeCounts(Error)[W_SNAPSHOT_VALUE_NONCONFORMING]; got != 0 {
		t.Errorf("CodeCounts(Error)[a warning's code] = %d; want 0", got)
	}
}

// Severity is independent of code: the same code at two severities counts
// once under each, and a severity-agnostic map could not tell them apart.
func TestResult_CodeCounts_SeparatesSeverities(t *testing.T) {
	c := NewCollectorUnlimited()
	c.Collect(NewIssue(Error, E_CONSTRAINT_FAIL, "as an error").Build())
	c.Collect(NewIssue(Warning, E_CONSTRAINT_FAIL, "retagged to a warning").Build())
	c.Collect(NewIssue(Warning, E_CONSTRAINT_FAIL, "and again").Build())
	r := c.Result()

	if got := r.CodeCounts(Error)[E_CONSTRAINT_FAIL]; got != 1 {
		t.Errorf("CodeCounts(Error) = %d; want 1", got)
	}
	if got := r.CodeCounts(Warning)[E_CONSTRAINT_FAIL]; got != 2 {
		t.Errorf("CodeCounts(Warning) = %d; want 2", got)
	}
	if got := len(r.CodeCounts(Hint)); got != 0 {
		t.Errorf("CodeCounts(Hint) has %d entries; want none", got)
	}
}

func TestCollector_Merge_SumsCodeCounts(t *testing.T) {
	src := NewCollector(1)
	src.Collect(NewIssue(Warning, W_ANNOTATION_SHADOWED, "one").Build())
	src.Collect(NewIssue(Warning, W_ANNOTATION_SHADOWED, "two, dropped").Build())

	dst := NewCollectorUnlimited()
	dst.Collect(NewIssue(Warning, W_ANNOTATION_SHADOWED, "three").Build())
	dst.Merge(src.Result())

	if got := dst.Result().CodeCounts(Warning)[W_ANNOTATION_SHADOWED]; got != 3 {
		t.Errorf("merged CodeCounts(Warning) = %d; want 3 (the dropped one included)", got)
	}
}

func TestResult_CodeCounts_ReturnsACopy(t *testing.T) {
	c := NewCollectorUnlimited()
	c.Collect(NewIssue(Warning, W_ANNOTATION_SHADOWED, "one").Build())
	r := c.Result()

	m := r.CodeCounts(Warning)
	m[W_ANNOTATION_SHADOWED] = 99
	if got := r.CodeCounts(Warning)[W_ANNOTATION_SHADOWED]; got != 1 {
		t.Errorf("a caller's write reached the result: %d; want 1", got)
	}
	if OK().CodeCounts(Warning) == nil {
		t.Error("CodeCounts on an empty result returned nil; want an empty map")
	}
}
