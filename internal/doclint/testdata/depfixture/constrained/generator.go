//go:build ignore

// A generator no host builds into the package; its import is not the package's.
package main

import _ "depfixture/absent"

func main() {}
