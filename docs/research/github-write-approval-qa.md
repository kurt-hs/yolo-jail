---
title: "GitHub write approval Stage 1 QA evidence"
status: accepted
stage: CURRENT
next: "Append Stage 2 implementation and combined-tree validation evidence as it is completed"
tags: [qa, github, approvals, evidence]
summary: "Recorded Stage 1 baseline and intentionally red production-caller evidence for exact GitHub write approval."
---

# GitHub write approval Stage 1 QA evidence

**Written against:** `b8a32ac0ff0a06d417ed5725a1e62239afd505e0` (2026-10-28). The private worktree
and `main` both had this commit at task admission; Stage 1 did not write to `main`. The governing
rulings remain [`BB-D77`](../design/boundary-broker.md#BB-D77) (exact Allow once),
[`BB-D98`](../design/boundary-broker.md#BB-D98) (the host request affordance), and
[`BB-D99`](../design/boundary-broker.md#BB-D99) (workspace-held, restart-safe grants). This page
records evidence; it does not replace those decisions.

## Stage 1 checks

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

The tracked source map, future exact-only window-test correction, conditional completion census,
full final integration suite, durable-workspace nested bundle check, and clean `just done` gate are
specified in the [implementation plan](../design/github-write-approval-plan.md). Record final Stage 2
commands and outcomes here after running them.
