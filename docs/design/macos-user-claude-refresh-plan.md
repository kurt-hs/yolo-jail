---
status: draft
stage: SKETCH
next: "Measure existing native CL-D22 lock and view behavior; refine only after owner discussion following 0.15, with no build authorization"
---

# Sketch: measure native Claude's existing coordination paths

**Status:** 2026-10-19 — incomplete; measurement sketch only, against `23eb5097b`. Not a build handoff. Any build and landing wait until after 0.15 and owner discussion.

**Design:** [native Claude measurement scope](macos-user-claude-refresh.md). The [owning view design](claude-login-without-interception.md) wins on behavior, the tree wins on fact, and this sketch is advice. Nothing is built from a `SKETCH`.

## Existing seams to recheck before promotion

| Path | Known role; not an edit instruction |
| :--- | :--- |
| [Claude pack](../../packs/claude/pack.json), [broker manifest](../../packs/claude/loopholes/claude-oauth-broker/manifest.jsonc) | Default-enabled broker host half and shared file; native declines the intercepted jail half. |
| [native launch](../../internal/cli/run/run.go#L792-L805), [store delivery](../../internal/cli/run/run.go#L1052-L1056) | Already registers an opt-in view and delivers the secure-storage directory to the sandbox on the default path. |
| [view selector](../../internal/cli/run/claudecredentialview.go), [secure-storage selector](../../internal/cli/run/claudesecurestorage.go) | One predicate excludes the shared-store pointer when a view is selected. |
| [view writer](../../internal/claudeview/claudeview.go), [enrollment](../../internal/oauthbroker/views.go) | Confined per-workspace write, unrelated-key preservation and vendor storage lock; native CLI consumption unmeasured. |
| [catch-up](../../internal/cli/run/claudelogincatchup.go), [ownership](host-daemon-ownership.md#HD-D29) | Existing per-launch daemon and shared broker refresh lock, no idle timer. |
| [native home tiers](../reference/macos-user-home-tiers.md) | Cross-workspace layout policy is the sibling lane's, not this sketch's. |

## Measurement order, not build tasks

1. **No-secret fixtures.** Verify one physical native shared-store and lock path for two synthetic workspace layouts and the separate paths when an opted-in view is delivered. Simulate concurrent mock redemptions, stale view repair, missing/corrupt login, workspace logout and host logout; test that no synthetic refresh-token literal appears in views, messages or logs. These tests cannot establish vendor file-versus-Keychain precedence or a real lock acquisition.
2. **Mac-agent inspection without login.** Record `claude --version`, synthetic directory/Seatbelt/endpoint ACL behavior and which secure-storage variable the native launch delivers. Do not read a real keychain item or credential file. Check the exact vendor version before the owner-authorized experiment.
3. **Owner-authorized bridge measurement first.** On a disposable native account/workspace pair, observe default `CLAUDE_SECURESTORAGE_CONFIG_DIR`, Keychain/file choice, overlapping vendor lock acquisitions and the second client's reread of a rotated file. If coordination works, record that without inventing a new failure or altering the path. If it fails, retain a redacted reproduction and stop for owner discussion. No automation in this design lane runs a real refresh.
4. **Owner-authorized view measurement second.** Adapt only workspace names and redaction from [the existing runbook](../plans/runbooks/claude-credential-view-measures.md): startup, no-refresh-token view, rewrite/401 recovery, native Keychain precedence, two concurrent distinct workspaces, login/logout scope, sleep/wake catch-up and broker teardown. Retain status, timing and redacted fingerprints, not actual tokens.
5. **After 0.15 and the owner review.** Re-read the owning [view design](claude-login-without-interception.md#11-decision-ledger) and current tree. If measurements support a native rollout, complete a narrow file/task map, failing tests, integration gates, fence and commit sequence. Respect [OQ-CL1](claude-login-without-interception.md#11-decision-ledger)'s other-backend measures and real rootless-host day before any global removal. If native CLI ignores the file store, report the supported vendor capability gap; do not build an interception substitute.

## Dependencies and stops

- Native coordination evidence depends on the vendor-lock measurement **before** assessing a missing-coordination claim. View rollout depends on the [existing view measurement gate](claude-login-without-interception.md#7-what-must-be-measured-before-building), not a new owner Answer in this sketch. A stronger per-workspace isolation claim depends on the workspace/profile lane and [the home-tier open question](../reference/macos-user-home-tiers.md#oq-ht6); any Keychain route touches [its owning question](keychain-from-a-jail.md#OQ-KC5).
- Stop on a vendor behavior unlike the design, a test requiring unauthorized real tokens, a new host-global credential grant, or a fix outside a subsequently approved task fence. Shared reference updates belong to the eventual integration owner, not this design lane.
