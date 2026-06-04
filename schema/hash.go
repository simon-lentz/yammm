package schema

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"math"
	"slices"
)

// StructuralHashVersion identifies the version of the structural hashing
// algorithm. Bump this when the hash algorithm changes in a way that
// invalidates previous hashes.
const StructuralHashVersion = 1

// StructuralHash computes a deterministic, content-based hash of a schema's
// structural shape. Two schemas produce the same hash if and only if they
// define the same types, properties, relations, compositions, data types,
// and constraints (by name, kind, and parameters).
//
// Invariants are deliberately excluded — they constrain runtime validation
// but do not affect the structural shape.
//
// The returned string has the format "sha256:<hex>".
//
// Panics if s is nil.
func StructuralHash(s *Schema) string {
	if s == nil {
		panic("schema: StructuralHash called on nil *Schema")
	}

	h := sha256.New()

	// Domain separator.
	writeTag(h, "yammm-schema-v1")

	// Schema name.
	writeStr(h, s.Name())

	// Types (lexicographic iteration guaranteed by s.Types()).
	for _, typ := range s.Types() {
		hashType(h, typ)
	}

	// Data types (lexicographic iteration guaranteed by s.DataTypes()).
	for _, dt := range s.DataTypes() {
		writeTag(h, "datatype")
		writeStr(h, dt.Name())
		hashConstraint(h, dt.Constraint())
	}

	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// hashType hashes a single type's structural shape.
func hashType(h hash.Hash, typ *Type) {
	writeTag(h, "type")
	writeStr(h, typ.ID().Name())

	// Primary keys (sorted by name).
	pks := typ.PrimaryKeysSlice()
	slices.SortFunc(pks, func(a, b *Property) int {
		return compareName(a.Name(), b.Name())
	})
	for _, pk := range pks {
		writeTag(h, "pk")
		writeStr(h, pk.Name())
		hashConstraint(h, pk.Constraint())
	}

	// All properties (sorted by name).
	props := typ.AllPropertiesSlice()
	slices.SortFunc(props, func(a, b *Property) int {
		return compareName(a.Name(), b.Name())
	})
	for _, prop := range props {
		writeTag(h, "prop")
		writeStr(h, prop.Name())
		writeBool(h, !prop.IsOptional()) // required
		writeBool(h, prop.IsPrimaryKey())
		hashConstraint(h, prop.Constraint())
	}

	// All associations (sorted by name).
	assocs := typ.AllAssociationsSlice()
	slices.SortFunc(assocs, func(a, b *Relation) int {
		return compareName(a.Name(), b.Name())
	})
	for _, rel := range assocs {
		writeTag(h, "assoc")
		writeStr(h, rel.Name())
		writeStr(h, rel.TargetID().Name())
		writeBool(h, rel.IsOptional())
		writeBool(h, rel.IsMany())

		// Edge properties (sorted by name).
		edgeProps := rel.PropertiesSlice()
		slices.SortFunc(edgeProps, func(a, b *Property) int {
			return compareName(a.Name(), b.Name())
		})
		for _, prop := range edgeProps {
			writeTag(h, "edge-prop")
			writeStr(h, prop.Name())
			writeBool(h, !prop.IsOptional()) // required
			hashConstraint(h, prop.Constraint())
		}
	}

	// All compositions (sorted by name).
	comps := typ.AllCompositionsSlice()
	slices.SortFunc(comps, func(a, b *Relation) int {
		return compareName(a.Name(), b.Name())
	})
	for _, rel := range comps {
		writeTag(h, "comp")
		writeStr(h, rel.Name())
		writeStr(h, rel.TargetID().Name())
		writeBool(h, rel.IsOptional())
		writeBool(h, rel.IsMany())
	}

	// Super types (sorted by ID name).
	supers := typ.SuperTypesSlice()
	slices.SortFunc(supers, func(a, b ResolvedTypeRef) int {
		return compareName(a.ID().Name(), b.ID().Name())
	})
	for _, ref := range supers {
		writeTag(h, "extends")
		writeStr(h, ref.ID().Name())
	}
}

// hashConstraint hashes a constraint's kind and parameters.
// Aliases are resolved first via [ResolveAlias].
// Panics on unrecognized constraint kinds.
func hashConstraint(h hash.Hash, c Constraint) {
	c = ResolveAlias(c)

	switch cc := c.(type) {
	case StringConstraint:
		writeTag(h, "c:string")
		lo, hasLo := cc.MinLen()
		writeBool(h, hasLo)
		if hasLo {
			writeInt64(h, lo)
		}
		hi, hasHi := cc.MaxLen()
		writeBool(h, hasHi)
		if hasHi {
			writeInt64(h, hi)
		}

	case IntegerConstraint:
		writeTag(h, "c:integer")
		lo, hasLo := cc.Min()
		writeBool(h, hasLo)
		if hasLo {
			writeInt64(h, lo)
		}
		hi, hasHi := cc.Max()
		writeBool(h, hasHi)
		if hasHi {
			writeInt64(h, hi)
		}

	case FloatConstraint:
		writeTag(h, "c:float")
		lo, hasLo := cc.Min()
		writeBool(h, hasLo)
		if hasLo {
			writeFloat64(h, lo)
		}
		hi, hasHi := cc.Max()
		writeBool(h, hasHi)
		if hasHi {
			writeFloat64(h, hi)
		}

	case BooleanConstraint:
		writeTag(h, "c:boolean")

	case TimestampConstraint:
		writeTag(h, "c:timestamp")
		writeStr(h, cc.Format())

	case DateConstraint:
		writeTag(h, "c:date")

	case UUIDConstraint:
		writeTag(h, "c:uuid")

	case EnumConstraint:
		writeTag(h, "c:enum")
		values := cc.Values()
		slices.Sort(values)
		writeLen(h, len(values))
		for _, val := range values {
			writeStr(h, val)
		}

	case PatternConstraint:
		writeTag(h, "c:pattern")
		patterns := cc.Patterns()
		slices.Sort(patterns)
		writeLen(h, len(patterns))
		for _, pat := range patterns {
			writeStr(h, pat)
		}

	case VectorConstraint:
		writeTag(h, "c:vector")
		writeLen(h, cc.Dimension())

	case ListConstraint:
		writeTag(h, "c:list")
		lo, hasLo := cc.MinLen()
		writeBool(h, hasLo)
		if hasLo {
			writeInt64(h, lo)
		}
		hi, hasHi := cc.MaxLen()
		writeBool(h, hasHi)
		if hasHi {
			writeInt64(h, hi)
		}
		hashConstraint(h, cc.Element())

	default:
		panic(fmt.Sprintf("schema: hashConstraint: unrecognized constraint kind %v", c.Kind()))
	}
}

// --- length-prefixed binary framing helpers ---

// writeStr writes a length-prefixed string: 4-byte big-endian uint32 length
// followed by the raw bytes.
func writeStr(h hash.Hash, s string) {
	writeLen(h, len(s))
	h.Write([]byte(s))
}

// writeTag writes a tagged string (alias for writeStr).
func writeTag(h hash.Hash, tag string) {
	writeStr(h, tag)
}

// writeBool writes a single byte: 0x01 for true, 0x00 for false.
func writeBool(h hash.Hash, b bool) {
	if b {
		h.Write([]byte{0x01})
	} else {
		h.Write([]byte{0x00})
	}
}

// writeLen writes a non-negative integer as 4 bytes big-endian.
// Used for string lengths and collection counts.
func writeLen(h hash.Hash, v int) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(v)) //nolint:gosec // lengths are always non-negative and well within uint32 range
	h.Write(buf[:])
}

// writeInt64 writes an int64 as 8 bytes big-endian.
func writeInt64(h hash.Hash, v int64) {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(v)) //nolint:gosec // reinterpret cast for binary encoding, not arithmetic
	h.Write(buf[:])
}

// writeFloat64 writes a float64 as 8 bytes IEEE 754 big-endian
// (via [math.Float64bits]).
func writeFloat64(h hash.Hash, v float64) {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], math.Float64bits(v))
	h.Write(buf[:])
}

// compareName is a string comparison helper for [slices.SortFunc].
func compareName(a, b string) int {
	return cmp.Compare(a, b)
}
