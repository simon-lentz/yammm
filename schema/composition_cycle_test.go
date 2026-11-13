package schema_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// A cycle whose every composition is required has no finite instance, so it is
// refused at load at the type declared first, with each link as related
// information.
func TestLoad_ARequiredCompositionCycleIsRefused(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		body    string
		message string
		links   int
	}{
		"two parts, (one) and (one:many)": {
			body: `part type PA {
	n String
	*-> B (one:many) PB
}
part type PB {
	n String
	*-> BACK (one) PA
}`,
			message: `composition cycle detected: PA -> PB -> PA; every composition on it is required, so no instance of "PA" is finite`,
			links:   2,
		},
		"a part that requires itself": {
			body: `part type PA {
	n String
	*-> NEXT (one) PA
}`,
			message: `composition cycle detected: PA -> PA; every composition on it is required, so no instance of "PA" is finite`,
			links:   1,
		},
		"a required link out of the cycle": {
			body: `part type PA {
	n String
	*-> B (one) PB
	*-> C (one) PC
}
part type PB {
	n String
	*-> A (one) PA
}
part type PC {
	n String
}`,
			message: `composition cycle detected: PA -> PB -> PA; every composition on it is required, so no instance of "PA" is finite`,
			links:   2,
		},
		"a link a part inherits": {
			body: `abstract type Base {
	*-> NEXT (one) PA
}
part type PA extends Base {
	n String
}`,
			message: `composition cycle detected: PA -> PA; every composition on it is required, so no instance of "PA" is finite`,
			links:   1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := "schema \"c\"\n" + tc.body + "\n"
			_, res := schema.LoadString(t.Context(), src, "c.yammm")
			issues := slices.Collect(res.Issues())
			if len(issues) != 1 || issues[0].Code() != diag.E_COMPOSITION_CYCLE {
				t.Fatalf("want one E_COMPOSITION_CYCLE; got %v", res)
			}
			if got := issues[0].Message(); got != tc.message {
				t.Errorf("message %q, want %q", got, tc.message)
			}
			if got := len(issues[0].Related()); got != tc.links {
				t.Errorf("%d related links, want %d", got, tc.links)
			}
			if at := src[issues[0].Span().Start.Byte:]; !strings.HasPrefix(at, "part type PA") {
				t.Errorf("the issue stands at %q, want the declaration of PA", firstLine(at))
			}
			for _, rel := range issues[0].Related() {
				if at := src[rel.Span.Start.Byte:]; !strings.HasPrefix(at, "*->") {
					t.Errorf("related %q stands at %q, want its composition", rel.Message, firstLine(at))
				}
			}
		})
	}
}

// One optional link ends the chain, so the cycle has a finite instance.
func TestLoad_ACompositionCycleWithAnOptionalLinkLoads(t *testing.T) {
	t.Parallel()
	for _, multiplicity := range []string{"(_:one)", "(many)", "(_:many)"} {
		src := `schema "c"
part type PA {
	n String
	*-> B (one) PB
}
part type PB {
	n String
	*-> BACK ` + multiplicity + ` PA
}
`
		if _, res := schema.LoadString(t.Context(), src, "c.yammm"); res.HasErrors() {
			t.Errorf("%s: %v", multiplicity, res)
		}
	}
}

// Types that require one another are one break however many cycles run through
// them, so they draw one issue in every declaration order, at the type declared
// first, naming the shortest cycle through it and relating every required link.
func TestLoad_TypesThatRequireOneAnotherDrawOneIssueInAnyOrder(t *testing.T) {
	t.Parallel()
	decls := map[string]string{
		"PA": "part type PA {\n\tn String\n\t*-> B (one) PB\n\t*-> C (one) PC\n}",
		"PB": "part type PB {\n\tn String\n\t*-> D (one) PD\n}",
		"PC": "part type PC {\n\tn String\n\t*-> D (one) PD\n}",
		"PD": "part type PD {\n\tn String\n\t*-> A (one) PA\n}",
	}
	for _, tc := range []struct {
		order []string
		cycle string
	}{
		{[]string{"PA", "PB", "PC", "PD"}, "PA -> PB -> PD -> PA"},
		{[]string{"PD", "PC", "PB", "PA"}, "PD -> PA -> PB -> PD"},
		{[]string{"PC", "PD", "PA", "PB"}, "PC -> PD -> PA -> PC"},
	} {
		t.Run(tc.order[0]+" first", func(t *testing.T) {
			t.Parallel()
			var src strings.Builder
			src.WriteString("schema \"c\"\n")
			for _, name := range tc.order {
				src.WriteString(decls[name] + "\n")
			}
			_, res := schema.LoadString(t.Context(), src.String(), "c.yammm")
			issues := slices.Collect(res.Issues())
			if len(issues) != 1 || issues[0].Code() != diag.E_COMPOSITION_CYCLE {
				t.Fatalf("want one E_COMPOSITION_CYCLE; got %v", res)
			}
			want := "composition cycle detected: " + tc.cycle + `; every composition on it is required, so no instance of "` + tc.order[0] + `" is finite`
			if got := issues[0].Message(); got != want {
				t.Errorf("message %q, want %q", got, want)
			}
			var links []string
			for _, rel := range issues[0].Related() {
				links = append(links, rel.Message)
			}
			slices.Sort(links)
			want = "PA requires PB through B|PA requires PC through C|PB requires PD through D|PC requires PD through D|PD requires PA through A"
			if got := strings.Join(links, "|"); got != want {
				t.Errorf("related links %q, want the group's five, %q", got, want)
			}
		})
	}
}

// Two groups that one optional link joins are two breaks, one issue each.
func TestLoad_TwoGroupsOfRequiredCompositionsDrawAnIssueEach(t *testing.T) {
	t.Parallel()
	src := `schema "c"
part type PA {
	n String
	*-> B (one) PB
	*-> C (_) PC
}
part type PB {
	n String
	*-> A (one) PA
}
part type PC {
	n String
	*-> C (one:many) PC
}
`
	_, res := schema.LoadString(t.Context(), src, "c.yammm")
	if counts := codeCounts(res); counts[diag.E_COMPOSITION_CYCLE] != 2 || len(counts) != 1 {
		t.Errorf("want two E_COMPOSITION_CYCLE and nothing else; got %v", res)
	}
}

// firstLine returns s up to its first line break.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
