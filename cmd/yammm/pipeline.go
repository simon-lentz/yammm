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

// assembly is what the data pipeline hands a command: the graph, and how many
// instances of how many type names the document lists.
type assembly struct {
	graph     *graph.Graph
	instances int
	types     int
}

// loadGraph assembles the graph as [assembleGraph] does and, when nothing the
// invocation diagnosed is an error, reports a summary line.
func loadGraph(cmd *cobra.Command, sink *cli.DiagnosticSink, s schema.Schema, path string) (*graph.Graph, error) {
	a, err := assembleGraph(cmd, sink, s, []string{path}, nil)
	if err != nil {
		return nil, err
	}
	reportLoaded(sink, a)
	return a.graph, nil
}

// reportLoaded reports a summary line when nothing the invocation diagnosed is
// an error.
func reportLoaded(sink *cli.DiagnosticSink, a assembly) {
	if !sink.Result().HasErrors() {
		sink.Statusf("loaded %d instances of %d types\n", a.instances, a.types)
	}
}

// assembleGraph is the one data pipeline: the JSON files are one document, and its
// valid instances go into base or a new graph ([assembleParsed]). Each file's parse
// diagnostics reach sink as it is read.
func assembleGraph(cmd *cobra.Command, sink *cli.DiagnosticSink, s schema.Schema, paths []string, base *graph.Graph) (assembly, error) {
	parsed := make(map[string][]instance.RawInstance)
	for _, path := range paths {
		fileParsed, parseResult, err := cli.LoadAndParseJSON(cmd.Context(), path)
		if err != nil {
			return assembly{}, err
		}
		sink.Add(parseResult)
		for typeName, raws := range fileParsed {
			parsed[typeName] = append(parsed[typeName], raws...)
		}
	}
	return assembleParsed(cmd, sink, s, parsed, base), nil
}

// assembleParsed validates a parsed document and adds its valid instances to base
// or a new graph ([cli.BuildGraph]).
func assembleParsed(cmd *cobra.Command, sink *cli.DiagnosticSink, s schema.Schema, parsed map[string][]instance.RawInstance, base *graph.Graph) assembly {
	doc, validateResult := cli.ValidateInstances(cmd.Context(), s, parsed)
	g, graphResult := cli.BuildGraph(cmd.Context(), s, base, doc)
	sink.Add(validateResult, graphResult)

	a := assembly{graph: g, types: len(parsed)}
	for _, raws := range parsed {
		a.instances += len(raws)
	}
	return a
}
