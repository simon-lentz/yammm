// Package buildversion resolves the version a binary reports, preferring the
// ldflags-injected value and falling back to the main-module version Go stamps
// into the build.
//
// The fallback is what makes `go install module/cmd/...@vX.Y.Z` binaries
// self-identifying: such builds never receive the Makefile's ldflags, so
// without it every tag-installed binary reports the "dev" default and a stale
// install is indistinguishable from a current one.
package buildversion

import "runtime/debug"

// Resolve returns the version the binary should report: the ldflags value when
// one was injected, else the main-module version from the running binary's
// build info.
func Resolve(ldflags string) string {
	bi, ok := debug.ReadBuildInfo()
	return resolve(ldflags, bi, ok)
}

// resolve is the pure core of [Resolve], split from the debug.ReadBuildInfo
// read so the whole decision matrix is unit-testable without a `go install`
// round trip.
//
// An ldflags value other than "" or "dev" always wins — the release workflow
// stamps the tag through it and that value is authoritative. Otherwise the
// build info's main-module version is used when it carries information:
// `go install pkg@vX.Y.Z` stamps the tag, and Go 1.24+ `go build` stamps a
// VCS-derived version. "(devel)" and an empty version carry no more
// information than "dev" and must not masquerade as a release version, so
// they keep the "dev" default.
func resolve(ldflags string, bi *debug.BuildInfo, ok bool) string {
	if ldflags != "" && ldflags != "dev" {
		return ldflags
	}
	if !ok || bi == nil {
		return "dev"
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	return "dev"
}
