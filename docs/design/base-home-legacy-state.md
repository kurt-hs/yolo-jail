---
title: "Why does every podman jail share one home? It should not — a per-jail skeleton instead"
date: 2026-09-20
status: accepted
tags: [base-home, jail-home, storage, backend-parity, design]
summary: "Podman mounts ONE machine-wide base, <state>/home, read-only at /home/agent in every jail. A jail needs nothing in it: it holds empty mountpoints, three redirect links, the machine's shared credential dirs and a login seed only the host reads. Sharing it is the defect: one workspace's pack dirs, host_files links and old bytes show up in every jail, and a jail that did not select claude can read the machine's Claude credential file. The design: mount a per-jail read-only skeleton built from the SELECTED packs, keep <state>/home as the machine store for the shared dirs and the Claude login seed, delete seedAgentDir, and leave legacy bytes unmounted and unread."
stage: BUILT
next: "Complete the native seed target/copy mutation and restored boot gate in base-home-legacy-state-plan.md before retirement; retain jail-home.md's live OQ-JH1 rather than hiding it in a completed reference"
---

# Why does every podman jail share one home? It should not — a per-jail skeleton instead

**Status:** reconciled 2026-10-07 against `931489400b`. The skeleton and seed-path
implementation are built; the original build and rootless observations below remain
historical evidence. MEASURED: the Apple Container fake login seed reached the jail and
the dotted directory layout held in [run 37133569003](https://github.com/mschulkind-oss/yolo-jail/actions/runs/37133569003)
at `5ca9b74856b1a181e91b0c30022f578382d54204`, 2026-10-03. UNMEASURED: authentic
Claude login, seed learning after a real login on that backend, and current target-host
acceptance. **Source-only update, 2026-10-09:** [the seed check](#3-the-apple-container-seed-defect)
now fails on a bad verdict, with offline controls and a structural test of the native caller's
hard-failure branch. [The native target/copy mutation and restored boot gate](base-home-legacy-state-plan.md#remaining-gate--seed-regression-promotion)
remains open. No design ruling is reopened. Graduation into the existing reference is
held while [OQ-JH1](../reference/jail-home.md#OQ-JH1) remains live there.

**Original build evidence, 2026-09-25:** every question is ruled, and every step of
[§8](#8-build-order-and-done-conditions) is built, [OQ-BH15](#decision-ledger) and
[OQ-BH16](#decision-ledger), the last two, the same day. The Apple Container experiment was
written on 2026-10-01 for `apple-container.yml`, which runs on the maintainer's self-hosted Mac. The doc was rewritten around a new premise after the maintainer's review,
then every open question settled the same day. **Steps 1–3 of [§8](#8-build-order-and-done-conditions)
built 2026-09-25** (the seed fixes, the podman skeleton, the refusal's deletion and the reworded
`yolo check` report); `integration/homeskeleton_test.go` PASSED in a nested, rootful jail the
same day, and ROOTLESS in CI on both arches the same evening (`ci.yml` run 36167524940). **Steps 4, 4a and 5 built 2026-09-25**: the
Apple Container seed paths ([OQ-BH12](#decision-ledger), initially host-side only), name
reservation over the selected packs only ([OQ-BH14](#decision-ledger)), and the reference docs and code
comments. **Review follow-ups built 2026-09-25** ([§8](#8-build-order-and-done-conditions),
last bullet): the reaper can now reach a jail's skeletons, a refused launch leaves none, and a
configured pack's shared dir gets its bind source.
MEASURED: what the base holds on two bases, EROFS on the home root, the jail writing through `/workspace/.yolo/home`, the machine credential file sitting in the base, and a
host `rmdir` detaching a bind (in a user+mount namespace; `internal/prune/shadowed.go` records
the same failure in a real jail, 2026-07-04). UNMEASURED in that investigation: the Apple
Container seed defect and a jail without claude actually reading the credential file
(inferred from the mounts). The fake-seed delivery observation above is later and narrower.
Rootless ID mapping of a host-built skeleton was MEASURED in CI, 2026-09-25. Evidence: [Appendix A](#appendix-a-evidence).

> **In short.** The shared base home is left over from an older design, and sharing it is the
> bug. Give each podman jail its own read-only skeleton, built from the packs that jail
> selected. Then a pack the jail did not select has no effect inside it.

**Why it matters.** Today one workspace's config reaches every jail on the machine: its
`writable_home_dirs`, its `host_files` links and every shipped pack's directory. Worse, a jail
that did not select the claude pack still sees `~/.claude-shared-credentials/.credentials.json`,
the machine's Claude refresh token, through the base (agy's dir likewise).

**The shape.** A per-jail *skeleton* (defined in [§1](#1-the-question-and-the-answer)) mounted
`:ro` at `/home/agent`. `<state>/home` stays as the *machine store*. `seedAgentDir` is deleted.
**Cost:** four writers move, one bind changes, the quarantine design is dropped
([§7](#7-what-died)). Rootless behavior can only be checked on a real host or in CI.

## Settled, 2026-09-25

The five questions closed in one review ([Decision Ledger](#decision-ledger)). **The two found in the
build are settled and BUILT too:** [OQ-BH15](#decision-ledger), `host_files`' surface-path reservation
narrows to the selected packs (it was the one rule left that read every shipped pack), and
[OQ-BH16](#decision-ledger), a launch removes its own skeleton when its jail ends, so a workspace used
alone no longer waits for `yolo prune --apply`. **Nothing is open.**

- **Yours:** [OQ-BH14](#decision-ledger): name reservation covers only the **selected** packs'
  directories, and [OQ-BH15](#decision-ledger) narrows the surface-path list the same way, so no
  validation rule reads the shipped set any more ([DIR-BH1](#decision-ledger)). [OQ-BH10](#decision-ledger): a new skeleton per
  fresh launch, never edited. [OQ-BH13](#decision-ledger): the launch refusal and its hatch go with the
  skeleton; `yolo check` keeps a reworded report.
- **Delegated to the build** ("an implementation detail"): [OQ-BH9](#decision-ledger), the skeleton
  lives under `AgentsDir/<cname>/`; [OQ-BH12](#decision-ledger), the Apple Container seed gets
  runtime-aware paths, verified on a Mac.

**Decided here without a ruling; object if any is wrong:**

- The skeleton is bound `:ro`, and `<state>/home` stays as the machine store. Your words were
  "per jail"; the read-only root is this design's reading ([§5](#5-alternatives-with-verdicts) B).
- Delete `seedAgentDir`; limit `SyncClaudeJSONSeed`'s forward pass to `claudeJSONSeedKeys`,
  which stays core-owned ([§2.7](#27-the-seed)).

**To measure, not to rule:** whether a `host_files` entry under an unselected shipped pack's dir
fails today ([§2.8](#28-reservation-is-a-rule-about-config-names-not-about-directories)). The
fix no longer depends on the answer.

**Reads with:** [`base-home-legacy-state-plan.md`](base-home-legacy-state-plan.md) (the build
sketch, not a hand-off), [`jail-home.md`](../reference/jail-home.md) (the home layout this
changes), and [`../reference/macos-user-home-tiers.md`](../reference/macos-user-home-tiers.md)
([`OQ-HT4`](../reference/macos-user-home-tiers.md#oq-ht4), the parity ruling this keeps).

---

## 1. The question, and the answer

The maintainer asked: *"Why is this not just per jail? It seems like it's nothing."* It can be
per jail, and for a jail it is nothing.

**Terms used here:**

- **`<state>`** is `paths.GlobalStorage()` (`~/.local/share/yolo-jail`); **the base home** is
  `paths.GlobalHome()` = `<state>/home`, bound `:ro` at `/home/agent` today (`podmanBaseMounts`).
- **`wsState`** is `<workspace>/.yolo/home` (`paths.WorkspaceHomeState`), the per-workspace
  overlay. Podman names its directories with the leading dot stripped (`claude`, `config`).
- **`cname`** is the container name, `runtime.FromWorkspace(workspace)`, one per workspace, so
  on podman "per jail" and "per workspace" mean the same thing.
- **Skeleton** *(coined here)*: a per-jail directory bound `:ro` at `/home/agent`, holding only
  the mountpoints and redirect links a launch needs. It is **not** a home and holds no file
  content.
- **Machine store** *(coined here)*: `<state>/home` once it stops being mounted at
  `/home/agent`. It holds the machine-scope shared dirs and the Claude login seed.
- **Legacy bytes**: files left in `<state>/home` from before 2026-04-07, when it was the
  shared writable home.
- **Fresh-launch path**: a launch that starts a container rather than attaching to one.

**What a podman jail gets from the base, and whether any of it needs to be shared:**

| What | Needs sharing? | After this design |
| :--- | :--- | :--- |
| Empty mountpoints: every shipped pack's dirs, the core dirs, eight single-file mountpoints, and every `writable_home_dirs`/`host_files`/`files`/skills/briefing mountpoint any launch ever made | No | The skeleton, from this jail's selection |
| Three redirect links (`.claude.json`, `.gitconfig`, `.bashrc`) into per-workspace binds | No; their targets are per-workspace | The skeleton |
| A read-only home root, so writing an undeclared path fails with EROFS | No | The skeleton, also bound `:ro` |
| Machine-scope shared dirs (`.claude-shared-credentials`, `.gemini-shared-credentials`, `.pi-shared-npm`) | **Yes, on purpose** | Their own rw bind from `<state>/home/<dir>` when the pack is selected, as today. Today they are also readable through the base when it is **not**; after, they are absent then |
| The Claude login seed, `<state>/home/.claude/claude.json` | **Yes, on purpose** | Unchanged. Only the host reads and writes it |

**Why it was shared: history, not design.** In the first public commit (`7deeb762`,
2026-03-22), `<state>/home` was the one **writable** `/home/agent` every jail shared. Commit
`2bcc4e76` (2026-04-07) made it `:ro`, moved every writable path into per-workspace overlays,
and added `seedAgentDir` to copy the old auth files out. What was left is a shell nobody
designed. It is a union of **every** shipped pack's directories only because `ensureStorage`
runs before `loadAndValidateConfig` (`run.go`), when the selection is not known yet.

**What sharing costs today** ([Appendix A](#appendix-a-evidence)):

- **A jail that did not select claude or agy can read that pack's machine credentials.** The
  shared dirs live inside the base, and their own bind is emitted only for selected packs
  (`packload.SharedDirs(in.packs)`, `assemble.go`). Under rootless podman, jail root maps to the
  host user, so mode `0600` does not stop the read (INFERRED; the file's presence MEASURED).
- **The same jail can read the machine `oauthAccount`**, through the `.claude.json` link into
  the base's `.claude/claude.json`.
- **Other workspaces' leftovers and every unselected pack's dir, with any legacy bytes in it,
  show up in every jail**, and nothing removes them (MEASURED). That includes a workspace-scope
  `host_files` home-root link (INFERRED: `prepareHostFiles`).
- Host-side, separately: **`seedAgentDir` copies every top-level file** of `<state>/home/.<dir>`
  into each new workspace, legacy bytes included, whatever is mounted.

**The rule this design is checked against** is [DIR-BH1](#decision-ledger): *"a
non-selected pack can never have an impact."* It has no exceptions: name reservation, the last
one, now covers only the selected packs ([OQ-BH14](#decision-ledger)).

## 2. The design: a per-jail skeleton

### 2.1 What the skeleton holds

Built from **this launch's** loaded config and selected packs, and nothing else:

- Core's own dirs, `paths.HomeSkeletonCoreDirs()`, and the eight single-file mountpoints
  (`paths.HomeFileMountpoints()`). The core dirs are `paths.BaseHomeCoreDirs()` without the pi
  pack's `.pi/agent`, which in a skeleton would put a `~/.pi` in every jail; the pi pack declares
  `.pi` itself. `BaseHomeCoreDirs` keeps it for the legacy walk, since an old base holds it.
- The mountpoints for the selected packs' `WritableDirs` and `SharedDirs`.
- The three `paths.HomeFileRedirects()` links. A link whose target dir is not bound dangles and
  reads as absent, which is correct.
- Mountpoints for `config.WritableHomeDirs(cfg)`; for `host_files`, the WritableDir mountpoints
  and the Symlink links.
- Mountpoints for the selected packs' `files` targets outside a writable dir, their skills
  destinations and their briefing destinations.

**Every `-v` destination directly under `/home/agent` either exists in the skeleton or sits
inside another bind.** A test over the argv pins that ([§8](#8-build-order-and-done-conditions)).
The mountpoints stay even though `jail-home.md` records that seven experiments could not
reproduce EROFS on a nested mountpoint: they are cheap, and they make mode and ownership
deterministic.

### 2.2 Where it lives: host-only, never in wsState

**Not in `wsState`**: `/workspace/.yolo/home` is writable from inside the jail (MEASURED). A
skeleton there would be read-only in name only, and the host's `MkdirAll`/`touchFile`/
`EnsureSymlink` would follow links the jail planted. It lives under
`paths.AgentsDir()/<cname>/` ([OQ-BH9](#decision-ledger)): host-only, keyed per jail, already bound into
jails for skills and briefings, and already reaped by liveness (`PruneOrphanAgentStaging`,
which reaps nothing when liveness is unknown, never the launching jail's own dir, and has a
one-hour age floor).

**Found in review, 2026-09-25: the reaper could not reach it.** `PruneOrphanAgentStaging`
keeps every name that is live **or tracked**, and a `--rm` jail's tracking file
(`CONTAINER_DIR/<cname>`, the container-to-workspace map `yolo ps` reads) survived every normal
exit: only a stale-container removal, `yolo ps`, `yolo check`'s cleanup and a capture removed
it. So a workspace in use kept its entry and gained one skeleton per fresh launch. **Fixed:** a
launch removes the tracking file when it sees its jail end (normal exit, the signal arm, a
runtime that never started), and only when the runtime answered that no container of that name
exists and the workspace lock was free (`forgetGoneContainer`,
`internal/cli/run/trackingcleanup.go`). Each of those three ends releases the launch's own lock
first, the normal exit included: the lock's other release waits on a poll for the container to
show as running, so a child that exits before it ever does would otherwise find its own lock
held and keep the file. The reaper then does what [OQ-BH10](#decision-ledger) assumed. On the same known-gone evidence each end
also removes the skeleton its launch built ([OQ-BH16](#decision-ledger)), so the reaper is left only the
launches that died with no teardown (SIGKILL, OOM). An "I could not ask" answer removes nothing.
Removing a skeleton whose container is known gone is not the modification
[OQ-BH10](#decision-ledger)'s "never modified afterwards" forbids.

> [!WARNING]
> **The "lock was free" half needs `workspaceLock.Close` to unlock explicitly.** The normal exit
> takes the workspace lock in `stopLoopholes` and again in `forgetGoneContainer` right after, and
> a child the launch forked in between (the housekeeping slot's podman calls) holds a copy of the
> first descriptor until its exec. Closing a descriptor does not release a flock another copy
> still holds, so on a slow runner the second take failed and the skeleton stayed (CI run
> 36486674316, ubuntu-24.04-arm, 2026-09-28). An explicit unlock releases it for every copy. Do
> not add a wait instead: that puts latency on the quit path to hide a lock this launch had
> already released.

**What the reaper left behind before [OQ-BH16](#decision-ledger)**, the background that question was filed on. With the tracking file gone at exit,
`PruneOrphanAgentStaging` reaps a workspace's whole `AGENTS_DIR/<cname>` once no container of
that name is live and the entry is an hour old. It runs from `yolo prune` and from a host
launch's housekeeping slot (debounced, `reapSmallAutomaticClasses`), but the slot always
keeps the launching jail's OWN name. So on a machine
where one workspace is used alone, that workspace's skeletons, one per fresh launch, go only
when someone runs `yolo prune --apply`. With two or more workspaces, any other workspace's
launch reaps the idle one.

### 2.3 When it is built

- **Only on the fresh-launch path, and only for podman**, where `prepareWsState` and
  `prepareHostFiles` run today: after `loadAndValidateConfig`, after the attach decision, and
  under the workspace flock. Never on attach. Apple Container and macos-user build none
  ([§2.9](#29-backends)).
- **Failure.** Today the entries leaving `EnsureGlobalStorage` (core dirs, file mountpoints,
  redirects) are **fatal**: each loop returns its error and `Run` exits 1. The config- and
  pack-driven ones (`prepareWsState`, `prepareHostFiles`, `preparePackFilesGlobal`) are
  **best-effort** (`_ =`). The skeleton keeps that split, and a failure names the path, since
  podman's own error would name neither. It also keeps their ORDER: every fatal entry, the
  redirects included, is made before any best-effort one, so a pack entry landing on a redirect
  name fails on its own, as a warning, instead of making the redirect fail the launch.
- **A launch that does not start a container leaves no skeleton** (found in review,
  2026-09-25). A fatal entry removes the builder's own partial directory, and every return
  between the build and the container start (two pre-flights that read the assembled argv, and
  a runtime that never started) removes the skeleton first (`discardUnheldSkeleton`). No
  container ever held that directory, so [§2.4](#24-the-three-rules-the-shared-base-obeys-by-accident)'s
  rule 2 does not apply to it.

### 2.4 The three rules the shared base obeys by accident

The shared union is host-only, only ever grows and is never pruned. That avoids three hazards
by accident. The skeleton has to state them as rules:

1. **Build it outside the workspace** ([§2.2](#22-where-it-lives-host-only-never-in-wsstate)).
2. **Never remove a mountpoint under a live jail**: a host `rmdir` silently detaches the bind
   inside (MEASURED). **Today the launch path cannot prove that no container named `cname` is
   alive.** A failed probe reads as "no container", and a failed stale-container removal can
   still fall through to the fresh path; the flock does not report whether it was acquired
   (INFERRED from `lifecycle.go`, `run.go`, `flock.go`; detail in
   [Appendix A](#appendix-a-evidence)). So editing a skeleton in place needs two new, UNBUILT
   signals: a tri-state liveness probe that removes only on "known absent", and an `acquired`
   flag on the lock. [OQ-BH10](#decision-ledger)'s ruling needs neither: a new skeleton per launch
   never removes anything.
3. **Keep `<state>/home` as the machine store** ([§2.5](#25-the-machine-store-stays)).

### 2.5 The machine store stays

Only its mount at `/home/agent` goes. It still backs the shared dirs' rw binds on both container
backends (`assemble.go`, `appleContainerBaseMounts`), the Claude login seed
(`syncClaudeJSONSeed`), the OAuth broker's default `--creds-file` (`defaultCredsPath`,
`internal/oauthbroker/oauthbrokercmd.go`) and the stranded-credential rescue
(`migrateOldOverlay`). `config.agentWritableTrees` keeps `paths.GlobalHome()`, because jails
still write the shared dirs; the host-built `:ro` skeleton does not join that list.

### 2.6 Which writers move

| Symbol | Writes into `<state>/home` today | After |
| :--- | :--- | :--- |
| `storage.EnsureGlobalStorage` | the union (`EmbeddedWritableDirs` + `EmbeddedSharedDirs` + `BaseHomeCoreDirs`), `fileMountpoints`, the redirects, the credential migration | keeps the storage dirs, `EmbeddedSharedDirs` (bind sources) and the credential migration; the rest moves to the skeleton builder |
| `prepareWsState` | `writable_home_dirs` mountpoints, skills destinations, briefing `touchFile`s | skeleton |
| `preparePackFilesGlobal` | `files` mountpoints | skeleton |
| `prepareHostFiles` | WritableDir mountpoints, Symlink links (`EnsureSymlink`) | skeleton |
| `podmanBaseMounts` | binds `paths.GlobalHome()+":/home/agent:ro"` | binds the skeleton at the same place |

Unchanged: the shared-dir binds, `appleContainerBaseMounts`, the seed path and
`migrateOldOverlay`. [OQ-BH10](#decision-ledger)'s ruling leaves `acquireWorkspaceLock` and the
container probes in `lifecycle.go` unchanged.

### 2.7 The seed

Decided here, not by a ruling:

- **Delete `seedAgentDir`** on every backend. Its only inputs since April are legacy bytes,
  zero-byte mountpoint files, and `claude.json`, which `SyncClaudeJSONSeed` already handles
  (that nothing else is live is INFERRED; the risk is in [§6](#6-risks)). Stopping the mount
  would not stop the copy, because the seed reads `<state>/home` on the host.
- **Limit `SyncClaudeJSONSeed`'s forward pass to `claudeJSONSeedKeys`** (`oauthAccount`,
  `hasCompletedOnboarding`). Today the forward loop copies **every** key the workspace lacks;
  only the reverse pass uses the allowlist (`internal/storage/claudejson.go`). So a legacy
  seed's `projects`/`mcpServers` reach every new workspace. The allowlist stays core-owned.
- Both are independent of the skeleton and ship first ([§8](#8-build-order-and-done-conditions)).

### 2.8 Reservation is a rule about config names, not about directories

`hostFileWritableRoots` (`internal/config/hostfiles.go`) and `reservedHomeDirs`
(`internal/config/writablehome.go`) are built in memory from `packload.Embedded*` and never look
at `<state>/home`. Today they refuse a `host_files` or `writable_home_dirs` entry claiming
**any** shipped pack's directory, so `writable_home_dirs: [".codex"]` is refused in a workspace
that never selects codex. That is an unselected pack's effect, against
[DIR-BH1](#decision-ledger).

**Ruled ([OQ-BH14](#decision-ledger)): reserve only the selected packs' directories.** The maintainer's
reason: packs can come from anywhere and be added or removed at any time, so reserving the
shipped set never covered every pack a jail could select. An unselected pack is treated as if it
does not exist. So:

- One user-scope entry can be valid in one workspace and refused in another, and a pack selected
  later can refuse an entry that passed before. Both are accepted.
- `jail-home.md` treats reserving a name and creating a directory as one thing. It needs
  rewording, and after this change the two sets are the same: the selection's dirs.
- AGENTS.md's bullet "`packload.Embedded*` is deliberately NOT selection-gated" states the
  opposite rule for these lists. The build rewrites that bullet in the same change. The
  embedded-tree leasing half of that bullet is unaffected.

**To measure:** `StagingFor` answers "already under a rw bind" for every shipped pack's dir,
true only when the pack is selected. So a `host_files` entry such as `~/.codex/x` in a
claude-only jail probably fails with EROFS today (INFERRED, untested). Under the ruling the fix is
the same whatever the measurement shows: gate `StagingFor` on the selection too, so an
unselected pack's dir is an ordinary path and the entry gets its normal skeleton link.

**Built 2026-09-25.** The measurement was not taken; the fix does not depend on it.

- `writable_home_dirs` reserves core's names plus the selected packs' writable and shared
  dirs, and a refusal names the pack. The launch passes its staged set; validation, which
  runs before staging, resolves the same selection from the user config and the pack store,
  `needs` closure included (`resolveSelectedPacks`, `internal/config/selectedpacks.go`). A
  configured pack the store cannot resolve reserves nothing, because staging refuses that
  launch anyway.
- `StagingFor` and its three readers (the workspace-overlay half, the skeleton, the argv)
  take the same selected packs.
- **Review fixes, 2026-09-25.** A destination under a selected pack's *shared* dir, or under a
  `writable_home_dirs` entry, is already under a read-write bind and is not staged again:
  staging it was a second bind at one destination, which podman refuses, and the ruling made
  that easy to reach (`writable_home_dirs: [".codex"]` beside `~/.codex/x.json`). Apple
  Container stages nothing: its home is the read-write `wsState` bind, and nothing on that
  backend created the backing dir an emitted bind named. Validation loads a configured pack
  with an `only`/`exclude` filter from a copy staged through the same filter, since the filter
  can drop the manifest.
- **Two reservations still hold in a workspace that selects no pack owning them.** `.claude`
  stays a reserved `writable_home_dirs` segment, because core's `~/.claude.json` redirect
  targets `.claude/claude.json`; that is core reserving its own link's target, not a pack's
  directory. `host_files`' surface-path reservation, a list of files rather than directories,
  follows the selection too since [OQ-BH15](#decision-ledger) (`selectedSurfacePaths`): e.g.
  `~/.codex/config.toml` is refused only while codex is selected.

**How an unselected pack could have this effect at all** (background to [OQ-BH15](#decision-ledger)). Every shipped pack is compiled
into the yolo binary, and `packload.Embedded()` hands back all of them, selected or not. A few
readers need the whole shipped set on purpose: `yolo pack ls` and `footprint` describe packs
you have not selected, and `storage.EnsureGlobalStorage` runs before config is loaded.
`builtinSurfacePaths` is not one of those. It was written before selection-aware resolution
existed, and it is the last validation rule that reads the shipped set instead of your
selection.

### 2.9 Backends

| Backend | Today | After |
| :--- | :--- | :--- |
| **podman** | shared `:ro` base at `/home/agent` | per-jail skeleton at `/home/agent`; every other bind and the dot-stripped `wsState` layout unchanged |
| **Apple Container** | `wsState` bound rw, whole, at `/home/agent`; no base mount | unchanged, except that `seedAgentDir` goes and the separate seed defect ([§3](#3-the-apple-container-seed-defect)) |
| **macos-user** | account home `/Users/_yolojail`; returns in `run.go` before `prepareWsState` | unaffected |

Legacy bytes are **left in `<state>/home`, unmounted and unread.** Nothing is moved or deleted,
so R1/R2 (move, never delete) are met; they still bind any future code that touches those
bytes. The old union's empty directories stay too, so a downgrade keeps working. What the
refusal and `yolo check` say about the bytes is [OQ-BH13](#decision-ledger).

## 3. The Apple Container seed defect

The old defect was a backend-path mismatch: `prepareWsState` synced podman's
`wsState/claude/claude.json`, but Apple Container binds `wsState` whole at `/home/agent`
and reads `wsState/.claude.json`. Creating podman's bind sources there also left unused
undotted `~/claude` and `~/npm-global` entries. [OQ-BH12](#decision-ledger) keeps runtime-aware
paths rather than adding podman's many mounts to a backend limited by mount count.

On `rt=container`, `prepareWsState` syncs the seed with `wsState/.claude.json`
(`claudeJSONInWsState`), creates the selected packs' dotted dirs plus `go`, and does not
create podman's dot-stripped bind sources (`preparePodmanBindSources`). Legacy layout
migrations follow the same backend-specific paths (`wsStateHomePath`). These host-side
paths are pinned by [`acseed_test.go`](../../internal/cli/run/acseed_test.go).

**MEASURED, 2026-10-03:** [run 37133569003](https://github.com/mschulkind-oss/yolo-jail/actions/runs/37133569003),
checkout `5ca9b74856b1a181e91b0c30022f578382d54204`, recorded
`AC-PARITY login-seed VERDICT: HOLDS`. The saved log reports the fake login in both the
host's `<ws>/.yolo/home/.claude.json` and the jail's `~/.claude.json`, with
`.claude:DIR`, `claude:ABSENT` and `npm-global:ABSENT`.

The experiment, `TestAppleContainerFreshWorkspaceBootsWithTheLoginSeed` in
[`applecontainerhome_test.go`](../../integration/applecontainerhome_test.go), plants a
per-run fake account in a private machine store, never the Mac's real store. It starts no
agent and tests no authentication. The entrypoint adds workspace settings to the delivered
JSON; their presence is not evidence that the seed forwarded non-login keys.

> [!WARNING]
> **Historical HOLDS is not current boot acceptance or a successful login.** The recorded
> experiment accepted either verdict. The [promotion rule](../../integration/applecontainerparity_test.go)
> is now enforced for this login-seed check: a bad verdict fails, with offline controls and a
> structural test pinning the native caller's hard-failure branch. The [companion's remaining gate](base-home-legacy-state-plan.md#remaining-gate--seed-regression-promotion)
> requires a native mutation of the actual container seed target/copy caller, followed by a restored
> boot green. UNMEASURED: real Claude authentication, reverse learning after a real login,
> and current target-host acceptance; neither the historical observation nor offline tests closes those.

**Found in review, 2026-09-25: the seed wrote through links.** `SyncClaudeJSONSeed` read and
wrote the workspace file with calls that follow a symlink, and that file is one the jail can
replace: `wsState/claude/claude.json` on podman, and on Apple Container `wsState/.claude.json`,
which is the jail's own `~/.claude.json`. A jail that planted a link there chose which host file
the next fresh launch overwrote with the seed's JSON, and which host file's login the seed
learned (the hazard [§2.2](#22-where-it-lives-host-only-never-in-wsstate) names). Fixed: a side
that is not a regular file takes no part in the sync, the open is `O_NOFOLLOW`, and the write is
a temp file renamed over the path. MEASURED as a unit test on both backends' paths
(`TestTheSeedDoesNotWriteThroughAJailPlantedLink`, and `internal/storage/claudejsonlink_test.go`).

## 4. What this does not cover

- **Not a migration**: nothing is moved, archived or deleted. Overlays that already hold seeded
  copies stay as they are, in the right tier.
- **Not the machine-scope shared dirs**, and not the `wsState` layout.
- **Not `PruneShadowedHome`.** It still empties and never removes, because a jail started by an
  older yolo still mounts `<state>/home`.
- **Not the macos-user account home.** Whether
  [`OQ-HT2`](../reference/macos-user-home-tiers.md#oq-ht2)'s discard should be reopened for an
  account used for real work belongs to that reference.
- **Not the nested-jail trust relation.** A nested jail's skeleton lives under the outer jail's
  home, which the outer agent can write; the outer jail is that nested jail's host, as today.

## 5. Alternatives, with verdicts

| Alternative | Verdict |
| :--- | :--- |
| **A. Keep the shared base; shadow the unselected pack dirs with empty binds** | **Rejected.** Other workspaces' `writable_home_dirs` and `host_files` leftovers still show, and it is one more mount per unselected pack |
| **B. Copy Apple Container: `wsState` rw at `/home/agent`** | **Rejected by this design, not by a ruling.** On that backend it works around a mount-count limit (`appleContainerBaseMounts` says so). It gives up EROFS on undeclared paths, and would rename every podman `wsState` out of the dot-stripped layout |
| **C. The skeleton inside `wsState`** | **Rejected.** The jail can write it ([§2.2](#22-where-it-lives-host-only-never-in-wsstate)) |
| **D. Keep sharing, build the union after config load** | **Rejected.** Selections differ per workspace, so a shared result still leaks |
| **E. Keep sharing, quarantine the legacy bytes (the previous design)** | **Superseded** ([§7](#7-what-died)) |

## 6. Risks

| Risk | Mitigation |
| :--- | :--- |
| A bind destination has no mountpoint and podman fails with an opaque crun error | The completeness test over the argv ([§2.1](#21-what-the-skeleton-holds)) |
| Rootless ID mapping makes the skeleton's owner or mode differ from today's | The same host user that creates `<state>/home` creates it; checked on a real rootless host ([§8](#8-build-order-and-done-conditions)) |
| A pre-April base holds a pack's only credential, or a file someone hand-placed to seed every workspace; with `seedAgentDir` gone, each NEW workspace on that machine needs one login | Live credentials travel through the shared-credential dirs and brokers; the pack dirs are MEASURED empty on the maintainer's host. That nothing else is live is INFERRED ([§2.7](#27-the-seed)). The seed file is the one deliberate channel, and it stays |
| The prune reaps a skeleton | Only when no container of that name is live or tracked, never the launching jail's own, past the one-hour floor. The next launch rebuilds it |
| Skeletons pile up, one per fresh launch | The tracking file now goes when the launch sees its container gone, so the prune reaches an idle workspace's entry; a launch that starts no container removes its own ([§2.3](#23-when-it-is-built)). Each end of a launch that sees its container gone also removes the skeleton it built ([OQ-BH16](#decision-ledger)); only a launch killed with no teardown leaves one for the reaper |

## 7. What died

- **The quarantine machinery** of the previous text (archive bucket, manifest, lock, marker,
  EXDEV copy, SQLite sibling rule), and its four-class taxonomy as a launch-path classifier.
  Only detection was built; it survives in `internal/basehome` if `yolo check` keeps its report
  ([OQ-BH13](#decision-ledger)). Bytes no jail mounts and no seed reads are inert disk.
- **The old invariants** ("a base state dir is a mountpoint whose steady state is empty",
  "top-level roots are never archived"), and **both old harms**: jails reading other
  workspaces' bytes goes with the mount, the seed copying them with `seedAgentDir`.
- **The launch refusal's justification** ("every jail can read the bytes, and `seedAgentDir`
  copies them"). Its comment's claim that every host write into `GlobalHome` is a directory
  `MkdirAll` is also false (`SyncClaudeJSONSeed`, `touchFile`, `EnsureSymlink`); its
  conclusion, that jails cannot write the base, holds.
- **Old questions [OQ-BH1](#decision-ledger)–[OQ-BH8](#decision-ledger)**, closed in the
  ledger.
- **Code comments citing this doc's pre-rewrite sections**, across `internal/basehome`,
  `internal/paths`, `internal/storage` and the two `basehomedisclosure` files, code and tests.
  Find them with `rg -n 'base-home-legacy-state|OQ-BH' internal` plus the bare `§` citations in
  `internal/basehome` and `basehomedisclosure*.go`. The build re-points or deletes them.

## 8. Build order and done-conditions

1. **The seed fixes** ([§2.7](#27-the-seed)), on every backend. They can ship alone.
2. **The skeleton**: the builder, the writers re-pointed, the podman bind switched, the golden
   argv updated.
3. **The refusal and the check report**, per [OQ-BH13](#decision-ledger), in the same change as step 2.
4. **The Apple Container seed fix**, per [OQ-BH12](#decision-ledger). It needs a Mac. Beside it, the
   plan's step 4a: **name reservation over the selected packs**, per [OQ-BH14](#decision-ledger).
5. **The docs**: `jail-home.md`, `storage-and-config.md`, and the code comments of [§7](#7-what-died).

**Done when** (each followed by where it stands, 2026-09-25):

- Mountinfo shows `/home/agent` bound `:ro` from the skeleton; `touch ~/.x` fails with EROFS.
  *Met in a nested jail, 2026-09-25: `TestPodmanHomeIsAPerJailReadOnlySkeleton`
  (`integration/homeskeleton_test.go`) passed, all three subtests (`claude only`, `codex only`,
  `writable_home_dirs`). That podman was rootful (a nested jail forces `--userns=host`); the
  rootless bullet below was met in CI the same evening.*
- In a claude-only jail, `~/.codex`, `~/.copilot`, `~/.oh-omp`, `~/.pi-lens` and
  `~/.gemini-shared-credentials` do not exist. *Met at the builder
  (`TestTheSkeletonCarriesOnlyTheSelectedPacksDirs`); the integration test above checks
  `~/.codex`, `~/.copilot`, `~/.pi` and `~/.gemini-shared-credentials` in a container, and
  passed in the nested run above. `~/.pi` is checked because review found the first build put
  it in every jail, from core's `.pi/agent` (`TestCoreSkeletonDirsNameNoPacksDir` now pins
  core's list).*
- In a jail selecting neither claude nor agy (e.g. `packs: ["codex"]`), `~/.claude` and
  `~/.claude-shared-credentials` do not exist, `~/.claude.json` dangles, and no machine
  `oauthAccount` or refresh token is readable anywhere under `~`. *The same two tests; the
  integration test passed in the nested run above. The "readable anywhere under `~`" half is
  now asserted by its `codex only` subtest, which plants a machine login in the store first (a
  seed `oauthAccount` and a shared refresh token) and searches all of `~`, links followed, for
  any `.credentials.json` and for the planted values. The plant goes into a machine store
  PRIVATE to the test: the harness's isolated home links the whole state dir back to the
  machine's, so a plant through it would overwrite the developer's real Claude login
  (`TestPlantMachineLoginLeavesTheRealStoreAlone` pins that it cannot). That assertion was
  added after the nested run and has not run yet.*
- No other workspace's `writable_home_dirs`/`host_files` entries appear; a dropped `host_files`
  home-root link is gone at the next fresh launch. Attach leaves the skeleton byte-identical.
  *Met: `TestEachFreshLaunchGetsANewSkeleton`, and `TestAttachLeavesTheSkeletonByteIdentical`,
  which drives the real `attachExisting` up to its exec (a real runtime is the one step it does
  not reach).*
- A fresh launch writes nothing into `<state>/home` except the shared dirs, the seed file and
  the credential migration; a snapshot test holds everything else byte-identical. *Met for the
  writers the snapshot drives: `TestAFreshLaunchLeavesTheMachineStoreByteIdentical`,
  `TestEnsureGlobalStorageProvisionsNoHome`. `TestOnlyTheListedFunctionsNameTheMachineStore`
  pins the call sites: only the functions of `internal/cli/run` it lists may name
  `paths.GlobalHome()`, and each one that writes is one the snapshot drives. Not pinned: a write
  that spells the path another way, or one made in another package.*
- A new claude workspace boots logged in and gets no `claude.json` key outside
  `claudeJSONSeedKeys`. A file planted in `<state>/home/.copilot` reaches no workspace. *Met
  at the unit level: `TestSeedForwardsOnlyTheLoginKeys`, `TestALoggedInWorkspaceStillBackPropagates`,
  `TestAFilePlantedInTheMachineStoreReachesNoWorkspace`; "boots logged in" is not run.*
- A test over the golden argv fails if any destination directly under `/home/agent` has no
  skeleton entry; another fails if the builder's call site in the launch path is deleted. *Met:
  `TestEveryHomeBindHasASkeletonEntry`, `TestRunContainerBuildsTheSkeletonOnTheFreshPath`; each
  was run against a private copy with its wiring removed, and failed. The call-site test pins the
  builder's result into the argv input statement by statement, requires exactly one call, and
  refuses any other caller in the package; its first cut checked only the field's name, and
  review showed a dropped assignment passing it.*
- **Rootless, on a real host or in CI; a nested jail cannot show this**, since it forces
  `--userns=host`. The report includes `podman info --format '{{.Host.Security.Rootless}}'` =
  `true`, a fresh launch boots, `stat -c '%u:%g %a'` of `/home/agent` and of a mountpoint match
  today's, and a `writable_home_dirs` mount works. *Met in CI, 2026-09-25: `ci.yml` run
  36167524940 at `e56d871e`, on `ubuntu-latest` and `ubuntu-24.04-arm`, whose jobs report
  `podman rootless=true`. All three subtests passed on both. The skeleton is mounted `ro`, and a
  write to the home root fails with EROFS. `/home/agent` and `/home/agent/.config` stat
  `0:0 755`, the jail user's own, because jail root maps to the host user who built the skeleton.
  The `writable_home_dirs` subtest wrote through its rw bind nested in the `:ro` skeleton.*
- **Step 4:** a fresh Apple Container jail reads the fake seed at `~/.claude.json`, and
  its home has no undotted `~/claude` or `~/npm-global`. *Host-side path and link defenses:
  `TestAppleContainerSeedReachesTheJailsClaudeJSON`, `TestAppleContainerLoginIsLearnedByTheSeed`,
  `TestPrepareWsStateLaysOutEachBackendsOwnPaths`, `TestLegacyMigrationsLandInEachBackendsLayout`,
  `TestTheSeedDoesNotWriteThroughAJailPlantedLink`, and the runtime argument pinned by
  `TestRunContainerHandsTheSelectionToTheReservationReaders`. Hardware fake-seed delivery:
  [the recorded HOLDS](#3-the-apple-container-seed-defect), 2026-10-03.
  Source regression enforcement has offline controls and a structural native-caller check;
  native target/copy mutation, restored boot and real login remain unmeasured. [The companion](base-home-legacy-state-plan.md#remaining-gate--seed-regression-promotion)
  retains that next gate.*
- **Step 4a:** `writable_home_dirs: [".codex"]` passes in a claude-only workspace and is
  refused once codex is selected, naming codex; a `host_files` entry under `~/.codex/` works
  in a claude-only jail. *Met: `TestWritableHomeDirCodexIsLegalUntilCodexIsSelected` (through
  `ValidateConfig`), `TestAConfiguredPacksDirIsReserved`, `TestStagingForKeysOnTheSelectedPacks`,
  `TestAHostFilesEntryUnderAnUnselectedPacksDirValidates`, and
  `TestAHostFilesEntryUnderAnUnselectedPacksDirIsStaged` (the backing dir, the skeleton
  mountpoint and the bind, for claude-only and for codex selected);
  `TestRunContainerHandsTheSelectionToTheReservationReaders` pins `runContainer`'s arguments,
  and `TestTheSkeletonAndTheBindSourcesDeriveWritableHomeDirsFromTheSelection` the skeleton's
  and the bind sources' own. The review fixes: `TestAWritableHomeDirCoversAHostFilesEntryUnderIt`,
  `TestAHostFilesEntryUnderASelectedPacksSharedDirIsNotStaged`,
  `TestAppleContainerStagesNoHostFilesWritableDir`, `TestAFilteredOutManifestReservesNothing`.
  Each was run against a private copy with the old behavior or its wiring restored, and
  failed. Not run in a container.*
- **Step 5:** `jail-home.md`, `storage-and-config.md` and `agent-credentials.md` describe the
  skeleton, the machine store, reservation and the seed; the code comments of [§7](#7-what-died)
  are re-pointed. *Met; the bare `§` numbers in `internal/basehome` are re-pointed at the
  pre-rewrite text (`git show 030c8f52:docs/design/base-home-legacy-state.md`) by that
  package's header rather than one by one.*
- **Review follow-ups, 2026-09-25**, each with the test that pins it. *Met at the unit level;
  none needs a container except the first, whose container half is the reaper's ordinary
  work.*
  - A jail's tracking file goes once its container is known gone, so the reaper reaches its
    skeletons ([§2.2](#22-where-it-lives-host-only-never-in-wsstate)):
    `TestANormalExitForgetsAGoneContainersTracking` drives the real teardown and then the real
    reaper; `TestTrackingStaysUnlessTheContainerIsKnownGone` covers every answer short of
    "gone"; `TestEveryEndOfTheLaunchForgetsTheContainer` pins the signal arm and the
    runtime-never-started branch.
  - A launch that starts no container leaves no skeleton ([§2.3](#23-when-it-is-built)):
    `TestAFatalSkeletonFailureLeavesNoDirectory`, `TestDiscardUnheldSkeletonRemovesOnlyASkeleton`,
    `TestNoReturnAfterTheSkeletonLeaksIt`.
  - The builder's best-effort warnings reach the launch output: `TestTheSkeletonsWarningsArePrinted`.
  - A CONFIGURED pack's machine-scope shared dir gets its bind source in the machine store on
    both container backends; `storage.EnsureGlobalStorage` creates only the shipped packs'
    ones, before config loads (`ensureSharedDirSources`, `internal/cli/run/shareddirsources.go`):
    `TestAConfiguredPacksSharedDirGetsABindSource`, `TestRunContainerCreatesTheSharedDirSources`.
  - Validation writes nothing into the pack store: `resolveSelectedPacks` resolves with
    `packsrc.Store.ResolveExisting`, not the launch's `Resolve`, which checks a missing tree out
    and removes an incomplete one first (`TestValidationLeavesThePackStoreAlone`).
  - Dead code deleted: `storage.EnsureSymlink` and `packload.EmbeddedWritableDirs`, which had no
    production caller, and `internal/config/zz_probe_test.go`, which asserted nothing.

  Each wiring test was run against a private copy of the tree with its call site removed, and
  failed.

## Decision Ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| R1 | **MOVE** legacy runtime artifacts to an archive, never delete. *Still binds* anything that touches legacy bytes; this design touches none | 2026-09-20 | [§2.9](#29-backends) | — (nothing to move) |
| R2 | **Move over delete**; any delete path is explicit opt-in. *Still binds*, the same way | 2026-09-20 | [§2.9](#29-backends) | — |
| R3 | **Fail closed**: no TTY or unconfirmed safety ⇒ do nothing, leave the marker unstamped, retry later. *Met trivially*: no migration, no marker | 2026-09-20 | [§7](#7-what-died) | — |
| R4 | **Generic**: every pack state dir, every backend (podman, `container`, macos-user). *Still binds*: [§2.9](#29-backends) covers all three | 2026-09-20 | [§2.9](#29-backends) | — |
| DIR-BH0 | **The maintainer's call:** no `yolo base-home --archive` verb; the user runs a printed `mv`. The verb was built and deleted the same day (commit `1f440af3`; the comment recording why, "Why a refusal and not a verb", is in `git show 1f440af3:internal/cli/run/basehomedisclosure.go`, a file step 3 deleted with the refusal) | 2026-09-21 | [§8](#8-build-order-and-done-conditions) | yes |
| DIR-BH1 | **Principle, the maintainer's:** *"a non-selected pack can never have an impact."* [OQ-BH14](#decision-ledger) narrowed name reservation to the selected packs; [OQ-BH15](#decision-ledger) narrowed `host_files`' surface-path reservation the same way | 2026-09-25 | [§1](#1-the-question-and-the-answer) | yes, 2026-09-25: the skeleton, including core's dirs (`paths.HomeSkeletonCoreDirs` drops the pi pack's `.pi/agent`), and name reservation ([OQ-BH14](#decision-ledger)). and `host_files`' surface-path reservation ([OQ-BH15](#decision-ledger)); no config validation rule reads the shipped set |
| DIR-BH2 | **Direction, the maintainer's:** *"So basically this directory seems like it's always empty and there's some complication with sharing across jails because then they could have different packs. Why is this not just per jail? It seems like it's nothing."* After the investigation: *"yes rewrite"*. What he directed: per jail, not shared, the doc rewritten around that. **This design's reading, not his words:** a `:ro` skeleton from the selected packs, `<state>/home` kept as the machine store, alternative B rejected ([§5](#5-alternatives-with-verdicts)) | 2026-09-25 | [§2](#2-the-design-a-per-jail-skeleton) | yes, podman, 2026-09-25 |
| OQ-BH1 | **Superseded by DIR-BH2.** Archive location and retention: no archive exists; legacy bytes stay in place, unmounted | 2026-09-25 | [§7](#7-what-died) | — |
| OQ-BH2 | **Superseded by DIR-BH2: no migration exists.** The same reason closes [OQ-BH3](#decision-ledger) (automatic move or confirmation), [OQ-BH6](#decision-ledger) (shadow layer, which a per-jail skeleton carries all the way, [§5](#5-alternatives-with-verdicts) A) and [OQ-BH8](#decision-ledger) (partial failure of a move). This row was trigger and marker | 2026-09-25 | [§7](#7-what-died) | — |
| OQ-BH4 | **Closed by this design, not by a ruling.** The launch path no longer classifies; the one remaining allowlist is `claudeJSONSeedKeys`, core-owned as today. The code comment citing this id (`internal/paths/basehomecore.go`) points here | 2026-09-25 | [§2.7](#27-the-seed) | — |
| OQ-BH5 | **Closed by this design, not by a ruling, split.** The seed half: delete `seedAgentDir` rather than allowlist it, its inputs since April being legacy-only (INFERRED). The posture half is now [OQ-BH13](#decision-ledger) | 2026-09-25 | [§2.7](#27-the-seed) | yes, 2026-09-25 (`seedAgentDir` deleted) |
| OQ-BH7 | **Superseded by DIR-BH2 for the container base**: no migration, so nothing to reuse [`OQ-HT2`](../reference/macos-user-home-tiers.md#oq-ht2)'s discard for; macos-user never mounts `<state>/home` or reaches `prepareWsState`. **Its macos-user half is dropped from this doc**: reopening [`OQ-HT2`](../reference/macos-user-home-tiers.md#oq-ht2) for an account used for real work belongs to [`../reference/macos-user-home-tiers.md`](../reference/macos-user-home-tiers.md) | 2026-09-25 | [§4](#4-what-this-does-not-cover) | — |
| OQ-BH9 | **Delegated to the build:** the skeleton lives under `paths.AgentsDir()/<cname>/`, reaped by `PruneOrphanAgentStaging`. The maintainer: *"This is an implementation detail that I don't care about."* | 2026-09-25 | [§2.2](#22-where-it-lives-host-only-never-in-wsstate) | yes, 2026-09-25 (`paths.HomeSkeletonRoot`) |
| OQ-BH10 | **The maintainer's ruling:** *"A new skeleton directory per fresh launch, never modified afterwards; old ones go with the jail's AgentsDir entry through PruneOrphanAgentStaging, which already declines when liveness is unknown."* | 2026-09-25 | [§2.4](#24-the-three-rules-the-shared-base-obeys-by-accident) | yes, 2026-09-25 (`buildHomeSkeleton`); the reaping half became true the same day, when a launch started removing a gone jail's tracking file (`forgetGoneContainer`, [§2.2](#22-where-it-lives-host-only-never-in-wsstate)); a launch's own skeleton goes at its exit since [OQ-BH16](#decision-ledger) |
| OQ-BH12 | **Delegated to the build:** runtime-aware seed paths in `prepareWsState`, verified on a Mac, landing separately from the podman change. The maintainer: *"Also an implementation detail for you."* | 2026-09-25 | [§3](#3-the-apple-container-seed-defect) | host paths built 2026-09-25; fake-seed delivery recorded HOLDS on a Mac in run 37133569003, 2026-10-03, at `5ca9b74856b1a181e91b0c30022f578382d54204`. Real authentication is unmeasured; source regression enforcement has offline controls and a structural native-caller check, while [native target/copy mutation and restored boot](base-home-legacy-state-plan.md#remaining-gate--seed-regression-promotion) remain owed |
| OQ-BH13 | **The maintainer's ruling:** delete the launch refusal and `YOLO_ALLOW_LEGACY_BASE_HOME` in the same change that stops the mount and deletes `seedAgentDir`; keep `yolo check`'s detection-only report, reworded, with the printed `mv` as optional cleanup | 2026-09-25 | [§8](#8-build-order-and-done-conditions) | yes, 2026-09-25 |
| OQ-BH14 | **The maintainer's ruling:** reserve only the selected packs' directories; an unselected pack is treated as nonexistent: *"it has to be like the other packs don't exist … packs could come from anywhere and be added, removed, whatever."* | 2026-09-25 | [§2.8](#28-reservation-is-a-rule-about-config-names-not-about-directories) | yes, 2026-09-25 (`resolveSelectedPacks`, `reservedHomeSegments`, `StagingFor`; AGENTS.md's bullet rewritten) |
| OQ-BH15 | **`host_files`' surface-path reservation covers only the selected packs**, by DIR-BH1, resolved as `writable_home_dirs` resolves them (`resolveSelectedPacks`); `config.SurfaceCollisions` still refuses two writers at launch. Confirmed in review: *"why is there even a thing that can read a pack that is not selected?"* | 2026-09-25 | [§2.8](#28-reservation-is-a-rule-about-config-names-not-about-directories) | yes, 2026-09-25 (`selectedSurfacePaths` over `resolveSelectedPacks`) |
| OQ-BH16 | **A launch removes its own skeleton at exit** on the known-gone evidence that removes its tracking file; "could not ask" removes nothing; that removal is not the modification [OQ-BH10](#decision-ledger) forbids. Delegated: *"this is an implementation decision for you. This is correctness … it has to exist when it has to exist, and then it should get cleaned up when we can clean it up"* | 2026-09-25 | [§2.2](#22-where-it-lives-host-only-never-in-wsstate) | yes, 2026-09-25 (`forgetGoneContainer` removes the launch's skeleton) |

## Appendix A: Evidence

From a five-agent investigation on 2026-09-25, one of them a skeptic; load-bearing claims were
re-read against HEAD for this rewrite.

- **MEASURED**: a fresh nested base holds 20 dirs, 13 files or symlinks and **0 non-empty
  files**; the older nested base's only non-empty file is `.claude/claude.json` (keys
  `oauthAccount`, `hasCompletedOnboarding`); on the maintainer's host `.copilot`, `.oh-omp`,
  `.pi-lens`, `.yolo-shims` and `.yolo-launchers` are empty. The nested base is 67 entries, 16K.
- **MEASURED**, leftovers: `.pi-lens` in the host base, which this workspace does not declare;
  `.foo`, `.keeper`, `.dropped`, `.filespack` and `yolo-it-newdir` in the nested base.
- **MEASURED**: in this jail `~/.claude-shared-credentials/.credentials.json` is 555 bytes, mode
  `0600`, bound from `<state>/home/.claude-shared-credentials`, a subdirectory of the base bound
  at `/home/agent`. The claude-oauth-broker README says it holds the jail identity's refresh
  token. In a nested claude-only jail, the unselected shared dirs were visible (empty there).
- **MEASURED**: an `oauthAccount` planted in workspace 1 reached
  `<state>/home/.claude/claude.json`, and from there workspace 2's `~/.claude.json`.
- **MEASURED** (re-measured for this rewrite): `touch /home/agent/.x` gives "Read-only file
  system"; mountinfo shows `/home/agent` bound `ro` from the host's
  `.local/share/yolo-jail/home`; `/workspace/.yolo/home` is writable from the jail.
- **MEASURED**: in a user+mount namespace made with `unshare`, a host-side `rmdir` of a
  mountpoint succeeded and detached the bind inside. 200 fork-per-call `mkdir`/`touch` calls
  took 0.2 s, so skeleton cost is no argument for sharing.
- **READ**, why the launch cannot prove a container absent: `findExistingContainer` and
  `findRunningContainer` (`internal/cli/run/lifecycle.go`) return `""` when the probe did not
  run. In `run.go`, when `removeStaleContainer` fails and the 5-second `waitForRunningContainer`
  returns `""`, the launch continues to `prepareWsState` with that container alive.
  `acquireWorkspaceLock` (`flock.go`) warns on a failed blocking flock and returns the same
  `*workspaceLock` as on success.
- **READ**: `git show 7deeb762` (a shared writable home, *"Global home as base (has auth,
  tools, configs)"*) and `2bcc4e76` (made `:ro`, `seedAgentDir` introduced).
