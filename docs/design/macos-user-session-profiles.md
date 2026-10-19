---
title: "Native macOS sessions need private Pi config without private identity"
date: 2026-10-19
status: in-review
stage: DESIGN
next: "Discuss the native profile-home proposal and its cross-workspace boundary with the owner; do not build before 0.15"
tags: [design, macos-user, sessions, pi, profiles]
summary: "Proposed native macos-user adaptation of the existing session-home contract; design only, not build authorization."
---

# Native macOS sessions need private Pi config without private identity

**Status:** 2026-10-19, checked against `23eb5097b`. Not built on macos-user; no Mac runtime or agent/subagent measurement is claimed. This is a post-0.15 proposal, not a build handoff.

> **In short.** Each native launch must render Pi's profile into a directory that only that launch's process tree reads, while linking login, history and installations to the existing shared state. A second launch in the same workspace must not rewrite the first one's Pi config.

**Why it matters.** Container sessions and owned host commands already have private Pi directories. Native macos-user still runs the shared render for every bootstrap; its session id names staging files, not Pi's config.

**The shape.** Reuse the pack's session-home declaration and render/capture rules, give the native bootstrap a validated session identity, and tie native retirement to its existing session lease.

**Cost.** More small directories and bookkeeping per native session; native's existing id is already valid for session homes. No change to account ownership or the host user's native Pi settings.

**Start at [§2](#2-one-native-launch).**

**Needs your ruling:** None in this draft; all native-specific choices below are recommendations for owner discussion, not prior owner Answers.

**Reads with:** [the settled cross-backend design](per-session-profiles.md) (the owner Answers and shared session-home semantics), [its implementation plan](per-session-profiles-plan.md) (what actually landed on containers and owned host), [this implementation sketch](macos-user-session-profiles-plan.md) (source seams; not build-authorized).

---

## 1. Boundary and evidence

The owner already accepted per-session Pi directories, shared links, later-session capture of Pi edits, unchanged profile selection, and owned-host parity in [the settled design's decision ledger](per-session-profiles.md#11-decision-ledger). Those are **Answers**, not a new request to choose another Pi config model. This document **recommends** their native adaptation; it does not claim an owner ruling about native lease lifetime, ID width, workspace namespace or Mac behavior.

The current native code already mints **a session id**: `macosuser.newSessionID` produces 16 lowercase hex digits for `RunPlan.SessionID`, staging files and a liveness record ([sessionfiles.go](../../internal/macosuser/sessionfiles.go#L67-L105), [orchestrator.go](../../internal/macosuser/orchestrator.go#L910-L915)). The entrypoint's `validSessionID` accepts **8–64** lowercase hex digits, so the native id is already a valid session-home key ([sessionhangup.go](../../internal/entrypoint/sessionhangup.go#L87-L98)); the missing piece is carrying it into the ordinary bootstrap and command environment. `prepare_session_homes` is explicitly excluded on Darwin ([bootsteps.go](../../internal/entrypoint/bootsteps.go#L429-L444)). `RunDarwinBootstrap` does not mark its boot as a session pass ([darwin.go](../../internal/entrypoint/darwin.go#L98-L113)). Its pack render therefore writes the shared account home, and its Darwin path does not run the container session capture wrapper ([bootsteps.go](../../internal/entrypoint/bootsteps.go#L460-L480)). Calling the native id *missing* would obscure the actual integration gap.

The native bootstrap already hydrates the **per-launch** sandbox env file for generation ([bootsteps.go](../../internal/entrypoint/bootsteps.go#L164-L185)); the launched commands source that same file. The sandbox account home is a native writable root under Seatbelt, but host-owned staging and arbitrary host homes are not automatically writable ([seatbelt.go](../../internal/macosuser/seatbelt.go#L200-L230)). This design adds no privilege or Pi feature.

## 2. One native launch

1. The host uses the **existing** native 16-hex id for its record, bootstrap and process tree. The entrypoint already accepts it; no new id format, second unrelated Pi id or record migration is needed. Do not accept an env-supplied id from the user, or silently treat a malformed/missing id as permission to render shared Pi config. Dry-run's placeholder is not a real id and makes no home.
2. The host supplies that id in its trusted bootstrap contract **and** in the launched command's per-session env file. The bootstrap uses the trusted value to make a home; a user `env_sources` entry must not override it. The shell, Pi typed later in that shell, foreground children and detached children inherit exactly that id and the resulting `PI_CODING_AGENT_DIR`. Native provisioning that starts declared programs must receive the same relocation, never run against the shared Pi config first.
3. Before any Pi surface renders, the sandbox user's bootstrap builds the home from the selected Pi pack's existing `session_homes` declaration. It renders the profile-dependent files **only** into the new home, redirects render baselines to session-local sidecars, and exports the declared relocation variable. Links carry shared auth, sessions/history, installations, tools, cache and agent definitions, including declared absent-file links; never copy them or link `*.lock`. Surfaces outside the declared directory keep their current behavior, including the cross-tool MCP path limitation in [the settled design](per-session-profiles.md#7-what-this-does-not-cover).
4. Another launch in the same workspace repeats with a different home. It may render shared compatibility files, but neither its render nor its cleanup may replace, delete or re-point the first session's Pi files or relocation. Nothing assumes which command will start Pi; a bare shell or unrelated command receives the same declared home. No `-p` continues to mean the configured default profile.

**Recommended namespace:** locate the home and baseline under the sandbox account's existing writable state and the workspace's sidecar respectively, but name native homes by the *workspace identity as well as the session id*. Today `HoldAccountHome` refuses another workspace while a session owns the account ([orchestrator.go](../../internal/macosuser/orchestrator.go#L102-L115)); this does not protect a stale home after that session has ended. A subsequent workspace must not capture or remove the prior workspace's leftover home. The host passes its existing canonical workspace identity as launch input; do not infer ownership from the current account home or from a random id alone. This is a narrow identity/cleanup interface to the future multi-workspace lane, not a selection of that lane's account model, ACL scheme or mount layout. If that lane changes where shared agent state lives, it must provide an equivalent writable shared-directory and workspace namespace; this design must not dictate whether accounts are reused.

**Explicit override:** when a user's composed environment already supplies `PI_CODING_AGENT_DIR`, keep that value, warn that this launch does not get Pi profile isolation, and do not render into the shared declared Pi directory on that session's behalf. Never overwrite the override. If home construction, link safety, baseline creation, or relocation export fails, refuse the launch with the failed path and a repair/retry instruction; falling back to shared Pi config would recreate the collision. No `session_homes` declaration or no Pi pack means no Pi home; other packs stay as they are.

## 3. Capture, life and failure

The owner-approved capture meaning is **user edits via the existing surface/layer capture**, not wholesale generated-file copy-back. Recommend serializing native capture and render for a workspace under its existing render lock; a new entry captures only completed rendered files against their own baseline, skips unparsable/mid-write files and retains those files for a retry. Computed/provider-selected fields are regenerated by the next entry's own profile; two edits of the same capturable key reach later entries in capture order, while the already-running sessions retain their snapshots. A boot failure after creating a home keeps its bytes and baseline for safe retry rather than treating incomplete generation as user edits. No capture path writes the host user's native `~/.pi/agent/settings.json`.

The existing native session-record flock is the evidence that a launch is active; a process may also leave detached descendants. **Recommendation:** do not retire a home merely because the foreground command ended or a later entry began. At normal exit, retain the home if descendants can still read it; reuse the existing session liveness/termination evidence to mark a home eligible, then capture and remove it on a later native launch or explicit cleanup once it is proven unused. Crash leaves the record/home for retry; an unknown or unavailable liveness result keeps the home. An incomplete capture or a stray (undeclared regular file or replaced link) retains its source and names a next action. Cleanup of host-owned staging files and sandbox-owned home files are separate acts with the same session key, not a new root operation. A reaper cannot assume host uid can unlink sandbox-owned Pi state; execute ordinary account-scoped cleanup through the already supported run-as boundary or let the sandbox bootstrap remove its own ended homes. Do not delete a home without successful capture and a positive gone verdict.

**The boundary with multi-workspace work:** its lane owns whether simultaneous workspaces can safely render *other* shared account files, how state/sidecars are isolated, and what account each workspace uses. This proposal solves simultaneous *profiles in one workspace* independently, if the existing native single-workspace launch is otherwise usable. It does **not** claim full simultaneous multi-workspace safety; workspace-qualified homes prevent one particularly destructive cleanup, but do not cure a shared auth policy, other packs' render or account overlap. The two implementations may land in either order only if they preserve this namespace/lease interface and are reconciled before both are called supported.

## 4. Proof and limits

**Automated, no agents or APIs:** two synthetic launches in one workspace with different profile wire tables produce distinct Pi homes; after B and a bare default-profile launch, A's `settings.json` and Pi list files are byte-identical; A's inherited env points only at A; shared absent-file links create one shared destination; override/no pack/refusal paths do not rewrite shared Pi files. Tests must exercise the real native plan/bootstrap/launch call sites, not only the home builder. Assert old 16-hex session records remain reapable, a live record protects its home, failed capture retains it, and cleanup cannot cross workspace namespaces. Verify sandbox read/write and profile denial of host-owned staging with a real Mac runtime; Linux unit tests and a darwin cross-build cannot prove Seatbelt.

**Mac agent, manual and separate:** on a Mac with two real Pi sessions in one workspace, observe provider A's new subagents before and after B attaches, then reverse; repeat with a bare unrelated command and configured default. Check foreground and background/detached subagents actually inherit `PI_CODING_AGENT_DIR` and keep A's model scope and provider policy; no agent/API call is made by automated tests. Verify `/login` is shared, both sessions see history and installations, a user setting saved in A reaches a later session through capture while B's running config is unchanged, and an explicit directory override warns. Measure active-child survival across parent exit, crash/relaunch capture and cleanup, and native Seatbelt physical-path access. Provider permissions may prohibit making both live calls in one account; record which providers can be exercised and do not substitute `pi --list-models` for subagent/API proof. A failed Mac measurement leaves the design unverified, not silently built.
