// Package parse turns YAMMM schema source into a syntax-level node tree and
// the diagnostics found along the way.
//
// # Position in the stack
//
// parse sits below the schema object model. It depends only on the foundation
// packages — [github.com/simon-lentz/yammm/diag],
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
// it emits three codes. E_SYNTAX covers text that does not fit the grammar.
// E_INVALID_CONSTRAINT covers a constraint whose written arguments contradict
// themselves — inverted bounds, an unparseable bound, a duplicate enum value,
// a regex that does not compile. E_INVALID_INVARIANT covers a literal inside
// an invariant expression that will not convert, such as a malformed number or
// an unquotable string. Parse never reports anything that needs another
// declaration, another file, or a resolved name to decide.
//
// Three consequences are worth stating because callers depend on them.
// Callers that want syntax errors alone must filter on the code category
// (diag.CategorySyntax), not on the presence of any diagnostic at all.
// Only E_SYNTAX is diag.CategorySyntax: both E_INVALID_CONSTRAINT and
// E_INVALID_INVARIANT are diag.CategorySchema, so that filter keeps one code
// of the three and drops every constraint and invariant defect — consume the
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
// point at which the whole of a malformed construct is known.
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
// This package does not restate the rule that picks between them — three
// attempts to paraphrase it were each found wrong, and the behaviour is
// recorded by measurement instead.
//
// [TestAbandonment_GroupsReportAgainstTheEnclosingConstruct] and
// [TestRecovery_UnterminatedGroupsAreReported] hold what the parser does at the
// sites those tables reach: the diagnostic, its anchor, and what survives into
// the tree. They are measured baselines, not a specification of the grammar and
// not a survey of it — a site absent from them is unmeasured, not exempt. Every
// row reports E_SYNTAX at diag.Error, so no malformed source loads through the
// sites they cover.
//
// Naming the recovery region rather than the construct that failed is a known
// divergence from the ANTLR parser, which names the construct.
//
// # Spans
//
// Positions are byte-native. The lexer records a byte offset and rune-counted
// line and column for every token, which is exactly [location.Position]'s
// currency, so spans are constructed directly with no offset conversion layer.
// Span ends are exclusive.
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
//     falls through to division — the same reading the LSP's brace scanner and
//     the TextMate grammar encode.
//   - FLOAT, word-boundary-anchored, comes before INVALID_NUMBER, which comes
//     before INTEGER. "2.5e10" must lex as one FLOAT, which first-match
//     ordering alone cannot express: INVALID_NUMBER's optional-exponent path
//     would otherwise reinterpret "e10" as trailing identifier junk. The
//     trailing \b makes FLOAT decline exactly the inputs where junk follows, so
//     "1.5x" and "1.5e5x" fall through to INVALID_NUMBER while every valid
//     float wins. INTEGER needs no boundary: a digit run with trailing junk was
//     already taken by INVALID_NUMBER.
//   - Every multi-character operator comes before its single-character prefix:
//     -->, *->, ->, >=, <=, ==, =~, !=, !~, &&, ||, @@.
//   - VARIABLE comes before DOLLAR, so a bare '$' stays a token of its own.
//   - ANY_OTHER is last: one rune, unconditional. It keeps the lexer total, so
//     every rejection happens in the parser with a position attached and no
//     input can fail to lex.
//
// The table contains no keywords. Every keyword lexes as LC_WORD or UC_WORD
// and reserved-word behavior is a parser decision, expressed as negative
// lookahead groups in the grammar's struct tags and as the reservedLC and
// datatypeKeywords maps. The struct tags are compile-time strings, so those
// groups are written out in place rather than composed from the maps;
// [TestTags_LookaheadGroupsAreAtTheirCanonicalSites] holds every copy to its
// map and every site to its set.
//
// The string-escape set matches the language's STRING rule exactly: an invalid
// escape makes the whole literal fail to lex, falling through to ANY_OTHER at
// the opening quote.
//
// # Expression precedence
//
// Invariant expressions are parsed by a Pratt parser whose binding powers are
// the language's own law. Three of them contradict what a C-family reading
// predicts, and all three are observable:
//
//   - Unary minus binds tighter than indexing, pipeline calls and property
//     access; '!' binds looser than all three. So negating an indexed value
//     parses as "index the negated operand", while negating with '!' parses as
//     "negate the indexed operand".
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
