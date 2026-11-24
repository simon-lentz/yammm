//go:build !goexperiment.jsonv2

package jsondata

// nestingFromRoot is false where encoding/json runs its own implementation,
// which counts the nesting limit within each value Decode reads.
const nestingFromRoot = false
