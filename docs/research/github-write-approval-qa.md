---
title: "GitHub write approval Stage 2 QA evidence"
status: in-progress
stage: CURRENT
next: "Root to inspect the private test/documentation successor and control any landing"
tags: [qa, github, approvals, evidence]
summary: "Records historical Stage 1 and Stage 2 evidence, the end-review block on f94aa9b, bounded repairs, and the final gates run on the private remediation candidate."
---

# GitHub write approval Stage 2 QA evidence

**Stage 1 baseline:** `b8a32ac0ff0a06d417ed5725a1e62239afd505e0` (2026-10-28). Stage 1 did not write to
`main`. The end-review candidate was `f94aa9b13534b998aa3d77200a4488369b3adf74`; the approved root
commit `d159a57f03310f3da5ed49dda7546763361e9a38` is its ancestor. This page records evidence; it does
not replace the policy decisions. The governing rulings remain [`BB-D77`](../design/boundary-broker.md#BB-D77)
(exact Allow once), [`BB-D98`](../design/boundary-broker.md#BB-D98) (the host request affordance), and
[`BB-D99`](../design/boundary-broker.md#BB-D99) (workspace-held, restart-safe grants).

## Stage 1 checks (historical)

| Command / evidence | Result |
| :--- | :--- |
| `go test ./internal/cli -run '^(TestProductionGitHubWriteApprovalFlow|TestProductionStandingWritePolicyRefusesWritesAndKeepsReads)$' -count=1` | **Expected RED** against the unmodified production path. Output is outside the repository at `/workspace/.yolo/durable/github-write-approval-item5/stage1-red-regression-05.txt`. The daemon still returns exit 77 before filing; no feature success is claimed. |
| `uvx vantage-check docs/research/github-write-approval.md docs/design/github-write-approval.md docs/design/github-write-approval-plan.md docs/research/github-write-approval-qa.md` | Passed with no findings after authoring all four documents. |
| `git diff --check` | Passed; no whitespace errors. |
| `gofmt -d internal/cli/ghbrokerapproval_test.go` | Passed with no output. |
| Lead-provided baseline: `go test -short ./internal/ghbroker ./internal/brokerstore ./internal/brokercore ./internal/cli` | Passed before the Stage 1 edits; capture at `/workspace/.yolo/durable/github-write-approval-item5/lead/baseline-tests.log`. This is not a green result for the new tests. |
| `go env GOCACHE` | `/home/agent/.cache/go-build`; left at the standard owner cache, with no private override. |
| Host preflight: `podman info --format '{{.Host.Security.Rootless}}'`; `podman info --format '{{.Host.RootlessNetworkCmd}}'` | `false`; `slirp4netns`. A nested verification can establish launcher/wiring only; it cannot prove rootless-only or reachability behavior. |

The Stage 1 caller test routes production `ghbroker.Main` and `ghbroker.Forward` through the real
service front and uses fake host `gh` / fake `on_request` only. The expected failures were intentional:
comment and close exited 77 before request filing, while the read-control succeeded and the standing-write
policy received the generic refusal instead of its policy-specific refusal. No GitHub API call,
credential change, user-config edit or production implementation was performed.

## Stage 2 candidate checks (historical, before the end-review block)

These are the captures and claims recorded for the original private candidate. They are not proof that
its end-review findings were fixed; the independent review below takes precedence on that point.

| Command / evidence | Result |
| :--- | :--- |
| `go test -short -count=1 -timeout 0 ./internal/brokerstore ./internal/brokercore ./internal/ghbroker ./internal/cli ./internal/cli/run` | Passed on the original Stage 2 candidate. |
| `go test -count=1 -run '^TestProductionGitHubWriteApprovalFlow$' ./internal/cli` | Passed on the original candidate's daemon/front/host CLI fixture; exact argument boundaries and non-empty stdin were not established. |
| `go test -count=1 -run '^TestGitHubBrokerRunsAReadAgainstTheHostGH$' ./integration` | Passed on the original candidate. |
| `python3 scripts/completion-census.py` | Passed on the original candidate; historical output updated the reader inventory. |
| `just check-ci` | The original QA recorded a clean-candidate pass after an earlier dirty-stamp attempt. The end-review exposed unrelated-to-that claim gaps in the production proof. |
| `go test -count=1 -timeout 0 ./integration` | Historical clean-candidate result: `ok github.com/mschulkind-oss/yolo-jail/integration 777.215s`. Not treated as new remediation evidence. |
| `just build-dev-bundle` and absolute launcher from the original stage2 workspace | Historical nested preflight only; not treated as validation of this remediation. |
| `just done` | Historical original-candidate result. A fresh run is required after this evidence update is committed. |

## Frozen end-review on `f94aa9b`

The immutable reviewer report is
`/home/agent/.pi/.yolo-sessions/8745c373ab70c66972ffc00cb09e21a6/agent/sessions/--workspace--/2026-10-28T19-20-01-098Z_01a183f4-7f4a-77f6-b01d-3bfac1df18fa/9d0bffe2-5280-4cd9-97a1-c7f7cbc7af14/run-0/subagent-artifacts/6f8a70ef-6202-4f43-aa9a-1739d9a3cd9e_reviewer_0_output.md`.
It verified clean `f94aa9b13534b998aa3d77200a4488369b3adf74`, no edits, and exact ancestry from
`d159a57f03310f3da5ed49dda7546763361e9a38`. Its verdict was **BLOCK** on three P1 findings and two
P2 findings. Its measured canceled-dispatch audit reproduced request/decision/grant-used writes but no
final call event. That captured block is preserved; this remediation does not rewrite the reviewed
candidate or its RED/history.

## Bounded remediation and evidence (2026-10-29)

The private commits are:

- `5eff5aca0cf4e956c76c2505201cd9caca58fc59` (`fix(github-broker): preserve cancellation audit and action identity`)
- `5a5ad6c990e010968496da898f4883f9a52daff3` (`test(github-broker): pin target and body replay refusals`)
- `64c065b92e0d5fa50b1e9c08776674c21e908d7e` (`test(github-broker): refuse unsafe production headers`)

They are descendants of the exact reviewed `f94aa9b` recovery ref; there is no main edit, push,
deployment, user-config/credential/grant change or real GitHub operation.

| Frozen finding | Remediation / evidence | Disposition |
| :--- | :--- | :--- |
| P1: canceled writes lost final outcome audit (`ghbroker/daemon.go`, `brokeraudit`) | `ServeContext` now writes its final call event with a separate 250ms cancellation-detached context; authorization, runner, spend, and required audits still use the caller context. When cancellation follows a spent dispatch, the terminal event is `canceled`, includes exit/request/grant/command/stdin digests, warns the action may need inspection, and never retries. `TestHandlerCanceledBoundedDispatchReleasesAuthorityLock` asserts the linked final event; `TestHandlerFinalAuditFailureDoesNotRetryCanceledAction` holds the audit lock to force final append failure and asserts one execution and spent grant; `TestWindowRequiredAuditLockHonorsCallerCancellation` keeps required-audit cancellation fail-closed. Logs: `cancel-audit-tests-04.log`, `affected-packages-final-12.log`, `race-cancel-audit-06.log` under `/workspace/.yolo/durable/github-write-approval-item5/remediation/`. | Fixed and verified in unit/race tests. Final outcome remains best-effort: if the bounded append fails, it cannot undo the host action or trigger an automatic retry. |
| P1: host approval omitted approved stdin identity | `RequestView` carries optional `command_digest` and `stdin_digest`; `RequestSummary` now appends both complete digests in a bounded suffix, so live text events and hook `YOLO_REQUEST_SUMMARY` retain action identity despite long argv. It exposes no stdin bytes. `TestProductionGitHubWriteApprovalFlow`, the red-first `TestApproveWatchTextLiveRequestsIncludeCompleteActionIdentity` (isolated summary fixture), `TestApproveWatchTextLiveProductionRequestsIncludeCompleteActionIdentity` (production Forward/front fixture), `TestProductionGitHubHookSummariesDistinguishStdinBodies`, `TestRequestSummaryLeadsWithTheSetAndPreservesDigestsWhenCommandIsLong`, and `TestRequestSummaryRedactsBodyAndHeaderValues` cover initial text/JSON, live text, actual hook environment, differing body digests, long argv, and no raw body/header disclosure. | Fixed; targeted normal tests pass. JSON additions remain optional and backward-readable; no policy or schema meaning changed. |
| P1: production caller proof omitted byte-exact argv/stdin and dispatch count | The real forwarder → service front → `ghbroker.Main` → host `cli.Main` fixture records NUL-delimited argv, actual stdin bytes, exact one-dispatch receipts, required audit linkage and no raw bodies. It covers hook output not being consent, standing/read controls and existing refusals/replay. Successor tests now drive unused-grant body and target mutations, expiry, explicit revoke, broker restart with same-workspace spend and repository/workspace fences, required spend-audit failure, and two concurrent Forward contenders through that same production route. Named tests include `TestProductionUnusedExactGrantSurvivesBodyMismatch`, `TestProductionUnusedGrantExpiryAndRevocation`, `TestProductionExactGrantSurvivesBrokerRestartAndScopeFences`, `TestProductionSpendAuditFailureSpendsWithoutDispatchOrRetry`, and `TestProductionConcurrentForwardersSpendOneExactGrantOnce`. No GitHub API or logged-in host CLI is used. | The previously missing production matrix has successor normal/race evidence and whole-tree / full-integration evidence recorded below. The frozen f94 verdict remains historical and is not erased; Root policy required no new independent reviewer for the later summary-only production delta or this test/docs refinement. | |
| P2: live approval listing contradicted feature / offered unsupported `--for` / printed literal `\\n` | Empty listing now says a windowed GitHub write can file pending Allow once; populated listing qualifies whole-set `--for` as source-supported only; refusal uses a real newline. `TestApproveEmptyListDescribesLiveGitHubRequestPath`, `TestGitHubApproveRefusesSetWideDecisionsButOtherSourcesKeepThem`, and `TestApproveWatchText` cover the live text surfaces and unchanged other-source behavior. | Fixed and covered. |
| P2: QA status called committed evidence outstanding | This page preserves the original f94 end-review block, labels prior gate results by candidate, and records the successor evidence without changing boundary-broker rulings. | The QA page remains `in-progress` only because Root has not yet completed private-candidate inspection/landing; the immutable f94 verdict is historical evidence, not a reviewer-follow-up gate for this bounded delta. |

### Remediation commands and captured outcomes

Every log below is durable under `/workspace/.yolo/durable/github-write-approval-item5/remediation/`;
each records its exact command and HEAD. Source tests used the standard `/home/agent/.cache/go-build`.

| Command | Result / capture |
| :--- | :--- |
| `go test ./internal/ghbroker ./internal/brokerstore ./internal/cli -run '^(TestHandlerCanceledBoundedDispatchReleasesAuthorityLock|TestProductionGitHubWriteApprovalFlow|TestProductionStandingWritePolicyRefusesWritesAndKeepsReads|TestGitHubApproveRefusesSetWideDecisionsButOtherSourcesKeepThem|TestApproveEmptyListDescribesLiveGitHubRequestPath)$' -count=1` | Passed; `targeted-tests-01.log`. |
| `go test ./internal/ghbroker ./internal/brokerstore ./internal/cli -run '^(TestHandlerCanceledBoundedDispatchReleasesAuthorityLock|TestWindowRequiredAuditLockHonorsCallerCancellation|TestProductionGitHubWriteApprovalFlow|TestProductionStandingWritePolicyRefusesWritesAndKeepsReads|TestGitHubApproveRefusesSetWideDecisionsButOtherSourcesKeepThem|TestApproveEmptyListDescribesLiveGitHubRequestPath|TestApproveWatchText)$' -count=1` | Passed; `targeted-tests-03.log`. |
| `go test ./internal/ghbroker -run '^(TestHandlerCanceledBoundedDispatchReleasesAuthorityLock|TestHandlerFinalAuditFailureDoesNotRetryCanceledAction|TestWindowRequiredAuditLockHonorsCallerCancellation)$' -count=1` | Passed; `cancel-audit-tests-04.log`. |
| `go test -count=1 -timeout 0 ./internal/ghbroker ./internal/brokerstore ./internal/brokercore ./internal/brokeraudit ./internal/cli` | Passed after adding the failure-injection test; `affected-packages-final-12.log`. |
| `go test -count=1 -timeout 0 ./internal/cli` after hostile header caller coverage | Passed; `cli-package-after-header-36.log`. |
| Targeted production unsafe header refusal | Passed in `TestProductionStandingWritePolicyRefusesWritesAndKeepsReads`; `api-header-caller-35.log`. The fixture asserts the fake host `gh` receipt does not change. |
| Targeted `go test -race` cancellation/audit, brokerstore/brokeraudit, and exact production CLI caller commands | All passed; `race-cancel-audit-06.log`, `race-store-audit-07.log`, `race-prod-cli-08.log`, `race-caller-final-13.log`, `race-caller-final-26.log`, and `race-header-final-37.log`. |
| `python3 scripts/completion-census.py` | Passed; generated `scripts/completion-readers.json` cleanup is committed; `completion-census-final-14.log`, `completion-census-after-target-27.log`, and `completion-census-after-header-38.log`. |
| `just format` and `git diff --check` | Passed before source commits; `format-15.log` records `just format`. `git diff --check` returned 0 in the clean source diff check. |
| `scripts/vantage-check.sh docs/design/github-write-approval-plan.md docs/research/github-write-approval-qa.md` | First caught the unsupported custom `stage` vocabulary; metadata was corrected to `CURRENT`. Later checks passed, including after the unsafe-header status update; latest capture `docs-vantage-after-header-39.log`. |
| `just check-ci` at source commit `5eff5aca0cf4e956c76c2505201cd9caca58fc59` | First attempt failed only at `internal/cli` `TestBackgroundAdvanceRepairCoordinationIsNoWaitAndConservative/observe`: producer did not reach `repair.observe` within the test's 7.09s wait (`check-ci-final-16.log`). The exact test passed isolated (`flaky-test-isolated-17.log`). The final-candidate run below passed. |
| `just build-dev-bundle` at `5eff5aca0cf4e956c76c2505201cd9caca58fc59` | Passed; absolute launcher `/home/agent/.local/share/yolo-jail/dev-bundles/964b31d4339f87a0-90084bccea1f45fc8466b3d7188ceb23/bin/yolo`; `build-dev-bundle-18.log`. |
| Absolute emitted launcher `check --no-build` from a fresh isolated workspace/home with fake host `gh` | Passed: 17 passed, 3 warnings, 5 skipped; `nested-check-no-build-19.log`. No installed launcher, host config or credential was changed. |
| `podman info --format '{{.Host.Security.Rootless}}'; podman info --format '{{.Host.RootlessNetworkCmd}}'` | Measured `false` and `slirp4netns`; `podman-host-boundaries-20.log`. Nested verification proves only the launcher/preflight path, not rootless-only or real reachability behavior. |
| `go test -count=1 -timeout 0 ./integration` | Passed at `5eff5aca0cf4e956c76c2505201cd9caca58fc59`: `ok .../integration 746.133s`; `integration-full-21.log`. The follow-up commit changed tests only, not production code. |
| `just check-ci` at final source candidate `5a5ad6c990e010968496da898f4883f9a52daff3` | Passed; includes lint, Linux/Darwin vet/staticcheck, short tests and docs checks; `check-ci-final-candidate-28.log`. This is the one passing whole-tree check-ci on the final source candidate. |
| `just done` after this evidence change | **Passed at `27de5719b79586339c77411e5c3ed43b40965719`**; 3 changed paths, clean tree; `just-done-after-header-40.log`. The final documentation-only amendment is followed by a fresh run. |

The full integration and nested runs used rootful podman. They do not prove rootless podman, host-loopback
reachability, macOS, real credentials or live GitHub effects. No additional issue was closed and no
deployment or land/merge claim is made.

## Final caller-proof refinement on `417dc70`

The verified starting candidate `417dc70de05fa26ecccfcee3585172c9b67b2b2e` already contains the
bounded production request-summary fix and the five real Forward/front/daemon/host-CLI lifecycle
cases. This refinement changes only tests and this evidence: it does not alter production source,
policy or dispatch behavior.

- `TestApproveWatchTextLiveProductionRequestsIncludeCompleteActionIdentity` now runs the actual
  host `approve --watch` through the production `approveAll` → `approveWatch` handler beside `newProductionGitHubFixture`, files two differently bodied
  requests through `ghbroker.Forward` → service front → `ghbroker.Main`, observes only the new
  `request-filed` events, and verifies each event's full command/stdin digests without body bytes.
  Host `cli.Main` denies and drains both requests; the fake host `gh` receives no write. The watcher
  signals output changes to synchronize startup and event receipt rather than sleeping for timing.
- The exact-success grant tests additionally compare actual nonempty fake-`gh` stdin bytes and SHA-256
  with the granted request, and require the matching allowed-decision, required `grant-used`, and final
  `call` audit identities exactly once. The restart case now checks the same receipt/audit chain after
  SIGTERM, drain and restart. The concurrent case links the one winner's receipt/spend/call, while
  checking that the loser is an exact-action pending request that is denied, audited and drained with
  no second dispatch. Expiry/revoke cases check the refilled request's command/stdin identity, refusal
  and denial records, the retained exact allow decision, and zero spend/dispatch. The audit-append
  fault case checks the durable consumed grant link, exact attempted action and absence of fake-`gh`
  receipts; the required spend append is necessarily absent because the fixture makes the audit lock
  unwritable. Existing unused-before-consumption body/target mutation checks remain intact.
- The frozen `f94aa9b` independent-review **BLOCK** is preserved exactly as historical review evidence;
  the confirmed findings have successor regressions and the lead verified the bounded production diff
  and normal/race tests. Root policy accepted the small request-summary-only production delta without
  another independent reviewer. This refinement does not claim a new independent review. Root retains
  inspection and private landing authority. Timed-resource/Add remain unfinished backlog.

### Final candidate commands and evidence

| Command / evidence | Result |
| :--- | :--- |
| `go test ./internal/cli -run '^(TestApproveWatchTextLiveRequestsIncludeCompleteActionIdentity|TestApproveWatchTextLiveProductionRequestsIncludeCompleteActionIdentity|TestProductionUnusedExactGrantSurvivesBodyMismatch|TestProductionConcurrentForwardersSpendOneExactGrantOnce|TestProductionExactGrantSurvivesBrokerRestartAndScopeFences|TestProductionSpendAuditFailureSpendsWithoutDispatchOrRetry|TestProductionUnusedGrantExpiryAndRevocation|TestProductionGitHubWriteApprovalFlow)$' -count=1` | Passed on the final focused test/docs tree; exact command, HEAD/status and exit: `/workspace/.yolo/durable/github-write-approval-item5/final-proof/focused-production-cases-normal.log`. |
| Same command with `-race` | Passed on the final focused test/docs tree; exact command, HEAD/status and exit: `/workspace/.yolo/durable/github-write-approval-item5/final-proof/focused-production-cases-race.log`. |
| `go test ./internal/cli -run '^TestProductionConcurrentForwardersSpendOneExactGrantOnce$' -count=5` | Passed; five repeated two-contender production cases; capture: `/workspace/.yolo/durable/github-write-approval-item5/final-proof/concurrency-repeat.log`. |
| `just format && git diff --check` | Passed on the final test/documentation delta; capture: `/workspace/.yolo/durable/github-write-approval-item5/final-proof/format-and-diffcheck.log`. |
| `just check-ci` | Passed once with the final source/test changes, including lint, Linux/Darwin vet/staticcheck, short tests and docs checks; subsequent edits only recorded that result and refined doc wording, then the final two-file doc check passed. Standard Go caches were preserved. Capture: `/workspace/.yolo/durable/github-write-approval-item5/final-proof/check-ci-final-refinement.log`. |
| `scripts/vantage-check.sh docs/design/github-write-approval-plan.md docs/research/github-write-approval-qa.md && git diff --check` | Passed on the final doc wording; capture: `/workspace/.yolo/durable/github-write-approval-item5/final-proof/final-format-and-docs-check.log`. |
| Full slow `go test -count=1 -timeout 0 ./integration` | Passed at exact candidate `417dc70de05fa26ecccfcee3585172c9b67b2b2e`: `766.416s`; capture: `/workspace/.yolo/durable/github-write-approval-item5/successor/lead-final-full-integration.log`. The later refinement is test/docs only; production source is unchanged, so this slow suite was not repeated. |
| Actual emitted-launcher nested jail | Passed at source/test candidate `398ad02298add896acd4aaacddca2f40066efea2`; capture: `/workspace/.yolo/durable/github-write-approval-item5/successor/nested-run-final.log`. The only difference from `398` to `417` was documentation, and this refinement changes tests/docs only, so no second runtime probe was needed. Host was rootful podman (`Rootless=false`, `slirp4netns`): this does not prove rootless-only, host-loopback reachability, native macOS or live GitHub behavior. |

The final targeted commands cover the actual live watch, body/target mutation, expiry/revoke, restart,
audit-write failure and concurrency cases. `just check-ci` runs the short integration package; the
full integration result above is separate, actual evidence at 417, not fabricated from that short gate.
The source remains private and unlanded; no user config, credentials, live GitHub effects or issue
state changed.
