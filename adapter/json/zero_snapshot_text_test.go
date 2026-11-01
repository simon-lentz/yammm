package json

import "testing"

func TestErrZeroSnapshot_NamesTheAdapterAndTheCondition(t *testing.T) {
	t.Parallel()
	const want = "json adapter: zero graph snapshot"
	if got := ErrZeroSnapshot.Error(); got != want {
		t.Errorf("ErrZeroSnapshot = %q, want %q", got, want)
	}
}
