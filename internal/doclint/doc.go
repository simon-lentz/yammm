// Package doclint holds the module's documentation gates: doc links that name
// nothing, doc comments go doc renders wrongly, dependency lines, cited
// diagnostic codes, command invocations, and process references in names.
//
// # Why
//
// A removed symbol leaves its doc links behind. Go's own tooling does not
// complain: godoc renders a link to a symbol that no longer exists either as a
// link to nothing or, in the symbol's own package, as plain brackets, and
// golangci-lint's documentation linters check style, not reference resolution. Two releases in a row shipped documentation advertising
// API that had been cut, and no gate caught either.
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
// [AssertCitedCodesExist] reads the diagnostic code names written in tracked Go
// comments, Markdown files and shell scripts' comments, outside testdata and
// the caller's exclusions, against the registry the caller passes. A renamed or removed code leaves its name in prose exactly as a
// removed symbol leaves its links, and a code name is not a doc link, so the
// resolver cannot see it.
//
// # Invocations
//
// [AssertInvocationsExist] reads every invocation of a command-line program
// written in a tracked Markdown file, testdata included, against the program's
// commands and flags, which the caller passes as data. A file matching one of
// the caller's exclusions is not read. Outside a git work tree the gate walks
// the filesystem instead, skipping node_modules and dot-directories, and reads
// every Markdown file it reaches that no exclusion matches. A table entry the
// gate could never read back from an invocation, such as a flag name holding
// "=" or a space, is refused before any file is read. A renamed flag or
// command leaves its old spelling in every example that uses it, and a flag
// copied from a neighbouring command reads as plausible to every reviewer.
//
// The Markdown is read as GitHub reads it, CRLF and a lone CR as line endings:
// CommonMark with GitHub's extensions, parsed by goldmark, the reader
// adapter/markdown puts its structural questions to. An invocation is a line of
// a fenced code block that, less its indentation, is the program's name alone
// or the name followed by white space, or a code span that starts with the name
// and white space; a span may cross the lines of its paragraph, and each line
// ending in it reads as a space. A fenced command's first line may open with a
// "$" prompt. A line that ends in a backslash continues onto the next, and one
// that ends the block is read without it. An indented code block, an HTML block
// and a fence's info string hold no invocation.
//
// The invocation ends at shell text: a word that opens a comment ("#"), a pipe,
// "&&", "||", "&", or a redirection (">", "2>", "&>", or "<" alone). It also
// ends after a word that ends in ";". The words after the program name the
// command, looked up as cobra's Find looks one up: a level at a time, and at
// each level every word left is read with that level's flags by the rule of
// cobra's stripFlags. There a flag written without "=" takes the next word as
// its value, unless the command at that level defines it as taking no value; a
// flag the lookup does not know takes one, and so does a flag the program
// defines only after the lookup (Flag.AfterLookup), as cobra does its help and
// version flags. For a single dash, only a word of two bytes such as -o takes
// one, and a synopsis word with alternatives, such as [-w|--check], takes
// none. A synopsis word is read as the flag it names, where cobra would read
// "[--output" as a word. A lone "-" is not part of the lookup. The first
// other word that names no subcommand ends the lookup. When the command reached has subcommands,
// that word is reported unless it is a placeholder or a path: a word holding
// "<" or "[", or "." or "/". After "--" no word is read.
//
// Every flag is judged against the command the lookup reached, its own flags
// and the ones it inherits, read in order as pflag parses them: a flag that
// takes a value, written with no "=" and no value attached, takes the next
// word, dash and all. A synopsis's brackets and alternatives are read, so [--output
// <path>] names --output and [-w|--check] names both flags. A single-dash word
// is read as pflag reads one: its first character is a shorthand, "="
// included. The rest is that shorthand's value when it is "=" and at least one
// character more, or when the shorthand takes a value; otherwise the next
// character is another shorthand, as in -wv, so -w= names the shorthand "=".
// An unknown shorthand ends the word, as pflag stops there.
//
// The gate sees that a name exists. It cannot see a false claim about what a
// command does, such as a fallback the command never calls.
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
