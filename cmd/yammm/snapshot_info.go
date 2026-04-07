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
		Long:  "Read metadata and statistics from a .ys file without loading the schema.",
		Args:  cobra.ExactArgs(1),
		RunE:  runSnapshotInfo,
	}
	return cmd
}

func runSnapshotInfo(cmd *cobra.Command, args []string) error {
	formatStr, _ := cmd.Flags().GetString("format")
	outputFormat, err := cli.ParseOutputFormat(formatStr)
	if err != nil {
		return err
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
