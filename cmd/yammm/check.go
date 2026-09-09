package main

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

func newCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check <schema.yammm> <data-file>",
		Short: "Validate data against a schema",
		Long:  "Validate JSON or CSV data against a yammm schema. Input format is auto-detected from file extension.",
		Args:  cobra.ExactArgs(2),
		RunE:  runCheck,
	}

	cmd.Flags().String("from", "", "input format override: json or csv")
	cmd.Flags().String("type", "", "type name for CSV data (required for single-type CSV)")
	cmd.Flags().String("type-column", "", "column name containing type names (for multi-type CSV)")

	registerModuleRootFlag(cmd)
	return cmd
}

func runCheck(cmd *cobra.Command, args []string) error {
	formatStr, _ := cmd.Flags().GetString("format")
	noColor, _ := cmd.Flags().GetBool("no-color")
	fromFormat, _ := cmd.Flags().GetString("from")
	typeName, _ := cmd.Flags().GetString("type")
	typeColumn, _ := cmd.Flags().GetString("type-column")

	outputFormat, err := cli.ParseOutputFormat(formatStr)
	if err != nil {
		return err
	}

	schemaPath := args[0]
	dataPath := args[1]

	absSchemaPath, err := filepath.Abs(schemaPath)
	if err != nil {
		return cli.Usagef("resolve path %q: %v", schemaPath, err)
	}

	// Load schema
	moduleRoot, loadOpts, err := moduleRootOptions(cmd)
	if err != nil {
		return err
	}
	s, schemaResult := schema.Load(cmd.Context(), absSchemaPath, loadOpts...)
	pending, loadErr := reportSchemaLoad(cmd, outputFormat, noColor, s, moduleRoot, absSchemaPath, schemaResult)
	if loadErr != nil {
		return loadErr
	}

	// Detect format
	if fromFormat == "" {
		fromFormat, err = cli.DetectFormat(dataPath)
		if err != nil {
			return err
		}
	}

	// Parse data
	var parsed map[string][]instance.RawInstance
	var parseResult diag.Result

	switch fromFormat {
	case "json":
		parsed, parseResult, err = cli.LoadAndParseJSON(cmd.Context(), dataPath)
	case "csv":
		if typeName == "" && typeColumn == "" {
			return cli.Usagef("CSV data requires --type or --type-column flag")
		}
		parsed, parseResult, err = cli.LoadAndParseCSV(cmd.Context(), dataPath, typeName, typeColumn, s)
	default:
		return cli.Usagef("unsupported format %q", fromFormat)
	}

	if err != nil {
		return err
	}

	// Validate instances
	_, validateResult := cli.ValidateInstances(cmd.Context(), s, parsed)

	// Merge results — the schema load's residual warnings included, so one
	// invocation writes one result (and in JSON, one document).
	result := cli.MergeResults(pending, parseResult, validateResult)

	// Render
	renderDiagnostics(cmd, outputFormat, noColor, s, diagRootFor(s, moduleRoot, absSchemaPath), result)

	exitCode := cli.ExitForResult(result)
	if exitCode != cli.ExitOK {
		return &cli.ExitError{Code: exitCode}
	}
	return nil
}
