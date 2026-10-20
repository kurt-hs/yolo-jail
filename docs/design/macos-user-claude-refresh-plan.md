---
status: accepted
stage: DECIDED
next: "Independently review the completed synthetic candidate; vendor and real-login trials remain separately authorized work"
depends-on:
  - macos-user-claude-refresh.md
---

# Plan: synthetic native Claude coordination contract

**Status:** 2026-10-20. Written against `f8478d3b76c46b25de60fc243c1f6d896c4fc0ce` after the owner's authorization of this bounded slice. Publication is unconfirmed; no publication or rollout work belongs here.

**Design:** [native Claude measurements](macos-user-claude-refresh.md). The design wins on behavior, the tree wins on fact, this plan is advice. No new product ruling is needed for synthetic fixtures.

## Map and reuse

| Path | Work |
| :--- | :--- |
| [synthetic test harness](../../internal/claudeview/nativecoordination_test.go) | New: cooperating fake subprocess and production shared-file writer, fixed fake bytes only. |
| [native layout harness](../../internal/cli/run/nativeclaudecoordination_test.go) | New: two scratch workspace mirrors reach the same machine-store file and lock inodes. |
| [synthetic runbook](../plans/runbooks/macos-user-claude-synthetic-measures.md) | New: commands, evidence contract, limitations and deferred native trials. |
| [native design](macos-user-claude-refresh.md) and this plan | Record the authorized slice, evidence and remaining external work. |

**Constraints:** reuse `UpdateSharedFile`, `StorageLockDir`, `StorageLockWait` and `SecureStorageEnv` from [the view writer](../../internal/claudeview/claudeview.go). Its timeout intentionally allows a write without the storage lock; do not change that contract or claim unconditional serialization. A directory lock survives participant death and is reclaimed by age, not automatically by the kernel. The fake refresh lock has no vendor heartbeat/retry/stale implementation.

Existing coverage is not work to duplicate:

- [Launch bridge tests](../../internal/cli/run/claudesecurestorage_test.go) drive native `Run`, assert the delivered account-home directory is writable, and pin both container call sites. The view and shared-store pointer are mutually exclusive.
- [View tests](../../internal/claudeview/claudeview_test.go) pin off-default selection, access-token-only projection, confined writes, replacement inode and stale/held storage locks.
- [Shared-file tests](../../internal/claudeview/sharedfile_test.go) pin link refusal/replacement and bounded unremovable or repeatedly planted stale locks.
- [Broker view tests](../../internal/oauthbroker/views_test.go) already cover registration and canonical-login projection; fake native locks do not replace broker tests.

## Task 1 — process-level synthetic contract (independent)

**Settled — do not reopen:** directory/file co-location and default wiring already exist. This is missing evidence, not a diagnosed production bug. Do not manufacture a failing behavior or edit production code to justify a test.

**Tests first:** add source-named tests in the new harnesses, then run them against unchanged production code. Require two scratch workspace mirrors derived from the production native layout to reach the same file and lock inodes. Require a real fake subprocess holding the exact storage directory lock, a blocked mutation until it writes/releases, a reread of its fixed fake file, refresh-lock survival, process death plus aged-lock recovery, and repeated old-or-new complete reads during production replacements. Assert `0600` and no leftover temporary files. The coverage-gap precondition is that `go test -list 'TestNativeSynthetic' ./internal/claudeview` lists no such tests at the written-against commit; the new tests are expected green, not a fabricated red regression.

**Advice:** use the Go test executable as the child with a narrowly selected helper test; handshake over pipes, bound waits, kill/reap in cleanup, and give it only scratch paths and fake literals. No child invokes Claude or the network. Keep global lock-wait overrides serial, like existing tests.

**Gates:**

```console
$ go test -count=1 -run 'TestNativeSynthetic' ./internal/claudeview ./internal/cli/run
$ go test -race -count=1 ./internal/claudeview
$ go test -short ./internal/cli/run -run 'Test(TheMacosUserSessionPointsClaudesStoreAtTheWritableSharedDir|TheBridgeAndTheViewAreMutuallyExclusive|BothContainerBackendsPointClaudesStoreAtTheWritableSharedDir)'
```

**Fences:** no production edits (especially the shared native launch file), rollout/default changes, host settings, credentials, keychain, new accounts/ACLs/root, real client/login/API calls, publication/install/deploy or primary landing. Standard shared Go caches only. Stop on unexpected launch/runtime failure or a requirement for unauthorized credentials; do not switch CLIs. Test-only changes do not require a live nested launch: no shipped runtime behavior changes.

**Commit:** `test(claudeview): measure synthetic native storage coordination`.

## Task 2 — runbook and measured bounds (after 1)

Describe exactly which fake protocol was checked and where to rerun it, distinguishing launch call-site coverage from filesystem process evidence. Record tested OS/filesystem and commands in the candidate report; do not certify native Seatbelt or vendor behavior on Linux. Link the owning [credential view runbook](../plans/runbooks/claude-credential-view-measures.md) and [host-keychain ruling](keychain-from-a-jail.md#8-decision-ledger). No real-login commands are executed in this slice.

Keep the whole native design `DECIDED`: the synthetic slice can finish, but native vendor file/Keychain precedence, real refresh lock behavior, login/logout, 401 recovery and hardware trials remain external work requiring separate explicit authorization. The existing rootless-host day and other-backend gates remain intact. A broker endpoint alone proves no native client route; the TLS terminator stays declined, the broker is not rejected.

**Gates:** run the pinned document checker on the changed docs, `git diff --check`, and one independent final review of the stable candidate (parent-owned). No repeated full integration/whole-tree gates here; the integration owner runs combined gates.

**Commit:** `docs(claude): record synthetic coordination evidence and native limits`.

## Candidate evidence

The harnesses are committed in `54038ec3f`. On Linux/amd64 with btrfs scratch storage, both synthetic packages passed twenty consecutive runs; the full view package and targeted native-layout harness passed with the race detector. The three existing launch bridge/selection checks passed. No production behavior changed. The pinned document checker passed the design, plan and synthetic runbook. Independent final review and combined landing gates belong to the parent integration owner, not this writer.

The completion reader census was regenerated because the native-layout fixture loads the shipped pack outside its package. Its generated map also discovered the checkout's existing hidden skill tree; that is census metadata, not new runtime authority.

## Remaining external work

No credentials are required to complete this slice. Synthetic evidence cannot authorize the access-token-only view rollout or delete interception. Later owner-authorized native trials follow bridge behavior first, then the existing view runbook; failure requires a redacted reproduction and owner discussion, not a new login mechanism, hostname interception or permission widening.
