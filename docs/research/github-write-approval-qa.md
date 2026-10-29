---
title: "GitHub write approval Stage 2 QA evidence"
status: in-progress
stage: CURRENT
next: "Complete the remaining production-caller proof matrix and obtain independent review on the private candidate; do not land without separate authority"
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

The private source commits are `5eff5aca0cf4e956c76c2505201cd9caca58fc59`
(`fix(github-broker): preserve cancellation audit and action identity`) and
`5a5ad6c990e010968496da898f4883f9a52daff3`
(`test(github-broker): pin target and body replay refusals`). They are descendants of the exact
reviewed `f94aa9b` recovery ref; there is no main edit, push, deployment, user-config/credential/grant
change or real GitHub operation.

| Frozen finding | Remediation / evidence | Disposition |
| :--- | :--- | :--- |
| P1: canceled writes lost final outcome audit (`ghbroker/daemon.go`, `brokeraudit`) | `ServeContext` now writes its final call event with a separate 250ms cancellation-detached context; authorization, runner, spend, and required audits still use the caller context. When cancellation follows a spent dispatch, the terminal event is `canceled`, includes exit/request/grant/command/stdin digests, warns the action may need inspection, and never retries. `TestHandlerCanceledBoundedDispatchReleasesAuthorityLock` asserts the linked final event; `TestHandlerFinalAuditFailureDoesNotRetryCanceledAction` holds the audit lock to force final append failure and asserts one execution and spent grant; `TestWindowRequiredAuditLockHonorsCallerCancellation` keeps required-audit cancellation fail-closed. Logs: `cancel-audit-tests-04.log`, `affected-packages-final-12.log`, `race-cancel-audit-06.log` under `/workspace/.yolo/durable/github-write-approval-item5/remediation/`. | Fixed and verified in unit/race tests. Final outcome remains best-effort: if the bounded append fails, it cannot undo the host action or trigger an automatic retry. |
| P1: host approval omitted approved stdin identity | `RequestView` includes optional `command_digest` and `stdin_digest`; therefore host text, JSON, watch and configured hook summaries expose the same binding without raw stdin. `printApproveList` labels both digests. `TestProductionGitHubWriteApprovalFlow` sends an actual nonempty `--body-file -` body, verifies hashes and canonical repository/argv, checks text/JSON/hook surfaces contain digests but not the body, and compares fake-`gh` stdin bytes. | Fixed and verified in production caller fixture. Other-source JSON fields are additive/optional; no policy or schema meaning changed. |
| P1: production caller proof omitted byte-exact argv/stdin and dispatch count | The real forwarder → service front → `ghbroker.Main` → host `cli.Main` fixture now records each fake-host argv argument NUL-delimited, actual stdin bytes, and verifies exactly one approved PR-comment / issue-close dispatch with correlated required audit. It verifies manual approval without a hook, optional hook output alone is not consent, pending calls do not dispatch, standing-write/read behavior, secret `auth token` and out-of-scope refusal, exact replay denial, changed comment-body digest, and changed target digest/argv are new requests and do not dispatch. It never calls GitHub or a logged-in host CLI. | The highest-risk exact caller binding is materially covered, but the full agreed production negative matrix remains **open**: expiry, explicit revoke, broker/jail restart, required spend-audit failure, API-header refusal, and concurrent contenders still rely on component tests and are not claimed as complete production-caller proof. Do not mark the reviewer’s full Task 5 matrix satisfied until those caller-level cases are closed or an independent reviewer accepts narrower evidence. |
| P2: live approval listing contradicted feature / offered unsupported `--for` / printed literal `\\n` | Empty listing now says a windowed GitHub write can file pending Allow once; populated listing qualifies whole-set `--for` as source-supported only; refusal uses a real newline. `TestApproveEmptyListDescribesLiveGitHubRequestPath`, `TestGitHubApproveRefusesSetWideDecisionsButOtherSourcesKeepThem`, and `TestApproveWatchText` cover the live text surfaces and unchanged other-source behavior. | Fixed and covered. |
| P2: QA status called committed evidence outstanding | This page now labels old Stage 2 claims historical, records the exact f94 end-review block, remediation commits, gate logs, remaining production matrix and native/rootless boundary honestly. | Fixed in this page; retain the `in-progress` status until remaining proof and fresh `just done` are handled. |

### Remediation commands and captured outcomes

Every log below is durable under `/workspace/.yolo/durable/github-write-approval-item5/remediation/`;
each records its exact command and HEAD. Source tests used the standard `/home/agent/.cache/go-build`.

| Command | Result / capture |
| :--- | :--- |
| `go test ./internal/ghbroker ./internal/brokerstore ./internal/cli -run '^(TestHandlerCanceledBoundedDispatchReleasesAuthorityLock|TestProductionGitHubWriteApprovalFlow|TestProductionStandingWritePolicyRefusesWritesAndKeepsReads|TestGitHubApproveRefusesSetWideDecisionsButOtherSourcesKeepThem|TestApproveEmptyListDescribesLiveGitHubRequestPath)$' -count=1` | Passed; `targeted-tests-01.log`. |
| `go test ./internal/ghbroker ./internal/brokerstore ./internal/cli -run '^(TestHandlerCanceledBoundedDispatchReleasesAuthorityLock|TestWindowRequiredAuditLockHonorsCallerCancellation|TestProductionGitHubWriteApprovalFlow|TestProductionStandingWritePolicyRefusesWritesAndKeepsReads|TestGitHubApproveRefusesSetWideDecisionsButOtherSourcesKeepThem|TestApproveEmptyListDescribesLiveGitHubRequestPath|TestApproveWatchText)$' -count=1` | Passed; `targeted-tests-03.log`. |
| `go test ./internal/ghbroker -run '^(TestHandlerCanceledBoundedDispatchReleasesAuthorityLock|TestHandlerFinalAuditFailureDoesNotRetryCanceledAction|TestWindowRequiredAuditLockHonorsCallerCancellation)$' -count=1` | Passed; `cancel-audit-tests-04.log`. |
| `go test -count=1 -timeout 0 ./internal/ghbroker ./internal/brokerstore ./internal/brokercore ./internal/brokeraudit ./internal/cli` | Passed after adding the failure-injection test; `affected-packages-final-12.log`. |
| `go test -count=1 -timeout 0 ./internal/cli` | Passed after target/body-mutation caller proof was added; `cli-package-final-25.log`. |
| Targeted `go test -race` cancellation/audit, brokerstore/brokeraudit, and exact production CLI caller commands | All passed; `race-cancel-audit-06.log`, `race-store-audit-07.log`, `race-prod-cli-08.log`, `race-caller-final-13.log`, and `race-caller-final-26.log`. |
| `python3 scripts/completion-census.py` | Passed; generated `scripts/completion-readers.json` cleanup is committed; `completion-census-final-14.log` and `completion-census-after-target-27.log`. |
| `just format` and `git diff --check` | Passed before source commits; `format-15.log` records `just format`. `git diff --check` returned 0 in the clean source diff check. |
| `scripts/vantage-check.sh docs/design/github-write-approval-plan.md docs/research/github-write-approval-qa.md` | First caught the unsupported custom `stage` vocabulary; metadata was corrected to `CURRENT`. The rerun passed, 2 files checked; `docs-vantage-30.log`. |
| `just done` | Passed on evidence commit `137e02ea8ea0209b7e6aaf694b4ccd37a7b0917e`; 11 paths, selective vet/staticcheck/tests/readers/docs checks, then clean tree. Capture: `just-done-initial-31.log`. The final QA-only update is followed by a fresh final `just done` run; its capture is the durable `just-done-final-32.log`. |
| `just check-ci` at source commit `5eff5aca0cf4e956c76c2505201cd9caca58fc59` | First attempt failed only at `internal/cli` `TestBackgroundAdvanceRepairCoordinationIsNoWaitAndConservative/observe`: producer did not reach `repair.observe` within the test's 7.09s wait (`check-ci-final-16.log`). The exact test passed isolated (`flaky-test-isolated-17.log`). The final-candidate run below passed. |
| `just build-dev-bundle` at `5eff5aca0cf4e956c76c2505201cd9caca58fc59` | Passed; absolute launcher `/home/agent/.local/share/yolo-jail/dev-bundles/964b31d4339f87a0-90084bccea1f45fc8466b3d7188ceb23/bin/yolo`; `build-dev-bundle-18.log`. |
| Absolute emitted launcher `check --no-build` from a fresh isolated workspace/home with fake host `gh` | Passed: 17 passed, 3 warnings, 5 skipped; `nested-check-no-build-19.log`. No installed launcher, host config or credential was changed. |
| `podman info --format '{{.Host.Security.Rootless}}'; podman info --format '{{.Host.RootlessNetworkCmd}}'` | Measured `false` and `slirp4netns`; `podman-host-boundaries-20.log`. Nested verification proves only the launcher/preflight path, not rootless-only or real reachability behavior. |
| `go test -count=1 -timeout 0 ./integration` | Passed at `5eff5aca0cf4e956c76c2505201cd9caca58fc59`: `ok .../integration 746.133s`; `integration-full-21.log`. The follow-up commit changed tests only, not production code. |
| `just check-ci` at final source candidate `5a5ad6c990e010968496da898f4883f9a52daff3` | Passed; includes lint, Linux/Darwin vet/staticcheck, short tests and docs checks; `check-ci-final-candidate-28.log`. This is the one passing whole-tree check-ci on the final source candidate. |
| `just done` after this evidence change | **Not yet run**; record its fresh result below before closing this evidence update. |

The full integration and nested runs used rootful podman. They do not prove rootless podman, host-loopback
reachability, macOS, real credentials or live GitHub effects. No additional issue was closed and no
deployment or land/merge claim is made.

## Current candidate status

Current feature source is private at `5a5ad6c990e010968496da898f4883f9a52daff3`; the required
production caller test matrix above is not complete, so the frozen reviewer’s full P1 caller-proof
finding remains open and **candidate handoff remains blocked**. `main` remains at
`d159a57f03310f3da5ed49dda7546763361e9a38`; the full source diff is private and unlanded. A later
review should be the same existing independent reviewer narrowed to the remaining Task 5 matrix, not a
new review panel.
