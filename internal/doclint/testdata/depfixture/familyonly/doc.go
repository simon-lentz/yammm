// Package familyonly documents other directories' edges, not its own.
//
// One row is TRUE and one is FALSE, so a gate that stopped reading family
// tables would go green here and a gate that reported every row would go red
// on both. Only a gate reading each row against the package its subject names
// reports exactly one.
//
// # Dependencies
//
//	correct  ──imports──▶  leaf
//	extra    ──imports──▶  nothing-of-the-sort
package familyonly
