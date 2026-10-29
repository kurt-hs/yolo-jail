---
title: "GitHub write approval has a real store path and an unwired production gate"
status: accepted
stage: CURRENT
next: "Implement the bounded Stage 2 path in github-write-approval-plan.md"
tags: [research, github, security, approvals]
summary: "Source audit for wiring GitHub write approval through the existing host request store, broker front, and yolo approve CLI."
---

# GitHub write approval has a real store path and an unwired production gate

Audited the code at `b8a32ac0ff0a06d417ed5725a1e62239afd505e0` on 2026-10-28. This is source
research, not a live GitHub check: the proposed consumer-path test uses a fake host `gh` and makes
no GitHub API calls. The bounded direction and source map live in the
[design](../design/github-write-approval.md) and [implementation plan](../design/github-write-approval-plan.md).
The existing policy authority remains [`boundary-broker.md`](../design/boundary-broker.md); this
page does not replace or complete its unfinished resource-grant policy.

## The caller and the refusal

| Boundary | Current implementation |
| :--- | :--- |
| Jail caller | `ghbroker.Forward` sends canonicalizable argv, an origin repository, and stdin bytes (base64) through the published endpoint; [`internal/ghbroker/forward.go`](../../internal/ghbroker/forward.go). The real front can be exercised with `svcendpoint.ServeFront` and `hostservice.ServeFrontedUnix`; `TestForwardThroughTheFront` already does so. |
| Production daemon | `ghbroker.Main`'s `NewHandler` creates `newBroker`, binds the launch scope, and supplies the required audit hook; [`internal/ghbroker/daemon.go`](../../internal/ghbroker/daemon.go). `newBroker` leaves `Broker.delivery` nil. `Broker.window` refuses before request filing unless a delivery reports ready; [`internal/ghbroker/window.go`](../../internal/ghbroker/window.go). Therefore the actual caller still answers exit 77. |
| Test-only path | `windowFixture` installs `fakeDelivery` and a fake executor, exercising useful lock, wait and audit cases but bypassing production readiness; [`internal/ghbroker/window_test.go`](../../internal/ghbroker/window_test.go). It cannot establish production writes work. |
| Host request authority | `brokerstore.Store` persists requests and grants and serializes decisions/spend; `runApprove` registers the host `yolo approve` CLI with `ghbroker.ApprovalAuthority` and required audit; [`internal/brokerstore/store.go`](../../internal/brokerstore/store.go), [`internal/cli/approve.go`](../../internal/cli/approve.go). Requests already expose listing and watch JSON. |
| Existing request hook | `brokercore.Daemon.Main` starts `StartOnRequest` with the configured host hook and store; [`internal/brokercore/daemon.go`](../../internal/brokercore/daemon.go). The hook notifies only; the human decision stays on `yolo approve`. |
| Audit | `brokerstore` calls a required audit callback while the shared store transaction is held; `brokercore.StoreAudit` records request, decision and spend events. It currently omits command/stdin digests from those audit rows; [`internal/brokercore/audit.go`](../../internal/brokercore/audit.go), [`internal/brokeraudit/brokeraudit.go`](../../internal/brokeraudit/brokeraudit.go). |

The policy already describes a delivery-gated handler as built, while its production constructor
has no delivery. The classifier and authority still read legacy permission sets. Most importantly,
`ConsumeDispatchContext` currently treats an empty grant digest as matching every command in its
set; that is compatibility behavior for the old set-wide store model, not valid Allow-once
coverage. Filtering only `yolo approve` would leave the executor authorization vulnerable to old
persisted rows. The GitHub handler must opt into an exact-grant spend selector in the shared
store; changing only the host approval CLI would not protect against old on-disk grants. Other
broker sources must keep the store's existing set-wide behavior.

## The host action is already a supported interface

The required building blocks exist: a canonical parsed request; `brokerstore.CommandDigest`, which
hashes canonical argv, resolved repository and stdin digest; locked approval and consumption;
`yolo approve <id>` as the host-side Allow-once action; a bounded caller wait/status response; the
existing background daemon log; optional `on_request` host command; and `yolo approve --watch --json`.
The trusted hook may invoke host `yolo approve`, but its output or exit status is not consent. Without
a hook, the background log and manual host CLI still let a human decide. No new HTTP approval
endpoint, terminal notice, user-file alias or pasted host command is needed. Unanswered or denied
requests never run `gh` or retry automatically.

The code's default classification gives a write the legacy `read-write` windowed outcome. User
configuration can declare a set `standing`, and the legacy `read-write` set can be configured that
way. This is a production vulnerability in the next phase: the outer dispatcher attempts to coerce
its initial write decision back to windowed, but `Broker.window` reclassifies it. When that second
classification is standing, the function takes its separate standing authorization/dispatch path,
which does not consume an exact grant. The missing delivery currently makes this branch unreachable
from production writes; enabling delivery alone would expose it. Reject a standing legacy
`read-write` policy for writes before filing or execution, while preserving ordinary standing reads
from the base classifier. Do not repair this by changing user config or adding a new grant schema.

## Measured red reproduction

Added `internal/cli/ghbrokerapproval_test.go`, a consumer-path regression test. It starts the actual
`ghbroker.Main` daemon in a subprocess, connects `ghbroker.Forward` through the real service front,
and provides only recording fake host `gh`/`on_request` commands. The test contains the host
`cli.Main` `yolo approve` step once a request has been filed; the current red baseline returns before
that step is reached. No production seams are set and no interactive agent or network API is
invoked. The targeted run is
captured outside the repository at
`/workspace/.yolo/durable/github-write-approval-item5/stage1-red-regression-05.txt` (do not commit
that log). Both PR-comment and issue-close attempts exit 77 before a request exists. The standing
policy case's ordinary standing read succeeds, while its write gets the current generic exit-77
host-run handoff rather than the required policy-specific refusal. The assertions are intentionally
for later green approval flow and explicit refusal behavior; this is measured-red evidence, not a
passing feature test.
