package main

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/hostpath"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

func newSnapshotSaveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "save <schema.yammm> <data-file> [data-file...]",
		Short: "Build a graph snapshot and save to a .ys file",
		Long: `Load schema, parse data files, validate, build graph, and save as a
persisted snapshot. Accepts multiple data files accumulated into a single graph.

Output is byte-level deterministic by default: no created_at timestamp is
written unless --timestamp stamps the current time, or --into carries the
merged file's own created_at forward.`,
		Args: cobra.MinimumNArgs(2),
		RunE: withDiagnostics(runSnapshotSave),
	}

	cmd.Flags().StringP("output", "o", "", "output path for the .ys file (required unless --into is given, which defaults it to the merged file)")
	cmd.Flags().StringArrayP("metadata", "m", nil, "key=value metadata pair (repeatable)")
	cmd.Flags().Bool("timestamp", false, "include created_at timestamp in header (breaks determinism)")
	cmd.Flags().Bool("indent", false, "produce indented output for human inspection")
	cmd.Flags().String("into", "", "existing .ys file to merge new data into")

	registerModuleRootFlag(cmd)
	return cmd
}

func runSnapshotSave(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	outputPath, _ := cmd.Flags().GetString("output")
	metadataRaw := stringArrayFlag(cmd, "metadata")
	timestamp, _ := cmd.Flags().GetBool("timestamp")
	indent, _ := cmd.Flags().GetBool("indent")
	intoPath, _ := cmd.Flags().GetString("into")

	// A path flag given empty names no file, so it is refused rather than read
	// as absent: -o "" with --into would otherwise overwrite the --into file.
	if cmd.Flags().Changed("output") {
		if err := cli.CheckOperand("output file", outputPath); err != nil {
			return err
		}
	}
	if cmd.Flags().Changed("into") {
		if err := cli.CheckOperand("snapshot file", intoPath); err != nil {
			return err
		}
	}
	if outputPath == "" && intoPath == "" {
		return cli.Usagef("either --output or --into is required")
	}
	if outputPath == "" {
		outputPath = intoPath // default: overwrite the --into file
	}

	// Parse metadata key=value pairs.
	metadata, err := parseMetadata(metadataRaw)
	if err != nil {
		return err
	}

	schemaPath := args[0]
	dataPaths := args[1:]
	if err := checkDataOperands(dataPaths...); err != nil {
		return err
	}

	absSchemaPath, err := schemaOperand(schemaPath)
	if err != nil {
		return err
	}
	// Load schema.
	moduleRoot, loadOpts, err := moduleRootOptions(cmd, sink)
	if err != nil {
		return err
	}
	s, loadErr := schema.Load(cmd.Context(), absSchemaPath, loadOpts...)
	if err := reportSchemaLoad(sink, s, moduleRoot, absSchemaPath, loadErr); err != nil {
		return err
	}

	// Load existing snapshot if --into is set. What the load reports reaches
	// the operator whether or not this command gets as far as writing anything.
	var g *graph.Graph
	var imported *snapshot.Header
	if intoPath != "" {
		if err := cli.CheckRewrite(outputPath, intoPath); err != nil {
			return cli.Runtime(err)
		}
		snap, header, loadErr := cli.LoadSnapshotFile(cmd.Context(), intoPath, s)
		if err := sink.AddSnapshotRead(intoPath, loadErr); err != nil {
			return err
		}
		g = graph.NewFromSnapshot(snap)
		imported = header
	}

	g, err = assembleGraph(cmd, sink, s, dataPaths, g)
	if err != nil {
		return err
	}
	if cli.Failed(sink.Result()) {
		return cli.Exit(cli.ExitForResult(sink.Result()))
	}

	// Create snapshot and marshal.
	snap := g.Snapshot()

	var opts []snapshot.Option
	switch {
	case timestamp:
		opts = append(opts, snapshot.WithCreatedAt(time.Now().UTC().Truncate(time.Second)))
	case imported != nil:
		opts = append(opts, snapshot.WithCreatedAt(imported.CreatedAt))
	}
	if indent {
		opts = append(opts, snapshot.WithIndent("\t"))
	}
	if merged := mergeMetadata(imported, metadata); len(merged) > 0 {
		opts = append(opts, snapshot.WithMetadata(merged))
	}

	// Marshal reports a failure as a diagnostic, so it reaches the sink and the
	// exit rule decides.
	data, marshalErr := snapshot.Marshal(cmd.Context(), snap, opts...)
	if err := sink.AddSnapshot(marshalErr); err != nil {
		return err
	}

	// One primitive whether or not --into names the same file, so the write is
	// atomic however the two flags spell the path.
	if err := cli.WriteFile(outputPath, data); err != nil {
		return cli.Runtimef("write output: %v", err)
	}

	// Raised only once the file exists: the code states that a snapshot was
	// written where a reader that discovers snapshots by extension will not find
	// it. A scan follows a link it finds, so either the host path, which reads
	// "out.ys/x/.." as out.ys, or the file its links reach is found.
	// The warning is placed in the file written, as a snapshot read's issues
	// are, so one run names one file one way in text and in JSON.
	if !foundByExtension(outputPath) {
		c := diag.NewCollector(diag.NoLimit)
		c.Collect(diag.NewIssue(diag.Warning, diag.W_SNAPSHOT_PATH_EXTENSION,
			fmt.Sprintf("output path %q reaches no file a scan for .ys files finds", outputPath)).Build())
		sink.Add(cli.PlaceIn(outputPath, c.Result()))
	}
	return nil
}

// mergeMetadata overlays flag pairs onto the imported header's. The header's
// annotations survive a merge that does not name them, and a flag wins on a
// key that appears in both.
func mergeMetadata(imported *snapshot.Header, flags map[string]string) map[string]string {
	merged := make(map[string]string, len(flags))
	if imported != nil {
		maps.Copy(merged, imported.Metadata)
	}
	maps.Copy(merged, flags)
	return merged
}

// stringArrayFlag returns the values a repeatable flag was given, each as
// typed. pflag's GetStringArray reads them back through their CSV form, which
// drops a lone empty value and turns a CR LF inside one into LF.
func stringArrayFlag(cmd *cobra.Command, name string) []string {
	return cmd.Flags().Lookup(name).Value.(pflag.SliceValue).GetSlice()
}

// parseMetadata parses key=value metadata pairs from --metadata flag values.
// Returns an empty (non-nil) map when raw is empty. A pair whose key or value
// is not valid UTF-8 is a usage error, judged before any read, though
// [snapshot.Marshal] would refuse it after the load.
func parseMetadata(raw []string) (map[string]string, error) {
	m := make(map[string]string, len(raw))
	for _, kv := range raw {
		k, v, ok := strings.Cut(kv, "=")
		switch {
		case !ok:
			return nil, cli.Usagef("invalid metadata %q: expected key=value format", kv)
		case k == "":
			return nil, cli.Usagef("invalid metadata %q: key must not be empty", kv)
		case !utf8.ValidString(k):
			return nil, cli.Usagef("invalid metadata %q: key is not valid UTF-8", kv)
		case !utf8.ValidString(v):
			return nil, cli.Usagef("invalid metadata %q: value is not valid UTF-8", kv)
		}
		m[k] = v
	}
	return m, nil
}

// foundByExtension reports whether a scan for .ys files can find what a write
// to path wrote: a name the write passes through, the path or a link it
// follows, ends in .ys and is an entry [snapshot.ScanDir] admits. A write that
// ends at one of the writer's own descriptor paths wrote the writer's stream,
// which a scan in another process cannot reach, so it is never found; another
// process's procfs entry is a link the kernel follows to the file that process
// holds, so the name it reads is judged too. A path it cannot resolve or stat
// is reported found, so no warning rests on a guess.
func foundByExtension(path string) bool {
	names, err := cli.WrittenNames(path)
	if err != nil {
		return true
	}
	last := names[len(names)-1]
	if _, own := hostpath.DescriptorNumber(last); own {
		return false
	}
	// A pipe's or a socket's entry reads as "pipe:" and a number, no file.
	if hostpath.Descriptor(last) {
		if target, err := os.Readlink(last); err == nil && filepath.IsAbs(target) {
			names = append(names, target)
		}
	}
	for _, name := range names {
		if strings.HasSuffix(name, ".ys") && scanAdmits(name) {
			return true
		}
	}
	return false
}

// scanAdmits reports whether [snapshot.ScanDir] admits the entry name names: a
// regular file, or a link whose target is one or cannot be stat'd, in a
// directory the process can list. A name it cannot stat is admitted, as
// foundByExtension reports it found; a directory that refuses a listing with
// EACCES is no guess, since a scan of it fails.
func scanAdmits(name string) bool {
	if dir, err := os.Open(hostpath.Parent(name)); errors.Is(err, fs.ErrPermission) {
		return false
	} else if err == nil {
		dir.Close() //nolint:gosec // opened only to learn whether it can be listed
	}
	info, err := os.Lstat(name)
	switch {
	case err != nil || info.Mode().IsRegular():
		return true
	case info.Mode()&fs.ModeSymlink == 0:
		return false
	}
	target, err := os.Stat(name)
	return err != nil || target.Mode().IsRegular()
}
