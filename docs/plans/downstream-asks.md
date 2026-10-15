---
title: "What a downstream pack consumer is still waiting on from yolo"
date: 2026-10-13
status: in-review
stage: DESIGN
next: "Rule OQ-DA1 and OQ-DA2 (pack version identity), then the four mechanism questions in their owning design docs; OQ-DA3 is a small diagnostics build"
depends-on:
  - ../design/patched-forks.md#OQ-PFK6
  - ../design/patched-forks.md#OQ-PFK7
  - ../design/model-lists-and-pickers.md#OQ-ML4
  - ../design/model-lists-and-pickers.md#OQ-ML5
tags: [handoff, downstream, packs, launcher, tracker]
summary: "The yolo-side index of the asks a downstream pack consumer (the dotfiles repository) carried in its 2026-10-13 handoff: which are still open at HEAD, what a landed version would look like, and where each one's design question is owned. It exists because none of these were in yolo's own docs before now."
vantage:
  status-chip: true
---

# What a downstream pack consumer is still waiting on from yolo

**Status:** 2026-10-13, re-verified in-tree at `80e08f98b` (the handoff was checked at
`0.13.0+51.g99f36da2e`, eight commits back). **Why this file exists:** the handoff's nine asks
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
| [1](#1-remove-for-config-list) | a `remove:` sibling for `config-list` | open | [`pack-system.md`](../reference/pack-system.md#adding-entries-to-an-array-config-list) (context); question in [OQ-DA4](#OQ-DA4) |
| [2](#2-a-pack-cannot-declare-the-yolo-version-it-needs) | a pack can declare a minimum yolo, or lint judges it against an older build | open | [OQ-DA1](#OQ-DA1) |
| [3](#3-a-build-has-no-version-identity) | two binaries from one revision are distinguishable, and a broadcast clause records its version | open | [OQ-DA2](#OQ-DA2) |
| [4](#4-a-reduced-window-child) | a reduced-window child does not silently inherit the small window | narrowed — yolo's part done | decision recorded below; the owner's declaration is the consumer's |
| [5](#5-a-clean-replay-discards-the-clone-it-built) | a clean replay keeps the clone, and a stale series is signalled | open | [OQ-PFK7](../design/patched-forks.md#OQ-PFK7) |
| [6](#6-a-declared-model-row-loses-the-catalogs-thinking-levels) | a `models.json` row keeps pi's catalog thinking levels | open | [OQ-ML4](../design/model-lists-and-pickers.md#OQ-ML4) |
| [7](#7-the-carry-key) | a `files` contribution may name a commit graph instead of a series | open | [OQ-PFK6](../design/patched-forks.md#OQ-PFK6) |
| [8](#8-model-inheritance--emit-sharedmodels-and-extends) | the pi derive emits `sharedModels` and `extends` on both surfaces | open | [OQ-ML5](../design/model-lists-and-pickers.md#OQ-ML5) |
| [9](#9-the-yolo-check-notice-for-a-bypassed-append_systemmd) | `yolo check` names a bypassed `~/.pi/agent/APPEND_SYSTEM.md` | partially landed | [OQ-DA3](#OQ-DA3) |

## 1. `remove:` for `config-list`

**Ask.** A `remove:` sibling so one contribution can take an element out of an array another
contribution owns, without restating the array.

**State.** `open`. **Evidence:** the `config-list` struct holds only `Path` and `Add`
([`contributes.go`](../../internal/packdecl/contributes.go)), and a contribution carrying `remove:`
is refused by `yolo pack lint` with `json: unknown field "remove"`.

**Why it matters.** An array-targeting overlay replaces the whole array, so without `remove:` the
only way to drop one element is to restate every sibling's — silently owning them.

**Landed.** A contribution can remove a named entry; removing an absent one is a no-op.

**Owner.** [OQ-DA4](#OQ-DA4). The kind itself is documented in
[`pack-system.md`](../reference/pack-system.md#adding-entries-to-an-array-config-list).

## 2. A pack cannot declare the yolo version it needs

**Ask.** A pack names a minimum yolo, or `yolo pack lint --for-version <v>` judges a pack against
an older build, so prose legal on one build and fatal on another is checkable instead of tragic.

**State.** `open`. **Evidence:** neither spelling exists; `min_yolo` appears once, in a comment
explaining why a floor is *not enforceable* — the reader that needs stopping is the build that
shipped before the key existed, to which the key is one more tolerated unknown
([`keys.go`](../../internal/loopholedecl/keys.go)).

**Landed.** A launch refuses with the pack's stated floor, or lint reports what an older build
would reject.

**Owner.** [OQ-DA1](#OQ-DA1).

## 3. A build has no version identity

**Ask.** Bake an identity into a build so two binaries can be told apart from the outside, and have
a broadcast clause record the version that added it.

**State.** `open`. **Evidence:** `yolo --version` prints a revision stamp and no hash of the binary
(`internal/version`). Two binaries with different behavior printed the same string once.

**Landed.** `yolo --version --full` (or equivalent) carries the commit and a build hash; a broadcast
clause names the version that introduced its form.

**Owner.** [OQ-DA2](#OQ-DA2).

## 4. A reduced-window child

**Ask.** Stop a child inheriting a reduced-window parent when a same-`base` full-window sibling
exists.

**State.** `narrowed`; **yolo's part is done.** **Evidence:** `profiles.<name>.subagents.default`
ships ([`modelroles.go`](../../internal/packload/modelroles.go),
[`luahook.Subagents`](../../internal/agentcfg/luahook/derive.go)), the explicit form yolo chose over
an automatic floor. Whether a profile declares it is the consumer's decision, not yolo's.

## 5. A clean replay discards the clone it built

**Ask.** (a) A clean-replay export hands over the clone instead of deleting it. (b) A signal names
a series gone stale against its upstream.

**State.** `narrowed`. **Evidence:** `yolo pack rebase` and its clone are gone
([PF-D85](../design/patched-forks.md#PF-D85)): every conflict prints the git commands that rebase
the series, so the clone is the user's own, and `yolo pack status` prints the same commands under a
series' base lag when the newest version takes it as it stands, which re-anchors it. The base lag
names a stale series on `yolo pack status` and on the launch's fork line
([`baselag.go`](../../internal/packsrc/baselag.go)); no `yolo check` warning exists.

**Landed.** (a) a clean replay leaves a built clone; (b) `yolo check` or a status line names a
series whose base moved.

**Owner.** [OQ-PFK7](../design/patched-forks.md#OQ-PFK7).

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

## Open Questions

The four mechanism asks with a design doc of their own are owned there:
[OQ-PFK6](../design/patched-forks.md#OQ-PFK6), [OQ-PFK7](../design/patched-forks.md#OQ-PFK7),
[OQ-ML4](../design/model-lists-and-pickers.md#OQ-ML4),
[OQ-ML5](../design/model-lists-and-pickers.md#OQ-ML5). The four below have no other home.

1. 💬 **OQ-DA1: Does yolo adopt a pack version declaration, a lint against an older build, or neither?**

   Decides whether a pack can describe the build it needs ([ask 2](#2-a-pack-cannot-declare-the-yolo-version-it-needs)).
   The enforceability objection is on the record
   ([`keys.go`](../../internal/loopholedecl/keys.go)): a floor binds only future deletions, never
   the old build that would trip on it.

   - **A — Neither.** Keep the release note as the only record, as `keys.go` argues.
   - **B — An advisory declaration.** A pack states its minimum yolo; yolo warns (never refuses) on
     a newer build, and lint judges a pack against a named build.
   - **C — Gate `yolo pack lint --for-version`.** No manifest key; lint runs a pack's declarations
     past an older build's rules and reports what it would reject.

   <!-- vantage: question id=OQ-DA1 leaning="B and C together: a declaration that is advisory plus a lint that is authoritative, since the unenforceable floor is still useful as prose a human reads, and the lint is the half that can actually stop a bad pack before a launch." -->

   _Leaning:_ B and C together: a declaration that is advisory plus a lint that is authoritative,
   since the unenforceable floor is still useful as prose a human reads, and the lint is the half
   that can actually stop a bad pack before a launch.

   **Answer:**
   > _(empty — fill in when decided)_

2. 💬 **OQ-DA2: What identity does a build carry beyond its revision?**

   Decides [ask 3](#3-a-build-has-no-version-identity). Two binaries built from one revision once
   printed the same version, so nothing could attribute a rendered artifact to its build or bisect by
   version.

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

3. 💬 **OQ-DA3: Does `yolo check` warn about a bypassed `APPEND_SYSTEM.md`?**

   Decides [ask 9](#9-the-yolo-check-notice-for-a-bypassed-append_systemmd)'s remaining half. The
   skill already says the thing; the question is whether a diagnostic should repeat it.

   - **A — Yes, warn at `yolo check` when a `~/.pi/agent/APPEND_SYSTEM.md` exists on the host.**
   - **B — No.** The briefing skill is the one place the rule is stated; a second copy drifts.

   <!-- vantage: question id=OQ-DA3 leaning="A: the skill reaches an agent only if it reads the skill, while `yolo check` is the tool a human runs when a projection is not taking effect, and the message is one line." -->

   _Leaning:_ A: the skill reaches an agent only if it reads the skill, while `yolo check` is the
   tool a human runs when a projection is not taking effect, and the message is one line.

   **Answer:**
   > _(empty — fill in when decided)_

4. 💬 **OQ-DA4: Does `config-list` gain a `remove:` form?**

   Decides [ask 1](#1-remove-for-config-list). The sibling `add` is built; the kind's owner is
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
