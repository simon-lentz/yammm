package main

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

const refusalSchema = `schema "grid"

type Grid {
	id String primary
	a Integer
	matrix List<List<Integer>>
	s String
	z Integer
	--> NEXT (_:many) Grid {
		when Timestamp
	}
}
`

// refusedSnapshot writes a .ys of one Grid holding props, and an edge to itself
// holding edge, built past the validator: export reads a .ys without
// revalidating it, so a value no validator would hold reaches the writer.
func refusedSnapshot(t *testing.T, dir string, props, edge map[string]any) (schemaPath, ysPath string) {
	t.Helper()
	schemaPath = filepath.Join(dir, "grid.yammm")
	if err := os.WriteFile(schemaPath, []byte(refusalSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s, res := schema.Load(ctx, schemaPath)
	if err := res.Err(); err != nil {
		t.Fatalf("load schema: %v", err)
	}
	grid, _ := s.Type("Grid")
	all := map[string]any{"id": "g1"}
	maps.Copy(all, props)
	var edges map[string]*instance.ValidEdgeData
	if edge != nil {
		edges = map[string]*instance.ValidEdgeData{"NEXT": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{"g1"}), immutable.WrapProperties(edge)),
		})}
	}
	g := graph.New(s)
	if r := g.Add(ctx, instance.NewValidInstance("Grid", grid.ID(), immutable.WrapKey([]any{"g1"}),
		immutable.WrapProperties(all), edges, nil, nil)); r.HasErrors() {
		t.Fatalf("graph.Add: %v", r.Err())
	}
	data, mres := snapshot.Marshal(ctx, g.Snapshot())
	if err := mres.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	ysPath = filepath.Join(dir, "grid.ys")
	if err := os.WriteFile(ysPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return schemaPath, ysPath
}

// Every value adapter/neo4j refuses is a refusal of the data, so `--to cypher`
// exits 1 over it whichever property's fault the write meets first, at a node
// or at an edge.
func TestExport_CypherRefusesAValueAtOneExitWhateverItsOrder(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		props, edge map[string]any
	}{
		"a float at an Integer sorting before a nested collection": {props: map[string]any{"a": 2.5, "matrix": []any{[]any{int64(1)}}}},
		"a float at an Integer sorting after a nested collection":  {props: map[string]any{"z": 2.5, "matrix": []any{[]any{int64(1)}}}},
		"a Timestamp that does not parse at an edge":               {edge: map[string]any{"when": "not a time"}},
		"a map at a String": {props: map[string]any{"s": map[string]any{"k": "v"}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			schemaPath, ysPath := refusedSnapshot(t, t.TempDir(), tc.props, tc.edge)
			code, out, errOut := executeCmdOutput(t, "export", "--to", "cypher", schemaPath, ysPath)
			if code != cli.ExitValidation || out != "" {
				t.Errorf("exit %d, stdout %q; want %d and no statement (stderr %q)", code, out, cli.ExitValidation, errOut)
			}
		})
	}
}
