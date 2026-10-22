---
status: accepted
stage: DECIDED
next: "Build the plan's slices in order; the account-home hold goes only after the native concurrent-workspace verification passes"
tags: [macos-user, workspaces, concurrency, seatbelt, home]
vantage:
  status-chip: true
---

# Two native workspaces use private homes under one guest UID

**Status:** 2026-10-22. All owner rulings are recorded; the architecture and threat boundary below are accepted. Nothing is built, and native enforcement is unmeasured; the current cross-workspace account-home hold remains in place. The companion plan is the build handoff.

> **In short.** Different workspaces run concurrently under one `_yolojail` guest OS user, each with a private `HOME` and persistent workspace state. Seatbelt preserves only explicitly selected shared machine paths; it does not grant access to sibling workspace homes.

**Why it matters.** The current global-home link layout cannot safely serve two workspaces at once: starting one can change paths used by the other. Independent workspace homes let both keep stable path views without adding OS accounts.

**The shape.** One guest UID, distinct private workspace homes and state, and Seatbelt access to the selected workspace plus existing, explicitly shared machine paths.

**Cost.** The current HOME layout and hold remain until a replacement is designed, implemented, and verified on macOS. Exact physical paths and shared-link geometry are not selected here.

**Start at [the selected boundary](#selected-architecture-and-boundary).**

**Needs your ruling:** None.

**Reads with:** [the home tiers](../reference/macos-user-home-tiers.md#decision-ledger) (selected architecture and current implementation), [native privilege lifecycle](macos-user-privilege-lifecycle.md#points-settled-in-the-walkthrough-2026-10-15) (one enrolled user and one run-as account), [native direction](../reference/macos-no-vm-direction.md) (native plus container fallback), [the implementation plan](macos-user-concurrent-workspaces-plan.md) (the build handoff: layout, slices and native verification steps).

---

## Selected architecture and boundary

Each project has its own private `HOME` and persistent workspace state under the single `_yolojail` guest OS user. A workspace's home and state do not change when another workspace starts; workspace state is not tied to an individual Pi session. Seatbelt must allow the selected workspace and explicitly selected shared machine paths while the kernel denies access to sibling workspace homes and state, including physical access through symlinks or `..`. The target access outcomes match the container backends: private workspace state and only the already-selected shared machine scope.

Sharing remains constrained by selected pack declarations and existing user configuration. In particular, pack-declared machine-scope state, existing cache/mise/Nix paths, and configured grants keep their established policy; no new credential source, provider policy, MCP permission, host-file grant, or wider credential access is implied. Relative pack-hook links still need a deliberate shared backing under the new path layout, but the architecture does not choose exact backing paths or link geometry.

The trusted launcher enters Seatbelt confinement before running the workspace command. That voluntary privilege drop cannot be reversed from inside the confined process, and descendants inherit it. The kernel must deny sibling-home/state access to those confined processes. The host user and administrator retain control of the guest account; unrelated host-controlled, unconfined processes under that same UID are trusted and outside this claim. This is not isolation from arbitrary unconfined same-UID processes. No new privilege or credential grant follows from [OQ-MCW2](#OQ-MCW2).

The architecture does not use accounts-per-workspace and does not make waiting for or refusing a second workspace the product behavior.

## Current implementation and transition

The shipped implementation still sets `HOME` to `/Users/_yolojail` and uses a single account-home layout with per-workspace sidecars. The host-user account-home hold in [HT-D15](../reference/macos-user-home-tiers.md#ht-d15) still refuses a different workspace while a session holds that home, before the nix build and sudo. It is protection for this current mutable layout, not a final concurrency choice. Keep it until the replacement path and kernel behavior are implemented and verified.

The historical [OQ-HT4 record](../reference/macos-user-home-tiers.md#oq-ht4) describes why the current layout was selected at the time; it is not a direct owner prohibition on per-workspace homes. The settled concurrency architecture is recorded under [OQ-HT6](../reference/macos-user-home-tiers.md#decision-ledger).

The existing same-workspace admission and staging serialization remain in force. The [per-session profile design](per-session-profiles.md) covers profile-dependent agent configuration for concurrent sessions of one workspace; that work is distinct and does not implement multi-workspace homes. A native profile lane needs its own session-scoped environment and render, not a rewrite of another session's files.

## Remaining design and verification work

The exact physical home/state layout and the treatment of every reader of `HOME` and its relatives (passwd/home lookups, login rc files, root-level `host_files`, install prefixes, mise/cache paths, relative pack hooks) are engineering decisions recorded in [the implementation plan](macos-user-concurrent-workspaces-plan.md#implementation-decisions). They preserve the selected shared-path policy rather than turn one broad home ancestor into a Seatbelt re-allow.

The project home and its ordinary state persist across Pi sessions, retained by default like a container workspace's `.yolo` state. Session exit or workspace removal does not authorize deletion. There is no automatic deletion, expiry or size cap. Any future explicit cleanup requires separate authorization and must refuse deletion while state is live or liveness is unknown; this design adds no cleanup operation. Exact migration mechanics remain engineering work under that retention rule. See [OQ-MCW3](#OQ-MCW3).

Native Seatbelt behavior is unproven by rendered-profile tests or Linux fixtures. Before the account-home hold is removed, prove on disposable Mac state that two simultaneous workspaces retain their own HOME/state view and the kernel denies sibling-home/state access while allowing only selected machine paths; the plan stages a kernel feasibility run before the bootstrap work and gates the HOME switch and the hold removal on native runs. The boundary is settled; its native enforcement remains unmeasured. Native hardware trials needing guest/root/account setup or credential access require separate authorization; none is granted here.

## Decision ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| OQ-MCW4 | Sharing remains limited to selected pack machine scope and existing configured shared paths. The exact backing and relative-link geometry are engineering design, not a new credential grant. | 2026-10-20 | [Selected architecture and boundary](#selected-architecture-and-boundary) | — |
| <a id="OQ-MCW2"></a>[OQ-MCW2](#OQ-MCW2) | Protect Seatbelt-confined processes and their descendants against sibling HOME/state access; confinement is irreversible from inside. Host/admin control and trusted unconfined same-UID processes are outside the claim. | 2026-10-22 | [Selected architecture and boundary](#selected-architecture-and-boundary) | — |
| <a id="OQ-MCW3"></a>[OQ-MCW3](#OQ-MCW3) | Retain persistent workspace HOME/state by default, like container workspace state. No automatic deletion, expiry or cap; any future separately authorized explicit cleanup must refuse live or unknown state. | 2026-10-22 | [Remaining design and verification work](#remaining-design-and-verification-work) | — |
