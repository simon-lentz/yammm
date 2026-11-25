package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

// objectSizes maps each object format git names to the size of its object
// names.
var objectSizes = map[string]int{"sha1": 20, "sha256": 32}

// indexCaches holds the index extensions git rewrites as caches, here on a
// read: the cache tree, the untracked cache, the fsmonitor state and the
// tables that let git read the entries in parallel.
var indexCaches = map[string]bool{"TREE": true, "UNTR": true, "FSMN": true, "EOIE": true, "IEOT": true}

var errIndex = errors.New("not an index git writes")

// readIndex returns the sum of each entry's mode, object name, flags and path
// in gitformat-index's versions 2 to 4, and each extension but the caches. It
// leaves out stat data, since go list's git status writes it again on a read.
func readIndex(path string, objectSize int) ([sha256.Size]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return [sha256.Size]byte{}, err //nolint:wrapcheck // the caller wraps it
	}
	sum, err := indexSum(b, objectSize)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("%s: %w", path, err)
	}
	return sum, nil
}

func indexSum(b []byte, objectSize int) ([sha256.Size]byte, error) {
	end := len(b) - objectSize // the trailing checksum
	if end < 12 || string(b[:4]) != "DIRC" {
		return [sha256.Size]byte{}, errIndex
	}
	version := binary.BigEndian.Uint32(b[4:8])
	if version < 2 || version > 4 {
		return [sha256.Size]byte{}, fmt.Errorf("%w: version %d", errIndex, version)
	}
	count := binary.BigEndian.Uint32(b[8:12])
	h := sha256.New()
	// Each entry is 40 bytes of stat data, the mode among them, then the
	// object name and 16 bits of flags.
	fixed := 40 + objectSize + 2
	off := 12
	var name []byte
	for range count {
		start := off
		if off+fixed > end {
			return [sha256.Size]byte{}, errIndex
		}
		mode, object := b[off+24:off+28], b[off+40:off+40+objectSize]
		flags := binary.BigEndian.Uint16(b[off+40+objectSize:])
		off += fixed
		var extended uint16
		if flags&0x4000 != 0 {
			if version < 3 || off+2 > end {
				return [sha256.Size]byte{}, errIndex
			}
			extended = binary.BigEndian.Uint16(b[off:])
			off += 2
		}
		if version == 4 {
			// The path is the previous one less a count of its last bytes,
			// then the bytes up to a NUL; no padding follows.
			strip, n := indexVarint(b[off:end])
			kept := len(name) - int(min(strip, uint64(len(name)))) //nolint:gosec // at most len(name)
			if n == 0 || strip > uint64(len(name)) {
				return [sha256.Size]byte{}, errIndex
			}
			off += n
			nul := bytes.IndexByte(b[off:end], 0)
			if nul < 0 {
				return [sha256.Size]byte{}, errIndex
			}
			name = append(name[:kept], b[off:off+nul]...)
			off += nul + 1
		} else {
			// The path ends at a NUL, and NULs pad the entry to a multiple of 8.
			nul := bytes.IndexByte(b[off:end], 0)
			if nul < 0 {
				return [sha256.Size]byte{}, errIndex
			}
			name = append(name[:0], b[off:off+nul]...)
			off = start + (off-start+nul+8)&^7
			if off > end {
				return [sha256.Size]byte{}, errIndex
			}
		}
		h.Write(mode)
		h.Write(object)
		// The low 12 bits of flags are the path's length.
		h.Write(binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(nil, flags&0xF000), extended))
		h.Write(name)
		h.Write([]byte{0})
	}
	for off < end {
		if off+8 > end {
			return [sha256.Size]byte{}, errIndex
		}
		size := int64(binary.BigEndian.Uint32(b[off+4 : off+8]))
		if size > int64(end-off-8) {
			return [sha256.Size]byte{}, errIndex
		}
		next := off + 8 + int(size)
		if !indexCaches[string(b[off:off+4])] {
			h.Write(b[off:next])
		}
		off = next
	}
	return [sha256.Size]byte(h.Sum(nil)), nil
}

// indexVarint decodes git's offset varint, the one index version 4 prefixes
// each path with, and returns it with the bytes it took, or 0 bytes when b
// does not hold one.
func indexVarint(b []byte) (uint64, int) {
	if len(b) == 0 {
		return 0, 0
	}
	c := b[0]
	v := uint64(c & 0x7f)
	n := 1
	for c&0x80 != 0 {
		v++
		if n == len(b) || v>>57 != 0 {
			return 0, 0
		}
		c = b[n]
		n++
		v = v<<7 | uint64(c&0x7f)
	}
	return v, n
}
