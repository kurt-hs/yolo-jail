---
status: in-review
stage: DESIGN
next: "Owner discussion: rule OQ-HT6 in the home-tier reference and OQ-MCW2 through OQ-MCW4 here; do not build before 0.15 releases"
tags: [macos-user, workspaces, concurrency, seatbelt, home]
vantage:
  status-chip: true
---

# Two native workspaces need two stable home views, not a faster link switch

**Status:** 2026-10-19. Design only, against `23eb5097`; no implementation authorization and no simultaneous-project Mac measurement. The existing single-workspace refusal remains in force. Build discussion belongs **after 0.15 releases**.

> **In short.** The native environment yolo manages must give each running project an unchanging view of its own configuration, packages and state while preserving only deliberately machine-scoped sharing. An account-home link set that changes when another project starts cannot provide that view.

**Why it matters.** Today another project's launch must refuse before its nix build: otherwise it repoints the first session's `.config`, installed-program paths and pack state while that session still runs.

**The shape.** Keep an immutable workspace-scoped path view for each active session and explicitly address the machine tier; choose its home/account mechanism only after the access and lifecycle measurements below.

**Cost.** A per-home scheme reopens the ruled constant `HOME`; a per-account scheme adds administrator provisioning and changes sharing. Neither is a one-line relaxation of the hold.

**Start at [the boundary](#the-boundary-that-must-hold).**

**Needs your ruling:** [OQ-MCW2](#OQ-MCW2), [OQ-MCW3](#OQ-MCW3), [OQ-MCW4](#OQ-MCW4).

**Prerequisite owner question:** [OQ-HT6](../reference/macos-user-home-tiers.md#oq-ht6) is the existing concurrent-workspace choice.

**Reads with:** [the home tiers](../reference/macos-user-home-tiers.md#concurrent-workspace-choice-background-and-full-options) (prior ruling and still-open [OQ-HT6](../reference/macos-user-home-tiers.md#oq-ht6)), [native privilege lifecycle](macos-user-privilege-lifecycle.md#points-settled-in-the-walkthrough-2026-10-15) (one enrolled user and one run-as account), [native direction](../reference/macos-no-vm-direction.md) (native plus container fallback), [the implementation sketch](macos-user-concurrent-workspaces-plan.md) (not a build handoff).

---

## Existing contract, not a fresh decision

The [home-tier ruling](../reference/macos-user-home-tiers.md#why-it-is-this-way) keeps `HOME=/Users/_yolojail`: `scope: workspace` pack state and `.config`, `.local`, `.npm-global`, `go`, `.yolo/bin` point into `<workspace>/.yolo/home`; `scope: machine` state, `~/.cache` and `~/.yolo/mise` stay in the account home, mirrored into sidecars for relative pack credential links. [OQ-HT4](../reference/macos-user-home-tiers.md#oq-ht4) specifically declined a per-workspace `HOME` to avoid a different shared-credential mechanism on native. [OQ-HT3](../reference/macos-user-home-tiers.md#oq-ht3) admits simultaneous terminals **of one workspace**; it did not authorize concurrent *different* workspaces. [HT-D15](../reference/macos-user-home-tiers.md#ht-d15) implements the refusal through a host-user flock held for the session, before build and sudo, with unknown counted live; [OQ-HT6](../reference/macos-user-home-tiers.md#oq-ht6) remains open, with a leaning to refuse until needed, then consider a workspace home. This new request supplies that need, **not** a ruling overturning the earlier answer.

The [privilege ruling](macos-user-privilege-lifecycle.md#points-settled-in-the-walkthrough-2026-10-15) installed one narrow passwordless run-as grant for `_yolojail`, not a general root capability, a second account, or an admin-free account factory. The host-owned staging root, session-specific env/profile/CA files and account switch must remain narrow; environment secrets are in protected files, not argv. The [native process](../reference/macos-user-nix-and-features.md) uses a Seatbelt profile with a workspace and home write allow and a `/Users` read deny with selected re-allows; `sandbox-exec` evaluates resolved physical paths. The sandbox UID still owns the shared account home, so Seatbelt and DAC both matter.

The [per-session profile design](per-session-profiles.md) addresses profile-dependent *agent* config for concurrent entries of a jail; it does not solve the native account-home link switch. Its native Pi session-home work is independently shippable for two sessions of the **same** workspace under the existing guard, so multi-project architecture must not block that lane. A native profile lane needs its own session-scoped environment and render, not a rewrite of another session's files. No Pi core, SDK or extension feature is required as a prerequisite.

## The boundary that must hold

- **Workspace bytes:** each project sees its own declared workspace tier, home-root redirects, host-files copies, generated settings, pack hooks, program install prefix, briefings and agent history. Starting B never rewrites a pathname A may resolve later. A's process and its descendants keep A's view after B starts and after B exits. Nothing is implicitly shared because both run under one UID.
- **Machine bytes:** only pack-declared `scope: machine` dirs, the existing shared cache/mise paths and expressly configured user-scope grants remain shared. Sharing is a policy decision already made in the relevant declarations, **not** a grant to read another workspace's sidecar. A machine-tier path must not become A's writable route into B's workspace via a mirror, redirect or hook. Cache relocation targets remain subject to the user's explicit config and Seatbelt grants; a shared cache is not an independent security boundary between mutually hostile same-UID sessions.
- **Host boundary:** no new ability to traverse the invoking user's home, other users' homes, other projects or credential sources. A user-supplied context mount and `env_sources` retain their own declared authority; a per-workspace home is not permission to broaden either. The `host_files` case of a new top-level home dir and the shared login rc files must be accounted for, not waved away as already private ([home-tier gaps](../reference/macos-user-home-tiers.md#what-is-still-shared-and-what-that-costs)).
- **Path reality:** the profile must permit the selected physical workspace/home and deny other physical workspace homes and sidecars, including access through symlinks, `..`, a tool's passwd-db home lookup and `TMPDIR`/`/private/tmp`. A test of rendered SBPL text alone does not prove the kernel verdict. Profile policy is path-based and the same UID can access its own directories outside that profile during yolo's bootstrap; a confined process's escape via a writable alias or its surviving child is the relevant test.
- **No half-admission:** until these invariants are demonstrated on Mac hardware and the owner rules the open choices, leave the [account-home hold](../reference/macos-user-home-tiers.md#ht-d15) in place. A second project must still get its present actionable refusal, not a silent queue, guessed home path or blanket `sudo` workaround.

## Two plausible shapes, neither selected

| Shape | Advantage | Unresolved cost |
| :--- | :--- | :--- |
| **Several homes, one `_yolojail` UID**: distinct persistent home root per workspace, with a deliberately shared machine-tier backing and per-session path/environment | Preserves current narrow sudo grant and per-project sidecar layout; avoids a link set that B repoints under A | Explicitly reopens [OQ-HT4](../reference/macos-user-home-tiers.md#oq-ht4). The UID can read/write both homes outside a correctly scoped Seatbelt profile; profile must exclude sibling homes and sidecars at their physical paths. Pack shared-directory relative links need a shared backing and verified mirrors. `$HOME`, passwd home, rc files, cache/mise, captured installers and root-level host files must agree without recopying secrets or widening host paths. |
| **Workspace-specific accounts**: each UID owns a distinct home | DAC provides stronger cross-project separation even outside Seatbelt | Setup/teardown, UID reservation, ACLs for each project, one-user sudo rule, credential sharing, machine caches and nix-daemon access all change. It cannot be inferred that the existing narrow run-as grant permits these switches or that administrative setup can be automated. Migration, account exhaustion and durable identity are owner policy, not implementation details. |

A per-process fake `HOME` within one mutable account home without moving *all* readers is not a third proven design: tools may consult `getpwuid`, `~`, absolute paths or relative pack links. Waiting for A to quit preserves safety but does **not** meet two projects at once. More sessions in the same project share that project's state by existing ruling, but profile A/B must remain session-scoped rather than last-writer-wins.

## Coordination and lifetime to settle before construction

The current [hold](../../internal/cli/run/accounthomehold.go) serializes claims under `.mutex`, keeps one shared lock per workspace and refuses another workspace; the [launch lock](../../internal/macosuser/orchestrator.go) covers sidecar staging, bootstrap and provisioning, then releases before the agent. Preserve that same-project admission and its staging serialization, while ensuring B's entry never edits A's home-root links, machine-tier aliases, credential files or profile lane. Per-session env/profile paths already have random session IDs and liveness records; a session's teardown removes only its own files and services. If a child survives its terminal/launcher, the home, pack tree and any link it needs must remain usable. A SIGKILL can drop the host hold while a sandbox child survives today; that known weakness cannot become the argument for reusing or deleting its home. An unknown live process or unreadable record retains bytes. Reboot recovery must recognize dead holders without mistaking a stale lock file for a live session; workspace removal, account deletion and GC must not erase another project's machine-tier or active data. `yolo stop` currently has no native jail to stop; any changed stop semantics require a separate ruling.

Install capture still uses a throwaway root with its own profile that explicitly denies reads of the shared sandbox home ([capture profile](../../internal/macosuser/seatbeltcapture.go)); it must not record or modify another project's home, machine credentials, or another capture's staging tree. The shared capture store is a content-addressed artifact store, not a license to share writable home state; its writer/claim and holder-lifetime proof are distinct from per-project home cleanup ([guest tree retention](../reference/macos-user-provisioning.md#the-guest-pack-tree--one-per-launch-never-replaced)). Machine-wide tool caches and a configured cache relocation likewise need intentional sharing and non-repointed targets, not a new per-project schema to unlock this feature.

## Proof before any build handoff

The Mac-agent instrument may use **only** the already-supported narrow, no-password jail entry, in two disposable projects under the neutral root. No interactive agent/API call, credential read, root request or account installation. First record the control: project A runs a noninteractive jail shell writing sentinels under workspace state, generated config, `$HOME`'s private surfaces and a child that waits; starting B is refused, A's child still sees A's sentinels. Then, once a design is built and separately authorized, keep both sessions live simultaneously; B writes *different* sentinels, changes profile lane without rewriting A, and A's child resolves each path again after B starts and after B quits. Reverse the order; run two terminals of A with distinct profiles and make a child survive one terminal's end. Inspect physical `realpath`, env, passwd-home view and Seatbelt allow/deny of A/B homes, sidecars, aliases, `/private/tmp`, `host_files` destinations, machine mirrors and configured cache targets. Avoid real credential contents: use synthetic pack fixtures and only absence/presence probes. Verify crash/unknown/reboot collection separately on disposable state, with no live work destroyed. Record commands, versions, profile paths and exit codes. A Linux fixture proves the call site and fallback, not the Mac kernel or a surviving daemon. **Measurements needing root** (creating accounts, changing sudoers/ACLs, inspecting protected account metadata) are separate, ungranted work: owner authorization and a managed Mac test are prerequisites, not presumed consequences of narrow jail entry.

## Open questions — owner decisions, not recommendations

The existing [OQ-HT6](../reference/macos-user-home-tiers.md#oq-ht6) is the **sole** authority for whether concurrent native projects are supported and which home/account alternative is selected. Its [OQ-HT4](../reference/macos-user-home-tiers.md#oq-ht4) constraint cannot be silently superseded: selecting another `HOME` requires an explicit owner amendment there. The following questions address separate boundary and lifecycle policies if concurrent use is selected; none answers the older question.

- 💬 **OQ-MCW2: What is the accepted isolation boundary between projects of the same sandbox UID?**

  <!-- vantage: question id=OQ-MCW2 leaning="Keep the current host boundary and require kernel-tested sibling-home denial; decide whether same-UID access outside Seatbelt is acceptable." -->

  _Leaning:_ Keep the current host boundary and require kernel-tested sibling-home denial; decide whether same-UID access outside Seatbelt is acceptable. Distinguish accidental config/auth/package leakage from isolation against hostile same-UID guest processes.

  **Answer:**
  > _(empty — owner discussion after 0.15)_

- 💬 **OQ-MCW3: Which state persists when a project is removed, a launcher dies or the Mac reboots?**

  <!-- vantage: question id=OQ-MCW3 leaning="Retain unknown or still-held homes and machine state; specify an explicit reclamation policy before deletion." -->

  _Leaning:_ Retain unknown or still-held homes and machine state; specify an explicit reclamation policy before deletion. This includes old constant-home bytes and whether a native stop operation is wanted.

  **Answer:**
  > _(empty — owner discussion after 0.15)_

- 💬 **OQ-MCW4: Which shared machine resources remain one copy under the selected shape?**

  <!-- vantage: question id=OQ-MCW4 leaning="Preserve pack-declared machine scope and existing user-configured cache grants; rule per-account sharing, if that shape wins, explicitly." -->

  _Leaning:_ Preserve pack-declared machine scope and existing user-configured cache grants; rule per-account sharing, if that shape wins, explicitly. Include nix client, shared credential hook, install capture and guest pack-tree lifetime; do not invent a schema prerequisite.

  **Answer:**
  > _(empty — owner discussion after 0.15)_
