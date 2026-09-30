package doclint

import (
	"strings"
	"testing"
)

// TestBuildContext_IsPinned pins that the constraint filter reads a fixed
// GOOS/GOARCH rather than the machine's. Inheriting them made the gate's
// verdict a property of where it ran: measured, the ambient context excludes
// nine tracked files under darwin/arm64 and linux/amd64 and ten under
// windows/amd64, so a Windows contributor and CI would not agree on what the
// module publishes. Returning build.Default turns this red.
func TestBuildContext_IsPinned(t *testing.T) {
	t.Parallel()
	ctx := buildContext()
	if ctx.GOOS != "linux" || ctx.GOARCH != "amd64" {
		t.Errorf("buildContext() = %s/%s, want a pinned linux/amd64", ctx.GOOS, ctx.GOARCH)
	}
}

// TestLoad_ReadsATaggedTestFile pins that a build constraint excludes a
// PRODUCTION file only. go doc renders no test declaration under any tag, so
// "outside the published documentation" cannot justify dropping a test file —
// and dropping one silently removed the regression anchors this gate exists to
// check, two of them live, while both trees stayed green. Applying the filter
// to test files again turns this red.
func TestLoad_ReadsATaggedTestFile(t *testing.T) {
	t.Parallel()
	m, err := Load("../..")
	if err != nil {
		t.Fatalf("loading the module: %v", err)
	}
	// adapter/neo4j/integration is behind a build tag and is nothing BUT test
	// files, so the whole directory vanished — TestMain among it.
	integration := packageAt(t, m, "adapter/neo4j/integration")
	if _, ok := integration.names["TestMain"]; !ok {
		t.Error("TestMain is not in the loaded name set: a tagged test file was excluded")
	}
}

// packageAt returns the loaded package whose directory ends in suffix.
func packageAt(t *testing.T, m *Module, suffix string) *Package {
	t.Helper()
	for _, p := range m.Packages {
		if strings.HasSuffix(p.Dir, suffix) {
			return p
		}
	}
	t.Fatalf("no package under a directory ending %q was loaded", suffix)
	return nil
}
