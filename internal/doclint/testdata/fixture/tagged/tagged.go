// Package tagged is a fixture whose one link names a symbol declared only
// under a build tag.
package tagged

// Reach names [TagOnly], which the default build does not declare, so the
// link dangles for every reader of the published documentation.
func Reach() {}
