package schema

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/simon-lentz/yammm/location"
)

// Registry is a thread-safe registry of compiled schemas.
//
// The registry is append-only by design: once a schema is registered, it cannot
// be removed. This simplifies concurrency guarantees and ensures stable TypeID
// lookups. For hot-reload or LSP scenarios requiring schema replacement, create
// a new Registry or use Clone() to create an isolated snapshot.
//
// The registry provides O(1) lookup by SourceID and schema name.
// It is safe for concurrent use by multiple goroutines.
type Registry struct {
	mu       sync.RWMutex
	schemas  map[location.SourceID]*Schema
	byName   map[string]*Schema
	byTypeID map[TypeID]*Type
	// hashes caches StructuralHash results for registered schemas keyed by
	// SourceID. Populated lazily on the idempotence path (first duplicate-
	// SourceID Register call); never populated for schemas that have not
	// triggered an idempotence check. Keeps the fresh-register path free
	// of hash-cost while the steady-state idempotence-check path pays one
	// StructuralHash on the incoming schema only.
	hashes map[location.SourceID]string
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		schemas:  make(map[location.SourceID]*Schema),
		byName:   make(map[string]*Schema),
		byTypeID: make(map[TypeID]*Type),
		hashes:   make(map[location.SourceID]string),
	}
}

// Register adds a schema to the registry.
//
// Idempotence: when a schema with the same SourceID is already registered AND
// its StructuralHash matches the incoming schema's hash, Register returns nil
// without mutating the registry. This makes the common case — the same .yammm
// file parsed twice in one process (e.g., a shared Registry used across
// multiple Load calls whose schemas share transitive imports) — a safe no-op.
// Re-indexing of the existing schema's types is skipped: pointer identity of
// types returned by LookupType is preserved across idempotent re-registration.
//
// A SourceID match with content divergence still returns a
// *RegistryError{Kind: DuplicateSourceID}. The error message preserves the
// "schema already registered with source ID: <id>" prefix and appends the
// existing and incoming structural hashes so divergent registrations fail
// loudly and diagnosably.
//
// New SourceIDs register normally. Zero SourceID, empty schema name, and
// duplicate name (different SourceID, same name) continue to return their
// respective RegistryError kinds; idempotence does not weaken these checks.
//
// Thread-safety: unchanged. Register acquires the registry's write lock for
// the duration of the check-and-register sequence. StructuralHash is computed
// on the duplicate-SourceID path only and runs under the lock; the happy
// path (fresh SourceID) pays no hash cost.
func (r *Registry) Register(s *Schema) error {
	if s == nil {
		return nil
	}

	// Reject schemas with zero SourceID
	if s.sourceID.IsZero() {
		return &RegistryError{
			Kind:    InvalidSourceID,
			Message: "cannot register schema with zero SourceID",
		}
	}

	// Reject schemas with empty name
	if s.name == "" {
		return &RegistryError{
			Kind:    InvalidName,
			Message: "cannot register schema with empty name",
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Idempotence on duplicate SourceID: same content is a no-op;
	// divergent content still errors with a full hash-diff diagnostic.
	// existing is a non-nil map value; s is non-nil per the top-of-function
	// guard — StructuralHash is safe on both. The existing hash is cached
	// on first use; steady-state re-registrations pay only the incoming
	// schema's hash.
	if existing, ok := r.schemas[s.sourceID]; ok {
		existingHash, cached := r.hashes[s.sourceID]
		if !cached {
			existingHash = StructuralHash(existing)
			r.hashes[s.sourceID] = existingHash
		}
		incomingHash := StructuralHash(s)
		if existingHash == incomingHash {
			return nil
		}
		return &RegistryError{
			Kind: DuplicateSourceID,
			Message: fmt.Sprintf(
				"schema already registered with source ID: %s (content divergence: existing=%s, incoming=%s)",
				s.sourceID.String(), existingHash, incomingHash,
			),
		}
	}

	// Check for duplicate name
	if _, ok := r.byName[s.name]; ok {
		return &RegistryError{
			Kind:    DuplicateName,
			Message: "schema already registered with name: " + s.name,
		}
	}

	// Register schema
	r.schemas[s.sourceID] = s
	r.byName[s.name] = s

	// Index all types for fast TypeID lookup
	for _, t := range s.types {
		r.byTypeID[t.ID()] = t
	}

	return nil
}

// LookupBySourceID returns the schema with the given source ID.
func (r *Registry) LookupBySourceID(id location.SourceID) (*Schema, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	s, ok := r.schemas[id]
	return s, ok
}

// LookupByName returns the schema with the given name.
func (r *Registry) LookupByName(name string) (*Schema, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	s, ok := r.byName[name]
	return s, ok
}

// LookupType returns the type with the given TypeID.
// This provides O(1) cross-schema type lookup.
func (r *Registry) LookupType(id TypeID) (*Type, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.byTypeID[id]
	return t, ok
}

// Contains reports whether a schema with the given source ID is registered.
func (r *Registry) Contains(id location.SourceID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.schemas[id]
	return ok
}

// Len returns the number of registered schemas.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.schemas)
}

// Clone creates a shallow copy of the registry.
// The clone shares the same Schema pointers but has independent maps.
// This is useful for creating isolated registry views without affecting the original.
func (r *Registry) Clone() *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	clone := &Registry{
		schemas:  make(map[location.SourceID]*Schema, len(r.schemas)),
		byName:   make(map[string]*Schema, len(r.byName)),
		byTypeID: make(map[TypeID]*Type, len(r.byTypeID)),
		hashes:   make(map[location.SourceID]string, len(r.hashes)),
	}

	maps.Copy(clone.schemas, r.schemas)
	maps.Copy(clone.byName, r.byName)
	maps.Copy(clone.byTypeID, r.byTypeID)
	maps.Copy(clone.hashes, r.hashes)

	return clone
}

// All returns all registered schemas in deterministic order (sorted by SourceID).
// The returned slice is a copy; modifications do not affect the registry.
func (r *Registry) All() []*Schema {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Schema, 0, len(r.schemas))
	for _, s := range r.schemas {
		result = append(result, s)
	}
	slices.SortFunc(result, func(a, b *Schema) int {
		return cmp.Compare(a.sourceID.String(), b.sourceID.String())
	})
	return result
}

// RegistryErrorKind identifies the type of registry error.
type RegistryErrorKind uint8

const (
	// DuplicateSourceID indicates a schema with the same SourceID is already registered.
	DuplicateSourceID RegistryErrorKind = iota
	// DuplicateName indicates a schema with the same name is already registered.
	DuplicateName
	// InvalidSourceID indicates a schema has an invalid (e.g., zero) SourceID.
	InvalidSourceID
	// InvalidName indicates a schema has an invalid (e.g., empty) name.
	InvalidName
)

// String returns a human-readable name for the error kind.
func (k RegistryErrorKind) String() string {
	switch k {
	case DuplicateSourceID:
		return "duplicate source ID"
	case DuplicateName:
		return "duplicate name"
	case InvalidSourceID:
		return "invalid source ID"
	case InvalidName:
		return "invalid name"
	default:
		return "unknown"
	}
}

// RegistryError represents an error from registry operations.
type RegistryError struct {
	Kind    RegistryErrorKind
	Message string
}

// Error implements the error interface.
func (e *RegistryError) Error() string {
	return e.Message
}
