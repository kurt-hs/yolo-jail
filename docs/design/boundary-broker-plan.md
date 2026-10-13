---
title: "Boundary broker: host request store slice"
status: accepted
stage: BUILT
next: "Supply host authority adapters and bounded request/wait/status/execution; keep broker writes refused until complete"
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

The next approved operations are host policy/scope/start adapter delivery to both the broker and approval frontend, canonical request filing/joining, bounded waiting and status, then revalidate/consume/audit/execute wiring. The standalone approval frontend deliberately has no current-authority adapter yet: synthetic pending records cannot be allowed by `yolo approve` without one; listing, deny and revoke still work. Production brokers still enqueue nothing, consume no grants and return 77 for windowed writes. Bounded wait and execution must be complete before enabling production requests.

Terminal notices remain blocked by [OQ-BB10](boundary-broker.md#OQ-BB10); notification delivery and answer ping remain separate dependencies on the notifier and `yolo notify`. This work resolves neither.
