// Package typetag provides type tag validation for the data adapters' parsers:
// the top-level keys of a JSON document and the type column of a CSV file.
//
// Type tags follow DSL grammar rules: a type name starts with an uppercase
// ASCII letter, and a qualified tag writes its alias before a dot (for example
// "alias.TypeName"). Neither half may spell a reserved word. The alias refuses
// every spelling the grammar refuses where either case is admitted, as an
// import alias does; the type name refuses the built-in datatype names. This
// package validates type tag syntax before semantic resolution occurs in the
// validator.
//
// This is an internal package; its API may change without notice.
package typetag
