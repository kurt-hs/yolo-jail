---
title: "Release CI fixture diagnoses"
date: 2026-10-20
status: open
tags: [lab-report, ci, testing]
summary: "Separates observed CI fixture failures, a launcher regression, and test-harness findings without claiming a release outcome."
---

# Release CI fixture diagnoses, 2026-10-20

A [lab report](README.md). These observations distinguish test-fixture failures from a confirmed launcher regression and an unresolved cleanup-harness observation. They do not claim a release outcome.

## Observed

- **Run 39359722948, attempt 1** failed `check-go` and `check-macos`. The Linux test overwrote a possibly read-only capture-store hard link; the Mac fixture used Bash syntax its runner rejected. See the [cached-good fixture](../../internal/cli/cachedgoodrecovery_test.go#L496-L520) and [floor-probe fixture](../../integration/macosuserfloor_test.go#L181-L193).
- **Run 39374929076, attempt 1**, failed a macOS bounded-existence-probe assertion because its fake could answer before the deadline, and a Linux readiness test's setup-inclusive stopwatch (264.7 ms against 180 ms) although its late refusal assertion passed. These were fixture defects, not evidence that production exceeded either deadline. See the [probe test](../../internal/cli/stopgone_test.go#L288-L314) and [readiness test](../../internal/cli/run/perjailstartupdeadline_test.go#L127-L157).
- **Run 39379025920, attempt 1, macOS job 118483596973**, failed only `TestAFailedBackgroundRefreshRefusesWhenTheRetryFails`. After `YOLO_USE_INSTALLED` handled a failed background refresh, the launcher still checked whether another refresh was due and announced/scheduled one; the test then found the failed-result file still present (`err == nil`). This is a real sequencing regression, not a timeout or native-test compile failure. See the [generated-launcher test](../../internal/entrypoint/prelaunchrefreshtiming_test.go#L334-L373) and [refresh caller](../../internal/entrypoint/prelaunchrefresh.go#L613-L628).
- **The same run's Linux job 118483596858** failed `TestStopHomeProcessesWaitsForAScratchRemover`, reporting `stopped [pid 12819 ()]`. That output does not prove a signal reached a live process: the old helper read existence and arguments separately, then treated empty or unreadable arguments as an ordinary worker. The exact interleaving is unknown. See the [cleanup helper](../../integration/preparationworkers_test.go).

## Fixed and checked

Commits `d57976593cfa606dbbe85bd1832274731848a6a6`, `903a35b020bf42c0374da224f6fc4bc06e51f585` and `0de3f095c7658adc449bb83fc4d29fec308b42a4` repair the cached-link, shell-pattern, bounded-probe and readiness fixtures above.

Commit `6ddb485188c638a9586469ae023f9e5a6e2cc0cc` fixes the launcher sequence: when the installed-version hatch handles a failed background result, that invocation returns before due checks can queue a replacement refresh. This follows the existing invocation-scoped contract in [PD-NF1 and PD-NF2](../design/program-delivery.md#decision-ledger); it does not change the default refusal, future launches or `agent_updates` behavior. The new generated-launcher assertion failed before the fix and passed 25 repetitions after it. On the final combined source, `just check-ci` passed, including Linux/Darwin vet and staticcheck; the failed historical macOS CI test has not been rerun natively.

Commit `010ab75ccc8a6b83e076051c38468c4226b1b724` adds two cleanup-helper safety fixes and permanent tests: a live scratch remover surviving the bound is an error without a signal, and a live process whose current argv is empty cannot inherit stale scratch-remover classification. Existing PID-identity, HOME, disappearance and signal-error checks remain. The helper reports uncertain live processes rather than signalling them. In the final local full integration run, `TestAJailRunsAPackBinaryItDownloaded`'s cleanup reported a live pid with empty argv because the process listing could not reverify its HOME. The exact interleaving is unknown; one focused rerun passed, which is not proof the full-suite failure is resolved.

A separate fixture omission caused an earlier full-suite failure: `TestHostFloorInstallsTheVendorsRelease` selected host commands without opting into `host_management: "own"`. Commit `6ac7df376e2674715cc9ab844836bec261d6b0e1` adds that explicit opt-in; the focused vendor-host test passed. A subsequent full integration run on that source still failed in the cleanup test above.

## Limits

Native macOS execution has not been repeated. The final full integration suite is **not green**; it failed at the cleanup observation above. The earlier two local full-suite runs used a stale isolated checkout at `6ddb485188c638a9586469ae023f9e5a6e2cc0cc` and therefore lacked both `010ab75`'s helper changes and `6ac7df3`'s fixture opt-in. The original 0.15.0 eligibility attempt, request 39359972447/1, stopped because exact-SHA CI was not green ([workflow](../../.github/workflows/release-request.yml)); no publication claim is made for request 39379046754.
