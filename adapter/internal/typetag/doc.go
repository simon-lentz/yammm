// Package typetag provides type tag validation for the data adapters' parsers:
// the top-level keys of a JSON document and the type column of a CSV file.
//
// Type tags follow DSL grammar rules: names must start with an uppercase letter,
// aliases use dot notation (e.g., "alias.TypeName"). This package validates
// type tag syntax before semantic resolution occurs in the validator.
//
// This is an internal package; its API may change without notice.
package typetag
