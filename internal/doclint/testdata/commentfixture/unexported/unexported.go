// Package unexported publishes nothing, so nothing it says reaches go doc.
package unexported

// hidden is documented by a block a blank line separates from it.

func hidden() {}
