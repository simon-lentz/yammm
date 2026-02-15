// Package complete implements schema completion - the phase where parsed ASTs
// are transformed into fully-resolved, validated Schema objects.
//
// Schema completion performs:
//   - Type and data type indexing with duplicate detection
//   - Import validation against alias rules and the schema registry
//   - Alias constraint resolution against local and imported data types
//   - Cycle detection in inheritance hierarchies
//   - Inheritance linearization (DFS left-to-right, keep-first deduplication)
//   - Property and relation collision detection (case-insensitive, normalized names)
//   - Relation target validation against the type index and registry
//   - Reserved prefix rejection (_target_ prefix)
//
// # Contracts
//
// The completion process enforces several behavioral contracts:
//   - No UUID placeholders or namespace logic
//   - No implicit ID injection
//   - No PluralName computation
//
// # Linearization Algorithm
//
// Inheritance is linearized using DFS left-to-right traversal with keep-first
// deduplication for diamond inheritance. This ensures consistent property and
// relation ordering across schema loads.
//
// # Collision Detection
//
// Properties and relations are checked for collisions:
//   - Case-insensitive property name collision (E_CASE_COLLISION)
//   - Relation name collision after lower_snake normalization
//   - Property-relation name collision (E_PROPERTY_RELATION_COLLISION)
//   - Reserved _target_ prefix rejection (E_RESERVED_PREFIX)
package complete
