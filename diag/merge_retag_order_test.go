package diag

import "testing"

// mixedCodesResult holds one issue of each of several codes, at two severities.
func mixedCodesResult() Result {
	c := NewCollector(NoLimit)
	for _, code := range []Code{E_SYNTAX, E_INTERNAL, E_UNKNOWN_PROPERTY, E_CASE_COLLISION, E_INHERIT_CYCLE, E_DUPLICATE_PROPERTY} {
		c.Collect(NewIssue(Warning, code, "w").Build())
		c.Collect(NewIssue(Error, code, "e").Build())
	}
	return c.Result()
}

func TestCollector_MergeRetag_PanicsOnTheFirstInvalidDeclarationInSeverityAndCodeOrder(t *testing.T) {
	t.Parallel()

	// Map order differs between runs, so one merge can name the first code by luck.
	res := mixedCodesResult()
	for range 64 {
		wantPanic(t, "invalid severity 0 for error E_CASE_COLLISION", func() {
			NewCollector(NoLimit).MergeRetag(res,
				func(Severity, Code) (Severity, bool) { return 0, true },
				func(is Issue) Issue { return is })
		})
	}
}

// Severities are walked from Fatal to Hint: an error's code is judged before a
// warning's whatever the codes' own order.
func TestCollector_MergeRetag_JudgesTheMoreSevereIssueFirst(t *testing.T) {
	t.Parallel()

	c := NewCollector(NoLimit)
	c.Collect(NewIssue(Warning, E_CASE_COLLISION, "w").Build())
	c.Collect(NewIssue(Error, E_SYNTAX, "e").Build())
	res := c.Result()
	var asked []string
	wantPanic(t, "invalid severity 0 for error E_SYNTAX", func() {
		NewCollector(NoLimit).MergeRetag(res,
			func(sev Severity, code Code) (Severity, bool) {
				asked = append(asked, sev.String()+" "+code.String())
				return 0, true
			},
			func(is Issue) Issue { return is })
	})
	if len(asked) != 1 {
		t.Errorf("retag was asked %v before the panic, want the error alone", asked)
	}
}

// A code the merge drops is never stored, so its declared severity is not
// judged.
func TestCollector_MergeRetag_JudgesNoSeverityForADroppedCode(t *testing.T) {
	t.Parallel()

	dst := NewCollector(NoLimit)
	dst.MergeRetag(mixedCodesResult(),
		func(Severity, Code) (Severity, bool) { return 0, false },
		func(is Issue) Issue { return is })
	if n := dst.Result().Len(); n != 0 {
		t.Errorf("a merge that drops every code stored %d issues", n)
	}
}

func TestCollector_MergeRetag_AsksRetagOncePerSeverityAndCode(t *testing.T) {
	t.Parallel()

	type key struct {
		sev  Severity
		code Code
	}
	calls := make(map[key]int)
	NewCollector(NoLimit).MergeRetag(mixedCodesResult(),
		func(sev Severity, code Code) (Severity, bool) {
			calls[key{sev, code}]++
			return sev, true
		},
		func(is Issue) Issue { return is })
	if len(calls) != 12 {
		t.Errorf("retag saw %d severity and code pairs, want 12", len(calls))
	}
	for k, n := range calls {
		if n != 1 {
			t.Errorf("retag ran %d times for %s %s, want once", n, k.sev, k.code)
		}
	}
}
