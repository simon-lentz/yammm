package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// stopGrace is how long a process sent a stop signal has before it is killed.
const stopGrace = 5 * time.Second

// envValue returns name's value in environ, the last one when it is set twice,
// as a process started with environ reads it.
func envValue(environ []string, name string) string {
	for _, kv := range slices.Backward(environ) {
		if key, value, ok := strings.Cut(kv, "="); ok && key == name {
			return value
		}
	}
	return ""
}

// setEnv returns environ with name set to value and nowhere else.
func setEnv(environ []string, name, value string) []string {
	kept := slices.DeleteFunc(slices.Clone(environ), func(kv string) bool {
		key, _, _ := strings.Cut(kv, "=")
		return key == name
	})
	return append(kept, name+"="+value)
}

// cleanEnv returns the environment every later command of the run starts
// from: the caller's, without the variables that bind git to one repository,
// without GOEXPERIMENT, which the jsonv2 configuration alone sets, without this
// program's own, and with GODEBUG set so the go command names a failed build in
// its events.
func cleanEnv(ctx context.Context, environ []string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--local-env-vars").Output()
	if err != nil {
		return nil, refusef("git rev-parse --local-env-vars: %v", err)
	}
	dropped := append(strings.Fields(string(out)), "GOEXPERIMENT", "YAMMM_PACKAGES_TRACKED", limitVar, benchLimitVar, recordVar, superviseLimitVar)
	env := slices.DeleteFunc(slices.Clone(environ), func(kv string) bool {
		key, _, _ := strings.Cut(kv, "=")
		return slices.Contains(dropped, key)
	})
	settings := slices.DeleteFunc(strings.Split(envValue(env, "GODEBUG"), ","), func(setting string) bool {
		key, _, _ := strings.Cut(setting, "=")
		return setting == "" || key == "gotestjsonbuildtext"
	})
	return setEnv(env, "GODEBUG", strings.Join(append(settings, "gotestjsonbuildtext=0"), ",")), nil
}

// gitOutput runs git in dir and returns what it printed.
func gitOutput(ctx context.Context, dir string, env []string, args ...string) ([]byte, error) {
	return gitInput(ctx, dir, env, nil, args...)
}

// gitInput runs git in dir with input on its standard input. Each pathspec is
// read as a path, never as a pattern.
func gitInput(ctx context.Context, dir string, env []string, input []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--literal-pathspecs"}, args...)...) //nolint:gosec // git, with this program's own arguments
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s in %s: %w\n%s", strings.Join(args, " "), dir, err, bytes.TrimSpace(stderr.Bytes()))
	}
	return out, nil
}

// pinToolchain returns env holding the toolchain the checkout's go.mod names,
// when the caller chose none, and refuses a run that another version would
// execute: a verdict is a statement about the program the gate judges.
func pinToolchain(ctx context.Context, checkout string, env []string) ([]string, error) {
	modfile := filepath.Join(checkout, "go.mod")
	content, err := os.ReadFile(modfile)
	if err != nil {
		return nil, refusef("toolchain: no go.mod in %s", checkout)
	}
	want := ""
	for line := range strings.Lines(string(content)) {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == "go" {
			if len(fields) >= 2 {
				want = fields[1]
			}
			break
		}
	}
	if want == "" {
		return nil, refusef("toolchain: %s names no go directive", modfile)
	}
	chosen := envValue(env, "GOTOOLCHAIN")
	if chosen == "" {
		chosen = "go" + want
		env = setEnv(env, "GOTOOLCHAIN", chosen)
	}
	var stdout, stderr bytes.Buffer
	code, err := goCommand(ctx, checkout, env, &stdout, &stderr, "env", "GOVERSION")
	if err != nil || code != 0 {
		return nil, refusef("toolchain: go env GOVERSION: exit %d %v\n%s", code, err, bytes.TrimSpace(stderr.Bytes()))
	}
	if have := strings.TrimSpace(stdout.String()); have != "go"+want {
		return nil, refusef("toolchain: %s pins go%s and this run is %s\ntoolchain: GOTOOLCHAIN=%s; unset it to take the pinned toolchain", modfile, want, have, chosen)
	}
	return env, nil
}

// checkExperiments refuses a run whose go env file sets GOEXPERIMENT. An
// empty variable does not override the file, so the setting would reach every
// configuration but jsonv2, which sets the variable.
func checkExperiments(ctx context.Context, checkout string, env []string) error {
	var stdout, stderr bytes.Buffer
	code, err := goCommand(ctx, checkout, env, &stdout, &stderr, "env", "GOEXPERIMENT")
	if err != nil || code != 0 {
		return refusef("go env GOEXPERIMENT: exit %d %v\n%s", code, err, bytes.TrimSpace(stderr.Bytes()))
	}
	if set := strings.TrimSpace(stdout.String()); set != "" {
		return refusef("the go env file sets GOEXPERIMENT=%s, which would reach every configuration but jsonv2; unset it there", set)
	}
	return nil
}

// goCommand runs one go command in dir and returns its exit status. On unix
// the command leads a process group: when ctx ends the group is sent a stop
// signal, what is left of it stopGrace later is killed, and every member still
// in the group is killed when the command returns. Elsewhere a stop kills the
// go command alone.
func goCommand(ctx context.Context, dir string, env []string, stdout, stderr io.Writer, args ...string) (int, error) {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	// The go command spells the directories it lists from PWD, and os/exec sets
	// it to Dir only for a command that inherits its environment.
	cmd.Env = setEnv(env, "PWD", dir)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	ownGroup(cmd)
	var stopped time.Time
	cmd.Cancel = func() error {
		stopped = time.Now()
		return stopGroup(cmd)
	}
	cmd.WaitDelay = stopGrace
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("go %s: %w", args[0], err)
	}
	err := cmd.Wait()
	if ctx.Err() != nil {
		// The go command ends at the signal. What it started gets the rest of
		// the grace to end by itself, a bounded wait: a member that has exited
		// and that nothing reaps can still count as one.
		for end := stopped.Add(stopGrace); groupAlive(cmd) && time.Now().Before(end); {
			time.Sleep(50 * time.Millisecond)
		}
	}
	killGroup(cmd)
	if ctx.Err() != nil {
		return 0, fmt.Errorf("go %s: %w", args[0], context.Cause(ctx))
	}
	// A process the command left holding its output kept Wait for stopGrace;
	// the command's own status still stands.
	if exit := (*exec.ExitError)(nil); errors.As(err, &exit) || errors.Is(err, exec.ErrWaitDelay) {
		return cmd.ProcessState.ExitCode(), nil
	}
	if err != nil {
		return 0, fmt.Errorf("go %s: %w", args[0], err)
	}
	return 0, nil
}
