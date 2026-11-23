package snapshot_test

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/snapshot"
)

// TestRead_AnEmptyEdgeListIsHeldToItsName pins that an edges entry holding no
// edge is held to the association rule by its name, as an edge is: Marshal
// writes no empty list, and one under a name the type does not declare as an
// association draws one E_GRAPH_UNKNOWN_RELATION on every read, revalidating
// ones included, which then judge no value of the root.
func TestRead_AnEmptyEdgeListIsHeldToItsName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadReaderSchema(t)
	doc := readerDoc(t, s,
		map[string]any{"$type": "Company", "id": "c1"},
		map[string]any{"$type": "Person", "id": "p0"},
		map[string]any{"$type": "Person", "id": "p1", "boss": map[string]any{"_target_id": "p0"}, "works_at": map[string]any{"_target_id": "c1"}},
	)
	for name, c := range map[string]struct {
		list    string
		refused bool
	}{
		"an empty list under an undeclared name":     {`"BOGUS":[]`, true},
		"a null list under an undeclared name":       {`"BOGUS":null`, true},
		"an empty list under a declared association": {`"FRIENDS":[]`, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			edited := []byte(editOnce(t, doc, `"edges":{"BOSS"`, `"edges":{`+c.list+`,"BOSS"`))
			_, plain := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
				return snapshot.Unmarshal(ctx, rehashDocument(t, edited), s, snapshot.WithWarnings(warn))
			})
			_, warn := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
				return snapshot.Unmarshal(ctx, rehashDocument(t, edited), s, snapshot.WithRevalidation(diag.Warning), snapshot.WithWarnings(warn))
			})
			_, fail := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
				return snapshot.Unmarshal(ctx, rehashDocument(t, edited), s, snapshot.WithRevalidation(diag.Error), snapshot.WithWarnings(warn))
			})
			for surface, res := range map[string]diag.Result{
				"Load": plain, "Load, revalidating at Warning": warn, "Load, revalidating at Error": fail,
				"Verify, revalidating at Error": diagtest.CaptureErr(t, func(warn func(diag.Issue)) error {
					return snapshot.Verify(ctx, rehashDocument(t, edited), s, snapshot.WithRevalidation(diag.Error), snapshot.WithWarnings(warn))
				}),
			} {
				failures := 0
				for issue := range res.Issues() {
					// The fixture's required records report at the option's severity.
					if (issue.Severity() == diag.Fatal || issue.Severity() == diag.Error) && issue.Code() != diag.W_SNAPSHOT_UNRESOLVED_REQUIRED {
						failures++
					}
				}
				switch {
				case !c.refused && failures != 0:
					t.Errorf("%s: %s, want no refusal", surface, res)
				case c.refused && (failures != 1 || !hasCode(res, diag.E_GRAPH_UNKNOWN_RELATION) || !strings.Contains(res.String(), `"BOGUS"`)):
					t.Errorf("%s: %s, want one E_GRAPH_UNKNOWN_RELATION naming \"BOGUS\"", surface, res)
				}
			}
		})
	}
}
