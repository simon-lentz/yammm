// Package alias provides import alias validation utilities for the schema layer.
//
// This package validates import aliases against grammar keywords and provides
// utilities for deriving default aliases from import paths.
//
// # Reserved Keywords
//
// The grammar defines certain tokens as keywords that cannot be used as import
// aliases because the lexer tokenizes them as literal tokens rather than
// identifiers. These include:
//
//   - DSL keywords: schema, import, as, type, datatype, required, primary,
//     extends, includes, abstract, part, one, many, in
//   - Datatype keywords: Integer, Float, Boolean, String, Enum, Pattern,
//     Timestamp, Date, UUID, Vector
//   - Boolean literals: true, false
//
// # Grammar Synchronization
//
// The reserved keyword list must stay synchronized with the grammar file
// (internal/grammar/YammmGrammar.g4). The Grammar-Alias Synchronization Test
// in alias_test.go parses the grammar and verifies consistency.
package alias
