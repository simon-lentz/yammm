package scripttest

import (
	"runtime"
	"strings"
	"testing"
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
// the repository's module requirements and lint configuration, the script, and
// one package.
func newLintFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, dir: t.TempDir()}
	f.copyFile("go.mod")
	f.copyFile("go.sum")
	f.copyFile(".golangci.yml")
	f.copyScript("lint.sh")
	f.write("ok/ok.go", "package ok\n")
	// The linter and its module come from the module cache.
	f.env = []string{"HTTP_PROXY=http://127.0.0.1:9", "HTTPS_PROXY=http://127.0.0.1:9", "NO_PROXY="}
	return f
}

// TestLintScript_LintsThisHostAndWindows holds the script to reading the
// Windows build: a file only Windows compiles, whose one call drops an error,
// fails the Windows target and no other.
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
		})
	}
}
