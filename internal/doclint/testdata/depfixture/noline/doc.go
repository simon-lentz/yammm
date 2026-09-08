// Package noline states its dependency claim in PROSE under the heading the
// gate reads, so the claim is published and nothing checks it.
//
// The package with no block at all is leaf, which carries no doc.go: it is not
// counted and not reported, because it states nothing. This one states
// something the gate cannot read, which is the case the gate must refuse.
//
// # Dependencies
//
// This package imports nothing outside the standard library.
package noline
