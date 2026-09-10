package main

import (
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

The write is atomic (tmp+fsync+rename) and preserves the file's mode.

This command uses the strict fast path and surfaces an
E_UPDATE_METADATA_BODY_OFFSET diagnostic if the input does not match a
Marshal-produced shape; recovery in that case is a fresh snapshot save
round-trip.`,
		Args: cobra.ExactArgs(1),
		RunE: withDiagnostics(runSnapshotUpdateMetadata),
	}

	cmd.Flags().StringArrayP("set", "s", nil, "key=value metadata pair to set (repeatable)")
	cmd.Flags().StringArray("unset", nil, "metadata key to remove (repeatable)")

	return cmd
}

func runSnapshotUpdateMetadata(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	setRaw, _ := cmd.Flags().GetStringArray("set")
	unsetKeys, _ := cmd.Flags().GetStringArray("unset")

	if len(setRaw) == 0 && len(unsetKeys) == 0 {
		return cli.Usagef("at least one --set or --unset is required")
	}

	setPairs, err := parseMetadata(setRaw)
	if err != nil {
		return cli.Usagef("%v", err)
	}

	// Applying --set then --unset on one key deleted it, exited 0, and reported
	// the count as if nothing had been asked. Documenting a precedence would
	// turn a silent contradiction into a documented one, so it is refused.
	for _, k := range unsetKeys {
		if _, both := setPairs[k]; both {
			return cli.Usagef("--set and --unset both name %q; pass one or the other", k)
		}
	}

	path := args[0]
	data, err := os.ReadFile(path)
	if err != nil {
		return cli.Runtimef("read %q: %v", path, err)
	}

	ctx := cmd.Context()

	header, headerRes := snapshot.HeaderOnly(ctx, data)
	sink.Add(headerRes)
	if headerRes.HasErrors() {
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
	sink.Add(updateRes)
	if updateRes.HasErrors() {
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	if err := cli.WriteFile(path, out); err != nil {
		return cli.Runtimef("write %q: %v", path, err)
	}

	sink.Flush()
	sink.Statusf("updated metadata on %s (%d keys)\n", path, len(newMeta))
	return nil
}
