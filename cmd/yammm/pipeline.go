package main

import (
	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// loadGraph assembles the graph as [assembleGraph] does and reports a summary
// line on success.
func loadGraph(cmd *cobra.Command, sink *cli.DiagnosticSink, s *schema.Schema, dataPath, fromFormat, typeName, typeColumn string) (diag.Result, *graph.Graph, error) {
	result, g, parsed, err := assembleGraph(cmd, s, dataPath, fromFormat, typeName, typeColumn)
	if err != nil {
		return diag.Result{}, nil, err
	}
	if !result.HasErrors() {
		instanceCount := 0
		for _, raws := range parsed {
			instanceCount += len(raws)
		}
		sink.Statusf("loaded %d instances of %d types\n", instanceCount, len(parsed))
	}
	return result, g, nil
}

// assembleGraph runs the whole data pipeline — detect the format, parse,
// validate every instance, build the graph — and returns the merged result,
// the graph, the parsed instances by type, and a usage or I/O error, which
// returns no graph. Building the graph is what checks primary-key uniqueness and
// resolves associations, so every data command runs it.
func assembleGraph(cmd *cobra.Command, s *schema.Schema, dataPath, fromFormat, typeName, typeColumn string) (diag.Result, *graph.Graph, map[string][]instance.RawInstance, error) {
	if fromFormat == "" {
		var err error
		fromFormat, err = cli.DetectFormat(dataPath)
		if err != nil {
			return diag.Result{}, nil, nil, err
		}
	}

	var parsed map[string][]instance.RawInstance
	var parseResult diag.Result
	var err error

	switch fromFormat {
	case "json":
		parsed, parseResult, err = cli.LoadAndParseJSON(cmd.Context(), dataPath)
	case "csv":
		if typeName == "" && typeColumn == "" {
			return diag.Result{}, nil, nil, cli.Usagef("CSV data requires --type or --type-column flag")
		}
		parsed, parseResult, err = cli.LoadAndParseCSV(cmd.Context(), dataPath, typeName, typeColumn, s)
	default:
		return diag.Result{}, nil, nil, cli.Usagef("unsupported format %q", fromFormat)
	}
	if err != nil {
		return diag.Result{}, nil, nil, err
	}

	valids, validateResult := cli.ValidateInstances(cmd.Context(), s, parsed)
	g, graphResult := cli.BuildGraph(cmd.Context(), s, valids)
	return cli.MergeResults(parseResult, validateResult, graphResult), g, parsed, nil
}
