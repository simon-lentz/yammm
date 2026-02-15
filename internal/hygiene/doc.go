// Package hygiene provides programmatic verification of architectural invariants.
//
// This package contains tests that enforce layering constraints across the
// module. These tests serve as the authoritative gate for dependency hygiene;
// shell snippets in documentation are for convenience only.
//
// # Foundation Tier Import Rules
//
// The module has a tiered architecture where foundation packages must not
// import upper-tier packages:
//
//   - immutable: stdlib only (no other packages)
//   - location: stdlib + golang.org/x/text/unicode/norm (no other packages)
//   - diag: stdlib + location (no upper-tier packages)
//
// Upper-tier packages that foundation packages must NOT import:
//
//   - schema
//   - instance
//   - graph
//   - adapter/*
//   - internal/trace
//
// # Test Coverage
//
// [TestFoundationImports] verifies these constraints using `go list -deps -test`,
// which includes both production and test dependencies. This catches cases where
// test files violate layering even if production code is clean.
//
// Packages that don't exist yet are skipped. Once a foundation package is
// created, it will automatically be tested.
package hygiene
