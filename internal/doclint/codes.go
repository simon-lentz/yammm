package doclint

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// CodeCitations counts the diagnostic code names [AssertCitedCodesExist] read,
// by where they were written.
type CodeCitations struct {
	// Comments counts the names read in Go comments.
	Comments int
	// Markdown counts the names read in Markdown files.
	Markdown int
	// Shell counts the names read in shell scripts' comment lines.
	Shell int
}

// CodeRules is what [AssertCitedCodesExist] checks citations against.
type CodeRules struct {
	// Codes holds every registered code name.
	Codes []string
	// Exclude holds path.Match patterns over slash-separated paths from the
	// root. A tracked file matching one is not read.
	Exclude []string
	// Placeholders holds names that stand for a code that does not exist by
	// design, such as the name an example passes to diag.NewCode.
	Placeholders []string
	// Removed maps a slash-separated path from the root to the names of codes
	// that no longer exist and that file may cite, such as a change record
	// naming the codes a release removed or renamed.
	Removed map[string][]string
}

// AssertCitedCodesExist reports every diagnostic code name cited under root
// that names no code in rules, and returns how many names it read.
//
// A code name is an E_ or W_ word in capitals, bare or qualified as
// diag.W_SNAPSHOT_PATH_FALLBACK, or the capitals that open a longer word, as
// in E_TYPE_MISMATCHes. A name that ends in an underscore and is followed by
// an asterisk, as in E_SNAPSHOT_*, cites a family and must be the prefix of at
// least one code. The names are
// read in every comment of every tracked Go file, whatever its build
// constraint, in every tracked Markdown file, and in every tracked shell
// script's lines that open on "#", a heredoc's included. A Go string literal is not read, and neither is
// a path under a testdata directory: both hold fixtures, not claims.
//
// An exclusion that matches no file the gate reads, a placeholder that is
// registered or cited nowhere, and a removed name that is registered or not
// cited in its file are reported too, so none can outlive what it names.
func AssertCitedCodesExist(t TB, root string, rules CodeRules) CodeCitations {
	t.Helper()
	var n CodeCitations
	for _, p := range rules.Exclude {
		if _, err := path.Match(p, ""); err != nil {
			t.Errorf("exclusion %q is not a valid pattern: %v", p, err)
			return n
		}
	}
	files, err := trackedPaths(context.Background(), root)
	if err != nil {
		t.Errorf("listing the paths of %s: %v", root, err)
		return n
	}
	registry := newCodeSet(rules.Codes)
	placeholders := make(map[string]bool, len(rules.Placeholders))
	for _, p := range rules.Placeholders {
		placeholders[p] = false
		if registry.has(p) {
			t.Errorf("placeholder %s is a registered code; cite it as one", p)
		}
	}
	removedCited := make(map[string]map[string]bool, len(rules.Removed))
	for rel, names := range rules.Removed {
		removedCited[rel] = make(map[string]bool, len(names))
		for _, name := range names {
			removedCited[rel][name] = false
			if registry.has(name) {
				t.Errorf("removed name %s in %s is a registered code; cite it as one", name, rel)
			}
		}
	}
	excluded := make(map[string]bool)
	fset := token.NewFileSet()
	for _, rel := range files {
		if slices.Contains(strings.Split(rel, "/"), "testdata") {
			continue
		}
		isGo, isMarkdown, isShell := strings.HasSuffix(rel, ".go"), strings.HasSuffix(rel, ".md"), strings.HasSuffix(rel, ".sh")
		if !isGo && !isMarkdown && !isShell {
			continue
		}
		if matched := matchingPatterns(rules.Exclude, rel); len(matched) > 0 {
			for _, p := range matched {
				excluded[p] = true
			}
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(rel))
		var cites []codeCitation
		switch {
		case isGo:
			cites, err = goCommentCitations(fset, full)
		case isMarkdown:
			cites, err = markdownCitations(full)
		default:
			cites, err = shellCitations(full)
		}
		if err != nil {
			t.Errorf("%v", err)
			continue
		}
		for _, c := range cites {
			switch {
			case isGo:
				n.Comments++
			case isMarkdown:
				n.Markdown++
			default:
				n.Shell++
			}
			if _, ok := placeholders[c.name]; ok {
				placeholders[c.name] = true
				continue
			}
			if _, ok := removedCited[rel][c.name]; ok {
				removedCited[rel][c.name] = true
				continue
			}
			if !registry.has(c.name) {
				t.Errorf("%s:%d: cites the diagnostic code %s, which %s", rel, c.line, c.name, registry.miss(c.name))
			}
		}
	}
	for _, p := range rules.Exclude {
		if !excluded[p] {
			t.Errorf("exclusion %q matches no tracked Go, Markdown or shell file", p)
		}
	}
	for _, p := range rules.Placeholders {
		if !placeholders[p] {
			t.Errorf("placeholder %s is cited nowhere", p)
		}
	}
	for _, rel := range slices.Sorted(maps.Keys(removedCited)) {
		for _, name := range rules.Removed[rel] {
			if !removedCited[rel][name] {
				t.Errorf("removed name %s is not cited in %s", name, rel)
			}
		}
	}
	return n
}

// matchingPatterns returns every pattern that matches rel, so an exclusion
// shadowed by another one is still counted as used.
func matchingPatterns(patterns []string, rel string) []string {
	var matched []string
	for _, p := range patterns {
		if ok, err := path.Match(p, rel); err == nil && ok {
			matched = append(matched, p)
		}
	}
	return matched
}

// codeCitation is one code name and the 1-based line it was written on.
type codeCitation struct {
	name string
	line int
}

// wordPattern matches a whole identifier-shaped word, so a code name is never
// read out of the middle of a longer one such as NODE_PROPERTY_UNIQUENESS.
var wordPattern = regexp.MustCompile(`[A-Za-z0-9_]+`)

// markdownEscapes undoes the backslash escapes Markdown prose puts inside a
// code name, as in E\_DUPLICATE\_PK and the family W\_SNAPSHOT\_\*. No
// replacement moves a newline.
var markdownEscapes = strings.NewReplacer(`\_`, "_", `\*`, "*")

// citationsIn returns the code names in text, whose first line is line. A name
// wrapped in emphasis underscores, as in _E_DUPLICATE_PK_, __E_DUPLICATE_PK__
// or the unbalanced _E_DUPLICATE_PK__, is read without them.
func citationsIn(text string, line int) []codeCitation {
	text = markdownEscapes.Replace(text)
	var out []codeCitation
	for _, loc := range wordPattern.FindAllStringIndex(text, -1) {
		family := loc[1] < len(text) && text[loc[1]] == '*'
		if name, ok := codeName(stripEmphasis(text[loc[0]:loc[1]], family)); ok {
			out = append(out, codeCitation{name: name, line: line + strings.Count(text[:loc[0]], "\n")})
		}
	}
	return out
}

// stripEmphasis removes the underscores that open word and those that close
// it. A family name keeps its closing underscores, which are part of the name;
// without the asterisk that marks a family, a closing underscore is emphasis.
func stripEmphasis(word string, family bool) string {
	inner := strings.TrimLeft(word, "_")
	if family {
		return inner
	}
	return strings.TrimRight(inner, "_")
}

// codeName returns the code name word cites: E_ or W_, a capital, then
// capitals, digits and underscores, up to the first lowercase letter. A word
// that goes on in lowercase cites the name before it only when that name has
// two characters or more after the prefix, as in E_TYPE_MISMATCHes: E_Known
// and W_Foo_bar are capitalized words, not codes.
func codeName(word string) (string, bool) {
	if !strings.HasPrefix(word, "E_") && !strings.HasPrefix(word, "W_") {
		return "", false
	}
	end := 2
	for end < len(word) && isCodeByte(word[end]) {
		end++
	}
	if end == 2 || word[2] < 'A' || word[2] > 'Z' {
		return "", false
	}
	if end == len(word) {
		return word, true
	}
	name := strings.TrimRight(word[:end], "_")
	if len(name) < 4 {
		return "", false
	}
	return name, true
}

func isCodeByte(c byte) bool {
	return 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '_'
}

func goCommentCitations(fset *token.FileSet, file string) ([]codeCitation, error) {
	f, err := parser.ParseFile(fset, file, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", file, err)
	}
	var out []codeCitation
	for _, g := range f.Comments {
		for _, c := range g.List {
			out = append(out, citationsIn(c.Text, fset.Position(c.Pos()).Line)...)
		}
	}
	return out, nil
}

// shellCitations reads every line of a shell script whose first non-blank
// character is "#", a heredoc's lines included, except a shebang: "#!" at the
// very start of the first line.
func shellCitations(file string) ([]codeCitation, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", file, err)
	}
	var out []codeCitation
	for i, l := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimLeft(l, " \t")
		if !strings.HasPrefix(trimmed, "#") || (i == 0 && strings.HasPrefix(l, "#!")) {
			continue
		}
		out = append(out, citationsIn(trimmed, i+1)...)
	}
	return out, nil
}

func markdownCitations(file string) ([]codeCitation, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", file, err)
	}
	return citationsIn(string(data), 1), nil
}

// codeSet answers whether a cited name exists: a whole name by equality, a
// family name ending in an underscore by prefix.
type codeSet struct {
	exact  map[string]bool
	sorted []string
}

func newCodeSet(codes []string) codeSet {
	s := codeSet{exact: make(map[string]bool, len(codes)), sorted: slices.Sorted(slices.Values(codes))}
	for _, c := range codes {
		s.exact[c] = true
	}
	return s
}

func (s codeSet) has(name string) bool {
	if !strings.HasSuffix(name, "_") {
		return s.exact[name]
	}
	i, _ := slices.BinarySearch(s.sorted, name)
	return i < len(s.sorted) && strings.HasPrefix(s.sorted[i], name)
}

// miss completes the failure message for a name that s does not hold.
func (s codeSet) miss(name string) string {
	if strings.HasSuffix(name, "_") {
		return "is the prefix of no registered code"
	}
	return "is not a registered code"
}
