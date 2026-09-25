package main

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

func newSnapshotSaveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "save <schema.yammm> <data-file> [data-file...]",
		Short: "Build a graph snapshot and save to a .ys file",
		Long: `Load schema, parse data files, validate, build graph, and save as a
persisted snapshot. Accepts multiple data files accumulated into a single graph.

Output is byte-level deterministic by default: no created_at timestamp is
written unless --timestamp stamps the current time, or --into carries the
merged file's own created_at forward.`,
		Args: cobra.MinimumNArgs(2),
		RunE: withDiagnostics(runSnapshotSave),
	}

	cmd.Flags().StringP("output", "o", "", "output path for the .ys file (required unless --into is given, which defaults it to the merged file)")
	cmd.Flags().String("from", "", "input format override: json or csv (auto-detected if not set)")
	cmd.Flags().String("type", "", "type name for CSV data (required for single-type CSV)")
	cmd.Flags().String("type-column", "", "column name containing type names (for multi-type CSV)")
	cmd.Flags().StringArrayP("metadata", "m", nil, "key=value metadata pair (repeatable)")
	cmd.Flags().Bool("timestamp", false, "include created_at timestamp in header (breaks determinism)")
	cmd.Flags().Bool("indent", false, "produce indented output for human inspection")
	cmd.Flags().String("into", "", "existing .ys file to merge new data into")

	registerModuleRootFlag(cmd)
	return cmd
}

func runSnapshotSave(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	outputPath, _ := cmd.Flags().GetString("output")
	metadataRaw, _ := cmd.Flags().GetStringArray("metadata")
	timestamp, _ := cmd.Flags().GetBool("timestamp")
	indent, _ := cmd.Flags().GetBool("indent")
	intoPath, _ := cmd.Flags().GetString("into")

	// Validate output destination: either --output or --into must be set.
	if outputPath == "" && intoPath == "" {
		return cli.Usagef("either --output or --into is required")
	}
	if outputPath == "" {
		outputPath = intoPath // default: overwrite the --into file
	}

	// Parse metadata key=value pairs.
	metadata, err := parseMetadata(metadataRaw)
	if err != nil {
		return cli.Usagef("%v", err)
	}

	schemaPath := args[0]
	in, err := dataInputOf(cmd, args[1:]...)
	if err != nil {
		return err
	}

	absSchemaPath, err := filepath.Abs(schemaPath)
	if err != nil {
		return cli.Usagef("resolve path %q: %v", schemaPath, err)
	}

	// Load schema.
	moduleRoot, loadOpts, err := moduleRootOptions(cmd, sink)
	if err != nil {
		return err
	}
	s, schemaResult := schema.Load(cmd.Context(), absSchemaPath, loadOpts...)
	if err := reportSchemaLoad(sink, s, moduleRoot, absSchemaPath, schemaResult); err != nil {
		return err
	}

	// Load existing snapshot if --into is set. A warning here (an unsupported
	// hash algorithm, say) means the imported snapshot's integrity was not fully
	// verified, and it reaches the operator whether or not this command gets as
	// far as writing anything.
	var g *graph.Graph
	var imported *snapshot.HeaderInfo
	if intoPath != "" {
		snap, header, loadResult, loadErr := cli.LoadSnapshotFile(cmd.Context(), intoPath, s)
		if loadErr != nil {
			return cli.Runtimef("%v", loadErr)
		}
		sink.Add(loadResult)
		if loadResult.HasErrors() {
			return &cli.ExitError{Code: cli.ExitValidation}
		}
		var importResult diag.Result
		g, importResult = graph.NewFromSnapshot(s, snap)
		sink.Add(importResult)
		if importResult.HasErrors() {
			return &cli.ExitError{Code: cli.ExitForResult(importResult)}
		}
		imported = header
	}

	a, err := assembleGraph(cmd, sink, s, in, g)
	if err != nil {
		return err
	}
	g = a.graph
	// The data file's own read failure rides this result for a streamed format,
	// so the exit rule decides: an I/O failure outranks a validation one.
	if sink.Result().HasErrors() {
		return &cli.ExitError{Code: cli.ExitForResult(sink.Result())}
	}

	// Create snapshot and marshal.
	snap := g.Snapshot()

	var opts []snapshot.Option
	switch {
	case timestamp:
		opts = append(opts, snapshot.WithCreatedAt(time.Now()))
	case imported != nil:
		// Exclusive, not additive: WithCreatedAtFrom wins over WithCreatedAt,
		// so appending both would make --timestamp silently do nothing.
		opts = append(opts, snapshot.WithCreatedAtFrom(imported))
	}
	if indent {
		opts = append(opts, snapshot.WithIndent("\t"))
	}
	if merged := mergeMetadata(imported, metadata); len(merged) > 0 {
		opts = append(opts, snapshot.WithMetadata(merged))
	}

	// Marshal reports a failure as an Error and a value it could not put on the
	// wire as a Warning; both are diagnostics, so both reach the sink.
	data, marshalResult := snapshot.Marshal(cmd.Context(), snap, opts...)
	sink.Add(marshalResult)
	if marshalResult.Err() != nil {
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	// One primitive whether or not --into names the same file, so the write is
	// atomic however the two flags spell the path.
	if err := cli.WriteFile(outputPath, data); err != nil {
		return cli.Runtimef("write output: %v", err)
	}

	// Raised only once the file exists: the code states that a snapshot was
	// written where a reader that discovers snapshots by extension will not find it.
	if !strings.HasSuffix(outputPath, ".ys") {
		c := diag.NewCollectorUnlimited()
		c.Collect(diag.NewIssue(diag.Warning, diag.W_SNAPSHOT_PATH_EXTENSION,
			fmt.Sprintf("output path %q does not use the .ys extension", outputPath)).
			WithPath(outputPath, "").Build())
		sink.Add(c.Result())
	}

	// Every diagnostic this invocation can produce is in the sink by now, so
	// flushing here keeps them above the line that says the work finished.
	sink.Flush()
	instanceCount, typeCount := countSnapshot(cmd.Context(), snap, data)
	sink.Statusf("saved snapshot: %d instances of %d types\n", instanceCount, typeCount)

	return nil
}

// countSnapshot counts what the written document holds: its root instances
// from the graph, and its types from the type table the bytes carry, which
// lists a composed child's type and is the set `snapshot info` reports.
func countSnapshot(ctx context.Context, snap *graph.Snapshot, data []byte) (instances, types int) {
	for _, id := range snap.Types() {
		instances += len(snap.InstancesOf(id))
	}
	if header, _ := snapshot.HeaderOnlyRead(ctx, bytes.NewReader(data)); header != nil {
		types = len(header.Types)
	}
	return instances, types
}

// mergeMetadata overlays flag pairs onto the imported header's. The header's
// annotations survive a merge that does not name them, and a flag wins on a
// key that appears in both.
func mergeMetadata(imported *snapshot.HeaderInfo, flags map[string]string) map[string]string {
	merged := make(map[string]string, len(flags))
	if imported != nil {
		maps.Copy(merged, imported.Metadata)
	}
	maps.Copy(merged, flags)
	return merged
}

// parseMetadata parses key=value metadata pairs from --metadata flag values.
// Returns an empty (non-nil) map when raw is empty.
func parseMetadata(raw []string) (map[string]string, error) {
	m := make(map[string]string, len(raw))
	if len(raw) == 0 {
		return m, nil
	}
	for _, kv := range raw {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, fmt.Errorf("invalid metadata %q: expected key=value format", kv)
		}
		if k == "" {
			return nil, fmt.Errorf("invalid metadata %q: key must not be empty", kv)
		}
		m[k] = v
	}
	return m, nil
}
