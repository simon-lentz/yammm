// Package constraintof finds the schema constraint a writer renders a value
// through: a declared property's, and a collection element's.
//
// The JSON writer renders each scalar in the form its constraint stores, through
// [github.com/simon-lentz/yammm/instance.CanonicalValue], and the Neo4j writer
// coerces a property's value and a collection's elements by their constraint.
// Both reach a property's and an element's constraint through this package, so
// they cannot disagree about which constraint either has. A key component's and
// an edge property's are read at each writer's own site.
//
// This is an internal package; its API may change without notice.
package constraintof
