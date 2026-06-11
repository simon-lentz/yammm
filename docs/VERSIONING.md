# yammm Versioning Policy

This document codifies yammm's versioning commitments — both API and wire-format — so consumers and contributors have one authoritative reference for what a given release bump is permitted to change. Added in v0.3.0 release prep.

## Scope

yammm's versioning covers two surfaces that evolve on related but distinct cadences:

1. **Go API surface** — exported types, functions, methods, constants, options, and their documented behavior under the `github.com/simon-lentz/yammm` module.
2. **`.ys` wire format** — the on-disk shape of snapshots produced by `snapshot.Marshal` and consumed by `snapshot.Load` / `snapshot.Info` / `snapshot.HeaderOnly` / `snapshot.Verify`. The wire format carries its own `version` field in the header (currently `2` as of v0.3.0).
3. **Generated Go output (`adapter/gogen`)** — the shape of the Go source `gogen.Marshal` emits for a given schema and options. Promoted to a committed surface in v0.5.2 (the v0.5.0 notes anticipated promotion "if a downstream consumer comes to depend on that shape"; rdata now byte-pins committed generated packages against it). Its tier rules are in "Generated-output surface" below.

The surfaces share a semver tag but have independent version-bump triggers, documented below.

## Pre-1.0 policy (current)

yammm is currently pre-1.0. Under Go module semver conventions, `0.X.Y` releases are understood to be unstable — backward-incompatible changes are permitted at minor-version bumps — but yammm's own policy is narrower than that. The following rules apply:

### Minor releases (`v0.X.0 → v0.X+1.0`) may include:

- **Additive API changes** — new exported symbols, new option types, new methods on existing types, new struct fields added at the end of a struct (subject to the type's own versioning contract, if any). Consumers written against the prior release continue to compile and run unchanged.
- **Subtractive and breaking API changes** — removal of exported symbols, and breaking changes to an exported symbol's signature, name, or type (e.g. changing a method's return type, renaming an exported function, or collapsing two carrier types into one), provided (a) a grep across the known consumer set returns **either** zero external Go references **or** external references confined to a consumer that pins an older yammm release and whose migration is tracked as a downstream backlog item — in which case the semver pin carries the coordination, so tagging the breaking release cannot break that consumer's build and the migration lands on the consumer's own schedule; (b) the change is explicitly documented in the release notes; and (c) the release-notes entry enumerates every removed, renamed, or signature-changed symbol for downstream auditing.
- **Behavior tightenings** — changes where the only observable difference is that a previously-erroring call becomes a no-op for input that was already semantically equivalent to the stored state, or similar correctness-preserving narrowings (e.g., `Registry.Register` becoming idempotent for exact-`SourceID` + exact-`StructuralHash` re-registration in v0.3.0). Documented in the release notes with the specific contract change.
- **Wire-format additions** — new optional fields added under the `omitempty` + header `features`-signal convention, provided the new field's absence in an older reader does not cause silent data loss. A v0.X reader consuming a v0.X+1-produced document skips the unknown field; a v0.X+1 reader consuming a v0.X-produced document reads missing fields as their zero value.

### Wire-format-incompatible changes require a format-version bump:

Any wire-format change whose absence in an older reader would cause **silent data loss** is required to:

1. **Increment the header's `version` field** (e.g., `1` → `2`).
2. **Rely on yammm's existing unknown-version-rejection semantics** — older readers encounter the new version and return Fatal `E_SNAPSHOT_UNSUPPORTED_VERSION` rather than partial-parsing a document they do not fully understand.
3. **Document the bump and the asymmetric-reader semantics** in the release notes: which older versions accept the new file (if any — typically none), which newer versions accept the older file (typically the immediately-prior N versions, losslessly).

The canonical example is v0.3.0's `UnresolvedEdge.Properties` addition: the new wire field cannot be `omitempty`-safe alone because a pre-v0.3.0 reader would silently drop edge properties on cross-batch unresolved edges. The v1 → v2 bump forces pre-v0.3.0 readers to reject cleanly; v0.3.0+ readers accept both v1 and v2 files (v1 files have no Properties to drop, so the read is lossless).

Wire-format version bumps are permitted at minor-version bumps during pre-1.0 (they are the yammm-native signal for "this format has diverged"); they are not deferred to major bumps under pre-1.0.

### Patch releases (`v0.X.Y → v0.X.Y+1`) are reserved for:

- Bug fixes that preserve the public API and wire format.
- Documentation-only changes.
- Internal refactors, test improvements, and dependency updates.
- **Additive API changes** — new exported symbols whose addition leaves every existing signature and documented behavior unchanged; consumers written against the prior release continue to compile and run unchanged.

Patch releases never introduce breaking changes: no removed or renamed exported symbols, no signature changes, no tightenings of documented behavior, and no wire-format changes. Anything in those categories requires at least a minor release.

## Generated-output surface (`adapter/gogen`)

For an unchanged schema, unchanged `Marshal` options, and an unchanged yammm version, generated output is byte-identical (all walks and name assignments are deterministic). Across versions, changes to the output classify as:

### Breaking (major post-1.0; pre-1.0 minor under the standard subtractive rules):

- Renaming or removing any emitted identifier derived from an unchanged schema: struct names, field names, named DataType/enum types and their value constants, `EDGE_` struct names, `Graph` fields, or the `SerializedModel` / `SerializedModelEntry` / `SchemaHash` declarations.
- Changing the type-mapping table (e.g. `Integer` → anything other than `int64`), the pointer-optionality rules, the json-tag shape, or the `Where`-block contract of `EDGE_` structs.
- Changing the `SerializedModel` re-load semantics (the documented `LoadString` / `LoadSourcesWithEntry`-with-`"."` recipes) or the meaning of `SchemaHash` (`schema.StructuralHash`).

### Additive / behavioral (minor or patch, release-noted):

- New emitted declarations in support of new schema features.
- Identifier derivation for new inputs (e.g. additions to the default initialisms set) — existing identifiers for existing inputs must not change.
- Byte-level changes that rename no identifier and change no semantics (comment text inside the generated file, formatting). Consumers that byte-pin via drift gates will see these and regenerate; the release notes must call them out.

### Explicitly uncommitted:

- Doc-comment prose inside generated files.
- Declaration order beyond "deterministic for a given yammm version".

The golden corpus under `adapter/gogen/testdata/` is this contract's executable form: a golden may change only in a release whose notes account for the change under the tiers above.

## Post-1.0 policy (future)

After the `v1.0.0` release, yammm commits to the stricter semver contract that consumers expect from a stable library:

### Major releases (`vN.X.Y → vN+1.0.0`) are required for any change that:

- Removes an exported symbol.
- Changes an exported symbol's signature (parameter types, return types, method set).
- Tightens an exported contract in any way that changes observable behavior (errors become silent, nil returns become non-nil, success paths change their side-effect shape).
- Bumps the `.ys` wire format version.
- Introduces a new required field, new required option, or any other change that forces consumers to update call sites.

### Minor releases (`vN.X.Y → vN.X+1.0`) are for strictly additive changes:

- New exported symbols (types, functions, methods, constants).
- New options that default to pre-existing behavior when omitted.
- New wire-format fields with `omitempty` semantics that do not trigger silent data loss on older readers.
- Documentation, test, and internal-refactor improvements that preserve the public API surface.

### Patch releases follow pre-1.0 patch-release rules verbatim.

## v0.3.0 under this policy

The v0.3.0 release is the first to explicitly apply this policy. v0.3.0 carries:

- **Two subtractive API removals** — `instance.Builder` (plus companion `EdgeBuilder`, `NewInstance`) and the `graph/walk` package. Justified under pre-1.0's "removal with documented no-consumers" rule. Release notes enumerate both; both removals verified against yammm itself (including the CLI binary at `cmd/yammm/`), the `lsp/` subpackage, and the known downstream consumer as of 2026-04-17.
- **One behavior tightening** — `schema.Registry.Register` becomes idempotent for exact `(SourceID, StructuralHash)` re-registration, paired with a loader-side cross-`Load` short-circuit. Justified under the "correctness-preserving tightening" rule. Release notes document both the contract and the paired loader change.
- **One wire-format version bump** — `.ys` v1 → v2 for the `UnresolvedEdge.Properties` addition. Required because the field is non-`omitempty`-safe at v1 readers; the bump is paired with the existing unknown-version-rejection path so v0.2.x binaries error cleanly on v0.3.0-written documents rather than round-tripping corrupted data. Release notes document both the bump and the asymmetric-reader semantics (v2 accepts v1 losslessly; v1 rejects v2 cleanly).

All three changes are documented inline in the v0.3.0 release notes with pointers back to this document's relevant policy sections.

## v0.4.0 under this policy

v0.4.0 bundles two independent, separately-reviewable streams, co-tagged once both land:

- **Additive coercion surface (`adapter/neo4j`)** — new exported `Coerce`, `CoerceParams`, `ParamTypes`, and `ParamTypesForType`, with the node and edge property paths routed through the single chokepoint internally. Strictly additive (new exported symbols + internal rewire); code written against `v0.3.x` compiles and runs unchanged. Justified under the pre-1.0 "additive API changes" rule.
- **Breaking `diag` surface tightening** — three classes of change:
  - *Seven removals* — `Result.IssuesSlice`, `ErrorsSlice`, `WarningsSlice`, `BySeveritySlice`, `IssuesAtLeastAsSevereAsSlice`, `Messages`, and `MessagesAtOrAbove` (each replaced by the corresponding iterator with `slices.Collect`, or `Result.String()` / `Renderer` for message formatting).
  - *One signature change* — `Result.WithContext(tag)` now returns `error` (nil when OK) rather than a `ResultWithContext` value.
  - *Type fold + rename* — `ResultWithContext` and `ErrorWithContext` collapse into a single `*ContextualError`, and `AsResultWithContext` becomes `AsContextualError` (returning `(*ContextualError, bool)`). Consequence in `graph`: `BatchAssembler.Add` / `AddValid` / `Finalize` now return `*diag.ContextualError` (was `*diag.ErrorWithContext`).

  One addition rides the same release: `diag.Collect(issues ...Issue) Result`.

  Justified under the **amended pre-1.0 rule (a)** (broadened 2026-06-02): there are zero external uncoordinated callers — every in-repo user (the two `graph` call sites and the ~115 test call sites across six packages that used the removed accessors) was updated in-tree, and the sole external consumer pins `v0.3.1` with its migration tracked as a downstream backlog item, so tagging `v0.4.0` cannot break its build. Per conditions (b) and (c), the release notes enumerate every removed, renamed, and signature-changed symbol.

The `.ys` wire format is unchanged at v0.4.0 (header `version` stays `2`). Both streams are documented inline in the v0.4.0 release notes with pointers back to this document's relevant policy sections.

## v0.5.0 under this policy

v0.5.0 bundles additive feature work (Go source generation, internal dispatch hardening) with one **breaking contract tightening** (schema-level primary-key enforcement). The Go API symbol surface and the `.ys` wire format are unchanged — code written against `v0.4.x` compiles and runs unchanged — but the **schema-loading contract is tightened**: a schema that loaded cleanly under `v0.4.x` can now be rejected (see the primary-key-enforcement bullet).

- **Additive API surface (`adapter/gogen`)** — a new leaf adapter package exporting `Marshal(s *schema.Schema, opts ...Option) ([]byte, error)`, the `Option` type, and the `WithPackageName` / `WithInitialisms` options. It generates Go source from a schema (one struct per type, named Enum/DataType types, owner-qualified `EDGE_` association structs, a `Graph` aggregate, and an embedded `SerializedModel`) and is schema-in, bytes-out — it imports the Primary API and is never imported back, consistent with the documented adapter-layer discipline. Strictly additive. Justified under the pre-1.0 "additive API changes" rule.
- **Additive CLI surface** — a new `yammm gen --to go <schema.yammm>` command (`cmd/yammm`), sibling to `export`, with `--package`, `--output`, and `--initialisms` flags. The CLI binary is outside the two formally-versioned surfaces (Go API + `.ys` wire format); the addition is backward-compatible (no existing command changes), noted here for completeness.
- **Internal hardening (no API impact)** — the `//exhaustive:enforce` rollout to the seven remaining `schema.ConstraintKind` dispatch switches (so a newly-added kind fails CI rather than reaching a backend un-handled), and the new `internal/ident.ToUpperCamelInitialisms` mechanism that `adapter/gogen` reuses. Both are internal — `internal/*` carries no compatibility guarantee — and change no exported behavior.
- **Breaking schema-loading tightening (primary-key enforcement + extends resolution)** — schema completion now rejects three schema shapes that loaded cleanly under `v0.4.x` via `schema.Load` / `LoadString` / `LoadSources`: (1) every **concrete** (non-abstract, non-part) type must declare or inherit at least one `primary` field, else `E_NO_PRIMARY_KEY`; and (2) an **association** (`-->`) must target a **concrete** type — an abstract target is now rejected with `E_INVALID_ASSOCIATION_TARGET` (a part target already was); and (3) a **concrete** type whose `extends` clause names a **qualified** supertype that does not resolve — an undefined import alias, or a type absent from the imported schema — is now rejected with `E_UNKNOWN_TYPE` when the load supplies a registry (every source-backed load does); previously such a reference was silently deferred, dropping all inheritance from that supertype with no diagnostic (an unqualified unknown supertype already errored). For (1) and (2), a node needs identity to enter a graph or be addressed by an edge, so both checks are hoisted from graph-construction / generation time to load time; (3) brings qualified `extends` resolution in line with the existing unqualified-supertype and relation-target handling. This tightens an exported contract (the schema-loading success path now errors for inputs it previously accepted) — normally **major-requiring**, but permitted in this pre-1.0 minor under the **amended rule (a)**: the affected inputs are `.yammm` schemas, and the sole external consumer (rdata) pins `v0.3.1` and migrates on its own schedule, so tagging `v0.5.0` cannot break its build. Per conditions (b)/(c), the release notes enumerate the three newly-rejected schema shapes and their diagnostic codes. (Composite primary keys — multiple `primary` fields — are **not** new; they were always supported, and v0.5.0 only corrects documentation that wrongly said "exactly one", which is not a breaking change.)

The `.ys` wire format is unchanged at v0.5.0 (header `version` stays `2`); `adapter/gogen` neither reads nor writes `.ys` (its embedded `SerializedModel` is verbatim `.yammm` source, re-loadable via `schema.LoadString` / `LoadSourcesWithEntry`). The shape of gogen's generated Go output (struct layout, `EDGE_` / `Graph` naming, `SerializedModel` form) is pinned by the package's golden corpus but is **not** yet a formally versioned surface under this policy; if a downstream consumer comes to depend on that shape, a future revision can promote it to a committed surface. The additive surface is documented inline in the v0.5.0 release notes with pointers back to this document's relevant policy sections.

## v0.5.1 under this policy

v0.5.1 carries one strictly additive API change and its documentation:

- **Additive API surface (`graph`)** — one new exported constructor, `graph.NewBatchAssemblerFromSnapshot(ctx, s, snap, opts ...BatchAssemblerOption)`, which constructs a `BatchAssembler` whose underlying graph starts pre-populated from an existing `*graph.Snapshot` (the same import semantics as `graph.NewFromSnapshot`) instead of empty. Requested by rdata so its resume pipelines can adopt `BatchAssembler` by seeding from a prior `.ys` snapshot rather than re-adding every previously-extracted instance. No existing signature, option, or documented behavior changes; code written against `v0.5.0` compiles and runs unchanged. Justified under the pre-1.0 patch-release "additive API changes" rule (amended 2026-06-09: patch releases may carry additive API surface and never introduce breaking changes).

The `.ys` wire format is unchanged at v0.5.1 (header `version` stays `2`).

## v0.5.2 under this policy

v0.5.2 closes the two rdata-filed gogen asks (the yammm-side counterpart of rdata's adoption record; see `.claude/plans/2026-06/june_1_checkpoint.md` Follow-Up — 2026-06-10):

- **Bug fix (restores documented behavior) — module-root-relative `SerializedModel` keys.** `adapter/gogen`'s package documentation has always promised module-root-relative embedded keys and a re-load that "needs no filesystem"; the implementation relativized against the entry schema's directory and silently fell back to cwd-relative disk reads when the load's module root differed (registry-style layouts: `schema.Load` + `WithModuleRoot(repoRoot)`). Keys now derive from the load's recorded module root (`Schema.ModuleRoot()`), so they match the import statements inside the sources and the embedded model re-loads hermetically from any working directory. `Marshal`'s round-trip self-check now runs with `WithSourcesOnly`, so a key regression fails generation loudly instead of being rescued by on-disk files. Generated bytes change only for module-root ≠ entry-dir loads (the shapes that were previously self-contradictory) plus one added comment line in the multi-source re-load instructions; the pre-existing golden corpus is otherwise byte-identical. Classified as a patch-tier bug fix: the affected surface was not yet committed (promotion lands in this same release, with the corrected keys as its baseline), and the documented contract is what the fix restores.
- **Bug fix — cross-Load registry cache hits now carry their sources.** A shared-`WithRegistry` load whose import short-circuited to a registry-cached schema produced a `Schema` whose `Sources()` lacked the cached import's content — breaking closure-content consumers (gogen's embedded model; cross-import diagnostics rendering). The cache-hit path now copies the cached schema's transitive closure content into the new load's source registry.
- **Additive API surface (`schema`)** — `Schema.ModuleRoot()` (the canonicalized module root the load resolved module-style imports against; `""` when none was in play) and the `WithSourcesOnly()` load option (import resolution restricted to pre-registered in-memory sources; a miss is `E_IMPORT_RESOLVE`, never a filesystem read). Justified under the amended pre-1.0 patch rule (additive exported symbols; nothing existing changes).
- **Additive CLI surface** — `yammm gen --module-root <dir>` resolves module-style imports against a root other than the schema's own directory; without it, registry-layout schemas could not be generated by the CLI at all.
- **Surface promotion** — generated Go output is now the third formally versioned surface (Scope item 3 + the "Generated-output surface" tier rules above). A documentation/policy commitment; precedent for landing a policy edit in a patch is v0.5.1's amendment of the patch rule itself.

The `.ys` wire format is unchanged at v0.5.2 (header `version` stays `2`).

## Revision history## Revision history

- **2026-04-17** — Initial document, added as part of v0.3.0 release prep.
- **2026-06-02** — Broadened the pre-1.0 minor-release rule from "removal of exported symbols" to also cover breaking signature, name, and type changes, and added a version-pinned-consumer carve-out to condition (a): a breaking change is permitted when the only external references live in a consumer pinned to an older yammm release whose migration is tracked downstream. Motivated by the v0.4.0 `diag` surface tightening, whose sole external consumer pins `v0.3.1` and migrates on its own schedule.
- **2026-06-05** — Added the "v0.5.0 under this policy" section for the additive `adapter/gogen` Go-source-generation surface and the `yammm gen` CLI command (plus the internal `ConstraintKind` exhaustiveness rollout and `internal/ident` initialisms mechanism). No existing API or wire-format commitment changed.
- **2026-06-08** — Folded the schema-level primary-key enforcement into v0.5.0, making it a **breaking** release: the schema-loading contract is tightened (concrete types must declare or inherit a `primary` — `E_NO_PRIMARY_KEY`; associations must target concrete types — `E_INVALID_ASSOCIATION_TARGET`; and a concrete type extending an unresolvable qualified supertype is reported — `E_UNKNOWN_TYPE` — instead of silently dropping the inheritance), so schemas valid under `v0.4.x` can be rejected. The Go API symbol surface and the `.ys` wire format remain unchanged. Justified under the amended pre-1.0 rule (a) — the sole external consumer (rdata) pins `v0.3.1` and migrates downstream; the newly-rejected schema shapes are enumerated in the v0.5.0 section. The section's opening "additive feature release" framing was revised accordingly.
- **2026-06-10** — Added the "v0.5.2 under this policy" section (module-root-aware `SerializedModel` keys + hermetic round-trip; the registry cache-hit sources fix; `Schema.ModuleRoot()` + `WithSourcesOnly()`; `yammm gen --module-root`), **promoted generated Go output to the third formally versioned surface** (Scope item 3 + the "Generated-output surface" tier rules), and recorded both as the close-out of the 2026-06-10 rdata Follow-Up asks.
- **2026-06-09** — Added the "v0.5.1 under this policy" section for the additive `graph.NewBatchAssemblerFromSnapshot` snapshot-seeding constructor (rdata-requested), and **amended the pre-1.0 patch-release rule**: patch releases may now carry additive API changes (new exported symbols that leave every existing signature and documented behavior unchanged); the invariant is that patch releases never introduce breaking changes (no removals, renames, signature changes, behavior tightenings, or wire-format changes). The rule previously read "patch releases never introduce new exported symbols," which would have classified v0.5.1's single additive constructor as minor-tier; an interim classification note recording that deviation was removed when the amendment landed. The post-1.0 section's "patch releases follow pre-1.0 patch-release rules verbatim" inherits this amendment.
