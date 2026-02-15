package schema

import (
	"cmp"
	"maps"
	"slices"
	"sync"

	"github.com/simon-lentz/yammm/location"
)

// LookupStatus indicates the result of a registry lookup.
type LookupStatus uint8

const (
	// LookupNotFound indicates the item was not found.
	LookupNotFound LookupStatus = iota
	// LookupFound indicates the item was found.
	LookupFound
)

// Found reports whether the lookup succeeded.
func (s LookupStatus) Found() bool {
	return s == LookupFound
}

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
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		schemas:  make(map[location.SourceID]*Schema),
		byName:   make(map[string]*Schema),
		byTypeID: make(map[TypeID]*Type),
	}
}

// Register adds a schema to the registry.
// Returns an error if the schema has a zero SourceID, empty name, or if a schema
// with the same SourceID or name is already registered.
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

	// Check for duplicate SourceID
	if _, ok := r.schemas[s.sourceID]; ok {
		return &RegistryError{
			Kind:    DuplicateSourceID,
			Message: "schema already registered with source ID: " + s.sourceID.String(),
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
func (r *Registry) LookupBySourceID(id location.SourceID) (*Schema, LookupStatus) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	s, ok := r.schemas[id]
	if !ok {
		return nil, LookupNotFound
	}
	return s, LookupFound
}

// LookupByName returns the schema with the given name.
func (r *Registry) LookupByName(name string) (*Schema, LookupStatus) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	s, ok := r.byName[name]
	if !ok {
		return nil, LookupNotFound
	}
	return s, LookupFound
}

// LookupType returns the type with the given TypeID.
// This provides O(1) cross-schema type lookup.
func (r *Registry) LookupType(id TypeID) (*Type, LookupStatus) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.byTypeID[id]
	if !ok {
		return nil, LookupNotFound
	}
	return t, LookupFound
}

// LookupSchema returns the schema containing the type with the given TypeID.
func (r *Registry) LookupSchema(id TypeID) (*Schema, LookupStatus) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	s, ok := r.schemas[id.schemaPath]
	if !ok {
		return nil, LookupNotFound
	}
	return s, LookupFound
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
	}

	maps.Copy(clone.schemas, r.schemas)
	maps.Copy(clone.byName, r.byName)
	maps.Copy(clone.byTypeID, r.byTypeID)

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
