package source

import (
	"bytes"
	"cmp"
	"fmt"
	"slices"
	"sync"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/location"
)

// sourceEntry holds the content and precomputed indices for a source.
type sourceEntry struct {
	content []byte
	// lineOffsets[i] is the byte offset of the start of line i+1.
	// lineOffsets[0] is always 0 (start of line 1).
	// len(lineOffsets) is the total number of lines.
	lineOffsets []int
	// runeOffsets[i] is the byte offset of the i-th rune.
	// Used for O(1) rune-to-byte conversion (ANTLR token positions).
	runeOffsets []int
}

// Registry provides source content storage and position conversion.
//
// Registry is thread-safe for concurrent access. It implements:
//   - [location.PositionRegistry] for byte offset → Position conversion
//   - [diag.SourceProvider] for source content lookup (via Content)
//   - [diag.LineIndexProvider] for line start byte lookup
type Registry struct {
	mu      sync.RWMutex
	entries map[location.SourceID]*sourceEntry
}

// RegistryStats contains memory usage statistics for a source registry.
type RegistryStats struct {
	SourceCount  int   // Number of registered sources
	ContentBytes int64 // Total bytes of stored content (sum of all source sizes)
	IndexBytes   int64 // Approximate bytes used by line/rune offset indices
}

// KeyCollisionError indicates that a registration was attempted with a SourceID
// that already exists but with different content.
type KeyCollisionError struct {
	SourceID location.SourceID
}

// Error implements the error interface.
func (e *KeyCollisionError) Error() string {
	return fmt.Sprintf("source key collision: different content registered for %q", e.SourceID.String())
}

// NewRegistry creates a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		entries: make(map[location.SourceID]*sourceEntry),
	}
}

// Register stores content under the given sourceID.
//
// Register is thread-safe. Expensive work (computing line and rune offsets)
// is performed before acquiring the lock,
//
// The content is defensively cloned; callers may freely mutate or discard
// the original slice after Register returns.
//
// Registration with an existing sourceID and identical content is idempotent
// (succeeds). Registration with an existing sourceID and different content
// returns [*KeyCollisionError].
func (r *Registry) Register(sourceID location.SourceID, content []byte) error {
	// Defensive clone and precompute indices BEFORE acquiring lock.
	// This minimizes time holding the write lock
	cloned := slices.Clone(content)
	lineOffsets := computeLineOffsets(cloned)
	runeOffsets := computeRuneOffsets(cloned)

	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.entries[sourceID]; ok {
		// Check if content matches (idempotent registration)
		if bytes.Equal(existing.content, cloned) {
			return nil
		}
		return &KeyCollisionError{SourceID: sourceID}
	}

	r.entries[sourceID] = &sourceEntry{
		content:     cloned,
		lineOffsets: lineOffsets,
		runeOffsets: runeOffsets,
	}

	return nil
}

// ContentBySource returns the full content for a source.
//
// This is the primary content lookup method. Returns nil, false if the
// sourceID is not registered.
//
// The returned slice is a defensive copy. Callers may safely modify it
// without affecting the registry.
func (r *Registry) ContentBySource(sourceID location.SourceID) ([]byte, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.entries[sourceID]
	if !ok {
		return nil, false
	}

	return slices.Clone(entry.content), true
}

// Content returns raw bytes for a source identified by the span's Source field.
//
// This method implements [diag.SourceProvider]. It extracts span.Source and
// delegates to [ContentBySource].
//
// The returned slice is a defensive copy. Callers may safely modify it
// without affecting the registry.
func (r *Registry) Content(span location.Span) ([]byte, bool) {
	return r.ContentBySource(span.Source)
}

// PositionAt converts a byte offset in the specified source to a Position.
//
// This method implements [location.PositionRegistry].
//
// Returns a zero Position (check with [location.Position.IsZero]) if:
//   - The source is not registered
//   - The byte offset is negative
//   - The byte offset exceeds the content length
//
// byteOffset == len(content) is valid and returns an EOF position.
//
// Column computation uses O(log n) binary search over precomputed rune offsets
// rather than O(line length) rune scanning, making this efficient for large files.
func (r *Registry) PositionAt(source location.SourceID, byteOffset int) location.Position {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.entries[source]
	if !ok {
		return location.UnknownPosition()
	}

	// Validate byte offset range
	if byteOffset < 0 || byteOffset > len(entry.content) {
		return location.UnknownPosition()
	}

	// Find the line containing this byte offset using binary search
	line := findLine(entry.lineOffsets, byteOffset)
	lineStart := entry.lineOffsets[line-1] // line is 1-based, lineOffsets is 0-indexed

	// Compute column using O(log n) binary search over precomputed rune offsets
	column := columnFromByteOffset(entry.runeOffsets, lineStart, byteOffset, len(entry.content))

	return location.NewPosition(line, column, byteOffset)
}

// LineStartByte returns the byte offset of the start of the given line.
//
// This method implements [diag.LineIndexProvider] for LSP UTF-16 offset
// computation.
//
// Lines are 1-based. Returns (0, false) if:
//   - The source is not registered
//   - The line number is less than 1
//   - The line number exceeds the number of lines in the source
func (r *Registry) LineStartByte(source location.SourceID, line int) (int, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.entries[source]
	if !ok {
		return 0, false
	}

	// Validate line range (1-based)
	if line < 1 || line > len(entry.lineOffsets) {
		return 0, false
	}

	return entry.lineOffsets[line-1], true
}

// RuneToByteOffset converts a rune index (0-based) to a byte offset.
//
// This method enables O(1) conversion from ANTLR token positions (which are
// rune-based) to byte offsets needed for [location.Position].
//
// Returns (0, false) if:
//   - The source is not registered
//   - The rune index is negative
//   - The rune index exceeds the number of runes in the source
//
// runeIndex == len(runeOffsets) returns (len(content), true) for EOF.
func (r *Registry) RuneToByteOffset(source location.SourceID, runeIndex int) (int, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.entries[source]
	if !ok {
		return 0, false
	}

	// Validate rune index range
	if runeIndex < 0 {
		return 0, false
	}

	// EOF position: runeIndex == number of runes
	if runeIndex == len(entry.runeOffsets) {
		return len(entry.content), true
	}

	if runeIndex > len(entry.runeOffsets) {
		return 0, false
	}

	return entry.runeOffsets[runeIndex], true
}

// Keys returns all registered source identifiers in sorted order.
//
// The returned slice is a defensive copy; callers may modify it freely.
// Sources are sorted by their String() representation.
func (r *Registry) Keys() []location.SourceID {
	r.mu.RLock()
	// Copy keys while holding lock
	keys := make([]location.SourceID, 0, len(r.entries))
	for k := range r.entries {
		keys = append(keys, k)
	}
	r.mu.RUnlock()

	// Sort outside lock to minimize lock hold time
	slices.SortFunc(keys, func(a, b location.SourceID) int {
		return cmp.Compare(a.String(), b.String())
	})

	return keys
}

// Has reports whether the given sourceID is registered.
func (r *Registry) Has(sourceID location.SourceID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.entries[sourceID]
	return ok
}

// Len returns the number of registered sources.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.entries)
}

// Clear removes all registered sources, resetting the registry to its
// initial state.
//
// Clear acquires an exclusive write lock and blocks all readers/writers
// during execution.
//
// After Clear returns:
//   - Len() returns 0
//   - Has(id) returns false for all sources
//   - Keys() returns an empty slice
//
// Note: Previously-obtained []byte slices from Content/ContentBySource remain
// valid since they are defensive copies, but re-fetching will fail.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Clear by creating a new empty map (allows GC of old entries)
	r.entries = make(map[location.SourceID]*sourceEntry)
}

// Stats returns memory usage statistics for the registry.
func (r *Registry) Stats() RegistryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var stats RegistryStats
	stats.SourceCount = len(r.entries)

	for _, entry := range r.entries {
		stats.ContentBytes += int64(len(entry.content))
		// Each int in lineOffsets and runeOffsets is 8 bytes on 64-bit systems
		stats.IndexBytes += int64(len(entry.lineOffsets) * 8)
		stats.IndexBytes += int64(len(entry.runeOffsets) * 8)
	}

	return stats
}

// computeLineOffsets precomputes the byte offset of each line start.
// lineOffsets[i] is the byte offset where line i+1 begins (0-indexed array, 1-based lines).
// Handles \r\n as a single line break.
func computeLineOffsets(content []byte) []int {
	// Always have at least one line starting at offset 0
	offsets := []int{0}

	for i := 0; i < len(content); i++ {
		switch content[i] {
		case '\n':
			// Line break found; next line starts at i+1
			offsets = append(offsets, i+1)
		case '\r':
			// Check for \r\n (CRLF)
			if i+1 < len(content) && content[i+1] == '\n' {
				// CRLF: treat as single line break, next line starts at i+2
				offsets = append(offsets, i+2)
				i++ // Skip the \n
			} else {
				// Bare \r: treat as line break (rare but possible)
				offsets = append(offsets, i+1)
			}
		}
	}

	return offsets
}

// computeRuneOffsets precomputes the byte offset of each rune.
// runeOffsets[i] is the byte offset of the i-th rune (0-indexed).
func computeRuneOffsets(content []byte) []int {
	// Count runes first to pre-allocate
	runeCount := utf8.RuneCount(content)
	offsets := make([]int, 0, runeCount)

	for i := 0; i < len(content); {
		offsets = append(offsets, i)
		_, size := utf8.DecodeRune(content[i:])
		// DecodeRune always returns size >= 1 (invalid bytes return RuneError, 1)
		i += size
	}

	return offsets
}

// findLine finds the 1-based line number for a given byte offset using binary search.
// The byte offset must be in range [0, len(content)].
func findLine(lineOffsets []int, byteOffset int) int {
	// Binary search for the largest line whose start offset <= byteOffset
	lo, hi := 0, len(lineOffsets)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if lineOffsets[mid] <= byteOffset {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo + 1 // Convert 0-indexed to 1-based line number
}

// columnFromByteOffset computes the 1-based column for a byte offset within a line.
// Uses O(log n) binary search over precomputed runeOffsets for efficient lookup.
//
// contentLen is needed to handle EOF positions where byteOffset equals the content length.
func columnFromByteOffset(runeOffsets []int, lineStartByte, byteOffset, contentLen int) int {
	if byteOffset <= lineStartByte {
		return 1
	}

	// Binary search for the rune index at lineStartByte
	lineStartRune := findRuneIndex(runeOffsets, lineStartByte)
	// Binary search for the rune index at byteOffset (floor semantics)
	targetRune := findRuneIndex(runeOffsets, byteOffset)

	// Handle EOF position: if byteOffset is at/past end of content,
	// targetRune should include all runes up to the end
	if byteOffset >= contentLen && len(runeOffsets) > 0 {
		targetRune = len(runeOffsets)
	}

	// Column is the difference + 1 (1-based)
	return targetRune - lineStartRune + 1
}

// findRuneIndex returns the rune index for a given byte offset using binary search.
// If byteOffset falls mid-rune, returns the index of that rune (floor semantics).
func findRuneIndex(runeOffsets []int, byteOffset int) int {
	if len(runeOffsets) == 0 {
		return 0
	}

	// Binary search for largest index where runeOffsets[i] <= byteOffset
	lo, hi := 0, len(runeOffsets)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if runeOffsets[mid] <= byteOffset {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// countRunesInRange counts the number of runes in content[start:end].
// Returns a 1-based column number (first column is 1).
//
// Uses floor semantics for mid-rune offsets: if end falls inside a multi-byte
// rune, that rune is NOT counted. This is consistent with LSP position semantics.
func countRunesInRange(content []byte, start, end int) int {
	if start >= end {
		return 1 // At line start, column 1
	}

	count := 0
	for i := start; i < end; {
		_, size := utf8.DecodeRune(content[i:])
		// Floor semantics: don't count partial rune at end boundary
		if i+size > end {
			break
		}
		count++
		i += size
	}

	return count + 1 // 1-based column
}
