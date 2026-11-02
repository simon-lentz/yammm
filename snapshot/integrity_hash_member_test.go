package snapshot_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// The hash reads the header's own member, so a schema name or a metadata key that
// spells "integrity_hash" neither breaks a written document nor shields one that
// was edited after it was written.
func TestIntegrityHash_ReadsTheHeadersOwnMember(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	for name, c := range map[string]struct {
		schemaName string
		opts       []snapshot.Option
	}{
		"a schema of that name":            {schemaName: "integrity_hash"},
		"a metadata key of that name":      {schemaName: "plain", opts: []snapshot.Option{snapshot.WithMetadata(map[string]string{"integrity_hash": "x"})}},
		"a schema of that name, indented":  {schemaName: "integrity_hash", opts: []snapshot.Option{snapshot.WithIndent("  ")}},
		"a metadata key of that name, tab": {schemaName: "plain", opts: []snapshot.Option{snapshot.WithIndent("\t"), snapshot.WithMetadata(map[string]string{"integrity_hash": "x"})}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, res := schema.LoadString(ctx, `schema "`+c.schemaName+`"

type T {
	id String primary
}
`, c.schemaName+".yammm")
			if res.HasErrors() {
				t.Fatalf("schema: %s", res)
			}
			g := graph.New(s)
			if r := g.Add(ctx, instancetest.Valid(t, s, "T", map[string]any{"id": "written"})); r.HasErrors() {
				t.Fatalf("add: %s", r)
			}
			data, res := snapshot.Marshal(ctx, g.Snapshot(), c.opts...)
			if res.HasErrors() {
				t.Fatalf("marshal: %s", res)
			}
			if _, res := snapshot.Load(ctx, data, s); res.HasErrors() {
				t.Errorf("Load: %s", res)
			}
			if res := snapshot.Verify(ctx, data, s); res.HasErrors() {
				t.Errorf("Verify: %s", res)
			}
			if _, res := snapshot.Info(ctx, data); res.HasErrors() {
				t.Errorf("Info: %s", res)
			}
			edited := []byte(strings.ReplaceAll(string(data), `"written"`, `"changed"`))
			if res := snapshot.Verify(ctx, edited, s); !res.HasCode(diag.E_SNAPSHOT_INTEGRITY_MISMATCH) {
				t.Errorf("Verify of an edited document: %s, want %s", res, diag.E_SNAPSHOT_INTEGRITY_MISMATCH)
			}
			updated, res := snapshot.UpdateMetadata(ctx, data, map[string]string{"integrity_hash": "y"})
			if res.HasErrors() {
				t.Fatalf("UpdateMetadata: %s", res)
			}
			if res := snapshot.Verify(ctx, updated, s); res.HasErrors() {
				t.Errorf("Verify after UpdateMetadata: %s", res)
			}
		})
	}
}
