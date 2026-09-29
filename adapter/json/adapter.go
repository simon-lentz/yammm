package json

// Adapter parses JSON data into RawInstance values and serializes snapshots
// to JSON. It holds no state, so one value serves any number of concurrent
// calls.
type Adapter struct{}

// New returns a JSON adapter.
func New() *Adapter {
	return &Adapter{}
}
