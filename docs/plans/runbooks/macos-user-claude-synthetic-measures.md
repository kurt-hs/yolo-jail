---
status: accepted
stage: CURRENT
next: "Rerun synthetic checks as wiring changes; native vendor and real-login trials require separate explicit authorization"
tags: [runbook, macos-user, claude, synthetic, credentials]
summary: "No-secret filesystem and launch-contract checks; not vendor or Keychain certification"
---

# Native Claude: no-secret coordination checks

**Status:** A synthetic procedure only. Every credential byte comes from a fixed fake literal in a test; it never reads a real credential or invokes Claude. Linux execution checks the local filesystem and native layout derivation, not macOS runtime enforcement.

**Scope:** [the authorized synthetic slice](../../design/macos-user-claude-refresh.md#the-native-measurement-contract). The [credential reference](../../reference/agent-credentials.md#the-claude-oauth-broker) owns the current bridge; [the view design](../../design/claude-login-without-interception.md#11-decision-ledger) owns rollout. The hostname TLS terminator stays declined, while the broker is not rejected. No rollout, login-flow redesign or permission grant is part of these checks.

## Run the synthetic checks

From the checkout, with ordinary shared Go caches:

```console
$ go test -count=1 -run 'TestNativeSynthetic' ./internal/claudeview ./internal/cli/run
$ go test -race -count=1 ./internal/claudeview
$ go test -short ./internal/cli/run -run 'Test(TheMacosUserSessionPointsClaudesStoreAtTheWritableSharedDir|TheBridgeAndTheViewAreMutuallyExclusive|BothContainerBackendsPointClaudesStoreAtTheWritableSharedDir)'
```

All layouts are under fresh `t.TempDir` roots. The fake child is the test executable, selected by a helper test name, with only its scratch `CLAUDE_SECURESTORAGE_CONFIG_DIR` and a synthetic-helper marker in its environment. It has a ten-second deadline and is killed/reaped on cleanup. No native account is created, no production bootstrap is run, and no real user settings, keychain or HOME files are opened. Do not replace the fixture paths with a real store.

Record the candidate commit, OS/architecture (`go env GOOS GOARCH`), scratch filesystem (`stat -f -c %T /tmp` on Linux; `stat -f %T /tmp` on macOS), commands and pass/fail results. Preserve only that metadata: there is no reason to export credential bodies, even fake ones. A failing check names its assertion; stop and retain the failure rather than falling back to a real CLI trial.

## What passing establishes

| Check | Established by fake participants | Not established |
| :--- | :--- | :--- |
| Native launch delivery | Existing tests drive native `Run` with an injected backend: default secure-storage env names the declared writable account-home machine tier; view selection excludes that pointer. | A real native process honors the env, or Seatbelt/ACL behavior matches the derived policy on this Mac. |
| Two workspace mirrors | Production layout derivation yields two temporary sidecar mirrors to one machine directory; file and both lock directories resolve to the same inodes. | Real account concurrent-session behavior, workspace isolation or Apple Container cross-VM coherence. |
| Storage-lock contention | A fake subprocess takes `.storage-write.lock` by `mkdir`; another acquisition conflicts. The production shared-file writer waits for release, rereads the replacement, preserves fake unrelated data and leaves `.oauth_refresh.lock` alone. | Claude uses either lock, rereads after refresh-lock acquisition, or performs one upstream redemption. The fake refresh directory is only a co-located marker, not a refresh engine. |
| Dead participant | Killing the fake process leaves its storage-lock directory. Explicitly aging its mtime allows the existing production writer to reclaim and release it. | Vendor heartbeat, refresh-lock stale period, real crashed-client detection or live vendor recovery. |
| Atomic replacement | A concurrent reader sees one of two complete fake payloads through repeated production writes; final mode is `0600` and no temporary/lock file remains. Existing inode-replacement tests pin rename. | Power-loss durability, remote filesystem guarantees or Claude's in-memory adoption of external changes. |
| Lock timeout | Existing held/stale-lock tests cover bounded waiting, including unremovable and repeatedly planted directories. | Unconditional serialization: the production writer intentionally proceeds unlocked after its wait expires. |
| Token view | Existing projection/selection tests establish allowlisted access-token-only bytes and off-default selection. | Native consumption, file-versus-Keychain precedence, revoked-token 401 recovery or rollout approval. |

The process tests add evidence to already-wired behavior, not a bug fix. They simulate neither proper-lockfile's full protocol nor Claude's refresh API. A kernel refusing a duplicate `mkdir` does not prove a vendor client requests it.

## Recorded synthetic evidence

On 2026-10-20, harness commit `54038ec3f` passed the commands above on Linux/amd64 with btrfs scratch storage. The two synthetic packages also passed twenty consecutive runs, and the native-layout test passed under the race detector. The three documents passed the pinned Vantage checker. These are local fixture results only: no real Mac vendor, Seatbelt or Keychain trial ran, and publication was neither checked nor performed.

## Deferred native and real-login work

Separate explicit authorization is required before a real credential read, Claude session, `/login`, `/logout`, auth-status or API call. Start with the bridge's native file-versus-Keychain choice and overlapping vendor refresh locks; do not assume they fail. Then follow [the existing view measurement runbook](claude-credential-view-measures.md#part-d--the-mac-and-the-day-on-a-rootless-host) for startup, external rewrites, 401 recovery, restart, concurrent workspaces, login/logout scope, sleep/wake catch-up and broker shutdown.

The [rootless-host day](claude-credential-view-measures.md#the-day-on-a-real-rootless-host), other-backend measures and off-default [CL-D11](../../design/claude-login-without-interception.md#CL-D11) remain unchanged. Synthetic success is not a substitute. Native keychain access remains governed by [the host-keychain ruling](../../design/keychain-from-a-jail.md#8-decision-ledger) and [the manual native boundary checks](macos-user-manual-checks.md); do not use those checks to infer a new credential grant. If a real vendor trial later contradicts the design, retain a redacted reproduction and return to the owner before changing mechanisms.
