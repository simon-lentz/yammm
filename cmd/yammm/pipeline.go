package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// loadGraph runs the full pipeline: detect format, parse, validate, build graph.
// Returns the merged diagnostic result, the graph (may be nil on error), and any I/O error.
func loadGraph(cmd *cobra.Command, s *schema.Schema, dataPath, fromFormat, typeName, typeColumn string) (diag.Result, *graph.Graph, error) {
	// Detect format
	if fromFormat == "" {
		var err error
		fromFormat, err = cli.DetectFormat(dataPath)
		if err != nil {
			return diag.Result{}, nil, err
		}
	}

	// Parse data
	var parsed map[string][]instance.RawInstance
	var parseResult diag.Result
	var err error

	switch fromFormat {
	case "json":
		parsed, parseResult, err = cli.LoadAndParseJSON(cmd.Context(), dataPath)
	case "csv":
		if typeName == "" && typeColumn == "" {
			return diag.Result{}, nil, errors.New("CSV data requires --type or --type-column flag")
		}
		parsed, parseResult, err = cli.LoadAndParseCSV(cmd.Context(), dataPath, typeName, typeColumn, s)
	default:
		return diag.Result{}, nil, fmt.Errorf("unsupported format %q", fromFormat)
	}
	if err != nil {
		return diag.Result{}, nil, err
	}

	// Validate instances
	valids, validateResult := cli.ValidateInstances(cmd.Context(), s, parsed)

	// Build graph
	g, graphResult := cli.BuildGraph(cmd.Context(), s, valids)

	// Merge all results
	result := cli.MergeResults(parseResult, validateResult, graphResult)

	// Print summary to stderr
	if !result.HasErrors() {
		typeCount := len(parsed)
		instanceCount := 0
		for _, raws := range parsed {
			instanceCount += len(raws)
		}
		fmt.Fprintf(os.Stderr, "loaded %d instances of %d types\n", instanceCount, typeCount)
	}

	return result, g, nil
}
