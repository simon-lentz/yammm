package snapshot_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/diagtest"
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
			s, res := diagtest.Capture(t, func(warn func(diag.Issue)) (schema.Schema, error) {
				return schema.LoadString(ctx, `schema "`+c.schemaName+`"

type T {
	id String primary
}
`, c.schemaName+".yammm", schema.WithWarnings(warn))
			})
			if diagtest.Err(res) != nil {
				t.Fatalf("schema: %s", res)
			}
			g := graph.New(s)
			if err := g.Add(ctx, instancetest.Valid(t, s, "T", map[string]any{"id": "written"})); err != nil {
				t.Fatalf("add: %s", err)
			}
			data, res := diagtest.Issues(t, func() ([]byte, error) {
				return snapshot.Marshal(ctx, g.Snapshot(), c.opts...)
			})
			if diagtest.Err(res) != nil {
				t.Fatalf("marshal: %s", res)
			}
			if _, err := snapshot.Unmarshal(ctx, data, s); err != nil {
				t.Errorf("Load: %s", err)
			}
			if err := snapshot.Verify(ctx, data, s); err != nil {
				t.Errorf("Verify: %s", err)
			}
			if _, err := snapshot.Stat(ctx, data); err != nil {
				t.Errorf("Info: %s", err)
			}
			edited := []byte(strings.ReplaceAll(string(data), `"written"`, `"changed"`))
			if err := snapshot.Verify(ctx, edited, s); !errors.Is(err, diag.E_SNAPSHOT_INTEGRITY_MISMATCH) {
				t.Errorf("Verify of an edited document: %s, want %s", err, diag.E_SNAPSHOT_INTEGRITY_MISMATCH)
			}
			updated, res := diagtest.Issues(t, func() ([]byte, error) {
				return snapshot.UpdateMetadata(ctx, data, map[string]string{"integrity_hash": "y"})
			})
			if diagtest.Err(res) != nil {
				t.Fatalf("UpdateMetadata: %s", res)
			}
			if err := snapshot.Verify(ctx, updated, s); err != nil {
				t.Errorf("Verify after UpdateMetadata: %s", err)
			}
		})
	}
}
