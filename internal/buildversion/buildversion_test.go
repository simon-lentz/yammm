package buildversion

import (
	"runtime/debug"
	"testing"
)

func withMainVersion(v string) *debug.BuildInfo {
	return &debug.BuildInfo{Main: debug.Module{Version: v}}
}

func TestResolve_Matrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ldflags string
		bi      *debug.BuildInfo
		ok      bool
		want    string
	}{
		{"ldflags wins over build info", "v0.10.0", withMainVersion("v0.9.1"), true, "v0.10.0"},
		{"ldflags wins with no build info", "v0.10.0", nil, false, "v0.10.0"},
		{"dev falls back to stamped tag", "dev", withMainVersion("v0.10.0"), true, "v0.10.0"},
		{"empty falls back to stamped tag", "", withMainVersion("v0.10.0"), true, "v0.10.0"},
		{"dev falls back to VCS pseudo-version", "dev", withMainVersion("v0.9.2-0.20260727120000-abcdef123456"), true, "v0.9.2-0.20260727120000-abcdef123456"},
		{"(devel) carries no information", "dev", withMainVersion("(devel)"), true, "dev"},
		{"empty main version carries no information", "dev", withMainVersion(""), true, "dev"},
		{"missing build info keeps dev", "dev", nil, false, "dev"},
		{"nil build info despite ok keeps dev", "dev", nil, true, "dev"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := resolve(tc.ldflags, tc.bi, tc.ok); got != tc.want {
				t.Errorf("resolve(%q, %v, %v) = %q; want %q", tc.ldflags, tc.bi, tc.ok, got, tc.want)
			}
		})
	}
}

// Resolve never returns an empty string: every caller feeds the result into a
// user-facing --version line, and "" there reads as a broken binary.
func TestResolve_NeverEmpty(t *testing.T) {
	t.Parallel()
	if got := Resolve(""); got == "" {
		t.Error(`Resolve("") returned the empty string`)
	}
}
