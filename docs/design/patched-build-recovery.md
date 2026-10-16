---
status: in-review
stage: DESIGN
next: "Rule OQ-PBR1 and OQ-PBR2 — the sticky-failure fix and the walk-back hatch are the two that unblock building"
tags: [packs, forks, patches, recovery, happy-path]
---

# When the series and the upstream both break, is there a way back to a working agent?

**Status:** 2026-10-16. Nothing built. Evidence read at `3a2b0956f`; seven scenarios run as a
throwaway test against the advance's own test fixture ([Appendix A](#appendix-a-the-measured-scenarios)).

> **In short.** Today a patched build has an escape for every failure *while something of the
> current series is on this machine*, and none once nothing is. The cure is small because the code
> already walks the upstream's versions newest-first; it stops at the first conflict on purpose.
> Letting a user ask it not to, plus fixing one sticky record, closes every dead end found here.

**Why it matters.** When the agent is the broken thing, the agent cannot repair itself. A pi
extension with no build stops pi even under `YOLO_ALLOW_MISSING_PROGRAMS=1`, and a recorded
conflict outlives the upstream release that fixes it.

**The shape.** Three parts: a default fix to which version a recorded patch failure points at,
an opt-in **walk-back** that builds the series on an older upstream version, and two lines that
name escapes the code already has.

**Cost.** One new environment hatch, one change to how a recorded failure is cleared, and a
changed meaning for an `agent_updates` hold if [OQ-PBR5](#OQ-PBR5) says so.

**Start at [§4](#4-the-dead-ends)** — the dead ends. The proposal falls out of them.

**Needs your ruling:** [OQ-PBR1](#OQ-PBR1), [OQ-PBR2](#OQ-PBR2), [OQ-PBR3](#OQ-PBR3),
[OQ-PBR4](#OQ-PBR4), [OQ-PBR5](#OQ-PBR5), [OQ-PBR6](#OQ-PBR6).

**Reads with:** [`patched-forks.md`](../reference/patched-forks.md) (the system reference this
amends; every PF-D id is its), [`pi-extensions.md`](../reference/pi-extensions.md) (the same
machinery over a `files` tree), [`patch-series.md`](../../userguide/guides/patch-series.md) (the
user guide), [`happy-path-principle.md`](../reference/happy-path-principle.md) (the rule
[§5](#5-escapes-that-exist-and-are-not-named) applies). No implementation sketch is opened yet:
nothing here is settled enough to park build detail.

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

Build the sticky fix by default ([OQ-PBR1](#OQ-PBR1)) and the walk-back as a hatch
([OQ-PBR2](#OQ-PBR2)). Both fit the maintainer's escape-hatch rule: an upstream that moved
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
  ([§6.2](#62-the-walk-back-hatch)). Not a rollback: it records nothing as the newest and moves no
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

## 6. Proposal

### 6.1 Fix the sticky failure, by default

**A recorded patch failure governs only while its target is the newest entry not already passed
over.** At a check that ran, when the list holds entries newer than the target, the advance walks
from the newest, as it would with no record: a clean newest is built and clears the failure; a
conflicting newest becomes the new recorded failure, and the recipe follows it.

- **No flag.** This is what [PF-D81](../reference/patched-forks.md#PF-D81)'s "a member that
  conflicts at the newest version" already says; the sticky target reads that newest as "the newest
  when it first failed".
- **Cost.** One more replay per new upstream version (seconds), and rewriting the tests that pin
  the current selection, among them `TestPatchFailureSelectedFirstFailureWins` and
  `TestPatchFailureCleanDifferentTargetDoesNotClear`, whose intent must be re-read first.
- **Done looks like:** scenario 1 builds v1.5.0 on its next launch, and a v1.6.0 that conflicts
  refuses naming v1.6.0.

### 6.2 The walk-back hatch

**`YOLO_ALLOW_OLDER_UPSTREAM=1`** *(name proposed here)*, beside a command, asks one run to walk
the series back until something of it builds.

| Question | Answer |
| :--- | :--- |
| Who reads it | A fresh jail launch, `yolo host -- <bin>`, `yolo host apply --assert`, `yolo capture`. Never an attach, a jail, the background scheduler, or a child process: it is never forwarded or persisted |
| When it acts | Only for a key with nothing of the current series serving (C9, C11, C15, C17, C18 and their extension forms). With a good build serving it does nothing and its line names `YOLO_ALLOW_PATCH_FAILURES=1`, which already works there |
| Which versions | The walk's list the last check recorded, newest first: the version tags containing the series' base under `release`, the tip and then those tags under `head`. Then the base. **No commits between tags**: there are hundreds, and a tag is what upstream calls a release |
| How far back | To the series' base, which bounds it by construction: the series was written there. A tag or commit `?ref=` gets no walk-back, since the user named the version |
| What it tries | Every entry is replayed, passing over recorded conflicts; each clean entry is built, newest first, until one is admitted. A failed build goes on to the next clean entry instead of straight to the base |
| Budget | At most **3 builds** per key per run ([OQ-PBR4](#OQ-PBR4)), each bounded as today (20 minutes); replays share the existing 60-second replay bound plus one of their own for the base. A Ctrl-C ends it as it ends any advance |
| Failures | Recorded exactly as today: a conflict per entry, a build failure with its back-off. A walk-back passes over an entry backing off rather than paying for a known failure again |
| What it records | The admitted build becomes the good build, held below the newest ([OQ-PBR3](#OQ-PBR3)). The newest entry's recorded failure stays. Nothing is written to the fork lock or the manifest |
| What it prints | Per key, one disclosure line no flag hides: *"walk-back: upstream v1.4.0 does not take 0001-x.patch; v1.3.0 takes the series and built — held at v1.3.0, below upstream's newest"*, then the error block for the newest as today |
| What it never does | Build a series that did not fully apply; build on the host; hide the newest's failure; move a good build that serves; run inside the throttle with nothing to do |

**Returning to following needs nothing.** With [OQ-PBR3](#OQ-PBR3)'s leaning the next launch
without the variable sees a recorded failure and an intact build of the series as it stands, so it
refuses with `YOLO_ALLOW_PATCH_FAILURES=1` offered, as in C4. Once the series is rebased, or upstream
releases a version it takes ([§6.1](#61-fix-the-sticky-failure-by-default)), the ordinary advance
moves the good build forward.

**Every refusal that leaves nothing names the hatch** when the walk's list holds an older entry or
the base, so C9, C11, C15, C17 and C18 each gain one line: *"To build your series on an older
upstream version: `YOLO_ALLOW_OLDER_UPSTREAM=1 yolo`"*.

**Forks and extensions are identical**, through the one advance. For an extension it is the only
flag that yields a working agent ([§3.3](#33-a-patched-extension)).

**Cost.** One environment hatch, one more option on the existing walk, a build cap, the line in the
refusals, and the background scheduler's refusal to read it. Nothing new is stored.

### 6.3 Name cached-good where it works

The missing-build refusal names `YOLO_USE_CACHED_GOOD=<key>` whenever a fork's older good build
validates, not only beside a patch failure (C17). This is the happy path principle applied, not a
new behavior, so it carries no question.

### 6.4 A hold that holds

If [OQ-PBR5](#OQ-PBR5) rules for it: under an `agent_updates` hold with a good build that serves,
a recorded failure for a version the hold declines does not refuse; the launch says it once, in the
held suffix, naming the release the hold skips. Then the guide's advice is true, and the
"one-command pin" the maintainer asked for is that one config key, for every user, owner or not.

## 7. What this does not propose

- **No automatic walk-back.** [PF-D81](../reference/patched-forks.md#PF-D81)'s default stands: a
  conflict at the newest stops the operation unless asked.
- **No rollback of a serving build**, and no pin written anywhere: the walk-back only fills an empty
  slot.
- **No new route around a failed edit's ratchet**: the old series is still served only by
  `YOLO_USE_CACHED_GOOD` or by reverting the edit ([PF-D23](../reference/patched-forks.md#PF-D23)).
- **Nothing for macos-user**, which delivers no patched build at all.
- **No repair**: yolo still never edits a patch ([PF-D85](../reference/patched-forks.md#PF-D85)).

## 8. Alternatives considered

| Alternative | Verdict |
| :--- | :--- |
| Make the walk-back the default when nothing serves | Rejected: reverses [PF-D81](../reference/patched-forks.md#PF-D81)'s ruling that a conflict is never silently replaced by an older fit |
| Walk upstream commits between tags, bisect-style | Rejected: hundreds of builds of unreleased code, each up to 20 minutes |
| Cached-good for extensions | Weaker than the walk-back: it runs the *old* series, and needs a second copy path for trees. See [OQ-PBR6](#OQ-PBR6) |
| Let `YOLO_ALLOW_MISSING_PROGRAMS` start pi without the extension | Rejected: an empty mountpoint at `~/<into>` makes pi fail to load the directory ([`pi-extensions.md` §9](../reference/pi-extensions.md#9-failure-and-the-next-step)) |
| A user-scope `source` ref override | Rejected for now: a second place a pack's address is written, when [§6.4](#64-a-hold-that-holds) gives a non-owner the same hold |
| A `yolo pack walk-back` verb | Rejected: [PF-D85](../reference/patched-forks.md#PF-D85)'s *"either it's automatic or it's not a command"* argues for a hatch on the act that needs the build |

## 9. Risks

| Risk | Mitigation |
| :--- | :--- |
| A walk-back builds a version with a known security fix missing | The disclosure line names the version and that it is below the newest; the held suffix repeats it every launch |
| The hatch is left in a shell profile and every launch walks back | It acts only when nothing serves; after its first admit something serves, so it does nothing more |
| Three failed builds cost an hour | The cap, the per-build bound and Ctrl-C; each failure is backed off, so the next walk-back skips it |
| The sticky fix replays on every release and slows a launch | Once per new release, inside the check that already ran |

## 10. Open questions

1. 💬 **OQ-PBR1: Should a recorded patch failure follow upstream's newest version instead of the version that first failed?**

   Today a conflict at v1.4.0 keeps refusing after v1.5.0, which the series takes, is released
   ([D1](#41-d1-the-sticky-failure)).

   - **Follow the newest** — v1.5.0 is built at the next check; a conflicting v1.6.0 becomes the
     failure. One replay per release.
   - **Keep the first target** — the user must rebase onto v1.4.0, or hold.
   - **Don't build** — and document the ref round trip instead.

   <!-- vantage: question id=OQ-PBR1 leaning="Follow the newest. It is what PF-D81's 'the newest version' says, and the current behavior blocks the one upstream move that would fix things." -->

   _Leaning:_ Follow the newest. It is what PF-D81's "the newest version" says, and the current behavior blocks the one upstream move that would fix things.

   **Answer:**
   > _(empty — fill in when decided)_

2. 💬 **OQ-PBR2: Build the walk-back as an opt-in hatch, acting only when nothing of the current series serves?**

   It is the only flag route to a working agent in [D2](#42-d2-nothing-of-the-current-series-is-here-and-the-newest-version-conflicts),
   [D3](#43-d3-the-maintainers-case-an-edited-series-and-a-broken-upstream) and
   [D4](#44-d4-nothing-of-the-current-series-builds-anywhere-it-is-tried).

   - **Build it, nothing-serves only** — example: edited S2 conflicts at v1.4.0;
     `YOLO_ALLOW_OLDER_UPSTREAM=1 yolo` builds S2 at v1.3.0.
   - **Build it, always** — it also replaces a serving build with an older fit, which
     `YOLO_ALLOW_PATCH_FAILURES=1` already covers.
   - **Don't build** — rebasing by hand stays the only way.

   <!-- vantage: question id=OQ-PBR2 leaning="Build it, nothing-serves only. Where a build serves, the existing bypass is already one variable; the gap is only where nothing does." -->

   _Leaning:_ Build it, nothing-serves only. Where a build serves, the existing bypass is already one variable; the gap is only where nothing does.

   **Answer:**
   > _(empty — fill in when decided)_

3. 💬 **OQ-PBR3: Does a walk-back's build become the good build, or serve only the launch that asked?**

   This decides whether the user must set the variable once or on every launch until the repair.

   - **The good build, held** — the next launch refuses at the newest's failure and offers
     `YOLO_ALLOW_PATCH_FAILURES=1`, which runs it; nothing new to learn.
   - **One launch only** — every launch repeats the walk-back, its replays and an exact lookup,
     and the record never names an older build as good.

   <!-- vantage: question id=OQ-PBR3 leaning="The good build, held. It reuses the ratchet and the existing bypass, and a build that was admitted is exactly what the good build means." -->

   _Leaning:_ The good build, held. It reuses the ratchet and the existing bypass, and a build that was admitted is exactly what the good build means.

   **Answer:**
   > _(empty — fill in when decided)_

4. 💬 **OQ-PBR4: How many builds may one walk-back try per key?**

   Replays are seconds; builds are minutes (pi's is over a hundred megabytes, bounded at 20
   minutes each).

   - **3** — the newest fit, the next, then the base: under an hour at worst.
   - **Until the base** — finds any working version, with no bound but the list.
   - **1** — the newest fit only; the base is today's fallback anyway.

   <!-- vantage: question id=OQ-PBR4 leaning="3. A release that built last month usually builds now; three failures in a row says the toolchain, not the version, is broken." -->

   _Leaning:_ 3. A release that built last month usually builds now; three failures in a row says the toolchain, not the version, is broken.

   **Answer:**
   > _(empty — fill in when decided)_

5. 💬 **OQ-PBR5: Should an `agent_updates` hold stop a recorded patch failure from refusing the launch?**

   Today it does not ([D5](#45-d5-the-hold-the-guide-recommends-does-not-hold-past-a-recorded-failure)),
   though the user guide says the hold keeps what you have. A user of a shipped pack has no other pin.

   - **Yes** — with a good build serving, the held launch runs it and says which release it skips;
     the hold is the one-key pin for owner and user alike.
   - **No** — keep the code's choice, and correct the guide to say only the bypass continues.

   <!-- vantage: question id=OQ-PBR5 leaning="Yes. A hold is the user saying they do not want the newer version, so a failure of that version is not theirs to fix today." -->

   _Leaning:_ Yes. A hold is the user saying they do not want the newer version, so a failure of that version is not theirs to fix today.

   **Answer:**
   > _(empty — fill in when decided)_

6. 💬 **OQ-PBR6: Should `YOLO_USE_CACHED_GOOD` also run an extension's older good build?**

   Today an edited extension series that does not apply leaves pi unable to start
   ([§3.3](#33-a-patched-extension)).

   - **Yes** — pi runs the old series' tree; a second materialization path for trees.
   - **No, the walk-back covers it** — the new series is built on an older upstream instead, which
     is what the user edited for.

   <!-- vantage: question id=OQ-PBR6 leaning="No, don't build it. With the walk-back the extension gets its new series, and only a series that fails even at its own base is left, which a revert fixes." -->

   _Leaning:_ No, don't build it. With the walk-back the extension gets its new series, and only a series that fails even at its own base is left, which a revert fixes.

   **Answer:**
   > _(empty — fill in when decided)_

## Decision ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| — | No rulings yet | — | — | — |

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
