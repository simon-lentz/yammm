package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A mutant is one directory of the mutants directory.
type mutant struct {
	id       string
	file     string // cleaned and slash-separated, from the checkout's root, as git lists it
	search   []byte
	replace  []byte
	spells   []string
	packages []string // as go test takes them, in the order results.tsv prints
}

// words splits text as a shell does an unquoted word list.
func words(text []byte) []string {
	return strings.FieldsFunc(string(text), func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' })
}

// readMutants reads every mutant of dir, sorted by id, and refuses the run,
// naming in one report each mutant that breaks a rule.
func readMutants(ctx context.Context, r *run, dir string) ([]mutant, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, refusef("the mutants directory: %v", err)
	}
	sets := &packageSets{run: r, named: map[string][]string{}}
	var mutants []mutant
	var faults []string
	for _, e := range entries {
		// A directory reached through a link is a mutant too. An entry whose name
		// begins with "." is not, as a git repository's own directory is not.
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, e.Name())); err != nil || !info.IsDir() {
			continue
		}
		m, err := readMutant(ctx, r, sets, dir, e.Name())
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("reading the mutants: %w", context.Cause(ctx))
			}
			faults = append(faults, fmt.Sprintf("mutant %s %v", printable(e.Name()), err))
			continue
		}
		mutants = append(mutants, m)
	}
	if len(faults) > 0 {
		return nil, refusef("%s", strings.Join(faults, "\n"))
	}
	if len(mutants) == 0 {
		return nil, refusef("no mutants in %s", dir)
	}
	slices.SortFunc(mutants, func(a, b mutant) int { return strings.Compare(a.id, b.id) })
	return mutants, nil
}

// printable quotes a name that would not print as itself.
func printable(name string) string {
	if strings.ContainsFunc(name, func(r rune) bool { return r == utf8.RuneError || !unicode.IsPrint(r) }) {
		return fmt.Sprintf("%q", name)
	}
	return name
}

func readMutant(ctx context.Context, r *run, sets *packageSets, dir, id string) (mutant, error) {
	m := mutant{id: id}
	// The id names a log file and is a cell of results.tsv, which is read as text.
	if printable(id) != id {
		return m, errors.New("has a name that is not printable text")
	}
	parts := map[string][]byte{}
	for _, name := range []string{"file", "search", "replace", "spell", "pkgs"} {
		path := filepath.Join(dir, id, name)
		// A read that opens a FIFO blocks until a writer comes.
		if info, err := os.Stat(path); err == nil && !info.Mode().IsRegular() {
			return m, fmt.Errorf("has a %s file that is not a regular file", name)
		}
		content, err := os.ReadFile(path)
		switch {
		case err == nil:
			parts[name] = content
		case errors.Is(err, os.ErrNotExist) && name == "pkgs":
		case errors.Is(err, os.ErrNotExist):
			return m, fmt.Errorf("has no %s file", name)
		default:
			return m, fmt.Errorf("has a %s file that cannot be read: %w", name, err)
		}
	}
	spelled := string(parts["file"])
	m.search, m.replace = parts["search"], parts["replace"]
	if !filepath.IsLocal(spelled) || strings.ContainsRune(spelled, 0) {
		return m, fmt.Errorf("names %q, which is no path inside the checkout", spelled)
	}
	// The path is opened as spelled, so a trailing separator still names no file.
	if info, err := os.Lstat(r.checkout + string(filepath.Separator) + filepath.FromSlash(spelled)); err != nil || !info.Mode().IsRegular() {
		return m, fmt.Errorf("names %q, which the checkout does not hold as a regular file", spelled)
	}
	// A report names the file as git lists it on every host.
	m.file = filepath.ToSlash(filepath.Clean(spelled))
	if !slices.Contains(r.tracked, m.file) && !slices.Contains(r.others, m.file) {
		return m, fmt.Errorf("names %q, which git ignores or does not list in the checkout, so no worker's copy holds it", m.file)
	}
	if len(m.search) == 0 {
		return m, errors.New("has an empty search file, which names no site")
	}
	m.spells = words(parts["spell"])
	if len(m.spells) == 0 {
		return m, errors.New("has a spell file that names no spelling")
	}
	for i, spell := range m.spells {
		if spell != "asis" && spell != "folded" {
			return m, fmt.Errorf("names an unknown spelling %q", spell)
		}
		if slices.Contains(m.spells[:i], spell) {
			return m, fmt.Errorf("names the spelling %s twice", spell)
		}
	}
	var err error
	if pkgs, ok := parts["pkgs"]; ok {
		m.packages, err = sets.namedBy(ctx, words(pkgs))
	} else {
		m.packages, err = sets.computed(ctx, m.file)
	}
	return m, err
}

// A listed is the part of a `go list -json` entry this program reads.
type listed struct {
	ImportPath     string
	Dir            string
	Name           string
	GoFiles        []string
	CgoFiles       []string
	SFiles         []string
	CFiles         []string
	CXXFiles       []string
	HFiles         []string
	SysoFiles      []string
	MFiles         []string
	FFiles         []string
	SwigFiles      []string
	SwigCXXFiles   []string
	EmbedFiles     []string
	TestGoFiles    []string
	XTestGoFiles   []string
	InvalidGoFiles []string // files the build takes and cannot load
	// The files the host's build leaves out.
	IgnoredGoFiles    []string
	IgnoredOtherFiles []string
	Imports           []string
	TestImports       []string
	XTestImports      []string
	Deps              []string
	Error             *struct{ Err string }
}

// listFields are the fields of listed, as go list's -json flag takes them.
const listFields = "ImportPath,Dir,Name,GoFiles,CgoFiles,SFiles,CFiles,CXXFiles,HFiles,SysoFiles,MFiles,FFiles,SwigFiles,SwigCXXFiles,EmbedFiles,TestGoFiles,XTestGoFiles,InvalidGoFiles,IgnoredGoFiles,IgnoredOtherFiles,Imports,TestImports,XTestImports,Deps,Error"

// goList runs go list -e in dir and returns its entries. With -e the command
// lists a package it cannot load, with its Error, where it would otherwise
// print nothing at all for the whole call.
func goList(ctx context.Context, dir string, env []string, args ...string) ([]listed, error) {
	var stdout, stderr bytes.Buffer
	args = append([]string{"list", "-e", "-json=" + listFields}, args...)
	code, err := goCommand(ctx, dir, env, &stdout, &stderr, args...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("go %s: exit %d\n%s", strings.Join(args, " "), code, bytes.TrimSpace(stderr.Bytes()))
	}
	var entries []listed
	for dec := json.NewDecoder(&stdout); ; {
		var e listed
		if err := dec.Decode(&e); errors.Is(err, io.EOF) {
			return entries, nil
		} else if err != nil {
			return nil, fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
		}
		entries = append(entries, e)
	}
}

// packageSets answers which packages judge a mutant: one listing of the
// checkout for every mutant with no pkgs file, and one for each word of each
// distinct pkgs file.
type packageSets struct {
	run     *run
	tracked []listed // the packages that hold a tracked Go file
	loaded  bool
	named   map[string][]string // a pkgs file's words, joined, to its packages
}

// load lists the checkout's packages, once when the listing succeeds. A
// package with no tracked Go file, whose Go files git ignores once
// scripts/packages.sh --check passes the checkout, is left out, as that script
// leaves it out of the suite.
func (s *packageSets) load(ctx context.Context) error {
	if s.loaded {
		return nil
	}
	entries, err := goList(ctx, s.run.checkout, s.run.env, "./...")
	if err != nil {
		return err
	}
	dirs := map[string]bool{}
	for _, name := range s.run.tracked {
		if strings.HasSuffix(name, ".go") {
			dirs[filepath.Join(s.run.checkout, filepath.FromSlash(filepath.ToSlash(filepath.Dir(name))))] = true
		}
	}
	for _, e := range entries {
		if dirs[e.Dir] {
			s.tracked = append(s.tracked, e)
		}
	}
	s.loaded = true
	return nil
}

// dotPath spells a package directory of the checkout as go test takes it.
func (s *packageSets) dotPath(dir string) string {
	rel, err := filepath.Rel(s.run.checkout, dir)
	if err != nil || rel == "." {
		return "."
	}
	return "./" + filepath.ToSlash(rel)
}

// computed returns the set of a mutant with no pkgs file: the file's package,
// each tracked package that imports it from its code or its tests, and the
// docs package, whose tests judge claims about packages it does not import.
func (s *packageSets) computed(ctx context.Context, file string) ([]string, error) {
	if err := s.load(ctx); err != nil {
		return nil, err
	}
	dir := filepath.Join(s.run.checkout, filepath.Dir(file))
	target := slices.IndexFunc(s.tracked, func(e listed) bool { return e.Dir == dir })
	if target < 0 {
		return nil, fmt.Errorf("names %q, whose directory is no tracked Go package, and has no pkgs file to say which packages judge it", file)
	}
	path := s.tracked[target].ImportPath
	var set []string
	for _, e := range s.tracked {
		if e.ImportPath == path || slices.Contains(e.Imports, path) || slices.Contains(e.TestImports, path) || slices.Contains(e.XTestImports, path) || e.Dir == filepath.Join(s.run.checkout, "docs") {
			set = append(set, s.dotPath(e.Dir))
		}
	}
	slices.Sort(set)
	return set, nil
}

// namedBy returns the words of a pkgs file after checking that each matches a
// package directory of the checkout. Each word is listed alone: go list warns
// and goes on past a word that matches no package.
func (s *packageSets) namedBy(ctx context.Context, patterns []string) ([]string, error) {
	key := strings.Join(patterns, " ")
	if set, ok := s.named[key]; ok {
		return set, nil
	}
	if len(patterns) == 0 {
		return nil, errors.New("has a pkgs file that names no package")
	}
	for _, p := range patterns {
		if strings.HasPrefix(p, "-") {
			return nil, fmt.Errorf("has a pkgs file that names %q, which go test would read as a flag", p)
		}
	}
	var entries []listed
	for _, p := range patterns {
		matched, err := goList(ctx, s.run.checkout, s.run.env, "--", p)
		if err != nil {
			return nil, fmt.Errorf("has a pkgs file go list cannot read: %w", err)
		}
		if len(matched) == 0 {
			return nil, fmt.Errorf("has a pkgs file whose word %s matches no package", p)
		}
		entries = append(entries, matched...)
	}
	for _, e := range entries {
		inside, err := within(s.run.checkout, e.Dir)
		if err != nil || !inside {
			return nil, fmt.Errorf("has a pkgs file that names %s, which is no package directory of the checkout", e.ImportPath)
		}
		if len(e.GoFiles)+len(e.CgoFiles)+len(e.TestGoFiles)+len(e.XTestGoFiles)+len(e.IgnoredGoFiles) == 0 {
			return nil, fmt.Errorf("has a pkgs file that names %s, a directory with no Go file", e.ImportPath)
		}
	}
	s.named[key] = patterns
	return patterns, nil
}
