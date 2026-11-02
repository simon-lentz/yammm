package snapshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"slices"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// FIELD ORDER IN WIRE STRUCTS IS PART OF THE FORMAT CONTRACT.
//
// encoding/json serializes struct fields in declaration order. The integrity
// hash covers the exact serialized bytes. Reordering fields changes every
// document Marshal writes from then on; a saved file still verifies, since the
// hash is checked over the file's own bytes, but no longer equals what Marshal
// writes for the same snapshot.
//
// Rules:
//   - Do NOT reorder existing fields within a format version.
//   - New fields may be appended (with omitempty for backward compatibility).
//   - Removing or reordering fields requires a format version bump.
//   - TestWireStructs_FieldOrder in wire_internal_test.go pins every wire
//     struct's field order and tags by name, and
//     TestWireStructs_EveryStructIsPinned fails when a new wire struct is
//     added without a row there.
//
// TOP-LEVEL KEY ORDER AND BODY-SUFFIX STABILITY CONTRACT.
//
// The .ys wire format commits to two structural contracts beyond the
// per-struct field order above, and UpdateMetadata adds a third rule of its
// own. The two format contracts are load-bearing for UpdateMetadata's
// byte-surgery primitive: they are properties of the bytes Marshal emits, and
// relaxing either silently breaks every existing and future metadata-rewrite
// call site. The third is a behaviour of UpdateMetadata rather than of the
// format, and relaxing it would announce itself through the version field.
//
//  1. Field-order contract (established pre-v0.3.0). The top-level
//     document has exactly four keys in a fixed order:
//     yammm_snapshot → types → instances → diagnostics. No future Marshal
//     change may reorder these or insert a new key in between.
//
//  2. Body-byte-range stability contract (introduced v0.3.0). The byte
//     range starting at the ',' immediately following the yammm_snapshot
//     header value's closing '}' and running through the document's
//     final '}' is the "body suffix". UpdateMetadata preserves this
//     suffix verbatim: it rebuilds only the header, recomputes the
//     integrity hash over new_header + body_suffix, and emits
//     new_header + body_suffix as the output. Future Marshal changes
//     that insert a fifth top-level key, shift the header-body
//     transition, or change the inter-key separator pattern silently
//     break UpdateMetadata even without relaxing the field-order rule.
//     UpdateMetadata(x, newMeta) == Marshal(Load(x), WithIndent(i)) for a
//     document the current Marshal produced with indent i that carries no
//     created_at, when newMeta is empty: UpdateMetadata keeps the input's
//     indent. The last two conditions are not the same kind of condition.
//     created_at is a condition on the document: UpdateMetadata preserves
//     it from the input header and Load does not return it, so a
//     re-marshal cannot reproduce it. Metadata is a condition on the
//     call: UpdateMetadata writes newMeta and discards whatever the
//     input carried, so a document holding metadata still matches a
//     re-marshal when newMeta is empty, and a document holding none
//     diverges when newMeta is not.
//
//  3. Version preservation (introduced v0.12.0). UpdateMetadata rebuilds
//     the header at the version it read, so it is a header rewrite and
//     never a migration. The only readable version is the current one:
//     an older document is refused with
//     E_SNAPSHOT_UNSUPPORTED_VERSION rather than rewritten, and
//     UpdateMetadataOrReMarshal's Load + Marshal fallback refuses it
//     the same way.
//
// These contracts are tested in snapshot/wire_test.go via:
//   - TestWireFormat_TopLevelKeyOrder (token-level key-order pin)
//   - TestWireFormat_BodySuffixContract (bodyOffset-capture + shape pin)
//
// the equivalence above in snapshot/wire_equivalence_test.go via
// TestWireContract_UpdateMetadataMatchesReMarshal, which pins the
// equality, the created_at divergence and both metadata directions in
// the same test, and version preservation in
// snapshot/update_contract_test.go via
// TestUpdateMetadata_PreservesInputVersion.
//
// TestWireFormat_TopLevelKeyOrder and TestWireFormat_BodySuffixContract run
// across a corpus of Marshal outputs under six combinations of the indent,
// created_at and metadata options, so a future Marshal-side
// change that silently breaks either contract fails at the wire-format
// test level before reaching the consumers that depend on it.
// See snapshot/update.go for the UpdateMetadata primitive that consumes
// these contracts.

// topLevelKeys is the exact key sequence contract 1 fixes. A document that
// does not present these four keys, in this order, exactly once each, and none
// of them null, did not come from Marshal.
var topLevelKeys = [...]string{"yammm_snapshot", "types", "instances", "diagnostics"}

// checkTopLevelKeys is the one definition of the document's outermost shape:
// presence, order, uniqueness and non-nullness of the four top-level keys, and
// that the document ends where the object does. Every surface holding the whole
// document runs it. [HeaderOnlyRead] and [ScanDir] cannot and say so on their
// own godoc: they read through a capped reader and never hold the body.
//
// It is not free: it scans the whole document and copies each top-level value
// to skip it, so a caller that never decodes the body still pays a pass over
// it. That cost is pinned by TestUpdateMetadataRatioFloor rather
// than described here, because the first draft of this comment asserted a cost
// profile nobody had measured and was false.
func checkTopLevelKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("expected JSON object: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("expected JSON object, got %s", diag.ValueText(fmt.Sprint(tok)))
	}

	seen := 0
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("expected top-level key: %w", err)
		}
		key, ok := tok.(string)
		if !ok {
			return fmt.Errorf("expected a top-level key, got %s", diag.ValueText(fmt.Sprint(tok)))
		}
		if seen == len(topLevelKeys) {
			return fmt.Errorf("document carries a fifth top-level key %s; the format has exactly %d",
				diag.QuoteValue(key), len(topLevelKeys))
		}
		if key != topLevelKeys[seen] {
			return fmt.Errorf("top-level key %d is %s, expected %q", seen, diag.QuoteValue(key), topLevelKeys[seen])
		}
		if err := skipValue(dec, key); err != nil {
			return err
		}
		seen++
	}
	if seen != len(topLevelKeys) {
		return fmt.Errorf("document carries %d top-level keys, expected %d; %q is missing",
			seen, len(topLevelKeys), topLevelKeys[seen])
	}

	// More reports false at the object's closing delimiter and at end of input
	// alike, so the loop above exits identically on a complete document and on
	// one truncated before its final brace. Reading the next token separates
	// them, and there is no third outcome: a decoder that reports no more values
	// at object level returns either that closing brace or an error, so the
	// brace itself is not worth a branch nothing can drive.
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("document ends before the top-level object closes: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("document carries trailing data after the top-level object")
	}
	return nil
}

// skipValue advances past one top-level value and reports a null.
//
// It decodes into a json.RawMessage, which copies the value. A token walk
// avoids the copy and is SLOWER — encoding/json finds a value's bounds with
// its scanner and only tokenizes on demand, so walking every token to skip a
// section costs more than copying it. Measured: the token form put
// UpdateMetadata below the 3x floor TestUpdateMetadataRatioFloor
// pins. The copy is the cheaper of the two.
func skipValue(dec *json.Decoder, key string) error {
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return fmt.Errorf("failed to read the value of %q: %w", key, err)
	}
	if string(raw) == "null" {
		return fmt.Errorf("top-level key %q is null", key)
	}
	return nil
}

// MinReadableVersion is the lowest .ys wire-format version this package
// accepts on read paths (Load, Verify, Info, HeaderOnly, HeaderOnlyRead, the
// ScanDir family) and at UpdateMetadata, which decodes the header it rewrites.
// The accept range is the closed interval [MinReadableVersion, currentVersion].
// A document of the expected shape outside the range draws an Error-severity
// [diag.E_SNAPSHOT_UNSUPPORTED_VERSION]. Load, Verify, Info, HeaderOnly and
// UpdateMetadata report one of another shape, or holding a string [checkDocument]
// refuses, as E_SNAPSHOT_MALFORMED first. The package doc's Wire Format Versions
// section and docs/API.md's Versioning Policy carry the full policy.
//
// It is DERIVED from currentVersion, not written as a second literal: this
// package reads only the version it writes, and two independent literals let
// a version bump widen the accept range by accident — a reader that silently
// admits the previous format instead of refusing it. Widening the range is a
// deliberate act that replaces this expression, never an edit that forgets to.
const MinReadableVersion = currentVersion

const (
	// Version 4 keys the types table by the declaring schema's NAME. Version 3
	// keyed it by source path, so a document written on one machine failed to
	// load against the same schema text at another path.
	currentVersion = 4

	// currentHashAlgoVersion follows the schema constant structurally: the
	// writer's label and the reader's comparison were two bare literals
	// once, and bumping one alone makes every fresh document self-reject.
	currentHashAlgoVersion = schema.StructuralHashVersion
)

// headerWire is used for decoding .ys headers. All fields are present
// including version and hash algorithm version (which are written as
// literal constants during marshal).
type headerWire struct {
	Version             int               `json:"version"`
	SchemaName          string            `json:"schema_name"`
	SchemaSource        string            `json:"schema_source"`
	SchemaHash          string            `json:"schema_hash"`
	SchemaHashAlgorithm int               `json:"schema_hash_algorithm"`
	IntegrityHash       string            `json:"integrity_hash"`
	Features            []string          `json:"features"`
	CreatedAt           string            `json:"created_at,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
	Attestation         *attestationWire  `json:"attestation,omitempty"`
}

// marshalHeaderWire is used for encoding .ys headers. Version and
// SchemaHashAlgorithm are written as literal parameters in the manual
// byte assembly, not serialized from this struct.
type marshalHeaderWire struct {
	SchemaName    string            `json:"schema_name"`
	SchemaSource  string            `json:"schema_source"`
	SchemaHash    string            `json:"schema_hash"`
	IntegrityHash string            `json:"integrity_hash"`
	Features      []string          `json:"features"`
	CreatedAt     string            `json:"created_at,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	Attestation   *attestationWire  `json:"attestation,omitempty"`
}

// attestationWire is the header's validity claim. Marshal writes it whenever
// the snapshot holds a claim; the pointer and omitempty state the read side —
// a document written from a snapshot with no claim carries no field, and
// UpdateMetadata preserves that absence rather than fabricating a claim.
type attestationWire struct {
	Values       bool `json:"values"`
	Associations bool `json:"associations"`
}

// provenanceWire is the wire representation of instance provenance.
type provenanceWire struct {
	SourceName string `json:"source_name"`
	Path       string `json:"path"`
}

// typeTableEntry is one row of the types table — the document's denotation
// set. Schema plus Name is the identity; every other position references a
// row by index.
//
// Schema is the declaring schema's DECLARED NAME, not its source path. The
// name travels between machines where a path does not, and it is unique
// across an import closure because a load refuses a second schema with an
// existing name. Keying by source path made
// two documents of one schema text — loaded from two directories, or from
// disk and from embedded sources — carry an identical schema_hash and
// mutually unreadable type tables, because
// [github.com/simon-lentz/yammm/schema.StructuralHash] excludes paths by
// design and this table did not.
type typeTableEntry struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
}

// instanceGroupWire is one entry of the instances section: one table row's
// root instances. Present with empty Items means the snapshot holds the
// type with no instances; absent means the snapshot does not hold the type.
type instanceGroupWire struct {
	Type  *int       `json:"type"`
	Items []instWire `json:"items"`
}

// instWire is the wire representation of a single instance. Type is
// required at every composed-child position and absent for a root, whose
// group entry denotes its type; a root carrying one is cross-checked.
type instWire struct {
	Key        []any                 `json:"key"`
	Type       *int                  `json:"type,omitempty"`
	Properties map[string]any        `json:"properties"`
	Edges      map[string][]edgeWire `json:"edges,omitempty"`
	Composed   map[string][]instWire `json:"composed,omitempty"`
	Provenance *provenanceWire       `json:"provenance"`
}

// edgeWire is the wire representation of an edge target. TargetType is a
// nullable row index: a null or absent reference is a malformed document
// and never binds to row 0.
type edgeWire struct {
	TargetType *int           `json:"target_type"`
	TargetKey  []any          `json:"target_key"`
	Properties map[string]any `json:"properties"`
}

// statesNothing reports whether the element is null or holds no field with a
// value. It then states no target key to judge.
func (e edgeWire) statesNothing() bool {
	return e.TargetType == nil && len(e.TargetKey) == 0 && len(e.Properties) == 0
}

// conflictWire is the address of the root a duplicate collided with.
type conflictWire struct {
	Type *int  `json:"type"`
	Key  []any `json:"key"`
}

// dupWire is the wire representation of a duplicate record. Type and Key
// identify the rejected root; Conflict addresses the root it collided with.
// Marshal writes no parent coordinates. The reader decodes them to refuse a
// record that states one, since it reads a member it does not declare as absent
// and would take such a record for a root's. A coordinate that is null or empty
// states none, so it loads and the next Marshal drops it.
type dupWire struct {
	Type       *int          `json:"type"`
	Key        []any         `json:"key"`
	Instance   instWire      `json:"instance"`
	Conflict   *conflictWire `json:"conflict"`
	ParentType *int          `json:"parent_type,omitempty"`
	ParentKey  []any         `json:"parent_key,omitempty"`
	Relation   string        `json:"relation,omitempty"`
}

// statesNothing reports whether the element is null or holds no field with a
// value: each is absent, null or empty, and a conflict or provenance object,
// even an empty one, is a value. That is one break however many facts it leaves
// unstated.
func (d dupWire) statesNothing() bool {
	i := d.Instance
	return d.Type == nil && len(d.Key) == 0 && d.Conflict == nil && d.ParentType == nil && len(d.ParentKey) == 0 && d.Relation == "" &&
		len(i.Key) == 0 && i.Type == nil && len(i.Properties) == 0 && len(i.Edges) == 0 && len(i.Composed) == 0 && i.Provenance == nil
}

// unresolvedWire is the wire representation of an unresolved edge record.
type unresolvedWire struct {
	SourceType *int           `json:"source_type"`
	SourceKey  []any          `json:"source_key"`
	Relation   string         `json:"relation"`
	TargetType *int           `json:"target_type"`
	TargetKey  []any          `json:"target_key"`
	Required   bool           `json:"required"`
	Reason     string         `json:"reason"`
	Properties map[string]any `json:"properties,omitempty"`
}

// statesNothing reports whether the element is null or holds no field with a
// value: each is absent, null or empty, and a required of false is no value, as
// the wire does not tell it from an absent one. That is one break however many
// facts it leaves unstated.
func (u unresolvedWire) statesNothing() bool {
	return u.SourceType == nil && len(u.SourceKey) == 0 && u.Relation == "" && u.TargetType == nil && len(u.TargetKey) == 0 &&
		!u.Required && u.Reason == "" && len(u.Properties) == 0
}

// diagWire is the wire representation of the diagnostics section.
type diagWire struct {
	Duplicates []dupWire        `json:"duplicates"`
	Unresolved []unresolvedWire `json:"unresolved"`
}

// checkDocument holds data to the document's outermost shape, to its member
// names and to its text. Every surface that holds the whole document runs it
// before it decodes the header, so a document one of them accepts is one Load
// decodes as written. A fault it finds draws E_SNAPSHOT_MALFORMED whatever version
// the header states; [HeaderOnlyRead] judges the version first.
func checkDocument(data []byte) error {
	if err := checkTopLevelKeys(data); err != nil {
		return err
	}
	return checkStrings(data)
}

// readString returns the index of the quote that closes the string opening at
// data[i], and whether the string holds an escape. It returns an error for a
// string encoding/json would rewrite: it decodes each byte that is not UTF-8,
// and each surrogate escape that is not half of a pair, as U+FFFD with no error.
// data must be JSON that encoding/json has read whole.
func readString(data []byte, i int) (int, bool, error) {
	end, escaped := i+1, false
	for data[end] != '"' {
		if data[end] == '\\' {
			end++
			escaped = true
		}
		end++
	}
	body := data[i+1 : end]
	if !utf8.Valid(body) {
		return 0, false, fmt.Errorf("the string at byte %d is not valid UTF-8; encoding/json would read each invalid byte as U+FFFD", i)
	}
	if escaped {
		if at := loneSurrogate(body); at >= 0 {
			return 0, false, fmt.Errorf("the string at byte %d holds the surrogate escape %s, which is half of no pair; encoding/json would read it as U+FFFD", i, body[at:at+6])
		}
	}
	return end, escaped, nil
}

// loneSurrogate returns the index in body of the first \uXXXX escape naming a
// UTF-16 surrogate that is not half of a pair, or -1. body is the text of a JSON
// string encoding/json has read, so each \u holds four hex digits.
func loneSurrogate(body []byte) int {
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' {
			continue
		}
		i++
		if body[i] != 'u' {
			continue
		}
		r := escapedRune(body[i+1:])
		start := i - 1
		i += 4
		if !utf16.IsSurrogate(r) {
			continue
		}
		if i+6 < len(body) && body[i+1] == '\\' && body[i+2] == 'u' && utf16.DecodeRune(r, escapedRune(body[i+3:])) != unicode.ReplacementChar {
			i += 6
			continue
		}
		return start
	}
	return -1
}

// escapedRune reads the four hex digits a \u escape holds.
func escapedRune(digits []byte) rune {
	var r rune
	for _, d := range digits[:4] {
		switch {
		case d <= '9':
			r = r<<4 | rune(d-'0')
		case d <= 'F':
			r = r<<4 | rune(d-'A'+10)
		default:
			r = r<<4 | rune(d-'a'+10)
		}
	}
	return r
}

// checkText returns an error for the first string of data that [readString]
// refuses. It is the part of [checkStrings] a reader that holds only a prefix of
// the document can run; the prefix ends between two values.
func checkText(data []byte) error {
	for i := 0; i < len(data); i++ {
		if data[i] != '"' {
			continue
		}
		end, _, err := readString(data, i)
		if err != nil {
			return err
		}
		i = end
	}
	return nil
}

// invalidMetadata refuses the first metadata key, in key order, that is not
// valid UTF-8 or names a value that is not. encoding/json writes each such
// byte as U+FFFD, so the document would read back other text, and two such
// keys as one repeated member.
func invalidMetadata(m map[string]string) error {
	for _, k := range slices.Sorted(maps.Keys(m)) {
		switch {
		case !utf8.ValidString(k):
			return fmt.Errorf("metadata key %s is not valid UTF-8", diag.QuoteValue(k))
		case !utf8.ValidString(m[k]):
			return fmt.Errorf("metadata value of %s is not valid UTF-8", diag.QuoteValue(k))
		}
	}
	return nil
}

// wireShape is how the reader decodes one JSON value of the document: a
// struct's members by their JSON names, or a map's or a slice's element. A nil
// shape decodes into any, whose objects are maps.
type wireShape struct {
	isStruct bool
	names    []string              // a struct's JSON names, in field order
	members  map[string]*wireShape // a struct's, by JSON name
	elem     *wireShape            // a map's or a slice's
}

// documentShape is the document as the reader decodes it, section by section,
// derived from the wire structs, so it cannot drift from them.
var documentShape = func() *wireShape {
	memo := make(map[reflect.Type]*wireShape)
	doc := &wireShape{isStruct: true, members: make(map[string]*wireShape)}
	for _, section := range []struct {
		name string
		typ  reflect.Type
	}{
		{"yammm_snapshot", reflect.TypeFor[headerWire]()},
		{"types", reflect.TypeFor[[]typeTableEntry]()},
		{"instances", reflect.TypeFor[[]instanceGroupWire]()},
		{"diagnostics", reflect.TypeFor[diagWire]()},
	} {
		doc.names = append(doc.names, section.name)
		doc.members[section.name] = shapeOf(section.typ, memo)
	}
	return doc
}()

// shapeOf returns t's shape, memoized so a struct that holds itself through a
// map or a slice (instWire's composed children) terminates.
func shapeOf(t reflect.Type, memo map[reflect.Type]*wireShape) *wireShape {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if s, ok := memo[t]; ok {
		return s
	}
	switch t.Kind() {
	case reflect.Struct:
		s := &wireShape{isStruct: true, members: make(map[string]*wireShape, t.NumField())}
		memo[t] = s
		for f := range t.Fields() {
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			s.names = append(s.names, name)
			s.members[name] = shapeOf(f.Type, memo)
		}
		return s
	case reflect.Map, reflect.Slice:
		s := &wireShape{}
		memo[t] = s
		s.elem = shapeOf(t.Elem(), memo)
		return s
	default:
		return nil
	}
}

// member returns the shape of the value under name in an object of shape s,
// and, when s is a struct that declares no member name and one name differs
// from it only in case, that declared name.
func (s *wireShape) member(name string) (*wireShape, string) {
	switch {
	case s == nil:
		return nil, ""
	case !s.isStruct:
		return s.elem, ""
	}
	if m, ok := s.members[name]; ok {
		return m, ""
	}
	for _, declared := range s.names {
		if strings.EqualFold(declared, name) {
			return nil, declared
		}
	}
	return nil, ""
}

// smallObject is how many member names [checkStrings] compares one by
// one as it reads them; a larger object's names are sorted and compared once,
// when it closes.
const smallObject = 16

// checkStrings returns an error for the first string of data that [readString]
// refuses, and for the first object that repeats a member name, or that decodes
// into a wire struct and holds a name differing from a declared one only in
// case. Names compare as decoded text, so a name spelled with an escape repeats
// the same name without one. data must be JSON that encoding/json has read whole;
// [checkTopLevelKeys] reads it so.
func checkStrings(data []byte) error {
	type frame struct {
		start int        // index in names of an object's first name; -1 for an array
		shape *wireShape // how the container decodes
		next  *wireShape // how the next value in it decodes
	}
	var (
		stack  []frame
		names  [][]byte
		atName bool
	)
	open := func(object bool) {
		shape := documentShape
		if len(stack) > 0 {
			shape = stack[len(stack)-1].next
		}
		f := frame{start: -1, shape: shape}
		if object {
			f.start = len(names)
		} else if shape != nil {
			f.next = shape.elem
		}
		stack = append(stack, f)
		atName = object
	}
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '{':
			open(true)
		case '[':
			open(false)
		case '}', ']':
			top := stack[len(stack)-1]
			if top.start >= 0 {
				if own := names[top.start:]; len(own) > smallObject {
					// The object's own names, sorted in place: the stack drops them next.
					slices.SortFunc(own, bytes.Compare)
					for j := 1; j < len(own); j++ {
						if bytes.Equal(own[j-1], own[j]) {
							return repeatedMemberError(own[j])
						}
					}
				}
				names = names[:top.start]
			}
			stack = stack[:len(stack)-1]
			atName = false
		case ',':
			atName = stack[len(stack)-1].start >= 0
		case '"':
			end, escaped, err := readString(data, i)
			if err != nil {
				return err
			}
			if atName {
				name := data[i+1 : end]
				if escaped {
					var decoded string
					if err := json.Unmarshal(data[i:end+1], &decoded); err != nil {
						return fmt.Errorf("member name %s does not decode: %w", diag.ValueText(string(data[i:end+1])), err)
					}
					name = []byte(decoded)
				}
				top := &stack[len(stack)-1]
				if own := names[top.start:]; len(own) < smallObject && slices.ContainsFunc(own, func(n []byte) bool { return bytes.Equal(n, name) }) {
					return repeatedMemberError(name)
				}
				next, declared := top.shape.member(string(name))
				if declared != "" {
					return fmt.Errorf("an object holds the member %s, which encoding/json reads as the member %q; every member is spelled as the format declares it", diag.QuoteValue(string(name)), declared)
				}
				top.next = next
				names = append(names, name)
				atName = false
			}
			i = end
		}
	}
	return nil
}

func repeatedMemberError(name []byte) error {
	return fmt.Errorf("an object repeats the member %s; every member name occurs once in its object", diag.QuoteValue(string(name)))
}
