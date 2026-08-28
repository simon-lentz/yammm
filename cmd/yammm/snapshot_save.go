package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

func newSnapshotSaveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "save <schema.yammm> <data-file> [data-file...]",
		Short: "Build a graph snapshot and save to a .ys file",
		Long: `Load schema, parse data files, validate, build graph, and save as a
persisted snapshot. Accepts multiple data files accumulated into a single graph.

Output is byte-level deterministic by default (no created_at timestamp).
Use --timestamp to include a creation timestamp.`,
		Args: cobra.MinimumNArgs(2),
		RunE: runSnapshotSave,
	}

	cmd.Flags().StringP("output", "o", "", "output path for the .ys file (required)")
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

func runSnapshotSave(cmd *cobra.Command, args []string) error {
	formatStr, _ := cmd.Flags().GetString("format")
	noColor, _ := cmd.Flags().GetBool("no-color")
	outputPath, _ := cmd.Flags().GetString("output")
	fromFormat, _ := cmd.Flags().GetString("from")
	typeName, _ := cmd.Flags().GetString("type")
	typeColumn, _ := cmd.Flags().GetString("type-column")
	metadataRaw, _ := cmd.Flags().GetStringArray("metadata")
	timestamp, _ := cmd.Flags().GetBool("timestamp")
	indent, _ := cmd.Flags().GetBool("indent")
	intoPath, _ := cmd.Flags().GetString("into")

	outputFormat, err := cli.ParseOutputFormat(formatStr)
	if err != nil {
		return err
	}

	// Validate output destination: either --output or --into must be set.
	if outputPath == "" && intoPath == "" {
		fmt.Fprintf(os.Stderr, "error: either --output or --into is required\n")
		return &cli.ExitError{Code: cli.ExitUsage}
	}
	if outputPath == "" {
		outputPath = intoPath // default: overwrite the --into file
	}

	// Parse metadata key=value pairs.
	metadata, err := parseMetadata(metadataRaw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	schemaPath := args[0]
	dataPaths := args[1:]

	absSchemaPath, err := filepath.Abs(schemaPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: resolve path %q: %v\n", schemaPath, err)
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	// Load schema.
	moduleRoot, loadOpts, err := moduleRootOptions(cmd)
	if err != nil {
		return err
	}
	s, schemaResult := schema.Load(cmd.Context(), absSchemaPath, loadOpts...)
	pending, failed := reportSchemaLoad(cmd, outputFormat, noColor, s, moduleRoot, absSchemaPath, schemaResult)
	if failed {
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Load existing snapshot if --into is set. Its diagnostics join the pending
	// set rather than rendering on their own: a warning here (an unsupported
	// hash algorithm, say) means the imported snapshot's integrity was not fully
	// verified, and it must reach the operator without turning one invocation
	// into two rendered results.
	var g *graph.Graph
	if intoPath != "" {
		snap, loadResult, loadErr := cli.LoadSnapshotFile(cmd.Context(), intoPath, s)
		if loadErr != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", loadErr)
			return &cli.ExitError{Code: cli.ExitRuntime}
		}
		pending = cli.MergeResults(pending, loadResult)
		if loadResult.HasErrors() {
			renderDiagnostics(cmd, outputFormat, noColor, s, diagRootFor(s, moduleRoot, absSchemaPath), pending)
			return &cli.ExitError{Code: cli.ExitValidation}
		}
		g = graph.NewFromSnapshot(s, snap)
	}

	// Parse all data files.
	allParsed, parseResult, err := parseDataFiles(cmd, s, dataPaths, fromFormat, typeName, typeColumn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	// Validate instances.
	valids, validateResult := cli.ValidateInstances(cmd.Context(), s, allParsed)

	// Build graph: fresh build or add to imported graph.
	var graphResult diag.Result
	if g != nil {
		graphResult = addInstancesToGraph(cmd.Context(), g, valids)
	} else {
		g, graphResult = cli.BuildGraph(cmd.Context(), s, valids)
	}

	// Merge all diagnostics and render once, whatever their severity: the
	// pending set carries load and imported-snapshot warnings that are only
	// reachable here.
	result := cli.MergeResults(pending, parseResult, validateResult, graphResult)
	renderDiagnostics(cmd, outputFormat, noColor, s, diagRootFor(s, moduleRoot, absSchemaPath), result)
	if result.HasErrors() {
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Create snapshot and marshal.
	snap := g.Snapshot()

	var opts []snapshot.Option
	if timestamp {
		opts = append(opts, snapshot.WithCreatedAt(time.Now()))
	}
	if indent {
		opts = append(opts, snapshot.WithIndent("\t"))
	}
	if len(metadata) > 0 {
		opts = append(opts, snapshot.WithMetadata(metadata))
	}

	data, marshalResult := snapshot.Marshal(cmd.Context(), snap, opts...)
	if err := marshalResult.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "error: marshal snapshot: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	// Warn on non-.ys extension.
	w := cmd.ErrOrStderr()
	if !strings.HasSuffix(outputPath, ".ys") {
		fmt.Fprintf(w, "warning: output path %q does not use the .ys extension\n", outputPath)
	}

	// Write file.
	if err := writeOutput(data, outputPath, intoPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: write output: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	// Print summary.
	instanceCount := 0
	for _, raws := range allParsed {
		instanceCount += len(raws)
	}
	fmt.Fprintf(w, "saved snapshot: %d instances of %d types\n", instanceCount, len(allParsed))

	return nil
}

// addInstancesToGraph adds validated instances to an existing graph and runs
// graph-level checks. Used for the --into workflow.
func addInstancesToGraph(ctx context.Context, g *graph.Graph, valids []*instance.ValidInstance) diag.Result {
	collector := diag.NewCollectorUnlimited()
	for _, valid := range valids {
		result := g.Add(ctx, valid)
		collector.Merge(result)
	}
	checkResult := g.Check(ctx)
	collector.Merge(checkResult)
	return collector.Result()
}

// writeOutput writes data to outputPath. When --into and --output point to the
// same file, uses atomic write (temp file + rename) to prevent data loss.
func writeOutput(data []byte, outputPath, intoPath string) error {
	if intoPath != "" && outputPath == intoPath {
		return atomicWrite(data, outputPath)
	}
	return os.WriteFile(outputPath, data, 0o600)
}

// atomicWrite writes data to a temp file in the same directory, then renames
// it to the target path. This prevents data loss if the process crashes.
func atomicWrite(data []byte, path string) (retErr error) {
	tmpFile, err := os.CreateTemp(filepath.Dir(path), ".yammm-save-*.ys")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		if retErr != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// parseDataFiles parses multiple data files into a merged instance map.
func parseDataFiles(cmd *cobra.Command, s *schema.Schema, dataPaths []string, fromFormat, typeName, typeColumn string) (map[string][]instance.RawInstance, diag.Result, error) {
	allParsed := make(map[string][]instance.RawInstance)
	collector := diag.NewCollectorUnlimited()

	for _, dataPath := range dataPaths {
		absDataPath, err := filepath.Abs(dataPath)
		if err != nil {
			return nil, diag.Result{}, fmt.Errorf("resolve path %q: %w", dataPath, err)
		}

		// Detect format per file if not overridden.
		format := fromFormat
		if format == "" {
			format, err = cli.DetectFormat(absDataPath)
			if err != nil {
				return nil, diag.Result{}, err
			}
		}

		var parsed map[string][]instance.RawInstance
		var parseResult diag.Result

		switch format {
		case "json":
			parsed, parseResult, err = cli.LoadAndParseJSON(cmd.Context(), absDataPath)
		case "csv":
			if typeName == "" && typeColumn == "" {
				return nil, diag.Result{}, errors.New("CSV data requires --type or --type-column flag")
			}
			parsed, parseResult, err = cli.LoadAndParseCSV(cmd.Context(), absDataPath, typeName, typeColumn, s)
		default:
			return nil, diag.Result{}, fmt.Errorf("unsupported format %q for %s", format, dataPath)
		}
		if err != nil {
			return nil, diag.Result{}, err
		}

		collector.Merge(parseResult)

		// Merge parsed instances.
		for typeName, instances := range parsed {
			allParsed[typeName] = append(allParsed[typeName], instances...)
		}
	}

	return allParsed, collector.Result(), nil
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
