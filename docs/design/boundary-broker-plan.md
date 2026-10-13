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
the resolved ceiling (implementation interpretation of §5.7 and §7). No production broker
caller exists. Before adding one, policy and scope must be revalidated at use; scope-file
mutations must join the store lock; teardown and audit events must be wired. Bounded wait,
status and notification integration remain unbuilt. This slice does not solve OQ-BB10 or build
`yolo notify`, and grants stored here cannot make a broker write run.
