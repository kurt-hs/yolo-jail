---
title: "Release CI fixture diagnoses"
date: 2026-10-20
status: open
tags: [lab-report, ci, testing]
summary: "Separates observed CI fixture failures from production conclusions and records their narrow repairs."
---

# Release CI fixture diagnoses, 2026-10-20

A [lab report](README.md). These observations separate CI fixture failures from production conclusions. Earlier repairs were verified against their committed source; the latest cleanup-helper repair below is a local candidate and has not been rerun in CI.

## Observed

- **Run 39359722948, attempt 1** failed both `check-go` and `check-macos`. On Linux, `TestBuildSlotCachedGoodRunsThroughGeneratedLauncherAndCaptureMaterializer` overwrote a target that can be a read-only hard link into the shared capture store. The fixture needed to unlink its installed target first, isolate its home, and assert the stored bytes stayed unchanged. On macOS, `TestMacosUserFloorProbeSeesTheFloorPastAMiseShim` used an unparenthesized `case` pattern inside command substitution, which the runner's Bash rejected. See the [cached-good fixture](../../internal/cli/cachedgoodrecovery_test.go#L496-L520) and [floor-probe fixture](../../integration/macosuserfloor_test.go#L181-L193).
- **Run 39374929076, attempt 1, macOS job 118470835260**, failed `TestWaitForJailContainerGoneBoundsExistenceProbe`: the assertion expected an unanswered probe, but its fake existence command could return `present` before the 40 ms deadline. The implementation correctly retains a valid presence answer when the overall deadline expires; the fixture had not guaranteed a timeout. See the [bounded-probe test](../../internal/cli/stopgone_test.go#L288-L314).
- **Run 39374929076, attempt 1, Linux job 118470835173**, failed `TestPerJailStartupReasonAfterReadinessDeadlineIsNotAccepted` on its elapsed-time assertion (264.7 ms against 180 ms). The test stopwatch included fixture setup and subprocess startup, while the production readiness deadline starts later. The test had already passed its assertion that the late refusal was not accepted; this was not evidence that production exceeded its readiness deadline. See the [late-reason test](../../internal/cli/run/perjailstartupdeadline_test.go#L127-L157).
- **Run 39379025920, attempt 1, Linux job 118483596858**, failed only `TestStopHomeProcessesWaitsForAScratchRemover` in the integration package during the quality gate. The assertion reported `stopped [pid 12819 ()]`, but that output does not prove a signal reached a live process: process existence and command arguments were read separately, and the cleanup helper ignored an empty or unreadable argument result before classifying the pid as an ordinary worker. The exact interleaving is unknown, so this is not evidence of a production stop failure. The separate automatic diagnostic passed `./internal/cli/run`; it did not rerun the integration test. See the [cleanup helper and regression test](../../integration/preparationworkers_test.go).

## Fixed and checked

Commit `d57976593cfa606dbbe85bd1832274731848a6a6` repaired the first two fixtures: it unlinks before rewriting the installed target and checks the capture store remains intact, and parenthesizes the shell case patterns. Commit `903a35b020bf42c0374da224f6fc4bc06e51f585` makes the fake existence command sleep beyond the whole budget. Commit `0de3f095c7658adc449bb83fc4d29fec308b42a4` removes the misleading setup-inclusive timing assertion while retaining the production late-refusal check and asserts the collected timeout outcome plus the expired-deadline single-probe behavior. The cleanup helper's local candidate rechecks the pid's Linux process start time and current arguments before every signal, drops pids already gone, and returns an error when a live candidate cannot be identified. HOME is reverified while taking the listing snapshot, bracketing the argument read with process-identity reads. A deterministic injected-probe test covers disappearance before and at final signal validation, scratch-remover reclassification, live empty/unreadable argv, pid reuse, ESRCH and other signal errors; the real one-second remover fixture and live-worker termination tests remain. `just format`, `go test -short ./integration -run 'TestStopHomeProcesses' -count=30`, `go vet ./integration`, both darwin-architecture integration compile checks, and targeted Linux/darwin staticcheck pass locally. The full integration suite and a new CI run were not run for this candidate. These are test-harness changes; production Go code is untouched.

The original 0.15.0 eligibility attempt, request 39359972447/1, failed because exact-SHA regular CI was not green; it stopped at eligibility before its prepare/tag jobs ([release-request workflow](../../.github/workflows/release-request.yml)). The later exact-SHA run 39379025920 also failed its Linux quality gate as recorded above. This report makes no publication claim for request 39379046754.

## Limits

Native macOS Bash was not locally available for reproducing the syntax failure. CI provides the observed macOS failure; the source changes are verified in Git. This report adds no runtime behavior or release instructions.
