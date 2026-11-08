package main

import (
	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// checkDataOperands refuses a data operand that is empty or not valid UTF-8, since
// its identity reaches both JSON wires. It reads no file, so a command that calls
// it before its first read reports a usage error alone.
func checkDataOperands(paths ...string) error {
	for _, path := range paths {
		if err := cli.CheckSourceOperand("data file", path); err != nil {
			return err
		}
	}
	return nil
}

// assembleGraph is the one data pipeline: the JSON files are one document, whose
// valid instances go into base or a new graph ([cli.BuildGraph]). Each file's parse
// diagnostics reach sink as it is read.
func assembleGraph(cmd *cobra.Command, sink *cli.DiagnosticSink, s schema.Schema, paths []string, base *graph.Graph) (*graph.Graph, error) {
	parsed := make(map[string][]instance.RawInstance)
	for _, path := range paths {
		fileParsed, parseResult, err := cli.LoadAndParseJSON(cmd.Context(), path)
		if err != nil {
			return nil, err
		}
		sink.Add(parseResult)
		for typeName, raws := range fileParsed {
			parsed[typeName] = append(parsed[typeName], raws...)
		}
	}
	doc, validateResult := cli.ValidateInstances(cmd.Context(), s, parsed)
	g, graphResult := cli.BuildGraph(cmd.Context(), s, base, doc)
	sink.Add(validateResult, graphResult)
	return g, nil
}
