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
// directory that the default build includes contributes names, test files
// included; a file behind a build constraint is outside the default build's
// documentation and outside this gate, as it is outside go doc. That is what lets a
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
// gate's business. A link under the module's own path is not outside it: when
// no package holds that path, the link dangles. Two paths under it are still
// another module's: a directory that carries its own go.mod, and a first
// element such as v2 that names a major version and no directory here.
//
// A directory whose every non-test file is behind a constraint the pinned
// context excludes still holds a package, one that exists under another build.
// A link into it resolves against the names that build declares; a link
// written inside it is not checked.
//
// # Code names
//
// [AssertCitedCodesExist] reads every diagnostic code name written in a Go
// comment or a Markdown file against the registry the caller passes. A renamed
// or removed code leaves its name in prose exactly as a removed symbol leaves
// its links, and a code name is not a doc link, so the resolver cannot see it.
//
// # Names
//
// [AssertProcessFreeNames] refuses a process reference in a name: a tracked
// path's file or directory name, testdata included, or a Test, Fuzz, Benchmark
// or Example function. A name states what its file or test holds. A review
// round, a fix-pass group or a row identifier does not, and it outlives the
// plan that gave it meaning.
//
// A name is split into lowercase words at every non-alphanumeric rune and at
// camel-case boundaries; a digit run stays on the word before it. A word is a
// process reference when it is residue, slate, tranche, fixpass or fixdiff; a
// stage word (tier, group, round, unit, step, phase, wave, pass, batch, stage,
// sweep, clause) carrying a number, as group3 or group_3; gate followed by fix or fixes,
// or fix followed by pass or diff; or a single letter and digits, as g11, p2
// and a01. Two words of that last shape are legitimate and pass: a version
// such as v2, and o1 for constant time. An all-lowercase run such as a fuzz
// corpus hash is one word, so it never takes the row-identifier shape.
package doclint
