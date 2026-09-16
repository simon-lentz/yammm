package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// TestDiagnosticsRoot_Relativizes pins where rendered diagnostic locations
// relativize: against the loaded schema's module root (or its canonicalized
// fallback), the same for every subcommand and independent of symlinks in
// the paths the user typed. Locations must render root-relative
// ("bad.yammm:3:1"), never as "." or as absolute resolved paths.
func TestDiagnosticsRoot_Relativizes(t *testing.T) {
	t.Parallel()

	// The only error is a missing primary key, anchored at the type
	// declaration on line 3.
	const badSchema = "schema \"bad\"\n\ntype Thing {\n\tname String\n}\n"

	t.Run("neo4j diff renders schema-load locations relative", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.yammm")
		if err := os.WriteFile(path, []byte(badSchema), 0o600); err != nil {
			t.Fatal(err)
		}

		code, stderr := executeCmdStderr(t, "neo4j", "diff", "--uri", "bolt://localhost:7687", path)
		if code != cli.ExitValidation {
			t.Errorf("exit code = %d, want %d", code, cli.ExitValidation)
		}
		if !strings.Contains(stderr, "bad.yammm:3") {
			t.Errorf("location should be root-relative, stderr:\n%s", stderr)
		}
		onDisk, err := yammmtest.DiskSpelling(dir)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(stderr, onDisk) {
			t.Errorf("location must not render absolute, stderr:\n%s", stderr)
		}
	})

	t.Run("gen with symlinked module root renders relative", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		real := filepath.Join(base, "real")
		if err := os.MkdirAll(real, 0o750); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(base, "ln")
		if err := os.Symlink("real", link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := os.WriteFile(filepath.Join(real, "bad.yammm"), []byte(badSchema), 0o600); err != nil {
			t.Fatal(err)
		}

		code, stderr := executeCmdStderr(t, "gen", "--to", "go", "--module-root", link, filepath.Join(link, "bad.yammm"))
		if code != cli.ExitValidation {
			t.Errorf("exit code = %d, want %d", code, cli.ExitValidation)
		}
		if !strings.Contains(stderr, "bad.yammm:3") {
			t.Errorf("location should be root-relative, stderr:\n%s", stderr)
		}
		onDisk, err := yammmtest.DiskSpelling(real)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(stderr, onDisk) {
			t.Errorf("location must not render absolute, stderr:\n%s", stderr)
		}
	})
}

// TestDiagRootFor_NeverReturnsASchemeString pins the guard that keeps a
// synthetic root out of the renderer. Schema.ModuleRoot now reports the
// synthetic root a load resolved against, and a scheme string is not a
// filesystem path: relativizing against it would produce nonsense locations.
// The CLI never synthetic-loads, so this is defence rather than a live path.
func TestDiagRootFor_NeverReturnsASchemeString(t *testing.T) {
	t.Parallel()

	sources := map[string][]byte{
		"assets/main.yammm": []byte("schema \"main\"\n\ntype T {\n\tid String primary\n}\n"),
	}
	s, res := schema.LoadSourcesWithEntry(t.Context(), sources, "assets/main.yammm", "",
		schema.WithSyntheticRoot("embedded://app"), schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("synthetic load: %v", res.Err())
	}
	if s.ModuleRoot() != "embedded://app" {
		t.Fatalf("fixture does not exercise the guard: ModuleRoot = %q", s.ModuleRoot())
	}

	entry := filepath.Join(t.TempDir(), "main.yammm")
	got := diagRootFor(s, "", entry)
	if strings.Contains(got, "://") {
		t.Errorf("diagRootFor = %q; a scheme string is not a path to relativize against", got)
	}
	if want := filepath.Dir(entry); !strings.HasPrefix(got, filepath.VolumeName(want)) || !filepath.IsAbs(got) {
		t.Errorf("diagRootFor = %q, want an absolute filesystem path", got)
	}
}

// TestDiagRootFor_DiscoversOnTheFailurePath pins that a failed load's
// diagnostics relativize against the same root the loader used. A successful
// load inherits discovery through Schema.ModuleRoot; a failed one has no
// schema, so the renderer must discover for itself or it relativizes against
// a root the loader did not use.
func TestDiagRootFor_DiscoversOnTheFailurePath(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, schema.ModuleRootMarker), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(sub, "bad.yammm")

	got := diagRootFor(nil, "", entry)
	onDisk, err := yammmtest.DiskSpelling(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != onDisk {
		t.Errorf("diagRootFor = %q, want the discovered root %q, not the entry directory", got, onDisk)
	}
}

// TestDiagRootFor_ResolvesAnExplicitRootOnTheFailurePath pins that a failed
// load's explicit root is the directory the loader resolved it to: a root typed
// through a link or in another case is returned as its directories list it.
func TestDiagRootFor_ResolvesAnExplicitRootOnTheFailurePath(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name string
		// typed returns a spelling of real, which sits in base.
		typed func(t *testing.T, base string) string
	}{
		{
			name: "typed through a link",
			typed: func(t *testing.T, base string) string {
				t.Helper()
				link := filepath.Join(base, "ln")
				if err := os.Symlink("real", link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				return link
			},
		},
		{
			name: "typed in another case",
			typed: func(t *testing.T, base string) string {
				t.Helper()
				if !yammmtest.CaseFoldingFilesystem(t, base) {
					t.Skip("the filesystem is case-sensitive, so two spellings name two directories")
				}
				return filepath.Join(base, "REAL")
			},
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			base := t.TempDir()
			real := filepath.Join(base, "real")
			if err := os.Mkdir(real, 0o750); err != nil {
				t.Fatal(err)
			}
			typed := row.typed(t, base)

			got := diagRootFor(nil, typed, filepath.Join(typed, "bad.yammm"))
			onDisk, err := yammmtest.DiskSpelling(real)
			if err != nil {
				t.Fatal(err)
			}
			if got != onDisk {
				t.Errorf("diagRootFor(nil, %q, ...) = %q, want %q", typed, got, onDisk)
			}
		})
	}
}
