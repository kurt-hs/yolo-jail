---
title: "Implementation plan: production GitHub Allow once"
status: accepted
stage: DECIDED
next: "Wait for the lead's Stage 1 review and authorization; then implement tasks 1–7"
tags: [implementation-plan, github, approvals, security]
summary: "A source-grounded hand-off to wire one exact host-approved GitHub write without changing other broker sources or enabling standing writes."
---

# Implementation plan: production GitHub Allow once

This is the implementation hand-off for [`github-write-approval.md`](github-write-approval.md).
The policy authority remains [`boundary-broker.md`](boundary-broker.md), especially BB-D76/77/79,
BB-D98 and BB-D99. Its timed-resource and working-set Add answers stay unfinished backlog. The
[research note](../research/github-write-approval.md) records current production wiring and the
measured-red caller test. **Do not start production edits until the lead accepts Stage 1.**

## Contract and fence

- Wire only exact, one-use, 15-minute Allow once for accepted GitHub writes, using the existing
  host request store and `yolo approve <id>`. Preserve the authenticated forward/front, classifier,
  read behavior, scope checks, runner isolation and all host-only GitHub credentials.
- Put an exact-grant selector in the shared broker store and opt the GitHub path into it. A legacy
  empty-digest GitHub grant must never match. Do not globally change set-wide store semantics or
  the git broker's independent grant model.
- Reject a configured standing legacy `read-write` policy for writes before request filing or
  dispatch. Name the `brokered.github.sets` repair step; do not edit or migrate user config and do
  not add another grant/config mechanism. Keep standing reads unchanged.
- Preserve the exact argv/repository/stdin-byte-digest binding from classification through request,
  decision, spend, audit and execution. Audit digests and IDs, never stdin contents or credential
  material; required request/decision/spend audit remains before dispatch.
- Reuse `brokercore.Daemon.Main`'s existing `StartOnRequest`/`on_request` path and host broker log;
  wire a real request notice plus host-CLI answer path into production readiness. No HTTP approval
  endpoint, terminal-startup notice, launcher warning, host-run fallback or interactive test.
- A pending request is visible in host `yolo approve` / `--json` / `--watch --json`. Deny, silence,
  delivery/audit failure and lost responses never run or automatically repeat a write. After an
  unanswered bounded wait, the agent may reissue only the exact action if the host still wants it.

## Stage 1 RED evidence (complete)

The current in-tree consumer-path tests are `TestProductionGitHubWriteApprovalFlow` and
`TestProductionStandingWritePolicyRefusesWritesAndKeepsReads` in
[`internal/cli/ghbrokerapproval_test.go`](../../internal/cli/ghbrokerapproval_test.go). They start
production `ghbroker.Main` in a helper process, route `ghbroker.Forward` through
`svcendpoint.ServeFront`, use the actual `cli.Main` `yolo approve` path, and run only a recording
fake host `gh`. No private delivery seam is installed. The first expects these exact actions and
host receipts after Allow once:

| Case | Forward argv | Expected fake-host receipt |
| :--- | :--- | :--- |
| PR comment | `pr comment 41 --repo o/r --body approved-comment` | `pr comment --repo=o/r --body=approved-comment 41` |
| Issue close | `issue close 7 --repo o/r` | `issue close --repo=o/r 7` |
| Standing-policy control read | `pr view 41 --repo o/r` | ordinary standing read returns `fake GitHub receipt` |

The standing-write test sets only its temporary user's `brokered.github.sets` to
`read-only: standing` and `read-write: standing`. It requires the read above to run, but the write
`pr comment 41 --repo o/r --body must-not-run` to return `ghbroker.ExitNoPerm` with both
`brokered.github.sets` and `remove` in the explanation; no request is filed and no comment reaches
fake `gh`. This does not alter any user config.

Exact RED command:

```sh
go test ./internal/cli -run '^(TestProductionGitHubWriteApprovalFlow|TestProductionStandingWritePolicyRefusesWritesAndKeepsReads)$' -count=1
```

The command's expected current non-zero result is captured outside the checkout at
`/workspace/.yolo/durable/github-write-approval-item5/stage1-red-regression-03.txt`; do not treat it
as a green feature test. Both write cases presently exit 77 before filing a request. The read
control succeeds, but the standing-policy write gets the generic missing-delivery/host-run refusal
instead of the required policy-specific refusal. The test must fail if production delivery wiring
is removed.

## Stage 2 tasks (ordered)

### 1. Close the standing-write escape before enabling delivery

In [`internal/ghbroker/authority.go`](../../internal/ghbroker/authority.go), reject a write that
resolves from the legacy `read-write` set to `OutcomeStanding`, with an actionable refusal naming
`brokered.github.sets` and telling the host user to remove or make that entry non-standing. Make the
initial classification fail closed, so `ServeContext` cannot route it into `window` and let its
second classification take the standing dispatch branch. Remove/update stale comments in
[`internal/ghbroker/daemon.go`](../../internal/ghbroker/daemon.go) that describe a standing write as
safe. Change `TestHandlerSerializesScopeRemovalThroughStandingAndWindowedDispatch` in
[`internal/ghbroker/dispatch_transaction_test.go`](../../internal/ghbroker/dispatch_transaction_test.go)
from proving a standing write dispatch into proving it is refused, no fake executor call occurs,
and ordinary standing reads remain covered by existing classifier tests and the production control
above. Do not refactor or weaken base read classification.

### 2. Add an opt-in exact-grant spend selector

In [`internal/brokerstore/store.go`](../../internal/brokerstore/store.go), preserve the current
default spend semantics for other sources but add an explicit exact-grant consume path. It must
ignore empty-digest set grants, match the grant digest to the invocation digest and require the
approved parent request to carry that same digest and set/workspace. Revalidate the parent and
actual invocation as today; a CLI-only filter is insufficient because persisted rows are consumed
here. Do not clamp or rewrite parent `Policy`, grant expiry or other metadata to force once/15m:
the existing exact-Allow-once issuance already provides that validity, and existing BB-D99 exact
rows remain live across restart. Exclude only empty-digest GitHub set-wide rows; retain the shared
store's default selector for other sources. Keep lock ordering, durable spend and required audit
before dispatch. Add source-named tests in [`internal/brokerstore/store_test.go`](../../internal/brokerstore/store_test.go)
for exact/non-exact selection, parent/invocation mismatch, concurrent spend and expiry/restart; keep
existing tests proving non-GitHub set grants still work.

### 3. Bind required store audit to the exact action and use

Update [`internal/brokercore/audit.go`](../../internal/brokercore/audit.go) and the narrow
[`internal/brokeraudit`](../../internal/brokeraudit) event shape only as required so request,
decision and `grant-used` records carry canonical argv/repository, command digest, stdin SHA-256,
request ID, grant ID and outcome. Never persist stdin bytes, tokens, auth headers or a substitute
secret. Keep transaction-required audit failure fail-closed and do not refund a spent use. Add tests
that show a consumed grant-use record links to its exact approved request/action and that stdin
mutation changes the digest without logging the body.

### 4. Wire actual GitHub request delivery and decision surfaces

In [`internal/ghbroker/daemon.go`](../../internal/ghbroker/daemon.go) / `newBroker`, install the
production `RequiredDelivery`; do not edit only the fake fixture. Use a real host-log request notice
and the existing host `yolo approve` store path. `Ready` must describe those real production
capabilities. Keep the already-configured `StartOnRequest` daemon hook as an optional notifier; it
must not approve. A request/log/delivery error leaves the request inspectable, returns its ID and
host `yolo approve <id>` step, and never dispatches.

In [`internal/cli/approve.go`](../../internal/cli/approve.go), reject `--for` and `--add` for pending
GitHub writes with the plain `yolo approve <id>` Allow-once next step. Do not alter these flags for
other broker sources, fabricate a timed/Add grant or silently coerce them. Refresh stale usage,
empty-state and decision messages so a user can distinguish a pending request from an old
set-wide grant. Tests must pin source-specific refusal and unchanged other-source behavior.

### 5. Prove the complete production caller, deny and no-notifier behavior

Finish the new CLI tests so the real front/daemon/host CLI flow approves both actions, checks the
exact fake-`gh` argv and receipt, request visibility, action/stdin digest and correlated request/grant
audit. Add production-path refusal checks for no approval, deny, expiry/revocation, changed
argv/repository/stdin, legacy empty-digest GitHub grants, standing `read-write` policy, required
audit failure and concurrent one-use calls. Verify one existing exact unspent grant remains valid
across restart and spend still prevents replay. Do not use a private executor or delivery mock to
claim production wiring; callee-only unit tests complement, never replace, this caller proof.

### 6. Update source-facing explanations without changing policy authority

Refresh [`internal/cli/gh.go`](../../internal/cli/gh.go),
[`internal/cli/help.go`](../../internal/cli/help.go),
[`packs/github/briefing/gh.md`](../../packs/github/briefing/gh.md),
[`packs/github/loopholes/github-broker/manifest.jsonc`](../../packs/github/loopholes/github-broker/manifest.jsonc),
[`internal/ghbroker/daemon_test.go`](../../internal/ghbroker/daemon_test.go), and the existing
read-only integration fixture [`integration/githubbroker_test.go`](../../integration/githubbroker_test.go):
remove the claim that all writes immediately exit 77 or that `host-run` is the approval path; do
not make the read-only regression test wait 30 seconds for an unapproved write. The integration
fixture may retain read/refusal coverage; production approved writes are proven by the actual
consumer-path test. Update only the implementation/status table and stale shipping-state paragraphs
in [`docs/design/boundary-broker.md`](boundary-broker.md), not its settled policy or open BB-D76/79
questions. Review every source-named statement with `rg` and the implemented path.

### 7. Validate the shipped caller and scoped change

Run `gofmt`, the affected package tests (`go test -short ./internal/ghbroker ./internal/brokerstore ./internal/brokercore ./internal/cli`), the relevant broker integration test(s), and `just check-ci`. Because production code is under `internal/`, perform the mandatory nested-jail check: `just build-dev-bundle`, then call its emitted launcher by absolute path from a throwaway workspace. Tests use fake host `gh`; make no live GitHub call. Inspect the owner Go cache at its normal path; do not set a private `GOCACHE` override. Never edit or write main. The current host preflight is `podman info --format '{{.Host.Security.Rootless}}'` = `false` and `podman info --format '{{.Host.RootlessNetworkCmd}}'` = `slirp4netns`; the nested jail proves launcher/production wiring only, not a rootless-only or reachability path.

## Proof matrix

| Claim | Proof required |
| :--- | :--- |
| Removing production delivery fails the caller test | Delete only production `RequiredDelivery` wiring in a disposable verification copy or prove the test fails at readiness; retain no destructive mutation in this worktree. |
| Approval authorizes only this action | Real `Forward` through front and `ghbroker.Main`; host `cli.Main` allows it; fake `gh` records byte-exact canonical argv and stdin digest; required audit includes request/grant/action linkage. |
| Standing writes cannot take the standing branch | Actual daemon + temporary host policy has `read-write: standing`; read passes; write gets explicit actionable refusal, no request/dispatch; existing unsafe scope-removal transaction test remains meaningful for approved windowed write. |
| Shared store is safe without regressions | Empty digest does not cover GitHub exact path; existing exact-grant rows remain valid under unchanged parent metadata after restart; other sources still consume intended set grants; concurrent one-use spend is serialized. |
| Failure, replay and restart are truthful | Required failure never executes; an approved unspent exact grant retries after restart once; spent, denied, expired and revoked states never execute. |

## Stage boundary

Stage 1 is research, design, the completed hand-off and the measured-red consumer test only. Keep its
log outside the repository at the named durable path, commit only the Stage 1 source test/docs in the
private worktree, and send the lead a blocking approval request. Stop there. Stage 2 production
changes begin only after the lead replies. Issue 65 remains open as an upstream wait throughout.
