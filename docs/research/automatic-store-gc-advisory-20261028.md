---
title: "Automatic store GC — independent advice, evidence and custody"
date: 2026-10-28
tags: [storage, retention, advisory, accounting]
summary: "Frozen evidence and verbatim independent opinions, with qualified capability/context audit, attributable usage and separate synthesis."
status: in-review
next: "Read the canonical design and rule its routed choices; advice is not accepted behavior"
---

# Automatic store GC — independent advice, evidence and custody

Recorded 2026-10-28 against source `e02bb526bc5b4d59fa5ba7293163cdee44f3ff78`. This is a sanitized research record, not an accepted joint design, implementation plan or software review. The [canonical proposal](../design/automatic-store-gc.md) owns policy choices; its [companion](../design/automatic-store-gc-plan.md) is only a seed.

## Provenance and audit boundary

The completed initial blind pair was not rerun or resumed. Workflow `0dcfa419-c043-447b-8398-26b69b33c921`, mission `69629c37-8e2f-4813-97ed-27e491a59d3f`:

| Role | Native run | Actual model / effort |
| :--- | :--- | :--- |
| Astra author | `011f0da7-f30c-4af5-85de-b68ad33dbac7` | `amazon-bedrock/global.openai.gpt-6-astra`, HIGH |
| Fable author | `fee5d325-3eda-48ab-93ed-9cb0b6748389` | `amazon-bedrock/global.anthropic.claude-fable-5-1`, HIGH |
| Evidence collector | `8bef18ea-fdc8-4c30-b005-862f94052a3f` | `amazon-bedrock/global.openai.gpt-6.1-sol`, medium |

Audit used each status-selected session, matching recovery descriptor/artifact metadata and the workflow receipt, not guessed sessions. Both author launches selected the project's `designer-advisory` profile: output false/inline, no assigned document output path; replace-mode system prompt; fresh context; project/global/skill inheritance all false. Automatic harness response artifacts are not author write access.

Both actual capability audits list **exactly** `subagent`, `subagent_supervisor`, `contact_supervisor` as effective tools. The system snapshots' `toolsAdded` contain the same set (order differs). No direct read/write/shell or MCP tools were effective. Both sessions contain one assistant response and no tool calls/helpers. Allowed helper capabilities still existed; nonuse is not absence.

**Not pure-packet isolation:** native runtime extensions remained enabled, with two runtime extensions and required `yolo-provider-policy`; configured extension list was empty, not effective extension list. Both sessions logged an identical ambient `memory-context` injection (content omitted here). Author system snapshots name only preamble/cwd, not project/skills sections, but flags and snapshots do not prove absence of all upstream/provider context. The opinions' own packet-only claims are qualified by this audit. No profile/access changes or new paid calls followed the discovery.

The first user task payloads are byte-identical to each other, SHA256 `c1c8a9f9c386c9004e6d6686cf9a8e57ed6fa69c80951be4c9430a5225a6a82b`. Each contains the exact frozen packet, SHA256 `acd5b9c6b0f88401d7ba1ea32eee5d9b7baf10ea4618121bbc781c7ad6707b58`, **including its trailing newline**. The packet is preceded by the same blind advisory contract and followed by the same 25-minute time-bound wrapper; neither payload contains the other opinion, preflight appendix or usage accounting. Ambient memory means the full effective context was broader than the task packet; complete provider-request bytes were not available to prove a stronger boundary.

## Attributable usage, not invoices

Per-assistant-message provider usage was deduplicated by message ID. Artifact metadata and run aggregates are cross-checks, not extra calls to add. Input, output, cache reads/writes and total counters remain separate; no character-to-token estimates or missing-counter reconstruction was used.

| Role / coverage | Input | Output | Cache read | Cache write | Total tokens | Recorded estimated cost USD |
| :--- | ---: | ---: | ---: | ---: | ---: | ---: |
| Astra, message `8986979e` | 2 | 5,896 | 0 | 8,500 | 14,398 | 0.4010700 |
| Fable, message `e316e94c` | 4 | 9,761 | 0 | 14,868 | 24,633 | 0.7854500 |
| Author pair, those two messages only | 6 | 15,657 | 0 | 23,368 | 39,031 | 1.1865200 |
| Collector, its assistant messages only | 44 | 12,418 | 1,674,347 | 109,847 | 1,796,656 | 0.5663202 |
| Parent/coordinating session, workflow-launch message `3d9826aa` only | 2 | 4,299 | 144,211 | 430 | 148,942 | 0.0584901 |
| Docs/custody worker, current run | unavailable | unavailable | unavailable | unavailable | unavailable | unavailable |

These costs are recorded configured estimates, **not invoices** or independently verified pricing. Parent/coordinating-session coverage excludes collection, reading, synthesis and later custody; no end-to-end mission total is claimed. The worker's final provider usage was unavailable at audit time and is not folded into author totals.

The parent/coordinating session is the orchestration/accounting role, distinct from the proposed GC coordinator component.

Raw per-call evidence retains provider raw usage, reasoning/diagnostic fields where present and cost components separately in private durable custody. No separate reasoning-token count appeared in the two author usage objects; HIGH is an effort setting, not a missing counter to invent. Astra cost components: input 0.0000200, output 0.2948000, cache read 0, cache write 0.1062500. Fable: input 0.0000400, output 0.4880500, cache read 0, cache write 0.2973600; Fable reports 14,868 one-hour cache-write tokens. Run token aggregates omit cache counters and therefore differ from normalized provider totals. Private session paths, ambient memory and credential material are intentionally not reproduced in tracked documentation.

## Synthesis comparison and unresolved advice

Both authors recommend producer events, quiet launch housekeeping, host recurrence and per-family authority. Both propose 8 GiB captures and a 30-day unused baseline, but neither measured the workload, capacity or reclaimability. Agreement is advice, not acceptance evidence.

| Topic | Astra | Fable | Synthesis, not owner ruling |
| :--- | :--- | :--- | :--- |
| Idle scheduling | Hourly user task plus paced backlog continuation | Debt-armed one-shot, removed when known work clears | Prefer hourly: future age eligibility, crash-before-arming and last-consumer release can otherwise be missed. User-session/scheduler limits remain explicit. |
| Capture limits | 8/6 GiB high/target, 1,000/750 keys, 64 MiB GC state | 8 GiB, winner protection floor; removal ledger 90 days or 500 records | Proposed 8/6 baseline and global count/metadata bounds, not measured quotas; preserve canonical receipts/drift evidence. |
| Pressure | Below max(2 GiB, min(10 GiB, 5% capacity)); may override age | Below min(10% capacity, 20 GiB); age shortens to 7 days | Real trade-off; choose storage/admission versus offline availability explicitly. No protection-breaking eviction. |
| Pass work | 30 seconds cooperative, 1,000 operations, 16 starts | 32 deletions; 20-second launch / 5-minute deferred pass | Operation-bounded resumable work and fair cursors, not a deletion-count convergence claim. |
| Publication / old callers | Durable obligation plus actual exclusion/isolation | 24-hour grace/acquirable locks plus version marker | Safety requires cooperative fence and exclusion; age/marker alone is not proof. |
| Namespace legacy | Use existing permitted backend rights; unresolved retains | Report-only manual namespace remedy | Established rootless cleanup rights where safe, no privilege widening; irrecoverable legacy exceptions, not routine maintenance. |

**Fable's complete opinion below remains verbatim advice. The following contradictions are unresolved, not silently repaired or called consensus:**

1. **Winner protection versus eviction:** K=1 winner-as-protection prevents the proposed winner eviction and global cardinality bound. The synthesis recommends protecting real obligations, not every historical computed winner.
2. **Timer arming gap:** future eligibility, consumer release or a crash can occur after zero debt or before debt is recorded. A later launch is not no-launch recovery.
3. **Publication/legacy gap:** 24 hours or an acquirable lock cannot prove no publication obligation or old reader remains.
4. **Mixed-version gap:** a version marker cannot make incompatible callers observe new protections; actual exclusion or isolation is required.
5. **Regeneration gap:** a vendor fallback does not demonstrate bounded regeneration or safe admission capacity.
6. **Zero-space gap:** same-filesystem rename is not proven to succeed with zero free blocks; safe failure/recovery must be defined.
7. **Convergence gap:** deletion count does not bound arbitrary tree sizes; the proposed overage/32 launch formula is not a valid byte bound.
8. **Evidence/lock gap:** truncating canonical drift evidence or unlinking an acquirable lock can destroy obligations or create a second lock inode. Bound disposable GC state, not canonical evidence; exclude old lockers or use stable bounded lock infrastructure.

Astra likewise supplies unmeasured defaults and conceptual lifecycle contracts, **not verified software**. The proposed safety shape uses family-specific known-gone/cooperative lease authority, immutable publication obligations and one guarded manual/writer/automatic retirement path; current/live/materializing/lazy/exact/handed/good-build references remain protected. Unknown retains; resumable markerless recovery and paced retries count staging/deleting/metadata. Neither opinion is a code review; no new vote panel or model critique was run.

## Frozen source packet

The following fenced packet preserves the independent source evidence supplied to both authors. Repository-relative paths and symbols name the stamped tree; command/path specimens inside the fence are historical evidence, not links to private runtime artifacts. No new tree survey was performed to fill the companion seed.

```markdown
# Item27 — automatic store GC: frozen DESIGN-ONLY evidence

## Exact design question

Owner: “have our designer pair work out a design for automatic GC of stores like this capture. I don't want to have ot run manual steps to stop my disk from filling up”. What automatic, safe, bounded steady-state retention and backlog recovery should yolo provide for recoverable stores, especially install captures, without routine manual maintenance? Compare mechanisms and identify decisions requiring owner approval; quotas, broader deletion authority and a scheduler are NOT approved. Manual inventory/dry-run may remain optional inspection, not the maintenance mechanism.

Yolo describes an agent's environment declaratively, reproducibly, per workspace; confinement is one attribute, strongest/default at jail (docs/reference/what-yolo-is.md:29–32,76–84). Consider the environment lifecycle across applicable host/jail/guest modes, not merely container cleanup.

## Observed contract / source revision

HEAD and refs/heads/main: e02bb526bc5b4d59fa5ba7293163cdee44f3ff78. Collected on an allocated isolated branch; full porcelain clean. Citations below are repository-relative at that revision. Owner reports disk growth, but no host byte count or 12 GB reclamation guarantee is established. Historical doc measurements are not current-host measurements.

**Capture means installations/fork build artifacts, NOT conversations.** paths.CapturesDir is machine storage's captures child (internal/paths/paths.go:1162–1181). Layout: staging/<id>, entries/<content-key>/tree, completion marker and adjacent metadata (internal/capture/store.go:10–18,76–114). Durable workspace worktrees and agent state are different classes (internal/cli/stores/inventory.go:1059–1071).

**Manual rule:** PruneSupersededCaptures takes the same scan as Select, removes complete entries not winning ANY Program, retains distinct winning keys (internal/capture/gc.go:163–198). Select orders by latest receipt Time, greater directory key on equal times; malformed time is zero/oldest. This is capture/build RECORD time, not access time or actual use (internal/capture/select.go:116–121,239–253). One content-key can answer multiple identities.

**Cardinality:** current Program is not merely (bin,platform). Ordinary/legacy: (Bin,Platform,Source,Compatibility); patched: (Bin,Platform,Fork), ignoring Source/Compatibility in that branch (select.go:40–62,179–186). One winner PER IDENTITY does not bound bytes/identity count: new resolved ordinary inputs create Compatibility identities; sources/platform origins also distinguish winners. No whole-store ceiling here. captureRecords reads both capture/build RECORD acts, including Compatibility/fork fields (internal/cli/capturematerialize.go:491–548). Missing/unreadable receipt logs make complete entries unattributed/reapable; nil reader is refused rather than defaulting attribution empty (select.go:214–234; gc.go:163–167).

**Safety discrepancy:** gc.go:15–31/select.go:9–12 still say Select is the only reader. Current patched readers perform exact Scan lookups by fork/bin/platform/source/commit/recipe and recover good builds by recipe (internal/cli/patchedadvance.go:3597–3668); materialization accepts explicit --key (capturematerialize.go:140–158,260–346). Generic prune does NOT consult handed keys, good-build records, build locks or exact lookup. Its complement cannot simply be assumed the full reader-unreachable set or granted recurrence without resolving this mismatch.

## Current data flow / active use / concurrency

1. **Produce → admit → attribute.** Explicit capture takes per-bin capture lock, Stage(bin) clears same-ID leftovers, runs producer, admits, THEN appends record with time.Now (internal/cli/capturehost.go:230–277,319–345,1124–1133). Store hashes bytes, takes per-content-key CAS lock, freezes regular files, renames staging output on same filesystem, writes marker LAST; duplicate bytes retain canonical metadata (store.go:269–358,378–397). Caller publication guard is additional protection, not a GC guard.
2. **Resolved ordinary admission:** prepareProgramCapture takes program-<identity> lock, warm-validates winner/metadata, otherwise uses deterministic program-<identity> staging and guarded admission; receipt append follows admission. Producer is wired (internal/cli/programcapture.go:21–141; programcaptureproducer.go:581). Complete-but-not-yet-attributed is therefore a real race window: generic prune can see it unattributed. Generic reaper takes no CAS/program/capture lock (gc.go:163–198,232–241).
3. **Read → independent destination.** Ordinary materialization selects matching Compatibility then Resolve checks marker only (capturematerialize.go:362–380; store.go:411–473). Materialize prefers reflink, hardlink, copy; macOS clonefile on APFS. Destination survives source unlink, but logical bytes removed need not become physical free space (internal/capture/materialize.go:28–72). Nontransactional failure can leave partial home; ordinary route falls through to vendor installer with diagnostic (capturematerialize.go:199–220). Workspace materialize receipt time.Now goes to workspace log, not store selection records (221–250); warm launch doesn't select again. Capture recency is NOT actual use.
4. **Patched writer-side retirement differs.** Move keeps good build, all handed keys, entries shared with other programs and another held build lock; unreadable delivery evidence keeps everything (patchedadvance.go:3417–3495). HandedForkKeys unions delivery records beside still-present per-launch pack trees: conservative retention, not a fresh runtime query (internal/cli/run/forkhanded.go:228–273). Lazy first invocation may need exact handed build long after launch. Extension copies instead use marker recheck/retry (store.go:424–436; patchedadvance.go:3440–3448). Host copies with untracked continuing consumers are explicitly retained (inventory.go:365–387).
5. **Crash/staging:** EntryKeys excludes ALL markerless entries. GC drops marker FIRST then tree, retaining manifest/receipts/adjacent metadata. Interrupted after marker removal leaves bytes generic scan no longer sees, despite gc.go's “next prune finishes” commentary (gc.go:57–76,232–266; store.go:447–473). Torn cleanup is same-content admission; staging cleanup same-ID Stage (store.go:172–205,360–376). Supersession does not bound staging/torn trees/metadata/lock files. Age alone does not prove staging dead.
6. **Stronger build witnesses:** forkBuildWorkspaceReclaimable holds final workspace launch lock through admission/cleanup, checks keeper, retained original backend and returned marker, then known container absence. Unknown retains (internal/cli/forkbuild.go:100–209). Legacy rootless leftovers may require namespace removal: clearStaging supports InNamespace; incomplete-entry refusal names podman unshare (store.go:159–182,360–376). Recurrence cannot erase platform ownership constraints.

## Automatic family / authority / accounting

**Captures excluded from launch slot:** explicit pending a single home for receipt reader (internal/cli/run/housekeeping.go:215–239,708–712). Manual prune wires reader, lists each removed key/reason and retained manifest; inventory capture trigger is only yolo prune --apply (internal/prune/prunecmd.go:855–911; internal/cli/stores/inventory.go:319).

Host slot runs after attachment, dies with launcher, must not delay/fail launch, logs to bounded workspace housekeeping.log instead of disrupting terminal TUI (housekeeping.go:22–88). Nonblocking machine pass lock plus shared per-deletion lock/recheck cooperate with load-side recording; class debounce stamps completion only (90–197,585–642). No further launch = no further pass. Generic capture prune does not join these locks.

Representative families have DIFFERENT authority contracts:
- Runtime images: current workspace pointers + runtime live veto; unknown declines; removal not forced (docs/reference/image-retention.md:115–157).
- Image GC roots: seven-day fresh-launch mtime for nonlive roots; live roots protected regardless of age; unknown reaps nothing. Link removal frees no closure bytes immediately (internal/prune/imageroots.go:9–99). Attach/staying up doesn't refresh age.
- Flake generations: current and runtime-mounted sources kept; unknown declines; guarded deletion rechecks (internal/flakebundle/flakebundle.go:174–209). Pack trees discard only unused/known-gone trees plus delivery records (internal/cli/run/packtree.go:103–117).
- Embedded pack trees: held/unknown lease kept; missing lease retains final legacy trees, temporary leftovers get floor; final deletion renames while exclusive lease held (internal/prune/embeddedtrees.go:175–263). Lease is not recency.
- Managed in-jail Nix roots: separate lease/cap registry, least-renewed eviction, serialized operations; watcher hourly lifecycle, event renewals, scans do NOT renew (internal/nixroots/registry.go:51–58,224–279,450–468,631–705; watch.go:38–65). Specialized recurrence is NOT general periodic-GC authority. Concurrent root registration/GC fence remains stated unmeasured (docs/reference/in-jail-nix-roots.md:101–112).

Inventory marks measured/partial/cached/absent/unknown and yolo/human/not-yolo ownership (inventory.go:28–77). Walks sum regular-file logical lengths with deadline/unreadable lower bounds, NOT unique allocated/reclaimable bytes (internal/cli/stores/walk.go:38–115). capture.treeBytes also sums lengths and skips unreadable data (gc.go:243–266). Roots defer reclamation; CoW/hardlinks and runtime layers share allocations. A count or logical-byte report is not proof of free space reclaimed.

Distinguish yolo-owned recoverable installations/generations/images/pack trees/roots from durable user/agent work, adoption archives and non-owned stores. Host-tool aliases are explicitly “not yolo's”, counted separately (inventory.go:200–234); stateReclaimers documents protected exceptions/untracked host copies (319–387). Bounded host Nix GC is explicit opt-in, host-only/rooting-aware (prunecmd.go:61–69,1410–1458). Yolo warns about min-free, never edits host nix.conf (docs/reference/image-retention.md:252–264). No authority here to clean arbitrary host Nix/tool stores, credentials or conversation state.

## Owner constraints / promises requiring explicit amendment

Automatic maintenance, not a suggestion to run prune. Runtime query failure NEVER means inactive/unreferenced; running consumers must not be stranded. Successful routine cleanup should not add noisy launch banners. No chosen defaults, quota guarantee, authority expansion or accepted design exists in this packet.

Explicitly reconcile/supersede:
- **A6 / OQ-YW1:** no daemon/timer/cron reclaim (docs/reference/disk-footprint.md:46,1042). Claim that launch fires as often as artifacts are made needs reconsideration for growth without another launch.
- **P1–P7 / BF-P1–4:** arbitrary-moment safety, fail-safe liveness, scoped bounded host deletion and per-store backfill; automatic only with complete evidence/bounded known regeneration, otherwise offered (disk-footprint.md:83–137). Unknown-size re-download can conflict with automatic backfill promise.
- **OQ-PD17:** K=1, no age floor/index/reference oracle (capture/gc.go:36–50). Compatibility/exact-reader expansion changes premise; K=1 per identity isn't a global bound.
- **Image retention:** no global keep-count or undo buffer; current pointers plus liveness (image-retention.md:115–157,317–349). Capture budgeting cannot silently overwrite image policy.
- Host Nix configuration and user/tool-owned stores remain outside automatic authority.

## Neutral architecture options (not a recommendation)

Compare writer-side retirement, reusable launch housekeeping, narrowly scoped recurring host mechanism, or combinations. Writers know publication/identity context but miss abandoned/backfill/idle stores; slot reuses locks/logging but misses no-launch periods; recurrence handles those periods but adds installation/upgrade/scheduler/shutdown lifecycle, requires A6 approval. Reuse a single receipt adapter without pretending every family has one liveness authority.

Compare supersession-only, age versus actual-use refresh, count/logical-byte budgets, actual free-space pressure and user pins/overrides. Name protected floors and over-budget-but-live outcomes; decide when redownload/rebuild is acceptable. No option controls all disk growth: durable/non-owned stores remain. Separate bounded work per pass, retention bound and eventual retry; neither age nor an MRU ledger is proof of inactivity.

## Explicit unknowns for the pair

No host capacity/growth/allocated-sharing/reclaimable-backlog measurements. No approved thresholds, retention override/pin scope, scheduler, opt-out or disk-ceiling promise. No demonstrated capture last-use ledger/general consumer lease. Resolve exact-reader/good-build/handed-key protections, publication-before-attribution, concurrent capture/admit/materialize/manual prune/GC and mixed-version callers. Classify markerless crash/legacy missing-record artifacts conservatively. Bound metadata without destroying drift evidence; establish staging liveness and deletion rights per backend. Define disk-full behavior when locks/progress/debounce writes fail; retries without another launch; platform scheduler availability/permissions and cancellation. Inspection/dry-run should show protected/unknown/deferred bytes and reasons while routine automatic success stays quiet. These are design gaps, not chosen product requirements or proven safe defaults.
```

## Astra independent opinion — verbatim advice

```markdown
# Recommendation

**Use one automatic retention coordinator with three triggers: producer/lifecycle events, opportunistic launch housekeeping, and a narrowly scoped recurring per-user host task.** Give each store family its own eligibility and deletion authority. Make captures a bounded, reconstructible cache—not an indefinitely growing collection with one permanent winner per identity.

Do **not** schedule today’s generic capture prune unchanged. Its safety assumptions no longer cover the documented readers, publication sequence, or interrupted deletion.

This is independent, packet-only advice against **e02bb526 / collector8bef18ea**. All numerical defaults and new mechanisms below are proposals, not measured requirements or accepted authority.

## 1. Evidence and its implications

The packet establishes four decisive facts:

- Capture winners are selected by **record time**, not use. Compatibility, source and platform distinctions can create unbounded identities; K=1 therefore bounds neither objects nor bytes (`capture/select.go:40–62,116–121,179–186`).
- Exact-key, recipe-recovery and handed-build readers exist beyond generic selection. Patched retirement already uses stronger protections than generic prune (`patchedadvance.go:3417–3495,3597–3668`).
- Admission precedes attribution, creating a genuinely vulnerable publication interval. Generic capture GC does not take producer/CAS guards (`programcapture.go:21–141`; `capture/gc.go:163–198`).
- Marker-first deletion can strand trees that the next normal scan cannot see (`capture/gc.go:232–266`; `capture/store.go:447–473`).

**My inference:** recurrence is necessary to satisfy maintenance without new launches, but it amplifies these defects unless the lifecycle contract changes first. A timer is a trigger, not a safety mechanism.

## 2. Scope and ownership

The coordinator should cover **yolo-owned recoverable environment artifacts** across host, jail and guest modes: captures, managed runtime images, generations, eligible pack trees, and appropriately scoped roots. It should not touch durable workspace work, agent/conversation state, adoption archives, credentials, arbitrary tool stores or other owners’ files.

Separate two responsibilities:

1. **Policy/coordinator:** schedules work, evaluates pressure, apportions bounded passes, maintains progress and reports outcomes.
2. **Family authority:** proves ownership, regeneration eligibility and reader safety; performs guarded deletion using the correct backend.

A common receipt adapter and reporting vocabulary are sensible. A universal “unreferenced” oracle is not: the packet shows materially different authority contracts.

### Per-family boundaries

| Family | Recommended automatic authority |
|---|---|
| Captures | New publication/reference protocol; active selections, exact consumers, pins, handed keys and good-build obligations protected. Unneeded reconstructible cache entries eligible. |
| Runtime images | Preserve current workspace pointers, runtime live veto and non-forced removal. Unknown means retain. No new global image keep-count or undo buffer. |
| Flake generations and pack trees | Preserve current/mounted sources, delivery obligations and guarded runtime/launch-lock checks. Delete only under their established known-gone rules. |
| Embedded trees | Preserve held/unknown leases and conservative final-legacy handling; use exclusive-lease retirement. |
| Image GC roots | Preserve seven-day fresh-launch rule and live-root protection. Report root removal separately from closure reclamation. |
| Managed in-jail Nix roots | Keep the specialized registry/cap/watch authority separate. Do not claim its stated registration/GC race is resolved. |
| Host Nix and untracked host copies | No expanded authority. Host Nix remains explicit opt-in and rooting-aware; untracked continuing copies remain protected. |

Unknown evidence should block the affected dependency set, not unrelated families whose authority is complete.

## 3. Concrete triggers and scheduling lifecycle

Recommend:

- **After production/publication and reference release:** coalesce a maintenance request within one minute. Writers provide excellent local evidence but should not each invent a collector.
- **After attachment:** reuse quiet launch housekeeping as an opportunistic trigger. Cleanup must neither delay nor fail an otherwise viable launch.
- **Hourly while the responsible host user service is available:** run a pressure check and bounded maintenance pass, even with no new launches.
- **On service startup, login/resume and upgrade:** recover pending deletions and resume backlog traversal.
- **With remaining reclaimable backlog:** schedule another bounded pass after one minute; back off failures up to fifteen minutes. Persistent unknown/protected conditions get slower rechecks, not a hot retry loop.

Use a per-user **launchd agent on macOS** and **systemd user timer/service where available on Linux**. Installation, upgrades, disabling, uninstall and interrupted execution need explicit lifecycle ownership. Do not silently install a privileged daemon, enable lingering or substitute cron.

A logged-out user agent may not run. State that honestly: hourly maintenance applies while its scheduling domain exists; catch-up occurs on return. If continuous logged-out service is required, that is a separate authority choice—not something to smuggle into setup.

For guests, prefer the host coordinator operating through existing permitted backend interfaces. Unavailable guests or required namespaces mean deferred work; do not boot a stopped guest, widen permissions or run a host scheduler from inside a jail merely to meet a cleanup target. A supported installation lacking a suitable scheduler must visibly report degraded event-only maintenance, not claim idle-time coverage.

## 4. Retention and pressure: proposed starting policy

These values are **conservative product proposals without host measurements**:

| Control | Proposed default |
|---|---|
| Capture logical-byte high/target watermarks | **8 GiB / 6 GiB** per capture store |
| Complete capture entry high/target watermarks | **1,000 / 750**, across all identities |
| Unpinned, unused capture retention | **30 days** since verified use |
| Superseded-capture comfort window | **7 days**, overridable by pressure |
| Low-free-space trigger | Available space below **max(2 GiB, min(10 GiB, 5% of filesystem capacity))** |
| Free-space recovery target | Trigger plus **2 GiB**, best effort |
| GC-owned bookkeeping budget | **64 MiB**, excluding durable user evidence |
| Pass budget | **30 seconds cooperative runtime**, at most **1,000 filesystem operations** and **16 entry retirements** |

GiB and MiB mean binary units. Filesystem calls can block beyond a cooperative deadline; these are work budgets, not hard real-time guarantees.

Capture size/count limits are **high-watermark cache policies**, not guaranteed filesystem quotas. Shared allocations mean logical bytes are neither uniquely allocated bytes nor assured reclaimable space. Measure actual available-space change separately.

Under normal conditions, evict expired unused entries and old superseded entries. Under size/count/free-space pressure, remove eligible entries in this order:

1. Recoverable deletion backlog and proven-dead staging.
2. Superseded, least-recently-used captures.
3. Other unpinned, reconstructible captures, including historical identity winners.

Pressure may override cache age windows, never active-use, pin, publication or uncertainty protections. Other families retain their own eligibility rules; capture thresholds do not rewrite image retention.

Actual-use tracking should record successful materialization and relevant exact-build delivery/use, with bounded coalescing. **Recency orders eligible candidates; it never proves inactivity.** Existing receipt times must not be relabeled as historical access times.

Automatic eviction also needs **known, bounded regeneration**. A vendor fallback alone does not establish that. Captures with unavailable sources, local-only inputs or unknown regeneration requirements remain protected unless separately authorized. The design should make future admissions declare that distinction rather than discover it during an emergency.

## 5. Selection, exact keys, pins and publication

“Selected” needs two meanings:

- An **in-flight selection** acquires a protection token under the same retirement fence used by GC. Hold it until the independent destination or downstream reference is safely established.
- Being the computed winner for an old Program identity is **not perpetual protection**.

Current explicit pointers with continuing reader obligations remain protected. Merely retaining an identity in history does not create such an obligation.

Exact-key readers must either acquire a live reference or rely on an explicit pin. All handed keys and retained good-build recovery keys remain protected until their authorities positively release or replace them. Lazy first invocation forbids assuming that launch completion ends the obligation. Unreadable delivery evidence remains conservative.

An arbitrary future `--key` request cannot imply infinite retention of every key. Recommend a documented **machine-store, content-key pin** for durable exact-key availability, with optional expiry; no default expiry for an explicit permanent pin. Pins do not authorize deleting other protected data to make room.

Publication should have a durable protected intent spanning **production → admission → attribution → availability**, with recovery distinguishing pending, committed and abandoned states. Completion markers alone cannot close the packet’s attribution race. This is a required contract change, not a claim about existing code.

Similarly, all deletion entrances—including manual prune and writer-side retirement—must use the same protection rules. A safe recurring collector beside an unsafe manual path would leave arbitrary-moment safety unresolved.

## 6. Bounded cardinality, metadata and admission

Do not preserve one object per unlimited identity. A key serving multiple identities counts once; an identity with no retained object, live reference or required evidence can leave the active index.

Keep GC lookup state compact:

- Coalesce use timestamps rather than append every access.
- Compact superseded bookkeeping while preserving necessary attribution and drift evidence.
- Bound routine logs and completed deletion summaries.
- Remove inactive index identities after their final obligation disappears.
- Avoid per-ever-seen-key lock files whose count grows forever; use bounded lock infrastructure.

Do not truncate canonical receipts blindly: the packet identifies existing readers of those records. GC-specific summaries can be bounded; durable evidence cannot be discarded merely to make the index small.

Retention alone cannot strictly bound protected or unknown objects. **Admission control is the necessary complement.** Count staging, pending publication and trash against cache accounting. Once protected data/backlog consumes the budget, stop discretionary prewarming and new cache admissions rather than evict protection floors. Existing usable environments keep working. A required build that cannot obtain safe temporary capacity can fail with a clear storage reason.

Use conservative reservations and bounded producers where feasible. Unknown-size production is not a hard-quota guarantee; strict byte enforcement would require a separately accepted mechanism. If required correctness metadata cannot fit, refuse new obligations rather than silently discard existing ones.

This yields a bounded *admitted steady state*, subject to declared exemptions—not a false promise that every legacy or live store is already below the target.

## 7. Races, crashes and interrupted deletion

Use one cooperating maintenance writer per store, with nonblocking pass locking, family-specific locks and final guarded rechecks. Producers can still run concurrently; their publication/reference transitions must participate in the retirement protocol. Liveness/reference updates and retirement need a consistent atomic ordering, not merely two unrelated locks.

Deletion should be resumable and idempotent:

- Transition an eligible object into a durable **deleting/quarantined state** under its guard.
- Preserve enough identity and provenance to finish cleanup after restart.
- Remove payload incrementally; finish bookkeeping only after deletion completes.
- Enumerate deletion backlog independently of completion markers.

Same-filesystem retirement renames are attractive, but require coordination with same-key admission and materializers. They are not, alone, a reader-safety proof.

Classify markerless artifacts separately: pending publication, interrupted deletion, abandoned staging, and unexplained legacy trees. Age by itself proves none dead. Automatically recover the first categories using durable intent and backend/lock evidence. Respect rootless namespace ownership and existing backend-specific cleanup rights.

For legacy artifacts, run bounded automatic reconciliation against named receipts, manifests, delivery/build evidence and runtime state. Unresolved objects remain unknown. Their bytes still count in pressure reports and admission decisions.

Mixed-version callers are a serious boundary. Enable recurring deletion for a capture namespace only once incompatible mutators/readers are excluded or safely isolated through a versioned store protocol. Merely detecting one newer launcher is insufficient.

## 8. Bounded recovery and disk-full behavior

Traversal needs restartable, fair cursors across entries, staging, trash and metadata. Otherwise a large store can repeatedly exhaust its scan budget before reaching reclaimable objects. Schedule continuation without another launch, and expose oldest pending work and failure categories.

A large tree should become one resumable deletion job; a per-pass byte threshold must not make it permanently ineligible. Apply incremental filesystem-operation budgets and cancellation between operations.

Disk-full handling must remain fail-safe:

- If locking, required reference recording or publication intent cannot complete, do not begin the corresponding unsafe operation.
- Do not convert “could not record use” into “unused.”
- Do not mark a pass complete before durable progress exists.
- Resume already-authorized deletion when its recorded identity and protections remain verifiable.
- Reserve a small bounded recovery journal/space allowance where feasible, without assuming it defeats every filesystem failure.

Permission errors, unknown liveness and nonreclaimable pressure receive paced retries. Repeated failures produce one changing health notification, not launch spam.

There is no guaranteed byte-recovery result. Durable data, pins, active builds, unknown legacy objects, shared allocations and unrelated writers can keep the disk full.

## 9. Reporting, rollout and observable acceptance

Successful routine maintenance stays quiet. Provide optional inspection showing policy, scheduler health, last/next attempt, backlog age, candidate reasons, and **protected / unknown / deferred** quantities. Preserve measured/partial/cached/unknown distinctions. Report logical removal, root release and observed filesystem free-space change separately.

Warn only on actionable transitions: sustained pressure, blocked admissions, unavailable recurrence or persistent recovery failure. Do not prescribe routine manual pruning.

Recommend a staged rollout: shadow classification first, then automatic cleanup of newly protocol-compliant artifacts, followed by bounded legacy backfill where safety evidence is complete. Shadow mode is temporary validation, not the maintenance solution.

Observable acceptance should include:

- Idle, no-new-launch periods still drain eligible backlog.
- Thousands of changing Compatibility identities do not create permanent winners.
- Paused publication, exact-key materialization and lazy handed builds survive concurrent collection.
- Restart during deletion completes cleanup of now-markerless payloads.
- Runtime-query failures retain affected artifacts.
- Full-disk bookkeeping failures preserve protection and retry later.
- Unsafe old callers are excluded from automatic namespaces.
- Protected over-budget stores remain intact and visibly constrained; no false “space reclaimed” claim appears.

These are design acceptance behaviors, not an implementation plan or evidence that they currently pass.

## 10. Promise amendments and owner choices

Explicitly amend:

- **A6/OQ-YW1:** replace no recurring reclamation with narrowly scoped per-user recurrence and stated availability limits.
- **OQ-PD17:** replace unconditional K=1/no-age/no-index assumptions with global cache policy, use ordering and reader protection. The proposed seven-day comfort window is not guaranteed undo retention.
- **P1–P7/BF-P1–4:** preserve fail-safe liveness and scoped deletion; clarify bounded regeneration eligibility, refusal under pressure and uncertain legacy backfill.
- Preserve image policy and host-Nix/non-owned-store exclusions unchanged.

Three genuine owner choices remain:

1. **Recurring per-user maintenance, enabled by default with an explicit opt-out?** Recommend yes; no privileged or lingering service by implication.
2. **The proposed cache defaults, pressure-overridable age windows and admission refusal rather than protection-breaking eviction?** Recommend yes as revisable defaults, not a disk-ceiling SLA.
3. **Explicit pins for lasting exact-key availability, and automatic eviction only with bounded regeneration evidence?** Recommend yes, accepting that some historical/local-only captures will remain protected.

Lock factoring, receipt-adapter placement and traversal mechanics are ordinary engineering decisions.

## Strongest counterargument, trade-offs and confidence

The strongest alternative is writer-side retirement plus launch housekeeping: smaller operational surface, no scheduler installation, and excellent publication context. It may address much normal growth. **Its decisive weakness is no independent recovery during idle periods**, especially after crashes or abandoned work—the owner’s automatic-maintenance requirement remains incomplete.

The recommended design costs scheduler lifecycle work, protection metadata, compatibility fencing and occasional rebuilds/admission refusals. Conservative legacy handling may disappoint expectations of immediate reclamation. Those costs are preferable to silently stranding lazy consumers or claiming a quota that excludes staging, identities and unknown artifacts.

**Confidence:** high that a combined trigger model and stronger capture lifecycle are necessary; medium in the proposed architecture’s operational complexity; low-to-medium in numerical defaults without capacity, workload and regeneration measurements.
```

## Fable independent opinion — verbatim advice

```markdown
# Advisory opinion — automatic store GC (item27, blind pass)

Packet e02 / collector 8bef18ea, SHA256 acd5b9c6…707b58. Everything marked **[packet]** is cited from the frozen evidence; everything marked **[inference]** is my reasoning. Advisory only; no defaults below are accepted until the owner says so.

## 1. Recommendation in one paragraph

Make capture retention a **three-layer automatic mechanism with one shared authority adapter**: (1) **writer-side retirement** at admission/attribution for the identity just written; (2) the **existing launch housekeeping slot** extended to captures once the receipt reader has a single home, running a supersession pass plus a bounded whole-store budget pass; (3) a **debt-armed, one-shot, user-level deferred pass** (launchd agent / systemd `--user` timer) that exists only while a pass has left deferred work behind and removes itself when the debt clears. Retention is **K=1 per identity as a protection floor, not as the policy**; the policy is a whole-store logical-byte budget with an idle age floor refreshed by actual materialization, a free-space pressure tier, user pins, and explicit per-family authority. This requires a narrow, explicit amendment to A6/OQ-YW1 and OQ-PD17, and it does **not** promise to prevent disk exhaustion.

## 2. Scope and ownership

**[packet]** Yolo-owned recoverable classes are: captures (install/fork build artifacts, not conversations), runtime images and image GC roots, flake generations, pack trees, embedded pack trees, managed in-jail Nix roots. Durable workspace worktrees, agent state, adoption archives, host-tool aliases (“not yolo's”), host Nix store/nix.conf, credentials and conversation state are outside automatic authority.

**[inference]** The automatic mechanism should own exactly the yolo-owned recoverable set, with a per-family *authority contract* plugged into one orchestrator. Every family answers the same four questions through its own adapter: *is this artifact selected/current? is it live or leased? is it pinned/handed? can it be regenerated boundedly?* The orchestrator never invents a liveness answer a family doesn’t provide. “Unknown” from any adapter means retain (consistent with images, roots, flake generations, embedded trees **[packet]**). This keeps one scheduler/budget/reporting path without pretending images and captures share a liveness oracle.

## 3. Why three layers, and what each covers

**[packet]** Writers know identity and publication context but miss abandoned/backfill/idle stores; the slot reuses locks and logging but misses no-launch periods; recurrence covers those periods but adds lifecycle and needs A6 approval.

**[inference]** A key observation: yolo-owned stores grow only while a yolo process runs (captures are produced by launches or explicit capture commands; the in-jail roots watcher already has its own lifecycle). So the "no-launch" gap is not *new growth going unnoticed*; it is (a) artifacts becoming reclaimable **after** the last consumer exits, and (b) **deferred work** left behind by bounded passes. Both are finite, known debts. That means recurrence can be *debt-armed* rather than permanent:

- **Layer 1 — writer-side retirement.** After the receipt append for identity I, the writer runs the supersession rule for I only, under the program/CAS locks it already holds. Patched writers keep their stricter Move rule (good build, handed keys, shared entries, held build locks, unreadable evidence ⇒ keep all **[packet]**). This closes the common steady-state case immediately with no extra authority.
- **Layer 2 — launch housekeeping slot.** Add a captures class to the existing slot once the receipt reader lives in one place (the explicit precondition in housekeeping.go **[packet]**). It runs after attachment, dies with the launcher, logs to the bounded housekeeping.log, never delays launch **[packet]**. It performs: whole-store supersession, torn-cleanup finish, bounded budget eviction, and metadata compaction.
- **Layer 2b — exit pass.** Same code, run on launcher shutdown after the workspace's consumers are known gone (same evidence forkBuildWorkspaceReclaimable already uses **[packet]**). This is where post-exit reclaim happens without recurrence. If shutdown is a crash, the debt simply remains for the next pass.
- **Layer 3 — debt-armed deferred pass.** If any pass ends with deferred work (budget not reached within its work bound; items skipped for busy locks; free-space pressure unresolved), it arms a one-shot user-level timer (e.g., +1h, with backoff to 24h) running `yolo prune --auto`. When a pass ends with zero debt, the timer is unregistered. No daemon, no permanent cron. If the platform scheduler is unavailable or permission is denied, the debt is recorded and surfaces in inspection; the next launch picks it up. This is the smallest A6 amendment that still delivers "no manual steps".

## 4. Retention policy (proposed defaults — owner choice, not accepted)

**[packet]** Current rule: K=1 per Program identity; identities are (Bin,Platform,Source,Compatibility) or (Bin,Platform,Fork); one winner per identity bounds nothing globally. Selection is by record time, not use. Walk sizes are logical lengths, not allocated/reclaimable.

**[inference]** Proposed policy, in order of application:

1. **Supersession** (existing rule) — always, every layer. Zero regeneration cost beyond what is already accepted today.
2. **Idle eviction of non-protected winners**, only when the store exceeds its budget. Candidate order: least-recently-materialized first; ties by oldest record time. Eligibility requires **all** of: not protected (§5); last materialization older than the **idle floor**; the receipt carries **bounded regeneration evidence** (a resolved Source/recipe or vendor-installer route the ordinary path already falls through to **[packet]**). Entries without regeneration evidence are *offered* in inspection, never auto-evicted — this is how P1–P7's "automatic only with complete evidence/bounded regeneration, otherwise offered" is honored.
3. **Pressure tier.** When host free space on the captures filesystem drops below a threshold, the idle floor shortens and per-pass work bound rises. Still never touches protected entries.

Proposed defaults (units explicit): captures budget **8 GiB logical bytes** per machine store; idle floor **30 days** since last materialization (**7 days** under pressure); pressure threshold **min(10% of filesystem, 20 GiB) free**; per-pass bounds **≤32 entry deletions, ≤20 s wall in the launch slot, ≤5 min in the deferred pass**; deferred timer **1h first, doubling to 24h max**; metadata compaction retains removal records **90 days or 500 records**, whichever is larger. These are starting points; the packet has no host measurements, so they are untested guesses to be tuned by inspection output in rollout, not promises.

**Actual-use refresh.** The packet shows materialization receipts go to the workspace log, not store records **[packet]**. Add a store-side per-entry *last-materialized* stamp written at materialization (ordinary factoring, no new authority). Record time stays the selection key; the use stamp is only the idle-eviction order. **Neither is proof of inactivity** — that is why idle eviction applies only to regenerable, unprotected entries, where the cost of being wrong is a bounded rebuild, not a stranded consumer.

**Over-budget-but-protected outcome.** If protections alone exceed budget, the pass reports "over budget: N GiB protected (reasons)", takes no forced action, and does **not** keep arming the deferred timer for it (nothing would change). Inspection shows it; a one-line rate-limited notice appears only under the pressure tier.

## 5. Protections and per-family authorities

**[packet]** Generic prune today does not consult handed keys, good-build records, build locks or exact `--key` materialization; gc.go/select.go still claim Select is the only reader; complete-but-unattributed entries are reapable.

**[inference]** Protection set, each a named reason surfaced in inspection:

- **Selected**: current winner per identity (K=1 floor).
- **Active/handed**: any key in HandedForkKeys' conservative union, good-build records, keys under a held build lock, extension-copy targets (marker recheck/retry). Unreadable evidence ⇒ protect all (matches Move).
- **Exact-key reference**: the `--key` materialization route must leave a *reference record* (same adapter the patched path uses), otherwise it is explicitly undocumented as unprotected. I recommend it records. This resolves the "only reader" discrepancy: the reader set becomes "Select + recorded references", and the docs are corrected.
- **Publication-before-attribution**: complete entries with no attributing record are retained if marker mtime is younger than an **attribution grace** (propose 24 h) *or* if the per-key CAS lock / program lock cannot be acquired nonblocking. Only entries older than grace, lock-acquirable, and still unattributed become reapable — and are listed as "unattributed, aged" in the report so missing-record legacy is visible, not silent.
- **Pinned**: a user pin record per key (`yolo stores pin <key> --reason`). Pinned bytes are exempt from eviction but counted against the budget and shown. Pins are the user override; no other override mechanism.
- **Build/publication witnesses**: fork build workspaces keep the forkBuildWorkspaceReclaimable rule unchanged.

Other families keep their existing contracts (images: pointers + live veto; roots: 7-day fresh-launch mtime with live protection; flake generations: current + runtime-mounted; embedded trees: lease; in-jail roots: own registry) **[packet]**. The capture budget **never** overrides image policy; each family reports its own protected/unknown totals.

## 6. Races, crashes, legacy, markerless artifacts

**[packet]** Reaper takes no locks; marker dropped first then tree; interrupted deletion leaves bytes the scan can't see; staging/torn trees unbounded; age alone doesn't prove staging dead; legacy rootless leftovers may need `podman unshare`.

**[inference]**:

- **One writer per key**: the reaper acquires the per-key CAS lock nonblocking before touching an entry; busy ⇒ skip and defer. Under the lock it rechecks marker, attribution and references, then proceeds. A machine-level nonblocking pass lock (reuse the slot's) ensures one GC pass at a time; manual `yolo prune --apply` joins the same locks, so mixed manual/automatic runs are safe and idempotent.
- **Tombstone before marker removal**: rename the marker to a `retiring` tombstone (rename needs no new blocks — important under disk-full), then remove the tree, then the tombstone. Markerless trees *with* a tombstone are reapable on any later pass; this fixes the "next prune finishes" commentary becoming true.
- **Markerless without tombstone**: staging/<id> is reapable only if the per-bin capture lock (or program staging lock) is acquirable and the dir is older than a short floor; torn same-content trees follow the same lock-then-reap rule. Not acquirable or unknown ⇒ retain and report. Legacy artifacts needing namespace ownership are *never* auto-deleted; they are reported with the exact remedy (the one place a manual step is acknowledged, because the platform forbids otherwise).
- **Mixed-version callers**: a version marker in the store root; older writers lacking reference records fall under the attribution grace and conservative protections; nothing in the new design requires old callers to change to remain safe.
- **Concurrent materialize vs. GC**: materialization reads under marker recheck; because the reaper holds the CAS lock and the materializer takes/rechecks it, a loser sees "absent" and falls through to the existing vendor-installer route **[packet]** rather than a partial source.

## 7. Bounding cardinality, metadata and backlog

**[inference]** Three bounds, independent of identity count:

1. **Bytes**: the whole-store budget (§4). Identities multiply but bytes don't exceed budget plus protected overage.
2. **Metadata**: retained manifests/receipts/lock files for *removed* entries are compacted into a bounded removal ledger (90 days / 500 records). Drift evidence survives as compact records (key, identity, reason, time), not full trees. Orphan CAS lock files for absent keys are removed when acquirable.
3. **Backlog**: per-pass work bounds plus the deferred timer guarantee eventual convergence without a launch, if the owner approves Layer 3. Without it, convergence is "within a bounded number of launches", which is honest but weaker.

## 8. Disk-full behaviour

**[inference]** The pass must make progress with zero free blocks: tombstone by rename, deletions first, progress/debounce stamps written *after* reclaiming, failures to write stamps logged but non-fatal (next pass simply re-evaluates). If the housekeeping.log itself cannot be written, the pass still deletes and reports via exit status only. Never fail or delay launch for GC reasons.

## 9. Reporting and inspection

Routine success is silent in the terminal; details go to housekeeping.log. `yolo stores` (or `yolo prune --dry-run`) shows, per family: reclaimable / protected (by reason) / unknown / deferred bytes, with the caveat that these are logical lengths, not guaranteed physical reclaim (CoW, hardlinks, shared layers **[packet]**). A single rate-limited notice appears only when the pressure tier cannot be relieved or a legacy artifact needs manual namespace removal.

## 10. Explicit amendments to prior promises

- **A6/OQ-YW1 (no daemon/timer/cron):** amend to "no persistent daemon; a debt-armed one-shot user timer may exist only while deferred work exists". The "launch fires as often as artifacts are made" claim is replaced by "launch and exit passes plus debt-armed retry".
- **OQ-PD17 (K=1, no age floor/index/oracle):** K=1 becomes the protection floor; add a store-side last-materialized stamp (an index, not an oracle), an idle floor, and a budget. Exact-reader/good-build/handed protections are added explicitly; docs claiming Select is the only reader are corrected.
- **P1–P7/BF-P1–4:** preserved by restricting automatic eviction to entries with recorded bounded regeneration; others are offered.
- **Image retention:** untouched; capture budget is per-family.
- **Host Nix/user stores:** untouched.

## 11. Rollout and observable acceptance

1. **Safety first (no behaviour change):** single receipt-reader home; tombstone/lock/attribution-grace fixes; exact-key reference records; doc corrections. *Observable:* concurrent materialize + manual prune never yields a markerless-but-present tree; interrupted prune leaves tombstones that a second prune finishes.
2. **Writer retirement + slot supersession + inspection with budget preview.** *Observable:* after N successive captures of one identity and one launch, only the winner remains; inspection shows what the budget *would* evict and why.
3. **Budget + idle eviction + pressure tier enabled.** *Observable:* a seeded over-budget store returns under budget within ≤ceil(overage/32) launches; protected totals are reported, never forced.
4. **Debt-armed deferred pass (if approved).** *Observable:* a seeded backlog clears with zero launches; timer is absent once debt is zero.

## 12. What cannot be safely reclaimed

Unknown-ownership or unknown-liveness artifacts; markerless trees without tombstone or acquirable lock; legacy namespace-owned leftovers; anything under an unreadable evidence log; pinned/handed/good-build keys; non-yolo stores; durable work. Physical free space may not follow logical deletion (CoW/hardlinks/roots deferring reclamation **[packet]**). This design reduces growth of yolo-owned recoverable stores; it does not and cannot prevent every cause of disk exhaustion.

## 13. Strongest counterargument, trade-offs, confidence

**Counterargument:** any recurrence (even debt-armed) re-opens the lifecycle risks A6 avoided — scheduler install/uninstall across upgrades, permission prompts on macOS, and a process acting with no live launcher to reason about liveness. One could instead accept "converges within a few launches" and keep A6 intact. I still recommend Layer 3 because the owner's stated requirement is no manual steps *and* bounded steady state; without it, a user who stops launching yolo retains the backlog indefinitely. If the owner declines, Layers 1–2b still deliver most of the value.

**Trade-offs:** idle eviction trades rebuild/redownload cost for bytes; the attribution grace trades 24 h of reclaim latency for safety; metadata compaction trades full drift forensics for bounded storage; conservative "unknown ⇒ retain" means over-budget-but-protected states are possible and visible.

**Evidence vs. inference:** the race windows, missing protections, torn-cleanup bug, cardinality problem and per-family authority differences are all in the packet. The three-layer split, debt-armed timer, tombstone-by-rename, use stamp, and every number are my inference with no host measurements behind them.

**Confidence:** high (~0.8) that Layers 1–2b plus the safety fixes are the right shape and are implementable without new authority; medium (~0.6) that debt-armed recurrence is the right A6 amendment; low (~0.3) on any specific default number.

## 14. Genuine owner choices (with my recommended answer)

1. Approve the debt-armed one-shot user timer as the A6 amendment? **Yes.**
2. Allow idle eviction of regenerable winners (OQ-PD17 change)? **Yes, with the 30-day/7-day floors.**
3. Starting defaults: 8 GiB budget, 10%/20 GiB pressure, 32 deletions/pass, 24 h attribution grace, 90-day/500-record metadata ledger? **Accept as tunable starting points, revisit after inspection data.**
4. Must `--key` materialization register a reference record (protected) rather than remain unprotected? **Register.**
5. Legacy namespace-owned leftovers: report-only with remedy, or allow an explicit opt-in `podman unshare` step in the deferred pass? **Report-only.**
```

