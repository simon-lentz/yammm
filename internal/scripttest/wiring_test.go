package scripttest

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
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
	Name            string            `yaml:"name"`
	Uses            string            `yaml:"uses"`
	Run             string            `yaml:"run"`
	Env             map[string]string `yaml:"env"`
	If              string            `yaml:"if"`
	ContinueOnError any               `yaml:"continue-on-error"`
	TimeoutMinutes  int               `yaml:"timeout-minutes"`
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
		hosts[entry["os"]] = strings.TrimSpace(vet.Run)
	}
	want := map[string]string{
		"ubuntu-latest":  "scripts/vet.sh --host",
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

// vetCommands returns every scripts/vet.sh command j runs, one per matrix
// entry where the command names a matrix value.
func vetCommands(j job) []string {
	var cmds []string
	for _, st := range j.Steps {
		if !strings.HasPrefix(st.Run, "scripts/vet.sh") {
			continue
		}
		if len(j.Strategy.Matrix.Include) == 0 {
			cmds = append(cmds, strings.TrimSpace(st.Run))
			continue
		}
		for _, entry := range j.Strategy.Matrix.Include {
			run := st.Run
			for key, value := range entry {
				run = strings.ReplaceAll(run, "${{ matrix."+key+" }}", value)
			}
			cmds = append(cmds, strings.TrimSpace(run))
		}
	}
	return cmds
}

// Every target the module's build constraints select is vetted, in a job of
// its own. On a Linux host scripts/vet.sh type-checks eleven targets where
// --host checks two, which took the Vet step of a host job 255 to 382 s
// against the other hosts' under 30 s and made that job the longest of the
// workflow. One job runs it, because a second would pay the cost twice, and
// the release calls this workflow whole, so a tag still waits for it.
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
// stack before the job is killed. Measured over 25 CI runs, the test jobs'
// Test step ended at most 688 s into the job and the integration job's test
// step started at most 23 s in; each margin adds headroom to its measurement.
var jobMargins = map[string]time.Duration{
	"test":        15 * time.Minute,
	"integration": 5 * time.Minute,
}

// testlessJobs names, per job of the test workflow that runs no go test, what
// it runs instead. A job is declared here or carries a margin, never both and
// never neither: the check cannot tell a job that runs no test from one whose
// test command it failed to read, so the distinction is stated rather than
// inferred.
var testlessJobs = map[string]string{
	"cross-vet": "it vets every target the module's build constraints select",
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
			d, f := goTestTimeouts("jobs."+name, st.Run)
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
// .github/workflows. The pathspecs take glob magic because a plain * also
// matches a slash, and GitHub runs no file in a subdirectory.
func trackedWorkflows(t *testing.T, root string) map[string]workflow {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "ls-files", "-z", "--",
		":(glob).github/workflows/*.yml", ":(glob).github/workflows/*.yaml")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	workflows := map[string]workflow{}
	for rel := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if rel == "" {
			continue
		}
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
	f := &fixture{t: t, dir: t.TempDir()}
	jobNamed := func(name string) string {
		return "jobs:\n  " + name + ":\n    runs-on: ubuntu-latest\n    steps:\n      - run: make\n"
	}
	f.write(".github/workflows/a.yml", jobNamed("top"))
	f.write(".github/workflows/b.yaml", jobNamed("b"))
	f.write(".github/workflows/sub/a.yml", jobNamed("nested"))
	f.write(".github/workflows/sub/c.yaml", jobNamed("nested"))
	f.write(".github/workflows/gone.yml", jobNamed("gone"))
	f.write(".github/workflows/notes.txt", "not a workflow\n")
	f.index()
	if err := os.Remove(filepath.Join(f.dir, ".github", "workflows", "gone.yml")); err != nil {
		t.Fatal(err)
	}

	got := trackedWorkflows(t, f.dir)
	if names := slices.Sorted(maps.Keys(got)); !slices.Equal(names, []string{"a.yml", "b.yaml"}) {
		t.Errorf("read %q, want [a.yml b.yaml]: a subdirectory, a deleted file and a non-workflow are not run", names)
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

// shellcheck reads every file under scripts/ and every shell script of the
// Claude plugin's hooks, at commit and in CI's pre-commit job, which skips only
// the hooks the host jobs run.
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
		if slices.Contains(strings.Split(s.Env["SKIP"], ","), "shellcheck") {
			t.Errorf("CI's pre-commit job skips shellcheck (SKIP=%s)", s.Env["SKIP"])
		}
	}
	if !ran {
		t.Error("CI's pre-commit job runs no pre-commit")
	}
}
