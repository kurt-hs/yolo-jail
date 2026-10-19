---
status: draft
stage: SKETCH
next: "After 0.15 releases and owner rulings, measure native paths, then promote against the then-current tree; no build authorized"
depends-on:
  - ../reference/macos-user-home-tiers.md#OQ-HT6
  - macos-user-concurrent-workspaces.md#OQ-MCW2
  - macos-user-concurrent-workspaces.md#OQ-MCW3
  - macos-user-concurrent-workspaces.md#OQ-MCW4
tags: [macos-user, workspaces, sketch]
vantage:
  status-chip: true
---

# Concurrent native workspaces — investigation and implementation sketch

**Status:** 2026-10-19 — incomplete, unstable while owner questions remain; written against `23eb5097`. **Not a build handoff, not build-authorized.** The design wins on behavior; the then-current tree wins on facts; this sketch is the first thing to correct when either moves.

**Design:** [two stable home views](macos-user-concurrent-workspaces.md). Blocked on the existing [home/concurrent-workspace ruling OQ-HT6](../reference/macos-user-home-tiers.md#oq-ht6), plus [same-UID boundary](macos-user-concurrent-workspaces.md#OQ-MCW2), [retention](macos-user-concurrent-workspaces.md#OQ-MCW3) and [shared resources](macos-user-concurrent-workspaces.md#OQ-MCW4). No second question duplicates the older ruling; no roadmap/reference edits belong to this candidate. Review/routing of new questions with the owner precedes promotion.

## Map of existing seams, not an edit list

| Seam | What to verify at promotion |
| :--- | :--- |
| [`HoldAccountHome`](../../internal/cli/run/accounthomehold.go), [`PreflightLaunch`](../../internal/macosuser/preconditions.go) | Control refusal is early, unknown-is-live; its host-user-only and SIGKILL limitations cannot be treated as lifecycle proof. Capture preflight uses this hold too. |
| [`RunMacosUser`](../../internal/macosuser/orchestrator.go), [`AcquireWorkspaceLockFor`](../../internal/cli/run/flock.go) | One same-workspace staging window across host-side content staging through bootstrap/provisioning; session itself does not hold workspace launch lock. |
| [`DeriveDarwinHomeLayout`](../../internal/entrypoint/darwinhomelayout.go), [`InstallHomeOverlay`](../../internal/entrypoint/darwin.go), [`WorkspaceHomeState`](../../internal/paths/paths.go) | Home symlinks, per-workspace sidecar and shared-dir mirrors are one linked system. Never repoint a live pathname or write generated files outside its chosen workspace. Inspect root-level host_files and rc paths as well. |
| [`BuildRunPlanWithStages`](../../internal/macosuser/runplan.go), [`SeatbeltProfile`](../../internal/macosuser/seatbelt.go), [`LaunchArgvWithGuard`](../../internal/macosuser/macosuser.go) | The profile and environment must name the same physical workspace/home; the launch does an inner `cd` into the workspace, while sudo starts in `/` for DAC. Passwd-home readers and surviving children need hardware checks. |
| [`SessionKey`](../../internal/macosuser/sessionfiles.go), [guest-tree claims](../../internal/macosuser/packtreereap.go), [host service sessions](../../internal/cli/run/servicessession.go) | Session env/profile/CA and services already have separate ownership. Prove child lifetime before home GC; tree claim alone is not a home-holder count. |
| [`SeatbeltCaptureProfile`](../../internal/macosuser/seatbeltcapture.go), [`hostNixReal`](../../internal/macosuser/hostnix.go), [run-as rule](../../internal/macosuser/privilege.go) | Capture's scratch/denied-home model, immutable nix-store client/daemon and one `_yolojail` run-as grant constrain either shape. Multiple accounts require a new owner-approved setup/privilege contract. |

## Order if the design is later authorized

1. **Measure before architecture.** With Mac-agent narrow passwordless guest entry only, run disposable noninteractive control probes under the current refusal: two neutral-root project dirs, synthetic sentinels and a waiting child. Record actual HOME, `getpwuid`, `realpath`, cwd and SBPL verdicts; record whether orphaned guest children survive the terminal and what guest-tree retention reports. No interactive agents, credentials, root or install. A denial is evidence of the existing guard, not evidence the new layout works.
2. **Owner review.** Rule the existing home-tier [OQ-HT6](../reference/macos-user-home-tiers.md#oq-ht6) and this design's distinct boundary/lifetime questions; explicitly amend [OQ-HT4](../reference/macos-user-home-tiers.md#oq-ht4) if a different `HOME` wins. Approve any policy change or root-requiring Mac experiment independently. A per-account plan may need a separately authorized account/setup design; do not silently rewrite the narrow sudo contract. No schema change is assumed. Native Pi session homes for two terminals of one workspace can proceed separately without changing the multi-project guard.
3. **Promote this sketch to a build handoff at a fresh commit.** Re-read each named seam and entrypoint call site, add a fenced file map, tests-first cases, migration, stop conditions and exact gate commands. Verify source proof for every proposed post-release state. Reconcile user guide and reference claims as part of the future implementation, not this doc-only lane.
4. **Build only then.** Keep the old refusal until the new home, profile, same-project and child-lifetime invariants pass. Cover release skew between host launcher and guest bootstrap; do not let an old host binary dispatch an unprotected layout.
5. **Exercise the real concurrent case.** Run A and B simultaneously with distinct synthetic settings, installed-program and config sentinels; re-read from A's surviving child after B starts/exits; reverse entry order, then test two same-project terminals on distinct profiles. Prove denies at physical A/B sidecar/home paths, synthetic shared-machine allowance, host boundary and capture exclusion with a bare control. Repeat after failure/forced exit and reboot only in isolated disposable state where owner approved any required privileges.

## Risk notes retained for handoff

- Home relocation can break `getpwuid` consumers, relative credential links, mise's global store, npm install prefix, shared cache relocations and login PATH: each needs a positive case and a negative cross-project case, not only a link-target assertion.
- The account home is writable by the guest and current Seatbelt profile re-allows its whole subtree. Merely giving B a second `$HOME` under that subtree without denying sibling paths leaves cross-project access. Include symlink swap and `..` probes against actual macOS Seatbelt; text-only unit tests do not prove the kernel judgment.
- Current same-project launches can write shared generated files; the per-session profile design is a distinct dependency for profile lanes. Check what is actually shipped on macos-user before promising profile isolation to a child that starts later.
- A real file at a link path is currently refused, not silently migrated. Do not apply the earlier no-migration answer to a newly selected persistent home without explicit owner ruling. A disposed project must never be mistaken for an unused home merely because its host launcher exited.
- On a shared UID, host-level and guest-level liveness are different: a SIGKILLed launcher can drop its host hold while the sandbox process outlives it. Unknown process info retains bytes and the launch must refuse rather than use a possibly live home.

**Gates for this sketch alone:** verify relative Markdown paths, frontmatter and question links; no Go or Mac command is a docs-only acceptance gate. Source and hardware gates must be written only after the owner rulings, when a plan can honestly predict their results.
