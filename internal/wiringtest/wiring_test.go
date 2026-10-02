package wiringtest

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"

	"github.com/simon-lentz/yammm/internal/gittree"
)

const testWorkflow = "./.github/workflows/yammm_test.yml"

type workflow struct {
	On       map[string]*trigger `yaml:"on"`
	Env      map[string]string   `yaml:"env"`
	Defaults runDefaults         `yaml:"defaults"`
	Jobs     map[string]job      `yaml:"jobs"`
}

// runDefaults is a workflow's or a job's defaults block: a shell and a working
// directory for each run step that sets none of its own, a job's overriding a
// workflow's.
type runDefaults struct {
	Run struct {
		Shell            string `yaml:"shell"`
		WorkingDirectory string `yaml:"working-directory"`
	} `yaml:"run"`
}

type trigger struct {
	Branches       []string `yaml:"branches"`
	BranchesIgnore []string `yaml:"branches-ignore"`
	Paths          []string `yaml:"paths"`
	PathsIgnore    []string `yaml:"paths-ignore"`
}

type job struct {
	RunsOn          runnerLabels `yaml:"runs-on"`
	Uses            string       `yaml:"uses"`
	Needs           stringList   `yaml:"needs"`
	If              string       `yaml:"if"`
	ContinueOnError any          `yaml:"continue-on-error"`
	TimeoutMinutes  int          `yaml:"timeout-minutes"`
	Strategy        struct {
		FailFast *bool  `yaml:"fail-fast"`
		Matrix   matrix `yaml:"matrix"`
	} `yaml:"strategy"`
	Env      map[string]string `yaml:"env"`
	Defaults runDefaults       `yaml:"defaults"`
	Steps    []step            `yaml:"steps"`
	// keys holds every key the job's mapping sets, those no field names too.
	keys []string
	// runnerGroup is the group a mapping runs-on names, which RunsOn drops.
	runnerGroup string
}

func (j *job) UnmarshalYAML(n *yaml.Node) error {
	type plain job
	if err := n.Decode((*plain)(j)); err != nil {
		return err
	}
	j.keys = mappingKeys(n)
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == "runs-on" && n.Content[i+1].Kind == yaml.MappingNode {
			var on struct {
				Group string `yaml:"group"`
			}
			if err := n.Content[i+1].Decode(&on); err != nil {
				return err
			}
			j.runnerGroup = on.Group
		}
	}
	return nil
}

type step struct {
	Name            string            `yaml:"name"`
	Uses            string            `yaml:"uses"`
	Run             string            `yaml:"run"`
	Shell           string            `yaml:"shell"`
	WorkDir         string            `yaml:"working-directory"`
	With            map[string]any    `yaml:"with"`
	Env             map[string]string `yaml:"env"`
	If              string            `yaml:"if"`
	ContinueOnError any               `yaml:"continue-on-error"`
	TimeoutMinutes  int               `yaml:"timeout-minutes"`
	// keys holds every key the step's mapping sets, those no field names too.
	keys []string
}

func (s *step) UnmarshalYAML(n *yaml.Node) error {
	type plain step
	if err := n.Decode((*plain)(s)); err != nil {
		return err
	}
	s.keys = mappingKeys(n)
	return nil
}

// mappingKeys returns the keys of n when it is a mapping, in order.
func mappingKeys(n *yaml.Node) []string {
	if n.Kind != yaml.MappingNode {
		return nil
	}
	keys := make([]string, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		keys = append(keys, n.Content[i].Value)
	}
	return keys
}

// stringList decodes a workflow key GitHub accepts as one string or a list.
type stringList []string

func (s *stringList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*s = []string{n.Value}
		return nil
	}
	var list []string
	if err := n.Decode(&list); err != nil {
		return err
	}
	*s = list
	return nil
}

// runnerLabels decodes runs-on, which GitHub accepts as one label, a list of
// labels, or a mapping whose labels key holds them.
type runnerLabels []string

func (r *runnerLabels) UnmarshalYAML(n *yaml.Node) error {
	var labels stringList
	if n.Kind == yaml.MappingNode {
		var m struct {
			Labels stringList `yaml:"labels"`
		}
		if err := n.Decode(&m); err != nil {
			return err
		}
		labels = m.Labels
	} else if err := labels.UnmarshalYAML(n); err != nil {
		return err
	}
	*r = runnerLabels(labels)
	return nil
}

// matrix is a job's strategy.matrix. Dynamic marks one that GitHub fills from
// an expression at run time, whose configurations cannot be known here.
type matrix struct {
	Axes    map[string][]string
	Include []map[string]string
	Exclude []map[string]string
	Dynamic bool
}

func (m *matrix) UnmarshalYAML(n *yaml.Node) error {
	var raw any
	if err := n.Decode(&raw); err != nil {
		return err
	}
	fields, ok := raw.(map[string]any)
	if !ok {
		m.Dynamic = true
		return nil
	}
	for key, value := range fields {
		if value == nil {
			continue
		}
		items, ok := value.([]any)
		if !ok {
			m.Dynamic = true
			continue
		}
		if key != "include" && key != "exclude" {
			if m.Axes == nil {
				m.Axes = map[string][]string{}
			}
			for _, item := range items {
				m.Axes[key] = append(m.Axes[key], fmt.Sprint(item))
			}
			continue
		}
		for _, item := range items {
			fields, ok := item.(map[string]any)
			if !ok {
				m.Dynamic = true
				continue
			}
			entry := make(map[string]string, len(fields))
			for k, v := range fields {
				entry[k] = fmt.Sprint(v)
			}
			if key == "include" {
				m.Include = append(m.Include, entry)
			} else {
				m.Exclude = append(m.Exclude, entry)
			}
		}
	}
	return nil
}

// combinations returns the configurations GitHub runs m as, by its documented
// rules: the product of the axes less each one an exclude entry partly matches,
// then each include entry merged into every such original whose axis values it
// keeps, or run on its own when it keeps none. A dynamic matrix returns none.
func (m matrix) combinations() []map[string]string {
	if m.Dynamic {
		return nil
	}
	var originals []map[string]string
	if len(m.Axes) > 0 {
		originals = []map[string]string{{}}
		for _, key := range slices.Sorted(maps.Keys(m.Axes)) {
			var next []map[string]string
			for _, c := range originals {
				for _, v := range m.Axes[key] {
					n := maps.Clone(c)
					n[key] = v
					next = append(next, n)
				}
			}
			originals = next
		}
		originals = slices.DeleteFunc(originals, func(c map[string]string) bool {
			return slices.ContainsFunc(m.Exclude, func(e map[string]string) bool {
				// GitHub refuses an exclude key that names no axis, so c holds each.
				for k, v := range e {
					if c[k] != v {
						return false
					}
				}
				return true
			})
		})
	}
	combos := make([]map[string]string, 0, len(originals)+len(m.Include))
	for _, c := range originals {
		combos = append(combos, maps.Clone(c))
	}
	for _, e := range m.Include {
		merged := false
		for i, c := range originals {
			if keepsAxisValues(e, c) {
				maps.Copy(combos[i], e)
				merged = true
			}
		}
		if !merged {
			combos = append(combos, maps.Clone(e))
		}
	}
	return combos
}

// keepsAxisValues reports whether the include entry e names no key of the
// original configuration axes with another value. An added value can be
// overwritten; an original one cannot.
func keepsAxisValues(e, axes map[string]string) bool {
	for k, v := range e {
		if av, ok := axes[k]; ok && av != v {
			return false
		}
	}
	return true
}

func decodeYAML(t *testing.T, path string, into any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(b, into); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func TestTestWorkflow_RunsOnEveryBranchAndWhenCalled(t *testing.T) {
	t.Parallel()
	var wf workflow
	decodeYAML(t, fromRoot(".github/workflows/yammm_test.yml"), &wf)

	for _, event := range []string{"push", "pull_request"} {
		tr := wf.On[event]
		if tr == nil {
			t.Errorf("on.%s is not set", event)
			continue
		}
		if !slices.Contains(tr.Branches, "**") || len(tr.BranchesIgnore) > 0 {
			t.Errorf("on.%s filters branches %q, ignoring %q: only `**` reaches a branch whose name holds a slash", event, tr.Branches, tr.BranchesIgnore)
		}
		if len(tr.Paths) > 0 || len(tr.PathsIgnore) > 0 {
			t.Errorf("on.%s filters paths %q, ignoring %q: a change outside them runs no suite", event, tr.Paths, tr.PathsIgnore)
		}
	}
	if _, ok := wf.On["workflow_call"]; !ok {
		t.Error("on.workflow_call is not set, so the release workflow cannot call the tests")
	}
}

// workflowFormFile is the test workflow less its comments, the form the
// workflow is held to.
const workflowFormFile = "testdata/yammm_test.yaml"

// parseOneDocument parses text as one YAML document and returns its root
// value. yaml.Unmarshal reads the first of several documents and drops the
// rest, where GitHub refuses the file.
func parseOneDocument(text string) (*yaml.Node, error) {
	dec := yaml.NewDecoder(strings.NewReader(text))
	var doc, next yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	if err := dec.Decode(&next); !errors.Is(err, io.EOF) {
		return nil, errors.New("it holds more than one YAML document")
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, errors.New("it holds no single value")
	}
	return doc.Content[0], nil
}

// oneDocument returns the root value of the one YAML document the file at
// path holds.
func oneDocument(t *testing.T, path string) *yaml.Node {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parseOneDocument(string(b))
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return doc
}

func TestParseOneDocument_RefusesAllButOneDocument(t *testing.T) {
	t.Parallel()
	if doc, err := parseOneDocument("on: push\n"); err != nil || doc.Kind != yaml.MappingNode {
		t.Fatalf("one document decodes to %v, error %v", doc, err)
	}
	for _, tt := range []struct{ name, text, want string }{
		{"a second document", "on: push\n---\njobs: {}\n", "more than one YAML document"},
		{"a second document that is empty", "on: push\n---\n", "more than one YAML document"},
		{"no document", "", "EOF"},
		{"text that is no YAML", "on: [push\n", "yaml:"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if doc, err := parseOneDocument(tt.text); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("parseOneDocument = %v, error %v, want an error holding %q", doc, err, tt.want)
			}
		})
	}
}

// describe spells a YAML node for a finding: a scalar by its tag and its text
// (a plain scalar's spelling, a quoted or block scalar's content), a mapping, a
// list or an alias by its kind, and any other node as no value.
func describe(n *yaml.Node) string {
	switch n.Kind {
	case yaml.ScalarNode:
		return n.ShortTag() + " " + strconv.Quote(n.Value)
	case yaml.MappingNode:
		return "a mapping"
	case yaml.SequenceNode:
		return "a list"
	case yaml.AliasNode:
		return "an alias"
	}
	return "no value"
}

// entry returns the value m holds under key, or nil when m is no mapping or
// holds no such key.
func entry(m *yaml.Node, key string) *yaml.Node {
	if m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// nodeFindings reports each place where the YAML value got departs from want:
// a value of another kind, a scalar of another tag or text, a key one side
// lacks or got sets twice, a list of another length, and a value that is an
// alias or carries an anchor or a written tag other than "!". A scalar is
// compared by its tag and its text, not by the value it decodes to, so 024 is
// not 20: GitHub reads some spellings as another value than this decoder does.
// Key order, quoting, block style and a key's own anchor or tag are not
// compared. path names the place, a list's item by its index and want's name.
func nodeFindings(path string, got, want *yaml.Node) []string {
	if got.Kind == yaml.AliasNode || got.Anchor != "" || got.Style&yaml.TaggedStyle != 0 {
		return []string{path + " is an alias, or carries an anchor or a tag, which the form does not hold"}
	}
	if got.Kind != want.Kind {
		return []string{fmt.Sprintf("%s is %s, want %s", path, describe(got), describe(want))}
	}
	var findings []string
	switch want.Kind {
	case yaml.ScalarNode:
		if got.ShortTag() != want.ShortTag() || got.Value != want.Value {
			findings = append(findings, fmt.Sprintf("%s is %s, want %s", path, describe(got), describe(want)))
		}
	case yaml.SequenceNode:
		if len(got.Content) != len(want.Content) {
			return []string{fmt.Sprintf("%s holds %d items, want %d", path, len(got.Content), len(want.Content))}
		}
		for i, w := range want.Content {
			at := fmt.Sprintf("%s[%d]", path, i)
			if name := entry(w, "name"); name != nil {
				at += " (" + name.Value + ")"
			}
			findings = append(findings, nodeFindings(at, got.Content[i], w)...)
		}
	case yaml.MappingNode:
		under := func(key string) string {
			if path == "" {
				return key
			}
			return path + "." + key
		}
		seen := map[string]bool{}
		for i := 0; i+1 < len(got.Content); i += 2 {
			key := got.Content[i].Value
			if seen[key] {
				findings = append(findings, under(key)+" is set twice")
			}
			seen[key] = true
		}
		for i := 0; i+1 < len(want.Content); i += 2 {
			key := want.Content[i].Value
			if v := entry(got, key); v != nil {
				findings = append(findings, nodeFindings(under(key), v, want.Content[i+1])...)
			} else {
				findings = append(findings, fmt.Sprintf("%s is not set, want %s", under(key), describe(want.Content[i+1])))
			}
		}
		for i := 0; i+1 < len(got.Content); i += 2 {
			if key := got.Content[i].Value; entry(want, key) == nil {
				findings = append(findings, fmt.Sprintf("%s is set to %s, which the form does not hold", under(key), describe(got.Content[i+1])))
			}
		}
	}
	return findings
}

// The test workflow holds its form: the keys, values, tags and scalar text of
// testdata/yammm_test.yaml, with key order, quoting, layout and comments aside.
// So a change to what the workflow holds is a change to that file too.
func TestTestWorkflow_IsItsKnownForm(t *testing.T) {
	t.Parallel()
	got := oneDocument(t, fromRoot(".github/workflows/yammm_test.yml"))
	for _, f := range nodeFindings("", got, oneDocument(t, workflowFormFile)) {
		t.Error(f)
	}
}

// The comparison itself, against each kind of edit to the form.
func TestNodeFindings_RefusesEachDeparture(t *testing.T) {
	t.Parallel()
	scalar := func(tag, value string) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
	}
	// in follows keys from doc: a mapping's key, a list item's name, or its index.
	in := func(doc *yaml.Node, keys ...string) *yaml.Node {
		t.Helper()
		n := doc
		for _, key := range keys {
			var next *yaml.Node
			if n.Kind == yaml.SequenceNode {
				if i, err := strconv.Atoi(key); err == nil && i < len(n.Content) {
					next = n.Content[i]
				}
				for _, item := range n.Content {
					if name := entry(item, "name"); name != nil && name.Value == key {
						next = item
					}
				}
			} else {
				next = entry(n, key)
			}
			if next == nil {
				t.Fatalf("the form holds nothing at %q", keys)
			}
			n = next
		}
		return n
	}
	put := func(m *yaml.Node, key string, v *yaml.Node) {
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value == key {
				m.Content[i+1] = v
				return
			}
		}
		m.Content = append(m.Content, scalar("!!str", key), v)
	}
	drop := func(m *yaml.Node, key string) {
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value == key {
				m.Content = slices.Delete(m.Content, i, i+2)
				return
			}
		}
		t.Fatalf("the form holds no key %s to drop", key)
	}
	form := func() *yaml.Node { return oneDocument(t, workflowFormFile) }
	if f := nodeFindings("", form(), form()); len(f) != 0 {
		t.Fatalf("the form against itself reports %q", f)
	}
	for _, tt := range []struct {
		name   string
		want   []string // the findings, in order
		change func(doc *yaml.Node)
	}{
		{"every host on one image", []string{`jobs.check.runs-on is !!str "ubuntu-24.04", want !!str "${{ matrix.os }}"`}, func(doc *yaml.Node) {
			put(in(doc, "jobs", "check"), "runs-on", scalar("!!str", "ubuntu-24.04"))
		}},
		{"a host gone from the matrix", []string{"jobs.test.strategy.matrix.include holds 2 items, want 3"}, func(doc *yaml.Node) {
			include := in(doc, "jobs", "test", "strategy", "matrix", "include")
			include.Content = include.Content[:2]
		}},
		{"a key in a matrix entry", []string{`jobs.test.strategy.matrix.include[0] (Linux).experimental is set to !!bool "true", which the form does not hold`}, func(doc *yaml.Node) {
			put(in(doc, "jobs", "test", "strategy", "matrix", "include", "Linux"), "experimental", scalar("!!bool", "true"))
		}},
		{"a matrix GitHub fills at run time", []string{`jobs.test.strategy.matrix.include is !!str "${{ fromJSON(needs.x.outputs.hosts) }}", want a list`}, func(doc *yaml.Node) {
			put(in(doc, "jobs", "test", "strategy", "matrix"), "include", scalar("!!str", "${{ fromJSON(needs.x.outputs.hosts) }}"))
		}},
		{"a strategy that is no mapping", []string{`jobs.test.strategy is !!str "none", want a mapping`}, func(doc *yaml.Node) {
			put(in(doc, "jobs", "test"), "strategy", scalar("!!str", "none"))
		}},
		{"a run that is a list", []string{`jobs.check.steps[9] (Vet).run is a list, want !!str "scripts/vet.sh --host"`}, func(doc *yaml.Node) {
			put(in(doc, "jobs", "check", "steps", "Vet"), "run", &yaml.Node{Kind: yaml.SequenceNode})
		}},
		{"a timeout GitHub reads as 24", []string{`jobs.check.timeout-minutes is !!int "024", want !!int "20"`}, func(doc *yaml.Node) {
			put(in(doc, "jobs", "check"), "timeout-minutes", scalar("!!int", "024"))
		}},
		{"a string where the form holds false", []string{`(Set up Go).with.cache is !!str "false", want !!bool "false"`}, func(doc *yaml.Node) {
			put(in(doc, "jobs", "test", "steps", "Set up Go", "with"), "cache", scalar("!!str", "false"))
		}},
		{"nothing where the form holds false", []string{`(Set up Go).with.cache is !!null "", want !!bool "false"`}, func(doc *yaml.Node) {
			put(in(doc, "jobs", "check", "steps", "Set up Go", "with"), "cache", scalar("!!null", ""))
		}},
		{"a key that holds nothing", []string{`(Vet).continue-on-error is set to !!null "", which the form does not hold`}, func(doc *yaml.Node) {
			put(in(doc, "jobs", "check", "steps", "Vet"), "continue-on-error", scalar("!!null", ""))
		}},
		{"a key the form holds, gone", []string{`(golangci-lint).with.skip-cache is not set, want !!bool "true"`}, func(doc *yaml.Node) {
			drop(in(doc, "jobs", "check", "steps", "golangci-lint", "with"), "skip-cache")
		}},
		{"a mapping the form holds, gone", []string{"jobs.check.defaults is not set, want a mapping"}, func(doc *yaml.Node) {
			drop(in(doc, "jobs", "check"), "defaults")
		}},
		{"a key set twice", []string{"jobs.test.timeout-minutes is set twice"}, func(doc *yaml.Node) {
			job := in(doc, "jobs", "test")
			job.Content = append(job.Content, scalar("!!str", "timeout-minutes"), scalar("!!int", "30"))
		}},
		{"a job that waits for another", []string{`jobs.test.needs is set to !!str "check", which the form does not hold`}, func(doc *yaml.Node) {
			put(in(doc, "jobs", "test"), "needs", scalar("!!str", "check"))
		}},
		{"a step added", []string{"jobs.check.steps holds 12 items, want 11"}, func(doc *yaml.Node) {
			steps := in(doc, "jobs", "check", "steps")
			steps.Content = append(steps.Content, &yaml.Node{Kind: yaml.MappingNode})
		}},
		{"two steps in the other order", []string{
			`(Test).name is !!str "Keep the test durations", want !!str "Test"`, "(Test).env is not set, want a mapping", `(Test).run is not set, want !!str "scripts/test.sh"`,
			`(Test).uses is set to !!str "actions/upload-artifact@v7"`, "(Test).with is set to a mapping",
			`(Keep the test durations).name is !!str "Test"`, "(Keep the test durations).uses is not set", "(Keep the test durations).with is not set",
			"(Keep the test durations).env is set to a mapping", `(Keep the test durations).run is set to !!str "scripts/test.sh"`,
		}, func(doc *yaml.Node) {
			steps := in(doc, "jobs", "test", "steps").Content
			i := slices.Index(steps, in(doc, "jobs", "test", "steps", "Test"))
			steps[i], steps[i+1] = steps[i+1], steps[i]
		}},
		{"a suite that runs no test", []string{`(Test).env.GOFLAGS is set to !!str "-run=^$", which the form does not hold`}, func(doc *yaml.Node) {
			put(in(doc, "jobs", "test", "steps", "Test", "env"), "GOFLAGS", scalar("!!str", "-run=^$"))
		}},
		{"an entry of another key restored", []string{`(Restore this job's Go caches).with.restore-keys is set to !!str "go-", which the form does not hold`}, func(doc *yaml.Node) {
			put(in(doc, "jobs", "cross-vet", "steps", "Restore this job's Go caches", "with"), "restore-keys", scalar("!!str", "go-"))
		}},
		{"a save on every ref", []string{`(Save this job's Go caches).if is !!str "${{ steps.go-restore.outputs.cache-hit != 'true' }}", want !!str`}, func(doc *yaml.Node) {
			put(in(doc, "jobs", "test", "steps", "Save this job's Go caches"), "if", scalar("!!str", "${{ steps.go-restore.outputs.cache-hit != 'true' }}"))
		}},
		{"an alias", []string{"jobs.test.defaults is an alias, or carries an anchor or a tag, which the form does not hold"}, func(doc *yaml.Node) {
			put(in(doc, "jobs", "test"), "defaults", &yaml.Node{Kind: yaml.AliasNode, Value: "shell", Alias: in(doc, "jobs", "check", "defaults")})
		}},
		{"an anchor", []string{"jobs.check.defaults is an alias, or carries an anchor or a tag, which the form does not hold"}, func(doc *yaml.Node) {
			in(doc, "jobs", "check", "defaults").Anchor = "shell"
		}},
		{"a tag on a quoted scalar", []string{"(Set up Go).with.cache is an alias, or carries an anchor or a tag"}, func(doc *yaml.Node) {
			put(in(doc, "jobs", "check", "steps", "Set up Go", "with"), "cache", &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.TaggedStyle | yaml.DoubleQuotedStyle, Tag: "!!bool", Value: "false"})
		}},
		{"a merge key", []string{"jobs.integration.strategy.matrix.include[0].<< is set to a mapping, which the form does not hold"}, func(doc *yaml.Node) {
			put(in(doc, "jobs", "integration", "strategy", "matrix", "include", "0"), "<<", &yaml.Node{Kind: yaml.MappingNode})
		}},
		{"a branch whose push runs no suite", []string{"on.push.branches holds 2 items, want 1"}, func(doc *yaml.Node) {
			branches := in(doc, "on", "push", "branches")
			branches.Content = append(branches.Content, scalar("!!str", "!main"))
		}},
		{"a dispatch that takes an input", []string{"on.workflow_dispatch.inputs is set to a mapping, which the form does not hold"}, func(doc *yaml.Node) {
			put(in(doc, "on", "workflow_dispatch"), "inputs", &yaml.Node{Kind: yaml.MappingNode})
		}},
		{"a run cancelled by the next push", []string{"concurrency is set to a mapping, which the form does not hold"}, func(doc *yaml.Node) {
			put(doc, "concurrency", &yaml.Node{Kind: yaml.MappingNode})
		}},
		{"a longer timeout for the mermaid job", []string{`jobs.mermaid.timeout-minutes is !!int "360", want !!int "15"`}, func(doc *yaml.Node) {
			put(in(doc, "jobs", "mermaid"), "timeout-minutes", scalar("!!int", "360"))
		}},
		{"a job gone and a job added", []string{"jobs.cross-vet is not set, want a mapping", "jobs.extra is set to a mapping, which the form does not hold"}, func(doc *yaml.Node) {
			drop(in(doc, "jobs"), "cross-vet")
			put(in(doc, "jobs"), "extra", &yaml.Node{Kind: yaml.MappingNode})
		}},
		{"a key in another case", []string{"jobs is not set, want a mapping", "Jobs is set to a mapping, which the form does not hold"}, func(doc *yaml.Node) {
			jobs := in(doc, "jobs")
			drop(doc, "jobs")
			put(doc, "Jobs", jobs)
		}},
		{"an edit to each of two jobs", []string{`jobs.check.timeout-minutes is !!int "21", want !!int "20"`, `jobs.integration.timeout-minutes is !!int "31", want !!int "30"`}, func(doc *yaml.Node) {
			put(in(doc, "jobs", "integration"), "timeout-minutes", scalar("!!int", "31"))
			put(in(doc, "jobs", "check"), "timeout-minutes", scalar("!!int", "21"))
		}},
		{"a workflow that is a list", []string{"a workflow is a list, want a mapping"}, func(doc *yaml.Node) {
			*doc = yaml.Node{Kind: yaml.SequenceNode}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := form()
			tt.change(doc)
			path := ""
			if doc.Kind != yaml.MappingNode {
				path = "a workflow"
			}
			f := nodeFindings(path, doc, form())
			if len(f) != len(tt.want) {
				t.Fatalf("findings = %q, want %d", f, len(tt.want))
			}
			for i, want := range tt.want {
				if !strings.Contains(f[i], want) {
					t.Errorf("finding %d = %q, want it to hold %q", i, f[i], want)
				}
			}
		})
	}
}

// testHosts are the images the check job and the test job each run on.
var testHosts = []string{"macos-26", "ubuntu-24.04", "windows-2025"}

// isCacheSave reports whether s does nothing but save a cache entry.
func isCacheSave(s step) bool {
	return strings.HasPrefix(s.Uses, "actions/cache/save@")
}

// gateFindings reports each way wf lets a failure pass or leaves a host
// unchecked: a job that is conditional, waits for another or may fail; a step
// that may fail or carries a condition other than the one that runs it after
// an earlier failure (a cache save may carry any); and a check job or a test
// job that does not run on each of testHosts, its config check with no
// condition and its lint, vet or suite after an earlier step fails. The form holds these today, and this check holds them against a
// change made to the workflow and the form together.
func gateFindings(wf workflow) []string {
	var findings []string
	for _, name := range slices.Sorted(maps.Keys(wf.Jobs)) {
		j := wf.Jobs[name]
		if j.If != "" {
			findings = append(findings, fmt.Sprintf("jobs.%s runs only if %q", name, j.If))
		}
		if j.ContinueOnError != nil {
			findings = append(findings, fmt.Sprintf("jobs.%s sets continue-on-error %v, so its failure passes the run", name, j.ContinueOnError))
		}
		if len(j.Needs) > 0 {
			findings = append(findings, fmt.Sprintf("jobs.%s needs %q, so it is skipped when one of them fails", name, []string(j.Needs)))
		}
		for i, s := range j.Steps {
			if s.If != "" && s.If != "${{ !cancelled() }}" && !isCacheSave(s) {
				findings = append(findings, fmt.Sprintf("jobs.%s step %d (%s) runs only if %q", name, i+1, s.Name, s.If))
			}
			if s.ContinueOnError != nil {
				findings = append(findings, fmt.Sprintf("jobs.%s step %d (%s) sets continue-on-error %v, so its failure passes the job", name, i+1, s.Name, s.ContinueOnError))
			}
		}
	}
	// checks are the steps a host job must run, each with the condition it runs on.
	type check struct {
		what, wantIf string
		is           func(step) bool
	}
	ran := func(cmd string) func(step) bool {
		return func(s step) bool { return strings.TrimSpace(s.Run) == cmd }
	}
	hostJobs := map[string][]check{
		"check": {
			{"scripts/lintconfig.sh", "", ran("scripts/lintconfig.sh")},
			{"the linter", "${{ !cancelled() }}", func(s step) bool { return strings.HasPrefix(s.Uses, "golangci/golangci-lint-action@") }},
			{"scripts/vet.sh --host", "${{ !cancelled() }}", ran("scripts/vet.sh --host")},
		},
		"test": {{"scripts/test.sh", "${{ !cancelled() }}", ran("scripts/test.sh")}},
	}
	for _, name := range slices.Sorted(maps.Keys(hostJobs)) {
		j, ok := wf.Jobs[name]
		if !ok {
			findings = append(findings, fmt.Sprintf("jobs.%s is not set", name))
			continue
		}
		if ff := j.Strategy.FailFast; ff == nil || *ff {
			findings = append(findings, fmt.Sprintf("jobs.%s does not set fail-fast: false, so one host's failure cancels the other hosts' reports", name))
		}
		if !slices.Equal(j.RunsOn, []string{"${{ matrix.os }}"}) {
			findings = append(findings, fmt.Sprintf("jobs.%s runs on %q, want the matrix's os", name, []string(j.RunsOn)))
		}
		var hosts []string
		for _, c := range j.Strategy.Matrix.combinations() {
			hosts = append(hosts, c["os"])
		}
		slices.Sort(hosts)
		if !slices.Equal(hosts, testHosts) {
			findings = append(findings, fmt.Sprintf("jobs.%s's matrix holds the hosts %q, want %q", name, hosts, testHosts))
		}
		for _, c := range hostJobs[name] {
			i := slices.IndexFunc(j.Steps, c.is)
			if i < 0 {
				findings = append(findings, fmt.Sprintf("jobs.%s has no step that runs %s", name, c.what))
			} else if got := j.Steps[i].If; got != c.wantIf {
				findings = append(findings, fmt.Sprintf("jobs.%s runs %s if %q, want %q", name, c.what, got, c.wantIf))
			}
		}
	}
	return findings
}

// No job or step of the test workflow lets a failure pass, and each host runs
// every check: the release builds once this workflow has passed.
func TestTestWorkflow_HoldsTheGate(t *testing.T) {
	t.Parallel()
	var wf workflow
	decodeYAML(t, fromRoot(".github/workflows/yammm_test.yml"), &wf)
	for _, f := range gateFindings(wf) {
		t.Error(f)
	}
}

// The check itself, against each weakening of the form's jobs.
func TestGateFindings_RefusesEachWeakening(t *testing.T) {
	t.Parallel()
	form := func() workflow {
		var wf workflow
		decodeYAML(t, workflowFormFile, &wf)
		return wf
	}
	// edit applies change to the named job of the form and to its step that is.
	edit := func(name string, is func(step) bool, change func(j *job, i int)) func(wf workflow) {
		return func(wf workflow) {
			j := wf.Jobs[name]
			i := 0
			if is != nil {
				if i = slices.IndexFunc(j.Steps, is); i < 0 {
					t.Fatalf("jobs.%s of the form has no such step", name)
				}
			}
			change(&j, i)
			wf.Jobs[name] = j
		}
	}
	lint := func(s step) bool { return strings.HasPrefix(s.Uses, "golangci/golangci-lint-action@") }
	runs := func(cmd string) func(step) bool { return func(s step) bool { return s.Run == cmd } }
	on := true
	if f := gateFindings(form()); len(f) != 0 {
		t.Fatalf("the form reports %q", f)
	}
	for _, tt := range []struct {
		name   string
		want   []string // the findings, in order
		change func(wf workflow)
	}{
		{"a job switched off", []string{`jobs.check runs only if "false"`}, edit("check", nil, func(j *job, _ int) { j.If = "false" })},
		{"a job on one event", []string{`jobs.integration runs only if "github.event_name == 'push'"`}, edit("integration", nil, func(j *job, _ int) { j.If = "github.event_name == 'push'" })},
		{"a job whose failure passes", []string{"jobs.test sets continue-on-error true"}, edit("test", nil, func(j *job, _ int) { j.ContinueOnError = true })},
		{"a job that names the default", []string{"jobs.mermaid sets continue-on-error false"}, edit("mermaid", nil, func(j *job, _ int) { j.ContinueOnError = false })},
		{"a job that waits for another", []string{`jobs.test needs ["check"]`}, edit("test", nil, func(j *job, _ int) { j.Needs = stringList{"check"} })},
		{"a step switched off", []string{`jobs.cross-vet step 6 (Vet) runs only if "false"`}, edit("cross-vet", runs("scripts/vet.sh"), func(j *job, i int) { j.Steps[i].If = "false" })},
		{"a step only after success", []string{`jobs.integration step 1 (Checkout) runs only if "${{ success() }}"`}, edit("integration", nil, func(j *job, i int) { j.Steps[i].If = "${{ success() }}" })},
		{"a step whose failure passes", []string{"jobs.integration step 3 (Integration tests) sets continue-on-error true"}, edit("integration", nil, func(j *job, _ int) { j.Steps[2].ContinueOnError = true })},
		{"a condition on an action that only names a cache save", []string{`jobs.check step 11 (Save this job's Go caches) runs only if`}, edit("check", isCacheSave, func(j *job, i int) { j.Steps[i].Uses = "docker://example/actions/cache/save@v6" })},
		{"a step that names the default", []string{"jobs.check step 1 (Keep committed line endings) sets continue-on-error false"}, edit("check", nil, func(j *job, i int) { j.Steps[i].ContinueOnError = false })},
		{"fail-fast on", []string{"jobs.check does not set fail-fast: false"}, edit("check", nil, func(j *job, _ int) { j.Strategy.FailFast = &on })},
		{"fail-fast at its default", []string{"jobs.test does not set fail-fast: false"}, edit("test", nil, func(j *job, _ int) { j.Strategy.FailFast = nil })},
		{"every host on one image", []string{`jobs.check runs on ["ubuntu-24.04"], want the matrix's os`}, edit("check", nil, func(j *job, _ int) { j.RunsOn = runnerLabels{"ubuntu-24.04"} })},
		{"a host gone from the matrix", []string{`jobs.test's matrix holds the hosts ["ubuntu-24.04" "windows-2025"]`}, edit("test", nil, func(j *job, _ int) {
			j.Strategy.Matrix.Include = slices.DeleteFunc(j.Strategy.Matrix.Include, func(c map[string]string) bool { return c["os"] == "macos-26" })
		})},
		{"a host on another image", []string{`jobs.check's matrix holds the hosts ["macos-15" "ubuntu-24.04" "windows-2025"]`}, edit("check", nil, func(j *job, _ int) {
			for _, c := range j.Strategy.Matrix.Include {
				if c["os"] == "macos-26" {
					c["os"] = "macos-15"
				}
			}
		})},
		{"no config check", []string{"jobs.check has no step that runs scripts/lintconfig.sh"}, edit("check", runs("scripts/lintconfig.sh"), func(j *job, i int) { j.Steps[i].Run = "true" })},
		{"a config check on one event", []string{`jobs.check step 8 (Verify the linter config) runs only if "github.event_name == 'push'"`, `jobs.check runs scripts/lintconfig.sh if "github.event_name == 'push'", want ""`}, edit("check", runs("scripts/lintconfig.sh"), func(j *job, i int) { j.Steps[i].If = "github.event_name == 'push'" })},
		{"no lint", []string{"jobs.check has no step that runs the linter"}, edit("check", lint, func(j *job, i int) { j.Steps[i].Uses = "actions/checkout@v7" })},
		{"a lint that stops at an earlier failure", []string{`jobs.check runs the linter if "", want "${{ !cancelled() }}"`}, edit("check", lint, func(j *job, i int) { j.Steps[i].If = "" })},
		{"a vet whose failure passes", []string{"jobs.check has no step that runs scripts/vet.sh --host"}, edit("check", runs("scripts/vet.sh --host"), func(j *job, i int) { j.Steps[i].Run = "scripts/vet.sh --host || true" })},
		{"a vet that stops at an earlier failure", []string{`jobs.check runs scripts/vet.sh --host if "", want "${{ !cancelled() }}"`}, edit("check", runs("scripts/vet.sh --host"), func(j *job, i int) { j.Steps[i].If = "" })},
		{"no suite", []string{"jobs.test has no step that runs scripts/test.sh"}, edit("test", runs("scripts/test.sh"), func(j *job, i int) { j.Steps[i].Run = "scripts/test.sh || true" })},
		{"a suite that stops at an earlier failure", []string{`jobs.test runs scripts/test.sh if "", want "${{ !cancelled() }}"`}, edit("test", runs("scripts/test.sh"), func(j *job, i int) { j.Steps[i].If = "" })},
		{"no check job and no test job", []string{"jobs.check is not set", "jobs.test is not set"}, func(wf workflow) {
			delete(wf.Jobs, "check")
			delete(wf.Jobs, "test")
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wf := form()
			tt.change(wf)
			f := gateFindings(wf)
			if len(f) != len(tt.want) {
				t.Fatalf("findings = %q, want %d", f, len(tt.want))
			}
			for i, want := range tt.want {
				if !strings.Contains(f[i], want) {
					t.Errorf("finding %d = %q, want it to hold %q", i, f[i], want)
				}
			}
		})
	}
}

// vetCommands returns the run text of each step of j that starts with
// scripts/vet.sh, once per matrix configuration, or once as written when the
// check knows none: no matrix, or one GitHub fills at run time.
func vetCommands(j job) []string {
	var cmds []string
	for _, st := range j.Steps {
		if !strings.HasPrefix(st.Run, "scripts/vet.sh") {
			continue
		}
		for _, run := range perCombination(st.Run, j.Strategy.Matrix.combinations()) {
			cmds = append(cmds, strings.TrimSpace(run))
		}
	}
	return cmds
}

func TestVetCommands_OnePerConfiguration(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, yaml string
		want       []string
	}{
		{"no matrix", "steps:\n  - run: scripts/vet.sh\n", []string{"scripts/vet.sh"}},
		{"an axis", "strategy:\n  matrix:\n    os: [a, b]\nsteps:\n  - run: scripts/vet.sh --host\n", []string{"scripts/vet.sh --host", "scripts/vet.sh --host"}},
		{"a reference", "strategy:\n  matrix:\n    include:\n      - flag: --host\n      - flag: ''\nsteps:\n  - run: scripts/vet.sh ${{matrix.flag}}\n", []string{"scripts/vet.sh --host", "scripts/vet.sh"}},
		{"another step", "steps:\n  - run: make\n", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := vetCommands(decodeJob(t, c.yaml)); !slices.Equal(got, c.want) {
				t.Errorf("vet commands %q, want %q", got, c.want)
			}
		})
	}
}

// Every target scripts/vet.sh lists is vetted, in a job of its own. On a
// Linux host scripts/vet.sh makes twelve vet runs over eleven targets where
// --host makes two over one, which took the Vet step of a host job 255 to
// 382 s against the other hosts' under 30 s and made that job the longest of
// the workflow in two of three measured runs. One job runs it, because a
// second would pay the cost twice, and the release calls this workflow whole,
// so a tag still waits for it.
func TestTestWorkflow_VetsEveryTargetInAJobOfItsOwn(t *testing.T) {
	t.Parallel()
	var wf workflow
	decodeYAML(t, fromRoot(".github/workflows/yammm_test.yml"), &wf)

	var crossing, all []string
	for _, name := range slices.Sorted(maps.Keys(wf.Jobs)) {
		for _, cmd := range vetCommands(wf.Jobs[name]) {
			all = append(all, name+": "+cmd)
			if cmd == "scripts/vet.sh" {
				crossing = append(crossing, name)
			}
		}
	}
	if len(crossing) != 1 {
		t.Errorf("%d jobs vet every target (%q); the workflow's vet commands are %q", len(crossing), crossing, all)
	}
}

// jobMargins holds, per job of the test workflow, how far every go test
// timeout the job runs must stay below the job's own timeout: at least as long
// as the job takes to start its last test binary, so a hung binary prints its
// stack before the job is killed. Measured on CI, a test job ended at most
// 795 s in, with no cache entry to restore, and the integration job's test
// step started at most 23 s in. The mermaid job's test binary starts after npm
// ci, which took 37 s from an empty npm cache, and a build, which took under
// 6 s with a cold build cache. Each margin adds headroom to its measurement.
var jobMargins = map[string]time.Duration{
	"test":        15 * time.Minute,
	"integration": 5 * time.Minute,
	"mermaid":     5 * time.Minute,
}

// testlessJobs names, per job of the test workflow that runs no go test, what
// it runs instead. A job is declared here or carries a margin, never both and
// never neither: the check cannot tell a job that runs no test from one whose
// test command it failed to read, so the distinction is stated rather than
// inferred.
var testlessJobs = map[string]string{
	"check":     "it lints and vets one host's build",
	"cross-vet": "it vets every target scripts/vet.sh lists",
}

// goTestCall matches go test written in text: the mermaid check classifies a
// step by it, and goTestRuns holds every match outside a comment to a run it
// judges.
var goTestCall = regexp.MustCompile(`\bgo\s+test\b`)

// githubExpression matches a ${{ }} expression in a step's run text, which
// GitHub replaces before bash reads the text. githubStandIn replaces it for
// the check, and goTestRuns reads any word holding it as a value only a run
// knows, in quotes too.
var (
	githubExpression = regexp.MustCompile(`(?s)\$\{\{.*?\}\}`)
	githubStandIn    = "${GITHUB_EXPRESSION}"
)

// staticWord returns w's value as bash passes it to a command, quotes and
// escapes removed, and false when a run can change it: w holds an expansion,
// a glob (globs), or a tilde bash expands (tildes).
func staticWord(w *syntax.Word) (string, bool) {
	static := true
	syntax.Walk(w, func(n syntax.Node) bool {
		switch n.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp, *syntax.ProcSubst, *syntax.ExtGlob:
			static = false
		}
		return static
	})
	if !static {
		return "", false
	}
	if globs(w) || tildes(w) {
		return "", false
	}
	// With no ReadDir, Fields removes quotes and escapes as bash does and
	// globs nothing; a brace expansion yields more than one field. A nil
	// Config would share expand's package-level one, which each call writes,
	// so parallel tests take one each.
	fields, err := expand.Fields(&expand.Config{}, w)
	if err != nil || len(fields) != 1 {
		return "", false
	}
	return fields[0], true
}

// globs reports whether w holds a glob character outside quotes that no
// backslash escapes: a * or ?, or a [ that an unescaped ] outside quotes closes
// later in the word, in the same part or another.
func globs(w *syntax.Word) bool {
	open := false
	for _, p := range w.Parts {
		lit, ok := p.(*syntax.Lit)
		if !ok {
			continue
		}
		for i := 0; i < len(lit.Value); i++ {
			switch lit.Value[i] {
			case '\\':
				i++
			case '*', '?':
				return true
			case '[':
				open = true
			case ']':
				if open {
					return true
				}
			}
		}
	}
	return false
}

// tildes reports whether w holds a ~ bash may expand: one that opens the word,
// or one after an unescaped = or : outside quotes. Bash expands the second
// only in a word shaped like an assignment; tildes reads every such ~ as one.
func tildes(w *syntax.Word) bool {
	for i, p := range w.Parts {
		lit, ok := p.(*syntax.Lit)
		if !ok {
			continue
		}
		if i == 0 && strings.HasPrefix(lit.Value, "~") {
			return true
		}
		for j := 0; j+1 < len(lit.Value); j++ {
			switch lit.Value[j] {
			case '\\':
				j++
			case '=', ':':
				if lit.Value[j+1] == '~' {
					return true
				}
			}
		}
	}
	return false
}

// splits reports whether w holds a parameter, command or arithmetic expansion
// outside double quotes or a glob, whose fields a run decides, or a brace
// expansion. A quoted list such as "$@" is none of them, and goTestTimeouts
// reads it as one word.
func splits(w *syntax.Word) bool {
	if globs(w) || slices.ContainsFunc(w.Parts, func(p syntax.WordPart) bool {
		switch p.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp:
			return true
		}
		return false
	}) {
		return true
	}
	// SplitBraces rewrites the word it is given, so it takes a copy.
	c := &syntax.Word{Parts: make([]syntax.WordPart, len(w.Parts))}
	for i, p := range w.Parts {
		if lit, ok := p.(*syntax.Lit); ok {
			l := *lit
			p = &l
		}
		c.Parts[i] = p
	}
	return syntax.SplitBraces(c)
}

// goTestRun is one go test command a script runs: the command as written, and
// for each word after test, the word as written, its value as bash passes it
// when a run cannot change it, and whether a run splits it.
type goTestRun struct {
	text    string
	written []string
	values  []string
	fixed   []bool
	splits  []bool
}

// shellCommands are the commands that run a script given as an argument:
// eval its arguments, a shell its -c operand.
var shellCommands = map[string]bool{"eval": true, "sh": true, "bash": true, "dash": true, "zsh": true}

// goTestRuns parses text as bash and returns every go test command the text
// fixes, wherever bash runs one: a pipeline, a list, a subshell, a command or
// process substitution, a loop or a function body. A command runs go test when
// a word fixed to go, or to a path whose last element is go, after any words
// before it (env, timeout, xargs and the like), is followed by test, with go's
// -C flag between them. The check reads what the text fixes. Its findings are
// go test written anywhere outside a comment and outside a run it judges (a
// string, a here-document, a here-string, an argument to eval or a shell); a
// command word a run splits; a word only a run can know followed by test, or
// after go where the subcommand goes; and a script only a run can know handed
// to eval or a shell. A go test a run assembles some other way, such as through
// an alias, is outside the check.
func goTestRuns(text string) (runs []goTestRun, findings []string, err error) {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash), syntax.KeepComments(true)).Parse(strings.NewReader(text), "")
	if err != nil {
		return nil, nil, err
	}
	source := func(n syntax.Node) string {
		return strings.TrimSpace(text[n.Pos().Offset():n.End().Offset()])
	}
	// judged holds each call the check judged or reported, so a go test written
	// there draws no second finding.
	var judged, comments [][2]uint
	report := func(n syntax.Node, what string) {
		findings = append(findings, what+source(n))
		judged = append(judged, [2]uint{n.Pos().Offset(), n.End().Offset()})
	}
	syntax.Walk(f, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Comment:
			comments = append(comments, [2]uint{n.Pos().Offset(), n.End().Offset()})
		case *syntax.CallExpr:
			if len(n.Args) > 0 && splits(n.Args[0]) {
				report(n, "runs a command only a run can name: ")
				return true
			}
			vals := make([]string, len(n.Args))
			fixed := make([]bool, len(n.Args))
			for i, w := range n.Args {
				vals[i], fixed[i] = staticWord(w)
				if strings.Contains(source(w), githubStandIn) {
					fixed[i] = false
				}
			}
			for i := range n.Args {
				if fixed[i] && shellCommands[path.Base(vals[i])] {
					script := n.Args[i+1:]
					if vals[i] != "eval" {
						k := slices.IndexFunc(n.Args[i+1:], func(w *syntax.Word) bool { v, ok := staticWord(w); return ok && v == "-c" })
						if k < 0 || i+2+k >= len(n.Args) {
							break
						}
						script = n.Args[i+2+k : i+3+k]
					}
					if slices.ContainsFunc(script, func(w *syntax.Word) bool {
						_, ok := staticWord(w)
						return !ok || strings.Contains(source(w), githubStandIn)
					}) {
						report(n, "runs a script only a run can know: ")
					}
					break
				}
				j := i + 1
				if j < len(n.Args) && fixed[j] && (vals[j] == "-C" || vals[j] == "--C") {
					j += 2
				} else if j < len(n.Args) && fixed[j] && (strings.HasPrefix(vals[j], "-C=") || strings.HasPrefix(vals[j], "--C=")) {
					j++
				}
				isGo := fixed[i] && path.Base(vals[i]) == "go"
				isTest := j < len(n.Args) && fixed[j] && vals[j] == "test"
				switch {
				case isGo && j < len(n.Args) && !fixed[j]:
					report(n, "runs go with a subcommand only a run can name: ")
					return true
				case isGo && isTest:
					r := goTestRun{text: source(n), values: vals[j+1:], fixed: fixed[j+1:]}
					for _, w := range n.Args[j+1:] {
						r.written = append(r.written, source(w))
						r.splits = append(r.splits, splits(w))
					}
					runs = append(runs, r)
					judged = append(judged, [2]uint{n.Pos().Offset(), n.End().Offset()})
					return true
				case !fixed[i] && isTest:
					report(n, "runs a command only a run can name with test: ")
					return true
				}
			}
		}
		return true
	})
	within := func(spans [][2]uint, at uint) bool {
		return slices.ContainsFunc(spans, func(s [2]uint) bool { return s[0] <= at && at < s[1] })
	}
	for _, m := range goTestCall.FindAllStringIndex(text, -1) {
		at := uint(m[0])
		if within(comments, at) || within(judged, at) {
			continue
		}
		line := text[strings.LastIndexByte(text[:m[0]], '\n')+1:]
		if end := strings.IndexByte(line, '\n'); end >= 0 {
			line = line[:end]
		}
		findings = append(findings, "writes go test where it judges no run: "+strings.TrimSpace(line))
	}
	return runs, findings, nil
}

// goTestFlags are the flags go test reads, each with whether it takes a value,
// and goTestPassed those it also reads spelled with a test. prefix and hands
// the test binary, which reads them so spelled after -args. Both are the
// toolchain's: TestGoTestFlags_MatchTheToolchain reads them from cmd/go's
// source. A flag outside goTestFlags, written without =, may take the next
// word, so goTestTimeouts cannot place what follows it.
var (
	goTestFlags = map[string]bool{
		"C": true, "a": false, "artifacts": false, "asan": false, "asmflags": true, "bench": true,
		"benchmem": false, "benchtime": true, "blockprofile": true, "blockprofilerate": true,
		"buildmode": true, "buildvcs": false, "c": false, "compiler": true, "count": true, "cover": false,
		"covermode": true, "coverpkg": true, "coverprofile": true, "cpu": true, "cpuprofile": true,
		"debug-actiongraph": true, "debug-runtime-trace": true, "debug-trace": true, "exec": true,
		"failfast": false, "fullpath": false, "fuzz": true, "fuzzminimizetime": true, "fuzztime": true,
		"gccgoflags": true, "gcflags": true, "installsuffix": true, "json": false, "ldflags": true,
		"linkshared": false, "list": true, "memprofile": true, "memprofilerate": true, "mod": true,
		"modcacherw": false, "modfile": true, "msan": false, "mutexprofile": true,
		"mutexprofilefraction": true, "n": false, "o": true, "outputdir": true, "overlay": true, "p": true,
		"parallel": true, "pgo": true, "pkgdir": true, "race": false, "run": true, "short": false,
		"shuffle": true, "skip": true, "tags": true, "timeout": true, "toolexec": true, "trace": true,
		"trimpath": false, "v": false, "vet": true, "work": false, "x": false,
	}
	goTestPassed = []string{
		"artifacts", "bench", "benchmem", "benchtime", "blockprofile", "blockprofilerate", "count",
		"coverprofile", "cpu", "cpuprofile", "failfast", "fullpath", "fuzz", "fuzzminimizetime", "fuzztime",
		"list", "memprofile", "memprofilerate", "mutexprofile", "mutexprofilefraction", "outputdir",
		"parallel", "run", "short", "shuffle", "skip", "timeout", "trace", "v",
	}
)

// goTestTimeouts returns the timeout every go test command goTestRuns finds
// runs under, and its findings. It reads the words as go test does: its flags
// and packages until --, -args, or a word no flag takes after the package list
// closed (a flag after it, or an unknown flag with =, closes it). After -args
// the test binary reads its own test.-spelled flags until -- or a word that is
// no flag; after -- or such a word it reads none. The last -timeout go reads,
// or a later -test.timeout the binary reads, applies. A command with none, or
// with one that is not positive, which switches Go's timeout off, is a
// finding. So is a word it cannot place: one a run splits (splits), one only a
// run can know written to open with a dash, a -timeout value only a run can
// know, a flag that takes a value with no word after it, and a flag written
// without = that the reader of the moment does not know: for go, one outside
// goTestFlags or spelled test. outside goTestPassed; for the binary, one not
// spelled test. or outside goTestPassed. Any other word only a run can know, such as a quoted variable or
// array, is read as a plain word: a package while go reads its package list, a
// flag's value after a flag that takes one, and otherwise the end of every
// flag; a -timeout it supplies is outside the check.
func goTestTimeouts(where, text string) (timeouts []time.Duration, findings []string) {
	runs, found, err := goTestRuns(text)
	if err != nil {
		return nil, []string{fmt.Sprintf("%s does not parse as bash: %v", where, err)}
	}
	for _, f := range found {
		findings = append(findings, where+" "+f)
	}
	const (
		goReads = iota
		binaryReads
		noneReads
	)
	for _, r := range runs {
		last, set, readable := "", false, true
		mode, inPkgs, closed := goReads, false, false
		// word reads a word no flag takes: a package while go reads, the end of
		// every flag once the package list closed or the binary reads.
		word := func() {
			switch {
			case mode == binaryReads || mode == goReads && closed:
				mode = noneReads
			case mode == goReads:
				inPkgs = true
			}
		}
		for k := 0; k < len(r.values) && readable && mode != noneReads; k++ {
			if !r.fixed[k] {
				if r.splits[k] || strings.HasPrefix(strings.TrimLeft(r.written[k], `"'`), "-") {
					readable = false
				} else {
					word()
				}
				continue
			}
			v := r.values[k]
			if !strings.HasPrefix(v, "-") || v == "-" {
				word()
				continue
			}
			if v == "--" {
				mode = noneReads
				continue
			}
			if mode == goReads && (v == "-args" || v == "--args") {
				mode = binaryReads
				continue
			}
			name, value, hasValue := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(v, "-"), "-"), "=")
			short, spelled := strings.CutPrefix(name, "test.")
			takesValue, known := goTestFlags[short]
			switch {
			case mode == binaryReads && !spelled, spelled && !slices.Contains(goTestPassed, short):
				known = false
			case mode == goReads:
				if inPkgs {
					inPkgs, closed = false, true
				}
			}
			if !known {
				if !hasValue {
					readable = false
				} else if mode == goReads {
					closed = true
				}
				continue
			}
			if takesValue && !hasValue {
				if k+1 == len(r.values) || short == "timeout" && !r.fixed[k+1] {
					readable = false
					break
				}
				k++
				value = r.values[k]
			}
			if short == "timeout" {
				last, set = value, true
			}
		}
		switch d, err := time.ParseDuration(last); {
		case !readable:
			findings = append(findings, fmt.Sprintf("%s runs go test with a word the check cannot place: %s", where, r.text))
		case !set:
			findings = append(findings, fmt.Sprintf("%s runs go test with no -timeout: %s", where, r.text))
		case err != nil:
			findings = append(findings, fmt.Sprintf("%s: -timeout %q: %v", where, last, err))
		case d <= 0:
			findings = append(findings, fmt.Sprintf("%s runs go test with -timeout %s, which switches the timeout off", where, last))
		default:
			timeouts = append(timeouts, d)
		}
	}
	return timeouts, findings
}

// timeoutFindings reports every way wf's jobs fail to time out after the go
// test runs they hold, scripts/test.sh's text being script. A step's own
// timeout bounds its go test runs too, and is held to the same margin. A job
// testless names runs no go test, and is held to its timeout alone.
func timeoutFindings(wf workflow, script string, margins map[string]time.Duration, testless map[string]string) []string {
	if len(wf.Jobs) == 0 {
		return []string{"the workflow holds no job"}
	}
	var findings []string
	for _, name := range slices.Sorted(maps.Keys(wf.Jobs)) {
		j := wf.Jobs[name]
		margin, measured := margins[name]
		_, declared := testless[name]
		switch {
		case measured && declared:
			findings = append(findings, fmt.Sprintf("jobs.%s has a measured margin and is declared to run no go test", name))
			continue
		case !measured && !declared:
			findings = append(findings, fmt.Sprintf("jobs.%s has no measured margin", name))
			continue
		}
		if j.TimeoutMinutes <= 0 {
			findings = append(findings, fmt.Sprintf("jobs.%s sets no timeout-minutes", name))
			continue
		}
		ran := false
		for _, st := range j.Steps {
			// Every invocation yields a timeout or a finding, so either one
			// means the step runs go test.
			var timeouts []time.Duration
			var found []string
			if strings.Contains(st.Run, "scripts/test.sh") {
				d, f := goTestTimeouts("scripts/test.sh", script)
				timeouts, found = append(timeouts, d...), append(found, f...)
			}
			d, f := goTestTimeouts("jobs."+name, githubExpression.ReplaceAllLiteralString(st.Run, githubStandIn))
			timeouts, found = append(timeouts, d...), append(found, f...)
			if len(timeouts)+len(found) > 0 {
				ran = true
			}
			if declared {
				continue
			}
			findings = append(findings, found...)
			limits := map[string]int{"jobs." + name: j.TimeoutMinutes}
			if st.TimeoutMinutes > 0 {
				limits[fmt.Sprintf("jobs.%s step %q", name, st.Name)] = st.TimeoutMinutes
			}
			for where, minutes := range limits {
				limit := time.Duration(minutes) * time.Minute
				for _, d := range timeouts {
					if d+margin > limit {
						findings = append(findings, fmt.Sprintf("%s times out at %v, less than %v past a go test timeout of %v", where, limit, margin, d))
					}
				}
			}
		}
		switch {
		case declared && ran:
			findings = append(findings, fmt.Sprintf("jobs.%s is declared to run no go test and runs one", name))
		case !declared && !ran:
			findings = append(findings, fmt.Sprintf("jobs.%s runs no go test this check can read", name))
		}
	}
	return findings
}

// Every job of the test workflow carries a timeout, so a hung runner costs
// minutes rather than GitHub's six-hour default, and every go test it runs
// times out at least the job's measured margin earlier, so a hung test binary
// reports its own stack first.
func TestTestWorkflow_EveryJobTimesOutAfterItsTests(t *testing.T) {
	t.Parallel()
	var wf workflow
	decodeYAML(t, fromRoot(".github/workflows/yammm_test.yml"), &wf)
	script, err := os.ReadFile(fromRoot("scripts/test.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range timeoutFindings(wf, string(script), jobMargins, testlessJobs) {
		t.Error(f)
	}
}

// mermaidPkgDir is the npm package the mermaid job installs.
const mermaidPkgDir = "adapter/markdown/testdata/mermaid"

// mermaidInstall is the mermaid job's install command. npm ci runs a package's
// lifecycle scripts, which could write GITHUB_ENV, unless told not to; the
// lock-filed package needs none.
const mermaidInstall = "npm ci --ignore-scripts"

// mermaidRunner is the one runner the mermaid job may run on: linux/amd64,
// with cgo on because its image carries a C compiler. runnerBuild takes that
// build from the toolchain.
const mermaidRunner = "ubuntu-24.04"

// mermaidActions are the actions a step of the mermaid job may use, each with
// the inputs it may take and the value each must hold, "" admitting any.
var mermaidActions = map[string]map[string]string{
	"actions/checkout":   {},
	"actions/setup-go":   {"go-version-file": "go.mod"},
	"actions/setup-node": {"node-version": ""},
}

// mermaidJobFindings reports each link of the mermaid job's chain that does
// not hold: mermaidInstall installs the lock-filed package; adapter/markdown
// holds a test the mermaid build tag admits on the job's runner, and none that
// needs the tag and the runner does not build (tagged and unbuilt, from
// taggedTestFiles); one step runs every test of the package under that tag; the
// linter reads the tag; and git ignores the installed package. The job is held
// to one known form, key by key and step by step, since anything the check
// does not know could leave a tagged test unrun while CI stays green.
func mermaidJobFindings(wf workflow, tagged, unbuilt, lintTags []string, gitignore string) []string {
	j, ok := wf.Jobs["mermaid"]
	if !ok {
		return []string{"jobs.mermaid is not set"}
	}
	var findings []string
	if len(wf.Env) > 0 {
		findings = append(findings, "the workflow sets env, which reaches every step")
	}
	if wf.Defaults != (runDefaults{}) {
		findings = append(findings, "the workflow sets defaults, which reach every run step")
	}
	for _, k := range jobKeys(j) {
		if !slices.Contains([]string{"name", "runs-on", "timeout-minutes", "steps"}, k) {
			findings = append(findings, "jobs.mermaid sets "+k+", which the check does not know leaves every test run")
		}
	}
	if !slices.Equal(j.RunsOn, []string{mermaidRunner}) || j.runnerGroup != "" {
		findings = append(findings, fmt.Sprintf("jobs.mermaid runs on %q in group %q, want %s alone, the runner runnerBuild models", []string(j.RunsOn), j.runnerGroup, mermaidRunner))
	}
	var goTests []step
	installs := 0
	for _, s := range j.Steps {
		switch {
		case goTestCall.MatchString(s.Run):
			goTests = append(goTests, s)
		case strings.HasPrefix(strings.TrimSpace(s.Run), "npm "):
			installs++
			findings = append(findings, stepKeyFindings("the install step", s, "name", "run", "working-directory")...)
			if run := strings.TrimSpace(s.Run); run != mermaidInstall {
				findings = append(findings, fmt.Sprintf("the install step runs %q, want %s", run, mermaidInstall))
			}
			if s.WorkDir != mermaidPkgDir {
				findings = append(findings, fmt.Sprintf("the install step runs in %q, want %s", s.WorkDir, mermaidPkgDir))
			}
		default:
			findings = append(findings, actionStepFindings(s)...)
		}
	}
	if installs == 0 {
		findings = append(findings, "no step of jobs.mermaid runs "+mermaidInstall+" in "+mermaidPkgDir)
	}
	if len(tagged) == 0 {
		findings = append(findings, "adapter/markdown declares no test behind the mermaid build tag that the job's runner builds")
	}
	for _, name := range unbuilt {
		findings = append(findings, name+" declares a test behind the mermaid build tag that the job's runner does not build")
	}
	if len(goTests) != 1 {
		findings = append(findings, fmt.Sprintf("jobs.mermaid runs go test in %d steps, want one", len(goTests)))
	} else {
		findings = append(findings, mermaidStepFindings(goTests[0])...)
	}
	if !slices.Contains(lintTags, "mermaid") {
		findings = append(findings, "the linter's build-tags leave the mermaid-tagged test unread")
	}
	if !slices.Contains(strings.Split(gitignore, "\n"), mermaidPkgDir+"/node_modules/") {
		findings = append(findings, ".gitignore does not ignore "+mermaidPkgDir+"/node_modules/")
	}
	return findings
}

// jobKeys returns, once each and sorted, the keys j's mapping holds and the
// key of each field set in j, so a job built in a test reads as its YAML would.
func jobKeys(j job) []string {
	m := j.Strategy.Matrix
	keys := slices.Clone(j.keys)
	for k, set := range map[string]bool{
		"runs-on":           len(j.RunsOn) > 0,
		"uses":              j.Uses != "",
		"needs":             len(j.Needs) > 0,
		"if":                j.If != "",
		"continue-on-error": j.ContinueOnError != nil,
		"timeout-minutes":   j.TimeoutMinutes != 0,
		"strategy":          j.Strategy.FailFast != nil || m.Dynamic || len(m.Axes)+len(m.Include)+len(m.Exclude) > 0,
		"env":               len(j.Env) > 0,
		"defaults":          j.Defaults != (runDefaults{}),
		"steps":             len(j.Steps) > 0,
	} {
		if set {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// stepKeyFindings reports each key s sets beyond allowed, where naming s. A
// key is set when s's mapping holds it or a field of s names it.
func stepKeyFindings(where string, s step, allowed ...string) []string {
	keys := slices.Clone(s.keys)
	for k, set := range map[string]bool{
		"name":              s.Name != "",
		"uses":              s.Uses != "",
		"run":               s.Run != "",
		"shell":             s.Shell != "",
		"working-directory": s.WorkDir != "",
		"with":              len(s.With) > 0,
		"env":               len(s.Env) > 0,
		"if":                s.If != "",
		"continue-on-error": s.ContinueOnError != nil,
		"timeout-minutes":   s.TimeoutMinutes != 0,
	} {
		if set {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	keys = slices.Compact(keys)
	var findings []string
	for _, k := range keys {
		if !slices.Contains(allowed, k) {
			findings = append(findings, fmt.Sprintf("%s sets %s, which the check does not know leaves every test run", where, k))
		}
	}
	return findings
}

// actionStepFindings holds a mermaid job step that neither installs Mermaid
// nor runs go test to one of mermaidActions and its inputs. Any other step
// could change what go test runs, through GITHUB_ENV, GITHUB_PATH or the tree.
func actionStepFindings(s step) []string {
	where := fmt.Sprintf("jobs.mermaid step %q", cmp.Or(s.Name, s.Uses, s.Run))
	action, _, _ := strings.Cut(s.Uses, "@")
	inputs, ok := mermaidActions[action]
	if !ok {
		return []string{where + " is not a step the check knows leaves every test run"}
	}
	findings := stepKeyFindings(where, s, "name", "uses", "with")
	for _, k := range slices.Sorted(maps.Keys(s.With)) {
		if want, known := inputs[k]; !known || (want != "" && fmt.Sprint(s.With[k]) != want) {
			findings = append(findings, fmt.Sprintf("%s passes %s: %v", where, k, s.With[k]))
		}
	}
	return findings
}

// mermaidGoTestFlags are the flags the mermaid job's go test may carry, each
// with whether it takes a value. None names or skips a test: -count must be
// present, since without it go test may report a result from the cache
// actions/setup-go restores; -count, -tags and -v are held to values that run
// every test; and an expired -timeout fails the job.
var mermaidGoTestFlags = map[string]bool{"count": true, "tags": true, "timeout": true, "v": false}

// unplainChar matches every character but ASCII letters and digits, space,
// tab and . _ / = , -: a set that holds each character bash may read as more
// than text (a quote, an expansion, a glob, an operator or a redirection) and
// some it reads as text. Bash splits a command line without one into exactly
// its white-space-separated fields.
var unplainChar = regexp.MustCompile(`[^A-Za-z0-9 \t._/=,-]`)

// mermaidStepFindings holds the mermaid job's go test step to the one form that
// runs every test of adapter/markdown under the mermaid tag: a run of one line
// of plain text, so no comment, continuation or second line reaches bash; go
// test first; only mermaidGoTestFlags, with -count present and positive, -tags
// mermaid alone and -v true; the one package ./adapter/markdown/; and no step
// key beyond name, run and timeout-minutes.
func mermaidStepFindings(s step) []string {
	findings := stepKeyFindings("the go test step", s, "name", "run", "timeout-minutes")
	line := strings.TrimSuffix(s.Run, "\n")
	if strings.Contains(line, "\n") {
		return append(findings, "the go test step's run holds more than one line")
	}
	if loc := unplainChar.FindStringIndex(line); loc != nil {
		r, _ := utf8.DecodeRuneInString(line[loc[0]:])
		return append(findings, fmt.Sprintf("the go test step's command holds %q, which bash reads as more than text", r))
	}
	words := strings.Fields(line)
	if len(words) < 2 || words[0] != "go" || words[1] != "test" {
		return append(findings, "the go test step's command does not start with go test")
	}
	var pkgs []string
	tags, counted := "", false
	for i := 2; i < len(words); i++ {
		w := words[i]
		if !strings.HasPrefix(w, "-") {
			pkgs = append(pkgs, w)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(w, "-"), "-"), "=")
		takesValue, known := mermaidGoTestFlags[name]
		if !known {
			// An unknown flag may take the next word, so nothing after it reads.
			return append(findings, fmt.Sprintf("the go test step passes %s, which the check does not know leaves every test run", w))
		}
		if takesValue && !hasValue {
			if i+1 == len(words) {
				findings = append(findings, fmt.Sprintf("the go test step's %s has no value", w))
				break
			}
			i++
			value = words[i]
		}
		switch name {
		case "count":
			counted = true
			if n, err := strconv.Atoi(value); err != nil || n < 1 {
				findings = append(findings, fmt.Sprintf("the go test step's -count %q runs no test", value))
			}
		case "tags":
			tags = value
		case "v":
			if hasValue && value != "true" {
				findings = append(findings, "the go test step passes "+w)
			}
		}
	}
	if !counted {
		findings = append(findings, "the go test step sets no -count, so go test may report a cached result")
	}
	if tags != "mermaid" {
		findings = append(findings, fmt.Sprintf("the go test step's -tags is %q, want mermaid alone, the tag the check builds with", tags))
	}
	if len(pkgs) != 1 || (pkgs[0] != "./adapter/markdown/" && pkgs[0] != "./adapter/markdown") {
		findings = append(findings, fmt.Sprintf("the go test step runs %q, want the one package ./adapter/markdown/", pkgs))
	}
	return findings
}

// runnerEnvRefused are the environment variables that choose the toolchain or
// change a field of the build context runnerBuild reads from go list; it sets
// the runner's own value of each or clears it.
var runnerEnvRefused = []string{"GOOS", "GOARCH", "GOAMD64", "GOEXPERIMENT", "CGO_ENABLED", "GOFLAGS", "GOENV", "GOTOOLCHAIN", "GOROOT"}

// runnerBuild returns the build context go test takes on mermaidRunner: the
// one the toolchain go.mod's go directive names, which actions/setup-go
// installs there, reports for linux/amd64 with cgo on. Its tool tags hold
// GOAMD64's level and the experiments that toolchain turns on for that target,
// which can differ from this host's.
func runnerBuild(t *testing.T) build.Context {
	t.Helper()
	mod, err := os.ReadFile(fromRoot("go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	var version string
	for line := range strings.Lines(string(mod)) {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "go" {
			version = f[1]
			break
		}
	}
	if version == "" {
		t.Fatal("go.mod has no go directive")
	}
	cmd := exec.CommandContext(t.Context(), "go", "list", "-f",
		"{{context.GOOS}} {{context.GOARCH}} {{context.Compiler}} {{context.CgoEnabled}}\n"+
			"{{join context.ToolTags \" \"}}\n{{join context.ReleaseTags \" \"}}", "runtime")
	cmd.Dir = repoRoot
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		// Windows reads environment names without regard to case.
		return slices.ContainsFunc(runnerEnvRefused, func(r string) bool { return strings.EqualFold(r, name) })
	}), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=1", "GOENV=off", "GOTOOLCHAIN=go"+version)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list the runner's build context: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("go list printed %q, want three lines", out)
	}
	head := strings.Fields(lines[0])
	if len(head) != 4 || head[0] != "linux" || head[1] != "amd64" || head[3] != "true" {
		t.Fatalf("go list reports the runner's build as %q, want linux amd64 with cgo", lines[0])
	}
	release := strings.Fields(lines[2])
	if lang := strings.Join(strings.SplitN(version, ".", 3)[:2], "."); len(release) == 0 || release[len(release)-1] != "go"+lang {
		t.Fatalf("go list reports release tags %q, want go.mod's go%s last", lines[2], lang)
	}
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH, ctx.Compiler, ctx.CgoEnabled = head[0], head[1], head[2], true
	ctx.ToolTags, ctx.ReleaseTags = strings.Fields(lines[1]), release
	return ctx
}

// taggedTestFiles reads srcs, adapter/markdown's test files by name, in the
// runner's build context. tagged holds each file that declares a test and that
// runner compiles with the mermaid tag and not without it. unbuilt holds each
// file that declares a test, whose build constraint needs the tag, and that
// runner does not compile even with it: a test the mermaid job never runs. Both
// are sorted. The build decision is go/build's, the one go test makes, so a
// //go:build or +build line, a GOOS or GOARCH file name and a leading "_" or
// "." each decide as go test decides on the runner.
func taggedTestFiles(runner build.Context, srcs map[string]string, tag string) (tagged, unbuilt []string, err error) {
	ctx := runner
	ctx.OpenFile = func(path string) (io.ReadCloser, error) {
		src, ok := srcs[filepath.Base(path)]
		if !ok {
			return nil, fs.ErrNotExist
		}
		return io.NopCloser(strings.NewReader(src)), nil
	}
	without := ctx
	ctx.BuildTags = []string{tag}
	for name, src := range srcs {
		// go/build ignores these names on every platform.
		if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}
		with, err := ctx.MatchFile(".", name)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", name, err)
		}
		plain, err := without.MatchFile(".", name)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", name, err)
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, nil, fmt.Errorf("%s does not parse: %w", name, err)
		}
		if !slices.ContainsFunc(f.Decls, func(d ast.Decl) bool {
			fn, ok := d.(*ast.FuncDecl)
			return ok && isGoTestFunc(fn)
		}) {
			continue
		}
		if with && !plain {
			tagged = append(tagged, name)
			continue
		}
		if with {
			continue
		}
		switch needs, err := needsTag(src, tag); {
		case err != nil:
			return nil, nil, fmt.Errorf("%s: %w", name, err)
		case needs:
			unbuilt = append(unbuilt, name)
		}
	}
	slices.Sort(tagged)
	slices.Sort(unbuilt)
	return tagged, unbuilt, nil
}

// needsTag reports whether go/build admits src with tag and refuses it
// without, for some setting of the other tags src's constraint lines name.
// Each setting is a context in which those tags alone hold, go/build's
// boringcrypto alias included, so go/build's own header rules decide which
// lines count. Past 16 other tags it answers true
// without looking, which reports the file rather than hiding it.
func needsTag(src, tag string) (bool, error) {
	var others []string
	var collect func(constraint.Expr)
	collect = func(x constraint.Expr) {
		switch x := x.(type) {
		case *constraint.TagExpr:
			if x.Tag != tag && !slices.Contains(others, x.Tag) {
				others = append(others, x.Tag)
			}
		case *constraint.NotExpr:
			collect(x.X)
		case *constraint.AndExpr:
			collect(x.X)
			collect(x.Y)
		case *constraint.OrExpr:
			collect(x.X)
			collect(x.Y)
		}
	}
	for line := range strings.Lines(src) {
		line = strings.TrimSpace(line)
		if constraint.IsGoBuild(line) || constraint.IsPlusBuild(line) {
			if x, err := constraint.Parse(line); err == nil {
				collect(x)
			}
		}
	}
	if len(others) > 16 {
		return true, nil
	}
	// No GOOS, GOARCH, compiler, cgo, tool or release tag holds here, so a
	// tag holds exactly when BuildTags names it, boringcrypto by its
	// experiment's name. The file name is neutral, so only the constraint
	// lines decide.
	const probe = "needstag_test.go"
	ctx := build.Context{OpenFile: func(string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(src)), nil
	}}
	for set := range 1 << len(others) {
		ctx.BuildTags = nil
		for i, t := range others {
			if set&(1<<i) != 0 {
				ctx.BuildTags = append(ctx.BuildTags, t)
				// go/build reads boringcrypto as goexperiment.boringcrypto.
				if t == "boringcrypto" {
					ctx.BuildTags = append(ctx.BuildTags, "goexperiment.boringcrypto")
				}
			}
		}
		without, err := ctx.MatchFile(".", probe)
		if err != nil {
			return false, err
		}
		ctx.BuildTags = append(ctx.BuildTags, tag)
		with, err := ctx.MatchFile(".", probe)
		if err != nil {
			return false, err
		}
		if with && !without {
			return true, nil
		}
	}
	return false, nil
}

// isGoTestFunc reports whether fn is one go test runs as a test, by cmd/go's
// rule: a top-level function named Test, or Test and a rune that is not lower
// case, with no result and one parameter, a pointer to a type named T, and no
// type parameter unless it is TestMain. cmd/go refuses the package when a
// function so named breaks the rest, save a TestMain taking a pointer to M.
func isGoTestFunc(fn *ast.FuncDecl) bool {
	name := fn.Name.Name
	if fn.Recv != nil || !strings.HasPrefix(name, "Test") {
		return false
	}
	if rest := name[len("Test"):]; rest != "" {
		if r, _ := utf8.DecodeRuneInString(rest); unicode.IsLower(r) {
			return false
		}
	}
	if (name != "TestMain" && fn.Type.TypeParams.NumFields() > 0) || fn.Type.Results.NumFields() > 0 {
		return false
	}
	params := fn.Type.Params.List
	if len(params) != 1 || len(params[0].Names) > 1 {
		return false
	}
	star, ok := params[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	switch t := star.X.(type) {
	case *ast.Ident:
		return t.Name == "T"
	case *ast.SelectorExpr:
		return t.Sel.Name == "T"
	}
	return false
}

// The mermaid job parses adapter/markdown's diagrams with real Mermaid only
// when every link of its chain holds.
func TestTestWorkflow_TheMermaidJobParsesTheDiagrams(t *testing.T) {
	t.Parallel()
	var wf workflow
	decodeYAML(t, fromRoot(".github/workflows/yammm_test.yml"), &wf)
	names, err := filepath.Glob(fromRoot("adapter/markdown/*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	srcs := map[string]string{}
	for _, name := range names {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		srcs[filepath.Base(name)] = string(src)
	}
	var lint struct {
		Run struct {
			BuildTags []string `yaml:"build-tags"`
		} `yaml:"run"`
	}
	decodeYAML(t, fromRoot(".golangci.yml"), &lint)
	ignore, err := os.ReadFile(fromRoot(".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	tagged, unbuilt, err := taggedTestFiles(runnerBuild(t), srcs, "mermaid")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range mermaidJobFindings(wf, tagged, unbuilt, lint.Run.BuildTags, string(ignore)) {
		t.Error(f)
	}
}

// The check itself: each form it refuses draws one finding naming the break,
// and each form it accepts draws none.
func TestMermaidJobFindings_RefusesEachBrokenLink(t *testing.T) {
	t.Parallel()
	const (
		run    = "go test -count=1 -tags mermaid -timeout 5m -v ./adapter/markdown/"
		ignore = "x\n" + mermaidPkgDir + "/node_modules/\n"
		// The job's steps by index.
		checkout, setupGo, npm, goTest = 0, 1, 3, 4
	)
	tagged := []string{"mermaid_parse_test.go"}
	wfOf := func(edit func(*workflow, *job, *step)) workflow {
		j := job{
			RunsOn:         runnerLabels{mermaidRunner},
			TimeoutMinutes: 15,
			keys:           []string{"name"},
			Steps: []step{
				{Name: "Checkout", Uses: "actions/checkout@v7"},
				{Name: "Set up Go", Uses: "actions/setup-go@v7", With: map[string]any{"go-version-file": "go.mod"}},
				{Name: "Set up Node", Uses: "actions/setup-node@v7", With: map[string]any{"node-version": 22}},
				{Name: "Install Mermaid", WorkDir: mermaidPkgDir, Run: mermaidInstall},
				{Name: "Parse the diagrams", Run: run},
			},
		}
		wf := workflow{}
		if edit != nil {
			edit(&wf, &j, &j.Steps[goTest])
		}
		wf.Jobs = map[string]job{"mermaid": j}
		return wf
	}
	withRun := func(r string) workflow { return wfOf(func(_ *workflow, _ *job, s *step) { s.Run = r }) }
	good := wfOf(nil)
	for name, tt := range map[string]struct {
		wf     workflow
		tagged []string
	}{
		"the known form":           {good, tagged},
		"flags as -f=v and --f":    {withRun("go test --count=2 -tags=mermaid --timeout=5m -v=true ./adapter/markdown"), tagged},
		"the last -tags applies":   {withRun("go test -count=1 -tags x -tags mermaid ./adapter/markdown/"), tagged},
		"a block scalar's newline": {withRun(run + "\n"), tagged},
		"a step timeout":           {wfOf(func(_ *workflow, _ *job, s *step) { s.TimeoutMinutes = 5 }), tagged},
		"setup-go without inputs":  {wfOf(func(_ *workflow, j *job, _ *step) { j.Steps[setupGo].With = nil }), tagged},
	} {
		if f := mermaidJobFindings(tt.wf, tt.tagged, nil, []string{"mermaid"}, ignore); len(f) != 0 {
			t.Errorf("%s: a whole chain reports %q", name, f)
		}
	}
	for _, tt := range []struct {
		name, want string
		wf         workflow
		tagged     []string
		tags       []string
		ignore     string
	}{
		{"no job", "is not set", workflow{Jobs: map[string]job{}}, tagged, []string{"mermaid"}, ignore},
		{"no npm ci", "no step of jobs.mermaid runs npm ci", wfOf(func(_ *workflow, j *job, _ *step) { j.Steps = slices.Delete(j.Steps, npm, npm+1) }), tagged, []string{"mermaid"}, ignore},
		{"npm ci elsewhere", "the install step runs in", wfOf(func(_ *workflow, j *job, _ *step) { j.Steps[npm].WorkDir = "adapter/markdown" }), tagged, []string{"mermaid"}, ignore},
		{"npm ci running scripts", `runs "npm ci", want`, wfOf(func(_ *workflow, j *job, _ *step) { j.Steps[npm].Run = "npm ci" }), tagged, []string{"mermaid"}, ignore},
		{"an env on npm ci", "the install step sets env", wfOf(func(_ *workflow, j *job, _ *step) { j.Steps[npm].Env = map[string]string{"X": "1"} }), tagged, []string{"mermaid"}, ignore},
		{"no go test step", "in 0 steps", wfOf(func(_ *workflow, j *job, _ *step) { j.Steps = j.Steps[:goTest] }), tagged, []string{"mermaid"}, ignore},
		{"two go test steps", "in 2 steps", wfOf(func(_ *workflow, j *job, _ *step) { j.Steps = append(j.Steps, step{Run: run}) }), tagged, []string{"mermaid"}, ignore},
		{"no tag", `-tags is ""`, withRun(strings.Replace(run, "-tags mermaid ", "", 1)), tagged, []string{"mermaid"}, ignore},
		{"a tag beside mermaid", `-tags is "mermaid,x"`, withRun(strings.Replace(run, "-tags mermaid", "-tags mermaid,x", 1)), tagged, []string{"mermaid"}, ignore},
		{"another package", "want the one package", withRun(strings.Replace(run, "./adapter/markdown/", "./...", 1)), tagged, []string{"mermaid"}, ignore},
		{"a second package", "want the one package", withRun(run + " ./adapter/json/"), tagged, []string{"mermaid"}, ignore},
		{"a -run", "passes -run", withRun(strings.Replace(run, " ./", " -run TestA ./", 1)), tagged, []string{"mermaid"}, ignore},
		{"a -skip", "passes -skip", withRun(strings.Replace(run, " ./", " -skip=TestA ./", 1)), tagged, []string{"mermaid"}, ignore},
		{"a -test.run", "passes -test.run", withRun(strings.Replace(run, " ./", " -test.run=TestA ./", 1)), tagged, []string{"mermaid"}, ignore},
		{"-args to the binary", "passes -args", withRun(run + " -args -test.run=TestA"), tagged, []string{"mermaid"}, ignore},
		{"a -list", "passes -list", withRun(strings.Replace(run, " ./", " -list . ./", 1)), tagged, []string{"mermaid"}, ignore},
		{"-count=0", "runs no test", withRun(strings.Replace(run, "-count=1", "-count=0", 1)), tagged, []string{"mermaid"}, ignore},
		{"-v=false", "passes -v=false", withRun(strings.Replace(run, "-v ", "-v=false ", 1)), tagged, []string{"mermaid"}, ignore},
		{"an -exec", "passes -exec=true", withRun(strings.Replace(run, " ./", " -exec=true ./", 1)), tagged, []string{"mermaid"}, ignore},
		{"a -coverpkg decoy", "passes -coverpkg", withRun("go test -count=1 -tags mermaid -coverpkg ./adapter/markdown/ ./adapter/json/"), tagged, []string{"mermaid"}, ignore},
		{"go test not first", "does not start with go test", withRun("echo " + run), tagged, []string{"mermaid"}, ignore},
		{"another go", "does not start with go test", withRun("./" + run), tagged, []string{"mermaid"}, ignore},
		{"a second command line", "more than one line", withRun(run + "\necho done"), tagged, []string{"mermaid"}, ignore},
		{"a comment line", "more than one line", withRun("# every test\n" + run), tagged, []string{"mermaid"}, ignore},
		{"a continued line", "more than one line", withRun("go test -count=1 \\\n  -tags mermaid -v ./adapter/markdown/"), tagged, []string{"mermaid"}, ignore},
		{"a comment", `holds '#'`, withRun(run + " # every test"), tagged, []string{"mermaid"}, ignore},
		{"a trailing backslash", `holds '\\'`, withRun(run + " \\"), tagged, []string{"mermaid"}, ignore},
		{"a form feed", `holds '\f'`, withRun("\f" + run), tagged, []string{"mermaid"}, ignore},
		{"a carriage return", `holds '\r'`, withRun(run + "\r"), tagged, []string{"mermaid"}, ignore},
		{"a pipe", `holds '|'`, withRun(strings.Replace(run, "-timeout 5m", "-timeout=5m|true", 1)), tagged, []string{"mermaid"}, ignore},
		{"a background", `holds '&'`, withRun(strings.Replace(run, "-timeout 5m", "-timeout=5m&", 1)), tagged, []string{"mermaid"}, ignore},
		{"no -count", "sets no -count", withRun(strings.Replace(run, "-count=1 ", "", 1)), tagged, []string{"mermaid"}, ignore},
		{"a value flag with no value", "has no value", withRun(run + " -timeout"), tagged, []string{"mermaid"}, ignore},
		{"a variable", `holds '$'`, withRun(strings.Replace(run, "5m", "$T", 1)), tagged, []string{"mermaid"}, ignore},
		{"a second command on the line", `holds ';'`, withRun(strings.Replace(run, "-timeout 5m", "-timeout=5m;true", 1)), tagged, []string{"mermaid"}, ignore},
		{"a quoted word", `holds '\''`, withRun(strings.Replace(run, "-tags mermaid", "-tags 'mermaid'", 1)), tagged, []string{"mermaid"}, ignore},
		{"a GitHub expression", `holds '$'`, withRun(strings.Replace(run, "-tags mermaid", "-tags ${{ env.TAGS }}", 1)), tagged, []string{"mermaid"}, ignore},
		{"a glob", `holds '*'`, withRun(strings.Replace(run, "./adapter/markdown/", "./adapter/mark*/", 1)), tagged, []string{"mermaid"}, ignore},
		{"an env on the step", "the go test step sets env", wfOf(func(_ *workflow, _ *job, s *step) { s.Env = map[string]string{"GOENV": "/tmp/go.env"} }), tagged, []string{"mermaid"}, ignore},
		{"an env on the job", "jobs.mermaid sets env", wfOf(func(_ *workflow, j *job, _ *step) { j.Env = map[string]string{"GOFLAGS": "-run=TestA"} }), tagged, []string{"mermaid"}, ignore},
		{"an env on the workflow", "the workflow sets env", wfOf(func(wf *workflow, _ *job, _ *step) { wf.Env = map[string]string{"CGO_ENABLED": "0"} }), tagged, []string{"mermaid"}, ignore},
		{"an if on the step", "the go test step sets if", wfOf(func(_ *workflow, _ *job, s *step) { s.If = "false" }), tagged, []string{"mermaid"}, ignore},
		{"an if on the job", "jobs.mermaid sets if", wfOf(func(_ *workflow, j *job, _ *step) { j.If = "false" }), tagged, []string{"mermaid"}, ignore},
		{"continue-on-error on the job", "jobs.mermaid sets continue-on-error", wfOf(func(_ *workflow, j *job, _ *step) { j.ContinueOnError = true }), tagged, []string{"mermaid"}, ignore},
		{"a needs", "jobs.mermaid sets needs", wfOf(func(_ *workflow, j *job, _ *step) { j.Needs = stringList{"test"} }), tagged, []string{"mermaid"}, ignore},
		{"a runner group", `in group "any"`, wfOf(func(_ *workflow, j *job, _ *step) { j.runnerGroup = "any" }), tagged, []string{"mermaid"}, ignore},
		{"continue-on-error", "the go test step sets continue-on-error", wfOf(func(_ *workflow, _ *job, s *step) { s.ContinueOnError = true }), tagged, []string{"mermaid"}, ignore},
		{"a shell", "sets shell", wfOf(func(_ *workflow, _ *job, s *step) { s.Shell = "python" }), tagged, []string{"mermaid"}, ignore},
		{"a working directory", "the go test step sets working-directory", wfOf(func(_ *workflow, _ *job, s *step) { s.WorkDir = "adapter" }), tagged, []string{"mermaid"}, ignore},
		{"workflow defaults", "the workflow sets defaults", wfOf(func(wf *workflow, _ *job, _ *step) { wf.Defaults.Run.WorkingDirectory = "adapter" }), tagged, []string{"mermaid"}, ignore},
		{"job defaults", "jobs.mermaid sets defaults", wfOf(func(_ *workflow, j *job, _ *step) { j.Defaults.Run.Shell = "sh" }), tagged, []string{"mermaid"}, ignore},
		{"a container", "jobs.mermaid sets container", wfOf(func(_ *workflow, j *job, _ *step) { j.keys = append(j.keys, "container") }), tagged, []string{"mermaid"}, ignore},
		{"a matrix", "jobs.mermaid sets strategy", wfOf(func(_ *workflow, j *job, _ *step) { j.Strategy.Matrix.Axes = map[string][]string{"go": {"1.26"}} }), tagged, []string{"mermaid"}, ignore},
		{"another runner", "runs on", wfOf(func(_ *workflow, j *job, _ *step) { j.RunsOn = runnerLabels{"ubuntu-24.04-arm"} }), tagged, []string{"mermaid"}, ignore},
		{"a step that writes GITHUB_ENV", `step "Env" is not a step`, wfOf(func(_ *workflow, j *job, _ *step) {
			j.Steps = slices.Insert(j.Steps, goTest, step{Name: "Env", Run: "echo GOFLAGS=-run=TestA >> $GITHUB_ENV"})
		}), tagged, []string{"mermaid"}, ignore},
		{"another action", `"someone/go-env@v1" is not a step`, wfOf(func(_ *workflow, j *job, _ *step) { j.Steps = append(j.Steps, step{Uses: "someone/go-env@v1"}) }), tagged, []string{"mermaid"}, ignore},
		{"checkout of another ref", "passes ref", wfOf(func(_ *workflow, j *job, _ *step) { j.Steps[checkout].With = map[string]any{"ref": "main"} }), tagged, []string{"mermaid"}, ignore},
		{"another Go", "passes go-version-file", wfOf(func(_ *workflow, j *job, _ *step) { j.Steps[setupGo].With["go-version-file"] = "old/go.mod" }), tagged, []string{"mermaid"}, ignore},
		{"an env on an action", `"Set up Go" sets env`, wfOf(func(_ *workflow, j *job, _ *step) { j.Steps[setupGo].Env = map[string]string{"X": "1"} }), tagged, []string{"mermaid"}, ignore},
		{"no tagged test", "declares no test", good, nil, []string{"mermaid"}, ignore},
		{"a linter blind to the tag", "linter", good, tagged, []string{"neo4j_integration"}, ignore},
		{"node_modules tracked", ".gitignore", good, tagged, []string{"mermaid"}, "x\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := mermaidJobFindings(tt.wf, tt.tagged, nil, tt.tags, tt.ignore)
			if len(f) != 1 || !strings.Contains(f[0], tt.want) {
				t.Errorf("findings = %q, want one naming %q", f, tt.want)
			}
		})
	}
}

// A test file the runner never builds is a finding even beside one it builds.
func TestMermaidJobFindings_RefusesAnUnbuiltTaggedTest(t *testing.T) {
	t.Parallel()
	var wf workflow
	decodeYAML(t, fromRoot(".github/workflows/yammm_test.yml"), &wf)
	f := mermaidJobFindings(wf, []string{"mermaid_parse_test.go"}, []string{"x_windows_test.go"}, []string{"mermaid"}, mermaidPkgDir+"/node_modules/\n")
	if len(f) != 1 || !strings.Contains(f[0], "x_windows_test.go declares a test") {
		t.Errorf("findings = %q, want one naming x_windows_test.go", f)
	}
}

// runnerBuild reports the runner's context whatever go environment the caller
// has, a user go env file included. It sets the environment, so it runs
// before the package's parallel tests.
func TestRunnerBuild_IgnoresTheCallersGoEnvironment(t *testing.T) {
	want := runnerBuild(t)
	out, err := exec.CommandContext(t.Context(), "go", "env", "GOMODCACHE", "GOCACHE", "GOPATH").Output()
	if err != nil {
		t.Fatal(err)
	}
	kept := strings.Fields(string(out))
	if len(kept) != 3 {
		t.Fatalf("go env printed %q, want three values", out)
	}
	home := t.TempDir()
	for _, dir := range []string{filepath.Join(home, "Library", "Application Support", "go"), filepath.Join(home, "go")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "env"), []byte("GOAMD64=v3\nGOEXPERIMENT=nodwarf5\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for k, v := range map[string]string{
		"GOMODCACHE": kept[0], "GOCACHE": kept[1], "GOPATH": kept[2],
		"HOME": home, "XDG_CONFIG_HOME": home, "AppData": home,
		"GOFLAGS": "-race", "GOAMD64": "v2", "GOEXPERIMENT": "nogreenteagc", "GOOS": "windows",
		"GOARCH": "arm64", "CGO_ENABLED": "0", "GOROOT": home,
	} {
		t.Setenv(k, v)
	}
	got := runnerBuild(t)
	if got.GOOS != want.GOOS || got.GOARCH != want.GOARCH || got.CgoEnabled != want.CgoEnabled ||
		!slices.Equal(got.ToolTags, want.ToolTags) || !slices.Equal(got.ReleaseTags, want.ReleaseTags) {
		t.Errorf("runnerBuild under the caller's environment = %s/%s cgo %v %q %q, want %s/%s cgo %v %q %q",
			got.GOOS, got.GOARCH, got.CgoEnabled, got.ToolTags, got.ReleaseTags,
			want.GOOS, want.GOARCH, want.CgoEnabled, want.ToolTags, want.ReleaseTags)
	}
}

// taggedTestFiles reads a file as go test reads it on the mermaid job's
// runner: the build constraint, the file name and the test declarations each
// decide whether the file holds a test only the mermaid tag runs.
func TestTaggedTestFiles_ReadsWhatGoTestBuildsOnTheRunner(t *testing.T) {
	t.Parallel()
	runner := runnerBuild(t)
	const head = "//go:build mermaid\n\npackage markdown\n\nimport \"testing\"\n"
	plain := "package markdown\n\nimport \"testing\"\n\nfunc TestPlain(t *testing.T) {}\n"
	file := func(constraint, decl string) string {
		return constraint + "\n\npackage markdown\n\nimport \"testing\"\n\n" + decl + "\n"
	}
	const test = "func TestA(t *testing.T) {}"
	for _, tt := range []struct {
		name          string
		srcs          map[string]string
		want, unbuilt []string
	}{
		{"a tagged test", map[string]string{"x_test.go": head + "\n" + test + "\n", "plain_test.go": plain}, []string{"x_test.go"}, nil},
		{"each tagged file, sorted", map[string]string{"b_test.go": file("//go:build mermaid", test), "a_test.go": file("//go:build mermaid", test)}, []string{"a_test.go", "b_test.go"}, nil},
		{"a *T parameter", map[string]string{"x_test.go": file("//go:build mermaid", "func TestA(t *T) {}")}, []string{"x_test.go"}, nil},
		{"a test named Test", map[string]string{"x_test.go": file("//go:build mermaid", "func Test(t *testing.T) {}")}, []string{"x_test.go"}, nil},
		{"a +build line", map[string]string{"x_test.go": file("// +build mermaid", test)}, []string{"x_test.go"}, nil},
		{"the runner's GOOS", map[string]string{"x_test.go": file("//go:build mermaid && linux", test)}, []string{"x_test.go"}, nil},
		{"the runner's GOARCH", map[string]string{"x_amd64_test.go": file("//go:build mermaid", test)}, []string{"x_amd64_test.go"}, nil},
		{"the runner's GOAMD64 level", map[string]string{"x_test.go": file("//go:build mermaid && amd64.v1", test)}, []string{"x_test.go"}, nil},
		{"cgo", map[string]string{"x_test.go": file("//go:build mermaid && cgo", test)}, []string{"x_test.go"}, nil},
		{"the toolchain's release", map[string]string{"x_test.go": file("//go:build mermaid && go1.26", test)}, []string{"x_test.go"}, nil},
		{"no constraint", map[string]string{"plain_test.go": plain}, nil, nil},
		{"a constraint the tag does not decide", map[string]string{"x_test.go": file("//go:build mermaid || linux", test)}, nil, nil},
		{"no test declared", map[string]string{"x_test.go": head}, nil, nil},
		{"a helper", map[string]string{"x_test.go": file("//go:build mermaid", "func Testx(t *testing.T) {}")}, nil, nil},
		{"a method", map[string]string{"x_test.go": file("//go:build mermaid", "type s struct{}\n\nfunc (s) TestA(t *testing.T) {}")}, nil, nil},
		{"two parameters in one field", map[string]string{"x_test.go": file("//go:build mermaid", "func TestA(a, b *testing.T) {}")}, nil, nil},
		{"a benchmark parameter", map[string]string{"x_test.go": file("//go:build mermaid", "func TestA(b *testing.B) {}")}, nil, nil},
		{"a bare *B parameter", map[string]string{"x_test.go": file("//go:build mermaid", "type B struct{}\n\nfunc TestA(b *B) {}")}, nil, nil},
		{"two parameter fields", map[string]string{"x_test.go": file("//go:build mermaid", "func TestA(t *testing.T, n int) {}")}, nil, nil},
		{"a value parameter", map[string]string{"x_test.go": file("//go:build mermaid", "func TestA(t testing.T) {}")}, nil, nil},
		{"a result", map[string]string{"x_test.go": file("//go:build mermaid", "func TestA(t *testing.T) int { return 0 }")}, nil, nil},
		{"a type parameter", map[string]string{"x_test.go": file("//go:build mermaid", "func TestA[P any](t *testing.T) {}")}, nil, nil},
		{"a TestMain", map[string]string{"x_test.go": file("//go:build mermaid", "func TestMain(m *testing.M) {}")}, nil, nil},
		{"a TestMain taking *T", map[string]string{"x_test.go": file("//go:build mermaid", "func TestMain(t *testing.T) {}")}, []string{"x_test.go"}, nil},
		{"a TestMain taking *T with a type parameter", map[string]string{"x_test.go": file("//go:build mermaid", "func TestMain[P any](t *testing.T) {}")}, []string{"x_test.go"}, nil},
		{"another GOOS without the tag", map[string]string{"x_test.go": file("//go:build windows", test)}, nil, nil},
		{"another GOOS by name", map[string]string{"x_windows_test.go": file("//go:build mermaid", test)}, nil, []string{"x_windows_test.go"}},
		{"another GOARCH by name", map[string]string{"x_arm64_test.go": file("//go:build mermaid", test)}, nil, []string{"x_arm64_test.go"}},
		{"another GOOS by constraint", map[string]string{"x_test.go": file("//go:build mermaid && !linux", test)}, nil, []string{"x_test.go"}},
		{"another GOARCH's level", map[string]string{"x_test.go": file("//go:build mermaid && arm64.v8.0", test)}, nil, []string{"x_test.go"}},
		{"no cgo", map[string]string{"x_test.go": file("//go:build mermaid && !cgo", test)}, nil, []string{"x_test.go"}},
		{"a later release", map[string]string{"x_test.go": file("//go:build mermaid && go1.99", test)}, nil, []string{"x_test.go"}},
		{"a +build line for another GOOS", map[string]string{"x_test.go": file("// +build mermaid,windows", test)}, nil, []string{"x_test.go"}},
		{"a negated tag", map[string]string{"x_test.go": file("//go:build !mermaid && windows", test)}, nil, nil},
		{"one built and one not", map[string]string{"a_test.go": file("//go:build mermaid", test), "b_windows_test.go": file("//go:build mermaid", test)}, []string{"a_test.go"}, []string{"b_windows_test.go"}},
		{"a +build line go/build ignores", map[string]string{"x_windows_test.go": "// +build mermaid\npackage markdown\n\nimport \"testing\"\n\n" + test + "\n"}, nil, nil},
		{"a +build line and a //go:build line", map[string]string{"x_test.go": file("//go:build mermaid && windows\n// +build mermaid", test)}, nil, []string{"x_test.go"}},
		{"the boringcrypto alias", map[string]string{"x_test.go": file("//go:build mermaid && boringcrypto", test)}, nil, []string{"x_test.go"}},
		{"a leading underscore", map[string]string{"_x_test.go": file("//go:build mermaid", test)}, nil, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, unbuilt, err := taggedTestFiles(runner, tt.srcs, "mermaid")
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) || !slices.Equal(unbuilt, tt.unbuilt) {
				t.Errorf("taggedTestFiles = %q, %q, want %q, %q", got, unbuilt, tt.want, tt.unbuilt)
			}
		})
	}
	if _, _, err := taggedTestFiles(runner, map[string]string{"bad_test.go": "//go:build mermaid\n\npackage"}, "mermaid"); err == nil || !strings.Contains(err.Error(), "does not parse") {
		t.Errorf("a file that does not parse: err = %v, want one naming it", err)
	}
}

// A decoded job or step reports each key its YAML sets, one that decodes to
// a zero value or into no field included.
func TestJobKeys_ReadEveryKeyTheYAMLSets(t *testing.T) {
	t.Parallel()
	j := decodeJob(t, "name: x\nruns-on: ubuntu-24.04\nstrategy:\n  max-parallel: 2\ntimeout-minutes: 0\nenv: {}\ncontainer: golang\nsteps:\n  - id: s\n    run: go test\n    timeout-minutes: 0\n")
	want := []string{"container", "env", "name", "runs-on", "steps", "strategy", "timeout-minutes"}
	if got := jobKeys(j); !slices.Equal(got, want) {
		t.Errorf("jobKeys = %q, want %q", got, want)
	}
	f := stepKeyFindings("the step", j.Steps[0], "run")
	if len(f) != 2 || !strings.Contains(f[0], "sets id") || !strings.Contains(f[1], "sets timeout-minutes") {
		t.Errorf("stepKeyFindings = %q, want id and timeout-minutes", f)
	}
	if g := decodeJob(t, "runs-on:\n  group: any\n  labels: ubuntu-24.04\n"); g.runnerGroup != "any" || !slices.Equal(g.RunsOn, []string{"ubuntu-24.04"}) {
		t.Errorf("runs-on decoded as %q in group %q, want ubuntu-24.04 in group any", g.RunsOn, g.runnerGroup)
	}
}

// goTestFlags and goTestPassed hold the flags the toolchain's cmd/go source
// registers for go test, each with its kind, so a toolchain that adds or
// changes one fails here rather than misleading the timeout check.
func TestGoTestFlags_MatchTheToolchain(t *testing.T) {
	t.Parallel()
	out, err := exec.CommandContext(t.Context(), "go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	internal := filepath.Join(strings.TrimSpace(string(out)), "src", "cmd", "go", "internal")
	fset := token.NewFileSet()
	var files []*ast.File
	for _, dir := range []string{"base", "test", "work"} {
		names, err := filepath.Glob(filepath.Join(internal, dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
		}
	}
	// A flag.Value type whose IsBoolFlag method exists reads as a boolean.
	boolTypes, varTypes := map[string]bool{}, map[string]string{}
	typeName := func(e ast.Expr) string {
		for {
			switch x := e.(type) {
			case *ast.StarExpr:
				e = x.X
			case *ast.ParenExpr:
				e = x.X
			case *ast.Ident:
				return x.Name
			default:
				return ""
			}
		}
	}
	for _, f := range files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv != nil && d.Name.Name == "IsBoolFlag" {
					boolTypes[typeName(d.Recv.List[0].Type)] = true
				}
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					if vs, ok := sp.(*ast.ValueSpec); ok && vs.Type != nil {
						for _, n := range vs.Names {
							varTypes[n.Name] = typeName(vs.Type)
						}
					}
				}
			}
		}
	}
	// The functions that register go test's flags: cmd/go's test package init
	// and the helpers it and work.AddBuildFlags call.
	registrars := []string{"AddBuildFlags", "AddBuildFlagsNX", "AddChdirFlag", "AddModFlag", "AddModCommonFlags", "AddCoverFlags", "init"}
	got := map[string]bool{}
	var passed []string
	for _, f := range files {
		file := fset.File(f.Pos()).Name()
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if ok && fn.Recv == nil && slices.Contains(registrars, fn.Name.Name) && (fn.Name.Name != "init" || strings.HasSuffix(file, "testflag.go")) {
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || len(call.Args) < 2 {
						return true
					}
					var name string
					for _, a := range call.Args {
						if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
							name, _ = strconv.Unquote(lit.Value)
							break
						}
					}
					if name == "" || strings.HasPrefix(name, "test.") {
						return true
					}
					switch sel.Sel.Name {
					case "Bool", "BoolVar":
						got[name] = false
					case "String", "StringVar", "Int", "IntVar", "Duration", "DurationVar", "Func":
						got[name] = true
					case "Var":
						arg := call.Args[0]
						if u, ok := arg.(*ast.UnaryExpr); ok {
							arg = u.X
						}
						tn := ""
						switch a := arg.(type) {
						case *ast.CallExpr:
							tn = typeName(a.Fun)
						case *ast.CompositeLit:
							tn = typeName(a.Type)
						case *ast.Ident:
							tn = varTypes[a.Name]
						}
						got[name] = !boolTypes[tn]
					}
					return true
				})
			}
			if gd, ok := d.(*ast.GenDecl); ok && strings.HasSuffix(file, "flagdefs.go") {
				for _, sp := range gd.Specs {
					vs, ok := sp.(*ast.ValueSpec)
					if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "passFlagToTest" {
						continue
					}
					for _, e := range vs.Values[0].(*ast.CompositeLit).Elts {
						kv := e.(*ast.KeyValueExpr)
						k, _ := strconv.Unquote(kv.Key.(*ast.BasicLit).Value)
						if id, ok := kv.Value.(*ast.Ident); ok && id.Name == "true" {
							passed = append(passed, k)
						}
					}
				}
			}
		}
	}
	if !maps.Equal(got, goTestFlags) {
		t.Errorf("cmd/go registers %v, goTestFlags holds %v", got, goTestFlags)
	}
	slices.Sort(passed)
	if !slices.Equal(passed, goTestPassed) {
		t.Errorf("cmd/go's passFlagToTest holds %q, goTestPassed %q", passed, goTestPassed)
	}
}

// goTestTimeouts reads every go test the text fixes, as bash parses it, with
// the findings goTestRuns and goTestTimeouts document.
func TestGoTestTimeouts_ReadEveryGoTestBashRuns(t *testing.T) {
	t.Parallel()
	m := func(n int) time.Duration { return time.Duration(n) * time.Minute }
	for _, c := range []struct {
		name, text string
		want       []time.Duration
		finding    string // a substring of the one finding expected; "" expects none
	}{
		{"a continuation", "go test \\\n  -timeout 5m ./...\n", []time.Duration{m(5)}, ""},
		{"a # inside quotes", "echo ' #' \" #\"; go test -timeout 5m ./...\n", []time.Duration{m(5)}, ""},
		{"an ANSI-C quote", "echo $'\\'' ' #'; go test ./...\n", nil, "no -timeout"},
		{"a here-document body", "cat <<E\nit's\nE\ngo test ./...\n", nil, "no -timeout"},
		{"a pipeline and a substitution", "x=$(go test -timeout 1m ./a) && go test -timeout 2m ./b | cat\n", []time.Duration{m(1), m(2)}, ""},
		{"a loop and a function", "f() { go test -timeout 1m ./a; }\nfor p in a; do go test -timeout 2m ./b; done\n", []time.Duration{m(1), m(2)}, ""},
		{"a quoted command word", "\"go\" test -timeout 3m ./...\n", []time.Duration{m(3)}, ""},
		{"go's -C flag", "go -C sub test -timeout 4m ./...\ngo -C=sub test -timeout 5m ./...\n", []time.Duration{m(4), m(5)}, ""},
		{"a wrapper", "env A=1 timeout 20m go test -timeout 5m ./...\n", []time.Duration{m(5)}, ""},
		{"-test.timeout", "go test -test.timeout=6m ./...\n", []time.Duration{m(6)}, ""},
		{"a -timeout after -args", "go test -timeout 5m ./... -args -timeout=0\n", []time.Duration{m(5)}, ""},
		{"packages only a run can know", "go test -timeout 5m \"${pkgs[@]}\" -run \"^(${n//,/|})\\$\"\n", []time.Duration{m(5)}, ""},
		{"a command only a run can name", "\"$GO\" test ./...\n", nil, "only a run can name"},
		{"go test in a string", "bash -c \"go test ./...\"\n", nil, "judges no run"},
		{"go test in a here-document", "bash <<E\ngo test ./...\nE\n", nil, "judges no run"},
		{"a -timeout only a run can know", "go test -timeout \"$T\" ./...\n", nil, "cannot place"},
		{"a flag only a run can know", "go test \"-timeout=$T\" ./...\n", nil, "cannot place"},
		{"text that does not parse", "go test (\n", nil, "does not parse"},
		{"escaped words", "\\go \\test ./...\n", nil, "no -timeout"},
		{"an escaped flag", "go test -timeout 5m ./... \\-timeout=0\n", nil, "switches the timeout off"},
		{"a quoted escape", "go test \"-timeout=5\\m\" ./...\n", nil, "-timeout"},
		{"a glob", "go test -timeout 5m ./... -timeo[u]t=0\n", nil, "cannot place"},
		{"a tilde", "go test -timeout 5m ~/x\n", []time.Duration{m(5)}, ""},
		{"an argument a run splits", "go test -timeout 5m $EXTRA ./...\n", nil, "cannot place"},
		{"a command a run splits", "$CMD ./...\n", nil, "only a run can name"},
		{"go test in a here-string", "bash <<<\"go test ./...\"\n", nil, "judges no run"},
		{"a subcommand only a run can name", "go \"$X\" -timeout=0 ./...\n", nil, "subcommand only a run can name"},
		{"a globbed subcommand", "go te?t ./...\n", nil, "subcommand only a run can name"},
		{"a glob across quoted parts", "go test -timeout 5m ./... [\"-\"]timeout=0\n", nil, "cannot place"},
		{"a glob character escaped", "go test -timeout 5m ./x[a\\]\n", []time.Duration{m(5)}, ""},
		{"a tilde after =", "go test -timeout 5m -coverprofile=~/c ./...\n", nil, "cannot place"},
		{"a quoted array", "go test -timeout 5m \"${a[@]}\"\n", []time.Duration{m(5)}, ""},
		{"go named by a path", "/usr/local/go/bin/go test -timeout 4m ./...\n./bin/go test -timeout 3m ./...\n", []time.Duration{m(4), m(3)}, ""},
		{"go named by a tilde path", "~/go/bin/go test ./...\n", nil, "only a run can name with test"},
		{"go test in a variable", "cmd=\"go test ./...\"; echo \"$cmd\"\n", nil, "judges no run"},
		{"a computed script to eval", "eval \"$cmd\"\n", nil, "script only a run can know"},
		{"a computed script to bash -c", "bash -c \"$CMD\"\n", nil, "script only a run can know"},
		{"a fixed script to bash -c", "bash -c 'echo ok'\n", nil, ""},
		{"an array as the command", "cmd=(go test ./...); \"${cmd[@]}\"\n", nil, "judges no run"},
		{"a brace expansion as the command", "{go,test} ./...\n", nil, "only a run can name"},
		{"a brace expansion as an argument", "go test -timeout 5m ./... {-timeout=0,-v}\n", nil, "cannot place"},
		{"go's --C flag", "go --C sub test -timeout 4m ./...\ngo --C=sub test -timeout 5m ./...\n", []time.Duration{m(4), m(5)}, ""},
		{"a value flag takes the next word", "go test -timeout 0 -run -timeout=5m ./...\n", nil, "switches the timeout off"},
		{"-- ends go's flags", "go test -timeout=0 ./... -- -timeout=5m\n", nil, "switches the timeout off"},
		{"a word after the packages ends go's flags", "go test -timeout=0 ./x -v junk -timeout=5m\n", nil, "switches the timeout off"},
		{"the binary's own timeout", "go test -timeout 5m ./... -args -test.timeout=0\n", nil, "switches the timeout off"},
		{"a flag go test does not know", "go test -frob -timeout 5m ./...\n", nil, "cannot place"},
		{"an unknown flag with a value", "go test -frob=1 -timeout 5m ./...\n", []time.Duration{m(5)}, ""},
		{"a double-dash flag", "go test --timeout=5m ./...\n", []time.Duration{m(5)}, ""},
		{"a value named timeout", "go test -run timeout -timeout 5m ./...\n", []time.Duration{m(5)}, ""},
		{"-timeout with no value", "go test ./... -timeout\n", nil, "cannot place"},
		{"--args", "go test -timeout 5m ./... --args -timeout=0\n", []time.Duration{m(5)}, ""},
		{"an arithmetic expansion", "go test -timeout \"$((T+5))m\" ./...\n", nil, "cannot place"},
		{"an unquoted arithmetic expansion", "go test -timeout 5m $((1)) ./...\n", nil, "cannot place"},
		{"a process substitution", "go test -timeout 5m ./... <(true)\n", []time.Duration{m(5)}, ""},
		{"a ? glob", "go test -timeout 5m ./... -timeou?=0\n", nil, "cannot place"},
		{"an escaped glob character", "go test -timeout 5m -run=x\\* ./...\n", []time.Duration{m(5)}, ""},
		{"a lone [", "go test -timeout 5m -run=[x ./...\n", []time.Duration{m(5)}, ""},
		{"a command a substitution names", "$(cat cmd.txt) ./...\n", nil, "only a run can name"},
		{"a boolean build flag", "go test -timeout 5m -buildvcs -timeout=0 ./...\n", nil, "switches the timeout off"},
		{"-artifacts", "go test -artifacts -timeout 5m ./...\n", []time.Duration{m(5)}, ""},
		{"a test. spelling go test does not read", "go test -timeout 5m -test.tags x ./...\n", nil, "cannot place"},
		{"a test. spelling go test reads", "go test -test.timeout 5m ./...\n", []time.Duration{m(5)}, ""},
		{"the binary reads nothing after --", "go test -timeout 0 ./... -- -test.timeout=5m\n", nil, "switches the timeout off"},
		{"the binary reads nothing after a word", "go test -timeout 0 . -v junk -test.timeout=5m\n", nil, "switches the timeout off"},
		{"the binary reads its flags after -args", "go test -timeout 5m . -args -test.v -test.timeout=0 x -test.timeout=5m\n", nil, "switches the timeout off"},
		{"an unknown flag with a value closes the package list", "go test -timeout 0 -frob=1 . -timeout 5m\n", nil, "switches the timeout off"},
		{"a flag the binary does not know", "go test -timeout 5m ./... -args -v\n", nil, "cannot place"},
		{"a value flag with no word after it", "go test -timeout 5m ./... -run\n", nil, "cannot place"},
		{"a lone ]", "go test -timeout 5m -run=x] ./...\n", []time.Duration{m(5)}, ""},
	} {
		got, f := goTestTimeouts("s", c.text)
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: timeouts = %v, want %v", c.name, got, c.want)
		}
		if c.finding == "" && len(f) != 0 || c.finding != "" && (len(f) != 1 || !strings.Contains(f[0], c.finding)) {
			t.Errorf("%s: findings = %q, want %q", c.name, f, c.finding)
		}
	}
}

// uploadArtifactOverwrite is the first major tag of actions/upload-artifact
// whose release declares the overwrite input, which v4.2.0 added. The check
// reads a major tag alone, so a pinned release or commit is refused.
const uploadArtifactOverwrite = 4

// durationsFindings reports each link of the test job's durations chain that
// does not hold: the test step names a file in TEST_DURATIONS, and a later
// step of an upload-artifact release that can overwrite uploads that file after
// a failure too, once per host, replacing an earlier attempt's upload and
// failing the job when the file is missing.
func durationsFindings(wf workflow) []string {
	j, ok := wf.Jobs["test"]
	if !ok {
		return []string{"jobs.test is not set"}
	}
	at := slices.IndexFunc(j.Steps, func(s step) bool { return s.Run == "scripts/test.sh" })
	if at < 0 {
		return []string{"no step of jobs.test runs scripts/test.sh"}
	}
	file := j.Steps[at].Env["TEST_DURATIONS"]
	if file == "" {
		return []string{"the test step sets no TEST_DURATIONS"}
	}
	up := slices.IndexFunc(j.Steps[at+1:], func(s step) bool {
		return strings.HasPrefix(s.Uses, "actions/upload-artifact@") && fmt.Sprint(s.With["path"]) == file
	})
	if up < 0 {
		return []string{"no step after the test step uploads " + file}
	}
	u := j.Steps[at+1+up]
	var findings []string
	major, err := strconv.Atoi(strings.TrimPrefix(u.Uses, "actions/upload-artifact@v"))
	if err != nil || major < uploadArtifactOverwrite {
		findings = append(findings, fmt.Sprintf("the upload uses %s; overwrite needs actions/upload-artifact@v%d or later", u.Uses, uploadArtifactOverwrite))
	}
	if u.ContinueOnError != nil {
		findings = append(findings, fmt.Sprintf("the upload sets continue-on-error %v, so a missing file passes", u.ContinueOnError))
	}
	names := map[string]bool{}
	for _, c := range j.Strategy.Matrix.combinations() {
		if c["name"] == "" || names[c["name"]] {
			findings = append(findings, fmt.Sprintf("the matrix name %q is empty or repeated, so two hosts' uploads collide", c["name"]))
		}
		names[c["name"]] = true
	}
	if u.If != "${{ !cancelled() }}" {
		findings = append(findings, fmt.Sprintf("the upload runs if %q; it must run after the tests fail", u.If))
	}
	if !strings.Contains(fmt.Sprint(u.With["name"]), "${{ matrix.name }}") {
		findings = append(findings, "the upload's name does not hold ${{ matrix.name }}, so the hosts' uploads collide")
	}
	if u.With["overwrite"] != true {
		findings = append(findings, "the upload does not set overwrite: true, so a re-run job cannot upload")
	}
	if u.With["if-no-files-found"] != "error" {
		findings = append(findings, "the upload does not set if-no-files-found: error, so a missing file passes")
	}
	return findings
}

// Each host's test job keeps its durations file.
func TestTestWorkflow_KeepsEachHostsTestDurations(t *testing.T) {
	t.Parallel()
	var wf workflow
	decodeYAML(t, fromRoot(".github/workflows/yammm_test.yml"), &wf)
	for _, f := range durationsFindings(wf) {
		t.Error(f)
	}
}

// The check itself, against each way the chain can break.
func TestDurationsFindings_RefusesEachBrokenLink(t *testing.T) {
	t.Parallel()
	const file = "${{ runner.temp }}/test-durations.tsv"
	upload := func(edit func(s *step)) step {
		s := step{Uses: "actions/upload-artifact@v7", If: "${{ !cancelled() }}", With: map[string]any{
			"name": "test-durations-${{ matrix.name }}", "path": file, "overwrite": true, "if-no-files-found": "error",
		}}
		if edit != nil {
			edit(&s)
		}
		return s
	}
	test := step{Run: "scripts/test.sh", Env: map[string]string{"TEST_DURATIONS": file}}
	hosts := func(names ...string) job {
		var j job
		for _, n := range names {
			j.Strategy.Matrix.Include = append(j.Strategy.Matrix.Include, map[string]string{"name": n, "os": "os-" + n})
		}
		return j
	}
	onHosts := func(j job, steps ...step) workflow {
		j.Steps = steps
		return workflow{Jobs: map[string]job{"test": j}}
	}
	wfWith := func(steps ...step) workflow { return onHosts(hosts("Linux", "Windows"), steps...) }
	if f := durationsFindings(wfWith(test, upload(nil))); len(f) != 0 {
		t.Fatalf("a whole chain reports %q", f)
	}
	for _, tt := range []struct {
		name, want string
		wf         workflow
	}{
		{"no job", "is not set", workflow{Jobs: map[string]job{}}},
		{"no test step", "runs scripts/test.sh", wfWith(upload(nil))},
		{"no file named", "sets no TEST_DURATIONS", wfWith(step{Run: "scripts/test.sh"}, upload(nil))},
		{"no upload", "uploads", wfWith(test)},
		{"an upload before the tests", "uploads", wfWith(upload(nil), test)},
		{"another file uploaded", "uploads", wfWith(test, upload(func(s *step) { s.With["path"] = "other.tsv" }))},
		{"another action uploading the file", "uploads", wfWith(test, upload(func(s *step) { s.Uses = "actions/cache@v5" }))},
		{"an upload skipped on failure", "after the tests fail", wfWith(test, upload(func(s *step) { s.If = "" }))},
		{"an upload only on success", "after the tests fail", wfWith(test, upload(func(s *step) { s.If = "${{ success() }}" }))},
		{"one name for every host", "collide", wfWith(test, upload(func(s *step) { s.With["name"] = "test-durations" }))},
		{"a name per run, not per host", "collide", wfWith(test, upload(func(s *step) { s.With["name"] = "test-durations-${{ github.run_id }}" }))},
		{"two hosts of one name", "repeated", onHosts(hosts("Linux", "Linux"), test, upload(nil))},
		{"a host with no name", "empty", onHosts(hosts("Linux", ""), test, upload(nil))},
		{"no overwrite", "re-run", wfWith(test, upload(func(s *step) { delete(s.With, "overwrite") }))},
		{"overwrite off", "re-run", wfWith(test, upload(func(s *step) { s.With["overwrite"] = false }))},
		{"a release with no overwrite", "or later", wfWith(test, upload(func(s *step) { s.Uses = "actions/upload-artifact@v3" }))},
		{"a missing file passing by default", "missing file", wfWith(test, upload(func(s *step) { delete(s.With, "if-no-files-found") }))},
		{"a missing file warned of", "missing file", wfWith(test, upload(func(s *step) { s.With["if-no-files-found"] = "warn" }))},
		{"a missing file ignored", "missing file", wfWith(test, upload(func(s *step) { s.With["if-no-files-found"] = "ignore" }))},
		{"an upload whose failure passes", "continue-on-error", wfWith(test, upload(func(s *step) { s.ContinueOnError = true }))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := durationsFindings(tt.wf)
			if len(f) != 1 || !strings.Contains(f[0], tt.want) {
				t.Errorf("findings = %q, want one naming %q", f, tt.want)
			}
		})
	}
}

// The check itself, against the inputs it exists to refuse.
func TestTimeoutFindings_RefusesEachWayAJobCanOutliveItsTests(t *testing.T) {
	t.Parallel()
	margins := map[string]time.Duration{"test": 15 * time.Minute}
	const okScript = "#!/usr/bin/env bash\n# go test -json runs the suite\ngo test -json -timeout=10m ./...\n"
	wfWith := func(jobMinutes, stepMinutes int, run string) workflow {
		return workflow{Jobs: map[string]job{"test": {
			TimeoutMinutes: jobMinutes,
			Steps:          []step{{Name: "Test", Run: run, TimeoutMinutes: stepMinutes}},
		}}}
	}
	testless := map[string]string{"test": "it vets every target"}
	for _, c := range []struct {
		name     string
		wf       workflow
		script   string
		margins  map[string]time.Duration
		testless map[string]string
		want     string // a substring of the one finding expected; "" expects none
	}{
		{"a job within its margin", wfWith(30, 0, "scripts/test.sh"), okScript, margins, nil, ""},
		{"a job exactly at its margin", wfWith(25, 0, "scripts/test.sh"), okScript, margins, nil, ""},
		{"a job one minute inside its margin", wfWith(24, 0, "scripts/test.sh"), okScript, margins, nil, "less than 15m0s past"},
		{"a job with no timeout", wfWith(0, 0, "scripts/test.sh"), okScript, margins, nil, "sets no timeout-minutes"},
		{"a job with no measured margin", wfWith(30, 0, "scripts/test.sh"), okScript, map[string]time.Duration{}, nil, "has no measured margin"},
		{"a job that runs no go test", wfWith(30, 0, "echo hello"), okScript, margins, nil, "runs no go test"},
		{"a go test with no -timeout", wfWith(30, 0, "scripts/test.sh"), "go test ./...\n", margins, nil, "no -timeout"},
		{"a go test spelled with two spaces", wfWith(30, 0, "scripts/test.sh"), okScript + "go  test -run X ./...\n", margins, nil, "no -timeout"},
		{"a -timeout only in a trailing comment", wfWith(30, 0, "scripts/test.sh"), "go test ./... # -timeout=10m\n", margins, nil, "no -timeout"},
		{"a -timeout on a continuation line", wfWith(30, 0, "scripts/test.sh"), "go test -json \\\n\t-timeout=10m ./...\n", margins, nil, ""},
		{"a zero -timeout", wfWith(30, 0, "scripts/test.sh"), "go test -timeout=0 ./...\n", margins, nil, "switches the timeout off"},
		{"a later -timeout overriding an earlier one", wfWith(30, 0, "scripts/test.sh"), "go test -timeout=10m -timeout=45m ./...\n", margins, nil, "past a go test timeout of 45m0s"},
		{"a go test inline in the step", wfWith(30, 0, "go test -timeout 20m ./x/"), okScript, margins, nil, "past a go test timeout of 20m0s"},
		{"a GitHub expression in the step", wfWith(30, 0, "go build -o \"${{ runner.temp }}/bin/\" x\ngo test -timeout 20m \"${{ matrix.pkg }}\""), okScript, margins, nil, "past a go test timeout of 20m0s"},
		{"a GitHub expression a run splits", wfWith(30, 0, "go test -timeout 20m ${{ matrix.pkgs }}"), okScript, margins, nil, "cannot place"},
		{"a command a GitHub expression names", wfWith(30, 0, "${{ matrix.cmd }}"), okScript, margins, nil, "only a run can name"},
		{"a -timeout a GitHub expression sets", wfWith(30, 0, "go test -timeout ${{ matrix.t }} ./x/"), okScript, margins, nil, "cannot place"},
		{"a GitHub expression in single quotes names the command", wfWith(30, 0, "'${{ matrix.go }}' test -timeout 20m ./x/"), okScript, margins, nil, "only a run can name"},
		{"a GitHub expression as a shell's script", wfWith(30, 0, "go test -timeout 10m ./x/\nbash -c '${{ matrix.cmd }}'"), okScript, margins, nil, "script only a run can know"},
		{"a GitHub expression over two lines", wfWith(30, 0, "go test -timeout 20m \"${{\n matrix.pkg }}\""), okScript, margins, nil, "past a go test timeout of 20m0s"},
		{"a step timeout inside the margin", wfWith(30, 20, "scripts/test.sh"), okScript, margins, nil, `step "Test" times out at 20m0s`},
		{"no job at all", workflow{}, okScript, margins, nil, "holds no job"},
		{"a job declared to run no test", wfWith(30, 0, "scripts/vet.sh"), okScript, nil, testless, ""},
		{"a job declared to run no test that runs one", wfWith(30, 0, "scripts/test.sh"), okScript, nil, testless, "declared to run no go test and runs one"},
		{"a job declared to run no test and given a margin", wfWith(30, 0, "scripts/vet.sh"), okScript, margins, testless, "has a measured margin and is declared"},
		{"a job declared to run no test with no timeout", wfWith(0, 0, "scripts/vet.sh"), okScript, nil, testless, "sets no timeout-minutes"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := timeoutFindings(c.wf, c.script, c.margins, c.testless)
			if c.want == "" {
				if len(got) != 0 {
					t.Errorf("findings %q, want none", got)
				}
				return
			}
			if len(got) != 1 || !strings.Contains(got[0], c.want) {
				t.Errorf("findings %q, want one naming %q", got, c.want)
			}
		})
	}
}

// knownWorkflows are the workflows the tracked tree must yield, so a pathspec
// that stops matching one fails rather than checking less. The CI workflow is
// the one spelled .yaml.
var knownWorkflows = []string{"ci.yaml", "publish_vscode.yml", "release.yml", "yammm_test.yml"}

// untimedJobs reports every job of workflows, keyed by file name, that runs
// steps with no positive timeout-minutes. A job that calls a reusable workflow
// cannot set one, which actionlint refuses as a syntax error; the called
// workflow's own jobs carry theirs.
func untimedJobs(workflows map[string]workflow) []string {
	if len(workflows) == 0 {
		return []string{"no workflow was read"}
	}
	var findings []string
	for _, file := range slices.Sorted(maps.Keys(workflows)) {
		wf := workflows[file]
		if len(wf.Jobs) == 0 {
			findings = append(findings, file+" holds no job")
			continue
		}
		for _, name := range slices.Sorted(maps.Keys(wf.Jobs)) {
			j := wf.Jobs[name]
			if j.Uses == "" && j.TimeoutMinutes <= 0 {
				findings = append(findings, fmt.Sprintf("%s: jobs.%s sets no timeout-minutes", file, name))
			}
		}
	}
	return findings
}

// trackedWorkflows decodes, keyed by file name, every workflow GitHub reads
// from root's tracked tree: the .yml and .yaml files directly under
// .github/workflows. GitHub runs no file in a subdirectory.
func trackedWorkflows(t *testing.T, root string) map[string]workflow {
	t.Helper()
	isWorkflow := func(rel string) bool {
		ext := path.Ext(rel)
		return path.Dir(rel) == ".github/workflows" && (ext == ".yml" || ext == ".yaml")
	}
	workflows := map[string]workflow{}
	for _, rel := range trackedFiles(t, root, isWorkflow) {
		file := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(file); errors.Is(err, fs.ErrNotExist) {
			// The index still lists a file the working tree has deleted.
			continue
		}
		var wf workflow
		decodeYAML(t, file, &wf)
		workflows[path.Base(rel)] = wf
	}
	return workflows
}

func TestTrackedWorkflows_ReadsWhatGitHubRuns(t *testing.T) {
	t.Parallel()
	f := newTree(t)
	jobNamed := func(name string) string {
		return "jobs:\n  " + name + ":\n    runs-on: ubuntu-24.04\n    steps:\n      - run: make\n"
	}
	f.write(".github/workflows/a.yml", jobNamed("top"))
	f.write(".github/workflows/b.yaml", jobNamed("b"))
	f.write(".github/workflows/sub/a.yml", jobNamed("nested"))
	f.write(".github/workflows/sub/c.yaml", jobNamed("nested"))
	f.write("nested/.github/workflows/d.yml", jobNamed("nested"))
	f.write(".github/workflows/gone.yml", jobNamed("gone"))
	f.write(".github/workflows/notes.txt", "not a workflow\n")
	f.index()
	if err := os.Remove(filepath.Join(f.dir, ".github", "workflows", "gone.yml")); err != nil {
		t.Fatal(err)
	}
	f.write(".github/workflows/untracked.yml", jobNamed("untracked"))

	got := trackedWorkflows(t, f.dir)
	if names := slices.Sorted(maps.Keys(got)); !slices.Equal(names, []string{"a.yml", "b.yaml"}) {
		t.Errorf("read %q, want [a.yml b.yaml]: a subdirectory, another tree's .github, a deleted file, an untracked file and a non-workflow are not run", names)
	}
	if _, ok := got["a.yml"].Jobs["top"]; !ok {
		t.Errorf("a.yml decoded as jobs %q, want the top-level file's job top", slices.Sorted(maps.Keys(got["a.yml"].Jobs)))
	}
}

// insideWorkTree asks about the root it is given, and a .git directory is no
// work tree.
func TestInsideWorkTree_AsksAboutItsRoot(t *testing.T) {
	t.Parallel()
	f := newTree(t)
	f.write("held.txt", "held\n")
	f.index()
	plain := t.TempDir()
	for dir, want := range map[string]bool{
		f.dir:                        true,
		filepath.Join(f.dir, ".git"): false,
		plain:                        false,
	} {
		if got := insideWorkTree(t, dir); got != want {
			t.Errorf("insideWorkTree(%s) = %v, want %v", dir, got, want)
		}
	}
}

// Outside a git work tree, as in the module cache's copy of a release, the
// files on disk are the tracked tree, and the same workflows are read.
func TestTrackedWorkflows_ReadsTheFilesOutsideAWorkTree(t *testing.T) {
	t.Parallel()
	f := newTree(t)
	probe := exec.CommandContext(t.Context(), "git", "rev-parse", "--is-inside-work-tree")
	probe.Dir = f.dir
	probe.Env = gittree.WithoutRepositoryVars(t, os.Environ())
	if probe.Run() == nil {
		t.Fatalf("the temporary directory %s lies inside a git work tree; set TMPDIR outside one", f.dir)
	}
	jobNamed := func(name string) string {
		return "jobs:\n  " + name + ":\n    runs-on: ubuntu-24.04\n    steps:\n      - run: make\n"
	}
	f.write(".github/workflows/a.yml", jobNamed("top"))
	f.write(".github/workflows/b.yaml", jobNamed("b"))
	f.write(".github/workflows/sub/a.yml", jobNamed("nested"))
	f.write(".github/workflows/dir.yml/notes.txt", "a directory named as a workflow\n")
	f.write("nested/.github/workflows/c.yml", jobNamed("nested"))
	f.write(".github/workflows/notes.txt", "not a workflow\n")

	got := trackedWorkflows(t, f.dir)
	if names := slices.Sorted(maps.Keys(got)); !slices.Equal(names, []string{"a.yml", "b.yaml"}) {
		t.Errorf("read %q, want [a.yml b.yaml]: a subdirectory, a directory, another tree's .github and a non-workflow are not run", names)
	}
	if _, ok := got["a.yml"].Jobs["top"]; !ok {
		t.Errorf("a.yml decoded as jobs %q, want the top-level file's job top", slices.Sorted(maps.Keys(got["a.yml"].Jobs)))
	}
}

// Every job of every tracked workflow carries a timeout, so a hung runner
// costs minutes rather than GitHub's six-hour default.
func TestWorkflows_EveryJobTimesOut(t *testing.T) {
	t.Parallel()
	workflows := trackedWorkflows(t, repoRoot)
	for _, want := range knownWorkflows {
		if _, ok := workflows[want]; !ok {
			t.Errorf("the tracked tree yields no %s", want)
		}
	}
	for _, f := range untimedJobs(workflows) {
		t.Error(f)
	}
}

// The check itself, against the inputs it exists to refuse.
func TestUntimedJobs_RefusesEachWayAJobCanRunUnbounded(t *testing.T) {
	t.Parallel()
	timed := job{TimeoutMinutes: 15, Steps: []step{{Run: "make"}}}
	for _, c := range []struct {
		name      string
		workflows map[string]workflow
		want      string // a substring of the one finding expected; "" expects none
	}{
		{"a timed job", map[string]workflow{"a.yml": {Jobs: map[string]job{"build": timed}}}, ""},
		{"a job with no timeout", map[string]workflow{"a.yml": {Jobs: map[string]job{"build": {Steps: []step{{Run: "make"}}}}}}, "a.yml: jobs.build sets no timeout-minutes"},
		{"a negative timeout", map[string]workflow{"a.yml": {Jobs: map[string]job{"build": {TimeoutMinutes: -1, Steps: []step{{Run: "make"}}}}}}, "jobs.build sets no timeout-minutes"},
		{"a step timeout alone", map[string]workflow{"a.yml": {Jobs: map[string]job{"build": {Steps: []step{{Run: "make", TimeoutMinutes: 15}}}}}}, "jobs.build sets no timeout-minutes"},
		{"a job that calls a reusable workflow", map[string]workflow{"a.yml": {Jobs: map[string]job{"test": {Uses: "./.github/workflows/t.yml"}, "build": timed}}}, ""},
		{"an untimed job after a timed one", map[string]workflow{"a.yml": {Jobs: map[string]job{"build": timed, "publish": {Steps: []step{{Run: "vsce"}}}}}}, "a.yml: jobs.publish"},
		{"an untimed job in a later file", map[string]workflow{"a.yml": {Jobs: map[string]job{"build": timed}}, "b.yaml": {Jobs: map[string]job{"publish": {Steps: []step{{Run: "vsce"}}}}}}, "b.yaml: jobs.publish"},
		{"a workflow with no job", map[string]workflow{"a.yml": {}}, "a.yml holds no job"},
		{"no workflow at all", map[string]workflow{}, "no workflow was read"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := untimedJobs(c.workflows)
			if c.want == "" {
				if len(got) != 0 {
					t.Errorf("findings %q, want none", got)
				}
				return
			}
			if len(got) != 1 || !strings.Contains(got[0], c.want) {
				t.Errorf("findings %q, want one naming %q", got, c.want)
			}
		})
	}
}

var matrixRef = regexp.MustCompile(`\$\{\{\s*matrix\.([A-Za-z0-9_-]+)\s*\}\}`)

// perCombination returns text once per matrix configuration, each matrix
// reference to a key the configuration holds replaced by its value and any
// other left as written, or text alone when there is no configuration.
func perCombination(text string, combos []map[string]string) []string {
	if len(combos) == 0 {
		return []string{text}
	}
	out := make([]string, 0, len(combos))
	for _, c := range combos {
		out = append(out, matrixRef.ReplaceAllStringFunc(text, func(ref string) string {
			if v, ok := c[matrixRef.FindStringSubmatch(ref)[1]]; ok {
				return v
			}
			return ref
		}))
	}
	return out
}

// floatingRunners reports each job of workflows, other than one that calls a
// reusable workflow, that names no runner label, can run on a label GitHub moves
// to a new image on its own schedule, such as ubuntu-latest, or runs on an
// expression the check cannot resolve to a label.
func floatingRunners(workflows map[string]workflow) []string {
	if len(workflows) == 0 {
		return []string{"no workflow was read"}
	}
	var findings []string
	for _, file := range slices.Sorted(maps.Keys(workflows)) {
		wf := workflows[file]
		for _, name := range slices.Sorted(maps.Keys(wf.Jobs)) {
			j := wf.Jobs[name]
			if j.Uses != "" {
				continue
			}
			at := fmt.Sprintf("%s: jobs.%s", file, name)
			if len(j.RunsOn) == 0 {
				findings = append(findings, at+" names no runner label")
				continue
			}
			seen := map[string]bool{}
			report := func(f string) {
				if !seen[f] {
					seen[f] = true
					findings = append(findings, f)
				}
			}
			combos := j.Strategy.Matrix.combinations()
			for _, label := range j.RunsOn {
				for _, l := range perCombination(label, combos) {
					switch {
					case strings.Contains(l, "${{"):
						report(fmt.Sprintf("%s runs on %q, an expression the check cannot resolve to a label", at, l))
					case slices.Contains(strings.Split(strings.ToLower(l), "-"), "latest"):
						report(fmt.Sprintf("%s runs on %q, a label GitHub moves to a new image", at, l))
					}
				}
			}
		}
	}
	return findings
}

// Every job names its image's OS release, so the gate moves to a new release
// only in a commit that says so. GitHub's weekly rebuilds of one release still
// change its preinstalled tools.
func TestWorkflows_EveryJobNamesItsImage(t *testing.T) {
	t.Parallel()
	for _, f := range floatingRunners(trackedWorkflows(t, repoRoot)) {
		t.Error(f)
	}
}

func TestRunnerLabels_DecodesEveryFormGitHubAccepts(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, yaml string
		want       []string
	}{
		{"one label", "runs-on: ubuntu-24.04\n", []string{"ubuntu-24.04"}},
		{"a list", "runs-on: [self-hosted, ubuntu-latest]\n", []string{"self-hosted", "ubuntu-latest"}},
		{"a mapping", "runs-on:\n  group: builders\n  labels: [macos-latest]\n", []string{"macos-latest"}},
		{"a mapping with one label", "runs-on:\n  labels: windows-latest\n", []string{"windows-latest"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var j job
			if err := yaml.Unmarshal([]byte(c.yaml), &j); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(j.RunsOn, c.want) {
				t.Errorf("runs-on decoded as %q, want %q", j.RunsOn, c.want)
			}
		})
	}
	for _, text := range []string{"runs-on: [{a: b}]\n", "runs-on:\n  labels: {a: b}\n"} {
		var j job
		if err := yaml.Unmarshal([]byte(text), &j); err == nil {
			t.Errorf("%q decoded as %q with no error; a label that is not a string must refuse", text, j.RunsOn)
		}
	}
}

// decodeJob decodes one job from its YAML text.
func decodeJob(t *testing.T, text string) job {
	t.Helper()
	var j job
	if err := yaml.Unmarshal([]byte(text), &j); err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	return j
}

// GitHub's two worked examples with the configurations its documentation lists,
// a case of its documented exclude rules, and matrices GitHub fills at run time.
func TestMatrixCombinations_ExpandsAsGitHubDocuments(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, yaml string
		want       []string // each configuration as sorted key=value pairs
	}{
		{
			"include merges into originals and adds the rest",
			"strategy:\n  matrix:\n    fruit: [apple, pear]\n    animal: [cat, dog]\n    include:\n" +
				"      - color: green\n      - color: pink\n        animal: cat\n      - fruit: apple\n        shape: circle\n" +
				"      - fruit: banana\n      - fruit: banana\n        animal: cat\n",
			[]string{
				"animal=cat color=pink fruit=apple shape=circle",
				"animal=dog color=green fruit=apple shape=circle",
				"animal=cat color=pink fruit=pear",
				"animal=dog color=green fruit=pear",
				"fruit=banana",
				"animal=cat fruit=banana",
			},
		},
		{
			"include only",
			"strategy:\n  matrix:\n    include:\n      - site: production\n        datacenter: site-a\n      - site: staging\n        datacenter: site-b\n",
			[]string{"datacenter=site-a site=production", "datacenter=site-b site=staging"},
		},
		{
			"exclude matches partly, and include adds back after it",
			"strategy:\n  matrix:\n    os: [a, b]\n    node: [14, 16]\n    exclude:\n      - os: b\n    include:\n      - os: b\n        node: 16\n",
			[]string{"node=14 os=a", "node=16 os=a", "node=16 os=b"},
		},
		{"a matrix from an expression", "strategy:\n  matrix: ${{ fromJSON(needs.plan.outputs.m) }}\n", nil},
		{"an axis from an expression", "strategy:\n  matrix:\n    os: ${{ fromJSON(inputs.hosts) }}\n", nil},
		{"an axis from an expression beside include", "strategy:\n  matrix:\n    os: ${{ fromJSON(inputs.hosts) }}\n    include:\n      - os: ubuntu-24.04\n", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, combo := range decodeJob(t, c.yaml).Strategy.Matrix.combinations() {
				var pairs []string
				for _, k := range slices.Sorted(maps.Keys(combo)) {
					pairs = append(pairs, k+"="+combo[k])
				}
				got = append(got, strings.Join(pairs, " "))
			}
			if !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(c.want))) {
				t.Errorf("configurations %q, want %q", got, c.want)
			}
		})
	}
}

// The check itself, against the inputs it exists to refuse.
func TestFloatingRunners_RefusesEachWayAJobCanFloat(t *testing.T) {
	t.Parallel()
	on := func(labels ...string) job { return job{RunsOn: labels, Steps: []step{{Run: "make"}}} }
	withInclude := func(label string, entries ...map[string]string) job {
		j := on(label)
		j.Strategy.Matrix.Include = entries
		return j
	}
	fromYAML := func(text string) job { return decodeJob(t, text+"steps:\n  - run: make\n") }
	one := func(j job) map[string]workflow {
		return map[string]workflow{"a.yml": {Jobs: map[string]job{"build": j}}}
	}
	for _, c := range []struct {
		name      string
		workflows map[string]workflow
		want      string // a substring of the one finding expected; "" expects none
	}{
		{"an image label", one(on("ubuntu-24.04")), ""},
		{"a -latest label", one(on("ubuntu-latest")), `a.yml: jobs.build runs on "ubuntu-latest"`},
		{"a latest segment inside a label", one(on("macos-latest-large")), `"macos-latest-large"`},
		{"a floating label in a list", one(on("self-hosted", "windows-latest")), `"windows-latest"`},
		{"a floating label in capitals", one(on("Ubuntu-Latest")), `"Ubuntu-Latest"`},
		{"latest inside a word", one(on("mylatestbox")), ""},
		{"a matrix of images", one(withInclude("${{ matrix.os }}", map[string]string{"os": "ubuntu-24.04"}, map[string]string{"os": "macos-26"})), ""},
		{"a matrix with one floating entry", one(withInclude("${{ matrix.os }}", map[string]string{"os": "macos-26"}, map[string]string{"os": "windows-latest"})), `runs on "windows-latest"`},
		{"one floating label from two entries", one(withInclude("${{ matrix.os }}", map[string]string{"os": "ubuntu-latest", "go": "1"}, map[string]string{"os": "ubuntu-latest", "go": "2"})), `runs on "ubuntu-latest"`},
		{"a matrix reference with no spaces", one(withInclude("${{matrix.os}}", map[string]string{"os": "ubuntu-latest"})), `runs on "ubuntu-latest"`},
		{"a key other than os", one(withInclude("${{ matrix.runner }}", map[string]string{"os": "ubuntu-24.04", "runner": "ubuntu-latest"})), `runs on "ubuntu-latest"`},
		{"a key with an underscore", one(withInclude("${{ matrix.runner_os }}", map[string]string{"runner_os": "ubuntu-latest"})), `runs on "ubuntu-latest"`},
		{"a matrix key an entry lacks", one(withInclude("${{ matrix.os }}", map[string]string{"name": "Linux"})), "cannot resolve"},
		{"a reference with no spaces an entry lacks", one(withInclude("${{matrix.os}}", map[string]string{"name": "Linux"})), "cannot resolve"},
		{"a matrix reference with no configuration", one(withInclude("${{ matrix.os }}")), "cannot resolve"},
		{"an expression that names no matrix key", one(on("${{ inputs.runner }}")), `"${{ inputs.runner }}", an expression the check cannot resolve`},
		{"a floating axis beside include", one(fromYAML("runs-on: ${{ matrix.os }}\nstrategy:\n  matrix:\n    os: [ubuntu-latest]\n    include:\n      - os: ubuntu-24.04\n")), `runs on "ubuntu-latest"`},
		{"an axis of images", one(fromYAML("runs-on: ${{ matrix.os }}\nstrategy:\n  matrix:\n    os: [ubuntu-24.04, macos-26]\n")), ""},
		{"a floating value excluded", one(fromYAML("runs-on: ${{ matrix.os }}\nstrategy:\n  matrix:\n    os: [ubuntu-24.04, ubuntu-latest]\n    exclude:\n      - os: ubuntu-latest\n")), ""},
		{"an axis that leaves the label unset", one(fromYAML("runs-on: ${{ matrix.os }}\nstrategy:\n  matrix:\n    go: [\"1.26\"]\n    include:\n      - go: \"1.27\"\n        os: ubuntu-24.04\n")), `"${{ matrix.os }}", an expression the check cannot resolve`},
		{"a matrix from an expression", one(fromYAML("runs-on: ${{ matrix.os }}\nstrategy:\n  matrix: ${{ fromJSON(needs.plan.outputs.m) }}\n")), "cannot resolve"},
		{"an axis from an expression beside include", one(fromYAML("runs-on: ${{ matrix.os }}\nstrategy:\n  matrix:\n    os: ${{ fromJSON(inputs.hosts) }}\n    include:\n      - os: ubuntu-24.04\n")), "cannot resolve"},
		{"a job that names no runner", one(job{Steps: []step{{Run: "make"}}}), "a.yml: jobs.build names no runner label"},
		{"a job that calls a reusable workflow", one(job{Uses: "./.github/workflows/t.yml"}), ""},
		{"a floating job in a later file", map[string]workflow{"a.yml": {Jobs: map[string]job{"build": on("ubuntu-24.04")}}, "b.yaml": {Jobs: map[string]job{"publish": on("ubuntu-latest")}}}, "b.yaml: jobs.publish"},
		{"no workflow at all", map[string]workflow{}, "no workflow was read"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := floatingRunners(c.workflows)
			if c.want == "" {
				if len(got) != 0 {
					t.Errorf("findings %q, want none", got)
				}
				return
			}
			if len(got) != 1 || !strings.Contains(got[0], c.want) {
				t.Errorf("findings %q, want one naming %q", got, c.want)
			}
		})
	}
}

func TestReleaseWorkflow_BuildsOnlyAfterTheTestWorkflow(t *testing.T) {
	t.Parallel()
	var wf workflow
	decodeYAML(t, fromRoot(".github/workflows/release.yml"), &wf)

	var callers []string
	for name, j := range wf.Jobs {
		if j.Uses == testWorkflow {
			callers = append(callers, name)
		}
	}
	if len(callers) == 0 {
		t.Fatalf("no job calls %s", testWorkflow)
	}
	for name, j := range wf.Jobs {
		if j.If != "" {
			t.Errorf("job %s runs only if %q: a condition can skip the tests or build after they fail", name, j.If)
		}
		if len(j.Steps) == 0 {
			continue
		}
		if !slices.ContainsFunc(j.Needs, func(n string) bool { return slices.Contains(callers, n) }) {
			t.Errorf("job %s needs %q, none of which calls the test workflow %q", name, j.Needs, callers)
		}
	}
}

// preCommitConfig is .pre-commit-config.yaml as the hook check reads it: every
// top-level key, and each hook's whole mapping.
type preCommitConfig struct {
	keys  []string
	Repos []struct {
		Repo  string           `yaml:"repo"`
		Hooks []map[string]any `yaml:"hooks"`
	} `yaml:"repos"`
}

func (c *preCommitConfig) UnmarshalYAML(n *yaml.Node) error {
	type plain preCommitConfig
	if err := n.Decode((*plain)(c)); err != nil {
		return err
	}
	c.keys = mappingKeys(n)
	return nil
}

// The files patterns of the gate hooks: pre-commit searches each staged path
// with a hook's pattern, and runs the hook when one matches.
const (
	lintConfigFiles = `(^\.golangci\.yml$)|(^go\.(mod|sum)$)`
	goFiles         = `\.go$`
	goBuildFiles    = `(\.go$)|(\bgo\.mod$)|(\bgo\.sum$)`
)

// gateHooks holds each lint, vet and test hook to one form: every key its
// mapping sets beside id and name, with its value. Any other key is a finding,
// since args, types and exclude each change what a hook runs or when it runs.
var gateHooks = map[string]map[string]any{
	"golangci-lint-config": {"language": "system", "pass_filenames": false, "files": lintConfigFiles, "entry": "scripts/lintconfig.sh"},
	"golangci-lint":        {"language": "system", "stages": []any{"pre-commit"}, "pass_filenames": false, "files": goFiles, "entry": "scripts/lint.sh --host"},
	"go-vet":               {"language": "system", "stages": []any{"pre-commit"}, "pass_filenames": false, "files": goBuildFiles, "entry": "scripts/vet.sh --host"},
	"go-test":              {"language": "system", "stages": []any{"pre-commit"}, "pass_filenames": false, "always_run": true, "entry": "scripts/committest.sh"},
	"golangci-lint-full":   {"language": "system", "stages": []any{"manual"}, "pass_filenames": false, "files": goFiles, "entry": "scripts/lint.sh"},
	"go-vet-full":          {"language": "system", "stages": []any{"manual"}, "pass_filenames": false, "files": goBuildFiles, "entry": "scripts/vet.sh"},
	"go-test-full":         {"language": "system", "stages": []any{"manual"}, "pass_filenames": false, "always_run": true, "entry": "scripts/test.sh"},
}

// hookFindings reports each way config departs from the two gates: a top-level
// key beside repos, two hooks of one id, a gate hook that is missing, not local
// or outside its one form, and any other hook that names stages. A hook that
// names none runs at the stages its own manifest gives it.
func hookFindings(config preCommitConfig) []string {
	var findings []string
	for _, k := range config.keys {
		if k != "repos" {
			findings = append(findings, fmt.Sprintf("the configuration sets %s, which the check does not know leaves every hook as written", k))
		}
	}
	seen := map[string]bool{}
	for _, repo := range config.Repos {
		for _, h := range repo.Hooks {
			id := fmt.Sprint(h["id"])
			if seen[id] {
				findings = append(findings, "two hooks are named "+id)
				continue
			}
			seen[id] = true
			want, gate := gateHooks[id]
			if !gate {
				if stages, ok := h["stages"]; ok {
					findings = append(findings, fmt.Sprintf("hook %s names stages %v, so a gate may leave it out", id, stages))
				}
				continue
			}
			if repo.Repo != "local" {
				findings = append(findings, fmt.Sprintf("hook %s comes from %s, want a local hook", id, repo.Repo))
			}
			keys := maps.Clone(h)
			maps.Copy(keys, want)
			for _, k := range slices.Sorted(maps.Keys(keys)) {
				got, set := h[k]
				w, wanted := want[k]
				switch {
				case k == "id" || k == "name":
				case !wanted:
					findings = append(findings, fmt.Sprintf("hook %s sets %s: %v, which is outside its one form", id, k, got))
				case !set:
					findings = append(findings, fmt.Sprintf("hook %s sets no %s, want %v", id, k, w))
				case !reflect.DeepEqual(got, w):
					findings = append(findings, fmt.Sprintf("hook %s sets %s: %v, want %v", id, k, got, w))
				}
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(gateHooks)) {
		if !seen[id] {
			findings = append(findings, "no hook "+id)
		}
	}
	return findings
}

// A commit runs the commit gate: the linter config check, the linter and vet for
// this host's build, and scripts/committest.sh. The full gate's three hooks run
// at the manual stage alone, which `make gate` names, and no other hook names a
// stage.
func TestPreCommitHooks_RunTheGateScripts(t *testing.T) {
	t.Parallel()
	var config preCommitConfig
	decodeYAML(t, fromRoot(".pre-commit-config.yaml"), &config)
	for _, f := range hookFindings(config) {
		t.Error(f)
	}
	for pattern, fires := range map[string][]string{
		lintConfigFiles: {".golangci.yml", "go.mod", "go.sum"},
		goFiles:         {"schema/load.go", "location/host_path_windows.go"},
		goBuildFiles:    {"schema/load.go", "go.mod", "go.sum"},
	} {
		files := regexp.MustCompile(pattern)
		for _, path := range fires {
			if !files.MatchString(path) {
				t.Errorf("a hook with the files pattern %q does not fire when %s changes", pattern, path)
			}
		}
	}
}

// The check itself: the repository's configuration draws no finding, and each
// departure from the two gates draws the findings that name it.
func TestHookFindings_RefusesEachDepartureFromTheTwoGates(t *testing.T) {
	t.Parallel()
	// Each row edits its own decoding of the repository's configuration.
	hook := func(t *testing.T, c *preCommitConfig, id string) map[string]any {
		t.Helper()
		for _, repo := range c.Repos {
			for _, h := range repo.Hooks {
				if h["id"] == id {
					return h
				}
			}
		}
		t.Fatalf("the configuration holds no hook %s", id)
		return nil
	}
	set := func(id, key string, value any) func(*testing.T, *preCommitConfig) {
		return func(t *testing.T, c *preCommitConfig) {
			t.Helper()
			hook(t, c, id)[key] = value
		}
	}
	drop := func(id, key string) func(*testing.T, *preCommitConfig) {
		return func(t *testing.T, c *preCommitConfig) {
			t.Helper()
			delete(hook(t, c, id), key)
		}
	}
	local := func(t *testing.T, c *preCommitConfig, id string) (repo, at int) {
		t.Helper()
		for r, rp := range c.Repos {
			for i, h := range rp.Hooks {
				if h["id"] == id {
					return r, i
				}
			}
		}
		t.Fatalf("the configuration holds no hook %s", id)
		return 0, 0
	}
	for _, tt := range []struct {
		name string
		edit func(*testing.T, *preCommitConfig)
		want []string // a substring of each finding expected, in order
	}{
		{"the repository's configuration", func(*testing.T, *preCommitConfig) {}, nil},
		{"default_stages", func(_ *testing.T, c *preCommitConfig) { c.keys = append(c.keys, "default_stages") }, []string{"the configuration sets default_stages"}},
		{"a top-level exclude", func(_ *testing.T, c *preCommitConfig) { c.keys = append(c.keys, "exclude") }, []string{"the configuration sets exclude"}},
		{"args on a full hook", set("golangci-lint-full", "args", []any{"--host"}), []string{"hook golangci-lint-full sets args: [--host], which is outside its one form"}},
		{"types on a commit hook", set("golangci-lint", "types", []any{"python"}), []string{"hook golangci-lint sets types"}},
		{"an exclude on a gate hook", set("go-vet-full", "exclude", "^schema/"), []string{"hook go-vet-full sets exclude"}},
		{"pass_filenames dropped", drop("go-test-full", "pass_filenames"), []string{"hook go-test-full sets no pass_filenames"}},
		{"pass_filenames on", set("golangci-lint", "pass_filenames", true), []string{"hook golangci-lint sets pass_filenames: true, want false"}},
		{"always_run dropped", drop("go-test", "always_run"), []string{"hook go-test sets no always_run"}},
		{"the full test at a commit", set("go-test", "entry", "scripts/test.sh"), []string{"hook go-test sets entry: scripts/test.sh, want scripts/committest.sh"}},
		{"the commit test by hand", set("go-test-full", "entry", "scripts/committest.sh"), []string{"hook go-test-full sets entry"}},
		{"every target vetted at a commit", set("go-vet", "entry", "scripts/vet.sh"), []string{"hook go-vet sets entry"}},
		{"a narrower files pattern", set("golangci-lint-full", "files", `^schema/.*\.go$`), []string{"hook golangci-lint-full sets files"}},
		{"another language", set("go-vet", "language", "golang"), []string{"hook go-vet sets language"}},
		{"a full hook at a commit too", set("go-test-full", "stages", []any{"manual", "pre-commit"}), []string{"hook go-test-full sets stages"}},
		{"a full hook at a merge commit too", set("go-test-full", "stages", []any{"manual", "pre-merge-commit"}), []string{"hook go-test-full sets stages"}},
		{"a full hook at every stage", drop("go-vet-full", "stages"), []string{"hook go-vet-full sets no stages"}},
		{"a commit hook by hand too", set("go-test", "stages", []any{"pre-commit", "manual"}), []string{"hook go-test sets stages"}},
		{"the legacy stage name", set("go-vet", "stages", []any{"commit"}), []string{"hook go-vet sets stages: [commit], want [pre-commit]"}},
		{"a stage on the config hook", set("golangci-lint-config", "stages", []any{"pre-commit"}), []string{"hook golangci-lint-config sets stages"}},
		{"a stage on another local hook", set("gofumpt", "stages", []any{"pre-commit"}), []string{"hook gofumpt names stages"}},
		{"a stage on a third-party hook", set("actionlint", "stages", []any{"pre-commit"}), []string{"hook actionlint names stages"}},
		{"a gate hook renamed", set("go-vet", "id", "vet"), []string{"hook vet names stages", "no hook go-vet"}},
		{"a gate hook removed", func(t *testing.T, c *preCommitConfig) {
			t.Helper()
			r, i := local(t, c, "go-test-full")
			c.Repos[r].Hooks = slices.Delete(c.Repos[r].Hooks, i, i+1)
		}, []string{"no hook go-test-full"}},
		{"two hooks of one id", func(t *testing.T, c *preCommitConfig) {
			t.Helper()
			r, i := local(t, c, "go-test")
			c.Repos[r].Hooks = append(c.Repos[r].Hooks, maps.Clone(c.Repos[r].Hooks[i]))
		}, []string{"two hooks are named go-test"}},
		{"a gate hook from another repository", func(t *testing.T, c *preCommitConfig) {
			t.Helper()
			r, _ := local(t, c, "go-test-full")
			c.Repos[r].Repo = "https://example.com/hooks"
		}, []string{"hook golangci-lint-full comes from", "hook go-vet-full comes from", "hook go-test-full comes from"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var config preCommitConfig
			decodeYAML(t, fromRoot(".pre-commit-config.yaml"), &config)
			tt.edit(t, &config)
			got := hookFindings(config)
			if len(got) != len(tt.want) {
				t.Fatalf("findings %q, want %d naming %q", got, len(tt.want), tt.want)
			}
			for i, want := range tt.want {
				if !strings.Contains(got[i], want) {
					t.Errorf("finding %q, want one naming %q", got[i], want)
				}
			}
		})
	}
}

// gateCommand is the one command that runs the full gate. --hook-stage manual
// selects the three hooks no commit runs, beside every hook that names no stage.
const gateCommand = "pre-commit run --all-files --hook-stage manual"

// ruleFindings holds target's rule in the makefile text to one form: a .PHONY
// line naming it alone, the line "target:", one recipe line holding command,
// and no other line that names the target before a colon. It does not read a
// conditional, an included file, a target named through a variable, or an
// assignment to SHELL or .RECIPEPREFIX.
func ruleFindings(makefile, target, command string) []string {
	lines := strings.Split(makefile, "\n")
	var rules []int
	for i, l := range lines {
		if strings.HasPrefix(l, "\t") || strings.HasPrefix(l, "#") {
			continue
		}
		if before, _, ok := strings.Cut(l, ":"); ok && slices.Contains(strings.Fields(before), target) {
			rules = append(rules, i)
		}
	}
	if len(rules) != 1 {
		return []string{fmt.Sprintf("the Makefile names %s before a colon on %d lines, want one rule", target, len(rules))}
	}
	at := rules[0]
	line := func(i int) string {
		if i < 0 || i >= len(lines) {
			return ""
		}
		return lines[i]
	}
	var findings []string
	if lines[at] != target+":" {
		findings = append(findings, fmt.Sprintf("the rule line is %q, want %q", lines[at], target+":"))
	}
	if line(at-1) != ".PHONY: "+target {
		findings = append(findings, fmt.Sprintf("the line before the rule is %q, want %q", line(at-1), ".PHONY: "+target))
	}
	if line(at+1) != "\t"+command {
		findings = append(findings, fmt.Sprintf("make %s runs %q, want %q", target, strings.TrimPrefix(line(at+1), "\t"), command))
	}
	// make reads a tab line after a blank line or a comment as one more command.
	for _, l := range lines[min(at+2, len(lines)):] {
		if second, ok := strings.CutPrefix(l, "\t"); ok {
			findings = append(findings, fmt.Sprintf("make %s runs a second command, %q", target, second))
		}
		if l != "" && !strings.HasPrefix(l, "#") {
			break
		}
	}
	return findings
}

// `make gate` is the full gate's command, and `make test` its test definition.
func TestMakefile_GateRunsTheFullGate(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(fromRoot("Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	for target, command := range map[string]string{"gate": gateCommand, "test": "scripts/test.sh"} {
		for _, f := range ruleFindings(string(b), target, command) {
			t.Error(f)
		}
	}
}

// The check itself: the one form draws no finding, and each other form in the
// table draws one.
func TestRuleFindings_RefusesEachOtherForm(t *testing.T) {
	t.Parallel()
	const rule = ".PHONY: gate\ngate:\n\tgo\n"
	for _, tt := range []struct {
		name, makefile, want string // want is a substring of the one finding expected; "" expects none
	}{
		{"the one form", "x := 1\n\n" + rule + "\nnext:\n\tother\n", ""},
		{"the one form at the end of the file", rule, ""},
		{"a comment after the recipe", rule + "# done\nnext:\n", ""},
		{"no rule", "gateway:\n\tgo\n", "on 0 lines"},
		{"a rule in a comment alone", "# gate:\n#\tgo\n", "on 0 lines"},
		{"a second rule", rule + "\ngate:\n\tother\n", "on 2 lines"},
		{"a target-specific variable", rule + "\ngate: export SKIP := x\n", "on 2 lines"},
		{"a double-colon pair", ".PHONY: gate\ngate::\n\tgo\ngate::\n\tother\n", "on 2 lines"},
		{"a second target on the rule line", ".PHONY: gate\ngate full:\n\tgo\n", `the rule line is "gate full:"`},
		{"a prerequisite", ".PHONY: gate\ngate: lint\n\tgo\n", `the rule line is "gate: lint"`},
		{"a space before the colon", ".PHONY: gate\ngate :\n\tgo\n", `the rule line is "gate :"`},
		{"not phony", "\ngate:\n\tgo\n", "the line before the rule"},
		{"phony beside another target", ".PHONY: gate lint\ngate:\n\tgo\n", "the line before the rule"},
		{"another command", ".PHONY: gate\ngate:\n\tother\n", `runs "other", want "go"`},
		{"no command", ".PHONY: gate\ngate:\n\nnext:\n", `runs "", want "go"`},
		{"a second command", rule + "\tother\n", `a second command, "other"`},
		{"a second command after a comment", rule + "# and\n\tother\n", `a second command, "other"`},
		{"a second command after a blank line", rule + "\n\tother\n", `a second command, "other"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ruleFindings(tt.makefile, "gate", "go")
			if tt.want == "" {
				if len(got) != 0 {
					t.Errorf("findings %q, want none", got)
				}
				return
			}
			if len(got) != 1 || !strings.Contains(got[0], tt.want) {
				t.Errorf("findings %q, want one naming %q", got, tt.want)
			}
		})
	}
}

// CI's pre-commit job runs the commit stage and skips the commit gate's four
// lint, vet and test hooks, whose checks the test workflow's jobs run.
func TestCIWorkflow_PreCommitJobSkipsTheGateHooks(t *testing.T) {
	t.Parallel()
	var ci workflow
	decodeYAML(t, fromRoot(".github/workflows/ci.yaml"), &ci)
	var runs []step
	for _, s := range ci.Jobs["pre-commit"].Steps {
		if strings.Contains(s.Run, "pre-commit run") {
			runs = append(runs, s)
		}
	}
	if len(runs) != 1 {
		t.Fatalf("CI's pre-commit job runs pre-commit in %d steps, want one", len(runs))
	}
	if got := strings.TrimSpace(runs[0].Run); got != "pre-commit run --all-files" {
		t.Errorf("CI's pre-commit job runs %q, want pre-commit run --all-files, the commit stage", got)
	}
	var commit []string
	for id, h := range gateHooks {
		if !reflect.DeepEqual(h["stages"], []any{"manual"}) {
			commit = append(commit, id)
		}
	}
	if got := slices.Sorted(slices.Values(skippedHooks(runs[0].Env["SKIP"]))); !slices.Equal(got, slices.Sorted(slices.Values(commit))) {
		t.Errorf("CI's pre-commit job skips %q, want the hooks that run at a commit, %q", got, slices.Sorted(slices.Values(commit)))
	}
}

// shellcheck reads every file under scripts/ and every shell script of the
// Claude plugin's hooks, at commit and in CI's pre-commit job, which skips only
// the hooks whose checks the host jobs run.
func TestPreCommitHooks_ShellcheckReadsEveryScript(t *testing.T) {
	t.Parallel()
	var config struct {
		Repos []struct {
			Repo  string `yaml:"repo"`
			Rev   string `yaml:"rev"`
			Hooks []struct {
				ID      string   `yaml:"id"`
				Files   string   `yaml:"files"`
				Exclude string   `yaml:"exclude"`
				Args    []string `yaml:"args"`
				Stages  []string `yaml:"stages"`
			} `yaml:"hooks"`
		} `yaml:"repos"`
	}
	decodeYAML(t, fromRoot(".pre-commit-config.yaml"), &config)
	found := false
	for _, repo := range config.Repos {
		for _, h := range repo.Hooks {
			if h.ID != "shellcheck" {
				continue
			}
			found = true
			if repo.Repo != "https://github.com/shellcheck-py/shellcheck-py" || repo.Rev == "" {
				t.Errorf("shellcheck comes from %s at %q, want the pinned shellcheck-py", repo.Repo, repo.Rev)
			}
			if !slices.Contains(h.Args, "--external-sources") {
				t.Errorf("shellcheck args %q do not follow sourced files", h.Args)
			}
			if len(h.Stages) != 0 || h.Exclude != "" {
				t.Errorf("shellcheck is narrowed by stages %q or exclude %q", h.Stages, h.Exclude)
			}
			files, err := regexp.Compile(h.Files)
			if err != nil {
				t.Fatalf("shellcheck files pattern %q: %v", h.Files, err)
			}
			scripts, err := filepath.Glob(fromRoot("scripts/*"))
			if err != nil {
				t.Fatal(err)
			}
			hooks, err := filepath.Glob(fromRoot("claude-plugin/hooks/*.sh"))
			if err != nil {
				t.Fatal(err)
			}
			if len(scripts) == 0 || len(hooks) == 0 {
				t.Fatalf("found %d scripts and %d plugin hook scripts; the globs read nothing", len(scripts), len(hooks))
			}
			for _, abs := range append(scripts, hooks...) {
				rel, err := filepath.Rel(fromRoot("."), abs)
				if err != nil {
					t.Fatal(err)
				}
				if rel = filepath.ToSlash(rel); !files.MatchString(rel) {
					t.Errorf("shellcheck does not read %s (files %q)", rel, h.Files)
				}
			}
		}
	}
	if !found {
		t.Fatal("no shellcheck hook")
	}

	var ci workflow
	decodeYAML(t, fromRoot(".github/workflows/ci.yaml"), &ci)
	ran := false
	for _, s := range ci.Jobs["pre-commit"].Steps {
		if !strings.Contains(s.Run, "pre-commit run") {
			continue
		}
		ran = true
		if slices.Contains(skippedHooks(s.Env["SKIP"]), "shellcheck") {
			t.Errorf("CI's pre-commit job skips shellcheck (SKIP=%s)", s.Env["SKIP"])
		}
	}
	if !ran {
		t.Error("CI's pre-commit job runs no pre-commit")
	}
}

// skippedHooks returns the hook ids a SKIP value names, read as pre-commit
// reads it: split on commas, each entry stripped of spaces, empty entries
// dropped.
func skippedHooks(skip string) []string {
	var ids []string
	for id := range strings.SplitSeq(skip, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func TestSkippedHooks(t *testing.T) {
	t.Parallel()
	for skip, want := range map[string][]string{
		"":                          nil,
		"shellcheck":                {"shellcheck"},
		"golangci-lint, shellcheck": {"golangci-lint", "shellcheck"},
		" shellcheck ,,":            {"shellcheck"},
	} {
		if got := skippedHooks(skip); !slices.Equal(got, want) {
			t.Errorf("skippedHooks(%q) = %q, want %q", skip, got, want)
		}
	}
}

// extensionCachePath matches the line that builds the directory runTest.mjs
// downloads VS Code to, and extensionRunTests the call that hands it on.
var (
	extensionCachePath = regexp.MustCompile(`const cachePath = path\.join\(os\.homedir\(\), '([^']+)', '([^']+)'\);`)
	extensionRunTests  = regexp.MustCompile(`runTests\(\{\s*cachePath,`)
)

// TestCIWorkflow_CachesWhereTheExtensionTestsDownload holds the VS Code
// integration job's cache to the directory runTest.mjs downloads VS Code to:
// a cache anywhere else restores nothing the harness reads.
func TestCIWorkflow_CachesWhereTheExtensionTestsDownload(t *testing.T) {
	t.Parallel()
	var wf workflow
	decodeYAML(t, fromRoot(".github/workflows/ci.yaml"), &wf)
	var cached []string
	for _, j := range wf.Jobs {
		for _, s := range j.Steps {
			if strings.HasPrefix(s.Uses, "actions/cache@") && strings.Contains(fmt.Sprint(s.With["key"]), "vscode-test") {
				cached = append(cached, fmt.Sprint(s.With["path"]))
			}
		}
	}
	if len(cached) != 1 {
		t.Fatalf("ci.yaml caches the VS Code test install in %d steps, want 1: %q", len(cached), cached)
	}

	script, err := os.ReadFile(fromRoot("lsp/editors/vscode/tests/integration/runTest.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	m := extensionCachePath.FindSubmatch(script)
	if m == nil {
		t.Fatal("runTest.mjs builds no cachePath under the home directory as this check reads it")
	}
	if !extensionRunTests.Match(script) {
		t.Error("runTest.mjs does not pass cachePath to runTests")
	}
	if want := "~/" + path.Join(string(m[1]), string(m[2])); cached[0] != want {
		t.Errorf("ci.yaml caches %q, runTest.mjs downloads to %q", cached[0], want)
	}
}
