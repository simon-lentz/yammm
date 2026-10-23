// Package constraintof finds the schema constraint a writer renders a value
// through: a declared property's, and a collection element's.
//
// The JSON and CSV writers render each scalar in the form its constraint
// stores, through [github.com/simon-lentz/yammm/instance.CanonicalValue], and
// the Neo4j writer coerces a collection's elements by their constraint. All
// three reach an element's constraint through this package, and the JSON and
// CSV writers a property's too, so they cannot disagree about which constraint
// a property or a collection's element has. A key component's and an edge property's are
// read at each writer's own site.
//
// This is an internal package; its API may change without notice.
package constraintof
