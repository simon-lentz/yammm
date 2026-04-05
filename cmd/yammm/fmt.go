package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/format"
)

func newFmtCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fmt <schema.yammm>",
		Short: "Format a schema file",
		Long:  "Format a .yammm schema file using canonical formatting (like gofmt).",
		Args:  cobra.ExactArgs(1),
		RunE:  runFmt,
	}

	cmd.Flags().BoolP("write", "w", false, "write result to source file instead of stdout")

	return cmd
}

func runFmt(cmd *cobra.Command, args []string) error {
	write, _ := cmd.Flags().GetBool("write")
	path := args[0]

	content, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	formatted, err := format.TokenStream(string(content))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	if write {
		if formatted == string(content) {
			return nil // already formatted
		}

		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: stat %s: %v\n", path, err)
			return &cli.ExitError{Code: cli.ExitUsage}
		}

		if err := os.WriteFile(path, []byte(formatted), info.Mode().Perm()); err != nil {
			fmt.Fprintf(os.Stderr, "error: write %s: %v\n", path, err)
			return &cli.ExitError{Code: cli.ExitValidation}
		}
		return nil
	}

	fmt.Print(formatted)
	return nil
}
