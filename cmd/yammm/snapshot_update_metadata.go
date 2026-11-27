package main

import (
	"maps"
	"os"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/internal/snapread"
	"github.com/simon-lentz/yammm/snapshot"
)

func newSnapshotUpdateMetadataCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-metadata <snapshot.ys>",
		Short: "Rewrite metadata on an existing .ys file",
		Long: `Update the metadata of a .ys file in place with snapshot.UpdateMetadata,
which rewrites the header and keeps every other byte of the file. It keeps
created_at unchanged; to change it, run snapshot save with --timestamp.

Use --set key=value to set or override a metadata key (repeatable).
Use --unset key to remove a metadata key (repeatable).
At least one --set or --unset is required.

A document whose integrity hash does not verify, or that a reader refuses
for its shape, is refused and the file is not written. The command does not
check the instances of the body: the verified hash seals the body a writer
wrote, and snapshot info checks it whole.

A regular file is replaced atomically (tmp+fsync+rename) and keeps its
permission bits. A FIFO, a device or a descriptor path such as /dev/fd/3 is
refused before it is read: a write through it would append a second document
after the first.`,
		Args: cobra.ExactArgs(1),
		RunE: withDiagnostics(runSnapshotUpdateMetadata),
	}

	cmd.Flags().StringArrayP("set", "s", nil, "key=value metadata pair to set (repeatable)")
	cmd.Flags().StringArray("unset", nil, "metadata key to remove (repeatable)")

	return cmd
}

func runSnapshotUpdateMetadata(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	setRaw := stringArrayFlag(cmd, "set")
	unsetKeys := stringArrayFlag(cmd, "unset")

	if len(setRaw) == 0 && len(unsetKeys) == 0 {
		return cli.Usagef("at least one --set or --unset is required")
	}

	setPairs, err := parseMetadata(setRaw)
	if err != nil {
		return err
	}

	for _, k := range unsetKeys {
		if !utf8.ValidString(k) {
			return cli.Usagef("invalid --unset %q: key is not valid UTF-8", k)
		}
		// Applying --set then --unset on one key deleted it, exited 0, and
		// reported the count as if nothing had been asked. Documenting a
		// precedence would turn a silent contradiction into a documented one,
		// so it is refused.
		if _, both := setPairs[k]; both {
			return cli.Usagef("--set and --unset both name %q; pass one or the other", k)
		}
	}

	path := args[0]
	if err := cli.CheckOperand("snapshot file", path); err != nil {
		return err
	}
	// What the read draws names the file relative to where the command was run.
	if wd, err := os.Getwd(); err == nil {
		sink.SetSource(nil, wd)
	}
	if err := cli.CheckRewrite(path, path); err != nil {
		return cli.Runtime(err)
	}
	data, err := cli.ReadFile(path)
	if err != nil {
		return cli.Runtimef("read %q: %v", path, err)
	}

	ctx := cmd.Context()

	// A header read accepts an unknown hash algorithm, which UpdateMetadata
	// then refuses. The read takes no size cap, since UpdateMetadata updates a
	// header of any size.
	read, headerErr := snapread.HeaderOf(ctx, data)
	if headerErr != nil {
		return sink.AddSnapshotRead(path, headerErr)
	}
	header := read.(*snapshot.Header)

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

	// UpdateMetadata refuses a document whose hash does not verify, so a
	// tampered file is never sealed under a fresh hash.
	out, updateErr := snapshot.UpdateMetadata(ctx, data, newMeta)
	if err := sink.AddSnapshotRead(path, updateErr); err != nil {
		return err
	}

	if err := cli.WriteFile(path, out); err != nil {
		return cli.Runtimef("write %q: %v", path, err)
	}
	return nil
}
