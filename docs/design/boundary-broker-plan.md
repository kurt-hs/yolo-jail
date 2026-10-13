---
title: "Boundary broker: host request store slice"
status: accepted
stage: BUILT
next: "Deliver required request/answer readiness and rule OQ-BB15 before production writes; combine build/nested verification and pins at integration"
---

# Host request store slice

Written against `689af7ae5`, 2026-10-13. Design: [§7](boundary-broker.md#7-grants-and-the-request-store), [§6.5](boundary-broker.md#65-when-there-is-no-notifier-or-nobody-answers), [BB-D27](boundary-broker.md#BB-D27). Design wins on behavior; the tree wins on fact.

## Settled scope

- One host-only store, serialized by flock; files are private, never jail-mounted.
- Requests bind canonical argv/repository/stdin digest to host-asserted jail and broker lifetime. Duplicate pending requests join only within that lifetime.
- First decision wins. Allow once covers one digest for 15 minutes and one use; set grants respect the filed policy ceiling/use cap. Session is capped at 12 hours.
- Check expiry, identity and uses at consumption; revocation takes effect at the next consumption. Keep ended records seven days.
- `yolo approve` lists, allows once or for a duration, denies, and revokes. It refuses in a jail.

## Build

1. Tests first: store decisions, concurrent duplicate requests/consumption, file permissions, policy refusals, lifetime mismatch, expiry, revocation and retention; CLI tests through its registry plus operator output.
2. Implement `internal/brokerstore`, the storage API for a later host broker caller. Request policy is supplied by the host caller, never from jail frames.
3. Add `internal/cli/approve.go`, dispatch/help entries and targeted CLI tests. No config or credential change.
4. Align design status; run targeted Go tests, `just format`, fresh-binary nested verification from a throwaway workspace; commit one coherent slice.

## Fences and deferred work

No production enqueue, no write execution, no notification, no answer ping, no new consent defaults. The current exit-77 write refusal remains true. Effective set classification/policy delivery and broker enqueue/consume/teardown wiring are a later integration slice. Existing scope files/approvals remain intact. Terminal notice waits on [OQ-BB10](boundary-broker.md#OQ-BB10); ping waits on `yolo notify`. Stop and ask for any uncovered security or consent choice. Integrator owns full combined gates and pack-binary repins.

## Completed and deferred

The host store and CLI dispatch are built. Allow once narrows to the shorter of 15 minutes and the resolved ceiling (implementation interpretation of [§5.7](boundary-broker.md#57-permission-sets) and [§7](boundary-broker.md#7-grants-and-the-request-store)). The lifecycle slice adds a fresh random start id to each host broker process, associates authenticated host-service jail ids with that process, ends its store lifetime during graceful teardown, and audits each grant it ends.

Windowed production calls still refuse with exit 77: this work does not file requests, consume grants or execute writes, and grants stored here cannot make a broker write run.

## Shared transaction slice (after `0c72f266a`)

MEASURED: focused store/scope/lifecycle tests and race regressions exercise the shared protocol; production approval execution remains unbuilt.

- `brokerlock.WithTransaction(sourceDir, func() error)` exclusively holds `<sourceDir>/.lock`. `Store.transaction`, actual `brokerscope.Write`, scope `Remove`, and `Sweep` all use it. Launch teardown calls `Remove`; broker teardown calls `Store.End`. Callback code must not call any lock-taking store/scope API for the same directory (including `List`), nor nest transactions.
- `Store.Revalidate` is mandatory immediately before approving in `Decide`; deny/revoke/list need none. The decision narrows the filed policy against today's ceiling and use cap. `Consume` invokes its mandatory callback inside that transaction, immediately before spending. The callback must re-read current host policy, approved scope and live start/jail identity; at consumption it must reclassify and scope-check the actual invocation captured in its closure, not merely the original request. Missing gates and callback errors fail closed. Policy narrowing invalidates existing grants conservatively.
- `brokerscope.ValidateCurrent` is a non-locking gate reader: bounded, no-follow scope read, known schema, exact launch/source/workspace/container/PID identity and repository membership. It is not a policy loader or start-id validator.
- `End` returns exactly the grants durably ended in its transaction, including partial progress on error. Broker teardown audits those records without a preceding `List`; failures are reported as incomplete and retained for retry.

## Host authority and gated handler slice (after `2cb2332b7`)

BUILT AND CHECKED: real host adapters and the actual bounded handler/caller integration.
Production still supplies no required-delivery readiness; every windowed write stays exit 77,
with nothing filed or waiting. No bypass or consent default was added.

- `config.CurrentBrokeredSets` strictly re-reads the host user scope, never a jail policy frame.
  Malformed entries are not skipped. The two default named sets support mode/window/ceiling/use
  bounds. Explicit `admit`, including empty, and custom sets refuse the entire policy until
  [OQ-BB15](boundary-broker.md#OQ-BB15) settles replacement versus augmentation and a rule grammar
  is supplied. No flag/API-pattern syntax was coined.
- `ghbroker.BindAuthority` binds a fresh start and broker PID to the launch scope using the shared
  transaction. A private flock lease proves the actual start is live even if a PID is recycled.
  `Authority.Close` invalidates it under that transaction; process death releases it too.
  Normal close removes its own lease file; abnormal death can leave an inert lease file, never
  usable authority. Abnormal-exit lease-file collection is not built.
- Both the default `yolo approve` registry caller and broker consumption use the real adapter.
  It checks holder/workspace/start, host launch path, current approved identity and each actual
  repository, current classification and canonical argv. Canonicalization normalizes the
  explicit repository flag before hashing; stdin is bound by digest, never stored as bytes.
- `Broker.ServeContext` is the production handler, not a test-only engine. RequiredDelivery is
  an explicit dependency that production leaves nil. It must honor context cancellation and
  deliver requests idempotently by request id, with notice and later answer capability ready.
  Fake delivery/executor tests pass through `Forward`, the real TLS front, the hostservice connection handler (`Broker.Handle`)
  and this handler. `yolo gh status/wait <request>` frames are wired and holder-scoped.
- `WithTransactionContext`, `FileContext`, `StatusContext` and `ConsumeContext` bound lock
  acquisition. A test-first regression held the real shared flock and reproduced an unbounded
  wait; it now exits on deadline without executing. Disconnect/stop cancel a caller's wait,
  not the persistent pending request. Decisions after that wait execute nothing.
- Store request/decision/use hooks run inside the shared transaction. `brokeraudit.Required`
  syncs authorization evidence and propagates failure. Consume persists its use first; required
  audit failure then runs nothing and never refunds. Standing-write policies also pass the
  required-delivery and in-transaction authority/audit fence. Existing standing reads remain
  best-effort call-audited. Frame type/base64 errors refuse rather than dropping arguments/bytes.

### Exact remaining operations and fences

1. [OQ-BB10](boundary-broker.md#OQ-BB10) launch-terminal notice transport/ruling; no fake manual-discovery substitute.
2. Real notification/request delivery, capability checks, idempotent delivery, answer/expiry
   observer and notification withdrawal; yolo notify ping (or the specified interim status
   delivery contract) must feed the required readiness dependency. No production implementation
   exists and no write is enabled.
3. [OQ-BB15](boundary-broker.md#OQ-BB15) explicit-list semantics, then declared command/flag/API rule evaluation; explicit
   lists and custom sets currently refuse rather than silently widening.
4. Production three-pending bound and ten-minute denial cooldown ([§9.2](boundary-broker.md#92-prompt-fatigue)); abnormal-exit lease
   metadata collection. Store retention/expiry and live-use bounds are already checked.
5. Integrator-owned build-go, isolated nested smoke, combined integration/gates and pack pins.
   Deferred commands: `GOMAXPROCS=2 just build-go`, then from a disposable workspace with
   disposable HOME/XDG config/state/data (shared Go/tool caches preserved), run the resulting
   `dist-go/linux-$(go env GOARCH)/yolo` by absolute path with `YOLO_REPO_ROOT` naming this tree.
   Assert production forwarded writes still exit 77/no request/no API write. Exercise host
   approval list/status/deny/revoke and in-jail host-only refusal against isolated synthetic
   records; never invoke a real GH write. This slice is committed, not fully landed/verified.

Targeted evidence: test-first host adapter/handler compile failures, shared-flock wait regression;
focused race tests include current narrowing/scope removal/lifetime closure, revocation/expiry,
concurrent one-use dispatch, audit failure ordering, hostile frames and unknown/explicit policy.
CLI registry tests use the real host adapter and required decision audit. Shared transaction
review approved the base slice with no blockers; its adapter obligations are the ones above.
