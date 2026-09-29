// Package doclint holds the module's documentation gates: doc links that name
// nothing, doc comments go doc renders wrongly, dependency lines, cited
// diagnostic codes, and process references in names.
//
// # Why
//
// A removed symbol leaves its doc links behind. Go's own tooling does not
// complain: godoc renders a link to a symbol that no longer exists either as a
// link to nothing or, in the symbol's own package, as plain brackets, and
// golangci-lint's documentation linters check style, not reference resolution. Two releases in a row shipped documentation advertising
// API that had been cut, and neither was found by a gate — the first by a
// cleanup pass that happened to open the file, the second by a consumer trying
// to upgrade.
//
// # What a link is
//
// A link is what go/doc/comment makes one, a rule deliberately inherited here:
// a bracketed name whose final component is a capitalized Go identifier (Name,
// Type.Method, Type.Field, pkg.Name, pkg.Type.Method), or a bracketed package:
// an import path holding a slash, a package name the file imports, the name of
// the one in-module package that carries it, or a standard-library package
// whose import path holds no slash, such as fmt. A package link resolves when
// the package exists. Any other bracketed lowercase name, such as
// someHelper, renders as literal text in published documentation, so it is not
// a link and this package does not resolve it. A reference of that shape naming
// a deleted symbol is real rot, but it is rot of a different class and needs a
// different instrument.
//
// # Resolution
//
// The resolution unit is the directory, not the package: every .go file in a
// directory contributes names, test files included. A non-test file behind a
// build constraint that the pinned linux/amd64 context excludes is outside this
// gate, as it is outside go doc there; a test file is read under any
// constraint. That is what lets a
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
// context excludes still holds a package, one that exists under other builds,
// unless every such file is marked //go:build ignore. A link into it resolves
// against the names its excluded files declare, whichever build each belongs
// to; a link written in its non-test files is not checked, and its test files
// are read like any other.
//
// # Code names
//
// [AssertCitedCodesExist] reads every diagnostic code name written in a Go
// comment, a Markdown file or a shell script's comment against the registry the
// caller passes. A renamed or removed code leaves its name in prose exactly as a
// removed symbol leaves its links, and a code name is not a doc link, so the
// resolver cannot see it.
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
// corpus hash is one word, and it takes the row-identifier shape only when one
// letter is followed by nothing but digits.
package doclint
