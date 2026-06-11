// Package yammmtest provides the repo-wide test support shared by package
// test suites: golden-file comparison behind a single -update flag,
// structural diffs via go-cmp, and a record-capturing slog handler.
//
// # Golden files
//
// [Golden] compares bytes against testdata/<name>.golden in the calling
// package's directory; [GoldenJSON] canonically marshals a value first.
// Both rewrite the file when the -update flag is set:
//
//	go test ./adapter/neo4j/... -update
//
// The flag is registered only in test binaries that import yammmtest, so
// always pass -update per package — a repo-wide `go test ./... -update`
// fails with "flag provided but not defined" in packages without goldens.
// Review every regenerated diff before committing: a rubber-stamped mass
// regeneration defeats the point of a golden.
//
// # House conventions for test helpers
//
// Helpers prefixed assert* report and continue (t.Errorf); helpers prefixed
// must* or require* stop the test (t.Fatalf). Table expectation fields are
// named want, wantErr, wantCode. Every helper calls t.Helper(). Builders
// return values and never store the testing.T; teardown registers with
// t.Cleanup rather than returning a closure.
//
// Helpers extract mechanical setup only: the scenario under test and its
// expectation stay readable in the test body, and assertion helpers stay
// domain-shaped (assertHasCode, assertIssue) rather than growing into a
// generic assertion DSL. A helper is worth extracting only when it is
// heavily reused, cannot plausibly be the thing that is wrong when the test
// fails, and keeps failures attributable (t.Helper plus discriminating
// arguments in the failure output).
//
// New test code does not import github.com/stretchr/testify — enforced by
// depguard; the files that imported testify before the freeze are
// grandfathered in .golangci.yml and that list only shrinks. New tests use
// this package plus the standard library.
package yammmtest
