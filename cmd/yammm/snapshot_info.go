package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/snapshot"
)

func newSnapshotInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <snapshot.ys>",
		Short: "Print summary information about a snapshot file",
		Long: "Read metadata and statistics from a .ys file without loading the schema. " +
			"Pass --header-only to read only the header, skipping the instance body and " +
			"integrity check — useful for dispatch-style workloads scanning many files.",
		Args: cobra.ExactArgs(1),
		RunE: runSnapshotInfo,
	}
	cmd.Flags().Bool("header-only", false,
		"read header only (skip instance body and integrity check; cost is O(header) ~ < 1 KiB per file)")
	return cmd
}

func runSnapshotInfo(cmd *cobra.Command, args []string) error {
	formatStr, _ := cmd.Flags().GetString("format")
	outputFormat, err := cli.ParseOutputFormat(formatStr)
	if err != nil {
		return err
	}

	if headerOnly, _ := cmd.Flags().GetBool("header-only"); headerOnly {
		f, err := os.Open(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: open file: %v\n", err)
			return &cli.ExitError{Code: cli.ExitRuntime}
		}
		defer f.Close()

		header, result := snapshot.HeaderOnlyRead(cmd.Context(), f)
		if result.HasErrors() {
			noColor, _ := cmd.Flags().GetBool("no-color")
			renderDiagnostics(cmd, outputFormat, noColor, nil, "", result)
			return &cli.ExitError{Code: cli.ExitValidation}
		}

		w := cmd.OutOrStdout()
		if outputFormat == cli.FormatJSON {
			enc, _ := json.MarshalIndent(header, "", "  ")
			fmt.Fprintln(w, string(enc)) //nolint:gosec // CLI output of parsed header metadata to stdout — no XSS sink.
			return nil
		}
		printHeaderInfo(w, header)
		return nil
	}

	data, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read file: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	info, result := snapshot.Info(cmd.Context(), data)
	if result.HasErrors() {
		noColor, _ := cmd.Flags().GetBool("no-color")
		renderDiagnostics(cmd, outputFormat, noColor, nil, "", result)
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	w := cmd.OutOrStdout()

	if outputFormat == cli.FormatJSON {
		enc, _ := json.MarshalIndent(info, "", "  ")
		fmt.Fprintln(w, string(enc))
		return nil
	}

	printSnapshotInfo(w, info)
	return nil
}

func printSnapshotInfo(w interface{ Write([]byte) (int, error) }, info *snapshot.SnapshotInfo) {
	fmt.Fprintf(w, "Snapshot: %s\n", info.SchemaName)
	fmt.Fprintf(w, "  Version:    %d\n", info.Version)

	if len(info.Features) > 0 {
		fmt.Fprintf(w, "  Features:   [%s]\n", strings.Join(info.Features, ", "))
	} else {
		fmt.Fprintf(w, "  Features:   none\n")
	}

	fmt.Fprintf(w, "  Schema:     %s (%s)\n", info.SchemaName, info.SchemaSource)
	fmt.Fprintf(w, "  Hash:       %s (algorithm v%d)\n", info.SchemaHash, info.SchemaHashAlgorithm)
	fmt.Fprintf(w, "  Integrity:  %s (%s)\n", info.IntegrityStatus, info.IntegrityHash)

	if info.CreatedAt != "" {
		fmt.Fprintf(w, "  Created:    %s\n", info.CreatedAt)
	} else {
		fmt.Fprintf(w, "  Created:    not set\n")
	}

	if len(info.Metadata) > 0 {
		pairs := make([]string, 0, len(info.Metadata))
		for k, v := range info.Metadata {
			pairs = append(pairs, k+"="+v)
		}
		fmt.Fprintf(w, "  Metadata:   %s\n", strings.Join(pairs, ", "))
	} else {
		fmt.Fprintf(w, "  Metadata:   none\n")
	}

	fmt.Fprintf(w, "\nTypes: %d\n", len(info.Types))
	for _, typeName := range info.Types {
		fmt.Fprintf(w, "  %s: %d instances\n", typeName, info.InstanceCounts[typeName])
	}

	fmt.Fprintf(w, "\nSummary:\n")
	fmt.Fprintf(w, "  Total instances: %d\n", info.TotalInstances)
	fmt.Fprintf(w, "  Total edges:     %d\n", info.TotalEdges)
	fmt.Fprintf(w, "  Duplicates:      %d\n", info.DuplicateCount)
	fmt.Fprintf(w, "  Unresolved:      %d\n", info.UnresolvedCount)
	fmt.Fprintf(w, "  File size:       %d bytes\n", info.FileSize)
}

// printHeaderInfo writes a human-readable header summary to w. gosec's
// taint tracker reports G705 (XSS via taint analysis) for the fmt.Fprintf
// calls because HeaderOnlyRead's io.Reader source flows into header field
// values; CLI stdout is not an XSS sink, so the warnings are false
// positives in this context.
//
//nolint:gosec // CLI output of parsed header metadata to stdout — no XSS sink.
func printHeaderInfo(w interface{ Write([]byte) (int, error) }, header *snapshot.HeaderInfo) {
	fmt.Fprintf(w, "Snapshot: %s\n", header.SchemaName)
	fmt.Fprintf(w, "  Version:    %d\n", header.Version)

	if len(header.Features) > 0 {
		fmt.Fprintf(w, "  Features:   [%s]\n", strings.Join(header.Features, ", "))
	} else {
		fmt.Fprintf(w, "  Features:   none\n")
	}

	fmt.Fprintf(w, "  Schema:     %s (%s)\n", header.SchemaName, header.SchemaSource)
	fmt.Fprintf(w, "  Hash:       %s (algorithm v%d)\n", header.SchemaHash, header.SchemaHashAlgorithm)
	fmt.Fprintf(w, "  Integrity:  %s (stored; not verified)\n", header.IntegrityHash)

	if header.CreatedAt != "" {
		fmt.Fprintf(w, "  Created:    %s\n", header.CreatedAt)
	} else {
		fmt.Fprintf(w, "  Created:    not set\n")
	}

	if len(header.Metadata) > 0 {
		pairs := make([]string, 0, len(header.Metadata))
		for k, v := range header.Metadata {
			pairs = append(pairs, k+"="+v)
		}
		fmt.Fprintf(w, "  Metadata:   %s\n", strings.Join(pairs, ", "))
	} else {
		fmt.Fprintf(w, "  Metadata:   none\n")
	}

	fmt.Fprintf(w, "\nTypes: %d\n", len(header.Types))
	for _, typeName := range header.Types {
		fmt.Fprintf(w, "  %s\n", typeName)
	}
}
