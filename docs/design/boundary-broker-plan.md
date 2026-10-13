---
title: "Boundary broker: host request store slice"
status: accepted
stage: BUILT
next: "Integrate resolved host policies, enqueue/consume/teardown, audit and shared scope locking; keep broker writes refused until complete"
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

The host store and CLI dispatch are built. Allow once narrows to the shorter of 15 minutes and
the resolved ceiling (implementation interpretation of [§5.7](boundary-broker.md#57-permission-sets) and [§7](boundary-broker.md#7-grants-and-the-request-store)). This lifecycle slice adds a fresh random start id to each host broker process, associates authenticated host-service jail ids with that process, ends its store lifetime during graceful teardown, and audits each grant it ends.

Windowed production calls still refuse with exit 77: this slice does not file requests, consume grants or execute writes, and grants stored here cannot make a broker write run. Before production request/consume wiring, both real writers must use the same transaction lock: `brokerscope.Write` (including its create/replace of approved launch scope) and every `brokerstore` mutation/consume path. `brokerlock.WithTransaction(dir, fn)` holds the single store flock while the caller re-reads current host policy and approved scope and makes its write/consume decision. `Store.Consume` runs its required revalidation callback inside its transaction immediately before matching and spending; scope writes invoke the same lock before replacing the scope record. The gate must reclassify and scope-check the actual invocation, not only the original request. Do not take same-directory lock-taking APIs from inside the callback.

Bounded wait/status and notification integration remain unbuilt. This work does not solve [OQ-BB10](boundary-broker.md#OQ-BB10) or build `yolo notify`; terminal notification and ping remain excluded.
