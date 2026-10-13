---
title: "Sketch: machine-level ordinary CLI and MCP preparation"
status: accepted
stage: SKETCH
next: "Verify the committed selected install-only and jail refresh contract, then supply the bounded capture adapter's tests-first handoff"
depends-on:
  - launch-update-preparation-plan.md
---

# Sketch: machine-level ordinary CLI and MCP preparation

**Status:** 2026-10-13. Architecture settled; source checked at `faf3a3d08`. Not a build
handoff: the shared selected install-only/refresh interface is still an agent dependency.
**Design:** [preparation boundary](launch-update-preparation.md) and
[OQ-PD23](program-delivery.md#decision-ledger). Design wins on behavior, tree wins on fact,
this sketch is advice and first to be wrong. **Nobody builds from this sketch.**

## Settled scope — do not reopen

Ordinary CLI and yolo-installed MCP programs prepare once per machine per compatible resolved
input, through repeatable sealed credential-free producers, and share immutable admitted
artifacts across workspaces. No universal cross-OS binary, captured login/config/state,
credential widening, agent registry or second builder. Explicit pins/version constraints,
strict patch failures, historical cached-good recovery and running-session stability survive.
Unsupported legacy installers retain their honest bounded existing route.

Scheduler, tree, patched-program, recorded plain-pin, host next-launch and Apple check-only
work proceeds through the [independent plan](launch-update-preparation-plan.md).
The capture architecture needs no further owner ruling.

## Source proof and reuse

| Read source | What it establishes / trap |
| :--- | :--- |
| [`resolveCaptureTarget`](../../internal/cli/capturehost.go#L587-L640) | Selected host packs and `HonoredInstalls` supply authority; native installer/source paths exist. npm declarations are rejected as needing no capture today. Do not pretend ordinary npm/MCP capture already exists. |
| [`runCaptureJail`](../../internal/cli/capturehost.go#L714-L756) | `run.Options.Sealed` is set only with a non-nil seal; existing installer capture passes nil. Reusing this caller alone does **not** prove sealed ordinary production. |
| [`capture.Program` / `Record`](../../internal/capture/select.go#L38-L109) | Ordinary selection is bin/platform and newest receipt; source/recipe fields presently describe forks. It cannot silently establish ordinary release/recipe/runtime/Node ABI compatibility. |
| [`Store.AdmitEntry` / `Complete`](../../internal/capture/store.go#L220-L350) | Reuse staged admission and completion validation, not a parallel store or success record. |
| [`Materialize`](../../internal/capture/materialize.go#L167) | Reuse confined placement/relocation and independent copies; account for guarded reread/reap rather than inventing universal session registration. |
| [`readyProgramsOf`](../../internal/entrypoint/readiness.go#L86-L116), [`serverrefresh.go`](../../internal/entrypoint/serverrefresh.go), [`shims.go`](../../internal/entrypoint/shims.go) | Actual selection/readiness/launcher/server callers are the replacement boundary, not just tests of a capture callee. |

Committed scheduler candidate objects `43e91ea41`, `dda75784c` and `5524c9c45` were inspected,
not materialized in this checkout. They expose tree registration, scheduling state, bounded
resolution, nonwaiting subscription admission guards and lifecycle fixtures. These are reuse
contracts for the capture adapter after integration, **not primary-tree symbols**. No claim of
capture support follows from the candidate's tree-only registration. Use its existing keyed
check/replay/build/admit and check/build pools; do not add global permits without measured need.

## Exact remaining source fact

The freshness work must commit and identify its **selected install-only shared helper and jail
internal refresh route**: inputs, selected declaration authority, install-only/no-update behavior,
credential-free child environment, outputs, error/cancellation semantics and production callers.
It must show how npm packages and yolo-installed MCP declarations reach that contract without
running an agent or rendering credentials. Do not name a proposed helper as existing code.

After that commit, the smart-role planner verifies the callable contract against source and adds
an exact failing fixture/test command plus adapter file/hunk fences. Until then this document
cannot honestly become a ready source handoff. This is an engineering dependency, not a new
question for the owner, nor a requirement for another approval ceremony.

## Bounded implementation brief for promotion

**Constraint:** first producer support is only declarations/platforms for which the committed
install-only contract can run in an actually sealed, credential-free environment. Explicitly
leave other installers on the existing bounded route; distinguish Linux container, Linux host
Landlock and native Darwin capture evidence. Do not relabel one as another.

1. Extend existing ordinary receipt/admission compatibility through the one receipt reader/writer:
   resolved release/source, recipe/dependency inputs and relevant runtime/Node ABI. Preserve old
   capture entries for their established route and historical-cache validation; no universal new
   provenance prerequisite. A legacy entry cannot falsely prove new compatibility.
2. Dispatch supported ordinary producers through the existing selected scheduler/request contract.
   One keyed winner checks/builds/admit; concurrent consumers join and reread it. Manual fresh-check
   bypasses automatic policy/throttle, not explicit source constraints; unchanged inputs reuse bytes.
3. Replace supported warm vendor/MCP updates only at tested readiness/launcher/materialization
   callers. Admission never renders settings or changes a running install. Keep unsupported routes
   and cold mandatory readiness/refusal hatches; attach reads its running generation.
4. Extend the actual adapter's retention/reap contract if shared live bytes are introduced. Ordinary
   copied jail installs need no global generation registry. Unknown liveness retains shared bytes.

**Required fixtures:** two workspaces reuse one release; changed recipe/platform/runtime/ABI or
explicit pin rejects incompatible reuse; concurrent waiters produce one act; no credentials,
login/config/state or host-file grants enter capture; failed/interrupted producer admits nothing;
revocation/edit rejects stale admission; timer never renders; cold/off/manual behavior; unsupported
installer stays bounded; strict patch failure and validated old-cache escape stay separate.
Assertions must reach the actual production callers, using fake programs only, no agent/API calls.

**Promotion fences:** capture producer/store/receipt adapters, selected launcher/readiness/server
handoff and their focused tests only. Scheduler/freshness files remain owned by their existing
work until integrated. Stop on dirty foreign source, missing committed contract, changed behavior,
or any need to widen auth. No implementation from this sketch.

**Acceptance still external:** real rootless and native Darwin sealing/runtime evidence, full
controlled cold/warm wall-clock measurements and real agent loading remain separate acceptance;
static compilation or nested rootful Podman cannot establish them.
