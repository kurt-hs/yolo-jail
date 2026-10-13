---
title: "Sketch: machine-level ordinary CLI and MCP preparation"
status: accepted
stage: SKETCH
next: "Wire the verified committed selected install-only helper into the sealed producer and launch/readiness/MCP consumers"
depends-on:
  - launch-update-preparation-plan.md
---

# Sketch: machine-level ordinary CLI and MCP preparation

**Status:** 2026-10-13. Source checked against `4e7d58311` plus this isolated candidate.
Compatibility, artifact-driver and keyed admission pieces are implemented and locally tested;
the selected sealed producer and launcher/readiness/MCP replacement are not wired. This is not
a ready handoff for the whole adapter.
**Design:** [preparation boundary](launch-update-preparation.md) and
[OQ-PD23](program-delivery.md#decision-ledger). Design wins on behavior, tree wins on fact,
this sketch is advice and first to be wrong. No new owner ruling is needed.

## Settled scope — do not reopen

Ordinary CLI and yolo-installed MCP programs prepare once per machine per compatible resolved
input, through repeatable sealed credential-free producers, and share immutable admitted
artifacts across workspaces. No universal cross-OS binary, captured login/config/state,
credential widening, agent registry or second builder. Explicit pins/version constraints,
strict patch failures, historical cached-good recovery and running-session stability survive.
Unsupported legacy installers retain their honest bounded existing route.

Scheduler, tree, patched-program, recorded plain-pin, host next-launch and Apple check-only
work proceeds through the [independent plan](launch-update-preparation-plan.md).

## Implemented independent contracts

| Source | Contract / constraint |
| :--- | :--- |
| [`capture.ProgramInputs.Identity`](../../internal/capture/programinputs.go) | Hashes selected owner/bin/declaration/resolver, resolved release/source, recipe, dependency set, runtime/toolchain identity, platform and applicable observed Node ABI. No workspace path or universal floor is an input. |
| [`CaptureReceipt`](../../internal/entrypoint/capturereceipt.go), [`captureRecords`](../../internal/cli/capturematerialize.go) | Additive ordinary compatibility identity through the existing receipt writer/reader/adapter. Old receipts still answer their established empty-identity route; they cannot prove a new resolved identity. |
| [`capture.Select`](../../internal/capture/select.go), [`capture.PruneSupersededCaptures`](../../internal/capture/gc.go) | Exact ordinary identities are separate selection keys; prune remains the complement of the reader and keeps each reachable identity's winner. Materialized copies need no session registry. |
| [`capture.Run`](../../internal/capture/inner.go), [`artifactSurfaces` / `ValidateArtifacts`](../../internal/capture/artifacts.go) | Opt-in artifact roots, not whole prefixes/home. Reject traversal, overlaps, yolo state, unselected/outside links, dangling/cyclic producer links, special files and unsafe permissions. Installer env is allowlisted with capture-private prefixes; context cancellation stops its process group. Legacy nil-artifact capture is unchanged. |
| [`runCaptureRun`](../../internal/cli/capture.go) | Repeated hidden `--artifact=HOME-RELATIVE-PATH` arguments reach the real driver. This mode refuses a missing sealed-build marker before executing the installer. **A marker is not sandbox evidence**: the host producer must actually use the seal. |
| [`Store.AdmitEntryGuarded`](../../internal/capture/store.go) | Digest/freeze and keyed CAS contention happen outside the caller's short selected-input publication guard; rename/completion happen within it. Identical bytes produced by different input identities serialize at the existing store. |
| [`prepareProgramCapture`](../../internal/cli/programcapture.go) | Existing receipt selection, pidlock, staging, immutable admission and original-runtime/keeper liveness proof. Same-identity waiters reread the winner; unchanged identity skips production. Cancellation/stale authority admits nothing; unknown runtime retains staging. The producer callback remains an integration seam, not an implemented installer. |
| [`materializeCapture`](../../internal/cli/capturematerialize.go) | Hidden `--compatibility=IDENTITY` selects exact ordinary bytes and preserves that identity in the materialize receipt. Existing fork `--key` and legacy native selection stay separate. Existing relocation/confinement machinery places independent copies. |

The compatibility tuple must contain **resolved facts**. Passing a declaration such as `latest`
as its resolved release, a backend name as its complete runtime/toolchain identity, or an unapplied
MCP list as its dependency vector does not meet this contract. The selected adapter must also
include output/refresh instruction changes in its recipe identity.

## Actual helper milestone and remaining seam

Read-only inspection of the Task 2 owned source found draft
`SelectedProgramPreparations(e, packs, mode)`, `ProgramPreparationEnv(mode)` and the context-cancellable
launcher runner. Its source HEAD was `4e7d58311`; these helper edits were **uncommitted** when inspected.
They are not APIs available in this candidate. The runner still inherits environment; wrapping it
would not establish credential-free production.

The following source work remains; it is engineering, not a new approval gate:

1. Verify the committed helper's selected/honored declaration, bin/platform/shadow selection,
   install-only/no-application-argv mode, cancellation and actual enabled MCP/declared refresh set.
   Resolve release/dependency/runtime/ABI facts without relaxing explicit version/source constraints.
2. Supply `prepareProgramCapture`'s producer through an **actual non-nil seal** at
   [`runCaptureJail`](../../internal/cli/capturehost.go), with the authorized narrowed pack set,
   exact program output roots and original-runtime record. The existing installer capture still
   passes nil and `resolveCaptureTarget` still rejects npm; neither proves this producer exists.
3. Keep the seal's MCP table withholding. Original server env/args can contain secrets:
   [`agentServerTables`](../../internal/cli/run/seal.go) correctly removes them. Only selected resolved
   install/refresh artifact inputs may enter production; never restore credential-bearing tables.
4. Join the existing scheduler/request checks and pools, supplying its short nonwaiting input guard.
   The admission adapter does not implement update timing, STARTED-boundary checks or shared-demand
   ownership. A cancelled waiter must not cancel work other demand still owns.
5. Wire exact compatibility into real readiness/launcher/server/host materialization callsites,
   replacing only supported routes. A raw npx server, unsupported native producer or pin stays on
   its honest legacy path. Timer work never renders; attach keeps its running generation.

## Verification and fences

Permanent local synthetic fixtures reach hidden driver/materializer dispatch and the actual
admission adapter: excluded secrets/state, two workspace copies, exact input isolation and prune,
concurrent same-identity reuse, distinct identities sharing identical CAS bytes, cancelled demand,
stale publication rejection and unknown-runtime staging retention. Inputs tests cover release,
recipe/dependencies, source, declaration, runtime, platform and Node ABI mismatches.

Targeted commands: `go test -short ./internal/capture ./internal/entrypoint ./internal/cli`,
`go test -race -short ./internal/capture ./internal/cli -run 'TestProgram'`,
`go vet ./internal/capture ./internal/entrypoint ./internal/cli`, and a build of `./cmd/yolo`.
These establish local behavior, **not** a sealed runtime launch or capture replacement at readiness.

Fences remain capture/store/receipt adapters and their tests. Scheduler/freshness, source-fork,
host/Apple/liveness and selected-helper edits stay with their owning tasks. Existing
[PF-D81 / PF-D82](patched-forks.md#decision-ledger) authority is untouched; no new compatibility
field is required of their historically valid receipts.

**Acceptance still external:** combined census/full gate and fresh non-agent runtime verification,
real rootless and native Darwin sealing evidence, controlled cold/warm wall-clock measurements
and real agent loading. Exact identity selection retains each still-reachable compatibility winner;
no age-based reap or new global liveness registry was added.
