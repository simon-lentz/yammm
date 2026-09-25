package main

import (
	"slices"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// dataInput is what a data command reads: its data files, each file's format,
// and the CSV type flags.
type dataInput struct {
	paths      []string
	formats    []string
	typeName   string
	typeColumn string
}

// dataInputOf reads the data flags every data command registers and decides
// each file's format from --from or its name. It reads no file, so a command
// that calls it before its first read reports a usage error alone.
func dataInputOf(cmd *cobra.Command, paths ...string) (dataInput, error) {
	from, _ := cmd.Flags().GetString("from")
	typeName, _ := cmd.Flags().GetString("type")
	typeColumn, _ := cmd.Flags().GetString("type-column")
	in := dataInput{paths: paths, formats: make([]string, len(paths)), typeName: typeName, typeColumn: typeColumn}
	for i, path := range paths {
		// The two operand refusals location.ResolveSourcePath makes before any
		// lookup, made here so they precede the command's first read.
		switch {
		case path == "":
			return dataInput{}, cli.Usagef("resolve data file %q: %w", path, location.ErrEmptyPath)
		case !utf8.ValidString(path):
			return dataInput{}, cli.Usagef("resolve data file %q: %w: %q", path, location.ErrInvalidUTF8Path, path)
		}
		format := from
		if format == "" {
			var err error
			if format, err = cli.DetectFormat(path); err != nil {
				return dataInput{}, err
			}
		}
		switch format {
		case "json":
		case "csv":
			if typeName == "" && typeColumn == "" {
				return dataInput{}, cli.Usagef("CSV data requires --type or --type-column flag")
			}
		default:
			return dataInput{}, cli.Usagef("unsupported format %q", format)
		}
		in.formats[i] = format
	}
	return in, nil
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
func loadGraph(cmd *cobra.Command, sink *cli.DiagnosticSink, s *schema.Schema, in dataInput) (*graph.Graph, error) {
	a, err := assembleGraph(cmd, sink, s, in, nil)
	if err != nil {
		return nil, err
	}
	if !sink.Result().HasErrors() {
		sink.Statusf("loaded %d instances of %d types\n", a.instances, a.types)
	}
	return a.graph, nil
}

// assembleGraph is the one data pipeline: the files are one document, and its
// valid instances go into base or a new graph ([cli.BuildGraph]). A --type the
// schema lacks, given for CSV data without --type-column, is refused before any
// file is read, and each file's parse diagnostics reach sink as it is read.
func assembleGraph(cmd *cobra.Command, sink *cli.DiagnosticSink, s *schema.Schema, in dataInput, base *graph.Graph) (assembly, error) {
	if in.typeColumn == "" && slices.Contains(in.formats, "csv") {
		if _, ok := s.ResolveTypeName(in.typeName); !ok {
			return assembly{}, cli.Usagef("type %q not found in schema", in.typeName)
		}
	}
	parsed := make(map[string][]instance.RawInstance)
	for i, path := range in.paths {
		var fileParsed map[string][]instance.RawInstance
		var parseResult diag.Result
		var err error
		if in.formats[i] == "json" {
			fileParsed, parseResult, err = cli.LoadAndParseJSON(cmd.Context(), path)
		} else {
			fileParsed, parseResult, err = cli.LoadAndParseCSV(cmd.Context(), path, in.typeName, in.typeColumn, s)
		}
		if err != nil {
			return assembly{}, err
		}
		sink.Add(parseResult)
		for typeName, raws := range fileParsed {
			parsed[typeName] = append(parsed[typeName], raws...)
		}
	}

	doc, validateResult := cli.ValidateInstances(cmd.Context(), s, parsed)
	g, graphResult := cli.BuildGraph(cmd.Context(), s, base, doc)
	sink.Add(validateResult, graphResult)

	a := assembly{graph: g, types: len(parsed)}
	for _, raws := range parsed {
		a.instances += len(raws)
	}
	return a, nil
}
