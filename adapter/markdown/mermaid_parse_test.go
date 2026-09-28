//go:build mermaid

package markdown

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/simon-lentz/yammm/schema"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// mermaidDir holds the lock-filed npm package that pins the Mermaid releases
// this test parses with, the node script that drives them, and the schema
// fixtures under fixtures/.
var mermaidDir = filepath.Join("testdata", "mermaid")

// goldenOptions are the options each golden was generated with, keyed by its
// path under testdata without ".md.golden". The test regenerates each golden
// and requires the bytes to match, so a wrong entry here fails loudly.
var goldenOptions = map[string][]Option{
	"no_diagram": {WithClassDiagram(false)},
	"no_members": {WithClassMembers(false)},
}

// diagramCase is one document whose class diagrams Mermaid parses.
type diagramCase struct {
	name       string
	doc        []byte
	marshalErr error // Marshal refused the schema; doc is the unchecked emission
	drift      bool  // the golden is not the document Marshal generates now
	wantFences int
	meant      diagramMeaning
	fences     []string
	first      int // index of fences[0] in the batch sent to node
}

// diagramMeaning is what the emitter means a class diagram to say, derived
// from the schema alone.
type diagramMeaning struct {
	direction string
	classes   []mermaidClass
	relations []mermaidRelation
}

type mermaidClass struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type mermaidRelation struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

// mermaidReading is what one Mermaid release parsed from one diagram text.
type mermaidReading struct {
	Error     string `json:"error"`
	Type      string `json:"type"`
	Direction string `json:"direction"`
	Classes   []struct {
		mermaidClass
		RawLabel string `json:"rawLabel"`
	} `json:"classes"`
	Relations []struct {
		mermaidRelation
		Arrow    string `json:"arrow"`
		RawLabel string `json:"rawLabel"`
	} `json:"relations"`
}

type mermaidOutput struct {
	Versions []struct {
		Package string           `json:"package"`
		Version string           `json:"version"`
		Results []mermaidReading `json:"results"`
	} `json:"versions"`
}

// TestMarshal_MermaidReadsWhatTheEmitterMeant parses every class diagram of
// every golden, and of every schema under testdata/mermaid/fixtures, with each
// Mermaid release the npm package in testdata/mermaid pins. For each release it
// requires the direction, the classes with their labels and the relations with
// their kinds and labels, as Mermaid's parser holds them, to equal what the
// schema says the diagram holds. The SVG Mermaid 11 renders a label's Markdown
// into is not read.
// Mermaid is the oracle here: a case whose schema Marshal refuses fails, and
// Mermaid's reading of the diagram the generator emitted is compared all the
// same.
func TestMarshal_MermaidReadsWhatTheEmitterMeant(t *testing.T) {
	requireMermaidPackage(t)

	var cases []*diagramCase
	cases = append(cases, goldenCases(t)...)
	cases = append(cases, fixtureCases(t)...)

	var batch []string
	for _, c := range cases {
		c.fences = mermaidFences(c.doc)
		c.first = len(batch)
		batch = append(batch, c.fences...)
	}
	out := runMermaid(t, batch)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.marshalErr != nil {
				t.Errorf("Marshal refused the schema: %v", c.marshalErr)
			}
			if c.drift {
				t.Errorf("the golden is not the document Marshal generates from its schema with its options; Mermaid parses the generated one")
			}
			if len(c.fences) != c.wantFences {
				t.Fatalf("document holds %d mermaid fences, the emitter wrote %d", len(c.fences), c.wantFences)
			}
			for i := range c.fences {
				for _, v := range out.Versions {
					got := v.Results[c.first+i]
					t.Run("mermaid@"+v.Version, func(t *testing.T) {
						t.Logf("Mermaid %s reads: %s", v.Version, describeReading(got))
						assertReading(t, got, c.meant, c.fences[i])
					})
				}
			}
		})
	}
}

// requireMermaidPackage fails the test when the npm package is not installed.
// The build tag is an explicit request for this test, so a missing tool is a
// failure, never a skip.
func requireMermaidPackage(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Fatalf("node is not on PATH: %v", err)
	}
	for _, pkg := range []string{"mermaid10", "mermaid11", "mermaid12", "jsdom"} {
		if _, err := os.Stat(filepath.Join(mermaidDir, "node_modules", pkg, "package.json")); err != nil {
			t.Fatalf("%s is not installed; run `npm ci` in adapter/markdown/%s: %v", pkg, filepath.ToSlash(mermaidDir), err)
		}
	}
}

// goldenCases returns one case per .md.golden under testdata, outside the
// mermaid directory. The document is a fresh Marshal of the golden's schema
// with its options; a golden that differs from it fails its case, so a passing
// case has parsed the golden's bytes.
func goldenCases(t *testing.T) []*diagramCase {
	t.Helper()
	var names []string
	err := filepath.WalkDir("testdata", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path == mermaidDir {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md.golden") {
			rel, err := filepath.Rel("testdata", path)
			if err != nil {
				return err
			}
			names = append(names, filepath.ToSlash(strings.TrimSuffix(rel, ".md.golden")))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking testdata: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("no golden under testdata")
	}
	var cases []*diagramCase
	for _, name := range names {
		golden, err := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(name)+".md.golden"))
		if err != nil {
			t.Fatalf("reading golden %s: %v", name, err)
		}
		opts := goldenOptions[name]
		s := loadTestdata(t, name)
		c := newDiagramCase("golden/"+name, s, opts)
		c.drift = !bytes.Equal(c.doc, golden)
		cases = append(cases, c)
	}
	return cases
}

// fixtureCases returns one case per schema under testdata/mermaid/fixtures: each
// .yammm file directly in it, and each main.yammm one directory below it, with
// the schemas it imports beside it or under it.
func fixtureCases(t *testing.T) []*diagramCase {
	t.Helper()
	root := filepath.Join(mermaidDir, "fixtures")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading %s: %v", root, err)
	}
	var cases []*diagramCase
	for _, e := range entries {
		var entry string
		switch {
		case e.IsDir():
			entry = filepath.Join(root, e.Name(), "main.yammm")
		case strings.HasSuffix(e.Name(), ".yammm"):
			entry = filepath.Join(root, e.Name())
		default:
			continue
		}
		rel, err := filepath.Rel("testdata", entry)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.ToSlash(strings.TrimSuffix(rel, ".yammm"))
		cases = append(cases, newDiagramCase("fixture/"+strings.TrimSuffix(e.Name(), ".yammm"), loadTestdata(t, name), nil))
	}
	if len(cases) == 0 {
		t.Fatalf("no fixture under %s", root)
	}
	return cases
}

// newDiagramCase generates the document for s. When Marshal refuses, the case
// keeps the error and the document the generator emitted before its
// self-check, so Mermaid's reading of the refused diagram is still measured.
func newDiagramCase(name string, s *schema.Schema, opts []Option) *diagramCase {
	cfg := config{classDiagram: true, classMembers: true}
	for _, o := range opts {
		o(&cfg)
	}
	c := &diagramCase{name: name, meant: meaningOf(s)}
	if cfg.classDiagram {
		c.wantFences = 1
	}
	doc, err := Marshal(s, opts...)
	if err != nil {
		g := newGenerator(s, cfg)
		g.emitDocument()
		doc = slices.Clone(g.buf.Bytes())
		c.marshalErr = err
	}
	c.doc = doc
	return c
}

// meaningOf derives, from the schema alone, what the class diagram says: one
// class per type in the import closure, labelled with the name the document
// gives the type; one inheritance relation from each resolved parent to its
// child; and one association or composition per relation a type declares,
// labelled NAME (multiplicity). Class ids follow the documented rule: the
// display name with every character outside ASCII letters, digits and
// underscores made an underscore, every "direction" written "direc_tion", and
// a later clash suffixed _2, _3, ….
func meaningOf(s *schema.Schema) diagramMeaning {
	m := diagramMeaning{direction: "TB"}
	ids := map[schema.TypeID]string{}
	taken := map[string]bool{}
	for _, sch := range s.Closure() {
		for _, t := range sch.TypesSlice() {
			label := displayOf(s, sch, t)
			base := strings.ReplaceAll(strings.Map(func(r rune) rune {
				if r == '_' || 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' {
					return r
				}
				return '_'
			}, label), "direction", "direc_tion")
			id := base
			for n := 2; taken[id]; n++ {
				id = base + "_" + strconv.Itoa(n)
			}
			taken[id] = true
			ids[t.ID()] = id
			m.classes = append(m.classes, mermaidClass{ID: id, Label: label})
		}
	}
	for _, sch := range s.Closure() {
		for _, t := range sch.TypesSlice() {
			child := ids[t.ID()]
			for _, ref := range t.InheritsSlice() {
				if parent, ok := sch.ResolveType(ref); ok {
					if pid, ok := ids[parent.ID()]; ok {
						m.relations = append(m.relations, mermaidRelation{From: pid, To: child, Kind: "inheritance"})
					}
				}
			}
			for kind, rels := range map[string][]*schema.Relation{
				"association": t.AssociationsSlice(),
				"composition": t.CompositionsSlice(),
			} {
				for _, rel := range rels {
					if target, ok := ids[rel.TargetID()]; ok {
						m.relations = append(m.relations, mermaidRelation{
							From: child, To: target, Kind: kind,
							Label: rel.Name() + " (" + dslMultiplicity(rel) + ")",
						})
					}
				}
			}
		}
	}
	return m
}

// displayOf is the name the document gives t: the entry schema's tag for a
// type it can address, else "Name (schema)", with each control character of
// the schema name written as its Go escape.
func displayOf(entry, sch *schema.Schema, t *schema.Type) string {
	if tag, ok := schema.AddressableTag(entry, t.ID()); ok {
		return tag
	}
	var b strings.Builder
	for _, r := range sch.Name() {
		if unicode.IsControl(r) {
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
			continue
		}
		b.WriteRune(r)
	}
	return t.Name() + " (" + b.String() + ")"
}

// dslMultiplicity is the DSL's canonical short multiplicity of a relation's
// forward direction.
func dslMultiplicity(rel *schema.Relation) string {
	switch {
	case rel.IsOptional() && rel.IsMany():
		return "many"
	case rel.IsMany():
		return "one:many"
	case rel.IsOptional():
		return "_"
	default:
		return "one"
	}
}

// mermaidFences returns the body of every fenced code block GitHub's Markdown
// reads as a mermaid block, nested blocks included, in document order.
func mermaidFences(doc []byte) []string {
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	root := md.Parser().Parse(text.NewReader(doc))
	var fences []string
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		f, ok := n.(*ast.FencedCodeBlock)
		if !entering || !ok || string(f.Language(doc)) != "mermaid" {
			return ast.WalkContinue, nil
		}
		var b strings.Builder
		for i := range f.Lines().Len() {
			line := f.Lines().At(i)
			b.Write(line.Value(doc))
		}
		fences = append(fences, b.String())
		return ast.WalkSkipChildren, nil
	})
	return fences
}

// runMermaid parses every diagram text with each pinned Mermaid release in one
// node process.
func runMermaid(t *testing.T, diagrams []string) mermaidOutput {
	t.Helper()
	in, err := json.Marshal(map[string][]string{"diagrams": diagrams})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "node", "parse.mjs")
	cmd.Dir = mermaidDir
	cmd.Stdin = bytes.NewReader(in)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("node parse.mjs: %v\n%s", err, stderr.String())
	}
	var out mermaidOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("reading node parse.mjs output: %v\n%s", err, raw)
	}
	if len(out.Versions) != 3 {
		t.Fatalf("node parse.mjs reported %d Mermaid releases, want 3", len(out.Versions))
	}
	for _, v := range out.Versions {
		if len(v.Results) != len(diagrams) {
			t.Fatalf("Mermaid %s returned %d readings for %d diagrams", v.Version, len(v.Results), len(diagrams))
		}
	}
	return out
}

// assertReading compares one Mermaid reading with the meaning.
func assertReading(t *testing.T, got mermaidReading, meant diagramMeaning, fence string) {
	t.Helper()
	if got.Error != "" {
		t.Fatalf("Mermaid fails to parse the diagram: %s\n%s", got.Error, fence)
	}
	if got.Type != "classDiagram" {
		t.Errorf("Mermaid reads a %q diagram, want classDiagram", got.Type)
	}
	if got.Direction != meant.direction {
		t.Errorf("Mermaid reads direction %q, the emitter meant %q", got.Direction, meant.direction)
	}
	classes := make([]mermaidClass, len(got.Classes))
	for i, c := range got.Classes {
		classes[i] = c.mermaidClass
	}
	relations := make([]mermaidRelation, len(got.Relations))
	for i, r := range got.Relations {
		relations[i] = r.mermaidRelation
	}
	reportDiff(t, "class", meant.classes, classes, compareClass)
	reportDiff(t, "relation", meant.relations, relations, compareRelation)
	if t.Failed() {
		t.Logf("diagram:\n%s", fence)
	}
}

func compareClass(a, b mermaidClass) int {
	return cmp.Or(cmp.Compare(a.ID, b.ID), cmp.Compare(a.Label, b.Label))
}

func compareRelation(a, b mermaidRelation) int {
	return cmp.Or(cmp.Compare(a.From, b.From), cmp.Compare(a.To, b.To), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Label, b.Label))
}

// reportDiff compares two multisets and names each element one holds more
// often than the other.
func reportDiff[T any](t *testing.T, what string, meant, got []T, compare func(a, b T) int) {
	t.Helper()
	meant, got = slices.Clone(meant), slices.Clone(got)
	slices.SortFunc(meant, compare)
	slices.SortFunc(got, compare)
	i, j := 0, 0
	for i < len(meant) || j < len(got) {
		switch {
		case j == len(got) || i < len(meant) && compare(meant[i], got[j]) < 0:
			t.Errorf("Mermaid does not read the %s the emitter meant: %+v", what, meant[i])
			i++
		case i == len(meant) || compare(meant[i], got[j]) > 0:
			t.Errorf("Mermaid reads a %s the emitter did not mean: %+v", what, got[j])
			j++
		default:
			i++
			j++
		}
	}
}

// describeReading summarizes a reading for the verbose log.
func describeReading(r mermaidReading) string {
	if r.Error != "" {
		return "parse error: " + r.Error
	}
	var b strings.Builder
	fmt.Fprintf(&b, "direction %s; %d classes", r.Direction, len(r.Classes))
	for _, c := range r.Classes {
		fmt.Fprintf(&b, " [%s %q]", c.ID, c.Label)
	}
	fmt.Fprintf(&b, "; %d relations", len(r.Relations))
	for _, rel := range r.Relations {
		fmt.Fprintf(&b, " [%s %s %s %q]", rel.From, rel.Arrow, rel.To, rel.Label)
	}
	return b.String()
}
