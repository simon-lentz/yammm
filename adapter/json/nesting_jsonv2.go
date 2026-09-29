//go:build goexperiment.jsonv2

package json

// nestingFromRoot is true where encoding/json runs on its v2 implementation,
// the default from Go 1.27, which counts the nesting limit from the root.
const nestingFromRoot = true
