// Package doclint resolves the doc links in this module's comments and reports
// the ones that name nothing.
//
// # Why
//
// A removed symbol leaves its doc links behind. Go's own tooling does not
// complain: godoc renders a link to a symbol that no longer exists as an
// ordinary link, and golangci-lint's documentation linters check style, not
// reference resolution. Two releases in a row shipped documentation advertising
// API that had been cut, and neither was found by a gate — the first by a
// cleanup pass that happened to open the file, the second by a consumer trying
// to upgrade.
//
// # What a link is
//
// Only a bracketed name whose final component is a capitalized Go identifier is
// a link: Name, Type.Method, Type.Field, pkg.Name, pkg.Type.Method, each in
// brackets. That is go/doc/comment's own rule, deliberately inherited here: a
// bracketed lowercase name such as someHelper renders as literal text in
// published documentation, so it is not a link and this package does not resolve
// it. A reference of that shape naming a deleted symbol is real rot, but it is
// rot of a different class and needs a different instrument.
//
// # Resolution
//
// The resolution unit is the directory, not the package: every .go file in a
// directory contributes names, test files included. That is what lets a
// production doc comment anchor a regression test by name — the convention the
// repo's comment rules sanction — while still catching an anchor that names a
// test somebody deleted.
//
// A qualified link resolves its package part against the referencing file's own
// imports first. This module has an adapter/json package, so a bare
// package-name match for a json-qualified link would collide with encoding/json.
// When the file does not import the name, a unique in-module package with that
// name is the fallback, which is how a comment can reference a package that
// would be an import cycle to depend on. Links resolving outside the module are
// skipped: the standard library and this module's dependencies are not this
// gate's business.
package doclint
