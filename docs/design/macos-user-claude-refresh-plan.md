---
status: draft
stage: SKETCH
next: "After owner discussion and Mac measurements, resolve the design's two questions and complete this sketch against the then-current tree; no build before 0.15 release"
depends-on:
  - macos-user-claude-refresh.md#OQ-MCR1
  - macos-user-claude-refresh.md#OQ-MCR2
---

# Sketch: verify the native Claude credential source before coordinating refresh

**Status:** 2026-10-19 — incomplete and unstable while [OQ-MCR1](macos-user-claude-refresh.md#OQ-MCR1) and [OQ-MCR2](macos-user-claude-refresh.md#OQ-MCR2) remain open. Written against `23eb5097b`; explicitly **not build-authorized**. Build and landing must wait until after 0.15 and owner discussion.

**Design:** [native Claude refresh](macos-user-claude-refresh.md). The design wins on behavior; the tree wins on fact; this sketch is advice and the first thing to be wrong. Do not implement from a `SKETCH`.

## Known seams to recheck at promotion

| Existing path | Evidence or possible reuse (not an edit instruction) |
| :--- | :--- |
| [Claude pack](../../packs/claude/pack.json), [broker manifest](../../packs/claude/loopholes/claude-oauth-broker/manifest.jsonc) | Selected pack declares shared and workspace state; native backend declines its declared intercepting jail daemon, **not** its host daemon. |
| [view selector](../../internal/cli/run/claudecredentialview.go), [secure-storage selector](../../internal/cli/run/claudesecurestorage.go), [Mac arm](../../internal/cli/run/run.go) | One view predicate controls registration versus shared store; Mac arm must deliver the same resolved predicate to the sandbox. |
| [view writer](../../internal/claudeview/claudeview.go), [registration and enrollment](../../internal/oauthbroker/views.go) | Confined per-workspace write; preserve unrelated keys and vendor storage lock. Check native Keychain/file behavior before treating this as sufficient. |
| [catch-up](../../internal/cli/run/claudelogincatchup.go), [per-launch ownership](host-daemon-ownership.md#HD-D29) | Already starts per-launch host service and catches up under shared refresh lock. Never invent a second timer or let a shutdown cut a redeemed token from its persisted replacement. |
| [native home tiers](../reference/macos-user-home-tiers.md), [native keychain route](keychain-from-a-jail.md#OQ-KC5) | Workspace overlay and Keychain route belong to other lanes; this sketch chooses neither. |

## Measurement-first sequence (future, not authorized here)

1. **No-secret fixtures.** Check a synthetic native `HOME` with two workspace overlays: view registration A/B, one canonical lock, missing/corrupt login, concurrent mock redemptions, stale view repair, workspace logout and machine logout. Assert neither file nor error/log output contains a synthetic refresh-token literal; assert no forced token copy into a workspace, no symlink-followed host write, and a failed view registration is announced rather than described as coordinated. Fixture-only tests are not proof of the native CLI's store choice.
2. **Mac-agent non-login inspection.** Record native `claude --version`, whether the documented secure-storage override is present in that version, layout/readability of *synthetic* account-home and workspace-tier paths, and Seatbelt launch/endpoint ACL observations; never open a real credential file. A `--version` probe cannot prove OAuth behavior.
3. **Owner-authorized real-login measurements.** Follow [the existing view runbook](../plans/runbooks/claude-credential-view-measures.md) after adapting only its workspace names and redaction to native Mac. Verify file versus Keychain source and rewrite adoption, 401 recovery, concurrent refresh, `/login`/`/logout`, sleep/wake catch-up and daemon teardown. Owner performs or explicitly authorizes every real-token operation and retains only statuses/timings/redacted fingerprints. **Blocked on [OQ-MCR1](macos-user-claude-refresh.md#OQ-MCR1)** before choosing any native client seam.
4. **Boundary review with the workspace/profile lane.** Agree which per-workspace overlay, account-home machine tier and host-only broker state are visible to each sandbox; test with synthetic credentials. **Blocked on [OQ-MCR2](macos-user-claude-refresh.md#OQ-MCR2)** before making isolation claims or planning a host write path.
5. **Promote only after rulings.** Re-read current source and decide a narrow file/task map, failing tests, integration gates, scope fences and one-commit tasks. If no supported native CLI input is demonstrated, stop and return the external capability gap; no transparent hostname interception, process-wide proxy or new schema as a workaround. The landing owner runs the relevant Mac suite and checks reference claims in [agent credentials](../reference/agent-credentials.md), [native features](../reference/macos-user-nix-and-features.md) and [OAuth interposition](../reference/claude-oauth-interposition.md). These shared docs are *not* edited in this design lane.

## Dependencies and stop conditions

- This lane depends on the [existing view design's measurement gate](claude-login-without-interception.md#7-what-must-be-measured-before-building), the owner-authorized native experiment, the [native keychain question](keychain-from-a-jail.md#OQ-KC5) for any route that touches Keychain, and the workspace/profile lane's isolation interface. It does **not** pick that lane's shared-home policy.
- Existing [OQ-CL1](claude-login-without-interception.md#11-decision-ledger) still orders global interception deletion after measured view behavior and a real rootless-host day. A native-only claim cannot waive that gate.
- Stop when native Claude ignores the file override, vendor behavior differs from the existing view assumptions, a test would require real-token automation, an auth service requires a new host-global credential grant, or a fix changes a file outside the settled implementation fence. Report the observed seam, not a guessed replacement.
