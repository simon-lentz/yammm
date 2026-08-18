// Package rotten is a fixture carrying one dangling link of each shape.
package rotten

// Gadget is the only type this fixture declares.
type Gadget struct{}

// Assemble names four things that do not exist.
//
// [Removed] is a package-level name, [Gadget.Vanished] is a member,
// [doclintfixture/clean.Absent] is qualified, and [TestGadget_Deleted] is a
// regression anchor whose test is gone. This file imports no json package, so
// [json.Absent] reaches the fixture json package through the unique-name
// fallback and is reported too.
func Assemble() Gadget { return Gadget{} }
