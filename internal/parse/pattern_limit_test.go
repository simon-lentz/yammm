package parse

import (
	"fmt"
	"regexp/syntax"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

// TestChecks_ThePatternLimitCountsEveryPatternWritten pins the count
// MaxPatterns bounds, over every list of one to four patterns drawn from a
// plain pattern, a pattern naming a surrogate and a pattern that does not
// compile: a list is over the limit when it holds more than MaxPatterns
// patterns, whatever refuses one of them, and the diagnostic states that
// number. Each refused pattern draws its own diagnostic beside it.
func TestChecks_ThePatternLimitCountsEveryPatternWritten(t *testing.T) {
	t.Parallel()
	alphabet := []string{`a`, `\x{D800}`, `(`}
	var lists [][]string
	var extend func(list []string)
	extend = func(list []string) {
		if len(list) > 0 {
			lists = append(lists, append([]string(nil), list...))
		}
		if len(list) == 4 {
			return
		}
		for _, p := range alphabet {
			extend(append(list, p))
		}
	}
	extend(nil)
	if len(lists) != 120 {
		t.Fatalf("built %d lists, want 120", len(lists))
	}
	for _, list := range lists {
		t.Run(strings.Join(list, " "), func(t *testing.T) {
			t.Parallel()
			compiling, surrogates := 0, 0
			quoted := make([]string, len(list))
			for i, p := range list {
				if _, err := syntax.Parse(p, syntax.Perl); err == nil {
					compiling++
				}
				if strings.Contains(p, "D800") {
					surrogates++
				}
				quoted[i] = `"` + strings.ReplaceAll(p, `\`, `\\`) + `"`
			}
			f, issues := Parse([]byte(checkSource("p Pattern["+strings.Join(quoted, ", ")+"]")), location.NewSourceID("s.yammm"))
			over := fmt.Sprintf("pattern constraint exceeds maximum of %d patterns (got %d)", MaxPatterns, len(list))
			gotOver, gotSurrogates := 0, 0
			for _, iss := range issues {
				switch {
				case strings.HasPrefix(iss.Message(), "pattern constraint exceeds"):
					gotOver++
					if iss.Message() != over {
						t.Errorf("limit diagnostic = %q, want %q", iss.Message(), over)
					}
				case strings.Contains(iss.Message(), "names the surrogate code point"):
					gotSurrogates++
				}
			}
			if wantOver := len(list) > MaxPatterns; (gotOver == 1) != wantOver || gotOver > 1 {
				t.Errorf("%d limit diagnostics for %d patterns: %s", gotOver, len(list), messages(issues))
			}
			if gotCompileErrors := len(issues) - gotOver - gotSurrogates; gotCompileErrors != len(list)-compiling {
				t.Errorf("%d compile diagnostics for %d patterns that do not compile: %s", gotCompileErrors, len(list)-compiling, messages(issues))
			}
			if gotSurrogates != surrogates {
				t.Errorf("%d surrogate diagnostics, want %d: %s", gotSurrogates, surrogates, messages(issues))
			}
			kept := 0
			for _, lit := range f.Types[0].Properties[1].Constraint.PatternLits {
				if lit.Kept != (lit.Regex != nil) {
					t.Errorf("literal %s: Kept = %v with Regex set = %v", lit.Raw, lit.Kept, lit.Regex != nil)
				}
				if lit.Kept {
					kept++
					if lit.Text != "a" {
						t.Errorf("kept a refused pattern: %s", lit.Raw)
					}
				}
			}
			if kept > MaxPatterns {
				t.Errorf("%d patterns carry a Regex, over the limit of %d", kept, MaxPatterns)
			}
		})
	}
}
