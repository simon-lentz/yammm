package value

// Resolved is a foreign-key component whose reader judged the address it
// names: the validator stores Value as the target key's component and judges
// it no further. The snapshot reader writes one at every edge it revalidates,
// whose key is judged at its address, and at a target_missing record whose
// target a refused unit stands at or states, whose refusal stands for it.
type Resolved struct {
	Value any
}
