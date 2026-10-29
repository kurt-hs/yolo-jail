---
title: "GitHub write approval Stage 2 QA evidence"
status: accepted
stage: CURRENT
next: "Run just done from the clean candidate and obtain independent review"
tags: [qa, github, approvals, evidence]
summary: "Recorded Stage 1 baseline and intentionally red production-caller evidence for exact GitHub write approval."
---

# GitHub write approval Stage 2 QA evidence

**Written against:** `b8a32ac0ff0a06d417ed5725a1e62239afd505e0` (2026-10-28). The private worktree
and `main` both had this commit at task admission; Stage 1 did not write to `main`. The governing
rulings remain [`BB-D77`](../design/boundary-broker.md#BB-D77) (exact Allow once),
[`BB-D98`](../design/boundary-broker.md#BB-D98) (the host request affordance), and
[`BB-D99`](../design/boundary-broker.md#BB-D99) (workspace-held, restart-safe grants). This page
records evidence; it does not replace those decisions.

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

The new caller test routes production `ghbroker.Main` and `ghbroker.Forward` through the real
service front and uses fake host `gh` / fake `on_request` only. The expected failures are intentional:
comment and close exit 77 before request filing, while the read-control succeeds and the standing-write
policy receives the generic refusal instead of its policy-specific refusal. No GitHub API call,
credential change, user-config edit or production implementation was performed. The front fixture
places the endpoint under `paths.JailShortHash("approval-fixture")`, matching the scope container's
host-asserted identity; no production identity check was weakened.

## Stage 2 candidate checks (2026-10-28)

| Command / evidence | Result |
| :--- | :--- |
| `go test -short -count=1 -timeout 0 ./internal/brokerstore ./internal/brokercore ./internal/ghbroker ./internal/cli ./internal/cli/run` | Passed after rewriting GitHub timed-grant tests for exact, one-use grants; generic brokerstore behavior remains covered. |
| `go test -count=1 -run '^TestProductionGitHubWriteApprovalFlow$' ./internal/cli` | Passed against production daemon/front/host CLI with fake host `gh`, both with and without the optional hook; hook output alone did not approve. |
| `go test -count=1 -run '^TestGitHubBrokerRunsAReadAgainstTheHostGH$' ./integration` | Passed; this used the real jail test harness and fake host `gh`. |
| `python3 scripts/completion-census.py` | Passed; updated `scripts/completion-readers.json` for the added exact-consume and production-caller tests. |
| `just check-ci` | The first uncommitted attempt hit environment-sensitive failures; retry on the clean candidate passed, including lint and `go test -short ./...`. |
| `go test -count=1 -timeout 0 ./integration` | The first pre-commit run had only the dirty-stamp mismatch; the clean-candidate rerun passed: `ok github.com/mschulkind-oss/yolo-jail/integration 777.215s`. |
| `go test -count=1 -timeout 0 ./integration -run '^TestTheJailsYoloCarriesItsMatchedBundleStamp$'` | Passed after candidate commit; the earlier `.dirty` suffix was caused by testing before the implementation commit. |
| `just build-dev-bundle` + absolute launcher from `/workspace/.yolo/durable/github-write-approval-item5/stage2-nested-workspace` | Passed on the nested podman path with a fake host `gh`; `gh pr view 1` reached fake host `gh` as `pr view --repo=yolo-it/app 1` and returned 0. `yolo check --no-build` validated the temporary user/workspace config. This nested environment is not evidence for rootless-only or reachability behavior. |
| `uvx vantage-check docs/design/boundary-broker.md docs/design/github-write-approval-plan.md docs/research/github-write-approval-qa.md userguide/guides/github.md userguide/reference/cli-reference.md packs/github/README.md packs/github/briefing/gh.md` | Passed: 7 files checked, no findings. |
| `just format` | Passed. |
| Merge of `d159a57f03310f3da5ed49dda7546763361e9a38` | Merged by normal ancestry into this private branch; `main` remains unchanged. |

Still outstanding: final QA-evidence commit, clean `just done`, and independent review. The clean
candidate is commit `a71c83693` atop the merge commit for exact root-approved `d159a57f03310f3da5ed49dda7546763361e9a38`; local `main` remains at that approved commit.
The nested preflight is podman rootful (`false` for `.Host.Security.Rootless`) with `slirp4netns`;
nested evidence does not establish rootless-only or real reachability behavior.
