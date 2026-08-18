// Package mdsnippet extracts fenced yammm code blocks from Markdown prose and
// gates them by fence tag.
//
// Prose rots differently from code: a sentence describing the loader stays
// readable and convincing long after the loader stopped behaving that way, and
// nothing compiles it. This package turns a document's own examples into the
// test of the claims around them.
//
// # Fence vocabulary
//
// The fence tag declares what genre a block belongs to, and the genre decides
// which gate applies. One vocabulary covers every Markdown corpus in the repo,
// so a tag cannot come to mean one thing in the specification and another in
// the plugin references.
//
//   - [TagYammm] and [TagSnippet] mark illustrative but real code. It must
//     PARSE. Most such blocks are fragments — a property line whose type is
//     declared three sections earlier, a type body with no primary key because
//     keys are not the subject — so parsing is the strongest blanket claim
//     available; see [Block.WrapForParsing].
//
//   - [TagSchema] marks a complete, loadable model. It must parse AND load
//     clean. Use it where an example's SEMANTICS are the point and a
//     plausible-looking mistake would survive parsing: an unknown @vector
//     keyword, a @@index naming a property that is not there.
//
//   - [TagInvalid] marks a deliberately wrong example — the WRONG half of a
//     WRONG/RIGHT pair. It must FAIL to load, whether by syntax error or by
//     semantic one. This is the gate with no counterpart in a conventional
//     test suite, and the one that catches the worst failure mode in a
//     documentation corpus: an example labelled WRONG that a loosened loader
//     has quietly made valid, leaving the document teaching a false lesson
//     with authority.
//
//   - Any other tag (text, go, bash) is not extracted. Grammar metasyntax —
//     "--> REL_NAME (multiplicity) TargetType" — belongs under text, because
//     placeholder words are not yammm code and gating them is a category
//     error.
//
// Independently of the tag, a block naming a diagnostic code in a comment is
// asserted to PRODUCE that code (see [Block.DiagnosticCode]). That converts a
// claim the surrounding prose makes about the loader into a test of the loader.
//
// The code must be one the LOADER can report. An instance-validation code —
// E_INVARIANT_FAIL, E_EVAL_ERROR, E_CONSTRAINT_FAIL — describes what
// happens to data long after the schema compiled, so asserting it against a
// load produces a failure that is about the harness rather than the example.
// Name a runtime code in the prose around the block, not in a comment inside
// it. The distinction is worth keeping: a mistake the loader cannot catch is
// exactly the kind worth documenting, and saying so is more useful to a reader
// than implying the compiler has their back.
//
// # Wrapping
//
// A fragment is not a schema, so gating one means supplying the context the
// document left implicit: a schema declaration, a type shell, a primary key.
// [Block.WrapForParsing] and [Block.WrapForLoading] do that, hoisting any
// import lines to schema level so an "import plus relation line" fragment —
// legal prose, two different scopes — still resolves.
package mdsnippet
