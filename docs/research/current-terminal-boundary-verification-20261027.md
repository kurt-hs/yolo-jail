---
title: "Current Linux TTY resume-boundary verification"
date: 2026-10-27
status: in-review
tags: [research, linux, tty, cancellation, tests]
summary: "Records the bounded TTY source, tests and review, plus later combined-gate qualification, without claiming item acceptance, incident cause, or native-runtime evidence."
---

# Current Linux TTY resume-boundary verification

**Status:** Current-source record for the bounded Linux TTY-proxy delta on top of `05a7742a8170d9f74a581a3708674f8b08916867` (tree `b12300122a7d61111451383c0ac5af10de20c136`). This is not an item verdict, landing certificate, incident-cause finding, or native macOS/Kitty certification.

This record bridges the two reviewed TTY commits `79564ac698d995cde901f6d3eb825220e7bfddb6` and `55ed066230021a078316229547a7fd3487fabf95` as source IDs and records fresh focused execution on the combined candidate. The immutable Git history holds the full source diffs.

## Later combined qualification (2026-10-28)

The exact four-path decoder-lifetime repair `c1ed01ddd88ba9283fb670a1e4043c2557d72127` was imported as `094a910bb3213a0697dc9531a590beb99e1df404`. It is test-only plus proof/census bookkeeping; the product decoder and the TTY files are unchanged. The original independent reviewer subsequently returned **OK / no issues** on that narrow correction; the [Mac caller record](./current-mac-caller-verification-20261027.md) contains the sanitized review scope and limits. Writer RED/GREEN receipts were from the repaired 05-base working tree before the c1 commit, not post-commit reruns.

On clean commit `094a910bb3213a0697dc9531a590beb99e1df404`, tree `30762d4f8d608f0b52839ecb5c636394f62ef1b7`, exactly one `just check-ci` ran and directly exited `0` in 237.087s. The documentation-only review-status update then became commit `d8ce418acb98eb73b7f02436c53f4354e64b4b14`, tree `f44a4b987eebcd1c4dd0b067cad8eee2e0beaff3`. `git diff --name-only 094a910bb3213a0697dc9531a590beb99e1df404..d8ce418acb98eb73b7f02436c53f4354e64b4b14` is only `docs/research/current-mac-caller-verification-20261027.md`; source, tests, census and build inputs match the passing check-ci tree. The updated file's targeted Vantage check passed; check-ci was not repeated for this docs-only edit.

Exactly one full `go test -count=1 -timeout 0 ./integration` then ran on d8ce/tree `f44a4b987eebcd1c4dd0b067cad8eee2e0beaff3` and directly exited `0` (package `ok` in 743.887s; command duration 744.386s). Its integration `TestMain` generated and used the matched absolute development launcher from that checkout's source. The normal harness isolates the user config to empty/fixtures and adds `YOLO_NO_PROGRAM_READINESS=1` only to launch subprocesses; the real vendor-install switch was absent, and inherited `HOME`/shared caches were not redirected. No real agent or credential/API activity is claimed. The full suite is Linux/Podman evidence only; this rootful nested host does not certify rootless-only or reachability behavior.

## Requirement, production caller, and regression

When a Linux TTY proxy resumes after `SIGCONT`, pending input must not be forwarded if caller cancellation or child exit was published while the `StageResumed` callback ran. The check protects this callback window; it is not a promise that cancellation arbitrates atomically with every later write.

The production entry is `internal/cli/run/proxy_linux.go`: `runWithProxy` delegates to `ttyproxy.RunWithProxyObserved`. In `internal/ttyproxy/ttyproxy.go`, `proxyLoop`'s `resumeSuspendedInput` runs the resumed callback, then reads `cancellationOrExitPublished`; when set it clears the pending bytes and returns before enqueue. The reviewed delta keeps that policy and its placement. Its added observer and write-injection fields are package-private test seams; with no test callback the resumed write still uses `unix.Write`.

The permanent regression is `TestSuspendResumePreservesOutputQueueOrder` in [`internal/ttyproxy/ttyproxy_test.go`](../../internal/ttyproxy/ttyproxy_test.go). Its controlled `caller-cancel-published-during-resume` subtest publishes cancellation from the real `StageResumed` callback and requires the owned child not to consume the queued ordinary input. The `caller-cancel-published-after-resume-check` control intentionally publishes after the observed check: the check has already sampled false, so the test requires and records the permitted subsequent write/consumption. That control marks the boundary; it does not grant product permission or claim an arbitrary atomic cancellation guarantee.

The same test retains foreground-resume, background-child-exit, and queued-termination controls, and adds transient resumed-write outcomes: `(0, EAGAIN)`, `(-1, EAGAIN)`, and `(-1, EINTR)` before a successful write. Accounting clamps a negative reported count to zero for observations; retry/order semantics remain covered. These are observation/accounting controls, not changes to payload, deadline, sampling, signal, or PID-ownership policy.

## Reviewed source delta and earlier findings

The imported source changes are exactly the two committed TTY deltas `79564ac698d995cde901f6d3eb825220e7bfddb6` and `55ed066230021a078316229547a7fd3487fabf95`, applied in order. They touch only [`internal/ttyproxy/ttyproxy.go`](../../internal/ttyproxy/ttyproxy.go) and [`internal/ttyproxy/ttyproxy_test.go`](../../internal/ttyproxy/ttyproxy_test.go). The first adds boundary receipts and write-result observation; the second hardens resumed-write injection and nonnegative count accounting. No assertion was weakened and no source outside those two files was repaired or changed for this import.

Review provenance is preserved rather than rewritten: the independent HIGH review of `79564` initially returned **BLOCK** with two P1 findings; the same reviewer’s correction `c5c0` returned **OK** and resolved both against the `55ed` correction. This bounded requalification does not erase that history or promote it to acceptance of the larger work item. The prior bounded review `9229` remains limited to its unchanged production source and its noted older-kernel signal-priority limitation; it is not a fresh review of the current combined tree.

The older negative evidence remains distinct and unresolved where it was unresolved. In particular, the bad-proxy/R-oracle receipt `SDW` is not the real-caller-gate-deletion receipt `SDWR`; neither substitutes for the other. The earlier `?ZAR?` natural failure and the preserved original timeout remain unclosed as to original cause. The focused green below is not causal closure, and does not establish Kitty, load, or historical kernel causality. See the retained [runtime/transport evidence archive](./runtime-transport-verification-20261027/README.md) for the original bounded evidence and its limitations; that archive is unchanged by this update.

## Focused execution and combined-tree gate status

On the combined source candidate, this uncached command passed:

```sh
go test -count=1 -timeout 0 ./internal/ttyproxy
```

The command directly exited `0`; the package reported `ok` in 48.793s (49.768s command duration). This executes the package suite, including the named resume-boundary controls.

One required broad gate was then run exactly once on this candidate:

```sh
just check-ci
```

It directly exited `1` after 288.892s. `go test -short ./...` reported eight failures in `internal/cli`: `TestPromoteRefusesToWriteIntoAPackWhoseManifestHasProblems`, `TestPromoteDryRunAndPlanSayAMalformedDestinationIsRefused`, `TestHostLaunchNamesTheMalformedPackDeclaringTheSelectedProfile`, `TestPackLintReportsStagingRefusalAndManifestTogether`, `TestPackLintValidatesManifest`, `TestPackLintNamesARenamedFieldOnce`, `TestPackLintRefusesTheBriefingDefaultsExceptions`, and `TestJailTreeFixturesRestoreTheStrictDecoder`. The last test explicitly reported that “the tolerant decoder was left on for every later test.” The other seven observed unexpected acceptance/omission of malformed or unknown manifest-field refusals. These outputs were consistent with a decoder-state leak, but the red itself did not establish which earlier test or caller caused it. Root independently observed the same result. The separately authorized narrow test-only repair is recorded above; the initial eight-test red remains preserved and was not rerun on the same unrepaired tree.

The required build and bounded nested launch probe passed on this candidate:

```sh
just build-dev-bundle
go test -v -count=1 -timeout 0 -run '^(TestNestedMechanicsTracksAppliedLaunchAndLiveMounts|TestStopEndsTheWorkspaceJail)$' ./integration
```

`just build-dev-bundle` exited `0`; its absolute launcher and adjacent bundle were stamped with commit `376dd306c093`, snapshot identity `4a4948437e85774c653b0efb62977a59a951c954ccca352df820ac269d63528b`, and build kind `development`. The focused integration command directly exited `0` and visibly ran both named tests: `TestNestedMechanicsTracksAppliedLaunchAndLiveMounts` passed (fresh non-agent launch, attach, and applied fixture observation), and `TestStopEndsTheWorkspaceJail` passed (non-agent fresh launch and explicit stop). The integration harness invokes the matched launcher by absolute path, isolates user configuration to its empty/fixture setup, and adds `YOLO_NO_PROGRAM_READINESS=1` only to its launch commands. For this invocation only, `TMPDIR` pointed at a mode-0700 durable scratch directory so harness workspaces were minted there; the inherited `HOME` and shared caches were unchanged. These launch tests do not themselves exercise an interactive PTY resume or establish rootless/reachability behavior.

**Capture provenance:** the earlier focused TTY package, initial pre-repair CI red, bundle build, and bounded nested fixture output were retained from parent-process pipes written after command exit; they are useful but not exclusive prelaunch captures. The passing `just check-ci` and full integration results documented above used new unique mode-0700 receipt directories with mode-0600 `O_EXCL` stdout/stderr files opened before process launch, direct child FDs, fsync, and readback. Existing earlier captures were not overwritten.

The full `go test -count=1 -timeout 0 ./integration` was not started on the known-red `376dd` candidate. It was later run once after the test-only repair on the combined tree documented above. No failure was skipped, masked, or fixed with an environment hatch; the later docs-only status edits are explicitly bridged to their actual tested source/test inputs.

## Scope and custody

The TTY source/test import itself remains the byte-preserved `79564` and `55ed` pair. The separately authorized `c1ed` test-only repair and later one-paragraph Mac review note are documented and committed independently; neither rewrites TTY policy or the historical controls. The owner lab bytes and protected tag remain unchanged. No item-2/TOML/library/dependency work, credentials, remote refs, protected tags, host source, native Mac execution, or process-ID ownership logic was changed.

This document records the TTY source/control history and later combined CI/full-integration results with their actual tested commits and documentation-only bridges. Passing these gates does not decide item-16 acceptance, close the historical failure causes, or certify native runtime behavior. Container nesting cannot certify rootless-only or host-loopback reachability; this host reported `Rootless=false`. Root retains serial landing and item acceptance.
