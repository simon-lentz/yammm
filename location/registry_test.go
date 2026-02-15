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

// C3: RuneOffsetConverter interface compliance tests

// mockRuneConverter is a mock implementation of RuneOffsetConverter for testing.
type mockRuneConverter struct {
	offsets map[SourceID]map[int]int // source -> runeOffset -> byteOffset
}

func newMockRuneConverter() *mockRuneConverter {
	return &mockRuneConverter{
		offsets: make(map[SourceID]map[int]int),
	}
}

func (m *mockRuneConverter) register(source SourceID, runeOffset, byteOffset int) {
	if m.offsets[source] == nil {
		m.offsets[source] = make(map[int]int)
	}
	m.offsets[source][runeOffset] = byteOffset
}

func (m *mockRuneConverter) RuneToByteOffset(source SourceID, runeOffset int) (byteOffset int, ok bool) {
	if runeOffset < 0 {
		return 0, false
	}
	sourceOffsets, exists := m.offsets[source]
	if !exists {
		return 0, false
	}
	byteOff, exists := sourceOffsets[runeOffset]
	if !exists {
		return 0, false
	}
	return byteOff, true
}

func TestRuneOffsetConverter_Interface(t *testing.T) {
	// Verify that mockRuneConverter implements RuneOffsetConverter
	var _ RuneOffsetConverter = (*mockRuneConverter)(nil)
}

func TestMockRuneConverter_Basic(t *testing.T) {
	source := NewSourceID("test://unit")
	conv := newMockRuneConverter()

	// Before registration, should return (0, false)
	byteOff, ok := conv.RuneToByteOffset(source, 0)
	if ok {
		t.Error("unregistered source should return ok=false")
	}
	if byteOff != 0 {
		t.Errorf("unregistered source should return byteOffset=0, got %d", byteOff)
	}

	// Register conversions (simulating ASCII content "hello")
	// rune 0 -> byte 0, rune 1 -> byte 1, etc.
	for i := range 5 {
		conv.register(source, i, i)
	}

	// Should now return correct byte offsets
	byteOff, ok = conv.RuneToByteOffset(source, 2)
	if !ok {
		t.Error("registered offset should return ok=true")
	}
	if byteOff != 2 {
		t.Errorf("RuneToByteOffset(2) = %d; want 2", byteOff)
	}

	// Different offset (not registered) should return (0, false)
	_, ok = conv.RuneToByteOffset(source, 100)
	if ok {
		t.Error("unregistered offset should return ok=false")
	}

	// Negative offset should return (0, false)
	_, ok = conv.RuneToByteOffset(source, -1)
	if ok {
		t.Error("negative offset should return ok=false")
	}
}

func TestMockRuneConverter_UTF8Simulation(t *testing.T) {
	// Simulate UTF-8 content: "hëllo" (ë is 2 bytes in UTF-8)
	// Rune indices: h(0), ë(1), l(2), l(3), o(4)
	// Byte offsets: h(0), ë(1-2), l(3), l(4), o(5)
	source := NewSourceID("test://utf8")
	conv := newMockRuneConverter()

	conv.register(source, 0, 0) // h
	conv.register(source, 1, 1) // ë starts at byte 1
	conv.register(source, 2, 3) // l (after 2-byte ë)
	conv.register(source, 3, 4) // l
	conv.register(source, 4, 5) // o

	// Test the 2-byte character offset
	byteOff, ok := conv.RuneToByteOffset(source, 2)
	if !ok {
		t.Error("registered offset should return ok=true")
	}
	if byteOff != 3 {
		t.Errorf("RuneToByteOffset(2) = %d; want 3 (after 2-byte ë)", byteOff)
	}
}
