// Package constraintof finds the schema constraint a writer renders a value
// through: a declared property's, and a collection element's.
//
// The data adapters' writers render each scalar in the form its constraint
// stores, through [github.com/simon-lentz/yammm/instance.CanonicalValue], and
// both reach the constraint the same way. This package holds that one way, so
// the JSON and CSV writers cannot disagree about which constraint a property or
// a collection's element has. A key component's and an edge property's are
// read from the relation at each writer's own site.
//
// This is an internal package; its API may change without notice.
package constraintof
