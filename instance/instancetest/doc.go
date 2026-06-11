// Package instancetest provides importable test support for constructing
// instance-layer values in tests across packages.
//
// It exists because test helpers that multiple test packages need — the
// graph, snapshot, and adapter test suites all construct
// [github.com/simon-lentz/yammm/instance] values — require an importable
// home: internal/ paths under instance/ are not visible to the adapter
// packages, and the generic test toolkit (internal/yammmtest) stays
// domain-free by design. The precedent is snapshot/snapshottest.
//
// Like snapshottest, this is test support: it may import "testing" and
// accept testing.TB, and it must never be imported by non-test code.
// Helpers follow the conventions documented in internal/yammmtest: builders
// return values, never store the testing.T, and keep the scenario under
// test visible at the call site.
package instancetest
