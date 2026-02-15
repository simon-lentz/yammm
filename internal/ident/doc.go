// Package ident provides rune-aware identifier tokenization and case conversion
// utilities for the YAMMM library.
//
// # Internal Package
//
// This package is internal to the YAMMM module and is not importable by
// external consumers per Go's internal/ package semantics. It is used by the
// schema layer for relation name normalization and by code generation for
// deriving Go export names.
//
// # lower_snake Algorithm
//
// The [ToLowerSnake] function implements the canonical lower_snake algorithm
// for relation name normalization (schema relation names to JSON field names).
//
// Common transformations:
//
//	WORKS_AT   -> works_at
//	HTTPProxy  -> http_proxy
//	CreatedBy  -> created_by
//	UserID     -> user_id
//
// # CamelCase Conversion
//
// The [Capitalize], [ToUpperCamel], and [ToLowerCamel] functions provide
// rune-aware CamelCase conversion with acronym preservation:
//
//	http_server -> HttpServer  (Capitalize/ToUpperCamel)
//	http_server -> httpServer  (ToLowerCamel)
//	HTTPServer  -> HTTPServer  (Capitalize preserves acronyms)
//
// # Thread Safety
//
// All functions in this package are stateless and safe for concurrent use.
// No global state is maintained.
//
// # Stdlib-Only Dependencies
//
// This package depends only on stdlib. It has no dependencies on other packages
// and can be imported by any layer.
package ident
