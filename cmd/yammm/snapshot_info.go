package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
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
		RunE: withDiagnostics(runSnapshotInfo),
	}
	cmd.Flags().Bool("header-only", false,
		"read header only (skip instance body and integrity check; cost is O(header) ~ < 1 KiB per file)")
	cmd.Flags().String("dir", "",
		"scan every .ys file in the given directory (header-only; mutually exclusive with the positional file argument)")
	return cmd
}

func runSnapshotInfo(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	if dirPath, _ := cmd.Flags().GetString("dir"); dirPath != "" {
		if len(args) > 0 {
			return cli.Usagef("--dir is mutually exclusive with a positional file argument")
		}
		return runSnapshotInfoDir(cmd, sink, dirPath)
	}
	if len(args) == 0 {
		return cli.Usagef("either --dir or a positional .ys file is required")
	}

	if headerOnly, _ := cmd.Flags().GetBool("header-only"); headerOnly {
		f, err := os.Open(args[0])
		if err != nil {
			return cli.Runtimef("open file: %v", err)
		}
		defer f.Close()

		header, result := snapshot.HeaderOnlyRead(cmd.Context(), f)
		sink.Add(result)
		if result.HasErrors() {
			return &cli.ExitError{Code: cli.ExitValidation}
		}

		sink.Render()
		w := cmd.OutOrStdout()
		if sink.Format() == cli.FormatJSON {
			enc, err := json.MarshalIndent(newHeaderInfoDTO(header, statSize(f, header.FileSize)), "", "  ")
			if err != nil {
				return cli.Runtimef("encode JSON: %v", err)
			}
			fmt.Fprintln(w, string(enc))
			return nil
		}
		printHeaderInfo(w, header)
		return nil
	}

	data, err := os.ReadFile(args[0])
	if err != nil {
		return cli.Runtimef("read file: %v", err)
	}

	info, result := snapshot.Info(cmd.Context(), data)
	sink.Add(result)
	if result.HasErrors() {
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	sink.Render()
	w := cmd.OutOrStdout()

	if sink.Format() == cli.FormatJSON {
		enc, err := json.MarshalIndent(newSnapshotInfoDTO(info), "", "  ")
		if err != nil {
			return cli.Runtimef("encode JSON: %v", err)
		}
		fmt.Fprintln(w, string(enc))
		return nil
	}

	printSnapshotInfo(w, info)
	return nil
}

// statSize reports the open file's size, keeping fallback when the handle
// cannot be stat'd. HeaderOnlyRead answers zero because a size is not knowable
// from an io.Reader, and this command holds the handle it opened.
func statSize(f *os.File, fallback int64) int64 {
	stat, err := f.Stat()
	if err != nil {
		return fallback
	}
	return stat.Size()
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
		fmt.Fprintf(w, "  Metadata:   %s\n", formatMetadata(info.Metadata))
	} else {
		fmt.Fprintf(w, "  Metadata:   none\n")
	}

	printAttestation(w, info.Attestation)

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

// printHeaderInfo writes a human-readable header summary to w.
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
		fmt.Fprintf(w, "  Metadata:   %s\n", formatMetadata(header.Metadata))
	} else {
		fmt.Fprintf(w, "  Metadata:   none\n")
	}

	printAttestation(w, header.Attestation)

	fmt.Fprintf(w, "\nTypes: %d\n", len(header.Types))
	for _, typeName := range header.Types {
		fmt.Fprintf(w, "  %s\n", typeName)
	}
}

// printAttestation renders the header's validity claim, or names its
// absence: a pre-v0.15.0 writer states no claim at all.
func printAttestation(w interface{ Write([]byte) (int, error) }, att *graph.Attestation) {
	if att == nil {
		fmt.Fprintf(w, "  Attested:   not stated (pre-v0.15.0 writer)\n")
		return
	}
	fmt.Fprintf(w, "  Attested:   values=%t associations=%t\n", att.Values, att.Associations)
}

// modTimeLayout renders a modification time at fixed width, so the key sorts
// as text in the order the times sort. RFC 3339 Nano trims trailing zeros,
// which puts a whole-second stamp above one recorded microseconds later.
const modTimeLayout = "2006-01-02T15:04:05.000000000Z07:00"

// formatMetadata renders annotation pairs in key order. Map iteration is
// randomized, so an unsorted walk reports one file two ways on consecutive
// runs and no diff of two reports means anything.
func formatMetadata(metadata map[string]string) string {
	pairs := make([]string, 0, len(metadata))
	for _, k := range slices.Sorted(maps.Keys(metadata)) {
		pairs = append(pairs, k+"="+metadata[k])
	}
	return strings.Join(pairs, ", ")
}

// The CLI owns the JSON shape of every `snapshot info` mode: the library's
// HeaderInfo and SnapshotInfo carry no json tags, so marshalling them directly
// published Go field names as a wire contract. These types are the only thing
// marshalled, and they keep one snake_case convention across all three modes.
type attestationDTO struct {
	Values       bool `json:"values"`
	Associations bool `json:"associations"`
}

type headerInfoDTO struct {
	Version             int                `json:"version"`
	Features            []string           `json:"features"`
	SchemaName          string             `json:"schema_name"`
	SchemaSource        string             `json:"schema_source"`
	SchemaHash          string             `json:"schema_hash"`
	SchemaHashAlgorithm int                `json:"schema_hash_algorithm"`
	IntegrityHash       string             `json:"integrity_hash"`
	CreatedAt           string             `json:"created_at"`
	Metadata            map[string]string  `json:"metadata"`
	Types               []snapshot.TypeRef `json:"types"`
	Attestation         *attestationDTO    `json:"attestation"`
	FileSize            int64              `json:"file_size"`
}

type snapshotInfoDTO struct {
	Version             int                      `json:"version"`
	Features            []string                 `json:"features"`
	SchemaName          string                   `json:"schema_name"`
	SchemaSource        string                   `json:"schema_source"`
	SchemaHash          string                   `json:"schema_hash"`
	SchemaHashAlgorithm int                      `json:"schema_hash_algorithm"`
	IntegrityHash       string                   `json:"integrity_hash"`
	CreatedAt           string                   `json:"created_at"`
	Metadata            map[string]string        `json:"metadata"`
	Types               []snapshot.TypeRef       `json:"types"`
	InstanceCounts      map[snapshot.TypeRef]int `json:"instance_counts"`
	TotalInstances      int                      `json:"total_instances"`
	TotalEdges          int                      `json:"total_edges"`
	DuplicateCount      int                      `json:"duplicate_count"`
	UnresolvedCount     int                      `json:"unresolved_count"`
	Attestation         *attestationDTO          `json:"attestation"`
	FileSize            int64                    `json:"file_size"`
	IntegrityStatus     string                   `json:"integrity_status"`
}

func newAttestationDTO(att *graph.Attestation) *attestationDTO {
	if att == nil {
		return nil
	}
	return &attestationDTO{Values: att.Values, Associations: att.Associations}
}

// newHeaderInfoDTO takes the size explicitly: the three producers of a
// HeaderInfo know it to different degrees, and only the caller knows which
// one it read.
func newHeaderInfoDTO(header *snapshot.HeaderInfo, fileSize int64) *headerInfoDTO {
	if header == nil {
		return nil
	}
	return &headerInfoDTO{
		Version:             header.Version,
		Features:            header.Features,
		SchemaName:          header.SchemaName,
		SchemaSource:        header.SchemaSource,
		SchemaHash:          header.SchemaHash,
		SchemaHashAlgorithm: header.SchemaHashAlgorithm,
		IntegrityHash:       header.IntegrityHash,
		CreatedAt:           header.CreatedAt,
		Metadata:            header.Metadata,
		Types:               header.Types,
		Attestation:         newAttestationDTO(header.Attestation),
		FileSize:            fileSize,
	}
}

func newSnapshotInfoDTO(info *snapshot.SnapshotInfo) *snapshotInfoDTO {
	if info == nil {
		return nil
	}
	return &snapshotInfoDTO{
		Version:             info.Version,
		Features:            info.Features,
		SchemaName:          info.SchemaName,
		SchemaSource:        info.SchemaSource,
		SchemaHash:          info.SchemaHash,
		SchemaHashAlgorithm: info.SchemaHashAlgorithm,
		IntegrityHash:       info.IntegrityHash,
		CreatedAt:           info.CreatedAt,
		Metadata:            info.Metadata,
		Types:               info.Types,
		InstanceCounts:      info.InstanceCounts,
		TotalInstances:      info.TotalInstances,
		TotalEdges:          info.TotalEdges,
		DuplicateCount:      info.DuplicateCount,
		UnresolvedCount:     info.UnresolvedCount,
		Attestation:         newAttestationDTO(info.Attestation),
		FileSize:            info.FileSize,
		IntegrityStatus:     info.IntegrityStatus,
	}
}

// dirEntryDTO mirrors ScanEntry for JSON output. The shape exposes the
// per-file basename, header (nil on failure), and a compact list of
// diagnostic codes + messages for any issues on the entry's Result.
type dirEntryDTO struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// FileSize repeats the header's when the header parsed, and carries the
	// size alone when it did not — a corrupt file still occupies disk.
	FileSize int64 `json:"file_size"`
	// ModTime is RFC 3339 in UTC at fixed width, empty when the file could not
	// be stat'd — a zero time would sort first and corrupt orderings.
	ModTime string         `json:"mod_time"`
	Header  *headerInfoDTO `json:"header"`
	// Issues is always present, never absent: header is an explicit null, so
	// one entry must not answer "nothing to report" two ways.
	Issues []dirIssueDTO `json:"issues"`
}

type dirIssueDTO struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

func runSnapshotInfoDir(cmd *cobra.Command, sink *cli.DiagnosticSink, dirPath string) error {
	entries, result := snapshot.ScanDirSlice(cmd.Context(), dirPath)
	sink.Add(result)
	if result.HasErrors() {
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	sink.Render()
	w := cmd.OutOrStdout()
	if sink.Format() == cli.FormatJSON {
		dtos := make([]dirEntryDTO, 0, len(entries))
		for _, entry := range entries {
			dtos = append(dtos, scanEntryToDTO(entry))
		}
		enc, err := json.MarshalIndent(dtos, "", "  ")
		if err != nil {
			return cli.Runtimef("encode JSON: %v", err)
		}
		fmt.Fprintln(w, string(enc))
		return dirEntriesExit(entries)
	}

	printDirEntries(w, dirPath, entries)
	return dirEntriesExit(entries)
}

// dirEntriesExit reports the failure a scanned directory earns.
//
// ScanDirSlice's own result covers reading the directory, not reading the files
// in it, so gating on that alone exits 0 over a directory whose every entry is
// malformed — while the same file named on its own exits 1. The entry's own
// result decides, so an I/O failure and a malformed file keep the codes they
// have everywhere else.
func dirEntriesExit(entries []snapshot.ScanEntry) error {
	worst := cli.ExitOK
	for _, entry := range entries {
		if code := cli.ExitForResult(entry.Result); code != cli.ExitOK {
			if worst == cli.ExitOK || code == cli.ExitRuntime {
				worst = code
			}
		}
	}
	if worst == cli.ExitOK {
		return nil
	}
	return &cli.ExitError{Code: worst}
}

func scanEntryToDTO(entry snapshot.ScanEntry) dirEntryDTO {
	// ScanDir already stat'd the entry, so the header's own size is the one to
	// carry; Header is nil by contract on an error-severity result.
	var headerSize int64
	if entry.Header != nil {
		headerSize = entry.Header.FileSize
	}
	dto := dirEntryDTO{
		Name:     entry.Name,
		Path:     entry.Path,
		FileSize: entry.FileSize,
		Header:   newHeaderInfoDTO(entry.Header, headerSize),
		Issues:   []dirIssueDTO{},
	}
	if !entry.ModTime.IsZero() {
		dto.ModTime = entry.ModTime.UTC().Format(modTimeLayout)
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
//
// An entry carrying warnings alone reads "warn" rather than "ok": the header
// parsed, so the row prints the same fields, but a scan that calls a file whose
// schema identity could not be checked healthy is the one place a dispatch
// workload would never look again.
func printDirEntries(w interface{ Write([]byte) (int, error) }, dirPath string, entries []snapshot.ScanEntry) {
	fmt.Fprintf(w, "Directory: %s\n", dirPath)
	if len(entries) == 0 {
		fmt.Fprintf(w, "  (no .ys files)\n")
		return
	}
	var okCount, warnCount, malformedCount, ioCount, otherCount int
	for _, entry := range entries {
		if !entry.Result.HasErrors() {
			status := "ok"
			if entry.Result.HasWarnings() {
				status, warnCount = "warn", warnCount+1
			} else {
				okCount++
			}
			created := entry.Header.CreatedAt
			if created == "" {
				created = "not set"
			}
			fmt.Fprintf(w, "  %-40s  %-12sschema=%s  v%d  created=%s\n",
				entry.Name, status, entry.Header.SchemaName, entry.Header.Version, created)
			continue
		}
		// Surface the first error code and message.
		var code diag.Code
		var msg string
		for iss := range entry.Result.Errors() {
			code = iss.Code()
			msg = iss.Message()
			break
		}
		switch code {
		case diag.E_SNAPSHOT_MALFORMED:
			malformedCount++
			fmt.Fprintf(w, "  %-40s  %-12s%s: %s\n", entry.Name, "malformed", code, msg)
		case diag.E_SNAPSHOT_IO:
			ioCount++
			fmt.Fprintf(w, "  %-40s  %-12s%s: %s\n", entry.Name, "io-error", code, msg)
		default:
			otherCount++
			fmt.Fprintf(w, "  %-40s  %-12s%s: %s\n", entry.Name, "error", code, msg)
		}
	}
	fmt.Fprintf(w, "\nSummary: %d ok", okCount)
	if warnCount > 0 {
		fmt.Fprintf(w, ", %d warn", warnCount)
	}
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
