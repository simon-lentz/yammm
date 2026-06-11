package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/adapter/gogen"
	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/schema"
)

func newGenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen <schema.yammm>",
		Short: "Generate source artifacts from a schema",
		Long: `Generate language artifacts from a yammm schema.

Currently supports Go (--to go): a single file of typed structs for every schema
type (including imported types), named Enum/DataType types, EDGE_ association
structs, a Graph aggregate, and an embedded SerializedModel. Output is stdlib-only.
Use --initialisms to upper-case extra acronyms (e.g. GUID,JWT) in generated
identifiers; they merge with the default golint acronym set.
Use --module-root to resolve module-style imports against a root directory other
than the schema's own (e.g. a repository root); embedded SerializedModel keys are
relative to that root.`,
		Args: cobra.ExactArgs(1),
		RunE: runGen,
	}
	cmd.Flags().String("to", "", "target: go (required)")
	cmd.Flags().String("package", "", "generated Go package name (default: derived from schema name)")
	cmd.Flags().String("output", "", "output file path (default: stdout)")
	cmd.Flags().StringSlice("initialisms", nil, "extra acronyms to upper-case in generated Go names, e.g. GUID,JWT")
	cmd.Flags().String("module-root", "", "root directory for module-style imports (default: the schema's directory)")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func runGen(cmd *cobra.Command, args []string) error {
	formatStr, _ := cmd.Flags().GetString("format")
	noColor, _ := cmd.Flags().GetBool("no-color")
	toFormat, _ := cmd.Flags().GetString("to")
	pkgName, _ := cmd.Flags().GetString("package")
	outputPath, _ := cmd.Flags().GetString("output")
	initialisms, _ := cmd.Flags().GetStringSlice("initialisms")
	moduleRoot, _ := cmd.Flags().GetString("module-root")

	outputFormat, err := cli.ParseOutputFormat(formatStr)
	if err != nil {
		return err
	}

	if strings.ToLower(toFormat) != "go" {
		fmt.Fprintf(os.Stderr, "error: unsupported gen target %q: must be go\n", toFormat)
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	absSchemaPath, err := filepath.Abs(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: resolve path %q: %v\n", args[0], err)
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	var loadOpts []schema.LoadOption
	diagRoot := filepath.Dir(absSchemaPath)
	if moduleRoot != "" {
		absRoot, err := filepath.Abs(moduleRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: resolve module root %q: %v\n", moduleRoot, err)
			return &cli.ExitError{Code: cli.ExitUsage}
		}
		loadOpts = append(loadOpts, schema.WithModuleRoot(absRoot))
		diagRoot = absRoot
	}
	s, schemaResult := schema.Load(cmd.Context(), absSchemaPath, loadOpts...)
	if schemaResult.HasErrors() {
		renderDiagnostics(cmd, outputFormat, noColor, s, diagRoot, schemaResult)
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	var opts []gogen.Option
	if pkgName != "" {
		opts = append(opts, gogen.WithPackageName(pkgName))
	}
	if len(initialisms) > 0 {
		opts = append(opts, gogen.WithInitialisms(initialisms...))
	}
	data, err := gogen.Marshal(s, opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: generate go: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	if err := cli.WriteTo(data, outputPath, cmd.OutOrStdout()); err != nil {
		fmt.Fprintf(os.Stderr, "error: write output: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}
	return nil
}
