---
title: "What a downstream pack consumer is still waiting on from yolo"
date: 2026-10-13
status: in-review
stage: DESIGN
next: "Ratify OQ-DA2 and OQ-DA4, both built on their leanings; rule OQ-DA3 (a small diagnostics build); the three mechanism questions owned by design docs; asks 10 and 11 wait on their own owners"
depends-on:
  - ../design/patched-forks.md#OQ-PFK6
  - ../design/patched-forks.md#OQ-PFK7
  - ../design/model-lists-and-pickers.md#OQ-ML4
  - ../design/model-lists-and-pickers.md#OQ-ML5
tags: [handoff, downstream, packs, launcher, tracker]
summary: "The yolo-side index of the asks a downstream pack consumer (the dotfiles repository) carried in its 2026-10-13 handoff, and two filed since: which are still open at HEAD, what a landed version would look like, and where each one's design question is owned. It exists because none of these were in yolo's own docs before now."
vantage:
  status-chip: true
---

# What a downstream pack consumer is still waiting on from yolo

**Status:** 2026-10-15, re-verified in-tree at `6750951f6`, the 0.14.0 release commit (first
written 2026-10-13 at `80e08f98b`; the handoff was checked at `0.13.0+51.g99f36da2e`). **Why this file exists:** the handoff's nine asks
were, with four exceptions now owned elsewhere, recorded nowhere in yolo's own documentation, so a
reader of the roadmap or a design doc could not tell which of them yolo had accepted, declined, or
never seen. This is their one index.

**How to read.** Each ask carries its **state at HEAD**, the **evidence** a reader can re-run, what
**landed** would look like, and its **owner** — the design doc that owns the decision, or this file
where no design doc owns it. An ask whose every part is open is the whole target; one whose yolo
half is done says so.

**One boundary.** This file is a work queue, like [`BACKLOG.md`](BACKLOG.md); it holds reasoning
only where a question has no design doc of its own. The source handoff lives outside version control
(a consumer's repository), so its long-form attachments (`yolo-fix-requests.md`,
`pi/carries/*.md`, `pi/model-inheritance.md`) are not links here.

## The asks at a glance

| # | Ask, in one line | State at HEAD | Owner |
| :--- | :--- | :--- | :--- |
| [1](#1-remove-for-config-list) | a `remove:` sibling for `config-list` | landed, with a profile gate | [`pack-system.md`](../reference/pack-system.md#adding-entries-to-an-array-config-list); ratify [OQ-DA4](#OQ-DA4) |
| [2](#2-a-pack-cannot-declare-the-yolo-version-it-needs) | a pack can declare a minimum yolo, or lint judges it against an older build | declined | [OQ-DA1](#decision-ledger) |
| [3](#3-a-build-has-no-version-identity) | two binaries from one revision are distinguishable, and a broadcast clause records its version | landed | ratify [OQ-DA2](#OQ-DA2) |
| [4](#4-a-reduced-window-child) | a reduced-window child does not silently inherit the small window | narrowed — yolo's part done | decision recorded below; the owner's declaration is the consumer's |
| [5](#5-a-clean-replay-discards-the-clone-it-built) | a clean replay keeps the clone, and a stale series is signalled | settled — no clone, base lag built | [OQ-PFK7](../design/patched-forks.md#decision-ledger) |
| [6](#6-a-declared-model-row-loses-the-catalogs-thinking-levels) | a `models.json` row keeps pi's catalog thinking levels | open | [OQ-ML4](../design/model-lists-and-pickers.md#OQ-ML4) |
| [7](#7-the-carry-key) | a `files` contribution may name a commit graph instead of a series | open | [OQ-PFK6](../design/patched-forks.md#OQ-PFK6) |
| [8](#8-model-inheritance--emit-sharedmodels-and-extends) | the pi derive emits `sharedModels` and `extends` on both surfaces | open | [OQ-ML5](../design/model-lists-and-pickers.md#OQ-ML5) |
| [9](#9-the-yolo-check-notice-for-a-bypassed-append_systemmd) | `yolo check` names a bypassed `~/.pi/agent/APPEND_SYSTEM.md` | partially landed | [OQ-DA3](#OQ-DA3) |
| [10](#10-model-auth-for-an-isolated-agent-process) | an isolated agent process gets model auth without a copy of the login | sketched, open | [`isolated-agent-model-auth.md`](../design/isolated-agent-model-auth.md) |
| [11](#11-the-host--p-profile-governs-pi-subagents-children) | an explicit `yolo host -p` governs pi-subagents' child defaults and model scope | yolo's part built, off by default; waits on pi-subagents | [MM-D42](../design/model-lists-and-pickers.md#MM-D42) |

## 1. `remove:` for `config-list`

**Ask.** A `remove:` sibling so one contribution can take an element out of an array another
contribution owns, without restating the array.

**State.** `landed` 2026-10-15 (`e57c7862c`), on [OQ-DA4](#OQ-DA4)'s leaning. **Evidence:** a
`config-list` takes `remove:` beside `add` ([`contributes.go`](../../internal/packdecl/contributes.go)):
every element equal to a listed entry leaves the array the lower layers produced, an absent one is a
silent no-op, and within one contribution `remove` applies before `add`. It also takes `profile`, so
a gated `add` or `remove` applies only while that profile is active. The kind is documented in
[`pack-system.md`](../reference/pack-system.md#adding-entries-to-an-array-config-list) and
`yolo config-ref`.

**Why it matters.** An array-targeting overlay replaces the whole array, so without `remove:` the
only way to drop one element is to restate every sibling's — silently owning them.

**Landed.** A contribution can remove a named entry; removing an absent one is a no-op.

**Owner.** [OQ-DA4](#OQ-DA4), open only for the maintainer to ratify what was built.

## 2. A pack cannot declare the yolo version it needs

**Ask.** A pack names a minimum yolo, or `yolo pack lint --for-version <v>` judges a pack against
an older build, so prose legal on one build and fatal on another is checkable instead of tragic.

**State.** `declined` by the maintainer, 2026-10-15: *"Nobody's running old versions."* Neither
spelling exists, and neither will. What covers the case instead: since 0.12.0 a launch skips, with
a warning, a contribution holding a field its yolo does not know
([`skew.go`](../../internal/packdecl/skew.go)), and a host `yolo` older than the source it launches
refuses, naming `just install` (`version.SourceSkew`). The enforceability objection stays on the
record in [`keys.go`](../../internal/loopholedecl/keys.go).

**Owner.** [OQ-DA1](#decision-ledger), compacted.

## 3. A build has no version identity

**Ask.** Bake an identity into a build so two binaries can be told apart from the outside, and have
a broadcast clause record the version that added it.

**State.** `landed` 2026-10-15, on [OQ-DA2](#OQ-DA2)'s leaning. **Evidence:** `yolo --version
--full` adds the build stamp, the commit the binary was stamped with and the sha256 of the running
executable (`8198098ab`, [`identity.go`](../../internal/version/identity.go)); plain `--version` is
unchanged. `yolo config-ref` now says a content contribution naming neither `into` nor `agents`
means broadcast "as of 0.9.0" (`00dd7d635`).

**Owner.** [OQ-DA2](#OQ-DA2), open only for the maintainer to ratify what was built.

## 4. A reduced-window child

**Ask.** Stop a child inheriting a reduced-window parent when a same-`base` full-window sibling
exists.

**State.** `narrowed`; **yolo's part is done.** **Evidence:** `profiles.<name>.subagents.default`
ships ([`modelroles.go`](../../internal/packload/modelroles.go),
[`luahook.Subagents`](../../internal/agentcfg/luahook/derive.go)), the explicit form yolo chose over
an automatic floor, and `yolo config-ref` documents it under `profiles.<name>.subagents`
(`9ff3b207c`). Whether a profile declares it is the consumer's decision, not yolo's.

## 5. A clean replay discards the clone it built

**Ask.** (a) A clean-replay export hands over the clone instead of deleting it. (b) A signal names
a series gone stale against its upstream.

**State.** `settled` 2026-10-15 by the maintainer's ruling
[PF-D85](../design/patched-forks.md#PF-D85), which compacted
[OQ-PFK7](../design/patched-forks.md#decision-ledger). **Evidence:** `yolo pack rebase` and its clone are gone
([PF-D85](../design/patched-forks.md#PF-D85)): every conflict prints the git commands that rebase
the series, so the clone is the user's own, and `yolo pack status` prints the same commands under a
series' base lag when the newest version takes it as it stands, which re-anchors it. The base lag
names a stale series on `yolo pack status` and on the launch's fork line
([`baselag.go`](../../internal/packsrc/baselag.go)); no `yolo check` warning exists. A launch's
replay also drops a member upstream already carries, even one upstream edited again, as `git rebase`
does ([PF-D86](../design/patched-forks.md#PF-D86)).

**Owner.** [OQ-PFK7](../design/patched-forks.md#decision-ledger), compacted.

## 6. A declared model row loses the catalog's thinking levels

**Ask.** A `models.json` row that states no thinking levels keeps pi's catalog map, so
`--thinking xhigh` is not silently clamped on a declared model.

**State.** `open` on the reported surface; **partially landed** on the other. **Evidence:** the
`base` key inherits the pi catalog's thinking levels, inputs, limits and cost — but
`config-ref` states the two keys that do this are "read by PI's model-list render alone"
([`config_ref.txt`](../../internal/cli/config_ref.txt)), and the reported symptom is on a
`models.json` row. So the model-list surface is covered and the `models.json` derive is not.

**Landed.** Either the derive merges catalog facts into a declared row that states none, or the
record states plainly that `base` covers the model-list surface only.

**Owner.** [OQ-ML4](../design/model-lists-and-pickers.md#OQ-ML4).

## 7. The `carry` key

**Ask.** Let a `files` contribution name either `patches` or a `carry`: a git repository plus a
branch, resolved to a commit, against which the launch asks git whether the carry merges cleanly
(`git merge-tree`, no working tree) and refuses, naming the paths, when it does not. Report the
carry's lag behind upstream.

**State.** `open`. **Evidence:** no `Carry` field exists and `yolo pack lint` refuses
`json: unknown field "carry"`; worse, an unknown contribution field is *skipped* with a warning, so
a pack cannot migrate to it even if it were ignored. The full ruled model is the consumer's
(`pi/carries/`), not yolo's.

**Landed.** A `carry` contribution lints clean; the launch builds the merged tree keyed on the
resolved commit; a conflict refuses naming paths; naming both `patches` and `carry` is refused by
name; `patches` is otherwise unchanged.

**Owner.** [OQ-PFK6](../design/patched-forks.md#OQ-PFK6).

## 8. Model inheritance — emit `sharedModels` and `extends`

**Ask.** The pi derive and the model-lists resolver emit a `sharedModels` entry per referenced
model and `extends: "<name>"` on the rows, on **both** rendered surfaces —
`~/.pi/agent/models.json` and `~/.pi/agent/yolo-model-lists.json`.

**State.** `open` on the yolo side; the pi extension half is the consumer's. **Evidence:** zero
occurrences of `sharedModels` in `internal/`, `cmd/` or `packs/`.

**Landed.** (Verbatim acceptance from the consumer's design.) The rendered surfaces carry
`sharedModels`/`extends` rather than restated facts; no route reports pi's 128000/16384 defaults; a
test fails on a same-value restatement; an unresolvable reference is refused by `yolo check` and by
pi naming the row; a route lowering `contextWindow` under an inherited `workingContextWindow` fails
loudly rather than clamping.

**Owner.** [OQ-ML5](../design/model-lists-and-pickers.md#OQ-ML5). ⚠ `extends` is a different
mechanism from the `base` key of [ask 6](#6-a-declared-model-row-loses-the-catalogs-thinking-levels);
the consumer's design argues why `base` does not subsume it.

## 9. The `yolo check` notice for a bypassed `APPEND_SYSTEM.md`

**Ask.** A `yolo check` notice naming a `~/.pi/agent/APPEND_SYSTEM.md` that no projection mounts
into a jail, pointing at the briefing audience as the route.

**State.** `partially landed`. **Evidence:** the shipped `configuring-the-jail` skill states that
the host's `~` is never projected and names `host_files` and the briefing route
([`SKILL.md`](../../internal/jailcontent/builtinskills/configuring-the-jail/SKILL.md)); the
`yolo check` notice itself does not exist.

**Landed.** `yolo check` prints the notice.

**Owner.** [OQ-DA3](#OQ-DA3).

## 10. Model auth for an isolated agent process

**Ask.** A second Pi process in a jail, with its own agent directory and candidate extension code,
gets model auth without a copy of the login's refresh material.

**State.** `sketched`; nothing is authorized to build. **Evidence:**
[`isolated-agent-model-auth.md`](../design/isolated-agent-model-auth.md) lays out four options (an
access-only view, a proxy that injects auth, a provider-scoped key, mocked dispatch) and leaves
[OQ-IA1](../design/isolated-agent-model-auth.md#OQ-IA1) to [OQ-IA6](../design/isolated-agent-model-auth.md#OQ-IA6) open.

**Owner.** [`isolated-agent-model-auth.md`](../design/isolated-agent-model-auth.md#8-open-questions).

## 11. The host `-p` profile governs pi-subagents' children

**Ask.** An explicit `yolo host -p` profile governs pi-subagents' profile-derived child defaults and
model scope, not only the parent session.

**State.** `yolo's part built` 2026-10-15 (`985c2c8d5`), **off by default**. **Evidence:** the pi
pack hands the profile's `subagents` block as `--subagents-model-settings <json>` for one launch,
writing nothing, only under `YOLO_PI_SUBAGENTS_MODEL_SETTINGS=1`, because pi exits on a flag no
loaded extension registers and pi-subagents registers none yet. The Pi side accepted the interface
on 2026-10-15; the flag ships off until pi-subagents registers it.

**Owner.** [MM-D42](../design/model-lists-and-pickers.md#MM-D42).

## Open Questions

The three mechanism asks still open in a design doc of their own are owned there:
[OQ-PFK6](../design/patched-forks.md#OQ-PFK6),
[OQ-ML4](../design/model-lists-and-pickers.md#OQ-ML4),
[OQ-ML5](../design/model-lists-and-pickers.md#OQ-ML5); [OQ-PFK7](../design/patched-forks.md#decision-ledger)
is settled. Of the four with no other home, [OQ-DA1](#decision-ledger) is settled and the three below
remain; two of them only await ratification of what was built.

1. 💬 **OQ-DA2: What identity does a build carry beyond its revision?**

   Decides [ask 3](#3-a-build-has-no-version-identity). Two binaries built from one revision once
   printed the same version, so nothing could attribute a rendered artifact to its build or bisect by
   version. *Since 2026-10-15* option A is built (`yolo --version --full`); a ruling ratifies or
   reverses it.

   - **A — A binary hash.** `yolo --version --full` prints the revision and a hash of the binary.
   - **B — A build stamp declared at compile time.** A monotonically increasing build id beside the
     revision.
   - **C — A content hash of the flake inputs.** Reuses the image-identity hash the skew gate
     already computes.

   <!-- vantage: question id=OQ-DA2 leaning="A: the narrowest change that answers the question, and it needs no new build input. C is attractive only if a rendered artifact must be attributable to the flake that produced it rather than the binary that wrote it." -->

   _Leaning:_ A: the narrowest change that answers the question, and it needs no new build input. C
   is attractive only if a rendered artifact must be attributable to the flake that produced it
   rather than the binary that wrote it.

   **Answer:**
   > _(empty — fill in when decided)_

2. 💬 **OQ-DA3: Does `yolo check` warn about a bypassed `APPEND_SYSTEM.md`?**

   Decides [ask 9](#9-the-yolo-check-notice-for-a-bypassed-append_systemmd)'s remaining half. The
   skill already says the thing; the question is whether a diagnostic should repeat it.

   - **A — Yes, warn at `yolo check` when a `~/.pi/agent/APPEND_SYSTEM.md` exists on the host.**
   - **B — No.** The briefing skill is the one place the rule is stated; a second copy drifts.

   <!-- vantage: question id=OQ-DA3 leaning="A: the skill reaches an agent only if it reads the skill, while `yolo check` is the tool a human runs when a projection is not taking effect, and the message is one line." -->

   _Leaning:_ A: the skill reaches an agent only if it reads the skill, while `yolo check` is the
   tool a human runs when a projection is not taking effect, and the message is one line.

   **Answer:**
   > _(empty — fill in when decided)_

3. 💬 **OQ-DA4: Does `config-list` gain a `remove:` form?**

   Decides [ask 1](#1-remove-for-config-list). *Since 2026-10-15* option A is built, with a
   `profile` gate beside it; a ruling ratifies or reverses it. The kind's owner is
   [`pack-system.md`](../reference/pack-system.md#adding-entries-to-an-array-config-list), whose
   other live questions ([OQ-AL1](../reference/pack-system.md#oq-al1) to [OQ-AL4](../reference/pack-system.md#oq-al4)) share the additive-config-lists topic.

   - **A — A `remove:` key taking the same entry shape as `add`**, applied after additions, a
     removal of an absent entry being a no-op.
   - **B — No.** Keep the kind additive; a pack that must drop an entry restates the array.
   - **C — A `remove:` that takes a whole-array replacement instead**, leaving `config-overlay` as
     the only mechanism for it (rejected: same effect, more ambiguity).

   <!-- vantage: question id=OQ-DA4 leaning="A: it is the missing half of the kind, the leaking-ownership failure is real, and `OQ-AL3`'s own options already assume the mechanism can express a removal." -->

   _Leaning:_ A: it is the missing half of the kind, the leaking-ownership failure is real, and
   [`OQ-AL3`](../reference/pack-system.md#oq-al3)'s own options already assume the mechanism can
   express a removal.

   **Answer:**
   > _(empty — fill in when decided)_

## Decision Ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| — | A reduced-window child is the profile's declaration, not an automatic floor: `profiles.<name>.subagents.default` shipped instead. | 2026-10-12 | [ask 4](#4-a-reduced-window-child) | ✅ |
| OQ-DA1 | **Maintainer ruling, A — neither:** *"Nobody's running old versions."* No pack version declaration and no `yolo pack lint --for-version`. The unknown-field skip a launch has made since 0.12.0 and the source-skew refusal cover a pack ahead of its yolo. Rejected: B, an advisory declaration; C, a lint against an older build | 2026-10-15 | [ask 2](#2-a-pack-cannot-declare-the-yolo-version-it-needs) | — (nothing to build) |
