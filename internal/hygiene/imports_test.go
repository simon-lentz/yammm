// Package hygiene provides programmatic verification of layering invariants.
package hygiene

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestFoundationImports verifies that foundation tier packages do not import
// upper-tier packages. This test is the authoritative gate for dependency
// hygiene.
//
// Foundation tier packages and their constraints:
//   - immutable: stdlib only (no other module packages)
//   - location: stdlib + golang.org/x/text/unicode/norm (no other module packages)
//   - diag: stdlib + location (no upper-tier packages)
//
// The -test flag is used to include test dependencies, catching cases where
// test files violate layering even if production code is clean.
//
// Packages that don't exist yet are skipped. Once a foundation package is
// created, it will automatically be tested.
func TestFoundationImports(t *testing.T) {
	modRoot := findModuleRoot(t)
	modPath := getModulePath(t, modRoot)

	// Define forbidden path suffixes (appended to module path)
	cases := []struct {
		pkg             string   // relative to module root (without ./)
		forbiddenSuffix []string // suffixes to append to module path for forbidden imports
	}{
		{
			pkg: "location",
			forbiddenSuffix: []string{
				"/schema",
				"/instance",
				"/graph",
				"/internal/trace",
				"/adapter",
				"/diag", // location is the lowest layer; cannot import diag
			},
		},
		{
			pkg: "diag",
			forbiddenSuffix: []string{
				"/schema",
				"/instance",
				"/graph",
				"/internal/trace",
				"/adapter",
				// diag may import location
			},
		},
		{
			pkg: "immutable",
			forbiddenSuffix: []string{
				"/schema",
				"/instance",
				"/graph",
				"/internal/trace",
				"/adapter",
				"/diag",
				"/location",
			},
		},
		{
			// trace is NOT a foundation tier package, but it must have
			// stdlib-only dependencies. It can be imported by core library
			// tier packages (schema, instance, graph) and adapters.
			pkg: "internal/trace",
			forbiddenSuffix: []string{
				"/schema",
				"/instance",
				"/graph",
				"/adapter",
				"/diag",
				"/location",
				"/immutable",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.pkg, func(t *testing.T) {
			pkgDir := filepath.Join(modRoot, tc.pkg)
			if _, err := os.Stat(pkgDir); os.IsNotExist(err) {
				t.Skipf("package %s not yet implemented", tc.pkg)
			}

			// Build full forbidden paths from module path + suffixes
			forbidden := make([]string, len(tc.forbiddenSuffix))
			for i, suffix := range tc.forbiddenSuffix {
				forbidden[i] = modPath + suffix
			}

			// -test includes test dependencies
			// Package path is validated against the cases table; not user input.
			ctx := t.Context()
			cmd := exec.CommandContext(ctx, "go", "list", "-deps", "-test", "-f", "{{.ImportPath}}", "./"+tc.pkg) //nolint:gosec // pkg is from test table, not user input
			cmd.Dir = modRoot

			out, err := cmd.Output()
			if err != nil {
				if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
					t.Fatalf("go list failed: %v\nstderr: %s", err, exitErr.Stderr)
				}
				t.Fatalf("go list failed: %v", err)
			}

			for imp := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
				for _, forbiddenPath := range forbidden {
					if strings.Contains(imp, forbiddenPath) {
						t.Errorf("forbidden import %q in %s", imp, tc.pkg)
					}
				}
			}
		})
	}
}

// findModuleRoot locates the module root from the test's location.
func findModuleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to determine test file location")
	}
	// imports_test.go is in internal/hygiene/
	// walk up to module root
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// getModulePath returns the module path by invoking 'go list -m'.
func getModulePath(t *testing.T, modRoot string) string {
	t.Helper()
	ctx := t.Context()
	cmd := exec.CommandContext(ctx, "go", "list", "-m", "-f", "{{.Path}}")
	cmd.Dir = modRoot
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			t.Fatalf("go list -m failed: %v\nstderr: %s", err, exitErr.Stderr)
		}
		t.Fatalf("go list -m failed: %v", err)
	}
	return strings.TrimSpace(string(out))
}
