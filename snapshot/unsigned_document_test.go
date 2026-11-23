package snapshot_test

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// A header that holds no hash attests nothing, and every writer stores the
// hash it computed, so each surface that checks the hash refuses an empty, a
// null and an absent integrity_hash as one mismatch, which says so. A caller
// that skips the check reads the document.
func TestRead_AHeaderThatHoldsNoHashIsAMismatch(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, err := schema.LoadString(ctx, "schema \"unsigned\"\n\ntype T {\n\tid String primary\n}\n", "unsigned.yammm")
	if err != nil {
		t.Fatalf("schema: %s", err)
	}
	g := graph.New(s)
	if err := g.Add(ctx, instancetest.Valid(t, s, "T", map[string]any{"id": "t1"})); err != nil {
		t.Fatalf("add: %s", err)
	}
	signed := regexp.MustCompile(`(,\s*)"integrity_hash":( ?)"(sha256:[0-9a-f]{64})"`)
	for layout, opts := range map[string][]snapshot.Option{
		"compact":  nil,
		"indented": {snapshot.WithIndent("  ")},
	} {
		data, err := snapshot.Marshal(ctx, g.Snapshot(), opts...)
		if err != nil {
			t.Fatalf("marshal: %s", err)
		}
		if n := len(signed.FindAll(data, -1)); n != 1 {
			t.Fatalf("%s: Marshal wrote %d signed integrity_hash members, want 1:\n%s", layout, n, data)
		}
		hash := string(signed.FindSubmatch(data)[3])
		for form, member := range map[string]string{
			"empty":  `${1}"integrity_hash":${2}""`,
			"null":   `${1}"integrity_hash":${2}null`,
			"absent": ``,
		} {
			unsigned := signed.ReplaceAll(data, []byte(member))
			t.Run(layout+"/"+form, func(t *testing.T) {
				t.Parallel()
				want := map[diag.Code]int{diag.E_SNAPSHOT_INTEGRITY_MISMATCH: 1}
				_, lres := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
					return snapshot.Unmarshal(ctx, unsigned, s, snapshot.WithWarnings(warn))
				})
				if !maps.Equal(errorCodes(lres), want) {
					t.Errorf("Load: %v, want %v:\n%s", errorCodes(lres), want, lres)
				}
				// An empty or a null hash leaves the document Marshal hashed, so the
				// actual hash is the one Marshal wrote; an absent one has none.
				wantDetails := []diag.Detail{{Key: diag.DetailKeyExpectedHash, Value: ""}}
				if form != "absent" {
					wantDetails = append(wantDetails, diag.Detail{Key: diag.DetailKeyActualHash, Value: hash})
				}
				for issue := range lres.Errors() {
					if !strings.Contains(issue.Message(), "holds no hash") || !strings.Contains(issue.Hint(), "skip the check") {
						t.Errorf("Load: the mismatch does not say the header holds no hash: %s (hint %q)", issue.Message(), issue.Hint())
					}
					if !slices.Equal(issue.Details(), wantDetails) {
						t.Errorf("Load: details %v, want %v", issue.Details(), wantDetails)
					}
				}
				if vres := diagtest.CaptureErr(t, func(warn func(diag.Issue)) error {
					return snapshot.Verify(ctx, unsigned, s, snapshot.WithWarnings(warn))
				}); !maps.Equal(errorCodes(vres), want) {
					t.Errorf("Verify: %v, want %v:\n%s", errorCodes(vres), want, vres)
				}
				info, ires := diagtest.Issues(t, func() (*snapshot.Info, error) {
					return snapshot.Stat(ctx, unsigned)
				})
				if !maps.Equal(errorCodes(ires), want) {
					t.Errorf("Info: %v, want %v:\n%s", errorCodes(ires), want, ires)
				}
				if info == nil || info.IntegrityStatus != snapshot.IntegrityMismatch {
					t.Errorf("Stat: IntegrityStatus of %+v, want mismatch", info)
				}

				skip := snapshot.WithIntegrityCheck(false)
				if _, err := snapshot.Unmarshal(ctx, unsigned, s, skip); err != nil {
					t.Errorf("Load skipping the check: %s", err)
				}
				if err := snapshot.Verify(ctx, unsigned, s, skip); err != nil {
					t.Errorf("Verify skipping the check: %s", err)
				}
			})
		}
	}
}
