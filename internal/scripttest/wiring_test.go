package scripttest

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const testWorkflow = "./.github/workflows/yammm_test.yml"

type workflow struct {
	On   map[string]*trigger `yaml:"on"`
	Jobs map[string]job      `yaml:"jobs"`
}

type trigger struct {
	Branches       []string `yaml:"branches"`
	BranchesIgnore []string `yaml:"branches-ignore"`
	Paths          []string `yaml:"paths"`
	PathsIgnore    []string `yaml:"paths-ignore"`
}

type job struct {
	Uses            string     `yaml:"uses"`
	Needs           stringList `yaml:"needs"`
	If              string     `yaml:"if"`
	ContinueOnError any        `yaml:"continue-on-error"`
	Strategy        struct {
		FailFast *bool `yaml:"fail-fast"`
		Matrix   struct {
			Include []map[string]string `yaml:"include"`
		} `yaml:"matrix"`
	} `yaml:"strategy"`
	Steps []step `yaml:"steps"`
}

type step struct {
	Name            string `yaml:"name"`
	Uses            string `yaml:"uses"`
	Run             string `yaml:"run"`
	If              string `yaml:"if"`
	ContinueOnError any    `yaml:"continue-on-error"`
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

func TestTestWorkflow_EveryHostRunsEveryCheck(t *testing.T) {
	t.Parallel()
	var wf workflow
	decodeYAML(t, fromRoot(".github/workflows/yammm_test.yml"), &wf)
	test, ok := wf.Jobs["test"]
	if !ok {
		t.Fatal("jobs.test is not set")
	}
	if test.If != "" {
		t.Errorf("jobs.test runs only if %q", test.If)
	}
	if test.ContinueOnError != nil {
		t.Errorf("jobs.test sets continue-on-error %v, so a failure passes the job", test.ContinueOnError)
	}
	if ff := test.Strategy.FailFast; ff == nil || *ff {
		t.Error("jobs.test does not set fail-fast: false, so one host's failure cancels the other hosts' reports")
	}

	find := func(match func(step) bool) (step, bool) {
		i := slices.IndexFunc(test.Steps, match)
		if i < 0 {
			return step{}, false
		}
		return test.Steps[i], true
	}
	if verify, ok := find(func(s step) bool { return s.Run == "scripts/lintconfig.sh" }); !ok {
		t.Error("no step runs scripts/lintconfig.sh")
	} else if verify.If != "" || verify.ContinueOnError != nil {
		t.Errorf("the verify step runs if %q with continue-on-error %v; it must run on every event and fail the job", verify.If, verify.ContinueOnError)
	}
	checks := map[string]func(step) bool{
		"lint": func(s step) bool { return strings.HasPrefix(s.Uses, "golangci/golangci-lint-action@") },
		"vet":  func(s step) bool { return strings.HasPrefix(s.Run, "scripts/vet.sh") },
		"test": func(s step) bool { return s.Run == "scripts/test.sh" },
	}
	for name, match := range checks {
		s, ok := find(match)
		if !ok {
			t.Errorf("no %s step", name)
			continue
		}
		if s.If != "${{ !cancelled() }}" {
			t.Errorf("the %s step runs if %q; it must run after an earlier step fails", name, s.If)
		}
		if s.ContinueOnError != nil {
			t.Errorf("the %s step sets continue-on-error %v, so its failure passes the job", name, s.ContinueOnError)
		}
	}

	vet, ok := find(checks["vet"])
	if !ok {
		return
	}
	hosts := map[string]string{}
	for _, entry := range test.Strategy.Matrix.Include {
		hosts[entry["os"]] = strings.TrimSpace(strings.ReplaceAll(vet.Run, "${{ matrix.vet_args }}", entry["vet_args"]))
	}
	want := map[string]string{
		"ubuntu-latest":  "scripts/vet.sh",
		"windows-latest": "scripts/vet.sh --host",
		"macos-latest":   "scripts/vet.sh --host",
	}
	for host, cmd := range want {
		if got, ok := hosts[host]; !ok {
			t.Errorf("the matrix holds no %s host", host)
		} else if got != cmd {
			t.Errorf("the %s host vets with %q, want %q", host, got, cmd)
		}
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

func TestPreCommitHooks_RunTheGateScripts(t *testing.T) {
	t.Parallel()
	var config struct {
		DefaultStages []string `yaml:"default_stages"`
		Repos         []struct {
			Hooks []struct {
				ID        string   `yaml:"id"`
				Entry     string   `yaml:"entry"`
				Files     string   `yaml:"files"`
				Exclude   string   `yaml:"exclude"`
				Stages    []string `yaml:"stages"`
				AlwaysRun bool     `yaml:"always_run"`
			} `yaml:"hooks"`
		} `yaml:"repos"`
	}
	decodeYAML(t, fromRoot(".pre-commit-config.yaml"), &config)
	atCommit := func(stages []string) bool {
		return len(stages) == 0 || slices.Contains(stages, "pre-commit") || slices.Contains(stages, "commit")
	}
	if !atCommit(config.DefaultStages) {
		t.Errorf("default_stages %q leaves out the commit stage", config.DefaultStages)
	}
	type hook = struct {
		entry     string
		files     string
		exclude   string
		stages    []string
		alwaysRun bool
	}
	hooks := map[string]hook{}
	for _, repo := range config.Repos {
		for _, h := range repo.Hooks {
			hooks[h.ID] = hook{h.Entry, h.Files, h.Exclude, h.Stages, h.AlwaysRun}
		}
	}

	rows := []struct {
		id        string
		entry     string
		fires     []string
		alwaysRun bool
	}{
		{id: "golangci-lint-config", entry: "scripts/lintconfig.sh", fires: []string{".golangci.yml", "go.mod", "go.sum"}},
		{id: "golangci-lint", entry: "scripts/lint.sh", fires: []string{"schema/load.go", "location/host_path_windows.go"}},
		{id: "go-vet", entry: "scripts/vet.sh", fires: []string{"schema/load.go", "go.mod", "go.sum"}},
		{id: "go-test", entry: "scripts/test.sh", alwaysRun: true},
	}
	for _, row := range rows {
		h, ok := hooks[row.id]
		if !ok {
			t.Errorf("no hook %s", row.id)
			continue
		}
		if h.entry != row.entry {
			t.Errorf("hook %s runs %q, want %q", row.id, h.entry, row.entry)
		}
		if row.alwaysRun && !h.alwaysRun {
			t.Errorf("hook %s does not always run", row.id)
		}
		if !atCommit(h.stages) {
			t.Errorf("hook %s runs only at stages %q, not at commit", row.id, h.stages)
		}
		if h.exclude != "" {
			t.Errorf("hook %s excludes %q, so a change it matches does not run it", row.id, h.exclude)
		}
		// pre-commit searches each staged path with the files pattern.
		files, err := regexp.Compile(h.files)
		if err != nil {
			t.Errorf("hook %s files pattern %q: %v", row.id, h.files, err)
			continue
		}
		for _, path := range row.fires {
			if !files.MatchString(path) {
				t.Errorf("hook %s does not fire when %s changes (files %q)", row.id, path, h.files)
			}
		}
	}
}
