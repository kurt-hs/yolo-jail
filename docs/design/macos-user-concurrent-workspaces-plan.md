---
status: draft
stage: SKETCH
next: "Complete the finer workspace-home/shared-path design and native Seatbelt/lifetime proof, then reconsider promotion from the then-current tree; no build is authorized"
depends-on:
  - ../reference/macos-user-home-tiers.md#OQ-HT6
  - macos-user-concurrent-workspaces.md#OQ-MCW2
  - macos-user-concurrent-workspaces.md#OQ-MCW3
  - macos-user-concurrent-workspaces.md#OQ-MCW4
tags: [macos-user, workspaces, sketch]
vantage:
  status-chip: true
---

# Concurrent native workspaces — incomplete implementation sketch

**Status:** 2026-10-20 — the architecture is selected, but this remains a sketch, not a build handoff or implementation authorization. Its seam list reflects the earlier investigation and must be checked against the tree when a handoff is authorized. The design doc wins on behavior; the then-current tree wins on facts; this sketch is the first thing to correct when either moves.

**Design:** [two private workspace homes under one guest UID](macos-user-concurrent-workspaces.md#selected-architecture-and-boundary). The selected [home-tier ruling](../reference/macos-user-home-tiers.md#decision-ledger) settles concurrent workspaces; this sketch still needs the finer physical layout, reader inventory, relative shared-link geometry, lifecycle details, and Mac-native proof before it can become a build handoff. No code change or trial is authorized here.

## Historically identified seams — re-verify before handoff

| Seam | What a future planner must verify |
| :--- | :--- |
| [`HoldAccountHome`](../../internal/cli/run/accounthomehold.go), `PreflightLaunch` in `internal/macosuser` | The current cross-workspace hold is a protection for the global HOME implementation. Preserve it until the replacement path is implemented and kernel-verified; unknown remains live under current behavior. |
| `RunMacosUser` in `internal/macosuser`, `AcquireWorkspaceLockFor` in `internal/cli/run` | Same-workspace staging serialization covers staging through bootstrap/provisioning; verify its scope against the selected multi-workspace behavior. |
| `DeriveDarwinHomeLayout`, `InstallHomeOverlay`, `WorkspaceHomeState` | The current symlinks, sidecar, and shared-dir mirrors form one layout. Re-read their callers and account for every HOME-relative reader before replacing it. |
| `BuildRunPlanWithStages`, `SeatbeltProfile`, `LaunchArgvWithGuard` | The HOME environment, profile's physical paths, and bootstrap paths must agree. Native kernel denial of sibling homes/state is not established by source or Linux tests. |
| `SessionKey`, guest-tree claims, host service sessions | Existing session-owned files/services are separate from persistent workspace home/state. A tree claim is not a workspace-home liveness proof. |
| `SeatbeltCaptureProfile`, `hostNixReal`, run-as rule | Capture and nix retain their existing boundaries. The selected design does not authorize new root, account setup, sudo grants, or credential access. |

## Before any build handoff

1. Re-read the tree and define the concrete per-workspace HOME/state paths and all readers, including passwd/home lookup, shell startup files, `host_files`, program prefixes, mise/cache, and relative pack-hook paths. Do not treat the illustrative paths in older discussion as an approved schema.
2. Define the physical shared backing for selected machine paths and relative links without broadening pack scope, user-configured grants, or credential/provider policy. Preserve private state and unknown/live bytes; do not invent migration, cleanup, TTL, or capacity policy.
3. Obtain authorized Mac-native evidence for concurrent workspace views and kernel denial of sibling home/state paths, with positive cases for only the selected machine paths. Rendered Seatbelt text and Linux tests are insufficient. Account/root setup, credentials, and other separately privileged trials are not authorized by this sketch.
4. Only after the finer design and evidence requirements are settled, promote this sketch against the then-current tree with a real file map, tests-first plan, stop conditions, and exact gates. Until then, leave the current account-home hold in place and make no code changes.
