---
title: "Release CI fixture diagnoses"
date: 2026-10-20
status: open
tags: [lab-report, ci, testing]
summary: "Records three CI test-fixture failures, their causes and narrow repairs without claiming a release outcome."
---

# Release CI fixture diagnoses, 2026-10-20

A [lab report](README.md). These are CI test failures and fixture repairs, not three production regressions. Each failure was reproduced in CI; the fixes below were verified against the committed source ranges.

## Observed

- **Run 39359722948, attempt 1** failed both `check-go` and `check-macos`. On Linux, `TestBuildSlotCachedGoodRunsThroughGeneratedLauncherAndCaptureMaterializer` overwrote a target that can be a read-only hard link into the shared capture store. The fixture needed to unlink its installed target first, isolate its home, and assert the stored bytes stayed unchanged. On macOS, `TestMacosUserFloorProbeSeesTheFloorPastAMiseShim` used an unparenthesized `case` pattern inside command substitution, which the runner's Bash rejected. See the [cached-good fixture](../../internal/cli/cachedgoodrecovery_test.go#L496-L520) and [floor-probe fixture](../../integration/macosuserfloor_test.go#L181-L193).
- **Run 39374929076, attempt 1, macOS job 118470835260**, failed `TestWaitForJailContainerGoneBoundsExistenceProbe`: the assertion expected an unanswered probe, but its fake existence command could return `present` before the 40 ms deadline. The implementation correctly retains a valid presence answer when the overall deadline expires; the fixture had not guaranteed a timeout. See the [bounded-probe test](../../internal/cli/stopgone_test.go#L288-L314).
- **Run 39374929076, attempt 1, Linux job 118470835173**, failed `TestPerJailStartupReasonAfterReadinessDeadlineIsNotAccepted` on its elapsed-time assertion (264.7 ms against 180 ms). The test stopwatch included fixture setup and subprocess startup, while the production readiness deadline starts later. The test had already passed its assertion that the late refusal was not accepted; this was not evidence that production exceeded its readiness deadline. See the [late-reason test](../../internal/cli/run/perjailstartupdeadline_test.go#L127-L157).

## Fixed and checked

Commit `d57976593cfa606dbbe85bd1832274731848a6a6` repaired the first two fixtures: it unlinks before rewriting the installed target and checks the capture store remains intact, and parenthesizes the shell case patterns. Commit `903a35b020bf42c0374da224f6fc4bc06e51f585` makes the fake existence command sleep beyond the whole budget. Commit `0de3f095c7658adc449bb83fc4d29fec308b42a4` removes the misleading setup-inclusive timing assertion while retaining the production late-refusal check and asserts the collected timeout outcome plus the expired-deadline single-probe behavior. These are test-only repairs.

The original 0.15.0 eligibility attempt, request 39359972447/1, failed because exact-SHA regular CI was not green; it stopped at eligibility before its prepare/tag jobs ([release-request workflow](../../.github/workflows/release-request.yml)). A later exact-SHA CI run 39379025920 and request 39379046754 were still in progress in the observed status snapshot. This report does not claim their eventual result or publication status.

## Limits

Native macOS Bash was not locally available for reproducing the syntax failure. CI provides the observed macOS failure; the source changes are verified in Git. This report adds no runtime behavior or release instructions.
