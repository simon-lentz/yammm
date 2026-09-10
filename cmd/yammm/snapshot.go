package main

import "github.com/spf13/cobra"

func newSnapshotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Snapshot persistence commands",
		Long:  "Build, inspect, and validate persisted graph snapshots (.ys files).",
		// See requireSubcommand: a parent that runs is a parent that exits 0
		// having done nothing.
		RunE: requireSubcommand,
		// A mistyped subcommand is reported as unknown, by name.
		Args: cobra.NoArgs,
	}

	cmd.AddCommand(
		newSnapshotSaveCmd(),
		newSnapshotInfoCmd(),
		newSnapshotVerifyCmd(),
		newSnapshotUpdateMetadataCmd(),
	)

	return cmd
}
