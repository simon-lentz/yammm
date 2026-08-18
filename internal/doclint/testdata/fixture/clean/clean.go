// Package clean is a fixture whose every doc link resolves.
package clean

// Widget is the type the other declarations reference.
type Widget struct {
	// Size is a field a link can name.
	Size int
}

// Spin is a method a link can name.
func (Widget) Spin() {}

// Build returns a Widget.
//
// It names [Widget], [Widget.Size] and [Widget.Spin]; the qualified
// [doclintfixture/rotten.Gadget]; the whole package [doclintfixture/rotten];
// and an out-of-module [fmt.Stringer] this gate does not resolve.
// [TestWidget_Builds] anchors the regression, and helper is a lowercase name
// the parser never linkifies.
func Build() Widget { return Widget{} }

// helper is unexported, so a bracketed [helper] is not a link.
func helper() {}
