---
title: "Ready handoff: ongoing built-artifact preparation"
status: accepted
stage: DECIDED
next: "Implement task 1's production timer, persistent tree subscriptions, inspection and cancellation; stop at targeted gates"
---

# Ready handoff: ongoing built-artifact preparation

**Status:** 2026-10-13. Source fence refreshed against `689af7ae5`: the launch still starts
due one-shot work. State/control/lifecycle primitives now have focused in-process tests;
production registration, authorized dispatch and guarded settle remain unimplemented.
No end-to-end scheduler or target-runtime observation is claimed.
**Design:** [preparation contracts](launch-update-preparation.md). Design wins on behavior;
tree wins on cosmetic fact; this plan is advice and first to be wrong.

**Authority:** tasks 1–4 cover scheduler, built trees, patched programs, recorded plain-fork pins,
host next-launch consumption, Apple check-only and reporting. They do not replace ordinary warm
CLI/MCP refresh. That work alone lives in the [capture sketch](launch-program-capture-plan.md),
blocked on [OQ-PD23](program-delivery.md#oq-pd23). No worker builds from that sketch.

All task behavior below is a constraint, quoted from the design; decomposition advice is labeled.
Stop and report missing symbols, someone else's dirty owned files, noncosmetic drift, an unexpectedly
green red test, fence expansion or unrelated gate failures. Do not twist code to this map.

## Source map and exclusive writers

New paths below are prospective exclusive files, not claims about existing source.

| Task | Exclusive new files | Narrow existing edits |
| :--- | :--- | :--- |
| 1 — lifecycle/tree scheduler | `internal/cli/preparationscheduler.go`, `preparationstate.go`, `preparationenv.go`, `preparationscheduler_test.go`, `preparationstate_test.go`, `preparationenv_test.go` | [`buildpool.go`](../../internal/cli/buildpool.go#L381-L451): post-pool registration; [`backgroundadvance.go`](../../internal/cli/backgroundadvance.go#L202-L219): bounded environment, registration wrapper; [`internal.go`](../../internal/cli/internal.go#L64-L68): new hidden dispatch; [`pack.go`](../../internal/cli/pack.go): preparation control branch; [`packstatuskey.go`](../../internal/cli/packstatuskey.go): append snapshot; [`treebackground_test.go`](../../internal/cli/treebackground_test.go): changed registration assertions; [`patchedadvance.go`](../../internal/cli/patchedadvance.go#L2317-L2343): optional nonwaiting admission guard inside `finish`'s record callback, nil preserves existing callers |
| 2 — fresh-check/request and flags | `internal/cli/preparationrequest.go`, `preparationrequest_test.go`, `preparationflags_test.go` | [`patchedadvance.go`](../../internal/cli/patchedadvance.go), [`runcmd.go`](../../internal/cli/runcmd.go), [`host.go`](../../internal/cli/host.go), [`commands.go`](../../internal/cli/commands.go), [`run/buildslot.go`](../../internal/cli/run/buildslot.go), [`run/forkbuild.go`](../../internal/cli/run/forkbuild.go), [`treedelivery.go`](../../internal/cli/treedelivery.go), task 1 registration hook in buildpool |
| 3 — built-program adapters | `internal/cli/preparationforks.go`, `preparationforks_test.go` | [`forkbuild.go`](../../internal/cli/forkbuild.go), buildpool's `addForkKeys`, background key resolution; serialized after task 2 |
| 4 — host and Apple consumption | `internal/cli/preparationhost_test.go`, `preparationapple_test.go`, `hosttreeliveness.go`, `hosttreeliveness_test.go` | [`hosttrees.go`](../../internal/cli/hosttrees.go), [`hostfloor.go`](../../internal/cli/hostfloor.go), task 1 dispatcher capability route; no CLI parser ownership |

Each task is a coherent source brief, not permission for parallel shared-file writers. Land 1 before 2,
2 before 3; 4 can follow 1 but serialize its shared dispatcher/host hooks with 2/3.
Reporting is owned by the seam producing it; QA reads those outcomes, never competes for source ownership.

## Reuse and traps

- `runBackgroundAdvanceUnder`, `withBackgroundKey`, `backgroundResult` in
  [background advance](../../internal/cli/backgroundadvance.go#L337-L385): user-authorized resolver,
  per-key exclusion and persistent failure evidence. Keep the old hidden one-shot verb and argv valid.
- `runTreesUnder`, `newAdvancePool`, `treeLane.options` in [tree pool](../../internal/cli/treepool.go):
  bounded checks/builds and shared cancel context. No global permits are required for this first increment.
- `treeAdvance`, `advancePatchedFork`, `servingTreeOf`, `wantsBackground`: reuse admission and due decisions;
  scheduler state must never declare build success independently of guarded store records.
- [Existing force mode](../../internal/cli/patchedadvance.go#L116-L118) rebuilds unchanged serving inputs,
  does not wait on the build lock and disables winner rereads. **Do not set it for fresh-check mode.**
- `deliverTree`, `copyTreeForLaunch`: independent jail copies and marker-checked retry; no universal leases.
- `readyForABackgroundAdvance`, `newPoolFixture`, `pf.launch`, `bgArgs`, `outcomeOf`, `stubSpawn`:
  existing local-Git/in-process fixtures. Never self-exec a test binary or rely on inherited `-short`.
- `launcherUpdateEnv` in [pack update](../../internal/cli/packupdate.go#L58-L73) is explicit human
  authorization; automatic false/stamps are not manual restrictions.
- `packInstallPins` in [pack update](../../internal/cli/packupdate.go#L107-L110) and standing pins in
  [source fork resolution](../../internal/cli/run/forkbuild.go): timers cannot call the manual pin-moving path.
- `notty.StartDetached` closes streams, not environment. `packsrc.CleanGitEnv` removes repository state,
  not credentials. Use [provider environment declarations](../../internal/packdecl/envnames.go) and
  existing runtime/source controls; never invent an agent-name credential list.

## Task 1 — ongoing scheduler, persistent inspection and cancellation

**Settled — do not reopen:** timer progresses currently supported user-authorized built-tree keys between
launches, without rendering. Register even when nothing is due; empty selection starts no worker.
Reuse pools/locks, admission, failure evidence and existing cold readiness unchanged.

**Precondition:** clean source fence; `git rev-parse HEAD` still matches the written-against stamp or
smart-role review has refreshed it. The current callsite starts only due one-shot work: deleting it
currently loses detached progress. New subscriptions must persist across no-due launches and restarts.

### Binding interface and state

Advice: keep state under `paths.BackgroundAdvanceDir()/scheduler`, using atomic JSON replacements
and a separate short transaction lock; one process-lifetime scheduler lease, never held as a transaction
across acts. `pidlock.Acquire(..., NoWait, nil)` provides the existing kernel lease,
NOT a process-identity protocol; its recorded PID is diagnostic only. Never unlink the lock inode.
The concrete cooperative seam uses `svcendpoint.Listen`, `DialLocal` and server-side
`ReadPreamble`, with a random expected lease incarnation in bounded JSON application commands.
A held lease without authenticated incarnation proof remains unknown: no raw PID signal,
forced takeover or evidence deletion. Owner publication is atomic/private after listener readiness.
Status opens/probes without creating or rewriting state. Stop cancels the dispatch context;
replacement rejects new dispatch and drains it while retaining the lease through teardown.
The selected executable path's byte digest, not a stamp or `/proc/self/exe` path alone,
is checked before dispatch and on busy ticks.

```go
// Prospective package-cli contracts; storage root is paths.BackgroundAdvanceDir().
type preparationSubscription struct {
    Key, Runtime, Platform, InputIdentity string
    Generation uint64
}
type preparationSnapshot struct {
    Key, Runtime, Platform, InputIdentity, Phase, ServingEntry, Reason, Log string
    Generation uint64
    At, RetryAt time.Time
    WorkerIdentity string
}
func registerTreePreparation(trees []packload.Fork, req run.TreeBuildRequest, w io.Writer) error
func runPreparationScheduler(ctx context.Context, ticks <-chan time.Time, w io.Writer) error
func readPreparationSnapshot(key string) (preparationSnapshot, error)
```

`InputIdentity` covers authorized owner/contributor, source, series, recipe and platform; not staged paths.
`Generation` increments only when these inputs change, not each launch. Registering duplicates coalesces.
One latest subscription per key/runtime/platform; stale identities cannot overwrite newer records.
Request IDs in task 2 are separate from subscription and existing background-outcome generations.

- New hidden `internal preparation-scheduler` opens the real 60-second ticker and calls this loop;
  tests drive the **same loop** with a channel. Registration starts it idempotently via a separate
  spawn seam. Old `internal background-advance` stays one-shot and keeps its existing flags and behavior.
- Each tick rereads user selection/policy and current declaration before calculating due work. Dispatch
  one batch through existing acts; no overlapping batch. Existing hour/backoff rules determine work.
  Missed ticks reevaluate once; busy retries next tick without changing success or attempt stamps.
- Retain subscriptions across restart, reauthorize before reuse. Removed/off/unsupported subscriptions
  remain inspectable but inactive; changed input replaces its generation and cancels stale work.
  Invalid config disables new dispatch; record reason and repair command. No reset of serving artifacts.
- Before settle, reread policy/declarations, then verify subscription generation under a short
  **nonwaiting** subscription guard inside `finish`'s record callback, before pointer publication.
  Add `advanceOptions.admitGuard func(commit func() error) error`: nil commits as today; scheduler
  guard validates and commits with registration mutations excluded. Acquisition is build→record→
  subscription guard (NoWait); contention defers admission, never stamps success or loses the candidate.
  Registration never acquires a record/build lock; no path waits for a subscription guard while holding
  a record. Revocation/edit cancels that key's context; stale work cannot admit or satisfy a request. A tick while
  a batch runs must still observe cancellation via a control watcher, not wait for all builds to finish.
- Snapshot combines subscription activity and existing outcome/admission: key, input generation,
  worker identity, phase, time, serving entry, retry and reason/log. Status is read-only and does not
  start work or consume notices. Corrupt state is reported with repair action, never interpreted as success.
- Add `yolo pack prepare start|stop|status`; start reauthorizes recorded subscriptions, empty set exits;
  stop signals only proven owned worker, cancels its children and suppresses restart in that invocation.
  Unknown process/container identity retains state and reports next action; never kills by bare PID.
- Worker signal/cancel uses existing child process groups, kill grace and known-owned jail removal.
  SIGKILL leaves leases/state for conservative recovery. Binary identity mismatch stops new dispatch,
  drains owned work and lets next explicit start/eligible launch restart. No OS service registration.
- `preparationenv` explicitly sets `exec.Cmd.Env`: allow home/storage/tool paths, locale/temp, noninteractive
  source transport/runtime controls actually needed; strip provider-declared agent credentials, git repo
  state, workspace launch variables and capability-changing hatch values. Host Git keeps authorized
  noninteractive helpers/SSH transport; sealed build environment remains independently credential-free.
  Unknown inherited variables are not passed. Synthetic provider declarations test this without tool names.

### Implemented prerequisite seam; remaining integration

`preparationstate.go` supplies `registerPreparationSubscriptions(root, selected)`,
`withPreparationGeneration(root, expected, commit)`, `publishPreparationSnapshot(root, snapshot)`
and `readPreparationSnapshotFor(root, subscription)`. Runtime/platform qualify a snapshot;
a key-only reader cannot safely merge host and jail selections. Registration coalesces identical
inputs and advances generations on edits; unmentioned subscriptions remain inspectable, not authorized.
The nonwaiting generation guard must run within the existing admission record callback and
must be combined with policy/declaration reauthorization. Presence in state is never permission.

`preparationscheduler.go` supplies `runPreparationWorker(ctx, root, selectedExecutable, ticks, dispatch)`;
`probePreparationWorker(root)` and `controlPreparationWorker(ctx, root, expectedIdentity, op)`
use authenticated `status|wake|stop|replace` control. The injected dispatcher receives the owned
cancellation context and must use existing build-child/jail cleanup. A timer-to-callback test pins
this lifecycle loop, but is NOT the launch-to-admitted-outcome test below.

Task 1 still needs the real 60-second ticker/hidden `internal preparation-scheduler` wrapper,
`pack prepare start|stop|status` routes, detached spawn/join, post-pool registration even when not due,
current-selection reauthorization and key cancellation while busy, due/backoff/capability dispatch
through existing acts, generation guard wiring at settle, and outcome-derived snapshot production.
Only then can the exact fixture below pass (including its deleted-callsite proof).
Task 2 request qualification additionally needs monotonic check-start sequence registration,
request identity/vector and fresh-check admission; a subscription generation alone cannot prove freshness.
No warm capture replacement is implemented or authorized here (OQ-PD23).

### Tests first — exact timer production-callsite fixture

Add this complete test to the new scheduler test file, with imports context, io, testing, time and run.
It uses existing fixtures and the proposed loop; red before implementation is missing production contracts,
not an asserted runtime failure already observed. Capture red output; do not claim these tests ran here.

```go
func TestPreparationTimerAdvancesWithoutAnotherLaunch(t *testing.T) {
    pf := readyForABackgroundAdvance(t)
    pf.agentUpdates(t, `{"treepool":"next-launch"}`)
    stubSpawn(t) // intercept process start only; registration itself must be real
    if _, out := pf.launch(t, "podman", &run.ActInterrupt{}); out == "" {
        t.Fatal("fixture launch produced no report")
    }
    builds := pf.countBuilds()
    ctx, cancel := context.WithCancel(context.Background())
    ticks := make(chan time.Time)
    done := make(chan error, 1)
    go func() { done <- runPreparationScheduler(ctx, ticks, io.Discard) }()
    ticks <- pf.now // no second launch, no direct dispatcher call
    deadline := time.After(5 * time.Second)
    complete := func() bool {
        for _, key := range []string{poolKeyA, poolKeyB} {
            got, err := readBackgroundOutcome(backgroundOutcomePath(key))
            if err != nil || got.To == "" { return false }
        }
        return builds.Load() >= 2
    }
    for !complete() {
        select {
        case <-deadline:
            cancel()
            <-done
            t.Fatal("production timer did not dispatch the registered trees")
        case <-time.After(time.Millisecond):
        }
    }
    cancel()
    if err := <-done; err != nil && err != context.Canceled {
        t.Fatal(err)
    }
    for _, key := range []string{poolKeyA, poolKeyB} {
        if got := outcomeOf(t, key); got.To == "" {
            t.Fatalf("%s: timer did not publish admitted outcome: %+v", key, got)
        }
    }
}
```

This must fail if either post-pool registration or the loop's timer-to-dispatch call is deleted.
Add companion tests using existing fixtures for: no-due registration then later tick; zero/duplicate keys;
concurrent registrations; busy key then release; offline/backoff; restart; malformed config/state;
input generation edit while child paused; false/removal cancellation before settle; signal propagation;
unknown jail liveness; binary mismatch; read-only status after notice consumed; environment exclusion.
Rewrite `TestANextLaunchKeyWithNothingDueStartsNone`: it may start an idle scheduler now, but must run
zero source checks/builds. Keep old background argv/verb assertions separately; do not weaken them.

**Red/green command:** `go test -short ./internal/cli -run 'TestPreparation|TestTheBackground|TestBackground|TestANextLaunch|TestTreeUpdateTiming'`.
**Fence:** no fresh-check flags, fork adapters, host rendering, Apple capability expansion, install templates,
config schema or global permit service. **Commit:** `feat(preparation): schedule admitted tree advances between launches`.
**Report:** red/green, deleted-callsite proof, cancellation and environment cases; source evidence only until runtime QA.

## Task 2 — fresh-check mode and shared launch flags (after 1)

**Settled — do not reopen:** `--update`/`-u` before `--` is human update authorization for fresh jail/host
launches, overriding automatic false/stamps/backoff, never explicit source constraints or safety gates.
No freshness claim on failure, cancellation, unsupported required route or historical cached-good recovery.

```go
// New request interface, separate from capture's force-rebuild option.
type preparationRequest struct {
    ID string
    RegisteredAt time.Time
    Keys []preparationSubscription
    AfterCheckSeq map[string]uint64 // sequence observed at registration, per key
}
type preparationFreshness struct {
    RequestID, Key, InputIdentity, ServingEntry string
    CheckStartedAt time.Time
    Err error
}
// advanceOptions gains freshCheck bool and request identity/start time;
// force retains its existing capture/rebuild meaning.
```

Check start is recorded under the keyed check lock before transport, with input identity, monotonic
check sequence and request ID. Registration snapshots the current started sequence under that same
short record transaction; a qualifying check has a greater sequence and identical inputs. A check
already running at registration never qualifies. Clock rollback cannot qualify an older check: the
sequence boundary takes precedence over wall time. Concurrent requests registered before the same
check share it. Waiters on a pre-request winner reread its admission, then request one qualifying follow-up check. Same
inputs/release reuse the admitted entry; no pending candidate is unchanged success, not rebuild.
Waiting locks are context-cancellable; preserve build→record→mirror order and winner reread. Fresh-check
bypasses check/build backoff but does not disable exact recipe/platform validation or patch failure proof.
One request's cancellation detaches only that waiter; shared scheduler demand may finish. No remaining
demand cancels owned work. Input edit invalidates old request: fail with retry command, never silently
retarget selection. Empty vector succeeds without spawning or checking.

**Tests first:** add a fresh-mode fixture alongside existing forced-advance tests: serving build, false
policy, check less than one hour ago, unchanged upstream; assert one qualifying check, zero child builds,
same delivery key. Contrast capture force with one rebuild. Pause a keyed winner and register two
requests on opposite sides of its start; only the later request needs a follow-up check. Edit recipe
before settle; neither stale outcome satisfies the request. Ctrl-C stops launch even with a good build.

**Change:** fresh-check paths at hold, CheckOptions.Force, pending selection, build lock and winner reread
in patchedadvance (existing force sites are **not** all fresh-check sites). Wire request into buildslot and
fork/tree delivery before consumption. Parse aliases in run/host shared scopes and command rewrite;
reject attach before advances; help/dry-run no effects; argv after separator untouched; top-level self-update
unchanged. Ordinary supported installers use existing explicit update mode, not a new capture promise.

**Callsite tests:** public rewrite/parser→request seam counts actual request creation, vectors, no help/dry-run
work, attach refusal, host owner restriction, unsupported Apple build refusal and strict PF failure.
They must fail when request propagation is deleted, not merely test the option helper.
**Gate:** `go test -short ./internal/cli ./internal/cli/run -run 'TestPreparation|Test.*Update|Test.*Forced|Test.*Interrupt'`.
**Fence:** capture replacement, manual source resolver internals, timer rendering. **Commit:**
`feat(launch): request fresh preparation without rebuilding unchanged artifacts`.
**Report:** red/green, wait/reread and unchanged reuse proof, flag scope and cancelled-launch proof.

## Tasks 3 and 4 — adapters and consumption

These briefs use 1's subscription/snapshot and 2's fresh request contracts; do not invent alternate state.

**Task 3, after 2 — built programs.** Register selected patched programs through existing advance admission.
Timer plain forks only fills missing artifact at recorded pin; manual request uses existing authorized
manual pin resolution, respecting explicit tags/commits. Cold false with no established identity preserves
initial resolution/readiness. Tests first: pause admission during recipe edit/revocation; false manual
refresh; standing branch timer pin unchanged; missing fixed-pin build; explicit commit unchanged;
unchanged artifact reuse; PF-D81 failure and PF-D82 separate historical recovery; production `addForkKeys`
registration deletion fails. Gate: targeted CLI/fork short tests plus existing background failure tests.
No installer/template/capture edits. Commit: `feat(preparation): schedule built programs at authorized inputs`.

**Task 4, after 1 and serialized with 2/3 — host and Apple.** Host `next-launch` reads admitted tree/fork
without advance waits, registers selected owner keys and materializes only on launch/apply. Cold readiness
still blocks; strict patch evidence still refuses. Retain shared host tree versions by proven session
liveness, conservatively retain unknown/untracked versions; independent jail copies need no new registry.
Apple dispatcher invokes check-only advance: no build child; retain pending candidate for foreground launch.
A required build runs only with proven absence of conflicting jails; unknown/busy refuses explicit freshness
with supported next command, never broadens capabilities. Tests first: host exec/apply callsites, zero
render/link writes on timer, owner filter, three overlapping host versions, unknown-liveness retention,
Apple check-only child count zero and pending foreground consumption. Rewrite old Apple always-foreground
expectations. Gate: targeted CLI/host/platform short fixtures; native evidence remains separate.
Commit: `feat(preparation): consume host builds and defer Apple builds safely`.

For both: stop on fence drift; report red/green and deleted production callsite proof. Advice: tests may use
Linux fake runtimes and local source fixtures, because these prove dispatch/authority, not native behavior.

## Ships with and retained downstream queue

- Source tasks update launch/host/pack help in their owned files and overlapping references:
  [build timing](pi-extension-store-builds.md), [patched forks](patched-forks.md),
  [program delivery](program-delivery.md), [host launch](host-launch-environment.md).
  Preserve required trust banners and prominent patch repair errors; every stop names its next action.
- Status/reporting spans cover registration, admitted read, check, key wait, build, copy/install, render,
  readiness, prelaunch refresh and handover. Persist reason/retry after notice consumption. Piped output
  stays bounded without terminal controls. Do not sum overlapping spans into total wall clock.
- Ordinary capture/MCP replacement remains the [question-dependent sketch](launch-program-capture-plan.md).
  Unsupported installer/platform routes, raw/hand-installed extensions and configured pack source refresh
  stay explicit residual routes; do not erase their waits or call them implemented timer support.
- Retain native extension loading, migration-kit acceptance, controlled cold/warm full wall-clock samples,
  real rootless Podman, native Darwin/Apple and shutdown measurements. Build/run synthetic programs only;
  no interactive agent or API tests. Host environment consistency remains its separate queue owner.
- Builders run `just format`, targeted tests and change-reaching integration, `just build-go`, fresh
  binary-by-path nested launch from a throwaway workspace. Add timer runtime fixture proving progress
  without second launch. Nested rootful success cannot prove rootless networking/ID mapping or native Mac.
- Landing owner runs combined whole-tree gates/full launch integration once. New cross-package file readers
  require completion census update. This planning task runs neither source nor full gates.
