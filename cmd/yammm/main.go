// Package main provides the entry point for the yammm CLI.
package main

import (
	"os"

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

// run executes the CLI and is the one place a failure is printed.
//
// Commands return their errors rather than writing them, so this is the single
// write site for every message the CLI emits on a failure path — which is what
// lets an in-process test drive run() and assert the text an operator sees.
func run() int {
	rootCmd := newRootCmd(buildversion.Resolve(version))
	err := rootCmd.Execute()
	cli.ReportError(rootCmd.ErrOrStderr(), err)
	return cli.ExitForError(err)
}
