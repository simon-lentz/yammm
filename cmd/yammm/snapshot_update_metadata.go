package main

import (
	"fmt"
	"maps"
	"os"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/snapshot"
)

func newSnapshotUpdateMetadataCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-metadata <snapshot.ys>",
		Short: "Rewrite metadata on an existing .ys file",
		Long: `Update the metadata header of a .ys file in-place using the
body-byte-reuse fast path (snapshot.UpdateMetadata). Preserves created_at
unchanged; operators requiring a different created_at should re-run
snapshot save with --timestamp.

Use --set key=value to set or override a metadata key (repeatable).
Use --unset key to remove a metadata key (repeatable).
At least one --set or --unset is required.

The write is atomic via snapshot.WriteFile (tmp+fsync+rename).

This command uses the strict fast path and surfaces an
E_UPDATE_METADATA_BODY_OFFSET diagnostic if the input does not match a
Marshal-produced shape; recovery in that case is a fresh snapshot save
round-trip.`,
		Args: cobra.ExactArgs(1),
		RunE: runSnapshotUpdateMetadata,
	}

	cmd.Flags().StringArrayP("set", "s", nil, "key=value metadata pair to set (repeatable)")
	cmd.Flags().StringArray("unset", nil, "metadata key to remove (repeatable)")

	return cmd
}

func runSnapshotUpdateMetadata(cmd *cobra.Command, args []string) error {
	formatStr, _ := cmd.Flags().GetString("format")
	noColor, _ := cmd.Flags().GetBool("no-color")
	setRaw, _ := cmd.Flags().GetStringArray("set")
	unsetKeys, _ := cmd.Flags().GetStringArray("unset")

	outputFormat, err := cli.ParseOutputFormat(formatStr)
	if err != nil {
		return err
	}

	if len(setRaw) == 0 && len(unsetKeys) == 0 {
		fmt.Fprintln(os.Stderr, "error: at least one --set or --unset is required")
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	setPairs, err := parseMetadata(setRaw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	path := args[0]
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read %q: %v\n", path, err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	ctx := cmd.Context()

	header, headerRes := snapshot.HeaderOnly(ctx, data)
	if headerRes.HasErrors() {
		renderDiagnostics(cmd, outputFormat, noColor, nil, "", headerRes)
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Build the new metadata map from the existing header.Metadata, apply
	// --set overrides, then remove --unset keys.
	newMeta := maps.Clone(header.Metadata)
	if newMeta == nil {
		newMeta = make(map[string]string, len(setPairs))
	}
	maps.Copy(newMeta, setPairs)
	for _, k := range unsetKeys {
		delete(newMeta, k)
	}

	// No UpdateOption needed: UpdateMetadata preserves the existing
	// CreatedAt byte-for-byte by default, which is the right behavior
	// for an in-place metadata rewrite. Operators who want to change
	// CreatedAt re-save with snapshot save --timestamp.
	out, updateRes := snapshot.UpdateMetadata(ctx, data, newMeta)
	if updateRes.HasErrors() {
		renderDiagnostics(cmd, outputFormat, noColor, nil, "", updateRes)
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	if err := snapshot.WriteFile(path, out); err != nil {
		fmt.Fprintf(os.Stderr, "error: write %q: %v\n", path, err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "updated metadata on %s (%d keys)\n", path, len(newMeta))
	return nil
}
