package scripttest

import (
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

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
	TimeoutMinutes  int        `yaml:"timeout-minutes"`
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
	TimeoutMinutes  int    `yaml:"timeout-minutes"`
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

// jobMargins holds, per job of the test workflow, how far every go test
// timeout the job runs must stay below the job's own timeout: at least as long
// as the job takes to start its last test binary, so a hung binary prints its
// stack before the job is killed. Measured over 25 CI runs, the test jobs'
// Test step ended at most 688 s into the job and the integration job's test
// step started at most 23 s in; each margin adds headroom to its measurement.
var jobMargins = map[string]time.Duration{
	"test":        15 * time.Minute,
	"integration": 5 * time.Minute,
}

var (
	goTestCall        = regexp.MustCompile(`\bgo\s+test\b`)
	goTestTimeoutFlag = regexp.MustCompile(`--?timeout[= ](\S+)`)
	// A shell comment starts at a word: a # at the line's start or after white
	// space. `$#` is not one.
	shellComment = regexp.MustCompile(`(^|\s)#.*$`)
)

// shellCommands returns text's command lines with backslash continuations
// joined and comments removed.
func shellCommands(text string) []string {
	var out []string
	var cur strings.Builder
	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, "\r\n")
		if head, ok := strings.CutSuffix(line, "\\"); ok {
			cur.WriteString(head)
			cur.WriteByte(' ')
			continue
		}
		cur.WriteString(line)
		out = append(out, shellComment.ReplaceAllString(cur.String(), ""))
		cur.Reset()
	}
	if cur.Len() > 0 {
		out = append(out, shellComment.ReplaceAllString(cur.String(), ""))
	}
	return out
}

// goTestTimeouts returns the timeout every go test command in text runs under.
// The flag package keeps a flag's LAST value, so the last -timeout is the one
// that applies. A command with none, or with one that is not positive, which
// switches Go's timeout off, is a finding.
func goTestTimeouts(where, text string) (timeouts []time.Duration, findings []string) {
	for _, cmd := range shellCommands(text) {
		if !goTestCall.MatchString(cmd) {
			continue
		}
		flags := goTestTimeoutFlag.FindAllStringSubmatch(cmd, -1)
		if flags == nil {
			findings = append(findings, fmt.Sprintf("%s runs go test with no -timeout: %s", where, strings.TrimSpace(cmd)))
			continue
		}
		last := flags[len(flags)-1][1]
		d, err := time.ParseDuration(last)
		switch {
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
// timeout bounds its go test runs too, and is held to the same margin.
func timeoutFindings(wf workflow, script string, margins map[string]time.Duration) []string {
	if len(wf.Jobs) == 0 {
		return []string{"the workflow holds no job"}
	}
	var findings []string
	for _, name := range slices.Sorted(maps.Keys(wf.Jobs)) {
		j := wf.Jobs[name]
		margin, ok := margins[name]
		if !ok {
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
			d, f := goTestTimeouts("jobs."+name, st.Run)
			timeouts, found = append(timeouts, d...), append(found, f...)
			if len(timeouts)+len(found) > 0 {
				ran = true
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
		if !ran {
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
	for _, f := range timeoutFindings(wf, string(script), jobMargins) {
		t.Error(f)
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
	for _, c := range []struct {
		name    string
		wf      workflow
		script  string
		margins map[string]time.Duration
		want    string // a substring of the one finding expected; "" expects none
	}{
		{"a job within its margin", wfWith(30, 0, "scripts/test.sh"), okScript, margins, ""},
		{"a job exactly at its margin", wfWith(25, 0, "scripts/test.sh"), okScript, margins, ""},
		{"a job one minute inside its margin", wfWith(24, 0, "scripts/test.sh"), okScript, margins, "less than 15m0s past"},
		{"a job with no timeout", wfWith(0, 0, "scripts/test.sh"), okScript, margins, "sets no timeout-minutes"},
		{"a job with no measured margin", wfWith(30, 0, "scripts/test.sh"), okScript, map[string]time.Duration{}, "has no measured margin"},
		{"a job that runs no go test", wfWith(30, 0, "echo hello"), okScript, margins, "runs no go test"},
		{"a go test with no -timeout", wfWith(30, 0, "scripts/test.sh"), "go test ./...\n", margins, "no -timeout"},
		{"a go test spelled with two spaces", wfWith(30, 0, "scripts/test.sh"), okScript + "go  test -run X ./...\n", margins, "no -timeout"},
		{"a -timeout only in a trailing comment", wfWith(30, 0, "scripts/test.sh"), "go test ./... # -timeout=10m\n", margins, "no -timeout"},
		{"a -timeout on a continuation line", wfWith(30, 0, "scripts/test.sh"), "go test -json \\\n\t-timeout=10m ./...\n", margins, ""},
		{"a zero -timeout", wfWith(30, 0, "scripts/test.sh"), "go test -timeout=0 ./...\n", margins, "switches the timeout off"},
		{"a later -timeout overriding an earlier one", wfWith(30, 0, "scripts/test.sh"), "go test -timeout=10m -timeout=45m ./...\n", margins, "past a go test timeout of 45m0s"},
		{"a go test inline in the step", wfWith(30, 0, "go test -timeout 20m ./x/"), okScript, margins, "past a go test timeout of 20m0s"},
		{"a step timeout inside the margin", wfWith(30, 20, "scripts/test.sh"), okScript, margins, `step "Test" times out at 20m0s`},
		{"no job at all", workflow{}, okScript, margins, "holds no job"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := timeoutFindings(c.wf, c.script, c.margins)
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
