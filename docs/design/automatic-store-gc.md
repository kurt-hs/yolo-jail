---
status: in-review
stage: DESIGN
next: "Rule OQ-GC1–OQ-GC3; do not implement until the safety contracts are resolved and the seed becomes a grounded plan"
---

# Automatic store maintenance must outlive the last launch

**Status:** 2026-10-28. Proposal only; source packet at `e02bb526bc5b4d59fa5ba7293163cdee44f3ff78`. Nothing built or measured here.

> **In short.** Yolo should automatically retire recoverable environment artifacts, using each family's own safety authority, even when the user stops launching jails. A scheduler supplies opportunities, not permission to delete.

**Why it matters.** Routine manual pruning is unacceptable to the owner; one retained capture per changing identity does not bound disk growth.

**The shape.** Producer events and quiet launch housekeeping feed one coordinator, backed by user-level host recurrence and family-specific guarded retirement.

**Cost.** Scheduler lifecycle, cooperative reader protection and migration fencing; some offline cache hits become rebuilds or safe admission refusals.

**Start at [Safety before recurrence](#safety-before-recurrence)** — today's generic capture sweep cannot simply become periodic.

**Needs your ruling:** [OQ-GC1](#OQ-GC1), [OQ-GC2](#OQ-GC2), [OQ-GC3](#OQ-GC3).

**Reads with:** [independent advice and audit](../research/automatic-store-gc-advisory-20261028.md), [companion seed](automatic-store-gc-plan.md) (not an implementation hand-off).

---

## Requirement and boundaries

**The settled requirement is automatic maintenance**, not an inspection command or a checklist the owner must repeatedly execute. Yolo manages an agent's environment per workspace; confinement is an attribute of that environment, not the organizing principle for retention ([charter](../reference/what-yolo-is.md#the-one-sentence-answer)).

This proposal covers yolo-owned recoverable installations, build artifacts and generations. Capture here means an installation or fork build, **not a conversation**. It does not authorize deleting credentials, agent state, workspace worktrees, adoption archives, non-owned tool caches or arbitrary host Nix closures. It promises neither a disk-full impossibility nor a physical-reclaim quantity. No measured 12 GB claim exists.

My recommendation is the combined-trigger architecture below. It records Root's synthesis of two independent opinions, **not a joint ruling or a third paid design pass**. Numerical policies and availability choices remain proposals.

## Current evidence and promises to amend

The frozen packet and complete opinions are in the [research record](../research/automatic-store-gc-advisory-20261028.md#frozen-source-packet). Current facts at the source stamp:

- `Program` includes Compatibility for ordinary captures and Fork for patched ones; `Select` ranks by **receipt time**, not use ([selection](../../internal/capture/select.go#L40-L62), [ordering](../../internal/capture/select.go#L239-L252)). One winner per identity bounds neither identities nor bytes.
- `PruneSupersededCaptures` uses the complement of selection ([generic GC](../../internal/capture/gc.go#L163-L198)), while exact recipe/good-build readers and explicit keyed materialization also exist ([exact readers](../../internal/cli/patchedadvance.go#L3597-L3668), [keyed route](../../internal/cli/capturematerialize.go#L140-L158)).
- Admission precedes receipt append ([ordinary admission](../../internal/cli/programcapture.go#L21-L141)); generic deletion does not join that publication fence. Marker-first deletion can strand a tree outside normal candidate enumeration ([deletion](../../internal/capture/gc.go#L232-L265), [enumeration](../../internal/capture/store.go#L447-L473)).
- Captures are explicitly excluded from the launch slot today ([housekeeping](../../internal/cli/run/housekeeping.go#L215-L239)). Logical walks do not measure unique allocation or assured reclaim ([inventory walk](../../internal/cli/stores/walk.go#L38-L115)).

This design **proposes superseding**, not silently amending, [A6](../reference/disk-footprint.md#why-its-this-way) and [OQ-YW1](../research/central-yolo-watcher.md#11-decision-ledger), which prohibit general periodic reclaim. It also proposes replacing [OQ-PD17](program-delivery.md#decision-ledger)'s K=1/no-age/no-reference-index premise, ruled 2026-09-04, because the reader and identity premises changed. Preserve [disk-footprint safety/backfill principles](../reference/disk-footprint.md#principles) and [image retention](../reference/image-retention.md) unless an explicit accepted amendment says otherwise. Existing reference documents remain authorities for current software.

## Safety before recurrence

**P1. Unknown means retain.** Failed runtime queries, unreadable receipts, missing ownership or unresolved readers block only the affected family/dependency set, never become proof of absence.

**P2. Publication is an obligation, not a time grace.** An immutable publication obligation spans admission through attribution and establishment of consumer references. It cannot disappear between a completion marker and receipt append. A crashed producer is reconciled using durable intent plus producer/backend evidence; age alone cannot abandon it.

**P3. Reader protection and retirement share a fence.** A **retirement fence** *(coined here)* is the serialization boundary joining acquisition/release of a reader obligation with the final eligibility check and transition to deletion; it is not a use timestamp or an unrelated GC lock. Materialization, lazy handed keys, exact-key access and good-build recovery are protected until their independent destination or continuing obligation is safely established/released. Current pointers and live references always remain protected.

**P4. Computed winners are not perpetual pins.** An old identity's computed winner may be eligible after every real obligation disappears. Lasting exact-key access requires a durable pin, defined here as an explicit promise to retain a content key, not a historical receipt. Pins count against accounting but are never overridden by pressure.

**P5. One deletion protocol.** Manual prune, writer retirement and automatic passes all join the same guarded path. One maintenance writer per store holds a nonblocking pass lock; family authority performs the final recheck under the retirement fence. Busy locks defer; producer concurrency is allowed only through the cooperative publication/reference protocol. Stable lock ordering is engineering work, not a policy choice.

**P6. No privilege expansion.** Use established rootless namespace cleanup rights where available. Do not request sudo, boot a stopped guest, enable lingering or alter host services to meet a target. Unsafe old callers must be excluded or isolated from the automatic namespace before deletion is enabled.

## Coordinator and family authority

The coordinator coalesces requests, apportions bounded work, evaluates pressure, resumes progress and reports health. A family authority proves ownership, regeneration eligibility and reader safety and performs retirement through its established backend. Shared reporting and a receipt adapter do not create a universal liveness oracle.

| Family | Proposed authority and retained policy |
| :--- | :--- |
| Captures | Cooperative publication/reference fence; materializers, exact/lazy/handed keys, good builds, pins and current pointers protected. Only unneeded artifacts with demonstrated bounded regeneration are automatic candidates. |
| Runtime images | Current workspace pointers plus runtime live veto; unknown declines, removal never forced; no new global image count or undo buffer. |
| Image GC roots | Existing seven-day fresh-launch floor for nonlive roots; live roots always protected; root release is not closure reclamation. |
| Flake generations / pack trees | Current/mounted sources, delivery records and launch/backend known-gone checks; runtime uncertainty retains. |
| Embedded pack trees | Held/unknown leases retained; exclusive-lease retirement; legacy missing-lease final trees remain conservative. |
| Managed in-jail Nix roots | Separate registry/cap/watcher authority; this proposal does not resolve the reference's unmeasured registration/GC fence. |
| Host Nix / non-owned / durable state | No automatic authority expansion; explicit opt-in host Nix policy and durable/non-owned exclusions remain. |

Family evidence: [image rules](../reference/image-retention.md), [root registry limitations](../reference/in-jail-nix-roots.md#the-handoff-fence), [generation reaper](../../internal/flakebundle/flakebundle.go#L174-L209), [embedded leases](../../internal/prune/embeddedtrees.go#L175-L263), [handed keys](../../internal/cli/run/forkhanded.go#L228-L273), [build workspace known-gone checks](../../internal/cli/forkbuild.go#L100-L209).

## Scheduling lifecycle and availability

Recommend **hourly user-level host recurrence**, plus producer events and the existing quiet after-attachment slot. This is a real alternative to Fable's **debt-armed one-shot**: a timer created only when a pass records deferred work and removed when that work clears. Hourly is simpler coverage of future age eligibility, crashes before work is recorded, and last-consumer disappearance; none necessarily leaves an armed timer.

Proposed lifecycle:

1. Yolo installs/enables its own user scheduling registration as part of the managed host lifecycle, updates it with launcher upgrades, and disables/removes it with opt-out/uninstall. Interrupted setup is detected and repaired automatically on the next managed host opportunity; do not turn setup into recurring manual maintenance.
2. Producer publication/reference release coalesces a request within **one minute**. A launch requests the quiet pass after attachment without waiting for it; launch exit is an additional opportunity only with known-gone consumer evidence, never the sole release witness.
3. A launchd user agent on macOS or systemd user timer/service where available on Linux requests a pass **hourly**, with catch-up on scheduler startup/login/resume/upgrade. It need not keep a privileged or continuously resident daemon.
4. Reclaimable backlog schedules continuation after **one minute**; failures back off up to **15 minutes**. Persistent unknown/protected conditions get hourly rechecks, not a hot loop. Coalesce concurrent requests; cancellation preserves progress and obligations.
5. Opt-out stops future automatic requests and cooperatively cancels ongoing work at a safe boundary; it does not erase pending retirement state. Re-enable resumes it. Host recurrence uses only existing backend access for guest work; an unavailable guest defers.

**Availability is qualified:** logged-out users or absent user scheduling services may have no recurrence. No privileged service, lingering, crontab fallback or manual scheduler setup is implied. A scheduler-unavailable installation reports degraded event/launch-only maintenance and retries registration when the host lifecycle next permits. Continuous logged-out coverage is not promised. [OQ-GC1](#OQ-GC1) chooses the default scheduling domain, not whether maintenance should be automatic.

## Proposed policy and accounting

All defaults below are **unmeasured hypotheses**, not accepted quotas. GiB means 2³⁰ bytes; MiB means 2²⁰ bytes. [OQ-GC2](#OQ-GC2) owns the storage-versus-offline-availability trade-off.

| Control | Proposed baseline |
| :--- | :--- |
| Capture logical bytes, high / target | **8 GiB / 6 GiB** per machine capture store |
| Complete capture keys, high / target | **1,000 / 750** globally, not per identity; shared content keys count once |
| Unpinned unused captures | **30 days** since verified use; old superseded entries have a **7-day** comfort window |
| Free-space pressure | Available space below **max(2 GiB, min(10 GiB, 5% of filesystem capacity))**; recovery target trigger + **2 GiB**, best effort |
| GC-specific metadata | **64 MiB** target; coalesced timestamps, active indices and bounded completed summaries, never canonical evidence truncation |
| Pass work | **30 seconds cooperative runtime**, **1,000 filesystem operations**, **16 retirement starts** maximum |

Policy order: recover already-authorized deletion backlog and proven-dead staging; then eligible superseded least-used captures; then other unpinned reconstructible captures, including old winners. Under ordinary conditions apply the age windows; byte/count/free-space pressure may override those windows, **never protection or uncertainty**. Empty stores are a quiet no-op. A key serving several identities remains protected if any obligation needs it. Malformed/missing use evidence is unknown, not ancient; historical receipt time must not be relabeled as use.

Account for **staging, pending admission, deletion backlog, GC metadata and protected/unknown legacy bytes**, not just complete entries. Walks retain partial/unknown labels. Logical accounting includes shared file lengths, so compare it separately from unique/shared allocations and observed available space. Reflinks, hardlinks, image layers and released roots make logical removal an unreliable physical-reclaim promise. Unknown-size entries cannot be counted as zero or silently admitted as bounded.

If protected/unknown bytes alone exceed limits, retain them, stop discretionary prewarming/new cache admissions and report protected overage. Existing viable launches continue. A required new build may fail safely when no temporary capacity can be reserved, with a storage reason and a next step; neither vendor fallback nor unknown-size production demonstrates bounded regeneration. A fallback rebuild must obey admission policy too. Required correctness metadata that cannot fit blocks new obligations, not protection of existing ones. Recovery-space reservation is best effort, not a hard filesystem quota.

## Retirement, crashes and disk-full recovery

The proposed state sequence is **protected publication → available → durably retiring → payload removed → completed bookkeeping**. An abandoned producer is a separate evidence-based branch, not a timeout transition. Retirement intent preserves immutable key/provenance and obligations across restart, including same-key re-admission.

- **Recheck then record:** under the shared fence verify every family protection, record resumable retirement, then remove payload. Losing a marker or moving a tree is not by itself permission to delete. Do not begin if necessary locks, intent or reference recording fail.
- **Enumerate independently:** complete entries, pending publication, markerless pending/deleting trees, staging and unexplained legacy trees each have bounded fair cursors. A normal completion-marker scan cannot recover all of them.
- **Incremental cleanup:** arbitrary large trees consume bounded operations per pass and resume fairly; a deletion-count limit is not a byte convergence proof. Oversized objects must not be permanently skipped. Blocked filesystem calls can exceed the cooperative deadline.
- **Metadata and locks:** compact only disposable GC summaries/index state. Preserve canonical receipts, manifests and drift evidence required by readers. Never unlink an acquirable lock file while an old inode may still be held; require true protocol exclusion or bounded stable lock infrastructure.
- **Full disk:** failure to write progress/debounce does not mark completion. Resume previously authorized deletion only when identity and protections remain verifiable. Rename can fail even on a full filesystem; there is no zero-free-block success guarantee. If use/reference recording fails, do not proceed as though nothing uses the entry.
- **Retry and visibility:** lock/permission/runtime/scheduler failures retain and retry at the stated pace without a new launch where recurrence is available. Routine success stays quiet in the launch terminal; changing pressure/admission/recovery failures get rate-limited health notices with a safe next step.

## Old state and mixed versions

Automatic enablement requires actual protocol exclusion or namespace isolation of incompatible readers/mutators; a new version marker does not make old software cooperate. New objects have durable publication obligations and explicit regeneration classification. Legacy backfill reads named receipts/manifests/delivery/build/runtime evidence and migrates only what it can prove safe under exclusion. Unresolved objects remain retained and counted.

A 24-hour attribution grace or an acquirable lock cannot prove absence of a publisher or legacy reader. Rootless ownership uses established namespace cleanup rights, with no new privileges. Irrecoverable legacy ownership/evidence cases are explicit exceptions with an actor/remedy, **not routine manual maintenance**. [OQ-GC3](#OQ-GC3) chooses what lasting exact-key availability to preserve; engineering must still demonstrate exclusion and safe migration before automatic deletion.

## Alternatives, costs and risks

| Alternative / risk | Disposition or cost |
| :--- | :--- |
| Writer plus launch only | Reject as complete solution: no idle catch-up after crashes, age eligibility or consumer disappearance. Keep both as useful triggers. |
| Debt-armed one-shot | Genuine scheduling option; smaller dormant lifecycle but incomplete without proofs that future eligibility/release/crash always arms work. Prefer hourly. |
| Schedule generic prune unchanged | Reject: exact-reader/publication/markerless safety gaps remain. |
| One liveness oracle / force quotas | Reject: different family authority and protected overage make it unsafe. |
| Permanent privileged service | Outside proposed authority; user-domain gaps remain explicit rather than silently enabling it. |
| Rebuilds and offline misses | Bounded automatic eviction reduces cache availability; durable pins/local-only protection trade bytes for certainty. |
| Conservative legacy retention | May prevent immediate recovery of reported disk growth; no numerical result is promised. |
| Scheduler/protection complexity | Adds lifecycle and correctness metadata; rollout begins with classification, then protocol-compliant cleanup, then provable legacy backfill. |

Astra's defaults and lifecycle contracts are conceptual, not verified software. Fable's winner-protection versus winner-eviction contradiction and timer/publication/compatibility/rename/convergence gaps remain explicitly unresolved in the [comparison](../research/automatic-store-gc-advisory-20261028.md#root-comparison-and-unresolved-advice). They are not evidence of consensus safety. The next planner must validate engineering feasibility against the then-current tree, not build from these opinions.

## Observable acceptance, not current results

Acceptance requires observing all of these after implementation, with platform availability stated:

- With no new launches, eligible idle backlog drains fairly; future age eligibility and crash recovery get another attempt.
- Thousands of changing ordinary Compatibility identities do not create perpetual winner pins; byte/count/metadata policies include staging and deletion backlog.
- Paused admission before attribution, exact materialization, good-build recovery and lazy handed readers survive concurrent manual/writer/automatic retirement.
- Restart after retirement/marker loss resumes payload cleanup; unknown runtime/evidence and zero-space metadata failures preserve protection.
- Incompatible old callers cannot enter automatic namespaces; unresolved legacy bytes remain visible and count toward admission constraints.
- Protected overage never forces deletion or fails a viable existing launch; new required builds fail safely when necessary.
- Inspection distinguishes candidate/protected/unknown/deferred logical bytes, backlog age, scheduler health, last/next attempt, root releases and observed free-space changes; routine success is quiet.

No tests or runtime measurements for this proposal exist in this docs-only closeout. Lock/data-structure factoring, stable candidate tie ordering, error wording and cursor encoding are delegated engineering decisions subject to these behaviors. A completed implementation plan and safety validation precede any build permission.

## Decision Ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| GC-D1 | Owner requires automatic maintenance of stores such as captures; no routine manual pruning to prevent disk growth. No numeric quota, scheduler or broader deletion authority accepted. | 2026-10-28 recorded | [Requirement](#requirement-and-boundaries) | — |

## Open Questions

1. 💬 **OQ-GC1: Should yolo check idle stores hourly or only while known cleanup remains?**

   Priya captures installations, exits her last jail and does not run yolo again for weeks. Today launch housekeeping cannot revisit those stores. An unused installation may age into eligibility after the last pass, so automatic maintenance needs a scheduling domain.

   - **A — Hourly user scheduling, enabled by yolo with opt-out.** Eligible idle work gets revisited; logged-out or unavailable user scheduling still waits for return.
   - **B — A one-shot timer only for recorded unfinished work.** No dormant registration after work clears, but future eligibility and crash/release coverage need additional proven arming rules.

   <!-- vantage: question id=OQ-GC1 leaning="Hourly user scheduling — simpler coverage of future eligibility, crash recovery and last-consumer disappearance without new privileges." -->

   _Leaning:_ Hourly user scheduling — simpler coverage of future eligibility, crash recovery and last-consumer disappearance without new privileges.

   **Answer:**
   > _(empty — fill in when decided)_

2. 💬 **OQ-GC2: How much offline cache availability should automatic cleanup trade for space?**

   Priya runs yolo after many changing installations. Today one winner per identity can keep growing. Evicting unused regenerable captures saves logical bytes but can require a download or build next time.

   - **A — Proposed 8 GiB high / 6 GiB target, global count/metadata bounds and pressure-overridable age windows.** Protect live/pinned/unknown data and constrain new admissions; existing usable launches continue.
   - **B — Preserve unused winners longer or raise the budget.** More offline hits, but larger protected/retained overage and later admissions may fail sooner.

   <!-- vantage: question id=OQ-GC2 leaning="Use the proposed baseline as revisable, unmeasured policy — admission constraints are safer than forced deletion of protected data." -->

   _Leaning:_ Use the proposed baseline as revisable, unmeasured policy — admission constraints are safer than forced deletion of protected data.

   **Answer:**
   > _(empty — fill in when decided)_

3. 💬 **OQ-GC3: Should lasting exact-key availability require an explicit pin?**

   Priya's launch retains a specific capture key; weeks later its generated launcher runs `yolo internal capture-materialize --key` for that build. Today keyed access exists without a global retention limit. Keeping every historical key forever conflicts with automatic bounded caching; legacy readers also need safe exclusion before migration.

   - **A — Explicit durable pins preserve lasting access.** Unpinned regenerable keys may expire; unresolved legacy/local-only obligations remain protected and visible.
   - **B — Preserve historical exact-key availability by default.** More old keys remain available, but their unbounded retained bytes constrain new admissions.

   <!-- vantage: question id=OQ-GC3 leaning="Explicit durable pins — lasting availability should be an intentional storage obligation, with conservative legacy migration and no forced eviction." -->

   _Leaning:_ Explicit durable pins — lasting availability should be an intentional storage obligation, with conservative legacy migration and no forced eviction.

   **Answer:**
   > _(empty — fill in when decided)_
