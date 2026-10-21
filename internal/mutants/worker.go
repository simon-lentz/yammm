package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// header names the columns of results.tsv.
func header() []string {
	return []string{"id", "spelling", "verdict", "seconds", "worker", "failing tests", "packages", "configuration"}
}

// A worker judges its mutants one at a time in a tree of its own.
type worker struct {
	number    int
	tree      *tree
	mutants   []mutant
	listings  map[string][]listed // a configuration and package set to its listing of the tree as copied
	baselines map[string]bool     // a configuration, package set and spelling whose packages passed
	out       io.Writer           // work/worker.N.out
	rows      *csv.Writer         // work/results.wN.tsv
}

// judge copies the checkout for each worker, runs the workers and writes
// results.tsv. It returns the run's exit status.
func (r *run) judge(ctx context.Context, stdout, stderr io.Writer) int {
	began := time.Now()
	count := min(r.workers, len(r.mutants))
	fmt.Fprintf(stdout, "mutants: %d, workers: %d\n", len(r.mutants), count)
	workers := make([]*worker, count)
	for i := range workers {
		w := &worker{number: i + 1, listings: map[string][]listed{}, baselines: map[string]bool{}}
		for j := i; j < len(r.mutants); j += count {
			w.mutants = append(w.mutants, r.mutants[j])
		}
		var err error
		if w.tree, err = newTree(ctx, r, filepath.Join(r.work, fmt.Sprintf("w%d", w.number))); err != nil {
			fmt.Fprintf(stderr, "mutants: %v\n", err)
			return 1
		}
		workers[i] = w
	}
	failed := make([]error, count)
	var wg sync.WaitGroup
	for i, w := range workers {
		wg.Go(func() { failed[i] = w.work(ctx) })
	}
	wg.Wait()
	if err := errors.Join(failed...); err != nil {
		fmt.Fprintf(stderr, "mutants: %v\n", err)
		return 1
	}
	if ctx.Err() != nil {
		fmt.Fprintf(stderr, "mutants: stopped by a signal; each worker's rows are in %s\n", r.work)
		return 1
	}
	rows, err := r.merge(count)
	if err != nil {
		fmt.Fprintf(stderr, "mutants: %v\n", err)
		return 1
	}
	verdicts, seconds := map[string]int{}, 0
	for _, row := range rows {
		verdicts[row[2]]++
		n, err := strconv.Atoi(row[3])
		if err != nil {
			fmt.Fprintf(stderr, "mutants: results.tsv: %v\n", err)
			return 1
		}
		seconds += n
	}
	fmt.Fprintf(stdout, "wall clock: %ds\n", int(time.Since(began).Seconds()))
	for _, verdict := range slices.Sorted(maps.Keys(verdicts)) {
		fmt.Fprintf(stdout, "  %s: %d\n", verdict, verdicts[verdict])
	}
	fmt.Fprintf(stdout, "  run seconds, summed: %d\n", seconds)
	if n := verdicts[verdictError]; n > 0 {
		fmt.Fprintf(stderr, "mutants: %d row(s) are ERROR: the run could not judge them, and each log says why\n", n)
		return 1
	}
	return 0
}

// merge writes results.tsv from every worker's rows, sorted by id and spelling.
func (r *run) merge(workers int) ([][]string, error) {
	var rows [][]string
	for n := 1; n <= workers; n++ {
		content, err := os.ReadFile(filepath.Join(r.work, fmt.Sprintf("results.w%d.tsv", n)))
		if err != nil {
			return nil, fmt.Errorf("the workers' rows: %w", err)
		}
		reader := csv.NewReader(bytes.NewReader(content))
		reader.Comma = '\t'
		part, err := reader.ReadAll()
		if err != nil {
			return nil, fmt.Errorf("the workers' rows: %w", err)
		}
		rows = append(rows, part...)
	}
	slices.SortFunc(rows, slices.Compare)
	var b bytes.Buffer
	writer := csv.NewWriter(&b)
	writer.Comma = '\t'
	if err := writer.WriteAll(append([][]string{header()}, rows...)); err != nil {
		return nil, fmt.Errorf("results.tsv: %w", err)
	}
	if err := os.WriteFile(filepath.Join(r.out, "results.tsv"), b.Bytes(), 0o600); err != nil {
		return nil, fmt.Errorf("results.tsv: %w", err)
	}
	return rows, nil
}

// work judges every mutant of the worker under each of its spellings. It
// returns an error only when the worker's own files cannot be written.
func (w *worker) work(ctx context.Context) error {
	work := w.tree.run.work
	out, err := os.Create(filepath.Join(work, fmt.Sprintf("worker.%d.out", w.number)))
	if err != nil {
		return fmt.Errorf("worker %d: %w", w.number, err)
	}
	defer out.Close()
	rows, err := os.Create(filepath.Join(work, fmt.Sprintf("results.w%d.tsv", w.number)))
	if err != nil {
		return fmt.Errorf("worker %d: %w", w.number, err)
	}
	defer rows.Close()
	w.out, w.rows = out, csv.NewWriter(rows)
	w.rows.Comma = '\t'
	for _, m := range w.mutants {
		for _, spell := range m.spells {
			if ctx.Err() != nil {
				return nil
			}
			began := time.Now()
			j := w.judge(ctx, m, spell)
			if ctx.Err() != nil {
				return nil
			}
			seconds := strconv.Itoa(int(time.Since(began).Seconds()))
			if j.verdict != verdictNoFold {
				head := fmt.Sprintf("id=%s spell=%s worker=%d configuration=%s TMPDIR=%s verdict=%s secs=%s\npkgs=%s\n",
					m.id, spell, w.number, j.config, w.tmp(spell), j.verdict, seconds, strings.Join(m.packages, " "))
				log := filepath.Join(w.tree.run.out, "logs", m.id+"."+spell+".log")
				if err := os.WriteFile(log, []byte(head+j.log.String()), 0o600); err != nil {
					return fmt.Errorf("worker %d: %w", w.number, err)
				}
			}
			row := []string{m.id, spell, j.verdict, seconds, fmt.Sprintf("w%d", w.number), strings.Join(j.tests, " "), strings.Join(m.packages, " "), j.config}
			if err := w.rows.Write(row); err != nil {
				return fmt.Errorf("worker %d: %w", w.number, err)
			}
			w.rows.Flush()
			if err := w.rows.Error(); err != nil {
				return fmt.Errorf("worker %d: %w", w.number, err)
			}
			fmt.Fprintf(w.out, "%s %s %s %ss\n", m.id, spell, j.verdict, seconds)
		}
	}
	return nil
}

// tmp returns the TMPDIR of a spelling.
func (w *worker) tmp(spell string) string {
	if spell == "folded" {
		return w.tree.run.folded
	}
	return w.tree.run.asis
}

// A judgement is one mutant's row and log.
type judgement struct {
	verdict string
	config  string
	tests   []string
	log     strings.Builder
}

// judge runs one mutant under one spelling. Whatever it finds is a verdict:
// an error of the worker's own reads ERROR, with the error in the log.
func (w *worker) judge(ctx context.Context, m mutant, spell string) *judgement {
	j := &judgement{}
	if spell == "folded" && w.tree.run.folded == "" {
		j.verdict = verdictNoFold
		return j
	}
	if err := w.judgeInto(ctx, j, m, w.tmp(spell)); err != nil {
		j.verdict = verdictError
		fmt.Fprintf(&j.log, "mutants: %v\n", err)
	}
	return j
}

func (w *worker) judgeInto(ctx context.Context, j *judgement, m mutant, tmp string) error {
	if err := w.restore(ctx, j, "before the mutant"); err != nil {
		return err
	}
	undo, matches, err := w.tree.mutate(m)
	if errors.Is(err, errNoMatch) {
		j.verdict = verdictNoMatch
		fmt.Fprintf(&j.log, "the search matched %d time(s) in %s, and the file is unchanged\n", matches, m.file)
		return nil
	}
	if err != nil {
		return err
	}
	if err := undo(); err != nil {
		return err
	}
	config, ok, err := w.configure(ctx, j, m)
	if err != nil || !ok {
		return err
	}
	j.config = config.name
	for _, bench := range runs(config) {
		// The benchmarks can only move a verdict the tests left open, so their
		// run before the change is made only then.
		if bench && j.verdict != verdictSurvived && j.verdict != verdictNoTest {
			break
		}
		if key := config.name + "\x00" + tmp + "\x00" + strings.Join(m.packages, " ") + runName(bench); !w.baselines[key] {
			base, err := w.test(ctx, config, tmp, m.packages, bench)
			if err != nil {
				return err
			}
			if base.fault != "" {
				base.report(&j.log)
				return errors.New("the run before the change cannot be read")
			}
			if !base.green() {
				j.verdict = verdictRed
				j.tests = base.failing()
				fmt.Fprintf(&j.log, "the packages do not pass before the change, so no verdict is possible%s\n", runName(bench))
				base.report(&j.log)
				return w.restore(ctx, j, "after the run before the change")
			}
			if err := w.restore(ctx, j, "after the run before the change"); err != nil {
				return err
			}
			w.baselines[key] = true
		}
		undo, matches, err := w.tree.mutate(m)
		if err != nil {
			return err
		}
		if !bench {
			fmt.Fprintf(&j.log, "applied to %s (%d match(es))\n", m.file, matches)
		}
		err = w.judgeChanged(ctx, j, m, config, tmp, bench)
		if undoErr := undo(); undoErr != nil {
			return errors.Join(err, undoErr)
		}
		if err != nil {
			return err
		}
		if err := w.restore(ctx, j, "after the mutant"+runName(bench)); err != nil {
			return err
		}
	}
	return nil
}

// runs returns the test runs a configuration's gate makes: its tests, then,
// where scripts/test.sh runs them, its benchmarks.
func runs(config configuration) []bool {
	if config.bench {
		return []bool{false, true}
	}
	return []bool{false}
}

// runName names the benchmark run in a log line; the test run needs no name.
func runName(bench bool) string {
	if bench {
		return " (the benchmark run)"
	}
	return ""
}

// judgeChanged builds and tests the tree that holds the mutant. The benchmark
// run comes after a test run that left the verdict SURVIVED or NOTEST, and its
// verdict replaces that one unless it is NOTEST, or SURVIVED where the tests
// left SURVIVED.
func (w *worker) judgeChanged(ctx context.Context, j *judgement, m mutant, config configuration, tmp string, bench bool) error {
	if !bench {
		var built bytes.Buffer
		env := setEnv(config.environment(w.tree.run.env), "TMPDIR", tmp)
		code, err := goCommand(ctx, w.tree.dir, env, &built, &built, slices.Concat([]string{"build"}, config.flags, []string{"./..."})...)
		if err != nil {
			return err
		}
		if code != 0 {
			j.verdict = verdictNoBuild
			fmt.Fprintf(&j.log, "the changed tree does not build, so no test judges it\n%s", built.String())
			return nil
		}
	}
	changed, err := w.test(ctx, config, tmp, m.packages, bench)
	if err != nil {
		return err
	}
	verdict := changed.verdict()
	if bench {
		fmt.Fprintf(&j.log, "each benchmark, once, as scripts/test.sh runs them:\n")
		switch verdict {
		case verdictSurvived, verdictNoTest:
			fmt.Fprintf(&j.log, "every benchmark passed\n")
			// A benchmark that ran judged the change, where no test did.
			if verdict == verdictSurvived {
				j.verdict = verdictSurvived
			}
			changed.report(&j.log)
			return nil
		case verdictTimeout:
			// scripts/test.sh fails a benchmark binary stopped at its limit.
			verdict = verdictKilled
			j.tests = changed.unfinished(packageTimedOut)
			fmt.Fprintf(&j.log, "a benchmark binary ran to the gate's limit of %v\n", w.tree.run.bench)
		default:
			j.tests = changed.failing()
		}
		j.verdict = verdict
		changed.report(&j.log)
		return nil
	}
	j.verdict = verdict
	switch j.verdict {
	case verdictTimeout:
		j.tests = changed.unfinished(packageTimedOut)
	case verdictSurvived:
		fmt.Fprintf(&j.log, "every named package passed, and nothing in them asserts what the change changed\n")
	case verdictNoTest:
		fmt.Fprintf(&j.log, "every named package passed and none ran a test, so nothing judged the change\n")
	default:
		j.tests = changed.failing()
	}
	changed.report(&j.log)
	return nil
}

// configure chooses the configuration m is judged under: the first in which a
// test binary of its packages compiles its file, in the tree as copied or,
// failing that, in the tree that holds the mutant. A file go list does not
// name is judged under the plain build when it is not Go or [goIgnores] holds
// it to no package. ok is false when err is not nil, or when the file reads
// NOTCOMPILED, which j then holds.
func (w *worker) configure(ctx context.Context, j *judgement, m mutant) (config configuration, ok bool, err error) {
	known := filepath.Ext(m.file) == ".go" && !goIgnores(m.file)
	for _, changed := range []bool{false, true} {
		config, found, named, err := w.compiledUnder(ctx, m, changed)
		if err != nil || found {
			return config, found, err
		}
		if known = known || named; !known {
			return configurations()[0], true, nil
		}
	}
	j.verdict = verdictNotCompiled
	fmt.Fprintf(&j.log, "no test binary of the named packages compiles %s on this host, before or after the change, under any configuration\n", m.file)
	return config, false, nil
}

// goIgnores reports whether file or a directory on its path is named testdata
// or begins with "." or "_", which `go help packages` says the go command
// ignores. It reads no build, so a Go file outside every package for another
// reason, as in a nested module, still reads NOTCOMPILED.
func goIgnores(file string) bool {
	for part := range strings.SplitSeq(filepath.ToSlash(file), "/") {
		if part == "testdata" || strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_") {
			return true
		}
	}
	return false
}

// compiledUnder returns the first configuration under which a test binary of
// m's packages compiles m's file, in the tree as copied or with the mutant
// applied, and whether go list names the file under any.
func (w *worker) compiledUnder(ctx context.Context, m mutant, changed bool) (config configuration, found, known bool, err error) {
	if changed {
		undo, _, mutateErr := w.tree.mutate(m)
		if mutateErr != nil {
			return config, false, false, mutateErr
		}
		defer func() { err = errors.Join(err, undo()) }()
	}
	for _, c := range configurations() {
		entries, err := w.listing(ctx, c, m.packages, changed)
		if err != nil {
			return config, false, false, err
		}
		compiled, named := w.compiled(entries, m.file)
		if compiled {
			return c, true, true, nil
		}
		known = known || named
	}
	return config, false, known, nil
}

// restore holds the tree to what was copied and notes in the log what differed.
func (w *worker) restore(ctx context.Context, j *judgement, when string) error {
	differed, err := w.tree.restore(ctx)
	if err != nil {
		return err
	}
	if len(differed) > 0 {
		fmt.Fprintf(&j.log, "the tree differed from the checkout %s and was restored: %s\n", when, strings.Join(differed, " "))
		fmt.Fprintf(w.out, "w%d: the tree differed %s and was restored: %s\n", w.number, when, strings.Join(differed, " "))
	}
	return nil
}
