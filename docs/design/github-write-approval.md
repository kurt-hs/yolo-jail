---
title: "One host yes authorizes one exact GitHub write"
status: accepted
stage: DECIDED
next: "Implement the production Allow-once path and its security matrix from github-write-approval-plan.md"
tags: [design, github, approvals, security]
summary: "A bounded implementation slice that turns the existing GitHub broker's production write refusal into a host-approved, exact-command Allow once."
---

# One host yes authorizes one exact GitHub write

**Status:** Settled bounded slice. **Written against:** `b8a32ac0ff0a06d417ed5725a1e62239afd505e0`
(2026-10-28). Source facts and the current exit-77 reproduction are recorded in
[the research note](../research/github-write-approval.md) and
[Stage 1 QA evidence](../research/github-write-approval-qa.md).

> **In short.** A GitHub write runs only after a human approves its exact canonical action through
the host's existing `yolo approve` interface. The grant is one use, expires after 15 minutes, and
cannot be widened by a workspace, a legacy set-wide grant or an agent-controlled response.

**Why it matters.** The host store, CLI and bounded handler already exist, but the production
broker has no required-delivery implementation and currently refuses every write with exit 77.
Enabling a fake or unchecked delivery bit would permit unreviewed use of the host GitHub credential.

**The shape.** The existing `gh` forwarder and authenticated service front carry the action to the
host broker; the request store binds it, the host `yolo approve` CLI records Allow once, and the
broker spends and audits it before running the host `gh`.

**Cost.** Pending requests become actionable; old GitHub set-wide grants stop authorizing writes.
Timed-resource grants and Add remain visible, named unfinished policy work, not removed decisions.

**Start at [§3](#3-the-exact-action-and-the-only-approval-path)** — the action binding and authority boundary.

**Needs your ruling:** None. The bounded behavior below is settled; it does not complete the other
resource-grant answers in the authority document.

**Reads with:** [`boundary-broker.md`](boundary-broker.md) (policy authority, especially
[§5.7](boundary-broker.md#57-permission-sets), [§5.8](boundary-broker.md#58-long-lived-access-and-temporary-access),
[§6.5](boundary-broker.md#65-when-there-is-no-notifier-or-nobody-answers),
[§6.6](boundary-broker.md#66-one-store-several-front-ends-and-a-stable-surface-for-the-ones-yolo-does-not-ship),
[§7](boundary-broker.md#7-grants-and-the-request-store), and [BB-D76/77/79/98/99](boundary-broker.md#15-decision-ledger));
[`github-write-approval-plan.md`](github-write-approval-plan.md) (tree-grounded build hand-off).

---

## 1. Scope and precedence

This design authorizes **Allow once only** for the production GitHub broker. It realizes the
already-settled one-exact-command grant in [BB-D77](boundary-broker.md#BB-D77) and the persistent
workspace holder in [BB-D99](boundary-broker.md#BB-D99). It does not re-specify the GitHub
classifier, widen the repository boundary, alter credential handling, or implement the complete
resource-grant model.

[`boundary-broker.md`](boundary-broker.md) remains the source of policy. Its resource-scoped
timed grants and working-set **Add** are named unfinished work under [BB-D76](boundary-broker.md#BB-D76)
and [BB-D79](boundary-broker.md#BB-D79): this slice neither implements nor deletes them. Until
that later work is done, the host CLI refuses GitHub `--for` and `--add` decisions for new requests
with the next step `yolo approve <id>` (Allow once, without flags). Existing persisted set-wide
GitHub grants, identifiable by their empty command digest, never authorize a production write;
they remain inspectable and revocable. The shared store keeps its existing set-wide behavior for
other sources; the GitHub broker opts into exact-grant selection at its spend boundary. GitHub
reads and current explicit scope/refusal rules are unchanged.

## 2. Actors and state

- **Jail caller:** the existing `yolo gh` forwarder sends the original action through the published,
  authenticated host-service front. Its existing transport token is unchanged; the jail sends no
  GitHub credential, store path or approval decision.
- **Host broker:** the production `ghbroker.Main` handler parses and canonicalizes the command,
  resolves the target repository, and applies current scope and command-specific credential, header and
  host-file refusals before creating a request.
- **Request store:** the existing host-owned, locked store records one exact request. Its command
  digest is over canonical argv, resolved repository and SHA-256 of the exact stdin bytes, including
  empty stdin. No stdin body alias or host-file alias is introduced.
- **Host approver:** the existing `yolo approve <id>` CLI is the only store decision surface. A
  configured `on_request` is a trusted host command and may call that CLI; its stdout, stderr or
  exit status alone is never interpreted as consent. `yolo approve --watch --json` may observe.
  A jail cannot read or write the store or approve itself.
- **Host executor:** the existing isolated host `gh` runner receives only broker-canonical argv and
  the captured stdin bytes; its existing empty working directory, reduced environment, host-file
  placement guard and no-token-to-jail boundary remain in force.

An Allow-once grant is held by the resolved workspace, survives broker/jail restarts, covers exactly
the stored command digest, permits one dispatch and expires 15 minutes after approval. These are the
existing authority rulings, not new configurable defaults. Existing approved exact-grant rows keep
their current parent policy and expiry and remain usable after restart; this slice excludes only
empty-digest set-wide GitHub rows, with no policy clamping or expiry migration.

## 3. The exact action and the only approval path

1. A jail request enters through the real forwarder/front/handler path. The broker repeats current
   host authority and scope checks, canonicalizes the action and computes its digest. The display,
   stored action, consumed action and executed action must denote those same argv, repository and
   body bytes; no request value supplied by the jail can stand in for host identity or consent.
2. If no exact live Allow-once grant covers it, the broker files the request and its required audit
   under the existing store transaction. The production delivery dependency is ready when the real
   background-log path and host CLI answer path are available; a configured `on_request` hook is
   optional. The request ID is written to the broker's existing background log. If configured, the
   trusted hook may call host `yolo approve`; its output is not a decision. Without a hook, the user
   can still find and decide the request with manual host `yolo approve`. No test-only ready bit is
   used.
3. The host human approves through `yolo approve <id>`. Decision audit and grant creation use the
   existing shared transaction. The host may also deny; denial never dispatches. A successful
   Allow once creates a workspace-held, digest-bound, one-use grant for 15 minutes.
4. Within the existing bounded forward/wait window, a newly approved exact action may spend the
   grant and return the host `gh` result through the existing response. If that window has returned
   pending status, a later agent attempt must reissue the exact action; the broker does not wake an
   agent through a new port and never retries a non-idempotent write automatically.
5. At dispatch, the existing shared store lock serializes grant selection, current authority
   revalidation, durable one-use spend and required grant-use audit before execution. Only after all
   required authorization evidence is committed may host `gh` run. A spent use is never restored,
   including after audit, executor or caller failure.

A legacy host policy that marks the old `read-write` set `standing` is unsupported for writes and
must be refused before request filing or dispatch, with a next step naming the host config entry to
remove or make non-standing. The broker must not reinterpret that policy into consent or execute
through a standing branch. This is a safety check, not a policy/config migration. Standing reads
keep their current behavior: ordinary read commands still run under the classifier's standing
read-only decision.

For this GitHub broker path, the spend selector accepts only a live grant whose non-empty digest
is equal to both the actual command digest and its approved parent request's digest. The stored
parent request must be the grant's exact approved action, and the actual invocation must independently
pass current repository scope and policy checks. A wider or changed action cannot borrow the prior
approval. Other broker sources retain their existing grant coverage. Existing exact rows are not
reissued or clamped; only empty-digest GitHub set rows fail this exact selector. No write is allowed to be
classified as an unreviewed standing read.

## 4. Requests, refusals and operator evidence

A pending request remains visible in the existing host `yolo approve` list and machine-readable
`--json`/`--watch --json` views. The broker background log records its request ID and an actionable
`yolo approve <id>` next step without exposing workspace paths, repository names, body bytes or
credentials. A configured trusted host `on_request` command may notify or call `yolo approve`; only
the store decision written by the CLI counts, never hook output. Without a configured hook, the
background-log entry and manual host `yolo approve` remain available. No new desktop notifier,
startup line, terminal notice file, HTTP port or pasted host-run command is added.

| Outcome | Broker behavior and next step |
| :--- | :--- |
| Request filed, no answer yet | Nothing executes. The existing bounded wait returns pending/status; inspect with host `yolo approve` or `yolo approve --watch --json`, then reissue the exact action only if still wanted. |
| Denied, expired, revoked, canceled or no longer in scope | Nothing executes. Report the observed state and name the host action needed to create fresh consent; never silently retry. |
| Request/log, decision, spend or required audit failure | Fail closed. No dispatch occurs. If the grant use was already spent, do not refund it; report the request/grant and repair/re-approve through the host CLI as appropriate. |
| Concurrent exact callers | The existing transaction lets at most one spend the one use. A loser may not execute and must obtain fresh approval; it cannot resurrect the spent grant. |
| `--for` or `--add` requested for a GitHub write | Refuse with `yolo approve <id>` (Allow once) as the available next step; do not translate it into a set grant. |
| Host policy marks legacy `read-write` standing | Refuse this write before filing or dispatch and name the host config entry the user must remove or make non-standing. Do not update config automatically; standing reads remain available. |
| Legacy empty-digest GitHub grant present | It may be listed or revoked, but never matches a production write. No automatic migration, deletion or implied consent. |

Audit records identify the canonical action and its digest, the stdin SHA-256 digest but never its
bytes, the request/grant IDs and the final outcome/exit. Request, decision and spend audit writes
are required and credential-free. Existing brokered-call audit behavior remains; no credential
material, stdin body or host path becomes an audit substitute for consent.

## 5. Security invariants retained

- The jail receives neither credential material nor an approval interface; all authority-bearing
  store operations remain host-side.
- Every command still passes the current classifier and explicit repository-scope checks. Its
  existing specific refusals for direct credential-printing calls, unsafe headers, host-file
  positionals and out-of-scope targets stay before grant lookup and cannot be answered around.
  Other classified safe actions keep their established behavior; this design does not assert a
  blanket refusal of every command a caller might label secret.
- Repository and body mutation changes the digest and therefore cannot spend other consent. The
  parent request, matched grant and actual action are tied together; audit failure or lost response
  cannot refund the one use.
- The host runner stays outside agent-writable workspace/home trees and receives no jail-selected
  executable or environment. Only fake host `gh` executables are used in automated tests.
- User- and workspace-level defaults/config are unchanged; there is no user setup prerequisite to
  manual host approval.

## 6. Done means

A real production-path test proves that an in-scope PR comment and issue close both file visible
requests, wait for a host `yolo approve` Allow once, execute the exact command through a fake host
`gh`, return its output/status, and leave an audit record identifying the action and consumed grant.
Deleting the production delivery wiring must make that test fail. Security tests also prove that no
request/approval, denial, timeout, revoked/expired grant, cancel, audit failure, concurrency
race, old set-wide row, altered argv/repository/body/header, out-of-scope target or standing-write
configuration can dispatch. Standing reads remain unchanged. An approved but unspent exact grant
survives broker/jail restart and permits one exact retry; after spend the same action cannot run a
second time. A retry after restart still passes current authority and scope revalidation.

The scoped Go change and documentation updates are checked with `just format`, `just check-ci`,
the relevant integration tests and `just build-dev-bundle` plus the emitted absolute launcher from
a durable throwaway workspace. The full resource model is not complete at this milestone: timed
resource grants and working-set Add remain backlog under [BB-D76/79](boundary-broker.md#BB-D76).

## 7. Decision ledger

| ID | Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| <a id="GWA-D1"></a>GWA-D1 | **Implementation decision: a request's holder is the broker's own launch, never the connection's crossing identity.** The holder that a request is filed under, and that `Authority.Revalidate`, the standing check, status, wait and the end-of-lifetime refusal compare, is `paths.JailShortHash` of the container named in the scope file the broker bound. The crossing identity is the jail name the host front writes into each connection's preamble: the base name of the directory the launch published its endpoint in, which is `yolo-host-services-<hash>` for a container backend and `yolo-host-services-<hash>-<suffix>` for a macos-user or `yolo host` session, never the bare hash. It only gates the mapping: a crossing identity naming one of this launch's endpoint directories maps to the launch's holder, and any other value is kept unchanged, so a connection through another launch's endpoint files, reads and spends nothing for this one. The audit line keeps the crossing identity. **The bug this fixes:** before it, the broker filed requests under the crossing identity, so in production every windowed write failed with "request holder is not this live host launch" and the standing branch failed with "standing holder mismatch". The tests passed because each one handed the broker the bare hash, including the forwarder test, which published its endpoint in a directory named after the hash. | 2026-10-29 | [§3](#3-the-exact-action-and-the-only-approval-path) | built: `ghbroker.launchHolder`, pinned by `TestWindowThroughRealForwarderHandlerAndStatus` (production directory names) and `TestAnotherLaunchsEndpointFilesAndSpendsNothing` |
