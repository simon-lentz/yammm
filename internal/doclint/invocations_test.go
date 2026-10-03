package doclint_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/doclint"
)

// invocationRules is a small CLI: persistent --format and --no-color on the
// program, --version on the program alone, and a help flag everywhere, the
// last two defined only after the lookup, as cobra defines them.
func invocationRules() doclint.CommandRules {
	help := doclint.Flag{Name: "help", Shorthand: "h", NoValue: true, AfterLookup: true}
	inherited := []doclint.Flag{{Name: "format"}, {Name: "no-color", NoValue: true}, help}
	with := func(own ...doclint.Flag) []doclint.Flag { return append(own, inherited...) }
	return doclint.CommandRules{
		Program: "yammm",
		Commands: []doclint.Command{
			{Path: "", Flags: with(doclint.Flag{Name: "version", Shorthand: "v", NoValue: true, AfterLookup: true})},
			{Path: "check", Flags: with(doclint.Flag{Name: "type"}, doclint.Flag{Name: "from"})},
			{Path: "fmt", Flags: with(doclint.Flag{Name: "write", Shorthand: "w", NoValue: true}, doclint.Flag{Name: "check", NoValue: true})},
			{Path: "gen", Flags: with(doclint.Flag{Name: "to"}, doclint.Flag{Name: "output"})},
			{Path: "snapshot", Flags: with()},
			{Path: "snapshot save", Flags: with(doclint.Flag{Name: "output", Shorthand: "o"}, doclint.Flag{Name: "into"})},
		},
		Exclude: []string{"notes/*.md"},
	}
}

// invocationDoc holds the cases, a line each save the continued one on lines
// 21 and 22 and the several spans of lines 8 and 9; a comment names a line's
// case where its text does not.
const invocationDoc = "# Fixture\n" +
	"\n" +
	"Run `yammm check schema.yammm data.json --type User` to validate.\n" + // 3: clean
	"Run `yammm check --bogus x` here.\n" + // 4: an unknown flag
	"Run `yammm fmt --to md x.yammm`.\n" + // 5: a flag of another command
	"Run `yammm verify x.yammm`.\n" + // 6: an unknown command
	"Run `yammm snapshot bogus x.ys`.\n" + // 7: an unknown subcommand
	"Run `yammm snapshot <subcommand>` or `yammm [command]` or `yammm schema.yammm`.\n" + // 8: placeholders and a path
	"Name `yammm` alone; ``yammm check --twice`` is a double-backtick span.\n" + // 9: the name alone is not read
	"The unclosed and escaped spans are edgeDoc's, each a paragraph of its own:\n" +
	"in one paragraph a span may cross a line.\n" +
	"Padded ` yammm check --type T ` reads without its spaces.\n" + // 12: clean
	"\n" +
	"```sh\n" +
	"$ yammm --format json check x.yammm\n" + // 15: a persistent flag before the command
	"yammm check x.yammm | grep --after-pipe\n" +
	"yammm check x.yammm > out --after-redirect\n" +
	"yammm check x.yammm # --in-comment\n" +
	"yammm check x.yammm 2>&1 --after-dup\n" +
	"yammm check x.yammm && yammm --after-and\n" +
	"yammm snapshot save \\\n" + // 21: continued onto 22
	"  -o out.ys --nope x.yammm\n" +
	"yammm fmt -wv x.yammm\n" + // 23: -w takes no value, so v is a shorthand
	"yammm snapshot save -ofile.ys x.yammm\n" + // 24: an attached value
	"yammm snapshot save -o=file.ys x.yammm\n" +
	"yammm fmt [-w|--check|--synopsis-alt] x.yammm\n" + // 26: a synopsis's alternatives
	"yammm gen --to md [--output <path>] [--synopsis-opt]\n" + // 27: a synopsis's options
	"yammm check -- --after-dashes\n" +
	"yammm snapshot -o out.ys save x.yammm\n" + // 29: cobra's lookup takes out.ys as -o's value
	"yammm check --no-color=true x.yammm\n" +
	"yammm check x.yammm; yammm --after-semicolon\n" +
	"```\n" +
	"~~~\n" +
	"yammm tilde --in-tilde\n" + // 34: an unknown command, and a flag the program lacks
	"```\n" +
	"yammm check --backticks-in-tilde\n" + // 36: ``` does not close ~~~
	"```\n" +
	"~~~\n" +
	"````md\n" +
	"```\n" +
	"yammm check --inner-fence\n" + // 41: ``` does not close ````
	"```\n" +
	"````\n" +
	"After the fences `yammm check --after-fences`.\n"

// invocationFixture writes the documents under the test's temporary
// directory and returns it. The gate walks the filesystem there when that
// directory lies outside any git work tree, as TMPDIR's does by default.
func invocationFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range map[string]string{
		"README.md":         invocationDoc,
		"docs/crlf.md":      "```\r\nyammm check --crlf x.yammm\r\n```\r\n",
		"notes/excluded.md": "`yammm check --excluded`\n",
		"notes.txt":         "`yammm check --not-markdown`\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// edgeDoc holds the fence and span shapes a reading can get wrong, each
// inline case a paragraph of its own, since a span may cross a paragraph's
// lines. Each line the gate must read and report carries a flag that names its
// case, lines 2 and 17 are read and clean, and a line it must not read would
// be reported if it were.
const edgeDoc = "```\n" +
	"yammm check x.yammm\n" +
	"```   \n" + // 3: a closing run followed by spaces closes
	"After it `yammm check --after-spaced-close`.\n" +
	"\n" +
	"  - item:\n" +
	"    ```sh\n" + // 7: a fence inside a list item
	"    yammm check --indented-fence\n" +
	"    ```\n" +
	"\n" +
	"```sh\n" +
	"echo `yammm check --span-in-fence`\n" + // 12: a span inside a fence is not read
	"yammm check --continued-at-close \\\n" + // 13: a continued last line is read at the close
	"```\n" +
	"```\n" +
	"yammm check --next-block\n" + // 16: nothing carries over from the block before
	"yammm\n" + // 17: the program alone
	"$  yammm check --two-space-prompt\n" +
	"```\n" +
	"``\n" + // 20: two backticks open no fence
	"\n" +
	"`yammm check --after-two-backticks`\n" +
	"\n" +
	"```yammm check --info-backtick``` is a span, not a fence\n" +
	"\n" +
	"~~~ `x`\n" + // 26: a tilde fence's info string may hold a backtick
	"yammm check --in-tilde-with-backtick-info\n" +
	"~~~\n" +
	"```\n" +
	"```sh\n" + // 30: a run with an info string does not close
	"yammm check --after-info-line\n" +
	"`````\n" + // 32: a longer run closes
	"After `yammm check --after-long-close`.\n" +
	"\n" +
	"`yammm check ``x --mixed-runs`\n" + // 35: a run of another length does not close
	"\n" +
	"``yammm check --unclosed-run`\n" + // 37: an unclosed run is literal
	"\n" +
	"` yammm check --leading-space-only`\n" + // 39: one space is stripped only from both ends
	"\n" +
	"`a` yammm check --between-spans `b`\n" + // 41: a closing run opens nothing
	"\n" +
	"`yammm\tcheck --after-tab`\n" +
	"\n" +
	"An unclosed `yammm check --unclosed span.\n" +
	"\n" +
	"Escaped \\`yammm check --escaped\\` is literal.\n" +
	"\n" +
	"<a title=\"`\"> text `yammm check --after-html` end\n" + // 49: an HTML tag takes its backtick first
	"\n" +
	"~~~ `yammm check --in-info-string`\n" + // 51: the opening line is not read
	"~~~\n" +
	"`yammm check\n" + // 53: a span crosses its paragraph's lines
	"--crossing-span`\n" +
	"\n" +
	"```\n" +
	"\\\n" + // 57: a backslash alone continues onto the text
	"yammm check --after-lone-backslash\n" +
	"```\n" +
	"| run |\n" + // 60: GitHub's table unescapes a pipe inside a span
	"| --- |\n" +
	"| `yammm check --in-table \\| cat` |\n" +
	"\n" +
	"`yammm check\r\n" + // 64: a CRLF ending in a span reads as one space
	"--crlf-span`\n" +
	"\n" +
	"```\n" +
	"$ \\\n" + // 68: a prompt alone on a continued line
	"yammm check --after-lone-prompt\n" +
	"```\n" +
	"```\n" +
	"yammm check --at-the-end \\\n" // 72: an unclosed fence runs to the end, and its continued line is read

func TestAssertInvocationsExist_ReadsFencesAndSpansAsCommonMarkDoes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "edges.md"), []byte(edgeDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &recorder{}
	n := doclint.AssertInvocationsExist(r, root, doclint.CommandRules{
		Program:  "yammm",
		Commands: []doclint.Command{{Path: ""}, {Path: "check"}},
	})
	var got []string
	for _, m := range r.msgs {
		loc, rest, _ := strings.Cut(m, ": `")
		_, flag, _ := strings.Cut(rest, " passes ")
		flag, _, _ = strings.Cut(flag, ",")
		got = append(got, loc+" "+flag)
	}
	want := []string{
		"edges.md:4 --after-spaced-close",
		"edges.md:8 --indented-fence",
		"edges.md:13 --continued-at-close",
		"edges.md:16 --next-block",
		"edges.md:18 --two-space-prompt",
		"edges.md:22 --after-two-backticks",
		"edges.md:24 --info-backtick",
		"edges.md:27 --in-tilde-with-backtick-info",
		"edges.md:31 --after-info-line",
		"edges.md:33 --after-long-close",
		"edges.md:35 --mixed-runs",
		"edges.md:43 --after-tab",
		"edges.md:49 --after-html",
		"edges.md:53 --crossing-span",
		"edges.md:58 --after-lone-backslash",
		"edges.md:62 --in-table",
		"edges.md:64 --crlf-span",
		"edges.md:69 --after-lone-prompt",
		"edges.md:72 --at-the-end",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("reports:\n%s\nwant:\n%s\nmessages: %v", strings.Join(got, "\n"), strings.Join(want, "\n"), r.msgs)
	}
	for _, exact := range []string{
		"edges.md:53: `yammm check --crossing-span` passes --crossing-span, which \"yammm check\" does not define",
		"edges.md:62: `yammm check --in-table | cat` passes --in-table, which \"yammm check\" does not define",
		"edges.md:64: `yammm check --crlf-span` passes --crlf-span, which \"yammm check\" does not define",
	} {
		if !slices.Contains(r.msgs, exact) {
			t.Errorf("no report %q", exact)
		}
	}
	// Every report above, the clean line 2 and the program alone on line 17.
	if n != len(want)+2 {
		t.Errorf("read %d invocations, want %d", n, len(want)+2)
	}
}

// CommonMark reads CRLF and a lone CR as line endings, so a CR-only file holds
// fences, and a span that opens at a CRLF ending is trimmed as one opening at
// LF.
func TestAssertInvocationsExist_ReadsEveryLineEndingAsCommonMarkDoes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	doc := "para\r```\ryammm check --cr-only\r```\r\r\n`\r\nyammm check --crlf-lead `\r\n"
	if err := os.WriteFile(filepath.Join(root, "endings.md"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &recorder{}
	doclint.AssertInvocationsExist(r, root, doclint.CommandRules{
		Program:  "yammm",
		Commands: []doclint.Command{{Path: ""}, {Path: "check"}},
	})
	want := []string{
		"endings.md:3: `yammm check --cr-only` passes --cr-only, which \"yammm check\" does not define",
		"endings.md:7: `yammm check --crlf-lead` passes --crlf-lead, which \"yammm check\" does not define",
	}
	if !slices.Equal(r.msgs, want) {
		t.Errorf("reports %q, want %q", r.msgs, want)
	}
}

// A prompt is a "$" alone or followed by white space; "$yammm" is a word.
func TestAssertInvocationsExist_CutsOnlyAPrompt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	doc := "```\n$yammm check --not-a-prompt\n$\vyammm check --after-vertical-tab\n```\n"
	if err := os.WriteFile(filepath.Join(root, "prompt.md"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &recorder{}
	doclint.AssertInvocationsExist(r, root, doclint.CommandRules{
		Program:  "yammm",
		Commands: []doclint.Command{{Path: ""}, {Path: "check"}},
	})
	want := []string{"prompt.md:3: `yammm check --after-vertical-tab` passes --after-vertical-tab, which \"yammm check\" does not define"}
	if !slices.Equal(r.msgs, want) {
		t.Errorf("reports %q, want %q", r.msgs, want)
	}
}

// containerDoc holds fences inside a blockquote and on a list item's marker
// line; each line the gate must read carries a flag that names its case.
const containerDoc = "> ```sh\n" +
	"> yammm check --in-quote\n" +
	"> ```\n" +
	"> ```\n" +
	"> yammm check --quote-open\n" +
	"After the quote `yammm check --after-quote`.\n" + // 6: a line outside the quote ends its fence
	"- ```sh\n" +
	"  yammm check --in-list-fence\n" +
	"  ```\n" +
	"`yammm check --after-list-fence`\n" +
	"1. ```sh\n" +
	"   yammm check --in-ordered-fence\n" +
	"   ```\n" +
	"> - ```sh\n" +
	">   yammm check --in-quoted-list\n" +
	">   ```\n" +
	"`yammm check --after-all`\n" +
	"\n" +
	"- ```sh\n" +
	"  yammm check --in-open-list-fence\n" +
	"\n" +
	"Outside `yammm check --after-list-item`\n" // 22: the list item, and its open fence, end here

func TestAssertInvocationsExist_ReadsFencesInsideContainers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "containers.md"), []byte(containerDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &recorder{}
	n := doclint.AssertInvocationsExist(r, root, doclint.CommandRules{
		Program:  "yammm",
		Commands: []doclint.Command{{Path: ""}, {Path: "check"}},
	})
	var got []string
	for _, m := range r.msgs {
		loc, rest, _ := strings.Cut(m, ": `")
		_, flag, _ := strings.Cut(rest, " passes ")
		flag, _, _ = strings.Cut(flag, ",")
		got = append(got, loc+" "+flag)
	}
	want := []string{
		"containers.md:2 --in-quote",
		"containers.md:5 --quote-open",
		"containers.md:6 --after-quote",
		"containers.md:8 --in-list-fence",
		"containers.md:10 --after-list-fence",
		"containers.md:12 --in-ordered-fence",
		"containers.md:15 --in-quoted-list",
		"containers.md:17 --after-all",
		"containers.md:20 --in-open-list-fence",
		"containers.md:22 --after-list-item",
	}
	if !slices.Equal(got, want) {
		t.Errorf("reports:\n%s\nwant:\n%s\nmessages: %v", strings.Join(got, "\n"), strings.Join(want, "\n"), r.msgs)
	}
	if n != len(want) {
		t.Errorf("read %d invocations, want %d", n, len(want))
	}
}

// cobra looks a command up a level at a time, and at each level reads every
// word left with that command's flags: a flag written before a subcommand is
// read again one level down, where it may take a value.
func TestAssertInvocationsExist_LooksUpALevelAtATime(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	doc := "`p --b a c --x`\n\n`p a c --x`\n"
	if err := os.WriteFile(filepath.Join(root, "levels.md"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &recorder{}
	n := doclint.AssertInvocationsExist(r, root, doclint.CommandRules{
		Program: "p",
		Commands: []doclint.Command{
			{Path: "", Flags: []doclint.Flag{{Name: "b", NoValue: true}}},
			{Path: "a", Flags: []doclint.Flag{{Name: "b"}}},
			{Path: "a c", Flags: []doclint.Flag{{Name: "x", NoValue: true}}},
		},
	})
	want := []string{"levels.md:1: `p --b a c --x` passes --x, which \"p a\" does not define (defined on p a c)"}
	if !slices.Equal(r.msgs, want) {
		t.Errorf("reports %q, want %q", r.msgs, want)
	}
	if n != 2 {
		t.Errorf("read %d invocations, want 2", n)
	}
}

func TestAssertInvocationsExist_ReportsEveryNameTheCLILacks(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	doclint.AssertInvocationsExist(r, invocationFixture(t), invocationRules())
	want := []string{
		"README.md:4: `yammm check --bogus x` passes --bogus, which \"yammm check\" does not define",
		"README.md:5: `yammm fmt --to md x.yammm` passes --to, which \"yammm fmt\" does not define (defined on yammm gen)",
		"README.md:6: `yammm verify x.yammm` names \"verify\", which is no command of \"yammm\"",
		"README.md:7: `yammm snapshot bogus x.ys` names \"bogus\", which is no command of \"yammm snapshot\"",
		"README.md:9: `yammm check --twice` passes --twice, which \"yammm check\" does not define",
		"README.md:21: `yammm snapshot save -o out.ys --nope x.yammm` passes --nope, which \"yammm snapshot save\" does not define",
		"README.md:23: `yammm fmt -wv x.yammm` passes -v, which \"yammm fmt\" does not define (defined on yammm)",
		"README.md:26: `yammm fmt [-w|--check|--synopsis-alt] x.yammm` passes --synopsis-alt, which \"yammm fmt\" does not define",
		"README.md:27: `yammm gen --to md [--output <path>] [--synopsis-opt]` passes --synopsis-opt, which \"yammm gen\" does not define",
		"README.md:34: `yammm tilde --in-tilde` names \"tilde\", which is no command of \"yammm\"",
		"README.md:34: `yammm tilde --in-tilde` passes --in-tilde, which \"yammm\" does not define",
		"README.md:36: `yammm check --backticks-in-tilde` passes --backticks-in-tilde, which \"yammm check\" does not define",
		"README.md:41: `yammm check --inner-fence` passes --inner-fence, which \"yammm check\" does not define",
		"README.md:44: `yammm check --after-fences` passes --after-fences, which \"yammm check\" does not define",
		"docs/crlf.md:2: `yammm check --crlf x.yammm` passes --crlf, which \"yammm check\" does not define",
	}
	for _, w := range want {
		if !slices.Contains(r.msgs, w) {
			t.Errorf("no report %q", w)
		}
	}
	if len(r.msgs) != len(want) {
		t.Errorf("got %d reports, want %d:\n%s", len(r.msgs), len(want), strings.Join(r.msgs, "\n"))
	}
}

// resolveDoc holds the lookups and flag words a reading can get wrong, a
// line each; a line the gate must not report would be reported if it were
// misread.
const resolveDoc = "`yammm --no-color verify`\n" + // 1: a flag taking no value takes no word
	"`yammm snapshot [-o|--into] save`\n" + // 2: a synopsis word with alternatives takes none
	"`yammm snapshot x.ys save -o out.ys`\n" + // 3: an operand ends the lookup
	"`yammm check schema data`\n" + // 4: a command without subcommands takes operands
	"`yammm snapshot dir/x`\n" + // 5: a path is no command word
	"`yammm check; --after-semicolon`\n" +
	"`yammm check x || yammm --after-or`\n" +
	"`yammm check x & --after-amp`\n" +
	"`yammm check < in.json --after-lt`\n" +
	"`yammm check x &>log --after-amp-gt`\n" +
	"`yammm check x 1>out --after-fd`\n" +
	"`yammm fmt (--paren-synopsis)`\n" +
	"`yammm fmt --comma-synopsis, x`\n" +
	"`yammm - check --type T`\n" + // 14: a lone dash is not part of the lookup
	"`yammm snapshot [--]`\n" + // 15: a bracketed "--" is a placeholder, not a flag
	"`yammm fmt -zq x.yammm`\n" + // 16: after an unknown shorthand the rest is its value
	"`yammm --format=json verify`\n" + // 17: a flag with "=" takes no word
	"`yammm -vq verify`\n" + // 18: a single-dash word of three characters takes no word
	"`yammm -é verify`\n" + // 19: a single-dash word of three bytes takes no word
	"`yammm check --output x`\n" +
	"`yammm check -=x y`\n" + // 21: "=" first is a shorthand
	"`yammm --help snapshot save`\n" + // 22: a flag defined after the lookup takes a word
	"`yammm --version check`\n" +
	"`yammm check -h`\n" + // 24: and is defined once the command is found
	"`yammm fmt -w= x`\n" + // 25: "=" with nothing after it is the next shorthand
	"`yammm fmt -w=true x`\n" +
	"`yammm check --type -weird x`\n" + // 27: a flag taking a value takes the next word, dash and all
	"`yammm check ; --after-lone-semicolon`\n" +
	"`yammm check -z --after-unknown-shorthand`\n" // 29: judged, an unknown shorthand ends its word and takes none

func TestAssertInvocationsExist_ResolvesAsCobraDoes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "resolve.md"), []byte(resolveDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	rules := invocationRules()
	rules.Exclude = nil
	r := &recorder{}
	n := doclint.AssertInvocationsExist(r, root, rules)
	want := []string{
		"resolve.md:1: `yammm --no-color verify` names \"verify\", which is no command of \"yammm\"",
		"resolve.md:3: `yammm snapshot x.ys save -o out.ys` passes -o, which \"yammm snapshot\" does not define (defined on yammm snapshot save)",
		"resolve.md:12: `yammm fmt (--paren-synopsis)` passes --paren-synopsis, which \"yammm fmt\" does not define",
		"resolve.md:13: `yammm fmt --comma-synopsis, x` passes --comma-synopsis, which \"yammm fmt\" does not define",
		"resolve.md:16: `yammm fmt -zq x.yammm` passes -z, which \"yammm fmt\" does not define",
		"resolve.md:17: `yammm --format=json verify` names \"verify\", which is no command of \"yammm\"",
		"resolve.md:18: `yammm -vq verify` names \"verify\", which is no command of \"yammm\"",
		"resolve.md:18: `yammm -vq verify` passes -q, which \"yammm\" does not define",
		"resolve.md:19: `yammm -é verify` names \"verify\", which is no command of \"yammm\"",
		"resolve.md:19: `yammm -é verify` passes -é, which \"yammm\" does not define",
		"resolve.md:20: `yammm check --output x` passes --output, which \"yammm check\" does not define (defined on yammm gen, yammm snapshot save)",
		"resolve.md:21: `yammm check -=x y` passes -=, which \"yammm check\" does not define",
		"resolve.md:22: `yammm --help snapshot save` names \"save\", which is no command of \"yammm\"",
		"resolve.md:25: `yammm fmt -w= x` passes -=, which \"yammm fmt\" does not define",
		"resolve.md:29: `yammm check -z --after-unknown-shorthand` passes -z, which \"yammm check\" does not define",
		"resolve.md:29: `yammm check -z --after-unknown-shorthand` passes --after-unknown-shorthand, which \"yammm check\" does not define",
	}
	if !slices.Equal(r.msgs, want) {
		t.Errorf("reports:\n%s\nwant:\n%s", strings.Join(r.msgs, "\n"), strings.Join(want, "\n"))
	}
	if n != 29 {
		t.Errorf("read %d invocations, want 29", n)
	}
}

// Every read invocation counts, a clean one included, so a caller can hold a
// floor: ten inline spans on lines 3 to 12, nineteen fenced lines, the span on
// line 44 and the CRLF file's line.
func TestAssertInvocationsExist_CountsWhatItRead(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	if n := doclint.AssertInvocationsExist(r, invocationFixture(t), invocationRules()); n != 31 {
		t.Errorf("read %d invocations, want 31", n)
	}
}

func TestAssertInvocationsExist_ReportsStaleAndMalformedExclusions(t *testing.T) {
	t.Parallel()
	rules := invocationRules()
	rules.Exclude = append(rules.Exclude, "notes/excluded.md", "notes.txt", "gone/*.md")
	r := &recorder{}
	doclint.AssertInvocationsExist(r, invocationFixture(t), rules)
	for _, stale := range []string{`exclusion "notes.txt" matches no tracked Markdown file`, `exclusion "gone/*.md" matches no tracked Markdown file`} {
		if !r.reports(stale) {
			t.Errorf("no report %q; got %v", stale, r.msgs)
		}
	}
	for _, used := range []string{`exclusion "notes/*.md"`, `exclusion "notes/excluded.md"`} {
		if r.reports(used) {
			t.Errorf("reported %s as stale: %v", used, r.msgs)
		}
	}

	rules = invocationRules()
	rules.Exclude = append(rules.Exclude, "notes/[")
	r = &recorder{}
	if n := doclint.AssertInvocationsExist(r, invocationFixture(t), rules); n != 0 {
		t.Errorf("read %d invocations under a malformed exclusion; want none", n)
	}
	if !r.reports(`exclusion "notes/[" is not a valid pattern`) {
		t.Errorf("a malformed exclusion was not reported: %v", r.msgs)
	}
}

// A table that cannot describe a program is refused before any file is read,
// since a gap in it resolves an invocation against the wrong command.
func TestAssertInvocationsExist_RefusesATableWithAGap(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		change func(*doclint.CommandRules)
		want   string
	}{
		{"no program", func(r *doclint.CommandRules) { r.Program = "" }, `program "" is not one word`},
		{"two-word program", func(r *doclint.CommandRules) { r.Program = "go tool" }, `program "go tool" is not one word`},
		{"no entry for the program", func(r *doclint.CommandRules) { r.Commands = r.Commands[1:] }, `no entry for the program itself`},
		{"no parent", func(r *doclint.CommandRules) { r.Commands = append(r.Commands[:4], r.Commands[5:]...) }, `command "snapshot save" has no entry for its parent "snapshot"`},
		{"listed twice", func(r *doclint.CommandRules) { r.Commands = append(r.Commands, doclint.Command{Path: "check"}) }, `command "check" is listed twice`},
		{"spacing", func(r *doclint.CommandRules) {
			r.Commands = append(r.Commands, doclint.Command{Path: "snapshot  info"})
		}, `command path "snapshot  info" is not its words joined by single spaces`},
		{"dashed name", func(r *doclint.CommandRules) {
			r.Commands[1].Flags = append(r.Commands[1].Flags, doclint.Flag{Name: "--type"})
		}, `command "check" has a flag named "--type"`},
		{"long shorthand", func(r *doclint.CommandRules) {
			r.Commands[1].Flags = append(r.Commands[1].Flags, doclint.Flag{Name: "tee", Shorthand: "tt"})
		}, `gives --tee the shorthand "tt"`},
		{"a name holding =", func(r *doclint.CommandRules) {
			r.Commands[1].Flags = append(r.Commands[1].Flags, doclint.Flag{Name: "a=b"})
		}, `command "check" has a flag named "a=b"`},
		{"an = shorthand", func(r *doclint.CommandRules) {
			r.Commands[1].Flags = append(r.Commands[1].Flags, doclint.Flag{Name: "tee", Shorthand: "="})
		}, `gives --tee the shorthand "="`},
		{"a shorthand given twice", func(r *doclint.CommandRules) {
			r.Commands[1].Flags = append(r.Commands[1].Flags, doclint.Flag{Name: "tee", Shorthand: "t"}, doclint.Flag{Name: "tally", Shorthand: "t"})
		}, `command "check" defines -t twice`},
		{"a command word an invocation reads as a flag", func(r *doclint.CommandRules) {
			r.Commands = append(r.Commands, doclint.Command{Path: "-x"})
		}, `command path "-x" holds "-x", which an invocation never reads as a command`},
		{"a command word that ends a command", func(r *doclint.CommandRules) {
			r.Commands = append(r.Commands, doclint.Command{Path: "snapshot #info"})
		}, `command path "snapshot #info" holds "#info"`},
		{"a name holding an alternative bar", func(r *doclint.CommandRules) {
			r.Commands[1].Flags = append(r.Commands[1].Flags, doclint.Flag{Name: "a|b"})
		}, `command "check" has a flag named "a|b"`},
		{"a name a synopsis trims", func(r *doclint.CommandRules) {
			r.Commands[1].Flags = append(r.Commands[1].Flags, doclint.Flag{Name: "v."})
		}, `command "check" has a flag named "v."`},
		{"a name holding a vertical tab", func(r *doclint.CommandRules) {
			r.Commands[1].Flags = append(r.Commands[1].Flags, doclint.Flag{Name: "c\vd"})
		}, `has a flag named "c\vd"`},
		{"a name holding a no-break space", func(r *doclint.CommandRules) {
			r.Commands[1].Flags = append(r.Commands[1].Flags, doclint.Flag{Name: "c\u00a0d"})
		}, `has a flag named`},
		{"a shorthand of more than one byte", func(r *doclint.CommandRules) {
			r.Commands[1].Flags = append(r.Commands[1].Flags, doclint.Flag{Name: "accent", Shorthand: "é"})
		}, `gives --accent the shorthand "é"`},
		{"a shorthand a synopsis trims", func(r *doclint.CommandRules) {
			r.Commands[1].Flags = append(r.Commands[1].Flags, doclint.Flag{Name: "tee", Shorthand: "."})
		}, `gives --tee the shorthand "."`},
		{"a name given twice", func(r *doclint.CommandRules) {
			r.Commands[1].Flags = append(r.Commands[1].Flags, doclint.Flag{Name: "type", NoValue: true})
		}, `command "check" defines --type twice`},
		{"a grandparent without its parent", func(r *doclint.CommandRules) {
			r.Commands = append(r.Commands, doclint.Command{Path: "gen go extra"})
		}, `command "gen go extra" has no entry for its parent "gen go"`},
		{"a command word ending in a semicolon", func(r *doclint.CommandRules) {
			r.Commands = append(r.Commands, doclint.Command{Path: "x;"})
		}, `command path "x;" holds "x;"`},
		{"the end-of-options marker as a command", func(r *doclint.CommandRules) {
			r.Commands = append(r.Commands, doclint.Command{Path: "--"})
		}, `command path "--" holds "--"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			rules := invocationRules()
			c.change(&rules)
			r := &recorder{}
			if n := doclint.AssertInvocationsExist(r, invocationFixture(t), rules); n != 0 {
				t.Errorf("read %d invocations against a refused table; want none", n)
			}
			if len(r.msgs) != 1 || !strings.Contains(r.msgs[0], c.want) {
				t.Errorf("want the one report %q; got %v", c.want, r.msgs)
			}
		})
	}
}

// A report on a flag several commands define names them in one order.
func TestAssertInvocationsExist_NamesAFlagsOwnersInOrder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "wide.md"), []byte("`yammm --wide`\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wide := []doclint.Flag{{Name: "wide"}}
	rules := doclint.CommandRules{Program: "yammm", Commands: []doclint.Command{
		{Path: ""}, {Path: "e", Flags: wide}, {Path: "b", Flags: wide}, {Path: "d", Flags: wide}, {Path: "a", Flags: wide}, {Path: "c", Flags: wide},
	}}
	want := "wide.md:1: `yammm --wide` passes --wide, which \"yammm\" does not define (defined on yammm a, yammm b, yammm c, yammm d, yammm e)"
	for range 5 {
		r := &recorder{}
		doclint.AssertInvocationsExist(r, root, rules)
		if !slices.Equal(r.msgs, []string{want}) {
			t.Fatalf("reports %q, want %q", r.msgs, want)
		}
	}
}

// Each flag's unreadable shorthand is reported once, and none of them is
// indexed, so two flags sharing one are not also reported as one spelling
// defined twice.
func TestAssertInvocationsExist_ReportsEachBadShorthandOnce(t *testing.T) {
	t.Parallel()
	rules := invocationRules()
	rules.Commands[1].Flags = append(rules.Commands[1].Flags,
		doclint.Flag{Name: "tee", Shorthand: "tt"}, doclint.Flag{Name: "tally", Shorthand: "tt"})
	r := &recorder{}
	doclint.AssertInvocationsExist(r, invocationFixture(t), rules)
	want := []string{
		`command "check" gives --tally the shorthand "tt", which is not one byte an invocation can spell`,
		`command "check" gives --tee the shorthand "tt", which is not one byte an invocation can spell`,
	}
	if !slices.Equal(r.msgs, want) {
		t.Errorf("reports %q, want %q", r.msgs, want)
	}
}

// A table's problems are reported in one order, whatever the order of the map
// the table is indexed by.
func TestAssertInvocationsExist_ReportsEveryTableProblemInOrder(t *testing.T) {
	t.Parallel()
	rules := invocationRules()
	rules.Commands = append(rules.Commands,
		doclint.Command{Path: "zeta extra"}, doclint.Command{Path: "alpha extra"},
		doclint.Command{Path: "mid extra"}, doclint.Command{Path: "beta extra"})
	want := []string{
		`command "alpha extra" has no entry for its parent "alpha"`,
		`command "beta extra" has no entry for its parent "beta"`,
		`command "mid extra" has no entry for its parent "mid"`,
		`command "zeta extra" has no entry for its parent "zeta"`,
	}
	for range 5 {
		r := &recorder{}
		doclint.AssertInvocationsExist(r, invocationFixture(t), rules)
		if !slices.Equal(r.msgs, want) {
			t.Fatalf("reports %q, want %q", r.msgs, want)
		}
	}
}

func TestAssertInvocationsExist_MissingRootIsReported(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	if n := doclint.AssertInvocationsExist(r, filepath.Join(t.TempDir(), "absent"), invocationRules()); n != 0 {
		t.Errorf("read %d invocations under a missing root; want none", n)
	}
	if len(r.msgs) != 1 || !strings.HasPrefix(r.msgs[0], "listing the paths of ") {
		t.Errorf("a missing root should give one report, the listing's; got %v", r.msgs)
	}
}

// In a work tree only tracked Markdown is read, and a tracked file the gate
// cannot read is reported, never skipped.
//
// Not parallel: it builds its own repository (isolateFromEnclosingRepository).
func TestAssertInvocationsExist_ReadsTheTrackedMarkdown(t *testing.T) {
	isolateFromEnclosingRepository(t)
	dir := t.TempDir()
	for name, body := range map[string]string{
		"tracked.md":   "`yammm check --tracked`\n",
		"gone.md":      "`yammm check`\n",
		"untracked.md": "`yammm check --untracked`\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "tracked.md", "gone.md"}} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.Remove(filepath.Join(dir, "gone.md")); err != nil {
		t.Fatal(err)
	}
	rules := invocationRules()
	rules.Exclude = nil
	r := &recorder{}
	n := doclint.AssertInvocationsExist(r, dir, rules)
	for _, want := range []string{"tracked.md:1: `yammm check --tracked` passes --tracked", "reading gone.md"} {
		if !r.reports(want) {
			t.Errorf("no report %q; got %v", want, r.msgs)
		}
	}
	if r.reports("--untracked") {
		t.Errorf("an untracked file was read: %v", r.msgs)
	}
	if n != 1 {
		t.Errorf("read %d invocations, want 1", n)
	}
}
