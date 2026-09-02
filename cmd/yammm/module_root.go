package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/schema"
)

// Every command that loads a schema takes --module-root with one meaning, on
// the neo4j_flags.go precedent: a flag one command has and another lacks is a
// schema that validates under `gen` and fails under `validate`.

// registerModuleRootFlag adds the shared --module-root flag.
func registerModuleRootFlag(cmd *cobra.Command) {
	cmd.Flags().String("module-root", "", "root directory for module-style imports (default: the nearest ancestor holding yammm.mod, else the schema's directory)")
}

// moduleRootOptions reads --module-root back as the absolute root and the
// load option carrying it. An unset flag yields "" and no option, so the
// loader discovers the root: the nearest ancestor holding a yammm.mod, else
// the schema's directory. It reports a usage error for a root that cannot be
// made absolute, having already written the message.
func moduleRootOptions(cmd *cobra.Command) (string, []schema.LoadOption, error) {
	root, _ := cmd.Flags().GetString("module-root")
	if root == "" {
		return "", nil, nil
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: resolve module root %q: %v\n", root, err)
		return "", nil, &cli.ExitError{Code: cli.ExitUsage}
	}
	return abs, []schema.LoadOption{schema.WithModuleRoot(abs)}, nil
}
