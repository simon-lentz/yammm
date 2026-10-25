package snapshot_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// revalidationFindings returns the code and the validator's own message of
// each issue res holds from the revalidation of the root named by prefix.
func revalidationFindings(res diag.Result, prefix string) []string {
	var out []string
	for issue := range res.Issues() {
		if msg, ok := strings.CutPrefix(issue.Message(), prefix); ok {
			out = append(out, issue.Code().String()+": "+msg)
		}
	}
	return out
}

// A target_missing record is an edge whose target the graph does not hold, so
// the validator is the oracle: revalidation reports for the record what the
// validator reports for the raw edge the record states.
func TestLoad_RevalidationJudgesARecordAsTheValidatorJudgesItsEdge(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	v := instance.NewValidator(s)
	for name, c := range map[string]struct {
		from, to string
		root     string
		raw      map[string]any
	}{
		"under a (one) association": {
			from: sbAbsent, to: sbLost("p2", "EMPLOYER", 1, `["c9"]`, `{"since":9999}`), root: "p2",
			raw: map[string]any{"id": "p2", "CLIENTS": []any{}, "EMPLOYER": map[string]any{"_target_id": "c9", "since": json.Number("9999")}},
		},
		"under a (many) association": {
			from: sbKnowsLost, to: strings.Replace(sbKnowsLost, `{"note":"m"}`, `{"note":7}`, 1), root: "p2",
			raw: map[string]any{"id": "p2", "CLIENTS": []any{}, "KNOWS": []any{map[string]any{"_target_id": "p8", "note": json.Number("7")}}},
		},
		"the second of two records under a (many) association": {
			from: sbKnowsLost, to: sbKnowsLost + `,` + sbLost("p2", "KNOWS", 4, `["p7"]`, `{"note":7}`), root: "p2",
			raw: map[string]any{"id": "p2", "CLIENTS": []any{}, "KNOWS": []any{
				map[string]any{"_target_id": "p8", "note": "m"},
				map[string]any{"_target_id": "p7", "note": json.Number("7")},
			}},
		},
		"beside an edge of the association": {
			from: sbUnresolved, to: sbUnresolved + sbLost("p1", "KNOWS", 4, `["p8"]`, `{"note":7}`) + `,`, root: "p1",
			raw: map[string]any{"id": "p1", "KNOWS": []any{
				map[string]any{"_target_id": "p2", "note": "n"},
				map[string]any{"_target_id": "p8", "note": json.Number("7")},
			}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, vres := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: c.raw})
			var want []string
			for issue := range vres.Issues() {
				want = append(want, issue.Code().String()+": "+issue.Message())
			}
			if len(want) != 1 {
				t.Fatalf("the validator reports %d issues for the raw edge, want 1: %s", len(want), vres)
			}
			edited := []byte(editOnce(t, doc, c.from, c.to))
			prefix := `revalidation of sb#Person[["` + c.root + `"]]: `
			for _, sev := range []diag.Severity{diag.Warning, diag.Error} {
				_, res := snapshot.Load(t.Context(), edited, s, snapshot.WithIssueLimit(0), snapshot.WithRevalidation(sev))
				got := revalidationFindings(res, prefix)
				if len(got) != 1 || got[0] != want[0] {
					t.Errorf("revalidation at %s reports %q, the validator %q:\n%s", sev, got, want, res)
				}
				if res.HasErrors() != (sev == diag.Error) {
					t.Errorf("revalidation at %s: HasErrors = %v:\n%s", sev, res.HasErrors(), res)
				}
			}
			if _, res := snapshot.Load(t.Context(), edited, s, snapshot.WithIssueLimit(0)); !res.OK() {
				t.Errorf("a load without revalidation judges no value: %s", res)
			}
		})
	}
}

const recordSourceSchema = `schema "rs"

type Event {
	at Timestamp primary
	--> NEXT (_:one) Event {
		weight Integer[0, 10]
	}
}
`

// A record reaches its root by the root's canonical address, as every other
// position that addresses an instance does: each document below spells one
// instant in two offset forms, at the root's stored key and at the record's
// source key.
func TestLoad_RevalidationFindsARecordByItsCanonicalSourceKey(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), recordSourceSchema, "rs.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	vi, res := instance.NewValidator(s).ValidateOne(t.Context(), "Event", instance.RawInstance{Properties: map[string]any{
		"at":   "2024-01-01T00:00:00Z",
		"NEXT": map[string]any{"_target_at": "2030-01-01T00:00:00Z", "weight": 5},
	}})
	if res.HasErrors() {
		t.Fatalf("validate: %s", res)
	}
	g := graph.New(s)
	if res := g.Add(t.Context(), vi); res.HasErrors() {
		t.Fatalf("add: %s", res)
	}
	data, res := snapshot.Marshal(t.Context(), g.Snapshot())
	if res.HasErrors() {
		t.Fatalf("marshal: %s", res)
	}
	for name, respell := range map[string]string{
		"the record spelled otherwise": `"source_key":["2024-01-01T00:00:00Z"]`,
		"the root spelled otherwise":   `{"key":["2024-01-01T00:00:00Z"]`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := editOnce(t, string(data), `"weight":5`, `"weight":50`)
			doc = editOnce(t, doc, respell, strings.Replace(respell, "00Z", "00+00:00", 1))

			opts := []snapshot.LoadOption{snapshot.WithIntegrityCheck(false), snapshot.WithIssueLimit(0)}
			if _, res := snapshot.Load(t.Context(), []byte(doc), s, opts...); !res.OK() {
				t.Fatalf("the document is structurally sound, and a load without revalidation reports: %s", res)
			}
			_, res := snapshot.Load(t.Context(), []byte(doc), s, append(opts, snapshot.WithRevalidation(diag.Error))...)
			got := revalidationFindings(res, "revalidation of rs#Event[[")
			if len(got) != 1 || !strings.Contains(got[0], "E_CONSTRAINT_FAIL: ") || res.Len() != 1 {
				t.Errorf("revalidation reports %q, want one E_CONSTRAINT_FAIL for the record's weight:\n%s", got, res)
			}
		})
	}
}

// Revalidation rebuilds a root's whole composed tree, so a value two levels
// down, in a Box inside a Box, is judged as the validator judges it there.
func TestLoad_RevalidationJudgesAGrandchildsValues(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	edited := []byte(editOnce(t, doc, sbInnerBox, strings.Replace(sbInnerBox, `"n":2`, `"n":"x"`, 1)))
	if _, res := snapshot.Load(t.Context(), edited, s, snapshot.WithIssueLimit(0)); !res.OK() {
		t.Fatalf("a load without revalidation judges no value: %s", res)
	}
	_, res := snapshot.Load(t.Context(), edited, s, snapshot.WithIssueLimit(0), snapshot.WithRevalidation(diag.Error))
	got := revalidationFindings(res, `revalidation of sb#Order[["o1"]]: `)
	if len(got) != 1 || !strings.HasPrefix(got[0], "E_TYPE_MISMATCH: ") || !strings.Contains(got[0], `"n"`) {
		t.Errorf("revalidation reports %q, want one E_TYPE_MISMATCH for the inner Box's n:\n%s", got, res)
	}
}
