package scripttest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/gittree"
)

// lintTargets is the order scripts/lint.sh lints in: this host, then Windows
// unless this host is Windows.
func lintTargets() []string {
	if runtime.GOOS == "windows" {
		return []string{"windows"}
	}
	return []string{runtime.GOOS, "windows"}
}

// newLintFixture returns a module the pinned linter can be built in and run on:
// the repository's module requirements and lint configuration, scripts/lint.sh
// and the script it sources, and one package. A golangci-lint that fails every
// run is first on its PATH, so a script that takes the linter from there, not
// from the module, lints nothing.
func newLintFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, dir: t.TempDir()}
	f.copyFile("go.mod")
	f.pinGoDirective()
	f.copyFile("go.sum")
	f.copyFile(".golangci.yml")
	f.copyScript("lint.sh")
	f.copyScript("toolchain.sh")
	f.write("ok/ok.go", "package ok\n")
	onPath := t.TempDir()
	gittree.WriteFile(t, onPath, "golangci-lint", []byte("#!/usr/bin/env bash\necho 'the golangci-lint on PATH ran' >&2\nexit 1\n"), 0o700)
	f.env = []string{
		// No build of the linter needs the network: its source and its
		// module's requirements are in the module cache.
		"HTTP_PROXY=http://127.0.0.1:9", "HTTPS_PROXY=http://127.0.0.1:9", "NO_PROXY=",
		"PATH=" + onPath + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
	return f
}

// toolPathGo is a go that answers `go tool -n golangci-lint` with the path
// GO_SHIM_LINTER holds and leaves every other command to the real toolchain.
const toolPathGo = `#!/usr/bin/env bash
if [ "$*" = "tool -n golangci-lint" ]; then
	printf '%s\n' "$GO_SHIM_LINTER"
	exit 0
fi
exec "$GO_SHIM_REAL" "$@"
`

// The script lints only with a linter go built for it. With none, it stops
// before any target and prints no summary, which would report a lint that did
// not run.
func TestLintScript_StopsWithoutALinterToRun(t *testing.T) {
	t.Parallel()
	newBareFixture := func(t *testing.T) *fixture {
		t.Helper()
		f := &fixture{t: t, dir: t.TempDir()}
		f.write("go.mod", "module "+fixtureModule+"\n\ngo "+goDirective()+"\n")
		f.copyScript("lint.sh")
		f.copyScript("toolchain.sh")
		f.write("ok/ok.go", "package ok\n")
		f.index()
		return f
	}
	wantNoSummary := func(t *testing.T, r result) {
		t.Helper()
		for _, summary := range []string{"lint: clean", "lint: FAILED"} {
			if strings.Contains(r.stdout+r.stderr, summary) {
				t.Errorf("the run printed a summary, %q, and linted nothing\nstdout:\n%s\nstderr:\n%s", summary, r.stdout, r.stderr)
			}
		}
	}

	t.Run("the module names no such tool", func(t *testing.T) {
		t.Parallel()
		r := newBareFixture(t).run("lint.sh")
		if r.code == 0 {
			t.Errorf("exit 0 with no linter\nstdout:\n%s\nstderr:\n%s", r.stdout, r.stderr)
		}
		wantNoSummary(t, r)
	})

	t.Run("go names a path that holds no file", func(t *testing.T) {
		t.Parallel()
		f := newBareFixture(t)
		real, err := exec.LookPath("go")
		if err != nil {
			t.Fatal(err)
		}
		shim := t.TempDir()
		gittree.WriteFile(t, shim, "go", []byte(toolPathGo), 0o700)
		gone := filepath.Join(shim, "removed", "golangci-lint")
		f.env = []string{
			"PATH=" + shim + string(os.PathListSeparator) + os.Getenv("PATH"),
			"GO_SHIM_REAL=" + real,
			"GO_SHIM_LINTER=" + gone,
		}

		r := f.run("lint.sh")
		r.wantCode(t, 2)
		r.wantStderr(t, "lint: go tool -n names no linter this run can execute: "+gone+"\n")
		wantNoSummary(t, r)
	})
}

// TestLintScript_LintsThisHostAndWindows holds the script to reading the
// Windows build: a file only Windows compiles, whose one call drops an error,
// fails the Windows target and no other. With --host the script reads this
// host's build alone, so off Windows that file fails nothing.
func TestLintScript_LintsThisHostAndWindows(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name     string
		windows  string
		wantCode int
		stdout   string
		stderr   string
	}{
		{
			name:     "a Windows file that lints clean",
			windows:  "//go:build windows\n\npackage ok\n\nimport \"os\"\n\nfunc Sep() string { return string(os.PathSeparator) }\n",
			wantCode: 0,
			stdout:   "lint: clean for " + strings.Join(lintTargets(), " ") + "\n",
		},
		{
			name:     "a Windows file that drops an error",
			windows:  "//go:build windows\n\npackage ok\n\nimport \"os\"\n\nfunc Move() { os.Chdir(\"x\") }\n",
			wantCode: 1,
			stdout:   "os.Chdir",
			stderr:   "lint: FAILED for windows (of " + strings.Join(lintTargets(), " ") + ")\n",
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			f := newLintFixture(t)
			f.write("ok/move_windows.go", row.windows)
			f.index()

			r := f.run("lint.sh")
			r.wantCode(t, row.wantCode)
			r.wantStdout(t, row.stdout)
			if row.stderr != "" {
				r.wantStderr(t, row.stderr)
			}

			// A Windows host's own build is the Windows build, so --host
			// changes nothing there.
			code, stdout, stderr := row.wantCode, row.stdout, row.stderr
			if runtime.GOOS != "windows" {
				code, stdout, stderr = 0, "lint: clean for "+runtime.GOOS+"\n", ""
			}
			host := f.run("lint.sh", "--host")
			host.wantCode(t, code)
			host.wantStdout(t, stdout)
			if stderr != "" {
				host.wantStderr(t, stderr)
			}
			// The arguments after --host still reach the linter, which prints
			// its usage and lints nothing.
			f.run("lint.sh", "--host", "--help").wantStdout(t, "golangci-lint run [flags]")
		})
	}
}
