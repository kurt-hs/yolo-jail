---
status: in-review
stage: DESIGN
next: "Rule the narrowed same-UID threat and workspace-state lifetime questions, then complete shared-path design and native Seatbelt proof before any build handoff"
tags: [macos-user, workspaces, concurrency, seatbelt, home]
vantage:
  status-chip: true
---

# Two native workspaces use private homes under one guest UID

**Status:** 2026-10-20. The owner selected the architecture below. Implementation and native Mac verification remain incomplete; the current cross-workspace account-home hold remains in place. This is not a build handoff.

> **In short.** Different workspaces run concurrently under one `_yolojail` guest OS user, each with a private `HOME` and persistent workspace state. Seatbelt preserves only explicitly selected shared machine paths; it does not grant access to sibling workspace homes.

**Why it matters.** The current global-home link layout cannot safely serve two workspaces at once: starting one can change paths used by the other. Independent workspace homes let both keep stable path views without adding OS accounts.

**The shape.** One guest UID, distinct private workspace homes and state, and Seatbelt access to the selected workspace plus existing, explicitly shared machine paths.

**Cost.** The current HOME layout and hold remain until a replacement is designed, implemented, and verified on macOS. Exact physical paths and shared-link geometry are not selected here.

**Start at [the selected boundary](#selected-architecture-and-boundary).**

**Needs your ruling:** [OQ-MCW2](#OQ-MCW2) (same-UID threat boundary), [OQ-MCW3](#OQ-MCW3) (workspace-state reclamation).

**Reads with:** [the home tiers](../reference/macos-user-home-tiers.md#decision-ledger) (selected architecture and current implementation), [native privilege lifecycle](macos-user-privilege-lifecycle.md#points-settled-in-the-walkthrough-2026-10-15) (one enrolled user and one run-as account), [native direction](../reference/macos-no-vm-direction.md) (native plus container fallback), [the implementation sketch](macos-user-concurrent-workspaces-plan.md) (incomplete, not a build handoff).

---

## Selected architecture and boundary

Each project has its own private `HOME` and persistent workspace state under the single `_yolojail` guest OS user. A workspace's home and state do not change when another workspace starts; workspace state is not tied to an individual Pi session. Seatbelt must allow the selected workspace and explicitly selected shared machine paths while the kernel denies access to sibling workspace homes and state, including physical access through symlinks or `..`. The target access outcomes match the container backends: private workspace state and only the already-selected shared machine scope.

Sharing remains constrained by selected pack declarations and existing user configuration. In particular, pack-declared machine-scope state, existing cache/mise/Nix paths, and configured grants keep their established policy; no new credential source, provider policy, MCP permission, host-file grant, or wider credential access is implied. Relative pack-hook links still need a deliberate shared backing under the new path layout, but the architecture does not choose exact backing paths or link geometry.

The architecture does not use accounts-per-workspace and does not make waiting for or refusing a second workspace the product behavior. The security claim for a same-UID process running outside the applicable Seatbelt profile is not settled here: do not assume the selected layout isolates from it, weaken sibling-workspace denial, or add permissions to pursue a broader claim. See [OQ-MCW2](#OQ-MCW2).

## Current implementation and transition

The shipped implementation still sets `HOME` to `/Users/_yolojail` and uses a single account-home layout with per-workspace sidecars. The host-user account-home hold in [HT-D15](../reference/macos-user-home-tiers.md#ht-d15) still refuses a different workspace while a session holds that home, before the nix build and sudo. It is protection for this current mutable layout, not a final concurrency choice. Keep it until the replacement path and kernel behavior are implemented and verified.

The historical [OQ-HT4 record](../reference/macos-user-home-tiers.md#oq-ht4) describes why the current layout was selected at the time; it is not a direct owner prohibition on per-workspace homes. The settled concurrency architecture is recorded under [OQ-HT6](../reference/macos-user-home-tiers.md#decision-ledger).

The existing same-workspace admission and staging serialization remain in force. The [per-session profile design](per-session-profiles.md) covers profile-dependent agent configuration for concurrent sessions of one workspace; that work is distinct and does not implement multi-workspace homes. A native profile lane needs its own session-scoped environment and render, not a rewrite of another session's files.

## Remaining design and verification work

A build handoff still needs the exact physical home/state layout and an inventory of all readers of `HOME` and its relatives, including passwd/home lookups, login rc files, root-level `host_files`, install prefixes, mise/cache paths, and relative pack hooks. That work must preserve the selected shared-path policy rather than turn one broad home ancestor into a Seatbelt re-allow. The project home and its ordinary state persist across Pi sessions; existing private state and unknown/live bytes are retained safely. No automatic deletion, TTL, cap, or migration behavior is selected. What operation, if any, permits reclaiming a workspace home after workspace removal remains [OQ-MCW3](#OQ-MCW3).

Native Seatbelt behavior is unproven by rendered-profile tests or Linux fixtures. Before a build handoff, prove on disposable Mac state that two simultaneous workspaces retain their own HOME/state view and the kernel denies sibling-home/state access while allowing only selected machine paths. The unproven same-UID threat boundary remains [OQ-MCW2](#OQ-MCW2). Native hardware trials needing guest/root/account setup or credential access require separate authorization; none is granted here.

## Decision ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| OQ-MCW4 | Sharing remains limited to selected pack machine scope and existing configured shared paths. The exact backing and relative-link geometry are engineering design, not a new credential grant. | 2026-10-20 | [Selected architecture and boundary](#selected-architecture-and-boundary) | — |

## Open questions

1. 💬 **OQ-MCW2: Is protection against arbitrary same-UID processes outside a workspace's Seatbelt profile part of the security claim?**

   <!-- vantage: question id=OQ-MCW2 leaning="Limit the claim to sibling-workspace denial for processes confined by the applicable Seatbelt profile; do not imply outside-profile isolation or grant new permissions." -->

   The architecture already requires the kernel to deny sibling workspace HOME/state paths. The selected one-UID architecture does not establish isolation from an arbitrary same-UID process outside Seatbelt; no broader access or privilege follows from this question.

   _Leaning:_ Limit the claim to sibling-workspace denial for processes confined by the applicable Seatbelt profile. Do not imply outside-profile isolation or grant new permissions to pursue it.

   **Answer:**
   > _(empty — fill in when decided)_

2. 💬 **OQ-MCW3: What explicit event, if any, permits reclaiming persistent workspace HOME/state?**

   <!-- vantage: question id=OQ-MCW3 leaning="Retain workspace bytes by default, and never reclaim when any process or liveness state is unknown; do not invent automatic cleanup, a TTL or a cap." -->

   The HOME and ordinary workspace state persist across Pi sessions; existing private state and unknown/live bytes are retained safely. This question concerns only when, if ever, an explicit workspace-removal/reclaim operation may delete those persistent bytes.

   _Leaning:_ Retain workspace bytes by default, and never reclaim when any process or liveness state is unknown. Do not invent automatic cleanup, a TTL or a cap.

   **Answer:**
   > _(empty — fill in when decided)_
