package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

func newRootCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "yammm",
		Short: "Schema validation DSL for graph-based data infrastructure",
		Long: `yammm is a schema validation DSL for graph-based data infrastructure.

A command that succeeds prints its payload and its diagnostics, and nothing else.

Global flags:
  --json      Write diagnostics as one JSON document on stderr, a clean run
              included, and write "snapshot info"'s payload as JSON on
              stdout. A "help", "completion" or --version that succeeds
              writes no document.
  --no-color  Disable ANSI color in diagnostic output.

yammm --version prints the version; the flag is the program's alone.`,
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.PersistentFlags().Bool("json", false, "write diagnostics, and snapshot info's payload, as JSON")
	cmd.PersistentFlags().Bool("no-color", false, "disable ANSI color output")
	// Defined here so cobra's version flag takes no -v shorthand.
	cmd.Flags().Bool("version", false, "print the version")

	cmd.AddCommand(
		newValidateCmd(),
		newFmtCmd(),
		newCheckCmd(),
		newNeo4jCmd(),
		newGenCmd(),
		newSnapshotCmd(),
	)

	return cmd
}

// initHelpAndCompletion adds cobra's help and completion commands to root and
// makes each refuse what it cannot answer: cobra's help exits 0 for a topic no
// command answers, printing the usage or the nearest command's help, and its
// completion parent prints help and exits 0. A shell command that cannot
// write its script is a runtime failure. [execute] calls it once the caller has set root's streams, since each shell command
// binds its output when it is created.
//
// It also refuses an unknown subcommand on the help and flag-error paths, through
// [unknownSubcommand], and returns the help path's refusal, which cobra's Execute
// cannot return: it calls the help function and returns nil.
func initHelpAndCompletion(root *cobra.Command) (helpRefusal func() error) {
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		switch c.Name() {
		case "help":
			show := c.Run
			c.RunE = func(c *cobra.Command, args []string) error {
				if _, rest, err := c.Root().Find(args); err != nil || len(rest) > 0 {
					return cli.Usagef("unknown help topic %q; run %q to list the commands", strings.Join(args, " "), c.Root().Name()+" --help")
				}
				show(c, args)
				return nil
			}
		case "completion":
			c.RunE = requireSubcommand
			// A shell's script fails only as its write fails, which is no
			// usage error.
			for _, shell := range c.Commands() {
				write := shell.RunE
				shell.RunE = func(c *cobra.Command, args []string) error { return cli.Runtime(write(c, args)) }
			}
		}
	}

	initHelpFlags(root)
	root.InitDefaultVersionFlag()

	var refused error
	help := root.HelpFunc()
	root.SetHelpFunc(func(c *cobra.Command, args []string) {
		if err := unknownSubcommand(c); err != nil {
			refused = err
			return
		}
		help(c, args)
	})
	flagError := root.FlagErrorFunc()
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		if unknown := unknownSubcommand(c); unknown != nil {
			return unknown
		}
		return flagError(c, err)
	})
	return func() error { return refused }
}

// initHelpFlags adds the help flag to c and every command under it. cobra adds
// it only to the command it runs, after it has found that command, so without
// it "yammm neo4j --help diff" reads "diff" as an unknown --help's value.
func initHelpFlags(c *cobra.Command) {
	c.InitDefaultHelpFlag()
	for _, sub := range c.Commands() {
		initHelpFlags(sub)
	}
}

// unknownSubcommand refuses the first word c's flags left when c holds
// subcommands, in cobra.NoArgs's words. cobra parses flags and answers --help
// before it judges a command's words, so without it "yammm neo4j bogus --help"
// prints neo4j's help at exit 0 and "yammm neo4j bogus --schema x" names the flag.
func unknownSubcommand(c *cobra.Command) error {
	if !c.HasSubCommands() {
		return nil
	}
	return cobra.NoArgs(c, c.Flags().Args())
}

// withDiagnostics adapts a command that diagnoses to cobra's RunE. It closes
// the sink with the command's error after the command returns, so no return
// path leaves a diagnostic unwritten, and under --json the failure is inside
// the one document.
//
// Every error it returns carries an exit code: one the command did not classify
// is a runtime failure here ([cli.Runtime]), so [run] can tell a command's
// failure from one cobra raised before any command ran.
func withDiagnostics(run func(*cobra.Command, []string, *cli.DiagnosticSink) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		format := cli.FormatText
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			format = cli.FormatJSON
		}
		noColor, _ := cmd.Flags().GetBool("no-color")
		// The terminal test reads the process stream while the writer is
		// cobra's, as it was when every render built its own renderer.
		sink := cli.NewDiagnosticSink(cmd.ErrOrStderr(), format, noColor, cli.IsTTY(os.Stderr.Fd()))
		return sink.Close(cli.Runtime(run(cmd, args, sink)))
	}
}

// requireSubcommand is the RunE of a command that only groups others.
//
// A grouping command with no RunE is run by cobra as a success: it prints help
// and exits 0, so `yammm snapshot` in a script reports that the snapshot was
// taken. --help is handled before RunE and still prints help.
func requireSubcommand(cmd *cobra.Command, _ []string) error {
	return cli.Usagef("%q requires a subcommand; run %q to list them", cmd.CommandPath(), cmd.CommandPath()+" --help")
}
