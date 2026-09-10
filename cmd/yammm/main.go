// Package main provides the entry point for the yammm CLI.
package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/internal/buildversion"
)

// version is the ldflags-injected release version; [buildversion.Resolve]
// falls back to the build-info module version for `go install pkg@tag` builds,
// which never receive ldflags.
var version = "dev"

func main() {
	os.Exit(run())
}

// run executes the CLI and is the one write site for a failure a command
// returned rather than diagnosed. Under --format json a command's sink has
// already folded its failure into the document, so a message reaching here is
// one cobra raised before any command ran, and it becomes a document.
func run() int {
	rootCmd := newRootCmd(buildversion.Resolve(version))
	err := rootCmd.Execute()
	stderr := rootCmd.ErrOrStderr()
	if failure, ok := cli.FailureResult(err); ok && jsonRequested(os.Args[1:]) {
		_ = cli.RenderResult(stderr, cli.NewRenderer(cli.FormatJSON, false, true, nil, ""), cli.FormatJSON, failure)
	} else {
		cli.ReportError(stderr, err)
	}
	return cli.ExitForError(err)
}

// jsonRequested reports whether args ask for --format json. It parses that
// flag alone, because cobra parses no flag at all before it fails to find a
// command, and an unknown command is still owed a document.
func jsonRequested(args []string) bool {
	probe := &cobra.Command{FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true}}
	format := probe.Flags().String("format", string(cli.FormatText), "")
	_ = probe.ParseFlags(args)
	return *format == string(cli.FormatJSON)
}
