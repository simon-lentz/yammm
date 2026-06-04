# yammm Versioning Policy

This document codifies yammm's versioning commitments — both API and wire-format — so consumers and contributors have one authoritative reference for what a given release bump is permitted to change. Added in v0.3.0 release prep.

## Scope

yammm's versioning covers two surfaces that evolve on related but distinct cadences:

1. **Go API surface** — exported types, functions, methods, constants, options, and their documented behavior under the `github.com/simon-lentz/yammm` module.
2. **`.ys` wire format** — the on-disk shape of snapshots produced by `snapshot.Marshal` and consumed by `snapshot.Load` / `snapshot.Info` / `snapshot.HeaderOnly` / `snapshot.Verify`. The wire format carries its own `version` field in the header (currently `2` as of v0.3.0).

The two surfaces share a semver tag but have independent version-bump triggers, documented below.

## Pre-1.0 policy (current)

yammm is currently pre-1.0. Under Go module semver conventions, `0.X.Y` releases are understood to be unstable — backward-incompatible changes are permitted at minor-version bumps — but yammm's own policy is narrower than that. The following rules apply:

### Minor releases (`v0.X.0 → v0.X+1.0`) may include:

- **Additive API changes** — new exported symbols, new option types, new methods on existing types, new struct fields added at the end of a struct (subject to the type's own versioning contract, if any). Consumers written against the prior release continue to compile and run unchanged.
- **Subtractive and breaking API changes** — removal of exported symbols, and breaking changes to an exported symbol's signature, name, or type (e.g. changing a method's return type, renaming an exported function, or collapsing two carrier types into one), provided (a) a grep across the known consumer set returns **either** zero external Go references **or** external references confined to a consumer that pins an older yammm release and whose migration is tracked as a downstream backlog item — in which case the semver pin carries the coordination, so tagging the breaking release cannot break that consumer's build and the migration lands on the consumer's own schedule; (b) the change is explicitly documented in the release notes; and (c) the release-notes entry enumerates every removed, renamed, or signature-changed symbol for downstream auditing.
- **Behavior tightenings** — changes where the only observable difference is that a previously-erroring call becomes a no-op for input that was already semantically equivalent to the stored state, or similar correctness-preserving narrowings (e.g., §7's `Registry.Register` becoming idempotent for exact-`SourceID` + exact-`StructuralHash` re-registration in v0.3.0). Documented in the release notes with the specific contract change.
- **Wire-format additions** — new optional fields added under the `omitempty` + header `features`-signal convention, provided the new field's absence in an older reader does not cause silent data loss. A v0.X reader consuming a v0.X+1-produced document skips the unknown field; a v0.X+1 reader consuming a v0.X-produced document reads missing fields as their zero value.

### Wire-format-incompatible changes require a format-version bump:

Any wire-format change whose absence in an older reader would cause **silent data loss** is required to:

1. **Increment the header's `version` field** (e.g., `1` → `2`).
2. **Rely on yammm's existing unknown-version-rejection semantics** — older readers encounter the new version and return Fatal `E_SNAPSHOT_UNSUPPORTED_VERSION` rather than partial-parsing a document they do not fully understand.
3. **Document the bump and the asymmetric-reader semantics** in the release notes: which older versions accept the new file (if any — typically none), which newer versions accept the older file (typically the immediately-prior N versions, losslessly).

The canonical example is v0.3.0's §6 `UnresolvedEdge.Properties` addition: the new wire field cannot be `omitempty`-safe alone because a pre-v0.3.0 reader would silently drop edge properties on cross-batch unresolved edges. The v1 → v2 bump forces pre-v0.3.0 readers to reject cleanly; v0.3.0+ readers accept both v1 and v2 files (v1 files have no Properties to drop, so the read is lossless).

Wire-format version bumps are permitted at minor-version bumps during pre-1.0 (they are the yammm-native signal for "this format has diverged"); they are not deferred to major bumps under pre-1.0.

### Patch releases (`v0.X.Y → v0.X.Y+1`) are reserved for:

- Bug fixes that preserve the public API and wire format.
- Documentation-only changes.
- Internal refactors, test improvements, and dependency updates.

Patch releases never introduce new exported symbols, change documented behavior, or touch the wire format.

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
- **One wire-format version bump** — `.ys` v1 → v2 for §6's `UnresolvedEdge.Properties` addition. Required because the field is non-`omitempty`-safe at v1 readers; the bump is paired with the existing unknown-version-rejection path so v0.2.x binaries error cleanly on v0.3.0-written documents rather than round-tripping corrupted data. Release notes document both the bump and the asymmetric-reader semantics (v2 accepts v1 losslessly; v1 rejects v2 cleanly).

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

## Revision history

- **2026-04-17** — Initial document, added as part of v0.3.0 release prep.
- **2026-06-02** — Broadened the pre-1.0 minor-release rule from "removal of exported symbols" to also cover breaking signature, name, and type changes, and added a version-pinned-consumer carve-out to condition (a): a breaking change is permitted when the only external references live in a consumer pinned to an older yammm release whose migration is tracked downstream. Motivated by the v0.4.0 `diag` surface tightening, whose sole external consumer pins `v0.3.1` and migrates on its own schedule.
