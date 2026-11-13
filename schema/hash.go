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

	"github.com/simon-lentz/yammm/internal/expr"
	"github.com/simon-lentz/yammm/location"
)

// StructuralHashVersion identifies the version of the structural hashing
// algorithm. Bump this when the hash algorithm changes in a way that
// invalidates previous hashes. Version 2 (v0.15.0) added invariants and
// the abstract/part markers. Version 3 (v0.17.0) widened the input from
// the entry schema's own declarations to its whole import closure, so the
// hash covers every rule that decides what instance data is valid — an
// imported type's constraints decide validity for every instance of it the
// importing schema admits. Version 4 (v0.21.0) writes a relation target as
// the pair (owning schema name, type name), so two closure members declaring
// one type name no longer collide, and normalizes the two float zeros of a
// Float bound.
const StructuralHashVersion = 4

// StructuralHash returns a deterministic "sha256:<hex>" digest of the rules
// that decide what instance data is valid, over every member of
// [Schema.Closure]. Two schemas with one digest agree on every hashed rule in
// their closures. The package doc's Structural Hash section states what is
// hashed and what is left out, the entry schema's name surface among it.
//
// Panics if s is zero.
func StructuralHash(s Schema) string {
	if s.IsZero() {
		panic("schema.StructuralHash: zero Schema")
	}

	h := sha256.New()

	// Domain separator.
	writeTag(h, "yammm-schema-v4")

	// The entry schema stays first and the rest sort by name, so import order is
	// not hashed. The slice is a copy, so sorting it touches no cached state.
	members := slices.Collect(s.Closure())
	slices.SortFunc(members[1:], func(a, b Schema) int {
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
func hashSchemaMember(h io.Writer, s Schema, owners map[location.SourceID]string) {
	writeTag(h, "schema")
	writeStr(h, s.Name())

	for _, typ := range slices.SortedFunc(s.Types(), func(a, b Type) int { return cmp.Compare(a.name, b.name) }) {
		hashType(h, typ, owners)
	}

	for _, dt := range slices.SortedFunc(s.DataTypes(), func(a, b DataType) int { return cmp.Compare(a.name, b.name) }) {
		writeTag(h, "datatype")
		writeStr(h, dt.Name())
		hashConstraint(h, dt.Constraint())
	}
}

// hashType hashes a single type's structural shape.
func hashType(h io.Writer, typ Type, owners map[location.SourceID]string) {
	writeTag(h, "type")
	writeStr(h, typ.ID().Name())
	writeBool(h, typ.IsAbstract())
	writeBool(h, typ.IsPart())

	// Primary keys (sorted by name).
	pks := slices.Collect(typ.PrimaryKeys())
	slices.SortFunc(pks, func(a, b Property) int {
		return compareName(a.Name(), b.Name())
	})
	for _, pk := range pks {
		writeTag(h, "pk")
		writeStr(h, pk.Name())
		hashConstraint(h, pk.Constraint())
	}

	// All properties (sorted by name).
	props := slices.Collect(typ.AllProperties())
	slices.SortFunc(props, func(a, b Property) int {
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
	assocs := slices.Collect(typ.AllAssociations())
	slices.SortFunc(assocs, func(a, b Relation) int {
		return compareName(a.Name(), b.Name())
	})
	for _, rel := range assocs {
		writeTag(h, "assoc")
		writeStr(h, rel.Name())
		writeTargetID(h, rel.TargetID(), owners)
		writeBool(h, rel.IsOptional())
		writeBool(h, rel.IsMany())

		// Edge properties (sorted by name).
		edgeProps := slices.Collect(rel.Properties())
		slices.SortFunc(edgeProps, func(a, b Property) int {
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
	comps := slices.Collect(typ.AllCompositions())
	slices.SortFunc(comps, func(a, b Relation) int {
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
	supers := slices.Collect(typ.SuperTypes())
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
	invs := slices.Collect(typ.AllInvariants())
	blobs := make([][]byte, 0, len(invs))
	for _, inv := range invs {
		e := inv.expr
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
// canonical rendering in internal/expr to reuse, and none to drift from.
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
		// A literal's zero keeps its sign, where a bound's does not: x / -0.0
		// and x / 0.0 evaluate apart.
		writeTag(w, "l:float")
		writeFloat64Bits(w, val)
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
		// Hash version 4 writes a format's length here. A Timestamp holds no
		// format, and leaving the write out would move the hash of every
		// schema that declares one.
		writeLen(h, 0)

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
		// The count, 0 or 1, keeps the encoding of a pattern list, so a
		// StructuralHash a consumer stored for a one-pattern constraint holds.
		writeTag(h, "c:pattern")
		if cc.compiled == nil {
			writeLen(h, 0)
			break
		}
		writeLen(h, 1)
		writeStr(h, cc.Pattern())

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
// (via [math.Float64bits]), the two zeros as one.
func writeFloat64(h io.Writer, v float64) {
	// FloatConstraint.Equal compares with ==, which calls the two zeros equal.
	if v == 0 {
		v = 0
	}
	writeFloat64Bits(h, v)
}

// writeFloat64Bits writes a float64 as its 8 IEEE 754 bytes, big-endian.
func writeFloat64Bits(h io.Writer, v float64) {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], math.Float64bits(v))
	hwrite(h, buf[:])
}

// compareName is a string comparison helper for [slices.SortFunc].
func compareName(a, b string) int {
	return cmp.Compare(a, b)
}
