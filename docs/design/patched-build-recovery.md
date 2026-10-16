---
status: accepted
stage: DECIDED
next: "Build the six rulings and the four new hatches, then graduate into patched-forks.md and pi-extensions.md"
tags: [packs, forks, patches, recovery, happy-path]
---

# When the series and the upstream both break, is there a way back to a working agent?

**Status:** 2026-10-16. Every question ruled; building. Diagnosis read at `3a2b0956f`, seven
scenarios measured ([Appendix A](#appendix-a-the-measured-scenarios)).

> **In short.** A patched build had an escape for every failure *while something of the current
> series was on this machine*, and none once nothing was. Five opt-in hatches with one grammar, a
> failure that follows upstream's newest, and a hold that holds close every dead end found here.

**Why it matters.** When the agent is the broken thing, the agent cannot repair itself.

**The shape.** One vocabulary of hatches (`internal/patchrecovery`), decided by the advance and
listed by every refusal as copyable commands.

**Start at [§6](#6-the-settled-design)** — the settled design. [§3](#3-the-as-built-path-table) to [§5](#5-escapes-that-exist-and-are-not-named) are the diagnosis it answers.

**Needs your ruling:** None.

**Reads with:** [`patched-forks.md`](../reference/patched-forks.md) (the system reference this
amends; every PF-D id is its), [`pi-extensions.md`](../reference/pi-extensions.md) (the same
machinery over a `files` tree), [`patch-series.md`](../../userguide/guides/patch-series.md) (the
user guide), [`happy-path-principle.md`](../reference/happy-path-principle.md).

---


## 1. Verdict

My read, from the code and the scenarios:

1. **The suspicion is right, and it is wider than suspected.** With nothing of the current series
   on the machine, a conflict at upstream's newest version is a launch nobody can start with a
   working agent by any flag. That holds whether the cause is a first install, an edited series or
   a wiped store, and for a pi extension it holds even when an older series' build is on the
   machine ([§4.2](#42-d2-nothing-of-the-current-series-is-here-and-the-newest-version-conflicts)).
2. **A second dead end nobody suspected**: a recorded conflict is never re-examined against a
   *newer* upstream release. A release that the series takes cleanly is not built, by any launch,
   `yolo pack update` or `yolo capture`, until the user rebases onto the old, failing version
   ([§4.1](#41-d1-the-sticky-failure)).
3. **The walk-back is cheap, because it exists.** The advance's walk replays down the version
   list and takes the newest fit ([PF-D10](../reference/patched-forks.md#PF-D10)); since
   [PF-D81](../reference/patched-forks.md#PF-D81) every caller passes `StopOnPatchFailure: true`,
   which ends the walk at the first conflict (`packsrc.WalkOptions`, `internal/packsrc/replay.go`).
   The hatch turns that one option off for one launch, and lets a failed build continue down the
   same list.

Build the sticky fix by default ([OQ-PBR1](#decision-ledger)) and the walk-back as a hatch
([OQ-PBR2](#decision-ledger)). Both fit the maintainer's escape-hatch rule: an upstream that moved
incompatibly or broke its build is external state, not a yolo bug.

## 2. Terms

- **Patched build** — a patched fork's program or a patched extension's tree: one built from the
  upstream plus a `patches` series or a `patch_branch`. Both run through one advance
  (`advancePatchedFork`, `internal/cli/patchedadvance.go`), so every cell below holds for both
  unless a row says otherwise.
- **Good build**, **candidate**, **advance**, **walk's list**, **newest fit**, **held** — as defined in
  [`patched-forks.md` §6](../reference/patched-forks.md#6-the-good-build-the-record-and-the-store-key).
- **Serves** — the good build's series digest and recipe are the manifest's *as it stands*, and its
  store entry is intact (`advance.serves`). An edited series, `build` or `produces` means nothing
  serves, even though the old good build's entry is still in the store
  ([PF-D23](../reference/patched-forks.md#PF-D23)).
- **Recorded patch failure** — the typed failure the check record keeps (`CheckRecord.PatchFailure`):
  the target version, the member, the paths. Every later launch re-reads it.
- **Older good build** *(coined here)* — the record's good build when it no longer serves because
  the series or recipe was edited: the build `YOLO_USE_CACHED_GOOD` runs
  ([PF-D82](../reference/patched-forks.md#PF-D82)).
- **Working agent** *(coined here)* — the launch starts the jail *and* the patched program (or the
  agent loading the patched extension) runs. A jail whose agent refuses to start is not one.
- **Walk-back** *(coined here)* — the proposed opt-in advance that tries the series on successively
  older versions from the walk's list, down to the series' base, until one builds
  ([§6.4](#64-the-walk-back)). Not a rollback: it records nothing as the newest and moves no
  pin.

## 3. The as-built path table

Each cell is a fresh jail launch on podman unless its section says otherwise. **VERIFIED** means read
in the code, a test pins it (named), or a scenario of [Appendix A](#appendix-a-the-measured-scenarios)
measured it; **INFERRED** means read from documents or from code paths I did not drive.

### 3.1 The flags and commands that exist

| Escape | What it does | Where it works |
| :--- | :--- | :--- |
| `YOLO_ALLOW_PATCH_FAILURES=1` | Runs the intact admitted build of the series *as it stands* for one run, past a recorded patch failure. Literal `1` only (`allowPatchFailures`, `internal/cli/patchfailure.go`) | jail, `yolo host`, `yolo capture`, `yolo pack update` |
| `YOLO_USE_CACHED_GOOD=<pack>/<bin>` | Runs the record's good build whatever its recipe, for one fresh launch, after validating its entry and receipt (`runCachedGoodFork`, `internal/cli/cachedgood.go`) | fresh podman or Apple Container launch, **forks only**; refused on an attach, in a jail, on macos-user, with `--update`, and for an extension key (`selectCachedGood`, `internal/cli/run/cachedgood.go`) |
| `YOLO_ALLOW_MISSING_PROGRAMS=1` | Starts the jail without the missing build. The fork's in-jail launcher, and the owning agent's launchers for an extension, still stop (`treeGateFor`, `internal/entrypoint/patchedtrees.go`) | jail; `yolo host` for another program |
| `--update` / `-u` | A fresh check and wait. Refuses every bypass and names launching without it (`refuseFreshPreparation`, `freshRefusal`) | jail, `yolo host` |
| `yolo pack update` | Forced check and replay; stops at the first conflict; builds nothing | host |
| `yolo capture <key>` | Forced check, builds the pending candidate; stops at a recorded patch failure | host |
| `yolo pack series check [--onto <ref>]` | Replays the series on the newest version, or on one named version, in a scratch mirror | host and jail |
| `yolo pack status` | Prints the record, the failure and the rebase recipe, offline | host |
| `agent_updates: {<pack>: false}` | A hold: no check, the good build serves | user config |
| `?ref=<tag>` on `source` | Holds the upstream at a version | the pack manifest's **owner only** ([`patched-forks.md` §3.4](../reference/patched-forks.md#34-holding-a-patched-fork)); I found no user-scope override |
| Revert the series edit | The old good build serves again | the pack's files |
| Drop the pack, or the list entry; an extension's `fallback` | Runs without the patched build | config or manifest |

### 3.2 Fresh jail launch, a patched fork

Inputs: **E** — the series or recipe was edited since the good build; **N** — what the newest
pending version does; **G** — what is on this machine.

| Cell | E | N | G | The launch does | Its next step names | What reaches a working agent | Evidence |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| C1 | no | nothing new, inside the hour | serves | runs the good build, no git | — | — | VERIFIED: `pendingOf`, P4 |
| C2 | no | applies and builds | serves | builds, moves, hands the new build | — | — | VERIFIED: scenario 1's last launch |
| C3 | no | applies, build fails | serves | runs the good build; backs the candidate off | `yolo capture`; `agent_updates`; a tag ref | nothing needed | VERIFIED: `TestAFailedBuildBacksOffWhileTheGoodBuildServes` |
| C4 | no | conflicts | serves | refuses | the rebase recipe; `YOLO_ALLOW_PATCH_FAILURES=1` | the named bypass | VERIFIED: `TestAConflictingTagFailsAndOnlyExplicitBypassContinues` |
| C5 | no | after C4, a newer release that fits | serves | **still refuses, at the old target** | the recipe onto the *old* target; the bypass | the bypass, forever on the old build | VERIFIED: scenarios 1 and 6 — [D1](#41-d1-the-sticky-failure) |
| C6 | no | fetch fails | serves | runs the good build, warns | `yolo pack update` | nothing needed | VERIFIED: `advance.run` |
| C7 | no | apply error (git too old, missing blob, replay timeout) | serves | runs the good build; retried at the next check | updating git, `yolo pack update` | nothing needed | VERIFIED: `TestAnApplyErrorIsRetriedByTheNextCheckNotEveryLaunch` |
| C8 | — | applies and builds | nothing | builds and hands it | — | — | VERIFIED: `TestAFirstAdvanceBuildsTheNewestFitAndHandsIt` |
| C9 | — | conflicts | nothing | refuses; no older fit, no base | the recipe; `YOLO_ALLOW_MISSING_PROGRAMS=1` | **none** | VERIFIED: `TestPatchFailureStopsBeforeAnOlderFitBuilds`, scenario 2 — [D2](#42-d2-nothing-of-the-current-series-is-here-and-the-newest-version-conflicts) |
| C10 | — | build fails; the base builds | nothing | builds the base and holds there | — | — | VERIFIED: `TestAFitThatFailsToBuildWithNothingServingBuildsTheBase` |
| C11 | — | build fails, and at the base | nothing | refuses; **every** launch rebuilds both | `yolo capture`; `YOLO_ALLOW_MISSING_PROGRAMS=1` | **none** | VERIFIED: scenario 7 — [D4](#44-d4-nothing-of-the-current-series-builds-anywhere-it-is-tried) |
| C12 | — | fetch fails | nothing | refuses; fetches again at every launch | the network | none until the network is back | VERIFIED: `RetryFailedFetch: !a.serves()` in `advance.run` ([PF-D88](../reference/patched-forks.md#PF-D88)); the refusal's words INFERRED |
| C13 | — | no version tag, or every version predates the base | nothing | builds the base | `follow: "head"` | — | VERIFIED: `TestATaglessUpstreamBuildsTheSeriesBaseAndSaysWhyOnce` |
| C14 | yes | applies and builds | older good | builds, moves | — | — | VERIFIED: `advance.run`'s edited branch |
| C15 | yes | conflicts | older good | refuses; no older fit, no base | the recipe; `YOLO_USE_CACHED_GOOD=<key>` | the named cached-good launch | VERIFIED: scenario 3 — [D3](#43-d3-the-maintainers-case-an-edited-series-and-a-broken-upstream) |
| C16 | yes | build fails; the base builds | older good | builds the edited series at its base, held | — | — | VERIFIED: `TestAFitThatFailsToBuildWithNothingServingBuildsTheBase` |
| C17 | yes | build fails, and at the base | older good | refuses | reverting the edit; `yolo capture`; `YOLO_ALLOW_MISSING_PROGRAMS=1` | `YOLO_USE_CACHED_GOOD`, **not named** | VERIFIED: scenario 4, `TestAUsersFailedEditTriesTheBaseThenGoes` — [§5](#5-escapes-that-exist-and-are-not-named) |
| C18 | yes | does not apply at its own base | older good | refuses | re-exporting with `--base`; `YOLO_USE_CACHED_GOOD` | the named cached-good launch | VERIFIED: `WalkResult.PatchFailure`'s `base` kind and `patchBypass` |
| C19 | no | any | serves, under an `agent_updates` hold | runs the good build, no git | — | — | VERIFIED: `TestAgentUpdatesHoldsAPatchedForkAtALaunch` |
| C20 | no | after C4, then a hold | serves, held | **still refuses** | the bypass | the bypass | VERIFIED: scenario 5 — [D5](#45-d5-the-hold-the-guide-recommends-does-not-hold-past-a-recorded-failure) |
| C21 | no | after C4, the owner sets `?ref=` to the good build's tag | serves | runs the good build; the recorded failure is dropped | — | — | VERIFIED: scenario 5 |
| C22 | — | the good build's store entry is gone | the record only | rebuilds from its inputs, as a first build | as C8–C13 | as C8–C13 | VERIFIED: `advance.run`'s "gone from the capture store" branch |

"Nothing" in **G** covers a first install, a wiped store (C22) and a record recovered with no entry.
A patch branch takes the same cells: its conflict is a merge conflict, and its edit is a moved
branch (`patchBranchResolved`).

### 3.3 A patched extension

The extension runs through the same advance, so every cell above holds, with two changes:

- **No cached-good.** `YOLO_USE_CACHED_GOOD` refuses an extension key, and `cachedGoodLabel`
  returns nothing for a tree, so C15, C17 and C18 offer `YOLO_ALLOW_MISSING_PROGRAMS=1` instead
  (VERIFIED: `selectCachedGood`, `advance.cachedGoodLabel`).
- **The missing-program hatch does not start the agent.** The owning agent's launchers carry the
  tree gate whatever the hatch says, so pi stops with the shell up
  (VERIFIED: `patchedTreesWire` sets `Stop` from the notch, the owner and `fallback` alone).
  An extension with a `fallback` installs its raw entry instead and stops nothing (XB-D7).

### 3.4 The other notches and timings

| Notch or timing | How it differs | Evidence |
| :--- | :--- | :--- |
| **Attach** | No advance and no refusal: it runs what the running jail was handed. A running jail is itself an escape | VERIFIED: the fork-build slot sits below every attach site in `run.go` |
| **`yolo host`, Linux** | The same advance and cells. No cached-good: `patchBypass` returns before offering it at the host. The pre-write check refuses before the first write (`hostPatchPreflight`). An edited series that does not build removes the floor's installed copy | VERIFIED for the bypass and the pre-write check; the removal INFERRED from `hostfloor/patched.go`'s comment |
| **Apple Container** | When liveness is unknown the advance only checks and leaves the candidate pending (`deferAppleCandidate`); with nothing serving the launch refuses naming `yolo capture` once other jails stop. Cached-good works | VERIFIED: `deferAppleCandidate`, `selectCachedGood` |
| **macos-user** | No patched fork or extension is delivered at all, and cached-good is refused; outside this design ([`patched-forks.md` §9](../reference/patched-forks.md#9-notch-coverage)) | VERIFIED: `selectCachedGood` |
| **`agent_updates: "next-launch"`** | A key with a serving build is handed it with no wait; the scheduler advances it in the background. A key with nothing builds in front, as in C8–C13. A failure the background found refuses the next launch (`backgroundPatchFailureFromOutcome`) | VERIFIED for the read of detached failures; the split INFERRED from `deliverTree` and `forkUpdateTiming` |
| **`--update`** | Every bypass is refused for the fresh request, and the refusal names launching without `--update`; cached-good cannot combine with it | VERIFIED: `patchFailureResult`, `selectCachedGood` |

## 4. The dead ends

A dead end here is a cell where no documented flag or command gets a working agent: only editing a
manifest, a series or the config does. Each recipe uses `release` following on a branch.

### 4.1 D1: the sticky failure

**Repro.** Good build at v1.1.0. Upstream tags v1.4.0, which conflicts: the launch refuses (C4).
Upstream then tags v1.5.0, which the series takes cleanly. Every later launch still refuses at
v1.4.0, and so does `yolo pack update`; `yolo capture` fails too; `YOLO_ALLOW_PATCH_FAILURES=1`
runs v1.1.0 and never builds v1.5.0 (scenarios 1 and 6).

**Why.** A recorded failure stays current while its target is anywhere on the check's list
(`CheckRecord.CurrentPatchFailure`). Under `release`, the list is every version tag containing the
series' base, so an old tag never leaves it. At each check that ran, the advance replays *only* that
target, meets the same conflict, and stops (`advance.run`'s retry of `cachedFailure`).

**What escapes it.** Only a manifest owner's round trip: set `?ref=v1.1.0`, launch once (the changed
inputs drop the record's failure), set `?ref=main` back; the next check walks from v1.5.0 and builds
it (scenario 5). A user of a shipped or fetched pack has no ref to edit. Rebasing works, but onto
v1.4.0, the version the recipe names, not v1.5.0.

### 4.2 D2: nothing of the current series is here, and the newest version conflicts

**Repro.** A first install, or a wiped store. Upstream's newest version conflicts; v1.1.0 and the
base would both take the series (scenario 2).

**What happens.** Nothing is built ([PF-D81](../reference/patched-forks.md#PF-D81): "no older fit or
base will be built"). The only bypass offered is `YOLO_ALLOW_MISSING_PROGRAMS=1`, which starts the
jail with the fork's launcher refusing. With [D1](#41-d1-the-sticky-failure), a later fitting release
does not end it either.

**What escapes it.** Rebasing the series, or pinning `?ref=` to an older tag as the owner. For a
pi extension with no `fallback`, the same, and pi does not start meanwhile.

### 4.3 D3: the maintainer's case, an edited series and a broken upstream

**Repro.** Good build of series S1 at v1.1.0. The maintainer edits the series to S2. Upstream's
newest, v1.4.0, conflicts with S2 (scenario 3).

| Subject | What the launch offers | Working agent? |
| :--- | :--- | :--- |
| Patched fork, jail | `YOLO_USE_CACHED_GOOD=<pack>/<bin>`, which runs S1 at v1.1.0 | **yes**, without the edit |
| Patched fork, `yolo host` | "Bypass: none on this machine" | **no** |
| Patched extension, jail or host | `YOLO_ALLOW_MISSING_PROGRAMS=1`, and pi stops | **no** |

In every row S2 is never tried on v1.1.0 or on its own base, though S2 may well apply there. The
user wanted S2; the only route to it is rebasing S2 onto v1.4.0 by hand while the agent that would
help is the one that will not start.

### 4.4 D4: nothing of the current series builds anywhere it is tried

**Repro.** Upstream's newest applies but fails to build (a broken release), and the base build fails
too, for instance because the base is old enough that its toolchain no longer works (scenarios 4
and 7).

**What happens.** The refusal names `yolo capture`, reverting an edit and
`YOLO_ALLOW_MISSING_PROGRAMS=1`. Only two versions are ever tried, the newest fit and the base,
never the releases between them. With no good build, every fresh launch rebuilds both with no
back-off (scenario 7 counted four builds in two launches), each bounded at 20 minutes.

**What escapes it.** For a fork with an older good build, `YOLO_USE_CACHED_GOOD` works, unnamed
([§5](#5-escapes-that-exist-and-are-not-named)). Otherwise only a ref pin by the owner.

### 4.5 D5: the hold the guide recommends does not hold past a recorded failure

[`patch-series.md`](../../userguide/guides/patch-series.md#commands) says *"To keep running what you
have, turn `agent_updates` off for the pack, or put a tag in `?ref=`."* After a recorded conflict,
the hold changes nothing: the launch still refuses (scenario 5). The code chooses this on purpose
(`advance.run`: *"A hold suppresses Git and repair retry, never already-classified failure
authority"*), and the guide does not say it. Not strictly a dead end, since the bypass runs the good
build, but the obvious "make it stop" move does not.

## 5. Escapes that exist and are not named

Each is a breach of [the happy path principle](../reference/happy-path-principle.md): the stop names
a next step, but not the easiest one that works.

| Where | The escape the stop leaves out | Evidence |
| :--- | :--- | :--- |
| C17: an edited fork series whose builds fail with an older good build here | `YOLO_USE_CACHED_GOOD=<key>`. `finish` sets the delivery's cached-good label only beside a patch failure, and `refuseMissingBuilds` never names the variable | VERIFIED: scenario 4 printed none |
| C5: the sticky failure | That a newer version exists that the series may take, and the ref round trip. The recipe and `yolo pack status` rebase onto the stale target | VERIFIED: scenario 1's block |
| C9 and C15 for an extension | The offered `YOLO_ALLOW_MISSING_PROGRAMS=1` starts the jail, not pi; the block says *"starts without it"*, true of the jail only | VERIFIED: `PatchBypass.lines`, `treeGateFor` |
| C20 | That the hold does not lift a recorded failure, and what does | VERIFIED: scenario 5 |

**Where the documents disagree with the code** (the code wins; fix the documents once the rulings
land):

- [`patched-forks.md` §6.2](../reference/patched-forks.md#62-the-check-record-and-what-is-pending)
  says a did-not-apply key is *"for (entry, series digest, yolo version, git version)"* and *"no
  launch replays that key again"*. The typed failure keys on the check inputs and the series digest
  only (`PatchFailure`), and its target is replayed again at every check that ran.
- [`patch-series.md`](../../userguide/guides/patch-series.md#commands)'s `agent_updates` advice, as
  in [D5](#45-d5-the-hold-the-guide-recommends-does-not-hold-past-a-recorded-failure).

## 6. The settled design

Every ruling below is the maintainer's of 2026-10-16 unless the [Decision Ledger](#decision-ledger)
marks it an implementation decision.

### 6.1 The hatches, and their one grammar

| Variable | Gets the launch running by | New? |
| :--- | :--- | :--- |
| `YOLO_ALLOW_PATCH_FAILURES` | running the intact admitted build of the series as it stands past a recorded patch failure, this run | existing; now scoped |
| `YOLO_USE_CACHED_GOOD` | running the record's **older good build** for one fresh jail launch, a fork's **or an extension's** ([§6.7](#67-cached-good-for-extensions)) | existing; now scoped, now trees too |
| `YOLO_ALLOW_OLDER_UPSTREAM` | the **walk-back** ([§6.4](#64-the-walk-back)) | new |
| `YOLO_SKIP_PATCHES` | building and running the upstream with no series applied ([§6.5](#65-skip-the-patches)) | new |
| `YOLO_SKIP_EXTENSIONS` | starting the owning agent without a built extension ([§6.6](#66-skip-an-extension)) | new |
| `YOLO_ALLOW_MISSING_PROGRAMS` | starting the jail without a program it cannot deliver | existing; unchanged, any non-empty value |

**The value grammar** is shared by the five scoped variables (`internal/patchrecovery`):

- `1` covers every patched build the act selects.
- Otherwise a comma-separated list of names. A name covers a patched build when it equals its key
  (`pi-fork/pi`), its pack (`pi-fork`) or its bin or extension name (`pi`). Spaces around a name and
  duplicates are dropped; an empty value is unset.
- **A name that covers nothing selected is a warning line, never a refusal**: a hatch left in a
  shell profile must not stop a workspace that does not select the pack it names. The line lists the
  keys that are here.
- `YOLO_ALLOW_PATCH_FAILURES` used to accept the literal `1` alone, and `YOLO_USE_CACHED_GOOD` exactly
  one key. Both still mean what they meant; the grammar only widens them.

**Who reads them.** A fresh jail launch, `yolo host -- <bin>`, `yolo host apply --assert`, and `yolo
capture` (for the walk-back and skip-patches). Never the background scheduler, never a child
process: no hatch is forwarded or persisted. On an attach, in a jail, or on macos-user (which
delivers no patched build), a set hatch does nothing and the launch says so in one line, except
`YOLO_USE_CACHED_GOOD`, which keeps its existing refusals there.

### 6.2 A recorded failure follows upstream's newest

At a check that ran, **a recorded patch failure governs only while its target is the newest entry on
the walk's list not already passed over.** When the list holds an entry newer than the target, the
advance walks from the newest as if there were no record: a clean newest builds and clears the
failure; a conflicting newest becomes the recorded failure, and the recipe names it. Inside the
check throttle, with no check run, the recorded failure stands. Done looks like: [D1](#41-d1-the-sticky-failure)'s v1.5.0 builds on the next launch whose check runs; a v1.6.0 that
conflicts refuses naming v1.6.0.

### 6.3 A hold holds

Under an `agent_updates` hold, **prior state has no bearing**: a recorded patch failure, the
background's included, never refuses a held launch.

- With a good build that serves, the launch runs it with no git, and its held suffix names the
  release the hold skips when a failure is recorded for one.
- With nothing serving (an edited series under a hold), the series is built at the good build's
  commit as today; a failure recorded for any other version is ignored, and only a failure at that
  commit itself refuses.

### 6.4 The walk-back

`YOLO_ALLOW_OLDER_UPSTREAM` asks one run to try older upstream releases when the newest does not take
the series or does not build.

| Question | Answer |
| :--- | :--- |
| When it acts | For a covered key whose newest pending entry conflicts or fails to build (at a check that ran, or a recorded failure). Not for a `base`-kind failure, and not under a tag or commit `?ref=` |
| Which versions | The walk's list, newest first: version tags under `release`, the tip then the tags under `head`; then the series' base when nothing serves. **Release tags only, never commits between them** |
| Lower bound | With a build of the series as it stands serving, only entries **newer than that build** are tried: the walk-back never replaces a serving build with an older one. With nothing serving, down to the base |
| What it tries | Each entry is replayed; recorded conflicts and entries still backing off are passed over; each clean entry is built until one is admitted |
| Budget | **At most 3 builds per key per run**, the base counting as one; each bounded as any build is |
| Failures | Recorded as today: a conflict per entry, a failed build with its back-off |
| What it records | The admitted build becomes the good build, and the record keeps a **walk-back hold** *(coined here: the newest entry's commit the series did not take, beside the build the walk-back admitted)* |
| What it prints | One disclosure line per key, no flag hiding it: `fork pi-fork/pi: walked back to v1.3.0; upstream v1.4.0 does not take the series` |
| Never | builds a partly applied series; hides the newest's failure; runs in the background |

**The result sticks until upstream changes.** While the walk-back hold names the commit that is still
the newest entry, a launch **without** the variable runs the walked-back good build, held, with that
line as its held suffix, and does not refuse. When a check lists an entry newer than the hold's, the
advance tries the newest again by [§6.2](#62-a-recorded-failure-follows-upstreams-newest): a clean build
moves the good build and clears the hold; a failure refuses, listing its hatches, the hold no longer
covering it. In practice the walk-back rarely builds at all, since a good build is usually here.

### 6.5 Skip the patches

`YOLO_SKIP_PATCHES` covers a patched fork or patched extension: this run delivers an **upstream
build** *(coined here: the upstream's newest list entry built with no series, under the fork's
own build recipe)*.

- **It keeps its own record**: the series' good build, failures, holds and back-offs are untouched, so
  with the variable unset the next launch is exactly what it would have been.
- **It is cached**: a second launch with the variable reuses the store entry, no rebuild, and follows
  the same check throttle.
- One disclosure line: `fork pi-fork/pi: running upstream v1.4.0 without its 3 patches`.
- If the upstream build itself fails, the launch refuses, listing the hatches left.
- Not for an unmodified extension, which has no patches to skip.

### 6.6 Skip an extension

`YOLO_SKIP_EXTENSIONS` covers a built extension, patched or unmodified: for this launch it is not
advanced, built or mounted, **its entry is left out of the owning agent's lists** wherever the launch
renders them (the host's render and the jail's boot), the owner's launchers are not stopped, and the
missing-build refusal does not count it. One disclosure line names each skipped extension. The next
launch without the variable restores it. This is the only hatch that gets pi itself running when its
extension cannot be built, since `YOLO_ALLOW_MISSING_PROGRAMS` starts the jail and pi still looks for
the directory.

### 6.7 Cached-good for extensions

`YOLO_USE_CACHED_GOOD` takes an extension key too: the record's older good build's tree is the
per-launch copy, after the same validation of its entry and receipt a fork's gets. Its refusals stay:
an attach, in a jail, macos-user, `--update`.

### 6.8 Every refusal lists its hatches

Every refusal or warning of a patched build has one shape (ruled 2026-10-16, pinned by golden tests):

```text
ERROR: fork pi-fork/pi: patch series does not apply to upstream v1.4.0 (abcdef01)
  Patch: 0001-x.patch
  Conflict: src/a.ts

Repair:
  git clone … (the recipe)

Start anyway:
  YOLO_ALLOW_PATCH_FAILURES=pi-fork/pi yolo    run v1.1.0 + 2 patches
  YOLO_ALLOW_OLDER_UPSTREAM=pi-fork/pi yolo    build on an older release
  YOLO_SKIP_PATCHES=pi-fork/pi yolo            run upstream without the patches
```

- **One ERROR line, the facts, then copyable commands.** No sentence justifying a hatch, no design
  ids, no "because". The commands are bold cyan and the labels dim when color is on (`tty.Color`).
- **A hatch is listed only when it would get THAT launch running**, with the subject's key as its
  value and the act's own command (`yolo`, `yolo host -- pi`, `yolo capture pi-fork/pi`):

| Hatch | Listed when |
| :--- | :--- |
| `YOLO_ALLOW_PATCH_FAILURES` | a build of the series as it stands serves |
| `YOLO_USE_CACHED_GOOD` | nothing serves, an older good build validates, and the act is a fresh podman or Apple Container launch |
| `YOLO_ALLOW_OLDER_UPSTREAM` | the walk's list holds an entry the walk-back would try ([§6.4](#64-the-walk-back)) |
| `YOLO_SKIP_PATCHES` | a patched build that has no upstream build failure of its own recorded |
| `YOLO_SKIP_EXTENSIONS` | an extension an agent pack loads |
| `YOLO_ALLOW_MISSING_PROGRAMS` | a fork at a jail launch with nothing to run |

**One authority decides the list**: the advance, which knows what serves and what its list holds. It
hands the list on the delivery, and the launch's refusal prints it as is.

## 7. What this does not do

- **No automatic walk-back.** [PF-D81](../reference/patched-forks.md#PF-D81)'s default stands: a
  conflict at the newest stops the operation unless asked.
- **No commit-by-commit bisecting**, and no rollback of a serving build.
- **No pin written anywhere**: no hatch writes the fork lock or a manifest.
- **No repair**: yolo still never edits a patch ([PF-D85](../reference/patched-forks.md#PF-D85)).
- **Nothing for macos-user**, which delivers no patched build at all.

## 8. Alternatives considered

| Alternative | Verdict |
| :--- | :--- |
| Walk-back as the default when nothing serves | Rejected: reverses PF-D81, and the maintainer asked for an opt-in hatch |
| Walk upstream commits between tags | Rejected by the maintainer: "roll back to some sane thing" |
| Walk-back for one launch only | Rejected by [OQ-PBR3](#decision-ledger): the result sticks until upstream changes |
| `YOLO_ALLOW_MISSING_PROGRAMS` starts pi without the extension | Rejected: an empty mountpoint makes pi fail to load the directory; `YOLO_SKIP_EXTENSIONS` drops the entry instead |
| An unknown hatch name refuses the launch | Rejected: a hatch in a shell profile would stop every other workspace |
| A user-scope `source` ref override | Rejected: the hold of [§6.3](#63-a-hold-holds) gives a non-owner the same pin |

## 9. Risks

| Risk | Mitigation |
| :--- | :--- |
| A walk-back or upstream build runs a version missing a security fix | Its disclosure line names the version, every launch |
| A hatch left in a shell profile | The walk-back acts only when the newest fails; skip-patches says so every launch |
| Three failed builds cost an hour | The cap, the per-build bound, Ctrl-C, and the back-off each failure records |
| The newest-follows fix replays on every release | Once per new release, inside a check that already ran |

## Decision ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| OQ-PBR1 | A recorded failure follows upstream's newest. "Number one, yes, for sure. Sounds like a bug." | 2026-10-16 | [§6.2](#62-a-recorded-failure-follows-upstreams-newest) | — |
| OQ-PBR2 | Build the walk-back, opt-in, release tags only. "the walkback should exist. Obviously it should try the latest, but if we can do something older when the latest fails then do it." / "I don't think we should go commit by commit to try to bisect this. We should roll back to some sane thing." | 2026-10-16 | [§6.4](#64-the-walk-back) | — |
| OQ-PBR3 | The walk-back's result sticks until upstream changes. "The walkback's result should stick up until upstream changes and we should try again." | 2026-10-16 | [§6.4](#64-the-walk-back) | — |
| OQ-PBR4 | At most 3 builds per walk-back. "yes, let's take three, but I think in practice it won't be that bad because we will likely already have had a successful one." | 2026-10-16 | [§6.4](#64-the-walk-back) | — |
| OQ-PBR5 | A hold runs what you have, whatever is recorded. "Your prior state should have absolutely no bearing on your current state if you turn off updates or whatever." | 2026-10-16 | [§6.3](#63-a-hold-holds) | — |
| OQ-PBR6 | Every hatch works for forks and extensions alike, cached-good included. "All of my opinions here go for anything that could be patched, the PI agent or PI extensions." | 2026-10-16 | [§6.7](#67-cached-good-for-extensions) | — |
| PBR-R7 | An opt-in hatch that applies no patches: "just use upstream directly" | 2026-10-16 | [§6.5](#65-skip-the-patches) | — |
| PBR-R8 | An opt-in hatch that starts pi without a patched extension | 2026-10-16 | [§6.6](#66-skip-an-extension) | — |
| PBR-R9 | A hatch may name a pack. "Yes, definitely allow naming a specific pack." | 2026-10-16 | [§6.1](#61-the-hatches-and-their-one-grammar) | — |
| PBR-R10 | Refusal output is a normal tool's: "named very clearly and very readable, set off by, you know, a blank line, maybe using colors … I don't want weird prose justifying these decisions in the user output here. Just do something normal like a normal tool would do." | 2026-10-16 | [§6.8](#68-every-refusal-lists-its-hatches) | — |
| PBR-D1 | *Implementation decision (orchestrator, reversible):* every scoped hatch takes `1` or names; every refusal lists every hatch that would get that launch running, with the exact command | 2026-10-16 | [§6.1](#61-the-hatches-and-their-one-grammar), [§6.8](#68-every-refusal-lists-its-hatches) | — |
| PBR-D2 | *Implementation decision (lead, reversible):* the names `YOLO_ALLOW_OLDER_UPSTREAM`, `YOLO_SKIP_PATCHES`, `YOLO_SKIP_EXTENSIONS`, beside the existing `YOLO_ALLOW_*` and `YOLO_USE_CACHED_GOOD`: `ALLOW_` for a hatch that lets yolo do more than it would, `SKIP_` for one that leaves something out | 2026-10-16 | [§6.1](#61-the-hatches-and-their-one-grammar) | — |
| PBR-D3 | *Implementation decision (lead, reversible):* a name covers a build by key, pack or bin/extension name; an unmatched name warns and never refuses | 2026-10-16 | [§6.1](#61-the-hatches-and-their-one-grammar) | — |
| PBR-D4 | *Implementation decision (lead, reversible):* with a build serving, the walk-back tries only entries newer than it, so it never moves a serving build backwards | 2026-10-16 | [§6.4](#64-the-walk-back) | — |
| PBR-D5 | *Implementation decision (lead, reversible):* the advance is the one authority for a refusal's hatch list and hands it on the delivery | 2026-10-16 | [§6.8](#68-every-refusal-lists-its-hatches) | — |

## Appendix A: the measured scenarios

Run 2026-10-16 at `3a2b0956f` as a throwaway `go test` in `internal/cli`, deleted afterwards, on
`newPatchedAdvanceFixture`: an upstream at v1.0.0, a two-member series over lines 10 and 12, a
version that edits line 11 conflicting with the first member. Each "launch" is the fixture's fresh
podman launch advance, two hours apart unless stated.

| # | Steps | Observed |
| :--- | :--- | :--- |
| 1 | good at v1.1.0; tag v1.4.0 (conflicts); launch; tag v1.5.0 (fits); launch | both refuse at v1.4.0; one build in total; good stays v1.1.0 |
| 2 | first install; v1.1.0 fits, v1.2.0 conflicts; launch; tag v1.3.0 (fits); launch | no build at all in either launch; the bypass offered is `YOLO_ALLOW_MISSING_PROGRAMS=1` |
| 3 | good at v1.1.0; tag v1.4.0 (conflicts); edit the series (a subject line); launch | refuses, nothing handed; the delivery offers cached-good `v1.1.0 + 2 patches` |
| 4 | good at v1.1.0; tag v1.3.0 (fits); edit the series; every build fails; launch | v1.3.0 and the base built and failed; nothing handed; no cached-good offered; the lines name reverting the edit and `yolo capture` |
| 5 | scenario 1, then `yolo pack update`; then `agent_updates: {forkpack: false}`; then `?ref=v1.1.0`; then `?ref=main` | update exits 1 at v1.4.0; the held launch still refuses; the tag ref serves v1.1.0 with no failure; back on `main` the launch builds v1.5.0 |
| 6 | scenario 1, then `yolo capture` (forced advance); then `YOLO_ALLOW_PATCH_FAILURES=1` | capture fails at v1.4.0 and builds nothing; the bypass serves v1.1.0, never v1.5.0 |
| 7 | first install; every build fails; launch twice | each launch builds the newest fit and the base: four builds, nothing handed |
