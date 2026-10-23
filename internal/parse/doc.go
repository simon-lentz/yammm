// Package parse turns YAMMM schema source into a syntax-level node tree and
// the diagnostics found along the way.
//
// # Position in the stack
//
// parse sits below the schema object model. Of this module's packages it
// depends only on the foundation packages — [github.com/simon-lentz/yammm/diag],
// [github.com/simon-lentz/yammm/location] and
// [github.com/simon-lentz/yammm/schema/expr] — and knows nothing about type
// resolution, inheritance, imports or any other semantic phase. Everything it
// reports is a fact about the text.
//
// The package lives at the repository root rather than under schema/ because
// Go scopes an internal/ subtree to importers under its parent: consumers
// outside schema/ could not import it from there.
//
// # What Parse reports and what it does not
//
// Parse reports the diagnostics a reader of the source alone can justify, and
// it emits five codes. E_SYNTAX covers text that does not fit the grammar, and
// the source rules: a byte that is not valid UTF-8, a byte order mark past the
// start and a NUL are refused at the byte inside a comment or a string or
// regex literal, one diagnostic for each run of one kind; anywhere else one
// diagnostic, and the parser recovers from it as from any unexpected token: a
// schema header holding it is a Fatal E_SYNTAX; any other import, declaration
// or member holding it is cut where the parser can end it or lost, as
// Recovery's paragraph on how far a failed parse propagates says; a member on
// whose line it comes first is lost; and nothing between two declarations
// fails, though every import after it is lost with a diagnostic of its own;
// a string literal whose escapes write bytes that are not UTF-8 is refused at
// the literal — under E_INVALID_INVARIANT, below, when the literal stands
// inside an invariant expression, unless that invariant's message holds an
// escape that will not unquote, which is then its one diagnostic — except an
// annotation argument, which keeps its written spelling and draws nothing. A
// literal refused either way has no value: no later check judges a value it
// does not have, and an enum's and a pattern list's count read it as a value
// written. Of a regex whose escapes unquote, one holding a NUL or a byte order
// mark is still compiled, as written, so its own compile error stands beside
// the source rule's (in an invariant, unless its message holds an escape that
// will not unquote, as above), and one holding a byte that is not UTF-8 is not
// compiled and draws the byte's diagnostic alone. E_INVALID_CONSTRAINT covers a
// constraint whose written arguments the DSL refuses, among them inverted
// bounds, an unparseable bound, an empty or duplicate enum value,
// an enum of fewer than two values, more than two patterns, a regex that does
// not compile or that writes a surrogate code point, and a vector dimension
// out of range. E_INVALID_INVARIANT covers a literal inside
// an invariant expression that will not convert, such as a number out of range
// or an unquotable string. E_INVALID_NAME covers an empty schema name and a
// relation name that is not UPPER_SNAKE, and E_REVERSE_CLAUSE_REMOVED a
// relation that still writes the removed reverse clause. Parse never reports anything that needs another
// declaration, another file, or a resolved name to decide.
//
// Four consequences are worth stating because callers depend on them.
// E_SYNTAX does not say that the source failed to parse: a caller that needs
// the token stream and the tree's shape alone reads [File.Unparsed], which
// holds the diagnostics for text the grammar does not match, an escape the
// language does not have outside an invariant expression among them, and
// leaves out the source rules' and an escape whose value is not UTF-8. A \u
// escape naming a surrogate is one the language does not have.
// Callers that want syntax errors alone must filter on the code category
// (diag.CategorySyntax), not on the presence of any diagnostic at all.
// Only E_SYNTAX is diag.CategorySyntax: the other four are
// diag.CategorySchema, so that filter keeps one code of the five and drops
// every constraint, invariant and name defect — consume the
// whole slice unless a narrower set is what you mean. And callers must not
// assume a diagnostic-free parse means a valid schema; it means a well-formed
// one.
//
// # Recovery
//
// Parsing is a hand-driven loop, not a single grammar match. Declarations and
// type-body members are parsed one at a time; a construct that fails reports
// one E_SYNTAX at the deepest position any alternative reached, then the loop
// skips forward to the next plausible construct start and keeps going. So a
// file with several independent mistakes reports several diagnostics in one
// pass, and every declaration that did parse survives into the tree.
//
// Parse always returns a file node. A source that fails entirely yields an
// empty node plus diagnostics, never nil.
//
// One rule overrides the diagnostic a failed construct would otherwise carry:
// a malformed numeric literal anywhere inside the construct owns the
// diagnosis. "0x10 is not a number" explains the failure, where the token the
// parser happened to stop on describes only its symptom — and the parser can
// stop before ever reaching the literal, since Integer[0x10, 5] fails at the
// '[' because an Integer with no bounds is itself well-formed. The construct's
// extent is therefore measured after recovery has run, which is the first
// point at which the whole of a malformed construct is known. The schema
// header is the exception: its recovery runs to the first declaration, so its
// extent ends where the failed parse reached, and a literal in a declaration
// it skipped does not take over its diagnostic.
//
// Diagnostic text names the construct, never the grammar. participle renders
// its own expected-set as the EBNF of this package's node structs, which names
// nothing in the YAMMM language and changes shape whenever a struct or a
// struct tag is edited. Each site passes the construct it was parsing and the
// unexpected token is reported against that, naming the region the recovery
// loop was in: the schema header, an import declaration, a declaration, or a
// type body. [TestParse_DiagnosticsNameNoGrammarTypes] holds the rule, deriving
// the banned names from the grammar itself so a node added later is covered.
//
// How far a failed parse propagates is participle's decision, not this
// package's: see group.Parse, disjunction.Parse and parseContext.Stop at the
// pinned version. A failed optional part is sometimes discarded, leaving the
// construct around it intact, and sometimes takes that construct down with it.
// This package does not restate the rule that picks between them; the
// behaviour is recorded by measurement instead.
//
// [TestAbandonment_GroupsReportAgainstTheEnclosingConstruct] and
// [TestRecovery_UnterminatedGroupsAreReported] hold what the parser does at the
// sites those tables reach: the diagnostic, its anchor, and what survives into
// the tree. They are measured baselines, not a specification of the grammar and
// not a survey of it — a site absent from them is unmeasured, not exempt. Every
// row reports E_SYNTAX at diag.Error, so no malformed source loads through the
// sites they cover.
//
// A diagnostic names the recovery region rather than the construct that
// failed: the construct is participle's to decide, and the region is what the
// parser can state.
//
// # Spans
//
// Positions are byte-native. Every span is built from byte offsets:
// spanFromOffsets derives each end's line and column from the source text,
// counting columns in runes as the lexer does, so a span agrees with the
// positions the lexer records for a token, save after a lone CR, which
// spanOf's comment states, and on line 1 after a leading byte order mark,
// which a span counts as a column and the lexer does not. Span ends are exclusive.
//
// [location.RangeWithBytes] panics when the end precedes the start, so every
// span this package builds — for nodes and for diagnostics, in the recovery
// paths above all — is constructed start-before-end by force rather than by
// assumption. See spanOf.
//
// The location.SourceID passed to Parse names the source spans belong to.
// Passing the zero SourceID is supported: spans still carry byte offsets and
// rune line and column, and only the source identity is absent. Callers that
// format or lint a bare string, with no file behind it, use that mode.
//
// # Lexical discipline
//
// The rule table is matched first-listed-first-matched, with no maximal munch
// across rules, so its order is part of the specification rather than a
// stylistic choice. It re-encodes, pair by pair, the outcomes a
// longest-match-then-declaration-order discipline produces for every
// overlapping token shape:
//
//   - Comment forms and REGEXP come before SLASH. "//…" and "/*…*/" win over a
//     regex literal on ties, and a '/' with no closing partner on the same line
//     falls through to division — the same reading the LSP's brace scanner
//     encodes.
//   - FLOAT, word-boundary-anchored, comes before INVALID_NUMBER, which comes
//     before INTEGER, so "2.5e10" lexes as one FLOAT. The trailing \b makes
//     FLOAT decline exactly the inputs where junk follows, so
//     "1.5x" and "1.5e5x" fall through to INVALID_NUMBER while every valid
//     float wins. INTEGER needs no boundary: a digit run with trailing junk was
//     already taken by INVALID_NUMBER.
//   - Every multi-character operator comes before its single-character prefix:
//     -->, *->, ->, >=, <=, ==, =~, !=, !~, &&, ||, @@.
//   - VARIABLE comes before DOLLAR, so "$name" lexes as one VARIABLE token
//     rather than DOLLAR and a word.
//   - ANY_OTHER is last: one rune, unconditional. It keeps the lexer total, so
//     every rejection happens in the parser with a position attached and no
//     input can fail to lex.
//
// The table contains no keywords. Every keyword lexes as LC_WORD or UC_WORD
// and reserved-word behavior is a parser decision, expressed as negative
// lookahead groups in the grammar's struct tags and as the reservedLC,
// datatypeKeywords and propertyNameExclusions maps. The struct tags are compile-time strings, so those
// groups are written out in place rather than composed from the maps;
// [TestTags_LookaheadGroupsAreAtTheirCanonicalSites] holds every copy to its
// map and every site to its set.
//
// The string-escape set matches the language's STRING rule exactly: a
// backslash before any character outside the escape vocabulary makes the whole
// literal fail to lex, falling through to ANY_OTHER at the opening quote. A \x
// or \u escape lexes whatever follows it, and unquoting refuses malformed
// digits, a surrogate code point, a value that is not valid UTF-8 and text the
// source rules refuse.
//
// # Expression precedence
//
// Invariant expressions are parsed by a Pratt parser whose binding powers are
// the language's own law. Three of them contradict what a C-family reading
// predicts, and all three are observable:
//
//   - Indexing, pipeline calls and property access are one postfix level and
//     chain left to right, so "$self.tags[0]" indexes the property and
//     "$i.name -> Len" pipes it. Unary minus binds tighter than all three; '!'
//     binds looser. So negating an indexed value parses as "index the negated
//     operand", while negating with '!' parses as "negate the indexed operand".
//   - 'in' binds tighter than the regex-match operators, which bind tighter
//     than equality.
//   - '^' is exclusive-or, at the same level as logical or. It is not a power
//     operator.
//
// # Concurrency
//
// Parse is safe for concurrent use. The compiled parsers and the lexer
// definition are built once and are immutable afterwards; each parse builds
// its own lexer instance and its own builder state.
package parse
