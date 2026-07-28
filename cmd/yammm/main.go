// Package main provides the entry point for the yammm CLI.
package main

import (
	"errors"
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

func run() int {
	rootCmd := newRootCmd(buildversion.Resolve(version))
	if err := rootCmd.Execute(); err != nil {
		if exitErr, ok := errors.AsType[*cli.ExitError](err); ok {
			return exitErr.Code
		}
		return cli.ExitUsage
	}
	return cli.ExitOK
}
