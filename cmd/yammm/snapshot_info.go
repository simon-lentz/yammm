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
		Use:   "info [<snapshot.ys>]",
		Short: "Print summary information about a snapshot file or directory of snapshots",
		Long: "Read metadata and statistics from a .ys file without loading the schema. " +
			"Pass --header-only to read only the header, skipping the instance body and " +
			"integrity check — useful for dispatch-style workloads scanning many files. " +
			"Pass --dir <path> to iterate every .ys file in a directory (header-only; " +
			"mutually exclusive with the positional file argument).",
		Args: cobra.MaximumNArgs(1),
		RunE: runSnapshotInfo,
	}
	cmd.Flags().Bool("header-only", false,
		"read header only (skip instance body and integrity check; cost is O(header) ~ < 1 KiB per file)")
	cmd.Flags().String("dir", "",
		"scan every .ys file in the given directory (header-only; mutually exclusive with the positional file argument)")
	return cmd
}

func runSnapshotInfo(cmd *cobra.Command, args []string) error {
	formatStr, _ := cmd.Flags().GetString("format")
	outputFormat, err := cli.ParseOutputFormat(formatStr)
	if err != nil {
		return err
	}

	if dirPath, _ := cmd.Flags().GetString("dir"); dirPath != "" {
		if len(args) > 0 {
			fmt.Fprintln(os.Stderr, "error: --dir is mutually exclusive with a positional file argument")
			return &cli.ExitError{Code: cli.ExitUsage}
		}
		return runSnapshotInfoDir(cmd, dirPath, outputFormat)
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: either --dir or a positional .ys file is required")
		return &cli.ExitError{Code: cli.ExitUsage}
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

// dirEntryDTO mirrors ScanEntry for JSON output. The shape exposes the
// per-file basename, header (nil on failure), and a compact list of
// diagnostic codes + messages for any issues on the entry's Result.
type dirEntryDTO struct {
	Name   string               `json:"name"`
	Path   string               `json:"path"`
	Header *snapshot.HeaderInfo `json:"header"`
	Issues []dirIssueDTO        `json:"issues,omitempty"`
}

type dirIssueDTO struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

func runSnapshotInfoDir(cmd *cobra.Command, dirPath string, outputFormat cli.OutputFormat) error {
	entries, result := snapshot.ScanDirSlice(cmd.Context(), dirPath)
	if result.HasErrors() {
		noColor, _ := cmd.Flags().GetBool("no-color")
		renderDiagnostics(cmd, outputFormat, noColor, nil, "", result)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	w := cmd.OutOrStdout()
	if outputFormat == cli.FormatJSON {
		dtos := make([]dirEntryDTO, 0, len(entries))
		for _, entry := range entries {
			dtos = append(dtos, scanEntryToDTO(entry))
		}
		enc, _ := json.MarshalIndent(dtos, "", "  ")
		fmt.Fprintln(w, string(enc))
		return nil
	}

	printDirEntries(w, dirPath, entries)
	return nil
}

func scanEntryToDTO(entry snapshot.ScanEntry) dirEntryDTO {
	dto := dirEntryDTO{
		Name:   entry.Name,
		Path:   entry.Path,
		Header: entry.Header,
	}
	for iss := range entry.Result.Issues() {
		dto.Issues = append(dto.Issues, dirIssueDTO{
			Severity: iss.Severity().String(),
			Code:     iss.Code().String(),
			Message:  iss.Message(),
		})
	}
	return dto
}

// printDirEntries writes a tabular directory-scan summary.
func printDirEntries(w interface{ Write([]byte) (int, error) }, dirPath string, entries []snapshot.ScanEntry) {
	fmt.Fprintf(w, "Directory: %s\n", dirPath)
	if len(entries) == 0 {
		fmt.Fprintf(w, "  (no .ys files)\n")
		return
	}
	var okCount, malformedCount, ioCount, otherCount int
	for _, entry := range entries {
		if !entry.Result.HasErrors() {
			okCount++
			created := entry.Header.CreatedAt
			if created == "" {
				created = "not set"
			}
			fmt.Fprintf(w, "  %-40s  ok          schema=%s  v%d  created=%s\n",
				entry.Name, entry.Header.SchemaName, entry.Header.Version, created)
			continue
		}
		// Surface the first error code and message.
		var code, msg string
		for iss := range entry.Result.Errors() {
			code = iss.Code().String()
			msg = iss.Message()
			break
		}
		switch code {
		case "E_SNAPSHOT_MALFORMED":
			malformedCount++
			fmt.Fprintf(w, "  %-40s  malformed   %s: %s\n", entry.Name, code, msg)
		case "E_SNAPSHOT_IO":
			ioCount++
			fmt.Fprintf(w, "  %-40s  io-error    %s: %s\n", entry.Name, code, msg)
		default:
			otherCount++
			fmt.Fprintf(w, "  %-40s  error       %s: %s\n", entry.Name, code, msg)
		}
	}
	fmt.Fprintf(w, "\nSummary: %d ok", okCount)
	if malformedCount > 0 {
		fmt.Fprintf(w, ", %d malformed", malformedCount)
	}
	if ioCount > 0 {
		fmt.Fprintf(w, ", %d I/O-error", ioCount)
	}
	if otherCount > 0 {
		fmt.Fprintf(w, ", %d other", otherCount)
	}
	fmt.Fprintf(w, ", %d scanned\n", len(entries))
}
