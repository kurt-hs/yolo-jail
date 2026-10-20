---
title: "Native macOS sessions need private Pi config without private identity"
date: 2026-10-19
status: accepted
stage: BUILT
next: "Measure native Seatbelt, account liveness and real Pi descendants on a Mac before claiming runtime verification"
tags: [design, macos-user, sessions, pi, profiles]
summary: "Authorized native adaptation of the settled session-home contract; Mac runtime measurement remains separate."
---

# Native macOS sessions need private Pi config without private identity

**Status:** 2026-10-20. Implementation through `46b45756c`, grounded against `f8478d3b`.
MEASURED in synthetic Linux fixtures: native plan identity, actual Darwin bootstrap private
render/capture, shared-state compatibility and real shell/child inheritance with leases.
Darwin arm64 and amd64 cross-builds pass. UNMEASURED: native Seatbelt, account process census
and real Pi descendants on a Mac. Release publication is unconfirmed; none was performed.

> **In short.** Each native launch must render Pi's profile into a directory that only that launch's process tree reads, while linking login, history and installations to the existing shared state. A second launch in the same workspace must not rewrite the first one's Pi config.

**Why it matters.** Container sessions and owned host commands already had private Pi directories. Native launches now key their declared config homes on the existing session identity too, instead of rewriting files another profile's process tree reads.

**The shape.** The existing native identity keys the pack-declared render, private baseline and inherited command lease. Retirement additionally requires an idle-account witness, because detached programs can close inherited descriptors.

**Cost.** More small directories and bookkeeping per native session; native's existing id is already valid for session homes. No change to account ownership or the host user's native Pi settings.

**Start at [§2](#2-one-native-launch).**

**Needs your ruling:** None. The owner authorized this lane on 2026-10-20; the settled session-profile contract is unchanged.

**Reads with:** [the settled cross-backend design](per-session-profiles.md) (the owner Answers and shared session-home semantics), [its implementation plan](per-session-profiles-plan.md) (what actually landed on containers and owned host), [this implementation sketch](macos-user-session-profiles-plan.md) (grounded build handoff).

---

## 1. Boundary and evidence

The owner already accepted per-session Pi directories, shared links, later-session capture of Pi edits, unchanged profile selection, and owned-host parity in [the settled design's decision ledger](per-session-profiles.md#11-decision-ledger). Those are **Answers**, not a new request to choose another Pi config model. The owner authorized their native adaptation on 2026-10-20. Native identity, workspace namespace and conservative cleanup below implement that contract; Mac behavior still requires measurement.

The native host mints one 16-hex identity for staging and its liveness record
([session files](../../internal/macosuser/sessionfiles.go)). The ordinary native plan carries
that same identity in a trusted bootstrap option and the child env file
([run plan](../../internal/macosuser/runplan.go)). Capture/sealed bootstrap receives no ordinary
session option. The [Darwin bootstrap](../../internal/entrypoint/darwin.go) enables declared
session homes and [normal capture](../../internal/entrypoint/sessioncapture.go), while other
native boot steps still run on every launch. No environment-supplied identity authorizes a
session pass.
The native bootstrap already hydrates the **per-launch** sandbox env file for generation ([bootsteps.go](../../internal/entrypoint/bootsteps.go#L164-L185)); the launched commands source that same file. The sandbox account home is a native writable root under Seatbelt, but host-owned staging and arbitrary host homes are not automatically writable ([seatbelt.go](../../internal/macosuser/seatbelt.go#L200-L230)). This design adds no privilege or Pi feature.

## 2. One native launch

1. The host uses the **existing** native 16-hex id for its record, bootstrap and process tree. The entrypoint already accepts it; no new id format, second unrelated Pi id or record migration is needed. Do not accept an env-supplied id from the user, or silently treat a malformed/missing id as permission to render shared Pi config. Dry-run's placeholder is not a real id and makes no home.
2. The host supplies that id in its trusted bootstrap contract **and** in the launched command's per-session env file. The bootstrap uses the trusted value to make a home; a user `env_sources` entry must not override it. The shell, Pi typed later in that shell, foreground children and detached children inherit exactly that id and the resulting `PI_CODING_AGENT_DIR`. Native provisioning that starts declared programs must receive the same relocation, never run against the shared Pi config first.
3. Before any Pi surface renders, the sandbox user's bootstrap builds the home from the selected Pi pack's existing `session_homes` declaration. It renders the profile-dependent files **only** into the new home, redirects render baselines to session-local sidecars, and exports the declared relocation variable. Links carry shared auth, sessions/history, installations, tools, cache and agent definitions, including declared absent-file links; never copy them or link `*.lock`. Surfaces outside the declared directory keep their current behavior, including the cross-tool MCP path limitation in [the settled design](per-session-profiles.md#7-what-this-does-not-cover).
4. Another launch in the same workspace repeats with a different home. It may render shared compatibility files, but neither its render nor its cleanup may replace, delete or re-point the first session's Pi files or relocation. Nothing assumes which command will start Pi; a bare shell or unrelated command receives the same declared home. No `-p` continues to mean the configured default profile.

**Namespace:** homes live under the constant account HOME, in a namespace derived from the
canonical workspace path and then the native session id; baselines live under the workspace's
own sidecar. The [namespace/lease helper](../../internal/nativehome/nativehome.go) produces and
enumerates only that workspace's namespace. Today `HoldAccountHome` refuses another workspace while a session owns the account ([orchestrator.go](../../internal/macosuser/orchestrator.go#L102-L115)); this does not protect a stale home after that session has ended. A subsequent workspace must not capture or remove the prior workspace's leftover home. The host passes its existing canonical workspace identity as launch input; do not infer ownership from the current account home or from a random id alone. This is a narrow identity/cleanup interface to the future multi-workspace lane, not a selection of that lane's account model, ACL scheme or mount layout. If that lane changes where shared agent state lives, it must provide an equivalent writable shared-directory and workspace namespace; this design must not dictate whether accounts are reused.

**Explicit override:** when a user's composed environment already supplies `PI_CODING_AGENT_DIR`, keep that value, warn that this launch does not get Pi profile isolation, and do not render into the shared declared Pi directory on that session's behalf. Never overwrite the override. If home construction, link safety, baseline creation, or relocation export fails, refuse the launch with the failed path and a repair/retry instruction; falling back to shared Pi config would recreate the collision. No `session_homes` declaration or no Pi pack means no Pi home; other packs stay as they are.

## 3. Capture, life and failure

The owner-approved capture meaning is **user edits via the existing surface/layer capture**, not wholesale generated-file copy-back. Serialize native capture and render for a workspace under its existing render lock; a new entry captures only completed rendered files against their own baseline, skips unparsable/mid-write files and retains those files for a retry. Computed/provider-selected fields are regenerated by the next entry's own profile; two edits of the same capturable key reach later entries in capture order, while the already-running sessions retain their snapshots. A boot failure after creating a home keeps its bytes and baseline rather than treating
incomplete generation as user edits. Before private renders, a capture-only compatibility pass
also reads existing shared stateful config into the normal overlay and advances its shared
baseline; it never rewrites those source settings or reads authentication files. Unchanged
legacy state is not re-adopted over newer private-session edits. No prior apply baseline is
required, and selected-profile generated values remain authoritative. No capture path writes the host user's native `~/.pi/agent/settings.json`.

The existing native session-record flock is the evidence that a launch is active; a process may also leave detached descendants. **Lifetime:** do not retire a home merely because the foreground command ended or a later entry began. At normal exit, retain the home if descendants can still read it; keep the native record contract and add an account-owned home lease across the command exec. Because a detached child can close inherited descriptors, retirement also requires an account-only process census proving no other sandbox-account process remains. Capture/cleanup is deferred while any such process is alive; an unavailable census retains all homes. Only a command that reached its launch marks its home eligible; provisioning alone does not. Capture and remove eligible homes on a later launch in the same workspace once both witnesses
prove them unused. No native global reaper or explicit cleanup command is added here; homes in
a workspace never launched again, or whose launch never completed, remain retained. Crash leaves the record/home for retry; an unknown or unavailable liveness result keeps the home. An incomplete capture or a stray (undeclared regular file or replaced link) retains its source and names a next action. Cleanup of host-owned staging files and sandbox-owned home files are separate acts with the same session key, not a new root operation. The sandbox bootstrap removes its own ended homes through the existing run-as boundary; host staging cleanup stays host-owned. Do not delete a home without successful capture and a positive gone verdict. Unknown namespace
siblings, dropped declarations and their baselines are retained, not recursively deleted.
A native render lock failure refuses capture/render; it does not inherit the container's
warning-only lock policy.

**The boundary with multi-workspace work:** its lane owns whether simultaneous workspaces can safely render *other* shared account files, how state/sidecars are isolated, and what account each workspace uses. This proposal solves simultaneous *profiles in one workspace* independently, if the existing native single-workspace launch is otherwise usable. It does **not** claim full simultaneous multi-workspace safety; workspace-qualified homes prevent one particularly destructive cleanup, but do not cure a shared auth policy, other packs' render or account overlap. The two implementations may land in either order only if they preserve this namespace/lease interface and are reconciled before both are called supported.

## 4. Proof and limits

**Automated, no agents or APIs:** two synthetic launches in one workspace with different profile wire tables produce distinct Pi homes; after B and a bare default-profile launch, A's `settings.json` and Pi list files are byte-identical; A's inherited env points only at A; shared absent-file links create one shared destination; override/no pack/refusal paths do not rewrite shared Pi files. Tests must exercise the real native plan/bootstrap/launch call sites, not only the home builder. Assert old 16-hex session records remain reapable, a live record protects its home, failed capture retains it, and cleanup cannot cross workspace namespaces. Verify sandbox read/write and profile denial of host-owned staging with a real Mac runtime; Linux unit tests and a darwin cross-build cannot prove Seatbelt.

**Mac agent, manual and separate:** on a Mac with two real Pi sessions in one workspace, observe provider A's new subagents before and after B attaches, then reverse; repeat with a bare unrelated command and configured default. Check foreground and background/detached subagents actually inherit `PI_CODING_AGENT_DIR` and keep A's model scope and provider policy; no agent/API call is made by automated tests. Verify `/login` is shared, both sessions see history and installations, a user setting saved in A reaches a later session through capture while B's running config is unchanged, and an explicit directory override warns. Measure active-child survival across parent exit, crash/relaunch capture and cleanup, and native Seatbelt physical-path access. Provider permissions may prohibit making both live calls in one account; record which providers can be exercised and do not substitute `pi --list-models` for subagent/API proof. A failed Mac measurement leaves native runtime behavior unverified even though the implementation and synthetic proof exist; it is not a global release blocker.
