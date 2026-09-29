package scripttest

import (
	"os"
	"strings"
	"testing"
)

func TestLintConfigScript_RefusesAKeyTheSchemaRefuses(t *testing.T) {
	t.Parallel()
	own, err := os.ReadFile(fromRoot(".golangci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	rows := []struct {
		name    string
		config  string
		refused bool
	}{
		{"the repository's config", string(own), false},
		{"an unknown top-level key", "version: \"2\"\nbogus_top_level_key: true\n", true},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			// The script reads the schema the pinned linter's module ships, so the
			// fixture carries the repository's module requirements.
			f := &fixture{t: t, dir: t.TempDir()}
			f.copyFile("go.mod")
			f.copyFile("go.sum")
			f.pinGoDirective()
			f.copyScript("lintconfig.sh")
			f.copyScript("toolchain.sh")
			f.write(".golangci.yml", row.config)
			f.index()
			// The schema comes from the module cache, so the run needs no network.
			f.env = []string{"HTTP_PROXY=http://127.0.0.1:9", "HTTPS_PROXY=http://127.0.0.1:9", "NO_PROXY="}

			r := f.run("lintconfig.sh")
			if !row.refused {
				r.wantCode(t, 0)
				return
			}
			if r.code == 0 {
				t.Fatalf("exit 0 for a config the schema refuses\nstdout:\n%s\nstderr:\n%s", r.stdout, r.stderr)
			}
			if !strings.Contains(r.stdout+r.stderr, "bogus_top_level_key") {
				t.Errorf("the refusal does not name the key\nstdout:\n%s\nstderr:\n%s", r.stdout, r.stderr)
			}
		})
	}
}
