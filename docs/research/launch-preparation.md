---
title: "Launch preparation: reuse the admission pipeline, not the terminal wait"
status: accepted
stage: CURRENT
next: "Use the preparation design and obtain controlled host/rootless/native timings during implementation QA"
---

# Launch preparation: reuse the admission pipeline, not the terminal wait

**Status:** CURRENT. Reconciled against the combined candidate source 2026-10-14.
This is a bounded source audit, not a runtime benchmark.

## Findings and verdict

| Source evidence | Finding | Verdict |
| :--- | :--- | :--- |
| [`treedelivery.go`](../../internal/cli/treedelivery.go) | `next-launch` serves an existing tree and requests detached work when due/pending; cold no-build/no-fallback still advances in front. | Reuse delivery and cold semantics. |
| [`backgroundadvance.go`](../../internal/cli/backgroundadvance.go) | Detached act is log-backed, resolves user-authorized selection, skips busy locks, cancels children and records keyed outcomes. | Extend its dispatcher/lifecycle; do not replace its build implementation. |
| [`backgroundadvance.go`](../../internal/cli/backgroundadvance.go) | Requested-key resolution and category adapters cover selected trees, programs and supported ordinary npm. | Add category adapters, not an agent registry. |
| [`patchedadvance.go`](../../internal/cli/patchedadvance.go) | Existing advance owns check/replay/build/admission, good-build serving, interrupts and lock order. | Scheduler dispatches this act; outcome state never substitutes for admission. |
| [`hosttrees.go`](../../internal/cli/hosttrees.go), [host advance](../../internal/cli/hosttrees.go) | Host launch consumes admitted results and registers owner-scoped demand; versioned copies and render-owned links are separate. | Timer admits only; launch/apply owns materialization. |
| [`agentupdates.go`](../../internal/config/agentupdates.go) | User-only timing/off policy defaults enabled; keys are pack names. | Preserve default/off/owner precedence, not a new updater registry. |
| [Pool bounds](../../internal/cli/run/buildslot.go) | Eight checks and a CPU-bounded build pool per act, Apple Container one. | Reuse existing pools and per-key exclusion first; global permits require actual resource evidence, not a foundational redesign. |
| [Backend capability](../../internal/cli/run/backendcaps.go) | Apple Container side-by-side build limitation is an existing inferred constraint; background builds remain disabled there. | Implement the already-ruled check-only slice; require native evidence before changing capability. |
| [Launch flag table](../../internal/cli/runcmd.go), [host flag scope](../../internal/cli/host.go) | Neither launch table reserves `--update` or `-u`; host jail-only rejection derives from the launch table. | Proposed alias is available; both parsers/help/rewriter and host-shared exclusion must change together. |
| [Capture policy ruling](../design/program-delivery.md#decision-ledger) | Machine-level ordinary CLI/MCP capture is settled and its supported npm producer/consumers are connected in the candidate. | Review the combined callers, then verify real sealed production and unsupported-route fallback; do not infer runtime equivalence from synthetic fixtures. |
| [Manual update](../../internal/cli/packupdate.go), [advance force](../../internal/cli/patchedadvance.go) | Human refresh overrides automatic policy/stamps; current force also rebuilds unchanged inputs and does not wait on a keyed winner. | Add fresh-check independently of force-rebuild; retain wait/reread and admitted artifact reuse. |
| [Standing fork pins](../../internal/cli/run/forkbuild.go) | Branch addresses keep persistent pins until manual resolution. | Timer can build missing bytes at that pin, never move it; explicit source constraints are a different restriction. |
| [Detach spawn](../../internal/cli/backgroundadvance.go) | No explicit environment is set, so the child inherits its launch environment. | Add bounded environment scrubbing deliberately; detachment is not credential exclusion. |
| [Cold held advance](../../internal/cli/patchedadvance.go) | A hold suppresses fetch when a good build exists, not when none exists. | Preserve initial cold establishment/readiness; no imaginary first-install held identity. |

The inspected detached machinery performs one act requested by a launch; it does not itself
schedule subsequent due work after that act exits. Persistent cadence belongs around the existing
acts, not inside each builder. Busy skips must remain observable and retryable without stamping success.

## Measurement boundaries

Available installed-host records identify the same short source hash as this audit. One sample reports
an approximately **2.66-second first-stage-to-handover window**, with apply **0.645 s**, readiness
**0.961 s** and program prelaunch refresh **0.848 s**. These are recorded spans, not a complete
wall-clock controlled benchmark, and have no trustworthy cold/warm classification.
Separate coarse host launch records of **2.7 s**, **0.5 s** and **0.6 s** are not interchangeable
with the notch spans. Older quit samples cannot establish current shutdown performance.
Raw personal paths, logs and conversation material are deliberately not part of this public record.

Before claiming a speedup, QA records executable/source identity, platform/backend, selection,
policy, cache state, complete launch wall clock, named spans and repeated samples. Compare ordinary
admitted launches, cold establishment, due background work and explicit freshness waits separately.
Never add overlapping shutdown spans. Nested rootful Podman cannot validate real rootless loopback
or ID-mapping behavior; compile/vet cannot establish Darwin runtime behavior.

## Research to implementation

- [Preparation design](../design/launch-update-preparation.md): lifecycle, scope, safety and concurrency contracts.
- [Ready independent plan](../design/launch-update-preparation-plan.md): scheduler, request and adapter implementation map and remaining acceptance; producer details stay in the [capture plan](../design/launch-program-capture-plan.md).
- [Existing build design](../design/pi-extension-store-builds.md): retained downstream slices and prior evidence.
- [Existing roadmap](../plans/roadmap.md): sequencing, not a duplicated state database.

No external research is needed to select a second pipeline: the existing one already supplies
provenance and sealed-build boundaries. The candidate connects scheduling, category adapters and launch selection/materialization.
The remaining work is independent review and combined runtime/platform/performance acceptance.
