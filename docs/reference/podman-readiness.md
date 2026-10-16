---
status: current
stage: CURRENT
verified: 2026-10-16
verified_commit: 8e65f344
covers:
  - internal/runtime/podmanready.go
  - internal/runtime/podmanreadyview.go
  - internal/runtime/podmanattempt.go
  - internal/runtime/podmanmachine.go
  - internal/cli/run/podmanready.go
  - internal/cli/run/preflight.go
  - internal/cli/run/lifecycle.go
  - internal/cli/run/launchrecord.go
  - internal/cli/run/housekeeping.go
  - internal/cli/check/podmanready.go
  - internal/prune/guard.go
tags: [launch, podman, reliability, readiness, housekeeping]
summary: "How a launch and `yolo check` wait for Podman: one patient, budget-bounded `podman info` gate that never kills the probe, retries only an early exit that can clear, refuses at once an answer that cannot, and hands its parsed answer to every later Podman fact on the launch path. With it: the housekeeping slot holds its lock one deletion at a time, and every launch leaves one line in a machine-wide launch log."
---

# Podman readiness — the launch's one patient `podman info`

**Status:** Verified 2026-10-16 against `8e65f344`. Graduated from the design doc
`docs/reference/podman-readiness.md`, which git history keeps. MEASURED by unit tests through
`run.Run`, runtime selection and `check.Check`, on a fake podman whose clock the test drives, and by
the attempt runner on real subprocesses ([Testing](#testing)). UNMEASURED: a real reboot, and a
rootless podman at all. A nested jail's podman is rootful, so the rootless row of
[the kill table](#why-a-timed-out-probe-is-not-a-failed-podman) is settled only on CI or a real
rootless host ([The real-host check](#the-real-host-check-still-owed)). Podman's error texts were
read from podman v6.1.2's source.

The first `podman info` after a boot does Podman's own post-boot cleanup, which can take many
seconds. A launch used to kill that probe at a fixed timeout and refuse, though the cleanup was
probably seconds from done. Now one **readiness gate** waits, within one budget, for Podman to
answer, never kills it, retries only after an early error, and reuses that one answer for every
later Podman fact the launch needs. There is no cross-workspace lock, because Podman already
serializes its own cleanup.

| Component | Lives in |
| :--- | :--- |
| The gate: attempts, backoff, classification, budget | `internal/runtime` (`WaitForPodman`, `WaitForPodmanShowing`, `ClassifyPodmanFailure`, `ClassifyStartError`, `ReadyResult`) |
| The attempt runner | `runtime.RunPodmanAttempt` |
| macOS Podman Machine's patient one-shot | `runtime.WaitForPodmanMachine` |
| The launch's use of it | `internal/cli/run` (`waitForPodman`, `acceptPodmanFacts`, `storeFactsFromGate`, `usesReadinessGate`, `usesPatientOneShot`) |
| `yolo check`'s use of it | `internal/cli/check` (`podmanGate`, `podmanGraphRoot`) |
| The attach decision's tri-state | `run.probeRunningContainer` |
| The machine-wide launch log | `run.recordLaunchOutcome`, `run.appendLaunchLine`, `run.HostLaunchRecord` |
| Housekeeping's per-deletion lock | `run.withHousekeepingPass`, `run.deletionGuard`, `prune.Guard`, the `…Guarded` reapers, `flakebundle.ReapGuarded` |

**Reads with:** [`perf-logging.md`](perf-logging.md) (the host timing log),
[`loopback-tls-reachability.md`](loopback-tls-reachability.md) (the host-loopback disposition the
gate's answer protects), [`report-tiers.md`](report-tiers.md#the-launch-stream) (the launch stream).

---

## Terms

- **Readiness gate** *(a term this design coined)*: the step of runtime selection that waits, within
  one fixed budget, for `podman info --format json` to answer. It is not a lock, not a cache between
  processes, and not a Podman service.
- **Podman facts** *(coined here)*: the gate's successful answer, the one parsed `podman info`
  document every later reader on the launch path uses.
- **Retry floor** *(coined here)*: how long podman took to give the answer being retried, capped. A
  retry starts only if the budget left after its backoff is longer than it.
- **Patient one-shot** *(coined here)*: macOS Podman Machine's form of the gate — one attempt,
  never retried, waited for up to the same budget.
- **Restore storm** *(coined here)*: the burst of launches a session restorer starts after login.

## Why a timed-out probe is not a failed Podman

Three facts decide the design.

**A timeout does not bound the wait when output goes through pipes.** With `os/exec` capturing
output into buffers, a kill of the direct child is followed by `cmd.Wait()`, and with no
`WaitDelay` set, `Wait` does not return until every process holding those pipes has closed them —
descendants that survived the kill included. A 1 s timeout on `sh -c 'sleep 30 & sleep 100'`
returned after 100 s (MEASURED). The repository fixes the same class for git with `gitWaitDelay` and
`DetachGit` in `internal/packsrc`.

**Podman serializes its own post-boot refresh** (SOURCED, libpod's `makeRuntime`). Every Podman
process takes the `alive.lck` lock in its engine tmpdir. The one that finds no `alive` file runs
`refresh()`, every other process blocks on that lock until the refresh is done, and `refresh()`
writes the `alive` file only after it has refreshed containers, pods and volumes.

**A kill affects rootless and rootful Podman differently** (SOURCED, `pkg/rootless`):

| Podman | What a kill hits | Result |
| :--- | :--- | :--- |
| Rootless, first command after boot (no pause process yet) | The outer process, which re-executed itself into a new user namespace. The child sets **no** parent-death signal, keeps stdout and stderr, and does the refresh; SIGKILL cannot be forwarded | The child finishes the refresh while the killer waits on its pipes, then the launch reports a timeout and discards a working answer |
| Rootless, joining an existing pause process | A parent whose child has `PR_SET_PDEATHSIG` set to SIGTERM | The child dies. If it was the refresher, no `alive` file is written, and the next client starts the refresh again |
| Rootful | The refresher itself | The refresh is aborted and the next client starts it again; a refresh longer than the timeout never completes under kill-and-retry |

So a per-attempt kill either discards the answer or destroys the work every later client is waiting
on, and a cross-workspace yolo lock would add nothing: Podman already runs the refresh once while
the other clients wait cheaply ([PR-D1](#PR-D1)).

## The gate

One function in `internal/runtime`, used by the launch's runtime selection (explicit and
autodetected alike) and by `yolo check` ([PR-D6](#PR-D6)).

1. **An attempt** runs `podman info --format json` in its own process group, so a Ctrl-C at the
   terminal does not reach it. Its stdout and stderr go to unlinked temp files, not pipes, so a
   surviving descendant can never stretch a wait, and closing an output end cannot SIGPIPE a
   refresh mid-way ([PR-D3](#PR-D3)). No deadline applies to an attempt except the remaining
   budget. **yolo never kills it** ([PR-D2](#PR-D2)).
2. **Success** is exit 0 with JSON that parses. The parsed document becomes the launch's Podman
   facts ([PR-D5](#PR-D5)).
3. **An early exit** — nonzero, or zero with unparsable output — is retried after an escalating
   backoff, never past the budget ([PR-D4](#PR-D4)), unless podman's own words say it cannot clear
   on its own ([OQ-PR1](#OQ-PR1)). Then it refuses at once and names the fix
   ([PR-D13](#PR-D13)). The classification reads podman's fatal lines only, from a table of
   podman's own error texts; a line that says podman is busy wins over one that looks permanent;
   and an error the table does not recognise keeps retrying within the budget, never refusing
   early (the tri-state rule: "could not ask" is never a yes, and "do not know" is never a no). A
   retry starts only if the budget left after its backoff is longer than the retry floor, so the
   gate never starts an attempt it could only abandon mid-answer ([PR-D22](#PR-D22)).
4. **An attempt that cannot start at all** is read by its errno under the same rule: a missing
   binary, or one this user cannot execute, fails at once and names the fix; one that can clear
   (a binary busy being replaced, a fork refused at a process limit) is retried like an early exit;
   and a failure of yolo's own scratch files is named as that, with the step its error calls for and
   no podman step after it, since that attempt never ran podman ([PR-D23](#PR-D23)).
5. **At budget expiry, or on an interrupt,** yolo stops waiting. It does not kill the attempt: that
   `podman` may be doing the refresh every later client needs. The refusal names the process —
   *"podman (pid N) is still running; yolo left it to finish"* — and keeps the last error an earlier
   attempt returned. An interrupt exits 130.
6. **The budget** is a constant, with no config key and no `YOLO_*` dial ([PR-D11](#PR-D11),
   [PR-D12](#PR-D12)), and applies to every launch and every `yolo check`, never keyed on a reboot
   ([OQ-PR1](#OQ-PR1)).

A warm launch pays nothing new: one `podman info --format json` in place of two or three separate
queries, and no sleep.

> [!WARNING]
> **Do not kill the attempt, or its process group, at a timeout.** That is right for git, whose
> helpers do no shared work; here it aborts the refresh every later client needs. And **do not
> ignore a failed `podman info` and launch anyway**: it hides a broken runtime and moves the failure
> into image or container operations.

### What the gate costs

A permanent fault yolo recognises — a needed `podman system migrate`, a refused user namespace, a
broken `newuidmap`, an unparsable `containers.conf`, a permission error — refuses at once, naming
the fix. One yolo does not recognise (a `CONTAINER_HOST` or default connection pointing at an
inactive socket, a stopped Linux `podman machine`) shows its reason within the progress grace
period and gives its final refusal only at the end of the budget; the user can press Ctrl-C at
once. Unrecognised is retried by rule, because a socket that comes up late at boot is exactly such
an answer, and it clears. A refused launch may leave one `podman` running, which the refusal names;
it is Podman's own work and finishes or blocks on Podman's own lock.

## Scope

**In scope:** a Linux launch whose runtime is Podman, whether selected explicitly or by
autodetection, on the host or in a nested jail, and `yolo check`.

**Apple Container** keeps its one-shot probe ([PR-D7](#PR-D7)). **macOS Podman Machine** takes the
patient one-shot ([PR-D24](#PR-D24)): to `podman info`, a stopped VM and a starting VM look alike
only until the client dials, and a stopped machine fails that dial at once, so waiting on an attempt
still running costs a stopped machine nothing. A stopped machine still fails at once with its
`podman machine start` hint; one still running at the end of the budget is reported as not
answering, with its pid, and the next step is `podman machine list`, then waiting, then a restart if
it still does not answer. Its answer is not the launch's Podman facts, which only Linux podman
reads. `yolo check` reports a podman still running at the end of the budget as not answering on
Linux too, never as one to start.

**Out of scope:** the other things a launch restored at boot depends on. Network readiness is one
(the pack refresh runs before runtime selection, and can fail DNS at boot); nix-daemon readiness is
the other. Each gets its own design if it bites.

The gate never removes containers, resets Podman's database, starts a host service, accepts a stale
image, or treats a probe it could not complete as permission to launch.

## What reads the gate's answer

| Caller | What it reads |
| :--- | :--- |
| `hostLoopbackFactsFor` (`internal/cli/run`) | The Podman facts. Before the gate, it ran its own `podman info` and, on failure, passed no forwarding option and set `YOLO_HOST_LOOPBACK=unknown`, which never escalates ([OQ-R3](loopback-tls-reachability.md#oq-r3)), leaving every loopback-TLS service unreachable without a refusal |
| The image copy's store facts (`AutoLoadOptions.StoreFacts`, `image.ParsePodmanStoreFacts`) | The Podman facts, so a rootless store reaches the copy's store spec. Before, a failed query read as "rootlessness unknown" and the bare copy ran |
| `yolo check`'s image-delivery and Disk I/O priority sections | The gate's answer, including `store.graphRoot`; no `info` of their own |
| `podman.facts` in `host-perf.log` | Noted from the gate's answer |
| The attach decision (`probeRunningContainer`) | Not the facts: a `ps` with its own deadline, tri-state, where "could not ask" refuses the launch rather than launching fresh ([PR-D8](#PR-D8), [PR-D18](#PR-D18)). The refusal names the runtime, the jail, why launching fresh would be wrong, and the runtime's own list command |

Everything after the gate stays single-shot: those calls assume a Podman that has just answered. The
gate adds no retries to image delivery or container creation, whose existing rules are unchanged,
including the image copy's one retry ([failure paths](image-staging-vs-baking.md#failure-paths)).

## What the user sees, and what is recorded

**On the [launch stream](report-tiers.md#the-launch-stream)** (stderr), with nothing that can be
hidden ([PR-D9](#PR-D9)):

- A warm probe prints nothing within the progress grace (`progress.DefaultGrace`).
- A slow first attempt shows `Checking that podman is running` with live detail: *"waiting for
  podman to answer (it may be finishing post-boot cleanup), 14s of 60s"*.
- Each early exit prints one line above the progress line: *"podman info: exit 125: <first stderr
  line>; retrying in 2s"*, so a fast-failing permanent fault shows its reason within the grace.
- Success after a wait closes with *"done after 3 attempts (41s)"*.
- The refusal stays on stdout with the other runtime-selection refusals (both streams are teed to
  `launch.log`). It names the attempt count, the elapsed time, the last stderr line, and any
  `podman` left running.

`yolo check` asks the gate once and keeps the answer; its progress and retry lines go to stderr,
because the report on stdout may be JSON, and it catches no Ctrl-C ([PR-D19](#PR-D19)).

**In `host-perf.log`**, when timing is on ([PR-D10](#PR-D10)): a `runtime.ready` span around the
gate; one `runtime.ready.attempt` note per attempt, giving its duration, its outcome (exit code,
still running, or interrupted) and its first stderr line, truncated; and `podman.facts` from the
gate's answer. Every event is also written on the failure path, before the refusal returns.

### The machine-wide launch log

Every launch, refused or not, writes one line to `launches.log` in yolo's machine-wide logs
directory, beside `crossings.log` ([OQ-PR3](#OQ-PR3), [PR-D17](#PR-D17)). The motivating
incident could not find its failing launch: launch records lived only in each workspace's `.yolo/`,
and the crossing log records only jails that reached a loophole, which a launch refused at runtime
selection never does.

The line, written once, the moment the launch's fate is known:

```text
<start, UTC> launch jail=<short code> runtime=<rt> podman_wait=<s> tries=<n> outcome=<started|attached|not-started|interrupted> rc=<n> after=<s>
```

with `-` for a field the launch has no value for. The **short code** is `paths.JailShortHash` of
the container name, the key the crossing log uses. `started` means the runtime was spawned (or the
macos-user backend handed the launch), written before the container wait. The time is the launch's
start, which is what orders a storm. A launch ended by a signal after pack staging — through its
launch guard, or through the handler that covers it from the keeper's spawn until the keeper
reports the runtime spawned — writes `interrupted` with 128 plus the signal's number, as does an
outer launch whose inner launch (a capture jail's) ends the process on a signal.

- **Always on.** A record written only when timing is on is off exactly when an unplanned reboot
  happens.
- **Never workspace names.** A jail that mounts the logs directory would learn the names of the
  user's other projects. The short codes are opaque but stable, and a jail that mounts the
  directory sees every launch under them, which the maintainer accepted: *"I don't see why we're
  worried about a jail getting access to that log. It's just like anything else on the host, it's
  outside of their view."*
- **Rotation** is one `.1` generation past a size cap, under one `flock` with the write, taken on a
  sibling lock file that is never rotated: a lock on the log's own descriptor locks the inode being
  renamed, and two launches at the boundary deleted the archived generation.

**The host notch writes the same line** ([PR-D25](#PR-D25)): every `yolo host -- <cmd>` past its
argument check writes `runtime=host podman_wait=- tries=-`, with `jail=` the short code of the
directory the command ran in. `outcome=started rc=-` is written immediately before the hand-over,
because an exec leaves no process to write it after; every earlier return writes
`outcome=not-started rc=<n>`. A usage error refused at the argument check writes none, and a signal
arriving before the line is written leaves none.

**The host notch's own launch log**, `host-launch.log` in the same directory, applies the same rule
to every line it keeps ([PR-D26](#PR-D26)): an argv disclosure names each argv by its program and
argument count (`you asked for: codex <2 arguments>`), a program typed as a path by its base name,
and the directory the command ran in, or a path under it, as `<cwd>` — unless that directory is the
root, the home or above the home, which names no project. Every line names its launch by its
process id (`=== yolo host launch <time> (pid <n>) ===`, `(pid <n>)` on each body line, and the
`handed over` and `launch done` trailers), because concurrent host launches are ordinary and a
resident launch's trailer lands below later launches' blocks. The terminal still gets every line
whole.

## Housekeeping, one deletion at a time

After a reboot every pre-reboot jail's leftovers are due at once, so the first restored launch runs
its biggest reaping pass while the other workspaces still need Podman — and the pass used to hold
the machine-wide housekeeping lock, which every other launch's image load takes around its
re-inspect, for its whole duration. Now the slot holds that lock one deletion at a time, and
nothing is skipped ([OQ-PR2](#OQ-PR2)):

- **Two locks** ([PR-D14](#PR-D14)). A pass lock is taken non-blocking for the whole pass, so a
  second pass skips rather than waits and two passes never interleave. The shared housekeeping lock
  is taken blocking around each deletion through a `prune.Guard` and released between deletions,
  so a launch's re-inspect waits at most one deletion.
- **Each class rechecks under the lock, right before each deletion, only what a launch can change
  while a pass runs** ([PR-D15](#PR-D15)): images against the current-image pointers and the load
  sentinel a launch records under the same lock; store outputs against their roots; caches, image
  tars and delivery directories by a fresh stat; agent staging by its age floor and tracking file;
  retired loophole state by presence and age; flake-bundle generations by the stable link and the
  grace period. None asks the runtime again per item, and image removal already refuses an image a
  container uses.
- **The scratch-volume class deletes nothing in the slot** ([PR-D16](#PR-D16)): it starts the
  detached remover, which takes no housekeeping lock, since nothing on the load side reads a
  volume.
- The manual `yolo prune` takes neither lock and passes no guard.

> [!WARNING]
> **Mixed yolo versions can interleave passes.** A pass from a yolo older than the per-deletion
> lock knows only the shared lock and holds it for its whole pass. An old pass skips while a new
> one is mid-deletion, but between a new pass's deletions it finds the shared lock free and runs
> whole; their deletions never overlap, and each later deletion of the new pass rechecks after it.
> Keeping them fully apart would mean holding the shared lock for the whole pass, which
> [OQ-PR2](#OQ-PR2) ruled out.

> [!WARNING]
> **Do not skip the slot on a slow probe, or during a window after boot.** Both guess Podman's load
> from a symptom, or stand a timer in for a cause. The maintainer: *"why are we going to skip a
> cleanup pass? … You're trying to, like, guess the load from what happens? That sounds like a bad
> idea."* And nothing shows housekeeping slowed anyone's probe; what is measured is the lock hold on
> the load path, which the per-deletion lock removes.

## Testing

Every test below runs under `go test -short`, and each fails if its call site goes.

- **Through runtime selection**, once down each way a launch reaches it (`YOLO_RUNTIME`, the
  config's `runtime` key, autodetection), in `internal/cli/run/podmanready_test.go`: a podman busy
  until the last attempt the budget allows; an attempt still running at the end, refused with its
  pid and left running; every early exit printed and the last in the refusal; a permanent error
  refused at once; an unstartable binary refused, and a busy one retried; Apple Container's one
  probe and Podman Machine's patient one-shot (`podmanmachine_test.go`); the span and notes on a
  refusal; a Ctrl-C exiting 130, once through the gate's interrupt seam and once as a real `SIGINT`.
- **One `podman info` per launch**, across a whole fake launch through `run.Run`
  (`TestALaunchAsksPodmanOnceAndEveryReaderTakesTheGatesAnswer`): the gate asks, the host-loopback
  decision and the image copy's store facts read its answer, and a rootless pasta host that
  recovered inside the gate gets the forwarding option on its argv — the wiring, not the network.
- **`yolo check`** (`internal/cli/check/podmanready_test.go`, `podmanmachine_test.go`,
  `section_imagedelivery_test.go`, `section_iopriority_test.go`): one gate per check, read by every
  section that needs Podman.
- **The attach decision**: a `ps` that exits 125 refuses; Apple Container's `container ls` keeps the
  same tri-state; the refusal names each runtime's list command.
- **The attempt runner, on real subprocesses** (`internal/runtime/podmanattempt_test.go`): it
  returns when a child exits though a grandchild holds its output; an attempt past its deadline is
  left running in its own process group; an interrupt stops the wait and not the process; and, on
  Linux, a binary busy being written is retried until it answers.
- **The classification**, row by row against podman's own texts (`TestClassifyPodmanFailure`) and
  errno by errno (`TestClassifyStartError`), and the retry floor
  (`internal/runtime/podmanready_test.go`).
- **The housekeeping slot** (`internal/cli/run/housekeeping_test.go`, `internal/prune/guard_test.go`,
  `internal/flakebundle/flakebundle_test.go`): passes never interleave; the shared lock is held only
  around each deletion; a deletion waits for a launch's hold and rechecks after it.
- **The launch line** (`internal/cli/run/launchrecord_test.go`, `hostlaunchlog_test.go`): every
  outcome above, rotation under a crowd of launches at the boundary, and the host notch's line and
  redactor. The package's `TestMain` starts every test in a `HOME` of its own, so no test writes the
  machine's real log.

Each test package's `TestMain` makes the real attempt runner refuse ([PR-D20](#PR-D20)), so no unit
test can reach the machine's podman.

A nested jail partly exercises the gate, because its first Podman command is a cold refresh. But a
nested Podman is forced rootful, so it takes the rootful row of
[the kill table](#why-a-timed-out-probe-is-not-a-failed-podman), never the rootless re-exec row.

### The real-host check, still owed

The maintainer's, on a rootless Linux host:

1. **Reproduce the cold refresh.** As a user with no running containers, delete `alive` from the
   rootless engine tmpdir to force a refresh with several stopped `--rm` jails present, then time a
   capped `podman info` against an uncapped one. A capped one that reports a timeout while its child
   finishes confirms the inferred reading of [the incident](#the-2026-09-29-reboot), and the
   uncapped time is the refresh duration that sizes the budget ([PR-D12](#PR-D12)). In the same
   session, time a `podman info` started during a `podman volume rm` of a large volume, to settle
   whether storage contention reaches `info` at all.
2. At a real reboot, let the restorer start its usual set, six or seven workspaces within half a
   minute plus a later straggler. Each must start or refuse within the budget. Read `runtime.ready`
   from each workspace's log, and the machine-wide launch log.
3. Record, per launch: host uptime, `loginctl show-user --property=Linger`, the launch's
   `XDG_RUNTIME_DIR`, `podman info --format '{{.Host.Security.Rootless}}'`, and the active Podman
   user and system units (`podman-restart`, quadlets) — the actors the gate is deliberately blind
   to.

## The 2026-09-29 reboot

The incident that motivated the gate, in brief. At a reboot, a session restorer relaunched six
workspaces within 44 seconds and a seventh five minutes later. One launch was refused with
`Cannot query container runtime` after a 22.5-second progress line, before its container existed,
with Podman stderr about cleaning up old volumes and refreshing a removed container (REPORTED; not
in any log a jail could read). The most likely reading (INFERRED, fitting every measured number):
that launch was the first Podman client after boot, so its `podman info` did the post-boot refresh;
the old code killed the probe's direct process at its timeout; Podman's rootless re-executed child
survived and finished the refresh; and the killed probe's `Wait` returned at the same moment, so
yolo reported a timeout just as Podman became ready. The alternative reading — a second refresh
with no reboot, after the runtime directory under `/run/user/<uid>` was removed with linger off —
changes nothing: both are a `podman info` doing the refresh, killed.

Separately, a launch that passed the old probe ran more `podman info` queries of its own, and the
first restored launch held the housekeeping lock for its whole reaping pass (about 16 seconds), on
which every other launch's image load blocks. Those are what [the gate's single
answer](#what-reads-the-gates-answer) and [the per-deletion lock](#housekeeping-one-deletion-at-a-time)
remove.

## Why it's this way

Rulings and decisions a maintainer would otherwise undo, keeping their original ids, which code
comments and sibling docs cite. Three standing rules bind the rows that cite them: a launch has no
quiet mode ([OQ-RO3](report-tiers.md#why-its-this-way)); "could not ask the runtime" is never a yes
(the tri-state rule in [AGENTS.md](../../AGENTS.md#architecture)); and an escape hatch is for broken
user config, never for a yolo bug
([attach-skew-and-contract-guardrails.md](attach-skew.md#why-its-this-way)).

| ID | Ruling |
| :--- | :--- |
| <a id="OQ-PR1"></a>[`OQ-PR1`](#OQ-PR1) | **Maintainer ruling: the budget applies to every launch and `yolo check`, never keyed on a reboot; answers that cannot clear on their own fail at once.** *"We definitely shouldn't detect a reboot, so we certainly shouldn't do something only after a reboot. But ideally we can distinguish between 'nobody attempted to start Podman ever' or 'it's not installed and it's just not responding' so we don't wait for something to come up that's never going to come up."* A boot window is a timer standing in for a cause: a refresh outside it, or any slow `info`, would get the old refusal back. The fail-fast rule is [PR-D13](#PR-D13) |
| <a id="OQ-PR2"></a>[`OQ-PR2`](#OQ-PR2) | **Maintainer ruling: the housekeeping slot holds the lock one deletion at a time; no skip, no load inference.** Built as [PR-D14](#PR-D14) to [PR-D16](#PR-D16) |
| <a id="OQ-PR3"></a>[`OQ-PR3`](#OQ-PR3) | **Maintainer ruling: one machine-wide line per launch, keyed by the workspace's short code**, never its name. Built as [PR-D17](#PR-D17); extended to the host notch by [PR-D25](#PR-D25) and [PR-D26](#PR-D26) |
| <a id="PR-D1"></a>PR-D1 | **No yolo readiness lock of any scope.** Podman's `alive.lck` already serializes the refresh, so a yolo lock would only queue each waiter's cheap verification behind it, and a queued waiter could fail with no Podman fault at all. Revisit only on a measured storm in which concurrent probes lengthen recovery |
| <a id="PR-D2"></a>PR-D2 | **An attempt is never killed.** Its only bound is the remaining budget; retries follow only an early exit (`TestAPodmanStillRunningAtTheBudgetIsNamedAndLeftRunning`) |
| <a id="PR-D3"></a>PR-D3 | **The probe's output goes to unlinked temp files, and the probe runs in its own process group.** A surviving descendant cannot hold `Wait`, closing an output end cannot SIGPIPE a refresh, and a terminal Ctrl-C does not reach Podman (`RunPodmanAttempt`; `TestTheAttemptReturnsWhenTheChildExitsThoughAGrandchildHoldsItsOutput`) |
| <a id="PR-D4"></a>PR-D4 | **An early exit is retried after an escalating, capped backoff that never runs past the budget**, except an answer [PR-D13](#PR-D13) or [PR-D23](#PR-D23) classifies as unable to clear, and subject to the retry floor ([PR-D22](#PR-D22)) |
| <a id="PR-D5"></a>PR-D5 | **The gate's probe is `podman info --format json`, and its parsed answer is the launch's one Podman facts record**, read by every later reader on the launch path. One `info` per launch, pinned by a whole-launch test |
| <a id="PR-D6"></a>PR-D6 | **`yolo check` calls the same gate, with the same budget and progress line**, so check and the launch cannot disagree about whether Podman is up. A shorter budget for check would bring that disagreement back |
| <a id="PR-D7"></a>PR-D7 | **Linux Podman only; Apple Container keeps its one-shot probe**, with an explicit macOS arm pinned by a test. Amended by [PR-D24](#PR-D24) for Podman Machine |
| <a id="PR-D8"></a>PR-D8 | **The attach decision's `ps` is tri-state, with a deadline; "could not ask" refuses the launch** rather than launching fresh. The other `findRunningContainer` callers are unchanged |
| <a id="PR-D9"></a>PR-D9 | **The wait, retry and success lines go on the launch stream; the refusal stays with the other runtime-selection refusals. Neither can be hidden** ([OQ-RO3](report-tiers.md#why-its-this-way)) |
| <a id="PR-D10"></a>PR-D10 | **Perf events: the `runtime.ready` span, `runtime.ready.attempt` notes, and `podman.facts` from the gate**, all written on the failure path too |
| <a id="PR-D11"></a>PR-D11 | **The budget is a constant: no config key, no `YOLO_*` dial.** A budget that is too short would be a yolo bug, and hatches are for broken user config only |
| <a id="PR-D12"></a>PR-D12 | **The budget's size** covers the inferred refresh with a wide margin, room for its retries, and the whole measured restore storm. It stands until the real-host check's step 1 measures the forced refresh |
| <a id="PR-D13"></a>PR-D13 | **An early exit is classified from podman's fatal lines only** (`Error: …`, and the bare lines its rootless C code prints before exiting), never its logrus lines, since a refresh logs an error for one container and still succeeds. The table (`runtime.podmanFailurePatterns`) is podman's own texts, with each group's source file named. **Refused at once:** a refused or disabled user namespace, broken or missing `newuidmap`/`newgidmap`, a subuid mapping error, BoltDB removed or left behind, any request for `podman system migrate`, a database configuration mismatch, an invalid `containers.conf` or `storage.conf`, a missing OCI runtime or conmon, a permission error, an unknown flag. **Retried:** a locked database, `EAGAIN`/`EBUSY`/`EINTR`/`ETXTBSY`, a timeout, and everything the table does not know. "No space left on device" and "cannot connect" are deliberately absent: yolo's own scratch remover may be freeing space, and a socket can come up late at boot. A transient line wins over a permanent one in the same answer |
| <a id="PR-D14"></a>PR-D14 | **Two housekeeping locks**: a pass lock taken non-blocking for the whole pass, and the shared lock taken blocking around each deletion through a `prune.Guard` and released between deletions |
| <a id="PR-D15"></a>PR-D15 | **Each class rechecks under the shared lock, right before each deletion, only what a launch can change while a pass runs**; none asks the runtime again per item |
| <a id="PR-D16"></a>PR-D16 | **The scratch-volume class stays outside the guard by design**: it deletes nothing in the slot, and nothing on the load side reads a volume |
| <a id="PR-D17"></a>PR-D17 | **The machine-wide launch log's file, line, rotation and lock** as [described above](#the-machine-wide-launch-log): one line per launch, refused or not, the three guards `Run` runs before any other work included, written once when its fate is known; signal exits through the launch guard and the keeper's handler write it too (`run.interruptedArmExit`) |
| <a id="PR-D18"></a>PR-D18 | **The attach decision's deadline is short and fixed**: it runs right after the gate heard podman answer, so it waits only on another launch's container create, not a refresh. Apple Container's `container ls` takes the same tri-state |
| <a id="PR-D19"></a>PR-D19 | **`yolo check` asks the gate once and keeps the answer; its progress goes to stderr**, because stdout may be JSON; the runtime section's failure carries the start hint first and the gate's refusal after it; check catches no Ctrl-C, and the probe, in its own process group, runs on |
| <a id="PR-D20"></a>PR-D20 | **The gate's parts are seams** (`Options.PodmanReadiness`: the attempt runner, its clock, its sleep and its interrupt), never `Exec`, whose timeout kill is what the gate exists to avoid. Each test package's `TestMain` makes the real runner refuse with an error wrapping `exec.ErrNotFound`, so the refusal is immediate (`refusingPodmanAttempt`) |
| <a id="PR-D21"></a>PR-D21 | **The gate's elapsed time is the larger of its clock and the backoffs it slept**, so a frozen clock still ends the loop at the budget (`TestAFrozenClockStillEndsTheGate`) |
| <a id="PR-D22"></a>PR-D22 | **A retry starts only if the budget left after its backoff exceeds the retry floor**, capped so an error that waited behind a lock cannot cost more than the cap. Without it, a podman failing the same way slowly got a last attempt it was then abandoned on mid-answer, leaving a podman running for nothing and a refusal naming it instead of podman's error |
| <a id="PR-D23"></a>PR-D23 | **An attempt that cannot start is classified by its errno, not its words.** Refused at once: `ENOENT`, `ENOTDIR`, exec's not-found and relative-path refusals, `EACCES`, `EPERM`, `ENOEXEC`, `EISDIR`. Retried: `ETXTBSY`, `EAGAIN`, `ENOMEM`, `EMFILE`, `ENFILE`, and every errno not named. A failure of yolo's own scratch files is a `ProbeScratchError`, refused only when the temporary directory is missing, not a directory, not writable or read-only; a gate that ends on one says podman never ran and names the step its error calls for (free space, the open-files limit, a usable temporary directory), and neither the launch nor `yolo check` adds a podman step after it |
| <a id="PR-D24"></a>PR-D24 | **macOS Podman Machine takes the patient one-shot**, from a CI failure in which the old fixed timeout reported a busy two-CPU machine as "installed but not started". One attempt through the gate's runner, never killed, waited up to the budget, never retried; a still-running attempt is reported as not answering, never as stopped. UNMEASURED on a real Mac: the stopped-machine answer is read from podman's source, not run |
| <a id="PR-D25"></a>PR-D25 | *Implementation decision under the maintainer's 2026-10-04 delegation; reversible.* **Every `yolo host -- <cmd>` past its argument check writes the same launch line**, with `runtime=host`, so a reader of a storm tells the host's launches from the jails' at a glance |
| <a id="PR-D26"></a>PR-D26 | *Implementation decision under the same delegation; reversible.* **The host notch's own launch log applies [OQ-PR3](#OQ-PR3)'s rule to every line**: an argument can be a secret or a prompt, the directory names a project, and the logs directory is one a jail may mount. Every line names its launch by process id |

## Current values

Verified at `8e65f344`. The prose above explains what each of these is for; this table is the only
place the exact values are stated.

| Value | Setting | Defined in |
| :--- | :--- | :--- |
| Readiness budget | 60 s | `runtime.PodmanReadyBudget` |
| Retry backoff | 1 s, 2 s, then 4 s (capped at 4 s) | `runtime.podmanReadyBackoff` |
| Retry floor cap | 5 s | `runtime.retryFloorCap` |
| Progress grace | 2 s | `progress.DefaultGrace` |
| Attach decision deadline | 30 s | `run.attachProbeTimeout` |
| Machine-wide launch log | `~/.local/share/yolo-jail/logs/launches.log`, mode `0600` | `run.MachineLaunchLogName`, `run.MachineLaunchLogPath` |
| Launch log rotation | one `.1` generation past 1 MiB, locked on a sibling `launches.log.lock` | `run.launchLogMaxBytes` |
| Host notch launch log | `~/.local/share/yolo-jail/logs/host-launch.log` | `run.HostLaunchLogName` |
| Housekeeping locks | `housekeeping-pass.lock` (per pass), `housekeeping.lock` (shared, per deletion) | `run.HousekeepingLockPath`, `run.withHousekeepingPass` |
| Interrupt exit status | 130 | `internal/cli/run` |
