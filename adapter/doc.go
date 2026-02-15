// Package adapter provides format-specific adapters for parsing instance data
// into [instance.RawInstance] values. Each adapter subpackage handles a specific
// data format (JSON, CSV, etc.) and may have its own external dependencies.
//
// # Architectural Boundary
//
// Adapters live at the outermost tier of the module. This design provides:
//
//   - Dependency hygiene via import granularity: Go modules are granular at the
//     import level. Consumers who import only schema and instance do not
//     transitively depend on tidwall/jsonc. Adapter dependencies are pulled only
//     when adapter/json is imported.
//
//   - Clear library/consumer boundary: The adapter package explicitly imports
//     the library to use it, mirroring how downstream consumers structure their
//     own adapters.
//
//   - Extensibility signal: Users see adapter/json and understand they can
//     create adapter/myformat using the same pattern.
//
// # Dependency Direction
//
// Adapters depend on library packages; library packages never depend on adapters:
//
//	adapter/json  ──imports──▶  instance
//	adapter/json  ──imports──▶  diag
//	adapter/json  ──imports──▶  location (for PositionRegistry interface)
//
// # Layering Discipline
//
// The adapter package does not import internal/* packages. This maintains a
// clean separation between core library internals and the adapter layer.
//
// # Subpackages
//
//   - [json]: JSON adapter with optional location tracking and JSONC support
package adapter
