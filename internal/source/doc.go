// Package source provides a schema source registry for content storage and
// position conversion.
//
// This package is the internal foundation for managing source content and
// computing byte offset / line-column conversions. It does NOT perform
// formatting or excerpt rendering - that responsibility belongs exclusively
// to the diag package.
//
// # Responsibilities
//
// The source registry has the following responsibilities:
//
//   - Store raw source bytes keyed by [location.SourceID]
//   - Precompute line-start byte offsets for efficient position lookup
//   - Precompute rune-to-byte offset tables for ANTLR token conversion
//   - Convert byte offset to [location.Position] (PositionAt)
//   - Provide raw bytes to consumers as a [diag.SourceProvider]
//   - Enforce uniqueness of source identity keys
//
// # Newline and Column Handling
//
// The registry follows these rules for newline handling:
//
//   - Treat \r\n (CRLF) as a single line break
//   - Treat \n (LF) as a single line break
//   - Treat bare \r (CR) as a single line break
//
// Column counting follows these rules:
//
//   - Columns count runes (Unicode code points) from line start, not bytes
//   - Tab characters count as 1 rune (no width expansion)
//   - Column numbers are 1-based (first column is 1)
//
// # Lifecycle and Concurrency
//
// The registry is designed for a "build once, read many" lifecycle:
//
//   - During schema loading, content is registered via Register
//   - Register is safe for concurrent access (synchronized with RWMutex)
//   - After build completes, the registry is effectively immutable
//   - Read methods (Content, PositionAt, etc.) are safe for concurrent reads
//   - Clear() resets the registry, requiring exclusive access
//
// # Identity and Uniqueness
//
// Source identity uses [location.SourceID]. The registry enforces uniqueness:
//
//   - Registration with an existing SourceID and identical content succeeds (idempotent)
//   - Registration with an existing SourceID and different content returns [*KeyCollisionError]
//   - This ensures the SourceID uniqueness invariant is enforced
//
// # Interface Satisfaction
//
// The [*Registry] type satisfies:
//
//   - [location.PositionRegistry] — via PositionAt method
//   - [diag.SourceProvider] — via Content method (accepts [location.Span])
//   - [diag.LineIndexProvider] — via LineStartByte method
//
// # Usage
//
// The typical usage pattern:
//
//	reg := source.NewRegistry()
//
//	// During loading/building:
//	sourceID := location.MustSourceIDFromPath("schema.yammm")
//	if err := reg.Register(sourceID, content); err != nil {
//	    // handle collision error
//	}
//
//	// During error reporting:
//	if content, ok := reg.ContentBySource(sourceID); ok {
//	    // use content for excerpt rendering via diag
//	}
//
//	// For position conversion:
//	pos := reg.PositionAt(sourceID, byteOffset)
//	if !pos.IsZero() {
//	    // pos.Line, pos.Column, pos.Byte are populated
//	}
package source
