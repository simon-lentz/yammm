// Package internal holds no code and has no API. It is the home of the tests
// that pin the behaviour of github.com/tidwall/jsonc that jsondata.Parse
// depends on, kept apart from jsondata's own suite because they test a
// dependency.
//
// Run them before a release that upgrades the dependency:
//
//	go test ./cmd/yammm/internal/jsondata/internal/...
package internal
