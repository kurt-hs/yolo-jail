---
title: "Doc triage, 2026-10"
date: 2026-10-15
status: in-review
stage: DESIGN
next: "The owner rules the items under Needs you; the to-do list shrinks as each lands"
tags: [docs, triage, cleanup]
summary: "What the October doc triage still needs: rulings and approvals from the owner, and a short to-do list. Everything already acted on (deleted, merged, graduated, kept) has left this record; git history has it."
vantage:
  status-chip: true
---

# Doc triage, 2026-10

**Status:** 2026-10-16. Only what still needs the owner, and the work still to do, is listed. A doc
that was deleted, merged or graduated left this record; a doc that stays as it is was never listed.
"Graduate" is [the house rule](../../AGENTS.md#where-things-live)'s word: a built design's lasting
content moves into a reference doc under `docs/reference/`, and the design doc is deleted.

## Needs you: rulings

### A. Rulings that unblock a graduation

"Graduate" means what [AGENTS.md](../../AGENTS.md#where-things-live) means by it: a built design's lasting content moves into a reference doc under `docs/reference/`, and the design doc is deleted.

#### 1. When two profiles in one active set both set the same pi key, which one wins? ([OQ-AP4](../design/active-provider-sets.md#OQ-AP4))
- **Why:** since 2026-10-08 a profile's config overlay applies for every member of the set, not only the first. So `-p pi=zai,openrouter`, with both packs setting `compaction.model`, now gives the value from whichever pack loads later. The order you typed plays no part. No shipped pack does this yet.
- **Options:** (A) the first profile you listed wins, so reordering `-p` changes the value. (B) pack order wins, as built, so `-p` order never matters. (C) refuse the launch and name both packs and the key.
- **My opinion:** A. You already read `-p` order as "this one first", and the change is one ordering rule plus a test.
- **To graduate:** move the unbuilt section 8 step 5 (several providers reached through routes) to the backlog, then move the doc into [`providers.md`](../reference/providers.md).

#### 2. Should yolo's messages print the user guide's web address instead of a `userguide/` repository path? ([OQ-DW3](../design/docs-website.md#OQ-DW3))
- **Why:** six messages name a guide page by repository path. Homebrew and release-archive users have no checkout, so for them that path points at nothing.
- **Options:** (a) print the site URL; (b) keep the path; (c) print both, URL first. A page's own address now loads in its `#/` form, for example `https://docs.yolo-jail.mschulkind.dev/#/guides/macos.md` (checked in headless Chromium on 2026-10-05). The catch: the site tracks `main`, so an old release can link to a page that describes newer behavior.
- **My opinion:** (a), in the `#/` form, plus a test that each page a message names exists under `userguide/`. A path names nothing for most users.
- **To graduate:** this is the stub's last question. Record the ruling in [`reference/docs-website.md`](../reference/docs-website.md) and delete the stub. The roadmap link needs repointing.

#### 3. Does the Kilo-specific code in the pi and claude config generators stay? ([OQ-GP4](../design/gateway-provider-packs.md#OQ-GP4))
- **Why:** a 2026-09-16 change made both generators spot Kilo by its name or URL. They rewrite `deepseek-` model ids, assume a context window and accept an unmapped model name as a literal id. That contradicts the doc's own section 3 rule. It also causes a live bug: claude believes **every** Kilo model has a 1M-token window, so on a smaller model it compacts too late ([§6](../design/gateway-provider-packs.md#6-the-kilo-special-casing-in-the-pi-and-claude-derives)).
- **Options:** (A) split it: keep the literal-id fallback for every provider, move context windows into the user's model map, and keep the `deepseek-` rewrite only with a dated source comment, or drop it. (B) keep everything and rewrite the rules to match. (C) remove everything.
- **My opinion:** A. The 1M-window assumption itself was removed on 2026-10-16, whatever you rule.
- **To graduate:** amend section 3, then fold the doc into [`providers.md`](../reference/providers.md), which already covers OpenRouter.

#### 4. Does a fetched pack's host-side half run at `yolo host` and on macos-user? ([OQ-HS5](../design/host-notch-services.md#OQ-HS5))
- **Why:** packs shipped with yolo and local packs (a path on your own disk) may already run code on the host outside any sandbox. A pack fetched from a `git+` URL is refused by name. Someone else's code would run on your machine at every launch that needs it.
- **Options:** (a) keep refusing; the workaround is a local clone. (b) allow it after a one-time approval at `pack install`, which brings back the approval prompt that [OQ-TP9](../design/trust-paths.md#decision-ledger) removed. (c) allow it the way a local pack is allowed, printing its command line before it starts.
- **My opinion:** (a). It matches your [OQ-EW1](../design/agent-event-watchers.md#14-decision-ledger) ruling for the sibling case, a host-side sidecar: only your own config may declare host code.
- **To graduate:** also record the 2026-10-03 Mac CI pass that its `next` names. Then split the content between [`wire-bridge.md`](../reference/wire-bridge.md) and [`host-agent-environment.md`](../reference/host-agent-environment.md).

#### 5. Is `yolo host apply` a narrow convenience, or a recommended way to configure agents? ([9.2](../design/host-render-target.md#9.2))
- **Why:** this is the "doesn't writing my real dotfiles defeat the sandbox?" worry from July. The guard its leaning relied on, refusing to install programs at the host, is gone: installs there now happen behind a confirm prompt ([9.4](../design/host-render-target.md#decision-ledger)).
- **Options:** "narrow", stated as a product position and defended in a doc; or "first-class", meaning the host is just the no-confinement setting.
- **My opinion:** close it as already answered. Your 9.1 ruling and [`what-yolo-is.md`](../reference/what-yolo-is.md) both say yolo manages the environment first and that `yolo host` is the no-confinement setting. That is the "first-class" answer, and the confirm prompts are the guard.

#### 6. Should a clash between `host_files` and a configured pack's file be caught by `yolo check`, not only at launch? ([9.6](../design/host-render-target.md#9.6))
- **Why:** two writers for one file are already refused, fatally. But that check runs in the launch path, so `yolo check` shows a pass and the launch then stops.
- **Options:** leave it (the leaning), or have `yolo check` run the same collision check after it loads the packs.
- **My opinion:** rule "leave it" for this doc and put the `yolo check` half on the backlog. The happy-path rule, that every stop names the next step, already holds, because the launch refusal names the fix.
- **To graduate (items 5 and 6):** with both closed, the doc goes to [`host-agent-environment.md`](../reference/host-agent-environment.md). The 9.N ids are cited from other docs and must keep an anchor.

#### 7. File the drafted mise bug report, or close the question without filing? ([SS-6](../design/jail-state-separation-design.md#ss-6))
- **Why:** mise's rust backend stores a project-specific Cargo path in a store entry shared across projects. yolo works around it with a gated cleanup step it may have to keep forever. The search found no existing upstream report, and a draft is written.
- **What changes:** filing costs you a few minutes in mise's Discussions and might one day let yolo delete that cleanup. Closing unfiled changes nothing in code. The harm measured on mise 2026.8.6 is narrow: the entry flips between projects, but a toolchain does not break mid-session.
- **My opinion:** file it, since the draft is ready (the jail cannot post it), and expect nothing.
- **To graduate:** the body already lives in the reference doc. Delete the stub, then rename the reference (see section D).

#### 8. Does `jail_env` stay refused for pack-shipped loopholes? ([OQ-LP5](../design/loophole-packaging.md#OQ-LP5))
- **Why:** this asks whether a pack may set an environment variable only when its loophole actually turns on. A loophole is a host capability wired into the jail. The one pack that wanted this, `audio`, no longer needs it: since [LP-D1](../design/loophole-packaging.md#LP-D1) its variables reach only a jail that binds the loophole.
- **What changes:** keeping the refusal changes nothing. Allowing it needs a new check for collisions across contribution kinds.
- **My opinion:** keep the refusal and close the question. No shipped pack needs this today.

#### 9. Does the `guest` setting get its own list of allowed config fields, or keep borrowing the host's? ([OQ-LP7](../design/loophole-packaging.md#OQ-LP7))
- **Why:** `guest` is the setting for a sandboxed process on the real machine, which is what macos-user does. Today it reuses the host's field list. Nothing reads that funnel: `guest` cannot be selected yet.
- **My opinion:** close it here and move it to the guest-notch questions in [`environment-manager-user-stories.md`](../design/environment-manager-user-stories.md), its own `next` says the two belong together. Write the separate list when `guest` is built, not before.
- **To graduate (items 8 and 9):** add an `LP-D1` anchor to [`loophole-system.md`](../reference/loophole-system.md), which has none today. Repoint the comments in 27 code files that cite `loophole-packaging`, then delete the stub.

#### 10. Is there an escape hatch for a mistyped pack, provider or other name? ([OQ-RM4](../design/reference-mismatch-diagnostics.md#OQ-RM4))
- **Why:** a mistyped name is now fatal. Other fatal stops have a `YOLO_ALLOW_*` override, but those cover conditions outside your config, such as being offline. The build shipped with no override.
- **Options:** no override; or `YOLO_ALLOW_UNMATCHED_REFERENCES`.
- **My opinion:** no override. The fix (correct the name) is always shorter than the workaround, and your rule that overrides are not for yolo's own bugs points the same way.
- **To graduate:** fold the doc into [`stringly-typed-references-principle.md`](../reference/stringly-typed-references-principle.md) as its as-built section.

#### 11. Should renaming a git remote ask for approval again? ([OQ-WW1](../design/workspace-widening.md#OQ-WW1))
- **Why:** the GitHub broker's approval record now keys a repository by the remote it came from. So `git remote rename origin upstream` asks once at the next fresh launch, in every project where the broker runs.
- **Options:** (a) keep it, as built: any change in where a repository comes from asks. (b) record remotes by kind only, so a rename asks nothing. Under (b), a second remote an agent added for the same repository keeps it in scope after you delete `origin`, and nobody sees it.
- **My opinion:** (a). Renames are rare, and (b) reopens the case its record exists to catch.
- **To graduate:** no reference covers the broker's scope yet. Write a short section in [`loophole-system.md`](../reference/loophole-system.md), or a new github-broker reference, from section 3 and the WW-D ledger.

#### 12. Read-only files at the host and in `host_files`: one decision covering three questions ([OQ-B](pack-host-management-plan.md#open-questions), [E1](BACKLOG.md#E1), [E2](BACKLOG.md#E2))
- **Why:** yolo marks some delivered files mode `0o444`, read-only by permission bits. That is advice, not enforcement: anyone can `chmod` the file, and Claude runs as root, so it ignores the bits.
- **The three questions:** should pack files written into a real home be `0o444` (OQ-B)? Should the `copy` mode merge into `readonly`, cutting `host_files` modes from four to three (E1)? Should `readonly` become a real read-only mount (E2)?
- **Example:** you hand-edit a pack-delivered file at the host after a `chmod`, and the next `yolo host apply` overwrites your edit.
- **Leanings:** OQ-B keeps `0o444`; E1 keeps four modes; E2 keeps `0o444` and changes the wording from "read-only" to something like "advisory".
- **My opinion:** take all three leanings as one ruling, so nothing changes in code, only words. A real read-only mount is real work on a live boot path and stops nothing a root agent can't get around.
- **To graduate:** this also makes [CFP-1](../design/composed-file-permissions.md#CFP-1) moot, because it only arises if E2 is "yes". The plan then folds into [`pack-system.md`](../reference/pack-system.md).

### B. The merge row waiting on a ruling

#### 13. Does the agent-directory map absorb the "trapped dotfile" diagnostics? ([OQ-AM8](../design/agent-directory-map.md#OQ-AM8))
- **Why:** two docs propose a way to warn when a file such as pi's `APPEND_SYSTEM.md` has no effect in the jail: [the design](../design/pack-declared-file-diagnostics.md) and [its plan](../design/pack-declared-file-diagnostics-plan.md). The agent-directory map already classifies every path in an agent's directory and can carry a per-entry note.
- **Options:** A, a trap is a map entry with a note; the two docs are superseded and their three questions close. B, keep both kinds, so two declarations name the same paths.
- **My opinion:** A. This can be ruled alone, ahead of the map's other nine questions. Once ruled, move the motivating examples into the map doc and delete both small docs.

### C. Pack-system question

#### 14. Is anything left of the "host daemon outlives its pack tree" problem? ([OQ-PK3](../reference/pack-system.md#oq-pk3))
- **Why:** this was asked when a host daemon could outlive the launch that started it. The pack tree its `{loophole_dir}` token points at could then be deleted under it. [HD-R1](../design/host-daemon-ownership.md#HD-R1) is now built: every host daemon starts and ends with one launch. The only exception is a daemon marked `detach`, finishing one request after its jail ends.
- **What changes:** closing it changes no code. I checked that no shipped pack uses `{loophole_dir}` in a host daemon's command.
- **My opinion:** close it as dissolved, as leaned. If a third-party pack ever needs that, the cheapest fix is to refuse the token in a host daemon's command and say why.

### D. Questions the graduations turned up

#### 16. Are the pi extension's known gaps acceptable for the active provider set? ([§3.4](../design/simultaneous-auth-and-pack-isolation.md#34-what-the-extension-cannot-close))
- **Why:** the doc's own `next` asks for your review before it graduates. The extension that keeps pi on the selected providers cannot stop: pi reading (and refreshing) a blocked provider's saved login; a `models.json` row's `!command` running before the block; `pi --no-extensions` and pi-subagents' external runners; `pi auth …` and `pi update models`; a pi older than 0.81.0.
- **What changes:** "acceptable" means these are written down as known gaps and the doc graduates into [`providers.md`](../reference/providers.md). "Must close" means patching pi itself.
- **My opinion:** acceptable. It is a policy about which provider is used, never a security boundary.

#### 17. Should the wire bridge's in-jail daemon move into the macos-user guest? ([JD-4](../reference/macos-user-provisioning.md#JD-4))
- **Why:** on macos-user the bridge runs its host half, launch-owned, because the guest has no place for the daemon's endpoint file. Moving it would retire that host half.
- **What changes:** "no" changes nothing; "yes" is a build that gives the guest a services directory.
- **My opinion:** no, unless the host half causes a real problem; it works today.

## Needs you: yes or no

Each is a reorganization that would leave fewer, clearer docs. Checked against the files on
2026-10-16.

Checked against the files on 2026-10-16.

- **`research/macos-support-matrix.md` → move to `docs/plans/`.** It is a living tracker, not research. I would not merge it into `backend-parity.md`, which lists drops per backend rather than a test matrix. Cut its section 3 feature-by-runtime table in favor of [`settings-per-setup.md`](../../userguide/reference/settings-per-setup.md). Note that it also has open [OQ-SH1](../research/macos-support-matrix.md#OQ-SH1) ("is macos-user self-hosting worth pursuing?"), which the triage row misses. *Opinion: yes.*
- **`plans/README.md` → cut the index to a short pointer list and point at [`roadmap.md`](roadmap.md).** Its own header says rows drift. Move its "Keeping this corpus honest" checks into a standalone doc, for example `docs/reference/doc-corpus-checks.md`, because AGENTS.md cites them. Keep the old anchor that three docs link. *Opinion: yes. This is the biggest clarity gain.*
- **`design/provisioner-evidence.md` → leave it alone for now.** You split it out on 2026-09-20 to keep [`provisioner-sets.md`](../design/provisioner-sets.md) decidable, and that doc still has open questions. Fold it back, or into a reference doc's appendix, only when provisioner-sets graduates. *Opinion: no action now.*
- **`storage-and-config.md` §Identity → replace it with a two-line pointer to [`git-identity.md`](../reference/git-identity.md).** The section restates the same composition, the read-only mount and the include warning ([§Identity](../reference/storage-and-config.md#identity)). *Opinion: yes.*
- **`loophole-transport.md` §Cooperative host-daemon startup refusal → cut it to a pointer to [`host-service-startup-diagnostics.md`](../reference/host-service-startup-diagnostics.md).** The section duplicates that doc's attempt-reason channel: the same fd-3 record, the same classes, the same limits. *Opinion: yes.*
- **`reference/jail-state-separation-design.md` → rename it to `jail-state-separation.md` once the design stub is deleted** (item 7). The "-design" suffix is wrong for a reference doc. Six files link to it. *Opinion: yes, after item 7.*
- **Design-doc twins of reference docs.** The `agent-program-runtimes`, `macos-user-home-tiers` and `macos-user-provisioning` design stubs are **already deleted**. The `docs-website` stub goes with item 2. The [`composed-file-permissions`](../design/composed-file-permissions.md) stub still holds **three** open questions, not one as the triage row says. CFP-1 falls away with item 12. CFP-2 (a table of degraded features for macos-user and Apple Container) and CFP-3 (the scope of the capture records) need a ruling before the stub can be folded in. *Opinion: rule CFP-2 yes and CFP-3 "keep per-workspace" in the item-12 sitting, then fold.*
- **`reference/pack-system.md` → split it.** It is about 4,200 lines. Its "The kinds" section, about 1,700 lines, becomes `reference/pack-kinds.md`. "Config surfaces and the compose engine" becomes its own doc. Move its seven open questions (ST2, AL3, AL4, DR1, PK1, ACP4, PK3) out to a design doc: a reference doc should not hold live questions, and its frontmatter still says `stage: DESIGN`. The cost is repointing many anchors. *Opinion: yes, as its own lane.*
- **`design/jail-lifetime-last-session-wins.md` → move section 9, "The keeper" (about 1,200 lines, built), into a new `reference/jail-keeper.md`.** Keep the design doc only for [OQ-JL10](../design/jail-lifetime-last-session-wins.md#OQ-JL10) and the Mac reruns it still owes. *Opinion: yes.*
- **`design/model-lists-and-pickers.md` → graduate it.** It has **no open questions left**; the triage row's "3 open" is stale. Move section 14 (about 900 lines of per-agent menu research) to `docs/research/`. Move the built rulings into [`providers.md`](../reference/providers.md). *Opinion: yes.*
- **`design/program-delivery.md` → do not split yet.** Rule [OQ-PD19 to PD21](../design/program-delivery.md#oq-pd19) first, then graduate the built parts in one pass. Splitting now means moving its anchors twice. *Opinion: no for now.*
- **`research/orbstack-as-a-podman-host-plan.md` → move it to `docs/plans/`**, or beside its design as the `-plan` companion. It is a build hand-off in the wrong directory. *Opinion: yes, trivial.*

## Needs you: hands-on steps

Things only you can do, on your own machines.

- **On your Mac, after the next upgrade:** the passwordless macos-user checks in [§11 of the manual-checks runbook](runbooks/macos-user-manual-checks.md), starting with `yolo macos-setup`; and dispatch `gh workflow run macos-user.yml`.
- **On this host:** set `min-free` and `max-free` in the host's `nix.conf`, then run the host check in [owed host steps](../reference/image-retention.md#owed-host-steps).
- **mise:** if you rule item 7 "file it", post the drafted report in mise's Discussions (the jail cannot post).

## To do (mine)

Removed as each lands.

- Graduate [`model-lists-and-pickers.md`](../design/model-lists-and-pickers.md): no question is open in it.
- Act on each "yes" above as it is ruled.

