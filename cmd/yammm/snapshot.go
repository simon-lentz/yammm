package main

import "github.com/spf13/cobra"

func newSnapshotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Snapshot persistence commands",
		Long:  "Build, inspect, and validate persisted graph snapshots (.ys files).",
	}

	cmd.AddCommand(
		newSnapshotSaveCmd(),
		newSnapshotInfoCmd(),
		newSnapshotVerifyCmd(),
		newSnapshotUpdateMetadataCmd(),
	)

	return cmd
}
