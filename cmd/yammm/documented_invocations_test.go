package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/simon-lentz/yammm/internal/doclint"
)

// TestDocumentedInvocationsExist points the invocation gate at every tracked
// Markdown file but the change record: each `yammm …` example names a command
// the CLI has and flags that command defines, so a flag legal only on another
// command is reported. The table is read from newRootCmd's tree as execute
// builds it, cobra's help and completion commands included, with the help and
// version flags marked as cobra defines them, after the lookup.
func TestDocumentedInvocationsExist(t *testing.T) {
	t.Parallel()
	n := doclint.AssertInvocationsExist(t, "../..", doclint.CommandRules{
		Program:  "yammm",
		Commands: executedCommandTable(),
		// A change record states the surface of the release it describes.
		Exclude: []string{"docs/VERSIONING.md"},
	})
	// The floor sits just under the count at the tree that set it, so a walk
	// that stops reaching the Markdown fails instead of passing over nothing.
	if n < invocationFloor {
		t.Errorf("read only %d invocations; the walk is not reaching the Markdown", n)
	}
	t.Logf("read %d invocations", n)
}

const invocationFloor = 125

// executedCommandTable returns newRootCmd's table as execute builds the tree:
// cobra's help and completion commands included.
func executedCommandTable() []doclint.Command {
	root := newRootCmd("test")
	initHelpAndCompletion(root)
	return commandTable(root)
}

// commandTable returns every visible command under root with the flags it
// accepts, inherited ones included. cobra adds the help and version flags only
// to the command it has found, so each flag those initialisers add is marked
// AfterLookup.
func commandTable(root *cobra.Command) []doclint.Command {
	var cmds []*cobra.Command
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		cmds = append(cmds, c)
		for _, sub := range c.Commands() {
			if !sub.Hidden {
				walk(sub)
			}
		}
	}
	walk(root)
	atLookup := make(map[*cobra.Command]map[string]bool, len(cmds))
	for _, c := range cmds {
		names := make(map[string]bool)
		visitFlags(c, func(f *pflag.Flag) { names[f.Name] = true })
		atLookup[c] = names
	}
	table := make([]doclint.Command, 0, len(cmds))
	for _, c := range cmds {
		c.InitDefaultHelpFlag()
		c.InitDefaultVersionFlag()
		var flags []doclint.Flag
		visitFlags(c, func(f *pflag.Flag) {
			flags = append(flags, doclint.Flag{
				Name:        f.Name,
				Shorthand:   f.Shorthand,
				NoValue:     f.NoOptDefVal != "",
				AfterLookup: !atLookup[c][f.Name],
			})
		})
		table = append(table, doclint.Command{
			Path:  strings.Join(strings.Fields(c.CommandPath())[1:], " "),
			Flags: flags,
		})
	}
	return table
}

// visitFlags calls fn for each flag c accepts, its own and those it inherits.
func visitFlags(c *cobra.Command, fn func(*pflag.Flag)) {
	c.LocalFlags().VisitAll(fn)
	c.InheritedFlags().VisitAll(fn)
}

// TestCommandTable_HoldsWhatCobraAccepts pins the table the gate reads: the
// commands execute adds, a help flag and the inherited flags on every command,
// every flag one cobra defines there, NoValue exactly on the boolean flags,
// AfterLookup exactly on the help and version flags cobra adds once it has
// found the command, and no hidden command.
func TestCommandTable_HoldsWhatCobraAccepts(t *testing.T) {
	t.Parallel()
	byPath := make(map[string]map[string]doclint.Flag)
	for _, c := range executedCommandTable() {
		flags := make(map[string]doclint.Flag)
		for _, f := range c.Flags {
			flags[f.Name] = f
		}
		byPath[c.Path] = flags
	}
	for _, path := range []string{"", "help", "completion", "completion bash", "check", "snapshot save", "neo4j diff"} {
		flags, ok := byPath[path]
		if !ok {
			t.Errorf("the table has no command %q", path)
			continue
		}
		if f := flags["help"]; f.Shorthand != "h" || !f.NoValue || !f.AfterLookup {
			t.Errorf("%q: help flag = %+v, want -h taking no value, defined after the lookup", path, f)
		}
		if f, ok := flags["format"]; !ok || f.NoValue || f.AfterLookup {
			t.Errorf("%q: format flag = %+v, %v; want it inherited, taking a value, known to the lookup", path, f, ok)
		}
		if f := flags["no-color"]; !f.NoValue || f.AfterLookup {
			t.Errorf("%q: no-color flag = %+v; want it inherited, taking no value, known to the lookup", path, f)
		}
	}
	if f := byPath[""]["version"]; f.Shorthand != "v" || !f.NoValue || !f.AfterLookup {
		t.Errorf("the program's version flag = %+v, want -v taking no value, defined after the lookup", f)
	}
	if _, ok := byPath["check"]["version"]; ok {
		t.Error("check inherits --version, which cobra defines on the program alone")
	}

	cli := newRootCmd("test")
	initHelpAndCompletion(cli)
	table := commandTable(cli)
	for _, c := range table {
		cmd, _, err := cli.Find(strings.Fields(c.Path))
		if err != nil {
			t.Fatalf("%q: %v", c.Path, err)
		}
		names := make(map[string]bool)
		for _, f := range c.Flags {
			names[f.Name] = true
			pf := cmd.Flags().Lookup(f.Name)
			if pf == nil {
				pf = cmd.InheritedFlags().Lookup(f.Name)
			}
			if pf == nil {
				t.Errorf("%q: the table holds --%s, which cobra does not define there", c.Path, f.Name)
				continue
			}
			if isBool := pf.Value.Type() == "bool"; f.NoValue != isBool {
				t.Errorf("%q: --%s NoValue = %v, but its value type is %s", c.Path, f.Name, f.NoValue, pf.Value.Type())
			}
			if cobraAdds := f.Name == "help" || f.Name == "version"; f.AfterLookup != cobraAdds {
				t.Errorf("%q: --%s AfterLookup = %v", c.Path, f.Name, f.AfterLookup)
			}
		}
		for _, want := range []string{"help", "format", "no-color"} {
			if !names[want] {
				t.Errorf("%q: the table has no --%s", c.Path, want)
			}
		}
	}
	if len(table) < 20 {
		t.Errorf("the table holds %d commands; the walk is not reaching the tree", len(table))
	}

	root := &cobra.Command{Use: "prog"}
	root.AddCommand(&cobra.Command{Use: "shown"}, &cobra.Command{Use: "secret", Hidden: true})
	var paths []string
	for _, c := range commandTable(root) {
		paths = append(paths, c.Path)
	}
	if !slices.Equal(paths, []string{"", "shown"}) {
		t.Errorf("commandTable paths = %q, want the program and its visible command", paths)
	}
}
