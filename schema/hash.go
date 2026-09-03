package schema

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"regexp"
	"slices"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/expr"
)

// StructuralHashVersion identifies the version of the structural hashing
// algorithm. Bump this when the hash algorithm changes in a way that
// invalidates previous hashes. Version 2 (v0.15.0) added invariants and
// the abstract/part markers. Version 3 (v0.17.0) widened the input from
// the entry schema's own declarations to its whole import closure, so the
// hash covers every rule that decides what instance data is valid — an
// imported type's constraints decide validity for every instance of it the
// importing schema admits. Version 4 (v0.21.0) writes a relation target as
// the pair (owning schema name, type name) and normalizes the two float
// zeros, so two closure members declaring one type name no longer collide.
const StructuralHashVersion = 4

// StructuralHash computes a deterministic, content-based identity over the
// rules that decide what instance data is valid, across the schema's whole
// import closure: for every member of [Schema.Closure], its types,
// properties, relations, compositions, data types, constraints, invariants,
// and the abstract and part markers.
//
// Two schemas that hash the same agree on every rule everywhere in their
// closures. The converse does not hold: a hash can move on a change to an
// imported schema this schema never references, because closure membership
// is part of the identity. Each member is framed under its schema name, the
// entry schema first and the remaining members ordered by name, so the
// order imports are declared in does not enter the digest.
//
// Annotations are deliberately excluded — they configure downstream store
// DDL and never reject data. So are import aliases and source paths: a member
// frame carries the schema's declared name, never its source, and a relation
// target is the pair (owning schema name, type name) — never a path. A hashed
// path would make embedded:// and disk loads of one schema text disagree, and
// consumer dispatch relies on the hash carrying no source path. A supertype
// hashes by name alone, which loses nothing: inheritance merges the ancestor's
// members into the subtype and those members are hashed here, so two
// candidate ancestors that differ in any rule already produce different
// digests.
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
	writeTag(h, "yammm-schema-v4")

	// Closure() returns the entry schema first; the rest are re-ordered by
	// name so a re-ordered import list hashes the same. The slice is a copy,
	// so sorting it in place touches no cached state. Ties on name keep
	// closure order, which is deterministic, so the digest stays so.
	members := s.Closure()
	slices.SortStableFunc(members[1:], func(a, b *Schema) int {
		return compareName(a.Name(), b.Name())
	})

	owners := make(map[location.SourceID]string, len(members))
	for _, member := range members {
		owners[member.SourceID()] = member.Name()
	}

	writeLen(h, len(members))
	for _, member := range members {
		hashSchemaMember(h, member, owners)
	}

	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// hashSchemaMember hashes one closure member's own declarations under a
// frame opened by its name: its local types, then its local data types. The
// frame is what keeps two different closures from serializing to one byte
// stream.
func hashSchemaMember(h io.Writer, s *Schema, owners map[location.SourceID]string) {
	writeTag(h, "schema")
	writeStr(h, s.Name())

	// Types (lexicographic iteration guaranteed by s.Types()).
	for _, typ := range s.Types() {
		hashType(h, typ, owners)
	}

	// Data types (lexicographic iteration guaranteed by s.DataTypes()).
	for _, dt := range s.DataTypes() {
		writeTag(h, "datatype")
		writeStr(h, dt.Name())
		hashConstraint(h, dt.Constraint())
	}
}

// hashType hashes a single type's structural shape.
func hashType(h io.Writer, typ *Type, owners map[location.SourceID]string) {
	writeTag(h, "type")
	writeStr(h, typ.ID().Name())
	writeBool(h, typ.IsAbstract())
	writeBool(h, typ.IsPart())

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
		writeTargetID(h, rel.TargetID(), owners)
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
		writeTargetID(h, rel.TargetID(), owners)
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

	// Invariants: order-independent, because two schemas differing only in
	// invariant order accept the same data. Each expression serializes to
	// its own blob; the blobs sort by bytes and hash length-prefixed. The
	// name is excluded — it is the failure message — and inherited
	// invariants hash like local ones, since both decide validity.
	invs := typ.AllInvariantsSlice()
	blobs := make([][]byte, 0, len(invs))
	for _, inv := range invs {
		e := inv.Expression()
		if e == nil {
			continue
		}
		var buf bytes.Buffer
		hashExpression(&buf, e)
		blobs = append(blobs, buf.Bytes())
	}
	slices.SortFunc(blobs, bytes.Compare)
	writeTag(h, "invariants")
	writeLen(h, len(blobs))
	for _, blob := range blobs {
		writeLen(h, len(blob))
		hwrite(h, blob)
	}
}

// hashExpression serializes one expression node deterministically: a
// node-kind tag, then the node's content, children in order. There is no
// canonical rendering in schema/expr to reuse, and none to drift from.
func hashExpression(w io.Writer, e expr.Expression) {
	if e == nil {
		writeTag(w, "x:nil")
		return
	}
	switch n := e.(type) {
	case expr.SExpr:
		writeTag(w, "x:sexpr")
		writeLen(w, len(n))
		for _, child := range n {
			hashExpression(w, child)
		}
	case expr.Op:
		writeTag(w, "x:op")
		writeStr(w, string(n))
	case expr.DatatypeLiteral:
		writeTag(w, "x:dt")
		writeStr(w, string(n))
	case *expr.Literal:
		writeTag(w, "x:lit")
		hashLiteralValue(w, n.Val)
	default:
		panic(fmt.Sprintf("schema: hashExpression: unrecognized node %T", e))
	}
}

// hashLiteralValue hashes a literal by Go kind and value, over the eight
// dynamic types [expr.Literal] documents.
func hashLiteralValue(w io.Writer, v any) {
	switch val := v.(type) {
	case nil:
		writeTag(w, "l:nil")
	case string:
		writeTag(w, "l:str")
		writeStr(w, val)
	case int64:
		writeTag(w, "l:int")
		writeInt64(w, val)
	case float64:
		writeTag(w, "l:float")
		writeFloat64(w, val)
	case bool:
		writeTag(w, "l:bool")
		writeBool(w, val)
	case *regexp.Regexp:
		writeTag(w, "l:re")
		writeStr(w, val.String())
	case []expr.Expression:
		writeTag(w, "l:args")
		writeLen(w, len(val))
		for _, e := range val {
			hashExpression(w, e)
		}
	case []string:
		writeTag(w, "l:params")
		writeLen(w, len(val))
		for _, s := range val {
			writeStr(w, s)
		}
	default:
		panic(fmt.Sprintf("schema: hashLiteralValue: unrecognized literal type %T", v))
	}
}

// hashConstraint hashes a constraint's kind and parameters.
// Aliases are resolved first via [ResolveAlias].
// Panics on unrecognized constraint kinds.
func hashConstraint(h io.Writer, c Constraint) {
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

// hwrite writes to a hash sink. Neither sink errors: hash.Hash documents
// that Write never returns one, and bytes.Buffer grows or panics.
func hwrite(w io.Writer, b []byte) {
	w.Write(b) //nolint:errcheck,gosec // hash sinks never error: hash.Hash documents it, bytes.Buffer grows or panics
}

// writeTargetID writes a relation target as the pair that is both unique and
// portable: the declared name of the closure member that owns the target, then
// the target's own name. The owning name is empty for a target outside the
// closure, which only an unresolved relation produces.
func writeTargetID(h io.Writer, id TypeID, owners map[location.SourceID]string) {
	writeStr(h, owners[id.SchemaPath()])
	writeStr(h, id.Name())
}

// writeStr writes a length-prefixed string: 4-byte big-endian uint32 length
// followed by the raw bytes.
func writeStr(h io.Writer, s string) {
	writeLen(h, len(s))
	hwrite(h, []byte(s))
}

// writeTag writes a tagged string (alias for writeStr).
func writeTag(h io.Writer, tag string) {
	writeStr(h, tag)
}

// writeBool writes a single byte: 0x01 for true, 0x00 for false.
func writeBool(h io.Writer, b bool) {
	if b {
		hwrite(h, []byte{0x01})
	} else {
		hwrite(h, []byte{0x00})
	}
}

// writeLen writes a non-negative integer as 4 bytes big-endian.
// Used for string lengths and collection counts.
func writeLen(h io.Writer, v int) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(v)) //nolint:gosec // lengths are always non-negative and well within uint32 range
	hwrite(h, buf[:])
}

// writeInt64 writes an int64 as 8 bytes big-endian.
func writeInt64(h io.Writer, v int64) {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(v)) //nolint:gosec // reinterpret cast for binary encoding, not arithmetic
	hwrite(h, buf[:])
}

// writeFloat64 writes a float64 as 8 bytes IEEE 754 big-endian
// (via [math.Float64bits]).
func writeFloat64(h io.Writer, v float64) {
	// FloatConstraint.Equal compares with ==, which calls the two zeros equal.
	if v == 0 {
		v = 0
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], math.Float64bits(v))
	hwrite(h, buf[:])
}

// compareName is a string comparison helper for [slices.SortFunc].
func compareName(a, b string) int {
	return cmp.Compare(a, b)
}
