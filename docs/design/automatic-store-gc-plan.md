---
status: draft
stage: SKETCH
next: "After the design is accepted with no open questions, a planner completes this seed against the then-current tree"
depends-on:
  - automatic-store-gc.md#OQ-GC1
  - automatic-store-gc.md#OQ-GC2
  - automatic-store-gc.md#OQ-GC3
---

# Automatic store GC — implementation material surfaced during design

**Status:** 2026-10-28 — seeded during design; not a hand-off, and incomplete until the design is accepted. No implementation or tests were authored in this closeout.

[The design](automatic-store-gc.md) wins on behavior. A planner, not this docs worker, completes the map, reuse survey, traps, build order and test plan after acceptance with no open questions. An implementer must not build from this seed.

## Surfaced seams, not a new tree survey

The frozen evidence already identified these seams; their presence does not authorize edits:

- The capture receipt reader is split across CLI paths, and the [housekeeping exclusion](../../internal/cli/run/housekeeping.go#L215-L239) names a single reader home as a precondition. Check the then-current reader call sites before proposing factoring.
- [Generic supersession](../../internal/capture/gc.go#L163-L198), [admission/publication](../../internal/cli/programcapture.go#L21-L141), [keyed materialization](../../internal/cli/capturematerialize.go#L140-L158), [patched retirement](../../internal/cli/patchedadvance.go#L3417-L3495) and [handed delivery evidence](../../internal/cli/run/forkhanded.go#L228-L273) all surfaced. A common guarded retirement protocol must cover the production callers, not merely a new helper.
- [Markerless enumeration](../../internal/capture/store.go#L447-L473) is separate from a future recovery journal. Check how retirement state, same-key re-admission and fair cursors can survive interruptions without canonical receipt truncation.
- Existing [embedded lease retirement](../../internal/prune/embeddedtrees.go#L175-L263) and [build workspace known-gone checks](../../internal/cli/forkbuild.go#L100-L209) illustrate distinct family authority, not one reusable universal liveness answer.

## Waits and validation stops

- Scheduling packaging/lifecycle depends on [OQ-GC1](automatic-store-gc.md#OQ-GC1). The hourly and one-shot alternatives are not both accepted. Later planning must validate platform registration, upgrade, opt-out/uninstall, restart and user-domain availability without privilege widening.
- Accounting/admission work depends on [OQ-GC2](automatic-store-gc.md#OQ-GC2). Proposed numbers are unmeasured; complete keys alone exclude staging, pending/deleting payload and metadata. No measured physical-reclaim quota exists.
- Pin/migration material depends on [OQ-GC3](automatic-store-gc.md#OQ-GC3). Check exclusion or namespace isolation of old callers and safe rootless ownership cleanup. Neither a version marker nor an attribution grace is a safety proof.

Safety engineering remains a first stop even after policy rulings: verify shared reader/publication/retirement ordering, stable lock infrastructure, disk-full journal failure and bounded fair recovery. The planner must ground these contracts or return an unresolved design gap rather than let the implementer invent behavior. The design's [observable acceptance](automatic-store-gc.md#observable-acceptance-not-current-results) is behavioral intent, not a full test plan or passing result. No additional tree survey or implementation map is claimed here.
