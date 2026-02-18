package source

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

func TestNewRegistry(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	if reg == nil {
		t.Fatal("NewRegistry() returned nil")
	}
	if reg.Len() != 0 {
		t.Errorf("NewRegistry().Len() = %d; want 0", reg.Len())
	}
}

func TestRegistry_Register_And_ContentBySource(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://test.yammm")
	content := []byte("type Person {\n  name: string\n}\n")

	// Register content
	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Retrieve content
	got, ok := reg.ContentBySource(sourceID)
	if !ok {
		t.Fatal("ContentBySource() returned false for registered source")
	}
	if string(got) != string(content) {
		t.Errorf("ContentBySource() = %q; want %q", got, content)
	}
}

func TestRegistry_ContentBySource_UnknownSource(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://unknown.yammm")

	_, ok := reg.ContentBySource(sourceID)
	if ok {
		t.Error("ContentBySource() returned true for unknown source")
	}
}

func TestRegistry_Content_Span(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://test.yammm")
	content := []byte("type Person {}")

	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Content via Span (adapter method)
	span := location.Point(sourceID, 1, 1)
	got, ok := reg.Content(span)
	if !ok {
		t.Fatal("Content(span) returned false for registered source")
	}
	if string(got) != string(content) {
		t.Errorf("Content(span) = %q; want %q", got, content)
	}
}

func TestRegistry_Register_IdempotentSameContent(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://test.yammm")
	content := []byte("type Person {}")

	// First registration
	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("First Register() error: %v", err)
	}

	// Second registration with same content should succeed
	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Second Register() with same content error: %v", err)
	}

	// Verify only one entry
	if reg.Len() != 1 {
		t.Errorf("Len() = %d; want 1 after idempotent registration", reg.Len())
	}
}

func TestRegistry_Register_CollisionDifferentContent(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://test.yammm")
	content1 := []byte("type Person {}")
	content2 := []byte("type Vehicle {}")

	// First registration
	if err := reg.Register(sourceID, content1); err != nil {
		t.Fatalf("First Register() error: %v", err)
	}

	// Second registration with different content should fail
	err := reg.Register(sourceID, content2)
	if err == nil {
		t.Fatal("Register() with different content should return error")
	}

	var collisionErr *KeyCollisionError
	if !errors.As(err, &collisionErr) {
		t.Errorf("Register() error = %T; want *KeyCollisionError", err)
	}
	if collisionErr.SourceID != sourceID {
		t.Errorf("KeyCollisionError.SourceID = %v; want %v", collisionErr.SourceID, sourceID)
	}
}

func TestRegistry_Register_DefensiveCopy(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://test.yammm")
	content := []byte("original content")

	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Mutate the original slice
	content[0] = 'X'

	// Registry should have the original content (defensive copy)
	got, ok := reg.ContentBySource(sourceID)
	if !ok {
		t.Fatal("ContentBySource() returned false")
	}
	if got[0] == 'X' {
		t.Error("Registry did not make defensive copy; mutation propagated")
	}
	if string(got) != "original content" {
		t.Errorf("ContentBySource() = %q; want %q", got, "original content")
	}
}

func TestRegistry_ContentBySource_DefensiveCopy(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://test.yammm")
	content := []byte("original content")

	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Get content and mutate the returned slice
	got1, ok := reg.ContentBySource(sourceID)
	if !ok {
		t.Fatal("ContentBySource() returned false")
	}
	got1[0] = 'X' // Mutate the returned slice

	// Get content again - should be unaffected by the mutation
	got2, ok := reg.ContentBySource(sourceID)
	if !ok {
		t.Fatal("ContentBySource() returned false on second call")
	}
	if got2[0] == 'X' {
		t.Error("ContentBySource() did not return defensive copy; mutation propagated to registry")
	}
	if string(got2) != "original content" {
		t.Errorf("ContentBySource() = %q; want %q", got2, "original content")
	}
}

func TestRegistry_PositionAt_SimpleASCII(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://test.yammm")
	// Line 1: "hello\n" (bytes 0-5, newline at 5)
	// Line 2: "world\n" (bytes 6-11, newline at 11)
	// Line 3: "!\n" (bytes 12-13, newline at 13)
	content := []byte("hello\nworld\n!\n")

	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	tests := []struct {
		name       string
		byteOffset int
		wantLine   int
		wantColumn int
		wantByte   int
		wantZero   bool
	}{
		{"start of file", 0, 1, 1, 0, false},
		{"middle of line 1", 2, 1, 3, 2, false},
		{"end of line 1 content", 4, 1, 5, 4, false},
		{"newline of line 1", 5, 1, 6, 5, false},
		{"start of line 2", 6, 2, 1, 6, false},
		{"middle of line 2", 8, 2, 3, 8, false},
		{"start of line 3", 12, 3, 1, 12, false},
		{"EOF position", 14, 4, 1, 14, false},
		{"negative offset", -1, 0, 0, 0, true},
		{"beyond EOF", 15, 0, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pos := reg.PositionAt(sourceID, tt.byteOffset)
			if pos.IsZero() != tt.wantZero {
				t.Errorf("PositionAt(%d).IsZero() = %v; want %v", tt.byteOffset, pos.IsZero(), tt.wantZero)
				return
			}
			if tt.wantZero {
				return
			}
			if pos.Line != tt.wantLine {
				t.Errorf("PositionAt(%d).Line = %d; want %d", tt.byteOffset, pos.Line, tt.wantLine)
			}
			if pos.Column != tt.wantColumn {
				t.Errorf("PositionAt(%d).Column = %d; want %d", tt.byteOffset, pos.Column, tt.wantColumn)
			}
			if pos.Byte != tt.wantByte {
				t.Errorf("PositionAt(%d).Byte = %d; want %d", tt.byteOffset, pos.Byte, tt.wantByte)
			}
		})
	}
}

func TestRegistry_PositionAt_CRLF(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://crlf.yammm")
	// Line 1: "ab\r\n" (bytes 0-3, CRLF at 2-3)
	// Line 2: "cd\r\n" (bytes 4-7, CRLF at 6-7)
	// Line 3: "e" (bytes 8, no newline)
	content := []byte("ab\r\ncd\r\ne")

	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	tests := []struct {
		name       string
		byteOffset int
		wantLine   int
		wantColumn int
	}{
		{"start of file", 0, 1, 1},
		{"after 'a'", 1, 1, 2},
		{"on CR of line 1", 2, 1, 3},
		{"on LF of line 1", 3, 1, 4},
		{"start of line 2", 4, 2, 1},
		{"on CR of line 2", 6, 2, 3},
		{"start of line 3", 8, 3, 1},
		{"EOF", 9, 3, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pos := reg.PositionAt(sourceID, tt.byteOffset)
			if pos.IsZero() {
				t.Fatalf("PositionAt(%d) returned zero Position", tt.byteOffset)
			}
			if pos.Line != tt.wantLine {
				t.Errorf("PositionAt(%d).Line = %d; want %d", tt.byteOffset, pos.Line, tt.wantLine)
			}
			if pos.Column != tt.wantColumn {
				t.Errorf("PositionAt(%d).Column = %d; want %d", tt.byteOffset, pos.Column, tt.wantColumn)
			}
		})
	}
}

func TestRegistry_PositionAt_UTF8(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://utf8.yammm")
	// "café" = c(1) a(1) f(1) é(2 bytes) = 5 bytes total
	// Line 1: "café\n" (bytes 0-5)
	// Line 2: "日本語" = 9 bytes (3 chars × 3 bytes each)
	content := []byte("café\n日本語")

	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	tests := []struct {
		name       string
		byteOffset int
		wantLine   int
		wantColumn int
	}{
		{"start", 0, 1, 1},
		{"after 'c'", 1, 1, 2},
		{"after 'a'", 2, 1, 3},
		{"after 'f'", 3, 1, 4},
		{"at newline", 5, 1, 5},           // byte 5 is newline character (5th char on line)
		{"start of line 2", 6, 2, 1},      // after newline
		{"after '日' (3 bytes)", 9, 2, 2},  // bytes 6-8 are first char
		{"after '本' (3 bytes)", 12, 2, 3}, // bytes 9-11 are second char
		{"EOF", 15, 2, 4},                 // after all 3 kanji
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pos := reg.PositionAt(sourceID, tt.byteOffset)
			if pos.IsZero() {
				t.Fatalf("PositionAt(%d) returned zero Position", tt.byteOffset)
			}
			if pos.Line != tt.wantLine {
				t.Errorf("PositionAt(%d).Line = %d; want %d", tt.byteOffset, pos.Line, tt.wantLine)
			}
			if pos.Column != tt.wantColumn {
				t.Errorf("PositionAt(%d).Column = %d; want %d", tt.byteOffset, pos.Column, tt.wantColumn)
			}
		})
	}
}

func TestRegistry_PositionAt_UnknownSource(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://unknown.yammm")

	pos := reg.PositionAt(sourceID, 0)
	if !pos.IsZero() {
		t.Error("PositionAt() returned non-zero Position for unknown source")
	}
}

func TestRegistry_LineStartByte(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://test.yammm")
	// Line 1: "hello\n" (starts at 0)
	// Line 2: "world\n" (starts at 6)
	// Line 3: "!" (starts at 12)
	content := []byte("hello\nworld\n!")

	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	tests := []struct {
		line     int
		wantByte int
		wantOK   bool
	}{
		{1, 0, true},
		{2, 6, true},
		{3, 12, true},
		{0, 0, false},  // invalid line
		{4, 0, false},  // beyond last line
		{-1, 0, false}, // negative line
	}

	for _, tt := range tests {
		byteOff, ok := reg.LineStartByte(sourceID, tt.line)
		if ok != tt.wantOK {
			t.Errorf("LineStartByte(line=%d) ok = %v; want %v", tt.line, ok, tt.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if byteOff != tt.wantByte {
			t.Errorf("LineStartByte(line=%d) = %d; want %d", tt.line, byteOff, tt.wantByte)
		}
	}
}

func TestRegistry_LineStartByte_UnknownSource(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://unknown.yammm")

	_, ok := reg.LineStartByte(sourceID, 1)
	if ok {
		t.Error("LineStartByte() returned ok=true for unknown source")
	}
}

func TestRegistry_RuneToByteOffset(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://rune.yammm")
	// "日本語abc" = 12 bytes (3+3+3+1+1+1)
	// Rune indices (0-based): 日=0, 本=1, 語=2, a=3, b=4, c=5
	// Byte offsets:           0,    3,    6,   9,   10,  11
	content := []byte("日本語abc")

	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	tests := []struct {
		runeIndex int
		wantByte  int
		wantOK    bool
	}{
		{0, 0, true},   // 日
		{1, 3, true},   // 本
		{2, 6, true},   // 語
		{3, 9, true},   // a
		{4, 10, true},  // b
		{5, 11, true},  // c
		{6, 12, true},  // EOF
		{7, 0, false},  // beyond
		{-1, 0, false}, // negative
	}

	for _, tt := range tests {
		byteOff, ok := reg.RuneToByteOffset(sourceID, tt.runeIndex)
		if ok != tt.wantOK {
			t.Errorf("RuneToByteOffset(%d) ok = %v; want %v", tt.runeIndex, ok, tt.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if byteOff != tt.wantByte {
			t.Errorf("RuneToByteOffset(%d) = %d; want %d", tt.runeIndex, byteOff, tt.wantByte)
		}
	}
}

func TestRegistry_RuneToByteOffset_UnknownSource(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://unknown.yammm")

	_, ok := reg.RuneToByteOffset(sourceID, 0)
	if ok {
		t.Error("RuneToByteOffset() returned ok=true for unknown source")
	}
}

func TestRegistry_EmptyContent(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://empty.yammm")
	content := []byte{}

	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Content retrieval
	got, ok := reg.ContentBySource(sourceID)
	if !ok {
		t.Fatal("ContentBySource() returned false for registered empty source")
	}
	if len(got) != 0 {
		t.Errorf("ContentBySource() len = %d; want 0", len(got))
	}

	// Position at offset 0 (EOF position for empty content)
	pos := reg.PositionAt(sourceID, 0)
	if pos.IsZero() {
		t.Fatal("PositionAt(0) returned zero Position for empty content")
	}
	if pos.Line != 1 || pos.Column != 1 {
		t.Errorf("PositionAt(0) = (line=%d, col=%d); want (1, 1)", pos.Line, pos.Column)
	}

	// RuneToByteOffset for empty content (EOF only)
	byteOff, ok := reg.RuneToByteOffset(sourceID, 0)
	if !ok {
		t.Fatal("RuneToByteOffset(0) returned false for empty content")
	}
	if byteOff != 0 {
		t.Errorf("RuneToByteOffset(0) = %d; want 0", byteOff)
	}
}

func TestRegistry_SingleLineNoNewline(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://single.yammm")
	content := []byte("hello")

	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Position at various offsets
	tests := []struct {
		byteOffset int
		wantLine   int
		wantColumn int
	}{
		{0, 1, 1},
		{2, 1, 3},
		{5, 1, 6}, // EOF
	}

	for _, tt := range tests {
		pos := reg.PositionAt(sourceID, tt.byteOffset)
		if pos.IsZero() {
			t.Fatalf("PositionAt(%d) returned zero Position", tt.byteOffset)
		}
		if pos.Line != tt.wantLine || pos.Column != tt.wantColumn {
			t.Errorf("PositionAt(%d) = (line=%d, col=%d); want (%d, %d)",
				tt.byteOffset, pos.Line, pos.Column, tt.wantLine, tt.wantColumn)
		}
	}
}

func TestRegistry_Keys(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()

	// Register multiple sources
	sources := []string{"test://c.yammm", "test://a.yammm", "test://b.yammm"}
	for _, s := range sources {
		sourceID := location.MustNewSourceID(s)
		if err := reg.Register(sourceID, []byte("content")); err != nil {
			t.Fatalf("Register() error: %v", err)
		}
	}

	keys := reg.Keys()
	if len(keys) != 3 {
		t.Fatalf("Keys() len = %d; want 3", len(keys))
	}

	// Should be sorted
	want := []string{"test://a.yammm", "test://b.yammm", "test://c.yammm"}
	for i, k := range keys {
		if k.String() != want[i] {
			t.Errorf("Keys()[%d] = %q; want %q", i, k.String(), want[i])
		}
	}

	// Keys should be a defensive copy
	keys[0] = location.MustNewSourceID("test://modified.yammm")
	keysAgain := reg.Keys()
	if keysAgain[0].String() == "test://modified.yammm" {
		t.Error("Keys() did not return defensive copy; modification propagated")
	}
}

func TestRegistry_Has(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://test.yammm")
	otherID := location.MustNewSourceID("test://other.yammm")

	if reg.Has(sourceID) {
		t.Error("Has() returned true before registration")
	}

	if err := reg.Register(sourceID, []byte("content")); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	if !reg.Has(sourceID) {
		t.Error("Has() returned false after registration")
	}
	if reg.Has(otherID) {
		t.Error("Has() returned true for unregistered source")
	}
}

func TestRegistry_Len(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()

	if reg.Len() != 0 {
		t.Errorf("Len() = %d; want 0 for empty registry", reg.Len())
	}

	for i := range 5 {
		sourceID := location.MustNewSourceID("test://source" + string(rune('0'+i)) + ".yammm")
		if err := reg.Register(sourceID, []byte("content")); err != nil {
			t.Fatalf("Register() error: %v", err)
		}
	}

	if reg.Len() != 5 {
		t.Errorf("Len() = %d; want 5", reg.Len())
	}
}

func TestRegistry_Clear(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://test.yammm")

	if err := reg.Register(sourceID, []byte("content")); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	if reg.Len() != 1 {
		t.Errorf("Len() = %d; want 1 before Clear", reg.Len())
	}

	reg.Clear()

	if reg.Len() != 0 {
		t.Errorf("Len() = %d; want 0 after Clear", reg.Len())
	}
	if reg.Has(sourceID) {
		t.Error("Has() returned true after Clear")
	}
	if len(reg.Keys()) != 0 {
		t.Errorf("Keys() len = %d; want 0 after Clear", len(reg.Keys()))
	}

	// Should be able to register again after Clear
	if err := reg.Register(sourceID, []byte("new content")); err != nil {
		t.Fatalf("Register() after Clear error: %v", err)
	}
}

func TestRegistry_Stats(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()

	// Empty registry
	stats := reg.Stats()
	if stats.SourceCount != 0 {
		t.Errorf("Stats().SourceCount = %d; want 0", stats.SourceCount)
	}
	if stats.ContentBytes != 0 {
		t.Errorf("Stats().ContentBytes = %d; want 0", stats.ContentBytes)
	}

	// Register some content
	content1 := []byte("hello\nworld") // 11 bytes, 2 lines, 11 runes
	content2 := []byte("日本語")          // 9 bytes, 1 line, 3 runes

	if err := reg.Register(location.MustNewSourceID("test://1.yammm"), content1); err != nil {
		t.Fatalf("Register() error: %v", err)
	}
	if err := reg.Register(location.MustNewSourceID("test://2.yammm"), content2); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	stats = reg.Stats()
	if stats.SourceCount != 2 {
		t.Errorf("Stats().SourceCount = %d; want 2", stats.SourceCount)
	}
	if stats.ContentBytes != 20 { // 11 + 9
		t.Errorf("Stats().ContentBytes = %d; want 20", stats.ContentBytes)
	}
	if stats.IndexBytes <= 0 {
		t.Errorf("Stats().IndexBytes = %d; want > 0", stats.IndexBytes)
	}
}

// UTF-8 Tests (adapted from v1 provenance_utf8_test.go)

func TestProvenanceUTF8ByteOffsets(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://utf8_test.yammm")

	// Test content with various UTF-8 sequences:
	// - ASCII: 1 byte per char
	// - é (latin): 2 bytes
	// - 日本語 (CJK): 3 bytes each
	// - emoji: 4 bytes
	//
	// Content: "type 日本語 { name: café 🎉 }\n"
	// Byte breakdown:
	// "type " = 5 bytes (0-4)
	// "日" = 3 bytes (5-7)
	// "本" = 3 bytes (8-10)
	// "語" = 3 bytes (11-13)
	// " { name: caf" = 12 bytes (14-25)
	// "é" = 2 bytes (26-27)
	// " " = 1 byte (28)
	// "🎉" = 4 bytes (29-32)
	// " }\n" = 3 bytes (33-35)
	// Total: 36 bytes
	content := []byte("type 日本語 { name: café 🎉 }\n")

	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	tests := []struct {
		name        string
		byteOffset  int
		wantLine    int
		wantColumn  int
		description string
	}{
		{
			name:        "start of file",
			byteOffset:  0,
			wantLine:    1,
			wantColumn:  1,
			description: "position at 't' in 'type'",
		},
		{
			name:        "start of CJK",
			byteOffset:  5,
			wantLine:    1,
			wantColumn:  6,
			description: "position at '日' (after 'type ')",
		},
		{
			name:        "second CJK char",
			byteOffset:  8,
			wantLine:    1,
			wantColumn:  7,
			description: "position at '本' (after 'type 日')",
		},
		{
			name:        "third CJK char",
			byteOffset:  11,
			wantLine:    1,
			wantColumn:  8,
			description: "position at '語' (after 'type 日本')",
		},
		{
			name:        "after CJK before brace",
			byteOffset:  14,
			wantLine:    1,
			wantColumn:  9,
			description: "position at ' ' after '語'",
		},
		{
			name:        "at latin extended char",
			byteOffset:  26,
			wantLine:    1,
			wantColumn:  21,
			description: "position at 'é' in 'café'",
		},
		{
			name:        "at emoji",
			byteOffset:  29,
			wantLine:    1,
			wantColumn:  23,
			description: "position at '🎉'",
		},
		{
			name:        "after emoji",
			byteOffset:  33,
			wantLine:    1,
			wantColumn:  24,
			description: "position at ' ' after emoji",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pos := reg.PositionAt(sourceID, tt.byteOffset)
			if pos.IsZero() {
				t.Fatalf("PositionAt(%d) returned zero Position", tt.byteOffset)
			}
			if pos.Line != tt.wantLine {
				t.Errorf("PositionAt(%d).Line = %d; want %d (%s)",
					tt.byteOffset, pos.Line, tt.wantLine, tt.description)
			}
			if pos.Column != tt.wantColumn {
				t.Errorf("PositionAt(%d).Column = %d; want %d (%s)",
					tt.byteOffset, pos.Column, tt.wantColumn, tt.description)
			}
		})
	}
}

func TestProvenanceUTF8RuneToByteOffset(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://utf8_col_test.yammm")

	// "日本語abc" = 12 bytes (3+3+3+1+1+1)
	// Rune positions (0-based): 日=0, 本=1, 語=2, a=3, b=4, c=5, \n=6
	content := []byte("日本語abc\n")

	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	tests := []struct {
		name      string
		runeIndex int
		wantByte  int
	}{
		{"first CJK", 0, 0},
		{"second CJK", 1, 3},
		{"third CJK", 2, 6},
		{"first ASCII after CJK", 3, 9},
		{"second ASCII", 4, 10},
		{"third ASCII", 5, 11},
		{"at newline", 6, 12},
		{"EOF", 7, 13},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotByte, ok := reg.RuneToByteOffset(sourceID, tt.runeIndex)
			if !ok {
				t.Fatalf("RuneToByteOffset(%d) returned false", tt.runeIndex)
			}
			if gotByte != tt.wantByte {
				t.Errorf("RuneToByteOffset(%d) = %d; want %d",
					tt.runeIndex, gotByte, tt.wantByte)
			}
		})
	}
}

func TestProvenanceUTF8Roundtrip(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://roundtrip_test.yammm")

	// Mix of 1, 2, 3, and 4 byte characters
	// "a" = 1 byte
	// "é" = 2 bytes
	// "中" = 3 bytes
	// "🔥" = 4 bytes
	content := []byte("aé中🔥\n")
	// Byte offsets: a=0, é=1-2, 中=3-5, 🔥=6-9, \n=10
	// Valid char boundaries: 0, 1, 3, 6, 10, 11(EOF)
	validBoundaries := []int{0, 1, 3, 6, 10, 11}

	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	for _, byteOffset := range validBoundaries {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			pos := reg.PositionAt(sourceID, byteOffset)
			if pos.IsZero() {
				t.Fatalf("PositionAt(%d) returned zero Position", byteOffset)
			}

			// Verify the byte offset is echoed back correctly
			if pos.Byte != byteOffset {
				t.Errorf("PositionAt(%d).Byte = %d; want %d",
					byteOffset, pos.Byte, byteOffset)
			}
		})
	}
}

func TestProvenanceUTF8MultiLineWithCJK(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://multiline_cjk.yammm")

	// Byte layout analysis:
	// Line 1: "schema \"日本語\"\n" = 7 + 1 + 9 + 1 + 1 = 19 bytes (0-18)
	// Line 2: "type 人 {\n" = 5 + 3 + 1 + 1 + 1 = 11 bytes (19-29)
	// Line 3: "  名前: String\n" = 2 + 6 + 1 + 1 + 6 + 1 = 17 bytes (30-46)
	// Line 4: "}\n" = 2 bytes (47-48)
	// Total: 49 bytes (indices 0-48)
	content := []byte("schema \"日本語\"\ntype 人 {\n  名前: String\n}\n")

	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	tests := []struct {
		name       string
		byteOffset int
		wantLine   int
		wantColumn int
	}{
		{"start of line 1", 0, 1, 1},
		{"start of CJK in line 1", 8, 1, 9}, // after 'schema "' (8 chars)
		{"start of line 2", 19, 2, 1},       // byte after '\n' at 18
		{"CJK type name", 24, 2, 6},         // '人' after 'type ' (5 chars)
		{"start of line 3", 30, 3, 1},       // byte after '\n' at 29
		{"CJK property name", 32, 3, 3},     // '名' after '  ' (2 chars)
		{"start of line 4", 47, 4, 1},       // byte after '\n' at 46
		{"closing brace", 47, 4, 1},         // '}' is at byte 47
		{"EOF", 49, 5, 1},                   // EOF after final '\n' is treated as new line start
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pos := reg.PositionAt(sourceID, tt.byteOffset)
			if pos.IsZero() {
				t.Fatalf("PositionAt(%d) returned zero Position", tt.byteOffset)
			}
			if pos.Line != tt.wantLine {
				t.Errorf("PositionAt(%d).Line = %d; want %d (%s)",
					tt.byteOffset, pos.Line, tt.wantLine, tt.name)
			}
			if pos.Column != tt.wantColumn {
				t.Errorf("PositionAt(%d).Column = %d; want %d (%s)",
					tt.byteOffset, pos.Column, tt.wantColumn, tt.name)
			}
		})
	}
}

// Thread-Safety Tests

func TestRegistry_ConcurrentRegister(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	const numGoroutines = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for range numGoroutines {
		go func() {
			defer wg.Done()
			sourceID := location.MustNewSourceID("test://concurrent.yammm")
			// All goroutines register the same content
			content := []byte("type Test {}")
			_ = reg.Register(sourceID, content)
		}()
	}

	wg.Wait()

	// Verify content was registered
	sourceID := location.MustNewSourceID("test://concurrent.yammm")
	content, ok := reg.ContentBySource(sourceID)
	if !ok {
		t.Fatal("ContentBySource() returned false after concurrent registration")
	}
	if string(content) != "type Test {}" {
		t.Errorf("ContentBySource() = %q; want %q", content, "type Test {}")
	}
}

func TestRegistry_ConcurrentRead(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://concurrent-read.yammm")
	content := []byte("hello\nworld\n日本語\n")

	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for range numGoroutines {
		go func() {
			defer wg.Done()
			// All these reads should succeed without data races
			_, _ = reg.ContentBySource(sourceID)
			_ = reg.PositionAt(sourceID, 5)
			_, _ = reg.LineStartByte(sourceID, 2)
			_, _ = reg.RuneToByteOffset(sourceID, 3)
			_ = reg.Has(sourceID)
			_ = reg.Len()
			_ = reg.Keys()
		}()
	}

	wg.Wait()
}

func TestRegistry_ConcurrentRegisterAndRead(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	const numSources = 10
	const numReaders = 50

	var wg sync.WaitGroup

	// Writers
	wg.Add(numSources)
	for i := range numSources {
		go func(idx int) {
			defer wg.Done()
			sourceID := location.MustNewSourceID(fmt.Sprintf("test://source%d.yammm", idx))
			content := fmt.Appendf(nil, "content for source %d", idx)
			_ = reg.Register(sourceID, content)
		}(i)
	}

	// Readers
	wg.Add(numReaders)
	for range numReaders {
		go func() {
			defer wg.Done()
			// Read operations on potentially non-existent sources
			for i := range numSources {
				sourceID := location.MustNewSourceID(fmt.Sprintf("test://source%d.yammm", i))
				_, _ = reg.ContentBySource(sourceID)
				_ = reg.PositionAt(sourceID, 0)
				_ = reg.Has(sourceID)
			}
			_ = reg.Len()
			_ = reg.Keys()
		}()
	}

	wg.Wait()
}

// Mid-rune offset tests (floor semantics verification)

func TestRegistry_PositionAt_MidRuneOffset_CJK(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://midrune_cjk.yammm")
	// "日本語" = 9 bytes: 日(0-2) 本(3-5) 語(6-8)
	content := []byte("日本語")

	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	tests := []struct {
		name       string
		byteOffset int
		wantColumn int
	}{
		{"at first rune boundary", 0, 1},
		{"mid first rune byte 1", 1, 1},
		{"mid first rune byte 2", 2, 1},
		{"at second rune boundary", 3, 2},
		{"mid second rune byte 1", 4, 2},
		{"mid second rune byte 2", 5, 2},
		{"at third rune boundary", 6, 3},
		{"mid third rune byte 1", 7, 3},
		{"mid third rune byte 2", 8, 3},
		{"EOF", 9, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pos := reg.PositionAt(sourceID, tt.byteOffset)
			if pos.IsZero() {
				t.Fatalf("PositionAt(%d) returned zero Position", tt.byteOffset)
			}
			if pos.Column != tt.wantColumn {
				t.Errorf("PositionAt(%d).Column = %d; want %d (floor semantics)",
					tt.byteOffset, pos.Column, tt.wantColumn)
			}
		})
	}
}

func TestRegistry_PositionAt_MidRuneOffset_Latin(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://midrune_latin.yammm")
	// "café" = 5 bytes: c(0) a(1) f(2) é(3-4)
	content := []byte("café")

	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	tests := []struct {
		name       string
		byteOffset int
		wantColumn int
	}{
		{"at 'c'", 0, 1},
		{"at 'a'", 1, 2},
		{"at 'f'", 2, 3},
		{"at 'é' (2-byte char)", 3, 4},
		{"mid 'é'", 4, 4}, // floor to column 4
		{"EOF", 5, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pos := reg.PositionAt(sourceID, tt.byteOffset)
			if pos.IsZero() {
				t.Fatalf("PositionAt(%d) returned zero Position", tt.byteOffset)
			}
			if pos.Column != tt.wantColumn {
				t.Errorf("PositionAt(%d).Column = %d; want %d (floor semantics)",
					tt.byteOffset, pos.Column, tt.wantColumn)
			}
		})
	}
}

func TestRegistry_PositionAt_MidRuneOffset_Emoji(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://midrune_emoji.yammm")
	// "a🔥b" = 6 bytes: a(0) 🔥(1-4) b(5)
	content := []byte("a🔥b")

	if err := reg.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	tests := []struct {
		name       string
		byteOffset int
		wantColumn int
	}{
		{"at 'a'", 0, 1},
		{"at emoji (4-byte char)", 1, 2},
		{"mid emoji byte 2", 2, 2},
		{"mid emoji byte 3", 3, 2},
		{"mid emoji byte 4", 4, 2},
		{"at 'b'", 5, 3},
		{"EOF", 6, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pos := reg.PositionAt(sourceID, tt.byteOffset)
			if pos.IsZero() {
				t.Fatalf("PositionAt(%d) returned zero Position", tt.byteOffset)
			}
			if pos.Column != tt.wantColumn {
				t.Errorf("PositionAt(%d).Column = %d; want %d (floor semantics)",
					tt.byteOffset, pos.Column, tt.wantColumn)
			}
		})
	}
}

func TestCountRunesInRange_MidRuneFloor(t *testing.T) {
	t.Parallel()

	// "café日本語" = c(0) a(1) f(2) é(3-4) 日(5-7) 本(8-10) 語(11-13)
	content := []byte("café日本語")

	tests := []struct {
		name  string
		start int
		end   int
		want  int // 1-based column
	}{
		// Rune-aligned boundaries (unchanged behavior)
		{"empty range", 0, 0, 1},
		{"through 'c'", 0, 1, 2},
		{"through 'a'", 0, 2, 3},
		{"through 'f'", 0, 3, 4},
		{"through 'é'", 0, 5, 5},
		{"through '日'", 0, 8, 6},
		{"through '本'", 0, 11, 7},
		{"entire string", 0, 14, 8},

		// Mid-rune offsets (floor semantics)
		{"mid 'é' (byte 4)", 0, 4, 4},   // floor to before 'é'
		{"mid '日' (byte 6)", 0, 6, 5},   // floor to before '日'
		{"mid '日' (byte 7)", 0, 7, 5},   // floor to before '日'
		{"mid '本' (byte 9)", 0, 9, 6},   // floor to before '本'
		{"mid '本' (byte 10)", 0, 10, 6}, // floor to before '本'
		{"mid '語' (byte 12)", 0, 12, 7}, // floor to before '語'
		{"mid '語' (byte 13)", 0, 13, 7}, // floor to before '語'
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := countRunesInRange(content, tt.start, tt.end)
			if got != tt.want {
				t.Errorf("countRunesInRange(%q, %d, %d) = %d; want %d",
					content, tt.start, tt.end, got, tt.want)
			}
		})
	}
}

// Helper function tests

func TestComputeLineOffsets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    []int
	}{
		{"empty", "", []int{0}},
		{"no newline", "hello", []int{0}},
		{"single LF", "a\nb", []int{0, 2}},
		{"multiple LF", "a\nb\nc", []int{0, 2, 4}},
		{"trailing LF", "a\n", []int{0, 2}},
		{"CRLF", "a\r\nb", []int{0, 3}},
		{"multiple CRLF", "a\r\nb\r\nc", []int{0, 3, 6}},
		{"mixed", "a\nb\r\nc", []int{0, 2, 5}},
		{"bare CR", "a\rb", []int{0, 2}},
		{"empty lines LF", "\n\n", []int{0, 1, 2}},
		{"empty lines CRLF", "\r\n\r\n", []int{0, 2, 4}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := computeLineOffsets([]byte(tt.content))
			if len(got) != len(tt.want) {
				t.Fatalf("computeLineOffsets(%q) len = %d; want %d", tt.content, len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("computeLineOffsets(%q)[%d] = %d; want %d", tt.content, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestComputeRuneOffsets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    []int
	}{
		{"empty", "", []int{}},
		{"ASCII", "abc", []int{0, 1, 2}},
		{"CJK", "日本語", []int{0, 3, 6}},
		{"mixed", "a日b本c", []int{0, 1, 4, 5, 8}},
		{"2-byte char", "café", []int{0, 1, 2, 3}}, // é is 2 bytes at index 3
		{"4-byte char", "a🔥b", []int{0, 1, 5}},     // 🔥 is 4 bytes
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := computeRuneOffsets([]byte(tt.content))
			if len(got) != len(tt.want) {
				t.Fatalf("computeRuneOffsets(%q) len = %d; want %d", tt.content, len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("computeRuneOffsets(%q)[%d] = %d; want %d", tt.content, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFindLine(t *testing.T) {
	t.Parallel()

	// Content: "a\nb\nc" (bytes 0,1,2,3,4)
	// Line 1: byte 0-1 (a + newline)
	// Line 2: byte 2-3 (b + newline)
	// Line 3: byte 4 (c)
	lineOffsets := []int{0, 2, 4}

	tests := []struct {
		byteOffset int
		wantLine   int
	}{
		{0, 1}, // start of line 1
		{1, 1}, // newline of line 1
		{2, 2}, // start of line 2
		{3, 2}, // newline of line 2
		{4, 3}, // start of line 3
		{5, 3}, // EOF (one past last char)
	}

	for _, tt := range tests {
		got := findLine(lineOffsets, tt.byteOffset)
		if got != tt.wantLine {
			t.Errorf("findLine(offsets, %d) = %d; want %d", tt.byteOffset, got, tt.wantLine)
		}
	}
}

func TestCountRunesInRange(t *testing.T) {
	t.Parallel()

	content := []byte("café日本語")
	// c(1) a(1) f(1) é(2) 日(3) 本(3) 語(3) = 14 bytes total
	// Runes: c a f é 日 本 語 = 7 runes

	tests := []struct {
		name  string
		start int
		end   int
		want  int // 1-based column
	}{
		{"empty range", 0, 0, 1},
		{"first char", 0, 1, 2},
		{"first 3 ASCII", 0, 3, 4},
		{"through é", 0, 5, 5},
		{"through 日", 0, 8, 6},
		{"entire string", 0, 14, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := countRunesInRange(content, tt.start, tt.end)
			if got != tt.want {
				t.Errorf("countRunesInRange(%q, %d, %d) = %d; want %d",
					content, tt.start, tt.end, got, tt.want)
			}
		})
	}
}

// Benchmarks

func BenchmarkRegistry_Register(b *testing.B) {
	content := []byte("type Person {\n  name: string\n  age: int\n}\n")

	for b.Loop() {
		reg := NewRegistry()
		sourceID := location.MustNewSourceID("test://bench.yammm")
		_ = reg.Register(sourceID, content)
	}
}

func BenchmarkRegistry_PositionAt(b *testing.B) {
	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://bench.yammm")
	content := []byte("type Person {\n  name: string\n  age: int\n}\n")
	_ = reg.Register(sourceID, content)

	b.ResetTimer()
	for b.Loop() {
		_ = reg.PositionAt(sourceID, 25)
	}
}

func BenchmarkRegistry_RuneToByteOffset(b *testing.B) {
	reg := NewRegistry()
	sourceID := location.MustNewSourceID("test://bench.yammm")
	content := []byte("type Person {\n  name: string\n  age: int\n}\n")
	_ = reg.Register(sourceID, content)

	b.ResetTimer()
	for b.Loop() {
		_, _ = reg.RuneToByteOffset(sourceID, 25)
	}
}

func BenchmarkComputeLineOffsets_LargeFile(b *testing.B) {
	// Simulate a large file with 1000 lines
	var content []byte
	for range 1000 {
		content = append(content, []byte("type Person { name: string; age: int }\n")...)
	}

	b.ResetTimer()
	for b.Loop() {
		_ = computeLineOffsets(content)
	}
}

func BenchmarkComputeRuneOffsets_LargeFile(b *testing.B) {
	// Simulate a large file with mixed content
	var content []byte
	for range 1000 {
		content = append(content, []byte("type 人物 { 名前: string; 年齢: int }\n")...)
	}

	b.ResetTimer()
	for b.Loop() {
		_ = computeRuneOffsets(content)
	}
}
