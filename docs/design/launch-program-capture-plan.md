---
title: "Ready handoff: machine-level ordinary CLI and MCP preparation"
status: accepted
stage: DECIDED
next: "Build the selected sealed npm producer and exact production consumers, then integrate the shared-demand owner's committed API"
depends-on:
  - launch-update-preparation-plan.md
---

# Ready handoff: machine-level ordinary CLI and MCP preparation

**Status:** 2026-10-13. Written against the stable combined candidate `a2b94b129`.
The selected helper and capture security components are committed. Engineering mechanics below
are settled for implementation; the producer, consumers and shared-demand integration remain
unbuilt. This is not a feature-completion claim.
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
| [`capture.Run`](../../internal/capture/inner.go), [`artifactSurfaces` / `ValidateArtifacts`](../../internal/capture/artifacts.go) | Opt-in artifact roots, not whole prefixes/home. Reject traversal, overlaps, yolo state, unselected/outside links, dangling/cyclic producer links, special files and unsafe permissions. Installer env is allowlisted with capture-private prefixes. Linux artifact mode uses an isolated same-binary subreaper and pinned no-follow descriptor copies; cleanup proof precedes validation. Unsupported artifact platforms refuse only this new route. Legacy nil-artifact capture is unchanged. |
| [`runCaptureRun`](../../internal/cli/capture.go) | Repeated hidden `--artifact=HOME-RELATIVE-PATH` arguments reach the real driver. This mode refuses a missing sealed-build marker before executing the installer. **A marker is not sandbox evidence**: the host producer must actually use the seal. |
| [`Store.AdmitEntryGuarded` / `AdmitEntryValidated`](../../internal/capture/store.go) | Digest/freeze and keyed CAS contention happen outside the caller's short selected-input publication guard; rename/completion happen within it. Identical bytes produced by different input identities serialize at the existing store. The validated route checks the retained canonical manifest before publication; incompatible metadata refuses without changing existing metadata or receipts. |
| [`prepareProgramCapture`](../../internal/cli/programcapture.go) | Existing receipt selection, pidlock, staging, immutable admission and original-runtime/keeper liveness proof. Same-identity waiters reread the winner; unchanged identity skips production. Cancellation/stale authority admits nothing; unknown runtime retains staging. The producer callback remains an integration seam, not an implemented installer. |
| [`materializeCapture`](../../internal/cli/capturematerialize.go) | Hidden `--compatibility=IDENTITY` selects exact ordinary bytes and preserves that identity in the materialize receipt. Existing fork `--key` and legacy native selection stay separate. Existing relocation/confinement machinery places independent copies. |

The compatibility tuple must contain **resolved facts**. Passing a declaration such as `latest`
as its resolved release, a backend name as its complete runtime/toolchain identity, or an unapplied
MCP list as its dependency vector does not meet this contract. The selected adapter must also
include output/refresh instruction changes in its recipe identity.

## Stable component handoff contract

- **Driver:** hidden `capture-run --home=ABS --out=ENTRY --scan-content-refs`
  with repeated `--artifact=HOME-RELATIVE-PATH`, followed by `-- INSTALLER ARGV`.
  The selected producer must use an actual non-nil seal and supply the sealed-build marker;
  the marker alone remains an accidental-use assertion. Config/login roots, whole shared
  install prefixes, unselected link targets and yolo state are excluded/refused.
- **Ownership:** Linux artifact mode self-execs the existing binary's hidden
  `capture-installer-supervise` helper. Only that helper enables subreaping. Its lifetime pipe
  closes on driver cancellation/death; after direct installer exit or cancellation it kills
  only kernel-reported direct children, including successive adopted escaped/session-changing
  generations, and reaps to `ECHILD`. Cleanup has a three-second bound. Cancellation also bounds a stopped helper: the driver
  waits four seconds for cleanup, then kills only its own helper and refuses without
  descendant-completion proof; the owned sealed runtime must finish teardown. Missing ownership,
  failed installer, missing completion evidence or unreaped descendants refuse before transfer;
  stop the owned sealed runtime and retry. No global subreaper or broad process kill exists.
  Unsupported platforms retain legacy/native installation and refuse only new artifact capture,
  naming the supported sealed Linux route.
- **Transfer:** pin HOME before installation; open every source ancestor and regular file
  fd-relative with no-follow checks, and read links without following them. Artifact mode
  copies changed leaves from pinned descriptors into the private output tree, even on one
  mount. It does not use the legacy rename optimization or pathname source cleanup/chown;
  its throwaway runtime owns source cleanup. Link existence/cycle checks use captured entries,
  not installer-controlled paths. Descriptor confinement remains necessary even after reaping.
- **Admission:** `prepareProgramCapture` receives resolved `ProgramInputs`, exact roots,
  producer callback and short publication guard. Before attaching identity to same-byte CAS,
  `AdmitEntryValidated` validates the retained canonical manifest under the content lock, outside
  the publication guard: platform, selected roots/entries, full relocation scan, relocation
  disposition/reference instructions, and original home when not relocatable. It never upgrades
  old canonical metadata. Incompatible reuse returns an error, publishes no new receipt, and
  leaves legacy metadata/receipts intact. Warm exact winners also validate canonical metadata.
  Different ABI identities with genuinely compatible metadata may still share one CAS entry.
- **Boundary:** ownership cleanup and descriptor reads do not seal a producer. The selected
  adapter below must call the actual empty-pack seal; its caller and assembled-runtime negatives
  are separate acceptance evidence from the artifact-driver tests.

## Selected npm production mechanics

**Constraint:** supported npm installation inputs contain only owner, bin, package selector and
Node floor. Custom install flags, source/native programs and vendor self-update recipes retain
their existing bounded route. MCP contributes only packages of effective enabled entries; never
server args, env or credential-bearing tables. Empty vectors do nothing.

**Advice:** resolve a package lock with scripts disabled in an actual sealed empty-pack runtime,
observing Node version/ABI, npm version, platform and image identity there. Persist the lock's
resolved version, source/integrity and complete dependency graph as immutable resolution facts.
The selected floor is satisfied through the existing mise toolchain, never a guessed ABI.
A second sealed runtime installs with `npm ci` only if exact admission has no winner. Validate
its observed runtime against the resolution before scripts run. Both phases use the existing
owned child/stream/cancellation lifecycle and original-runtime known-gone witness.

**Constraint:** the production call to `runCaptureJail` has a non-nil seal and an explicitly empty
pack selection. No selected pack manifest, hook, provider or sibling program crosses; only the
installation input file enters the scratch workspace. The existing seal withholds host config,
credential channel, services, grants, MCP tables and shared caches from the first runtime start.
Use the hidden artifact driver for the install phase, full relocation scan and exact roots.

**Advice:** one declaration-keyed resolution record stores the latest successful resolved facts
and started-check sequence. Normal warm delivery reads that record and exact receipts without
runtime/registry work; fresh requests register before checking and require a later sequence.
Keep check/entry exclusion separate from short publication transactions. Runtime recipe changes
invalidate the record. Guard declaration/runtime identity again immediately before publication.
Failed/cancelled manual resolution never turns old-good bytes into successful freshness.

**Constraint:** exact per-launch compatibility decisions feed generated launchers, readiness,
enabled MCP materialization and the host's actual declared owner/applied MCP vector. Independent
identity-named payload directories keep running programs stable; only the next launch/apply
moves entry links. Timer work prepares immutable bytes and never materializes a home or settings.
Workspace-only declarations the user resolver cannot authorize retain the real foreground route.
Shared ownership extends the committed shared-demand worker/result protocol with an
`ordinary-program` discriminator and exact ordinary result. It does not mint a source-fork
request, carry a manifest/executable path, or create a second worker, monitor, registry or pool.
The existing unique requester/kernel lease, detached worker, current user resolver, timer lease
and final no-wait demand/subscription guards remain the owners. Short metadata contention is
retried outside those locks without repeating resolution or installation; cancellation and
revocation still forbid success. Scheduler dispatch lives in the existing persistent loop.

**Constraint:** sealed reuse supports only the independently matching **baseline** runtime.
The image marker identifies the base flake, not `packages:` or store-delivered loader variants.
Customized packages, configured Node toolchains, loader/tool overrides in `env_sources` or the
composed channel, and file-backed env sources whose runtime effect is not established use the
existing authorized bounded installers. They are disclosed, not refused or silently dropped;
`--update` must still finish its qualifying legacy check or fail before the target executes.
Do not widen the empty-config seal to inherit caller packages or credentials to claim equality.

**Constraint:** the HOST floor selects a capture only after its own Node binary/version/ABI,
npm, platform and actual baseline identity match every producer fact. Missing baseline identity,
custom loader environment and unsupported Darwin artifact mode retain the real host installer,
its freshness/wait/cancellation and fixed-selector rules. No new native producer is required.
The compatible route holds one exact decision through floor publication, keeps independent
copies, and can consume a timer-prepared generation without a mutable registry poll.

### Source and test map

- New selected producer/inner npm driver under `internal/cli`; dispatch in the existing hidden
  internal command table. Reuse `prepareProgramCapture`, validated CAS admission, exact receipts,
  materializer and the existing capture-child lifecycle.
- `programcapturedemand.go` resolves installation authority from current user selection and
  adapts ordinary requests/results to `shareddemand.go`; `programcapturescheduler.go` adds
  ordinary records/dispatch to `preparationscheduler.go`, never another timer loop.
- `programcapturehost.go` wires compatibility-gated ordinary reuse into the actual host floor;
  its independent bounded installer remains the fallback, including unsupported platforms.
- Public run and HOST tests in `programcapturedemand_test.go`, `programcapturehost_test.go` and
  `programcaptureruntime_test.go` exercise real callbacks/consumers with private synthetic npm
  fixtures and an observed installed Node. `programcapturelifetime_test.go` tests the actual
  shared worker's second-demand retention, zero-demand teardown and selection revocation;
  `programcapturescheduler_test.go` drives the existing production timer and checks running-home
  stability. Generated readiness/MCP/bootstrap consumers use the actual hidden materializer.
- Fresh container preparation callsite in `internal/cli/run`, wired only by the normal CLI front
  door. The callback returns exact compatibility decisions, never copies or a global generation.
- Entrypoint generated npm launchers and enabled-server refresh read those exact decisions;
  legacy capture/fork selection stays separate. Host floor resolves its declared ordinary owner.
- Permanent synthetic tests must reach these production callers: cold/warm cross-workspace reuse,
  lock/runtime/ABI conflicts, fresh newer registry/fixed pins, cancellation and stale guard,
  running-generation stability, and actual assembled seal mount/config/env/input negatives.
  Test registries/installers are local fixtures; no agent execution or provider API calls.

**Gates:** targeted caller tests, affected short suites, vet, Linux build and Darwin compile,
format/diff check and document check. Combined gates, pins, census and nested/native acceptance
remain once-only landing-owner work. Preserve actual unfinished source outside the disposable tree.

## Candidate implementation checklist

The selected adapter now supplies the real non-nil seal and empty staged pack tree, resolved
npm lock/runtime facts, original-runtime witness, STARTED boundaries and exact admission.
Public run/host callbacks, generated readiness/MCP/bootstrap materialization, generic shared
demand dispatch and ordinary records in the persistent scheduler are wired in this candidate.
The source/test map above names those callsites; this is not acceptance of a live container.
Legacy `yolo capture` still rejects npm and retains its nil-artifact installer semantics.
Unsupported runtime/installer routes keep their existing bounded checks and fixed constraints.
The seal still withholds MCP/server env/args; timer work admits bytes without rendering homes,
and an attach keeps its immutable running-launch vector.

## Verification and fences

Permanent local synthetic fixtures reach hidden driver/materializer dispatch and the actual
admission adapter: excluded secrets/state, two workspace copies, exact input isolation and prune,
concurrent same-identity reuse, distinct identities sharing identical CAS bytes, cancelled demand,
stale publication rejection and unknown-runtime staging retention. Inputs tests cover release,
recipe/dependencies, source, declaration, runtime, platform and Node ABI mismatches. Security
regressions cover successful survivors, immediate double-fork/new-session children, escaped
cancellation, missing helper evidence, parent swaps with excluded regular files, incompatible
Linux/Darwin same-byte reuse, historical symlink-only metadata and retained relocation instructions.

Targeted commands: `go test -short ./internal/capture ./internal/entrypoint ./internal/cli`,
`go test -race -short ./internal/capture ./internal/cli -run 'TestProgram'`,
`go vet ./internal/capture ./internal/entrypoint ./internal/cli`, and a build of `./cmd/yolo`.
These establish local behavior, **not** a sealed runtime launch or capture replacement at readiness.

The ordinary adapter extends only the generic shared-demand dispatch and scheduler record kind.
Independent source-fork, host/Apple/liveness repairs retain their committed owning-task behavior
and permanent tests. Existing
[PF-D81 / PF-D82](patched-forks.md#decision-ledger) authority is untouched; no new compatibility
field is required of their historically valid receipts.

**Acceptance still external:** combined census/full gate and fresh non-agent runtime verification,
real rootless and native Darwin sealing evidence, controlled cold/warm wall-clock measurements
and platform-specific runtime evidence. No automated check starts a coding agent or calls its API. Exact identity selection retains each still-reachable compatibility winner;
no age-based reap or new global liveness registry was added.
