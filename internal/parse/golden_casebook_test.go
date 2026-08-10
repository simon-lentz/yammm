package parse

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
)

// Golden set 2 — the malformed casebook. Fifty-two seeded cases, each
// planting known defects, frozen on two axes: what this parser says about
// malformed input, and which declarations survive it.
//
// The survival axis is the reason this set is not just the diagnostics. A
// recovery rule once scored better than the reference parser on recall,
// first-hit and noise while silently deleting three well-formed properties
// from a type; diagnostics alone could not see it. Freezing the declaration
// inventory means the same regression fails by name.
//
// Both axes used to be checked against a second implementation at run time.
// That oracle is untracked and retires with the harness, so the exemptions it
// justified — the oracle_only entries below — are carried here as prose. They
// are the only record of why a difference from the reference parser is
// deliberate.

type caseManifest struct {
	Cases []caseSpec `json:"cases"`
}

type caseSpec struct {
	File         string             `json:"file"`
	Category     string             `json:"category"`
	Defects      []defectSpec       `json:"defects"`
	OracleOnly   []oracleOnlySpec   `json:"oracle_only,omitempty"`
	AllowedNoise []allowedNoiseSpec `json:"allowed_noise,omitempty"`
}

type defectSpec struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"` // "syntax" | "semantic"
	ExpectCode    string `json:"expect_code"`
	Anchor        [2]int `json:"anchor"`
	AllowSpanless bool   `json:"allow_spanless,omitempty"`
	// OracleOnly marks a defect the reference parser reports and this one does
	// not. The assertion runs in the direction that keeps it honest: such a
	// defect must stay unmatched, so the flag cannot cover one that comes back.
	OracleOnly bool   `json:"oracle_only,omitempty"`
	Note       string `json:"note,omitempty"`
}

type oracleOnlySpec struct {
	Decl string `json:"decl"`
	Why  string `json:"why"`
}

type allowedNoiseSpec struct {
	Code string `json:"code"`
	Why  string `json:"why"`
}

func TestGoldenCasebook(t *testing.T) {
	cases, dir := loadCasebook(t)
	for _, cs := range cases {
		t.Run(cs.File, func(t *testing.T) {
			src := readCase(t, dir, cs.File)
			got, _ := project(cs.File, src)
			yammmtest.GoldenJSON(t, filepath.Join("golden", "casebook", cs.File), got)
		})
	}
}

// TestGoldenCasebookSurvival freezes the two axes in one readable artifact:
// which planted defect each case's diagnostics reach, and which declarations
// come out of the parse. A lost declaration is a one-line diff here, where in
// the per-case projection it would be a span-shaped delta among many.
func TestGoldenCasebookSurvival(t *testing.T) {
	cases, dir := loadCasebook(t)
	var b strings.Builder
	b.WriteString("# Golden set 2 — casebook defect reporting and declaration survival\n")
	b.WriteString("# 'matched' means a diagnostic of the expected code overlaps the planted\n")
	b.WriteString("# anchor lines. 'oracle_only' entries are deliberate differences from the\n")
	b.WriteString("# retired reference parser, kept with the reason each was accepted.\n")

	for _, cs := range cases {
		src := readCase(t, dir, cs.File)
		got, _ := project(cs.File, src)

		fmt.Fprintf(&b, "\n## %s [%s]\n", cs.File, cs.Category)

		matched := map[string]bool{}
		noise, firstHit := 0, ""
		for i, d := range got.Diags {
			id := classify(cs.Defects, d.Code, !d.Spanless, d.Line, d.EndLine)
			if id == "" {
				noise++
				continue
			}
			matched[id] = true
			if i == 0 {
				firstHit = id
			}
		}
		fmt.Fprintf(&b, "first-hit: %s\nnoise: %d\n", orNone(firstHit), noise)

		b.WriteString("defects:\n")
		for _, d := range cs.Defects {
			status := "unmatched"
			if matched[d.ID] {
				status = "matched"
			}
			if d.OracleOnly {
				status += " (oracle-only: must stay unmatched)"
			}
			fmt.Fprintf(&b, "  %-4s %-8s %-28s %s\n", d.ID, d.Kind, d.ExpectCode, status)
			if matched[d.ID] && d.OracleOnly {
				t.Errorf("%s: defect %s is flagged oracle-only but is matched; "+
					"the flag must not cover a defect this parser reports", cs.File, d.ID)
			}
		}

		b.WriteString("decls:\n")
		for _, d := range declInventory(got) {
			fmt.Fprintf(&b, "  %s\n", d)
		}

		if len(cs.OracleOnly) > 0 {
			b.WriteString("oracle_only (the reference parser built these; this one deliberately does not):\n")
			for _, o := range cs.OracleOnly {
				fmt.Fprintf(&b, "  %s — %s\n", o.Decl, o.Why)
			}
		}
		for _, n := range cs.AllowedNoise {
			fmt.Fprintf(&b, "allowed_noise: %s — %s\n", n.Code, n.Why)
		}
	}
	yammmtest.Golden(t, filepath.Join("golden", "casebook_survival"), []byte(b.String()))
}

// declInventory names every declaration the parse produced, in the vocabulary
// the retired survival comparison used, so its records stay readable.
func declInventory(s *projSchema) []string {
	var out []string
	for _, d := range s.DataTypes {
		out = append(out, "datatype "+d.Name)
	}
	for _, t := range s.Types {
		out = append(out, "type "+t.Name)
		for _, p := range t.Props {
			out = append(out, t.Name+"."+p.Name)
		}
		for _, r := range t.Rels {
			out = append(out, t.Name+" --> "+r.Name)
		}
	}
	sort.Strings(out)
	return out
}

// classify returns the id of the first defect an issue satisfies — code
// equality plus anchor-line overlap, or spanless tolerance — or "" for noise.
func classify(defects []defectSpec, code string, hasSpan bool, startLine, endLine int) string {
	for _, d := range defects {
		if code != d.ExpectCode {
			continue
		}
		if !hasSpan {
			if d.AllowSpanless {
				return d.ID
			}
			continue
		}
		if startLine <= d.Anchor[1] && endLine >= d.Anchor[0] {
			return d.ID
		}
	}
	return ""
}

func loadCasebook(tb testing.TB) ([]caseSpec, string) {
	tb.Helper()
	dir := filepath.Join("testdata", "casebook")
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		tb.Fatalf("read casebook manifest: %v", err)
	}
	var m caseManifest
	if err := json.Unmarshal(b, &m); err != nil {
		tb.Fatalf("parse casebook manifest: %v", err)
	}
	sort.Slice(m.Cases, func(i, j int) bool { return m.Cases[i].File < m.Cases[j].File })

	// Manifest and corpus must describe each other exactly, so neither drifts.
	present := map[string]bool{}
	for _, c := range m.Cases {
		present[c.File] = true
	}
	found, err := filepath.Glob(filepath.Join(dir, "*.yammm"))
	if err != nil {
		tb.Fatalf("glob casebook: %v", err)
	}
	if len(found) != len(m.Cases) {
		tb.Errorf("casebook has %d sources and %d manifest entries", len(found), len(m.Cases))
	}
	for _, f := range found {
		if name := filepath.Base(f); !present[name] {
			tb.Errorf("casebook source %s has no manifest entry", name)
		}
	}
	return m.Cases, dir
}

func readCase(tb testing.TB, dir, file string) string {
	tb.Helper()
	b, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		tb.Fatalf("read casebook source %s: %v", file, err)
	}
	return string(b)
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
