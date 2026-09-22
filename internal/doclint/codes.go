package doclint

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
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
}

// AssertCitedCodesExist reports every diagnostic code name cited under root
// that names no code in rules, and returns how many names it read.
//
// A code name is an E_ or W_ word in capitals, bare or qualified as
// diag.W_SNAPSHOT_PATH_FALLBACK. A name ending in an underscore, as in E_SNAPSHOT_*,
// cites a family and must be the prefix of at least one code. The names are
// read in every comment of every tracked Go file, whatever its build
// constraint, and in every tracked Markdown file. A Go string literal is not
// read, and neither is a path under a testdata directory: both hold fixtures,
// not claims.
//
// An exclusion that matches no file, and a placeholder that is registered or
// cited nowhere, are reported too, so neither can outlive what it names.
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
	excluded := make(map[string]bool)
	fset := token.NewFileSet()
	for _, rel := range files {
		if slices.Contains(strings.Split(rel, "/"), "testdata") {
			continue
		}
		isGo, isMarkdown := strings.HasSuffix(rel, ".go"), strings.HasSuffix(rel, ".md")
		if !isGo && !isMarkdown {
			continue
		}
		if p, ok := matchesAny(rules.Exclude, rel); ok {
			excluded[p] = true
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(rel))
		var cites []codeCitation
		if isGo {
			cites, err = goCommentCitations(fset, full)
		} else {
			cites, err = markdownCitations(full)
		}
		if err != nil {
			t.Errorf("%v", err)
			continue
		}
		for _, c := range cites {
			if isGo {
				n.Comments++
			} else {
				n.Markdown++
			}
			if _, ok := placeholders[c.name]; ok {
				placeholders[c.name] = true
				continue
			}
			if !registry.has(c.name) {
				t.Errorf("%s:%d: cites the diagnostic code %s, which %s", rel, c.line, c.name, registry.miss(c.name))
			}
		}
	}
	for _, p := range rules.Exclude {
		if !excluded[p] {
			t.Errorf("exclusion %q matches no tracked Go or Markdown file", p)
		}
	}
	for _, p := range rules.Placeholders {
		if !placeholders[p] {
			t.Errorf("placeholder %s is cited nowhere", p)
		}
	}
	return n
}

func matchesAny(patterns []string, rel string) (string, bool) {
	for _, p := range patterns {
		if ok, err := path.Match(p, rel); err == nil && ok {
			return p, true
		}
	}
	return "", false
}

// codeCitation is one code name and the 1-based line it was written on.
type codeCitation struct {
	name string
	line int
}

// wordPattern matches a whole identifier-shaped word, so a code name is never
// read out of the middle of a longer one such as NODE_PROPERTY_UNIQUENESS.
var wordPattern = regexp.MustCompile(`[A-Za-z0-9_]+`)

// markdownEscapes undoes the backslash escape Markdown prose puts inside a code
// name, as in E\_DUPLICATE\_PK. A name that ends in an underscore cites a
// family whether an asterisk follows or not, so an escaped asterisk needs no
// undoing. No replacement moves a newline.
var markdownEscapes = strings.NewReplacer(`\_`, "_")

// citationsIn returns the code names in text, whose first line is line. A name
// wrapped in emphasis underscores, as in _E_DUPLICATE_PK_ or
// __E_DUPLICATE_PK__, is read without them.
func citationsIn(text string, line int) []codeCitation {
	text = markdownEscapes.Replace(text)
	var out []codeCitation
	for _, loc := range wordPattern.FindAllStringIndex(text, -1) {
		word := text[loc[0]:loc[1]]
		family := loc[1] < len(text) && text[loc[1]] == '*'
		word = stripEmphasis(word, family)
		if isCodeName(word) {
			out = append(out, codeCitation{name: word, line: line + strings.Count(text[:loc[0]], "\n")})
		}
	}
	return out
}

// stripEmphasis removes the underscores that open word and as many that close
// it. A family name keeps its closing underscore, which is part of the name.
func stripEmphasis(word string, family bool) string {
	inner := strings.TrimLeft(word, "_")
	if family {
		return inner
	}
	for range len(word) - len(inner) {
		inner = strings.TrimSuffix(inner, "_")
	}
	return inner
}

// isCodeName reports whether word has a code's shape: E_ or W_, a capital,
// then capitals, digits and underscores.
func isCodeName(word string) bool {
	rest, ok := strings.CutPrefix(word, "E_")
	if !ok {
		rest, ok = strings.CutPrefix(word, "W_")
	}
	if !ok || rest == "" || rest[0] < 'A' || rest[0] > 'Z' {
		return false
	}
	return strings.Trim(rest, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") == ""
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
