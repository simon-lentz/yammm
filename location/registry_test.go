package location

import "testing"

// mockRegistry is a simple mock implementation of PositionRegistry for testing.
type mockRegistry struct {
	positions map[SourceID]map[int]Position
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{
		positions: make(map[SourceID]map[int]Position),
	}
}

func (m *mockRegistry) register(source SourceID, byteOffset int, pos Position) {
	if m.positions[source] == nil {
		m.positions[source] = make(map[int]Position)
	}
	m.positions[source][byteOffset] = pos
}

func (m *mockRegistry) PositionAt(source SourceID, byteOffset int) Position {
	if byteOffset < 0 {
		return Position{}
	}
	positions, ok := m.positions[source]
	if !ok {
		return Position{}
	}
	pos, ok := positions[byteOffset]
	if !ok {
		return Position{}
	}
	return pos
}

func TestPositionRegistry_Interface(t *testing.T) {
	// Verify that mockRegistry implements PositionRegistry
	var _ PositionRegistry = (*mockRegistry)(nil)
}

func TestMockRegistry_Basic(t *testing.T) {
	source := NewSourceID("test://unit")
	reg := newMockRegistry()

	// Before registration, should return zero Position
	pos := reg.PositionAt(source, 0)
	if !pos.IsZero() {
		t.Error("unregistered source should return zero Position")
	}

	// Register a position
	reg.register(source, 42, Position{Line: 5, Column: 10, Byte: 42})

	// Should now return the registered position
	pos = reg.PositionAt(source, 42)
	if pos.IsZero() {
		t.Error("registered offset should return non-zero Position")
	}
	if pos.Line != 5 || pos.Column != 10 {
		t.Errorf("PositionAt(42) = %v; want {5, 10, 42}", pos)
	}

	// Different offset should return zero
	pos = reg.PositionAt(source, 100)
	if !pos.IsZero() {
		t.Error("unregistered offset should return zero Position")
	}

	// Negative offset should return zero
	pos = reg.PositionAt(source, -1)
	if !pos.IsZero() {
		t.Error("negative offset should return zero Position")
	}
}
