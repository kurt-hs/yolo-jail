---
title: "What may a session home contain, and what may its reaper delete?"
date: 2026-10-28
status: proposed
stage: DESIGN
tags: [design, sessions, host, prune, capture, pi]
summary: "The reaper's job is to prove that deletion destroys no unaccounted byte, and the current inventory check tries to prove that by demanding a closed world: that a config directory a program believes is its own contains only the files yolo rendered. That world was never promised. A session home isolates the rendered surfaces only; every other byte that appears in it is program-owned content addressed to the real config directory, held by yolo as custodian, not owner. This review redefines the reaper's requirement as a classification against the originating launch rather than the current pack selection, gives strays one fate on every path and backend, removes janitorial work from the launch's critical path, and separates the smallest fix that ends the lockout from the durable change."
---

# What may a session home contain, and what may its reaper delete?

## 1. Terms and scope

A **surface** is a file yolo renders into a program's config directory from a pack declaration (for pi, `.pi/agent` via `PI_CODING_AGENT_DIR`). A **session home** is the per-launch directory that stands in for that config directory: it contains this launch's render of the surfaces as real files, symlinks to everything else in the real directory, and capture sidecars. **Capture** is the end-of-session diff of each surface against its baseline sidecar (e.g. `<agent>-<name>.last_render`), folded into the host capture store so user edits to config survive. A **stray** is an entry in a session home that the declaration did not foresee: a new entry the program created, or a symlink the program replaced with a real file or directory by renaming over it. The **reaper** (`ReapHostSessionHomes`) is the sweep that captures and removes ended host session homes whose `.lease` no process holds. The **host notch** is the unconfined host arm; a **jail** is the confined arm (podman, Apple Container, or macos-user backends).

This review is reasoning from the measured facts in the review brief, including the 2026-10-27/28 `pi-web-access` lockout. Nothing below adds new measurements; unknowns are marked UNMEASURED.

**Status 2026-10-28.** Built for the host notch, in two commits: `5f215ed10` (a stray is captured,
kept whole, named and never a launch refusal; one rule on the reap and resident paths; `yolo prune`
reports a kept home) and `6be8f6439` (the launch manifest, SHR-D3; repatriation, SHR-D4; kept-home
reporting, SHR-D9). Not built: the jail boot's retire path still keeps a stray home and warns
rather than repatriating it — it loses nothing today, so parity (SHR-D5) is a follow-up. Open:
SHR-Q2's audit, which decides whether link replacement is the common case or a one-extension
accident.

## 2. Q1: the reaper's requirement

The requirement is a property about destruction, not about directory contents:

> **No byte is deleted unless it is provably regenerable, provably someone else's (a link), or already preserved somewhere the user can reach.**

Deletion of a session home is safe exactly when every entry falls in one of five classes:

- **(a) Rendered and captured** — a rendered surface whose capture has succeeded, or which provably matches its baseline.
- **(b) A symlink** — deleting it destroys nothing.
- **(c) Yolo's own** — its capture sidecars and `.lease`.
- **(d) Yolo's own scaffolding** — an entry created for the render that holds no program data.
- **(e) A preserved stray** — first moved aside, or adopted into the real directory per [§3](#3-q2-what-a-stray-is-and-its-fate).

Anything unclassifiable fails the property and must be *kept*, which is a statement about the reaper's hands, not about the next launch.

"The directory matches the current render inventory" is not a valid proxy for this property, for three reasons. First, **closed world vs. open directory**: the program was told the session home *is* its config directory. It may create any entry a config directory may acquire, and third-party extensions do. The render inventory enumerates yolo's outputs; it was never a promise about the program's. SP-D3 already knows this ("strays are kept and named"), so the inventory check enforces a world the design explicitly disclaims. Second, **wrong point in time**: accountability attaches to the launch that built the home — what it rendered, what it linked, what baselines it wrote. Judging an old home against the *current* pack selection makes every legitimate pack edit retroactively suspicious and, worse, makes the check fail for reasons (a removed surface) that say nothing about whether the bytes on disk are safe to delete. Third, **it confuses two obligations**: capture accountability (did each *surface* get diffed?) is real and surface-shaped; occupancy (what else is in the directory?) is not yolo's to account for at all. The check welds them, so a 20-byte version marker fails a test that exists to protect `settings.json` edits.

What the reaper can know at reap time that it does not use today: the retained baseline sidecars already name the originating launch's stateful surfaces, and `lstat` distinguishes links from real entries — the (a)/(b)/(c) classification above needs no reference to the current selection for the deletion decision. The current selection is needed only to *perform* capture (to interpret a surface's schema), and capture failure should retain that surface's bytes, not escalate. What is missing — UNMEASURED whether it exists beyond the baselines — is a complete **launch manifest**: a yolo-written record, kept in the home's scratch area outside the declared directory, listing rendered paths, linked names and their targets, and sidecar names at build time. With it, classification at reap time is exact and entirely against the originating launch; without it, the reaper is reduced to guessing provenance from today's packs, which is how the inventory check happened.

## 3. Q2: what a stray is, and its fate

The three existing answers — jail keeps and warns, host resident path deletes silently, host reap path refuses forever — are three different products. Parity (the maintainer's stated principle for the host arm) requires one. The silent delete is the worst of the three: it is the only one that loses data, and it makes the reap path's caution pointless, since the same stray survives or vanishes depending on which code path closes the session.

The correct fate follows from whose bytes they are. A stray is program-owned state that the program addressed to its real config directory — from inside, there is no other directory. Its intended lifetime is therefore the real directory's, not the session's. But automatic adoption is not always safe: if the name already exists in the real directory (the replaced-link case), the stray is a *divergent copy*, and merging two file trees silently is a decision yolo cannot make. Hence **repatriation**, a defined end-of-session phase distinct from capture:

| Stray form | Fate | Rationale |
|---|---|---|
| New top-level name, absent from the real directory | **Adopt**: move it into the real directory, report in one line | It is where the program meant it to be; next launch links it normally |
| Name exists in the real directory (incl. a shared link replaced by rename) | **Keep aside**: move the home (or the conflicting entry, with sidecars) outside the swept root, report path and step | Two divergent trees; only the user can merge or discard |
| Entry inside a rendered surface's parent tree that no surface renders (e.g. `settings.json.old`) | Keep aside, as above | Bytes are preserved; capture of surviving surfaces is unaffected |
| Symlinks, rendered-and-captured surfaces, sidecars, `.lease` | Delete with the home | Classes (a)–(d) of [§2](#2-q1-the-reapers-requirement) |

The `pi-web-access` case is row one: all three preserved homes show the name **absent** from the
real directory at build time. Whether a later session turns it into row two depends on how the
extension writes, which is UNMEASURED (see [§7](#7-q6-smallest-change-durable-change-evidence)):
the maintainer created the real directory after the lockout, and no ended home has yet been
judged since, so "the program replaces the link" is a hypothesis, not a measurement. It matters,
because if the program writes *through* an existing link then declaring the name shared **is**
the cure, and the jail warning that recommends it is right. What is certain either way is that
the declaration cannot be the *only* remedy: it is unavailable to a third-party extension whose
name the pack cannot know in advance, and the first session to run one creates a stray regardless.

## 4. Q3: is the symlink farm the right mechanism

A symlink is not transparent directory semantics. Programs that write atomically (write temp, rename over target) replace links as a matter of course; programs that resolve paths or `mkdir` existing names behave differently again. The farm asks every present and future extension to write *through* links it cannot see. That is the mechanism's real exposure — but the alternatives must be weighed on all three backends plus the host notch, where there are no namespaces:

| Option | Isolation of profiles | New program entries land… | Host notch | Main cost / risk |
|---|---|---|---|---|
| (a) Symlink farm (today) | Per-launch real rendered files; isolation holds even when links are replaced | In the throwaway home (recoverable via repatriation) | Works; no privileges | Links break under rename — expected, handled in [§3](#3-q2-what-a-stray-is-and-its-fate), not prevented |
| (b) Bind the real directory as the session view, bind rendered files over the surface paths | Per-launch mount view; surfaces private, rest genuinely shared | In the real directory, by construction | **Unavailable** (no mount namespace at the host notch) | Rename over a bind-mounted file, concurrency, per-backend mount plumbing — all UNMEASURED; a plain overlay does *not* have this property (new entries land in the upper layer, i.e. the throwaway) |
| (c) Home holds only rendered surfaces; everything else reached "another way" | Surfaces only | Nowhere defined | No mechanism named | The program takes exactly one config directory; without links or mounts there is no other way. Copying breaks shared state (`auth.json`, `sessions/`) and concurrent launches |

Verdict: **(a) is the right portable substrate, and the wrong contract.** The farm's core job — no launch ever reads another launch's rendered profile — survives link replacement, because surfaces are real files, not links; what breaks is only the *sharing* of non-surface entries, which repatriation repairs after the fact. Option (b) is semantically the cleanest where it exists, but it cannot be the parity mechanism: adopting it in jails would make jail and host diverge in exactly the behavior under review, and would hide the stray class in jails while it continues at the host. The durable fix is therefore not a new view mechanism but a corrected contract for (a): links are hints the program may legitimately defeat, and defeat is a normal, handled event. Option (b) should be revisited only on evidence that programs defeat the farm in a way repatriation cannot repair — e.g. rename over a *rendered surface* breaking capture, or rename over the `auth.json` link forking credentials (both UNMEASURED, see [§7](#7-q6-smallest-change-durable-change-evidence)). That evidence, not elegance, is what would pay for per-backend mount machinery.

## 5. Q4: should reaping refuse a launch

Reaping is janitorial work performed during an unrelated launch. Coupling them means one dead session's housekeeping can deny service indefinitely — the measured lockout. What does refusal buy? Attention, and a guarantee that nothing proceeds while bytes are unaccounted. But preservation buys the same guarantee without the denial: a home that is kept aside is exactly as safe as a home whose existence blocks a launch. The only thing refusal adds is coercion, and coercion is only legitimate when the user can act on it.

So the property that justifies a launch refusal is: **proceeding would itself cause irreversible loss or break isolation for the new launch, in a way retention cannot prevent — and the stop names a step that works.** That covers the new launch's own failures: a session home that cannot be built (SP-D6), a refused surface render (SP-D26), a real directory missing so no honest home exists. It covers nothing on the reap list. An extinct baseline (a surface the current packs no longer declare) means those edits cannot be captured *now*; keeping the home aside preserves them until the pack is restored or the user discards them. Blocking every launch until then protects nothing that retention does not. Accordingly: janitorial reaping during a launch **never refuses the launch**; it keeps, warns once, and proceeds. `yolo prune --apply`, whose whole job is the sweep, may exit non-zero and report kept homes as its result — that is a report to a user who asked, not a gate on a user who didn't. Every keep-aside message must name a step that works: the kept path, and the two real resolutions (move the entry into the real directory, or delete the kept home).

## 6. Q5: the missing requirement

The owner is right; here it is. The design calls the session home "a throwaway directory," and that adjective was applied to the wrong noun. What is throwaway is the **composition** — this launch's arrangement of rendered files and links. The **contents** have three different owners and three different lifetimes, and the design never stated the rule that separates them:

> **A session home isolates rendered surfaces only. Everything else that appears in it is program-owned content addressed to the real config directory, because from the program's side the session home is that directory. Yolo owns what it rendered, linked, and wrote as sidecars, and may delete exactly that (after capture); for everything else it is a custodian whose only duties are to preserve and to hand back.**

Three corollaries were missing and are needed:

1. **Lifetime** — non-surface contents inherit the real directory's lifetime, not the session's; "session home removed at session end" (SP-D24, [§4.7](per-session-profiles.md#47-the-host) of the design) is a statement about the composition.
2. **Capture's remit** — capture answers for surfaces: config edits diffed against baselines, and nothing else. "Anything the user may miss" is preservation's remit, discharged by repatriation, not by stretching capture into an inventory of the unknown.
3. **The negative duty** — unknown contents are the reaper's business only in that it must not destroy them; it never has to account for them, classify their meaning, or obtain a declaration covering them.

The inventory check was invented precisely because corollary (3) was missing: with no rule saying unknowns are preserved-and-ignored, the only safe-looking options were "account for everything" or "delete and hope," and the codebase picked one on each path.

## 7. Q6: smallest change, durable change, evidence

**Smallest change** — accept the staged fix with two amendments. Accept: an unrendered regular file is a stray; capture the surfaces; move the home to `<state>/host-session-homes-kept/<name>`; warn once naming strays, kept path, and a working next step; let the launch proceed. Amend: (i) apply the identical classification on the resident `Close` path — this closes the silent-delete, the only actual data-loss path; (ii) drop the residual refusals for extinct baselines and unreadable roots *as launch refusals* (per [§5](#5-q4-should-reaping-refuse-a-launch), they become keep-aside plus warning during a launch, non-zero only under `yolo prune --apply`). Adjust the two pinning tests: the renamed-baseline and `settings.json.old` cases now expect capture of surviving surfaces plus a kept home, not a refusal. Kept homes will accumulate one per offending session until the durable change lands; that is visible, bounded by user deletion, and strictly better than a lockout.

**Durable change.**

1. Write a launch manifest at home build time (rendered paths, link names and targets, sidecars), stored outside the declared directory.
2. Classify at reap and close time against that manifest, never the current selection; the current packs are consulted only to perform capture.
3. Implement repatriation ([§3](#3-q2-what-a-stray-is-and-its-fate)): adopt absent names into the real directory, keep aside only true collisions.
4. Make `yolo prune` list kept homes with their resolutions, so a kept home is a queue the user can drain rather than archaeology.
5. State [§6](#6-q5-the-missing-requirement)'s ownership rule in the design document, and fix the jail warning: for a replaced link the working advice is repatriation or keep-aside, not "declare it shared".

**Evidence that would settle the open questions** (all UNMEASURED today):

| Question | Measurement | Where |
|---|---|---|
| *How* does `pi-web-access` defeat the link — rename, unlink+mkdir, other? | File-operation trace of one extension write against a linked name | Maintainer's jail, podman backend |
| Does any program rename over a rendered **surface**, or over the `auth.json` link? (Would reopen [§4](#4-q3-is-the-symlink-farm-the-right-mechanism) toward mounts) | Audit kept/session homes across backends for replaced surface paths and replaced credential links | Jail session homes under `~/.pi/.yolo-sessions/` and host `<state>/host-session-homes*/`, over normal use |
| How common are strays in jails vs. host, per backend? | Count and classify kept homes per [§3](#3-q2-what-a-stray-is-and-its-fate)'s table over a fixed window | podman, Apple Container, macos-user, host notch |
| Is per-file bind-mounting (option b) viable where namespaces exist? | Rename-over-mounted-file behavior, two concurrent profile views, capture diffing | Container backends only; explicitly not a parity candidate |

## 8. Measured: the three homes the lockout left behind

The maintainer preserved three ended homes from the 2026-10-27 lockout (`pi-870433002` 19:38,
`pi-3571155922` 21:05, `pi-195229834` 21:30). They are byte-identical in every file: the seven
rendered files (`settings.json` 9332 B, `models.json`, `mcp.json`, `yolo-model-lists.json`,
`yolo-openai-codex-models.json`, and the two extension configs), all fifteen capture sidecars
(baselines, overlays, selection, list-capture, list-record, provenance), and one stray each —
`pi-web-access/last-seen-version.json`, 20 bytes, `{"version":"0.45.0"}`, the same bytes in all
three.

MEASURED, and it settles the severity question in [§2](#2-q1-the-reapers-requirement): three
launches were refused over a constant no surface ever claimed, and no byte of user config differed
between the three sessions, so nothing the check exists to protect was ever at risk. It also
classifies what a launch manifest would have to record: the only unrendered entry across three
sessions was one top-level directory the program created, which is [§3](#3-q2-what-a-stray-is-and-its-fate)'s
first row (a name absent from the real directory), not a collision.

## 9. Decision ledger

| ID | Decision | Reason |
|---|---|---|
| SHR-D1 | The reaper's requirement is: no byte deleted unless regenerable, a link, yolo's own, captured, or preserved ([§2](#2-q1-the-reapers-requirement)) | It is a property about destruction that can be checked per entry |
| SHR-D2 | The current-selection inventory check is removed as a deletion gate | It enforces a closed world the design never promised (SP-D3) and judges old homes against new packs |
| SHR-D3 | Classification at reap/close time is against the originating launch (manifest; baselines until then), not the current packs | Accountability attaches to the launch that built the home |
| SHR-D4 | A stray's fate is repatriation: adopt if the name is absent from the real directory, keep aside on collision; never silently delete | The bytes are program-owned and addressed to the real directory; divergent merges are the user's call |
| SHR-D5 | One stray rule on every path: jail boot, host resident `Close`, host reap | Parity; the silent host delete is the only data-loss path and ends first |
| SHR-D6 | Retain the symlink farm; treat link replacement as normal program behavior, repaired by SHR-D4 | It is the only mechanism available on all backends and the host notch; profile isolation survives replacement |
| SHR-D7 | Revisit bind-mount views only on measured evidence of harm repatriation cannot repair ([§7](#7-q6-smallest-change-durable-change-evidence)) | Mounts cannot serve the host notch, so they cannot be the parity mechanism |
| SHR-D8 | Janitorial reaping during a launch never refuses the launch; it keeps aside and warns once. Launch refusal is reserved for the new launch's own build/render failures (SP-D6, SP-D26) | Retention protects the same bytes without denying service; a stop must name a step that works |
| SHR-D9 | `yolo prune --apply` reports kept homes and may exit non-zero; kept-home messages name the kept path and a working resolution | The user who asked for the sweep gets the report; the user who asked for a session gets the session |
| SHR-D10 | Capture's remit is surfaces only; preservation of unknowns is a separate, negative duty of the reaper | Names the missing requirement ([§6](#6-q5-the-missing-requirement)) so the inventory check has no reason to return |
| SHR-D11 | Ship the staged fix amended per [§7](#7-q6-smallest-change-durable-change-evidence) now; stage manifest + repatriation as the durable change | The lockout is total and self-renewing; the amendment closes the silent delete in the same pass |

## 10. Open questions

| ID | Question | Settled by |
|---|---|---|
| SHR-Q1 | Does a complete launch manifest already exist in some form beyond baseline sidecars? | **Settled: no.** The sidecars are per-surface, keyed by `agent-name` (`last_render`, `selection`, `overlay`, `list-capture`, `list-record`, `provenance`); none records a rendered path, a link name or a link target, so SHR-D3 needs a new record rather than a reader |
| SHR-Q2 | Do any packs' programs replace links to *files* (not directories) holding credentials or session state? (UNMEASURED) | The audit in [§7](#7-q6-smallest-change-durable-change-evidence), row 2 |
| SHR-Q3 | For a kept-aside collision, should `yolo prune` grow an explicit adopt/discard verb, or is naming the path enough? | Maintainer preference after living with kept-home volume from SHR-Q2's window |
| SHR-Q4 | Should adoption (SHR-D4) apply recursively inside rendered parent trees, or is keep-aside right for all non-top-level strays? | SHR-Q2's classification data; keep-aside is the safe default until then |