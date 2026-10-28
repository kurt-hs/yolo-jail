---
title: "Current Linux TTY resume-boundary verification"
date: 2026-10-27
status: in-review
tags: [research, linux, tty, cancellation, tests]
summary: "Records the reviewed resume-boundary diagnostic delta, current production call site, permanent controls, and the focused package run without treating it as item acceptance or native-runtime evidence."
---

# Current Linux TTY resume-boundary verification

**Status:** Current-source record for the bounded Linux TTY-proxy delta on top of `05a7742a8170d9f74a581a3708674f8b08916867` (tree `b12300122a7d61111451383c0ac5af10de20c136`). This is not an item verdict, landing certificate, incident-cause finding, or native macOS/Kitty certification.

This record bridges the two reviewed TTY commits `79564ac698d995cde901f6d3eb825220e7bfddb6` and `55ed066230021a078316229547a7fd3487fabf95` as source IDs and records fresh focused execution on the combined candidate. The immutable Git history holds the full source diffs.

## Requirement, production caller, and regression

When a Linux TTY proxy resumes after `SIGCONT`, pending input must not be forwarded if caller cancellation or child exit was published while the `StageResumed` callback ran. The check protects this callback window; it is not a promise that cancellation arbitrates atomically with every later write.

The production entry is `internal/cli/run/proxy_linux.go`: `runWithProxy` delegates to `ttyproxy.RunWithProxyObserved`. In `internal/ttyproxy/ttyproxy.go`, `proxyLoop`'s `resumeSuspendedInput` runs the resumed callback, then reads `cancellationOrExitPublished`; when set it clears the pending bytes and returns before enqueue. The reviewed delta keeps that policy and its placement. Its added observer and write-injection fields are package-private test seams; with no test callback the resumed write still uses `unix.Write`.

The permanent regression is `TestSuspendResumePreservesOutputQueueOrder` in [`internal/ttyproxy/ttyproxy_test.go`](../../internal/ttyproxy/ttyproxy_test.go). Its controlled `caller-cancel-published-during-resume` subtest publishes cancellation from the real `StageResumed` callback and requires the owned child not to consume the queued ordinary input. The `caller-cancel-published-after-resume-check` control intentionally publishes after the observed check: the check has already sampled false, so the test requires and records the permitted subsequent write/consumption. That control marks the boundary; it does not grant product permission or claim an arbitrary atomic cancellation guarantee.

The same test retains foreground-resume, background-child-exit, and queued-termination controls, and adds transient resumed-write outcomes: `(0, EAGAIN)`, `(-1, EAGAIN)`, and `(-1, EINTR)` before a successful write. Accounting clamps a negative reported count to zero for observations; retry/order semantics remain covered. These are observation/accounting controls, not changes to payload, deadline, sampling, signal, or PID-ownership policy.

## Reviewed source delta and earlier findings

The imported source changes are exactly the two committed TTY deltas `79564ac698d995cde901f6d3eb825220e7bfddb6` and `55ed066230021a078316229547a7fd3487fabf95`, applied in order. They touch only [`internal/ttyproxy/ttyproxy.go`](../../internal/ttyproxy/ttyproxy.go) and [`internal/ttyproxy/ttyproxy_test.go`](../../internal/ttyproxy/ttyproxy_test.go). The first adds boundary receipts and write-result observation; the second hardens resumed-write injection and nonnegative count accounting. No assertion was weakened and no source outside those two files was repaired or changed for this import.

Review provenance is preserved rather than rewritten: the independent HIGH review of `79564` initially returned **BLOCK** with two P1 findings; the same reviewer’s correction `c5c0` returned **OK** and resolved both against the `55ed` correction. This bounded requalification does not erase that history or promote it to acceptance of the larger work item. The prior bounded review `9229` remains limited to its unchanged production source and its noted older-kernel signal-priority limitation; it is not a fresh review of the current combined tree.

The older negative evidence remains distinct and unresolved where it was unresolved. In particular, the bad-proxy/R-oracle receipt `SDW` is not the real-caller-gate-deletion receipt `SDWR`; neither substitutes for the other. The earlier `?ZAR?` natural failure and the preserved original timeout remain unclosed as to original cause. The focused green below is not causal closure, and does not establish Kitty, load, or historical kernel causality. See the retained [runtime/transport evidence archive](./runtime-transport-verification-20261027/README.md) for the original bounded evidence and its limitations; that archive is unchanged by this update.

## Focused execution

On the combined source candidate, this uncached command passed:

```sh
go test -count=1 -timeout 0 ./internal/ttyproxy
```

The command directly exited `0`; the package reported `ok` in 48.793s (49.768s command duration). This executes the package suite, including the named resume-boundary controls. It is focused package evidence only: it does not replace the whole-tree check, full integration suite, bundle/nested-launch qualification, independent review, or Root's serial landing guard. No native macOS, Apple Container, real Kitty, real-agent/API, true-rootless, or reachability result is claimed.

## Scope and custody

The only intended source inputs are the byte-preserved `79564` and `55ed` changes. This record is the sole new documentation path. The six producer-independent tests/proof documents and owner lab/tag were not edited; the runtime and Mac caller proof records remain as committed on the base tree. No item-2/TOML/library/dependency work, credentials, remote refs, protected tags, host source, native Mac execution, or process-ID ownership logic was changed.

This document records the focused run, not combined-tree qualification completion. The required serial `just check-ci`, full `go test -count=1 -timeout 0 ./integration`, and build-dev-bundle/absolute-launcher nested verification must be recorded from their actual candidate runs before any landing claim. Container nesting cannot certify rootless-only or host-loopback reachability behavior. Root retains the landing decision and item acceptance.
