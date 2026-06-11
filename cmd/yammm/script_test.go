package main

import (
	"os"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/simon-lentz/yammm/internal/yammmtest"
)

// TestMain registers the real CLI entrypoint as the `yammm` command for
// testscript: `exec yammm …` in a testdata/script/*.txtar script re-execs
// this (instrumented) test binary straight into run(), so script executions
// count toward coverage and capture the exact stdout/stderr the shipped
// binary produces — including the os.Stderr writes that in-process cobra
// buffers cannot see.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"yammm": func() { os.Exit(run()) },
	})
}

// TestScripts runs every testdata/script/*.txtar scenario. The scripts own
// the success/failure and output contracts; exact exit-code distinctions
// (usage vs validation vs runtime) stay in the in-process executeCmd tests,
// since testscript can only assert pass/fail.
func TestScripts(t *testing.T) {
	t.Parallel()
	testscript.Run(t, testscript.Params{
		Dir:           "testdata/script",
		UpdateScripts: yammmtest.Update(),
	})
}
