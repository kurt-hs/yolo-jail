---
title: "Planning notes: explicit jail inheritance exclusions"
status: draft
stage: SKETCH
next: "Resolve the design questions; after the design is accepted, have a planner re-read the tree and complete an implementation plan before any build"
depends-on:
  - jail-inheritance-exclusions.md#OQ-IH2
  - jail-inheritance-exclusions.md#OQ-IH3
summary: "Provisional tree-survey notes for design discussion; not an implementation brief and not safe to build from."
---

# Planning notes: explicit jail inheritance exclusions

**Status:** 2026-10-21. These survey notes were recorded against `bd86fc29d69e5043706170e33bb111f4a8fb13e4` while the design questions remain open. They are not a completed implementation plan.

**Design:** [`jail-inheritance-exclusions.md`](jail-inheritance-exclusions.md). It alone defines behavior. These notes preserve possible code seams and coverage areas for discussion; they are provisional, and the tree may have moved. Nothing here authorizes implementation.

## Surveyed map — candidate touchpoints

These paths and descriptions record what the earlier survey identified, not a definitive change list. A planner must inspect the current tree and decide whether each remains relevant after design acceptance.

| Path | Possible area to reassess |
| :--- | :--- |
| [`internal/config/config.go`](../../internal/config/config.go) | Whether the accepted config shape needs a schema entry. |
| [`internal/config/validate.go`](../../internal/config/validate.go) | Validation seam for user-only scope, pointer syntax, target status, and ownership behavior. |
| New `internal/config/hostimportexclusions.go` | Candidate location for resolving and normalizing user-scope policy. |
| [`internal/config/inherit.go`](../../internal/config/inherit.go) | Inheritance classification seam for preflight and nested snapshots. |
| New `internal/config/hostimportexclusions_test.go` | Candidate coverage for config shape, scope, target status, and ownership behavior. |
| [`internal/config/inherit_test.go`](../../internal/config/inherit_test.go) | Candidate coverage for user-snapshot propagation and workspace exclusion. |
| New `internal/agentcfg/hostimportexclusions.go` | Candidate shared transformation location using the surface codec and `internal/jsonptr`. |
| New `internal/agentcfg/hostimportexclusions_test.go` | Candidate coverage for pointer semantics, unmatched pointers, immutability, determinism, and codec failures. |
| [`internal/entrypoint/hostlayerlabel.go`](../../internal/entrypoint/hostlayerlabel.go) | Existing host-layer wire / launch-frozen policy seam to re-evaluate. |
| [`internal/cli/run/packhostgrants.go`](../../internal/cli/run/packhostgrants.go) | Container launch policy resolution and report seam to re-evaluate. |
| [`internal/cli/run/macosctxtree.go`](../../internal/cli/run/macosctxtree.go) | macos-user source-copy and provenance seam to re-evaluate. |
| [`internal/macosuser/runplan.go`](../../internal/macosuser/runplan.go) | macos-user host-context delivery seam to re-evaluate. |
| [`internal/entrypoint/packsurfaces.go`](../../internal/entrypoint/packsurfaces.go) | Host-layer composition seam to re-evaluate against the final design. |
| [`internal/cli/config.go`](../../internal/cli/config.go) | Staged-source preview seam to re-evaluate. |
| [`internal/entrypoint/surfaceloop_test.go`](../../internal/entrypoint/surfaceloop_test.go) | Candidate coverage for filtering at the intended layer boundary. |
| [`internal/cli/confighostlayer_test.go`](../../internal/cli/confighostlayer_test.go) | Candidate preview-parity coverage. |
| [`internal/cli/run/hostlayerreport_test.go`](../../internal/cli/run/hostlayerreport_test.go) | Candidate coverage for launch policy delivery and environment boundaries. |
| [`internal/macosuser/hostlayers_test.go`](../../internal/macosuser/hostlayers_test.go) | Candidate coverage for macos-user wire parity. |
| New `integration/hostimportexclusions_test.go` | Candidate real-jail composition and host-source-preservation coverage. |
| [`internal/cli/config_ref.txt`](../../internal/cli/config_ref.txt) | Potential user-facing config reference update, if the feature is accepted. |

## Reuse candidates from the survey

These observations are leads, not instructions; verify the symbols and contracts at the planner's chosen tree revision.

- [`config.UserScopeConfig`](../../internal/config/userlayer.go#L146-L150) reads user-scope inputs directly, including nested inherited user scope and the user layer; compare it with merged [`LoadConfig`](../../internal/config/workspacefile.go).
- [`config.HostManagementDeclared`](../../internal/config/hostmanagement.go#L86-L109) is the direct user ownership boundary.
- [`config.FilterInheritErr`](../../internal/config/inherit.go) and [`run.inheritScopeFiles`](../../internal/cli/run/inheritscope.go) are related to generated jail user snapshots.
- [`jsonptr.Parse`](../../internal/jsonptr/jsonptr.go#L23-L49) owns strict pointer grammar and accepts the root pointer, which the current design rejects.
- [`codec.LookupCodec`](../../internal/agentcfg/codec/codec.go#L61-L66) and codec `Decode`/`Encode` are candidate format boundaries; the design only permits object-kind codecs.
- [`entrypoint.HostLayerWire`](../../internal/entrypoint/hostlayerlabel.go) and [`packload.HostLayerReport`](../../internal/packload/hostlayer.go) carry launch-frozen delivery and render provenance.
- [`agentcfg.DropHostProviderKeys`](../../internal/agentcfg/providerkeys.go#L51-L85) is a neighboring host-byte boundary, but its matching and removal semantics differ from the proposed exact-pointer behavior.
- [`hostLayerFor`](../../internal/cli/config.go#L736-L771) identifies staged preview sources and render-baseline handling.
- [`captureFromSessionHomes`](../../internal/entrypoint/sessioncapture.go#L269-L325) runs after host-layer preparation; the design says exclusions must not filter captured in-jail bytes.

## Survey observations to recheck

- `LoadConfig` merges workspace values last; treating that merged map as the policy source would conflict with the design's user-only ownership boundary.
- Inherited scope is generated from effective config; the inheritance census and snapshot tests need a fresh review if the design is accepted.
- Container and macos-user stage host bytes differently, while the design expects shared render-provenance meaning.
- A host render baseline is distinct from a user host layer; the exact filtering seam depends on preserving that distinction.
- Codec failure after an explicit exclusion cannot silently fall through to original bytes under the current design.
- Session-home capture may preserve a real edit at an excluded pointer; the proposed control is not capture cleanup.
- Attach does not restage the running pack tree, so the design's proposed change takes effect on a later launch.
- Reading or writing the original host path would change the proposal into host mutation.

## Provisional work outlines — discussion only

These outlines group the earlier survey by concern. They do not prescribe build order, authorize work, or replace the design's open decisions. The headings are not ready-to-run tasks.

### Outline A — policy source and inheritance

Potential concerns include config validation, direct user-scope resolution, generated preflight/nested snapshots, and launch delivery. The current design leaves target handling open in [OQ-IH2](jail-inheritance-exclusions.md#OQ-IH2); policy-source work must not assume its answer.

Coverage areas noted in the survey: absent and empty values; malformed maps, lists, and pointers; root-pointer rejection; user-only declaration; workspace-only and conflicting declarations; unset/`none` and `own`; and user-snapshot propagation without workspace-sourced policy.

### Outline B — exact-member transformation and composition

Potential concerns include a shared codec-aware transform and its placement relative to host-layer provenance, provider-key filtering, capture, and writing. The current design leaves unmatched-pointer reporting open in [OQ-IH3](jail-inheritance-exclusions.md#OQ-IH3); do not bake either answer into a build instruction.

Coverage areas noted in the survey: exact object traversal; escaped and empty names; final values of different types; object-only traversal; missing members and non-object intermediates; parent preservation; duplicates and ancestor overlap; deterministic output; source immutability; malformed pointers; non-object documents; and decode/encode failures. Composition coverage would distinguish imported host bytes from defaults, pack, computed, captured, and render-baseline values.

### Outline C — preview, backend parity, and user reference

Potential concerns include staged-source preview, container and macos-user delivery parity, config reference wording, and real-jail behavior. The tree and design would both need rechecking before narrowing these into implementation units.

Coverage areas noted in the survey: preview parity with boot; unavailable staged source; baseline handling; sealed or unsupported delivery; copied/mounted host-layer target parity; and real-jail composition with host-source preservation.

## Planner completion required after acceptance

The design remains `in-review` with [OQ-IH2](jail-inheritance-exclusions.md#OQ-IH2) and [OQ-IH3](jail-inheritance-exclusions.md#OQ-IH3) open. Do not build from this sketch. Only after the design is accepted and every question is closed may a planner treat these notes as leads, inspect the then-current tree, and decide what survives.

At that point the planner must complete a real implementation plan: verify each symbol and path; replace the candidate map, reuse notes, and traps with current findings; settle task boundaries and order; provide complete failing test literals and exact red commands; specify green gates, fences, stop conditions, and reports. The coverage areas above are not full test specifications, and no test is asserted to fail here. Until that planner pass is complete and the plan has its repository's ready stage, these notes are not a build brief.

## Discussion boundaries

The current design proposes no change to default imports, workspace schema, pack manifests, host files, hooks, or host-side apply behavior. It also rejects automatic regex/drop behavior, final-file pruning, tombstones, host writes, vendor-specific names, wildcard/path heuristics, implicit file reads, and hook-body copying as part of this feature. These are summaries of the design, not independent policy decisions; the design wins if it changes.
