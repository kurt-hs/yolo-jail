---
title: "Native per-session Pi profile homes: implementation sketch"
date: 2026-10-19
status: draft
stage: SKETCH
next: "After owner discussion and 0.15, reconcile native identity/lease behavior with the design and re-read the tree before promoting this sketch"
depends-on: [macos-user-session-profiles.md]
tags: [plan, macos-user, pi, sessions]
---

# Native per-session Pi profile homes: implementation sketch

**Status:** 2026-10-19 — incomplete and unstable pending owner discussion and the 0.15 release. Written against `23eb5097b`. **Not build-authorized.** No task here is a cold-implementer handoff.

**Design:** [the native proposal](macos-user-session-profiles.md). It wins on behavior; the current tree wins on facts; this sketch is advice and the first thing to become stale. Stop and return to the owner if a behavior decision changes, a named seam disappears or the cross-workspace lane changes the account/state topology. Do not twist production code to match an old sketch.

## Present seams, not completed work

| Seam | Fact to recheck before promotion |
| :--- | :--- |
| [`internal/macosuser/sessionfiles.go`](../../internal/macosuser/sessionfiles.go) | `newSessionID` is 16 hex; `parseSessionKey` recognizes that width; `openSessionRecord` and `sweepGoneSessions` manage host-owned per-session staging and flock. Entrypoint's `validSessionID` accepts this id already. |
| [`internal/macosuser/orchestrator.go`](../../internal/macosuser/orchestrator.go) | `RunMacosUser` mints once after dry-run; opens a record, sweeps, installs the env/profile, bootstraps, then launches; deferred teardown handles staging. Native provisioning is between bootstrap and launch. |
| [`internal/macosuser/runplan.go`](../../internal/macosuser/runplan.go) | `RunPlan.SessionID`, `BuildRunPlanWithStages`, `buildBootstrapEnv`, per-launch `SandboxEnvFileContent`, `BootstrapArgv` and `LaunchArgv` are the plan's readback points. `CapturePlan` also uses `buildBootstrapEnv` for a *throwaway* home: do not turn an install capture into an ordinary Pi session. |
| [`internal/entrypoint/darwin.go`](../../internal/entrypoint/darwin.go), [`bootsteps.go`](../../internal/entrypoint/bootsteps.go) | `RunDarwinBootstrap` sets target `bootDarwin` but not `sessionPass`; `prepare_session_homes` explicitly excludes Darwin; Darwin `configure_pack_surfaces` bypasses `beginSessionCapture`. Hydration of per-launch env happens before rendering. |
| [`internal/entrypoint/sessionhome.go`](../../internal/entrypoint/sessionhome.go), [`sessioncapture.go`](../../internal/entrypoint/sessioncapture.go), [`internal/sessionhome/`](../../internal/sessionhome/) | Already resolve generic pack declarations, build/link, redirect render and sidecars, capture through existing overlay and preserve strays. Reuse, rather than inventing a native Pi schema. `beginSessionCapture` currently decides container pid1 retirement via `JailMainEnv`: native lifetime needs its own positive-gone rule. |
| [`packs/pi/pack.json`](../../packs/pi/pack.json), [the settled plan](per-session-profiles-plan.md) | Pi already declares `session_homes`; its extensions already resolve list files under the relocation. Shared names and no linked `*.lock` are existing contracts. |
| [`internal/macosuser/seatbelt.go`](../../internal/macosuser/seatbelt.go), [`internal/entrypoint/darwinhomelayout.go`](../../internal/entrypoint/darwinhomelayout.go) | Account home and workspace are the allowed writable roots; resolve physical links and check whether the native home/sidecar paths are actually under those roots. Do not assume a sandbox process can delete host-owned staging files. |

## Proposed build slices, only after authorization

1. **Identity contract and plan proof.** Carry the *existing launch's* 16-hex id to bootstrap and child env by separate trusted-plan and per-launch-file paths; keep the native id/record format unchanged. Assert in plan tests that dry-run creates none, capture/sealed build gets none, a user env source cannot replace the trusted bootstrap id, and no plan can launch a Pi session with a different id from its record. An id alone does not authorize a `sessionPass`: only an ordinary native launch does.
2. **Native render.** Mark ordinary Darwin boot as a session pass, enable `prepare_session_homes`, and reuse the existing redirect/baseline builder. Ensure the bootstrap's `e.Vars` and process env consistently carry the id and relocation to the provisioning and command stages, without forwarding bootstrap-only secrets. Assert a failed build refuses before shared Pi config is touched, and a user-declared `PI_CODING_AGENT_DIR` wins with a disclosure. Keep install capture's throwaway/bootstrap and sealed builds excluded.
3. **Capture and lifetime.** Workspace-qualify native home enumeration, use the session-record lease to exclude active/unknown sessions, serialize capture/render against the workspace's shared sidecars, and keep failed/stray sources. Schedule account-scoped deletion only after capture and positive gone. Reconcile with any future multi-workspace account arrangement rather than making that arrangement here. An exit with a live detached child must not trigger deletion.
4. **Gates and docs when built.** Native plan-invariant/unit tests, a real-Darwin bootstrap fixture, a Seatbelt runtime test for the physical symlinked paths, `GOOS=darwin` cross-build, `just test-fast`, and a nested launch only where its platform shape is relevant. Manually perform the Mac agent measurements named in [the design](macos-user-session-profiles.md#4-proof-and-limits), then update the owning reference/design status from observed evidence. Do not run agents in automated tests or treat Linux-green as a Mac Seatbelt result.

**Pre-promotion traps:** native's 16-hex id is valid for entrypoint's 8–64-hex `validSessionID`, but the native record key requires exactly 16 hex: keep the same id in both places instead of minting another or changing the record grammar. `sessionhome.Home` currently derives a path from the shared directory plus id, and `sessioncapture.findSessionHomes` enumerates that parent, so a workspace-scoped namespace must reach **both** producers and consumers. `beginSessionCapture` currently captures other homes before render; on native macOS an active foreground session can still write while the next bootstrap runs, so lock + parse-skip do not prove quiescence. Check the exact descendant liveness semantics on a Mac before committing cleanup behavior. A test that calls only `prepareSessionHomes` without the actual Darwin boot or plan caller gives a false green.

**Fences:** no Pi core, SDK or extension development; no new pack schema prerequisite; no host-management ownership change; no account architecture or broad root grant; no change to the host user's real Pi settings; no roadmap, changelog or shared reference edits in this design-only lane. Promote to a real task-by-task implementation plan only after owner discussion, post-0.15, with failing tests, exact commands, fences and fresh line citations.
