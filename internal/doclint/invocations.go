package doclint

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// CommandRules is what [AssertInvocationsExist] checks invocations against.
type CommandRules struct {
	// Program is the word an invocation starts with, such as yammm.
	Program string
	// Commands holds every command the program accepts. The program itself
	// is the command whose Path is "".
	Commands []Command
	// Exclude holds path.Match patterns over slash-separated paths from the
	// root. A tracked Markdown file matching one is not read.
	Exclude []string
}

// Command is one command of a program and the flags it accepts.
type Command struct {
	// Path is the command's words after the program, joined by single
	// spaces, such as "snapshot save". The program itself is "".
	Path string
	// Flags holds every flag the command accepts, inherited ones included.
	Flags []Flag
}

// Flag is one flag a command accepts.
type Flag struct {
	// Name is the long name without its dashes, such as format.
	Name string
	// Shorthand is the one-byte name without its dash, or "", as pflag
	// allows.
	Shorthand string
	// NoValue reports that the flag takes no separate value, as a boolean
	// flag does: the word after it is never its value.
	NoValue bool
	// AfterLookup reports that the program defines the flag only once it has
	// found the command, as cobra does its help and version flags. Written
	// before the command's name, such a flag takes the next word.
	AfterLookup bool
}

// AssertInvocationsExist reports every invocation of rules.Program in the
// tracked Markdown under root that names a command the program lacks, or a
// flag the command it names does not define, and returns how many invocations
// it read. The package
// documentation states what an invocation is and how it is read.
//
// An exclusion that matches no Markdown file is reported too, so none can
// outlive the file it names.
func AssertInvocationsExist(t TB, root string, rules CommandRules) (checked int) {
	t.Helper()
	tree, problems := newCommandTree(rules)
	for _, p := range rules.Exclude {
		if _, err := path.Match(p, ""); err != nil {
			problems = append(problems, fmt.Sprintf("exclusion %q is not a valid pattern: %v", p, err))
		}
	}
	if len(problems) > 0 {
		for _, p := range problems {
			t.Errorf("%s", p)
		}
		return 0
	}
	files, err := trackedPaths(context.Background(), root)
	if err != nil {
		t.Errorf("listing the paths of %s: %v", root, err)
		return 0
	}
	excluded := make(map[string]bool)
	for _, rel := range files {
		if !strings.HasSuffix(rel, ".md") {
			continue
		}
		if matched := matchingPatterns(rules.Exclude, rel); len(matched) > 0 {
			for _, p := range matched {
				excluded[p] = true
			}
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("reading %s: %v", rel, err)
			continue
		}
		invs, err := invocations(data, rules.Program)
		if err != nil {
			t.Errorf("reading %s: %v", rel, err)
			continue
		}
		for _, inv := range invs {
			checked++
			for _, p := range tree.check(inv.text) {
				t.Errorf("%s:%d: `%s` %s", rel, inv.line, inv.text, p)
			}
		}
	}
	for _, p := range rules.Exclude {
		if !excluded[p] {
			t.Errorf("exclusion %q matches no tracked Markdown file", p)
		}
	}
	return checked
}

// commandTree is a program's commands indexed for resolution: each path's
// flags by the spelling an invocation writes, "--name" or "-s".
type commandTree struct {
	program string
	flags   map[string]map[string]Flag
	// groups holds the paths that have subcommands.
	groups map[string]bool
}

// newCommandTree indexes rules and returns each entry that cannot describe a
// program: a gap resolves an invocation against the wrong command, and an
// entry the gate cannot read back from an invocation never matches one.
func newCommandTree(rules CommandRules) (commandTree, []string) {
	tree := commandTree{
		program: rules.Program,
		flags:   make(map[string]map[string]Flag),
		groups:  make(map[string]bool),
	}
	var problems []string
	if !slices.Equal(strings.Fields(rules.Program), []string{rules.Program}) {
		problems = append(problems, fmt.Sprintf("program %q is not one word", rules.Program))
	}
	for _, c := range rules.Commands {
		words := strings.Fields(c.Path)
		if c.Path != strings.Join(words, " ") {
			problems = append(problems, fmt.Sprintf("command path %q is not its words joined by single spaces", c.Path))
			continue
		}
		if i := slices.IndexFunc(words, func(w string) bool { return !readsAsCommand(w) }); i >= 0 {
			problems = append(problems, fmt.Sprintf("command path %q holds %q, which an invocation never reads as a command", c.Path, words[i]))
			continue
		}
		if _, dup := tree.flags[c.Path]; dup {
			problems = append(problems, fmt.Sprintf("command %q is listed twice", c.Path))
			continue
		}
		byName := make(map[string]Flag, 2*len(c.Flags))
		for _, f := range c.Flags {
			if f.Name == "" || strings.HasPrefix(f.Name, "-") || !readsAsFlag("--"+f.Name) {
				problems = append(problems, fmt.Sprintf("command %q has a flag named %q, which an invocation cannot spell", c.Path, f.Name))
				continue
			}
			spellings := []string{"--" + f.Name}
			if f.Shorthand != "" {
				if len(f.Shorthand) != 1 || !readsAsFlag("-"+f.Shorthand) {
					problems = append(problems, fmt.Sprintf("command %q gives --%s the shorthand %q, which is not one byte an invocation can spell", c.Path, f.Name, f.Shorthand))
					continue
				}
				spellings = append(spellings, "-"+f.Shorthand)
			}
			for _, sp := range spellings {
				if _, dup := byName[sp]; dup {
					problems = append(problems, fmt.Sprintf("command %q defines %s twice", c.Path, sp))
					continue
				}
				byName[sp] = f
			}
		}
		tree.flags[c.Path] = byName
	}
	if _, ok := tree.flags[""]; !ok {
		problems = append(problems, "the commands hold no entry for the program itself, the path \"\"")
	}
	for p := range tree.flags {
		if p == "" {
			continue
		}
		parent := ""
		if i := strings.LastIndexByte(p, ' '); i >= 0 {
			parent = p[:i]
		}
		if _, ok := tree.flags[parent]; !ok && parent != "" {
			problems = append(problems, fmt.Sprintf("command %q has no entry for its parent %q", p, parent))
		}
		tree.groups[parent] = true
	}
	slices.Sort(problems)
	return tree, problems
}

// readsAsWord reports whether [commandTree.check] reads tok, written alone, as
// the word tok: one field that ends no command and is not the end-of-options
// marker --.
func readsAsWord(tok string) bool {
	return slices.Equal(strings.Fields(tok), []string{tok}) && !strings.HasSuffix(tok, ";") && !endsCommand(tok) && tok != "--"
}

// readsAsCommand reports whether an invocation can name a command word w.
func readsAsCommand(w string) bool { return readsAsWord(w) && !isFlagWord(w) }

// readsAsFlag reports whether an invocation can spell a flag sp, "--name" or
// "-s": the gate reads it back as that one flag.
func readsAsFlag(sp string) bool {
	return readsAsWord(sp) && !strings.Contains(sp, "=") && slices.Equal(flagAlternatives(sp), []string{sp})
}

// check returns what is wrong with one invocation, read by the rules the
// package documentation's Invocations section states.
func (tree commandTree) check(text string) []string {
	var problems []string
	args := invocationArgs(text)
	path, stopped := tree.lookup(args)
	if stopped != "" && tree.groups[path] && !strings.ContainsAny(stopped, "<[./") {
		problems = append(problems, fmt.Sprintf("names %q, which is no command of %q", stopped, tree.name(path)))
	}
	skip := false
	for _, tok := range args {
		if skip {
			skip = false
			continue
		}
		if tok == "--" {
			break
		}
		alts := flagAlternatives(tok)
		for _, alt := range alts {
			names, takesNext := tree.readFlag(path, alt)
			for _, name := range names {
				if _, ok := tree.flags[path][name]; !ok {
					problems = append(problems, fmt.Sprintf("passes %s, which %q does not define%s", name, tree.name(path), tree.elsewhere(name)))
				}
			}
			skip = takesNext && len(alts) == 1
		}
	}
	return problems
}

// invocationArgs returns the words after the program, up to the shell text
// that ends the command; a word ending in ";" is kept without it and is the
// last.
func invocationArgs(text string) []string {
	var args []string
	for _, tok := range strings.Fields(text)[1:] {
		tok, last := strings.CutSuffix(tok, ";")
		if tok == "" || endsCommand(tok) {
			break
		}
		args = append(args, tok)
		if last {
			break
		}
	}
	return args
}

// lookup returns the command args reach as cobra's Find reaches one, and the
// first word that named no subcommand of it, or "".
func (tree commandTree) lookup(args []string) (path, stopped string) {
	for {
		words := tree.stripFlags(args, path)
		if len(words) == 0 {
			return path, ""
		}
		next := joinPath(path, words[0])
		if tree.flags[next] == nil {
			return path, words[0]
		}
		args = tree.argsMinusFirst(args, words[0], path)
		path = next
	}
}

// stripFlags returns the words of args that cobra's stripFlags keeps as
// command words at path, a synopsis word read as the flag it names.
func (tree commandTree) stripFlags(args []string, path string) []string {
	var words []string
	for i := 0; i < len(args); i++ {
		switch tok := args[i]; {
		case tok == "--":
			return words
		case isFlagWord(tok):
			if tree.takesNextWord(path, tok) {
				i++
			}
		case tok[0] != '-':
			words = append(words, tok)
		}
	}
	return words
}

// argsMinusFirst returns args without the first command word x, as cobra's
// argsMinusFirstX does before it looks x's subcommands up.
func (tree commandTree) argsMinusFirst(args []string, x, path string) []string {
	for i := 0; i < len(args); i++ {
		switch tok := args[i]; {
		case tok == "--":
			return args
		case isFlagWord(tok):
			if tree.takesNextWord(path, tok) {
				i++
			}
		case tok == x:
			return slices.Concat(args[:i], args[i+1:])
		}
	}
	return args
}

// endsCommand reports whether tok is shell text that ends the command: a
// comment, a pipe, a list operator or a redirection. "<" alone is a
// redirection; "<schema>" is a placeholder.
func endsCommand(tok string) bool {
	switch tok {
	case "|", "||", "&&", "&", "<":
		return true
	}
	if strings.HasPrefix(tok, "#") || strings.HasPrefix(tok, ">") || strings.HasPrefix(tok, "&>") {
		return true
	}
	digits := strings.TrimLeft(tok, "0123456789")
	return len(digits) < len(tok) && strings.HasPrefix(digits, ">")
}

// isFlagWord reports whether tok is a flag once a synopsis's brackets are
// removed, as in [--output <path>] and [-w|--check].
func isFlagWord(tok string) bool { return len(flagAlternatives(tok)) > 0 }

// flagAlternatives returns the flags tok writes: one, or each alternative of a
// synopsis such as [-w|--check]. An alternative that is not flag-shaped, such
// as a value, is left out.
func flagAlternatives(tok string) []string {
	tok = strings.TrimLeft(tok, "[(")
	tok = strings.TrimRight(tok, "]),.")
	var out []string
	for alt := range strings.SplitSeq(tok, "|") {
		if len(alt) >= 2 && alt[0] == '-' && alt != "--" {
			out = append(out, alt)
		}
	}
	return out
}

// takesNextWord reports whether a flag word consumes the word after it during
// the lookup, by the rule of cobra's stripFlags: a flag the lookup does not
// know takes one, and a synopsis word with alternatives takes none.
func (tree commandTree) takesNextWord(path, tok string) bool {
	alts := flagAlternatives(tok)
	if len(alts) != 1 {
		return false
	}
	flag := alts[0]
	if strings.Contains(flag, "=") {
		return false
	}
	if !strings.HasPrefix(flag, "--") && len(flag) != 2 {
		return false
	}
	f, ok := tree.flags[path][flag]
	return !ok || f.AfterLookup || !f.NoValue
}

// readFlag returns the flags one flag word names on path, each spelled
// "--name" or "-s", read as pflag parses the word, and whether the flag takes
// the next word as its value. The package documentation states pflag's rule.
func (tree commandTree) readFlag(path, word string) (names []string, takesNext bool) {
	if strings.HasPrefix(word, "--") {
		name, _, hasValue := strings.Cut(word, "=")
		f, ok := tree.flags[path][name]
		return []string{name}, ok && !f.NoValue && !hasValue
	}
	for rest := word[1:]; rest != ""; {
		r, size := utf8.DecodeRuneInString(rest)
		name := "-" + string(r)
		names = append(names, name)
		rest = rest[size:]
		f, ok := tree.flags[path][name]
		switch {
		case !ok, len(rest) >= 2 && rest[0] == '=':
			return names, false
		case f.NoValue:
			continue
		default:
			return names, rest == ""
		}
	}
	return names, false
}

// elsewhere names the commands that do define flag, so a report on a flag
// written on the wrong command says where it belongs.
func (tree commandTree) elsewhere(flag string) string {
	var owners []string
	for p, fs := range tree.flags {
		if _, ok := fs[flag]; ok {
			owners = append(owners, tree.name(p))
		}
	}
	if len(owners) == 0 {
		return ""
	}
	slices.Sort(owners)
	return fmt.Sprintf(" (defined on %s)", strings.Join(owners, ", "))
}

func (tree commandTree) name(path string) string { return joinPath(tree.program, path) }

func joinPath(path, word string) string {
	if path == "" {
		return word
	}
	if word == "" {
		return path
	}
	return path + " " + word
}

// invocation is one invocation and the 1-based line it starts on.
type invocation struct {
	line int
	text string
}

// invocations returns every invocation of program in a Markdown document,
// read by the rules the package documentation's Invocations section states.
func invocations(doc []byte, program string) ([]invocation, error) {
	// CommonMark reads CRLF and a lone CR as line endings; goldmark reads LF.
	doc = bytes.ReplaceAll(bytes.ReplaceAll(doc, []byte("\r\n"), []byte("\n")), []byte("\r"), []byte("\n"))
	root := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(text.NewReader(doc))
	var out []invocation
	err := ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.FencedCodeBlock:
			out = append(out, fencedInvocations(doc, n.Lines(), program)...)
			return ast.WalkSkipChildren, nil
		case *ast.CodeSpan:
			var span strings.Builder
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					span.Write(t.Segment.Value(doc))
				}
			}
			text := strings.ReplaceAll(span.String(), "\n", " ")
			if first, ok := n.FirstChild().(*ast.Text); ok && invokes(text, program, false) {
				out = append(out, invocation{line: lineOf(doc, first.Segment.Start), text: text})
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking its Markdown: %w", err)
	}
	return out, nil
}

// fencedInvocations returns the invocations among a fenced code block's
// lines. A line ending in a backslash is joined to the next without it; a
// backslash ending the block's last line is dropped. An invocation's line is
// where its text starts.
func fencedInvocations(doc []byte, lines *text.Segments, program string) []invocation {
	var out []invocation
	var parts []string
	start := 0
	flush := func() {
		text := strings.Join(parts, " ")
		parts = nil
		if invokes(text, program, true) {
			out = append(out, invocation{line: start, text: text})
		}
	}
	for i := range lines.Len() {
		seg := lines.At(i)
		rest, more := strings.CutSuffix(strings.TrimSpace(string(seg.Value(doc))), `\`)
		rest = strings.TrimSpace(rest)
		if len(parts) == 0 {
			rest = cutPrompt(rest)
		}
		if rest != "" {
			if len(parts) == 0 {
				start = lineOf(doc, seg.Start)
			}
			parts = append(parts, rest)
		}
		if !more {
			flush()
		}
	}
	flush()
	return out
}

// cutPrompt removes a "$" shell prompt that opens a trimmed line: a "$" alone
// or followed by white space.
func cutPrompt(line string) string {
	rest, ok := strings.CutPrefix(line, "$")
	if r, _ := utf8.DecodeRuneInString(rest); !ok || (rest != "" && !unicode.IsSpace(r)) {
		return line
	}
	return strings.TrimSpace(rest)
}

// lineOf returns the 1-based line of the byte at offset in doc.
func lineOf(doc []byte, offset int) int { return 1 + bytes.Count(doc[:offset], []byte("\n")) }

// invokes reports whether text is program followed by white space, or, when
// bare is set, program alone.
func invokes(text, program string, bare bool) bool {
	rest, ok := strings.CutPrefix(text, program)
	if !ok {
		return false
	}
	if rest == "" {
		return bare
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return unicode.IsSpace(r)
}
