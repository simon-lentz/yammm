// Package main provides the entry point for the yammm CLI.
package main

import (
	"errors"
	"os"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	rootCmd := newRootCmd(version)
	if err := rootCmd.Execute(); err != nil {
		if exitErr, ok := errors.AsType[*cli.ExitError](err); ok {
			return exitErr.Code
		}
		return cli.ExitUsage
	}
	return cli.ExitOK
}
