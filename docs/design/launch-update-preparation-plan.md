---
title: "Combined preparation: implementation map and remaining verification"
status: accepted
stage: DECIDED
next: "Review the combined source, then run final runtime and platform acceptance before graduation"
---

# Combined preparation: implementation map and remaining verification

**Status:** 2026-10-14. The combined candidate connects the independent demand worker,
source freshness, timer, host consumers, Apple checks and ordinary npm captures. Short source
fixtures do not establish a real sealed runtime, native macOS or rootless behavior. Final
runtime acceptance and independent review remain; this plan is not a shipped claim.

**Design:** [preparation contracts](launch-update-preparation.md). The design wins on behavior;
the tree wins on fact. This map describes the selected implementation, not another registry.
Ordinary CLI/MCP producer details live in the [capture plan](launch-program-capture-plan.md).

## Source map

| Component | Source | Constraint |
| :--- | :--- | :--- |
| Timer and selection | [scheduler](../../internal/cli/preparationscheduler.go), [state](../../internal/cli/preparationstate.go) | One scheduler state; per-key generation and retained diagnostic evidence are not admission. |
| Authenticated lifecycle | [control](../../internal/cli/preparationcontrol.go), [publication](../../internal/cli/preparationpublication.go) | Random lifetime identity and kernel lease; unknown ownership neither admits nor destroys. File sync, rename and directory sync must succeed. |
| Independent production | [shared demand](../../internal/cli/shareddemand.go) | Requester leases attach to the same worker. Losing one requester cannot cancel surviving demand. Workspace-only declarations remain foreground. |
| Source request boundaries | [request identity](../../internal/cli/preparationrequest.go), [short ledger](../../internal/packsrc/checkstart.go) | An epoch and sequence identify a check start after registration. Recreated ledgers and older in-flight checks cannot satisfy freshness. |
| Plain pin publication | [plain requests](../../internal/cli/preparationplainrequest.go), [fork lock](../../internal/packsrc/forklock.go) | The independent worker publishes under the fork lock and current freshness authority. Timers never move a standing pin. |
| Actual launch slot | [build pool](../../internal/cli/buildpool.go), [launch slot](../../internal/cli/run/buildslot.go) | Registration boundaries precede pooled work, below attach decisions. Exact unchanged artifacts reuse their admission. |
| Host consumption | [host trees](../../internal/cli/hosttrees.go), [floor](../../internal/cli/hostfloor.go), [readiness](../../internal/cli/hostreadiness.go) | Exact recorded entry and full selected declaration; independent installed copies and unknown-session retention. |
| Apple capability | [checks](../../internal/cli/preparationapple.go), [listing](../../internal/cli/applebuildcapability.go) | Linux timer checks only. Exact reuse precedes required-build admission; malformed or unavailable listing never proves absence. |
| Ordinary npm | [npm adapter](../../internal/cli/programnpm.go), [demand](../../internal/cli/programcapturedemand.go) | The selected sealed producer joins this worker, not a second builder. Unsupported/customized runtime inputs retain their bounded declared route. |
| Reporting | [status](../../internal/cli/preparationstatus.go), [phase timing](../../internal/cli/buildreport.go), [log append](../../internal/cli/logsink.go) | Read-only diagnostic status; real phase spans and confined locked appends at their production callers. |

## Invariants and traps

- Source freshness is an epoch/sequence boundary with current guarded publication, not proof from
  historical sequence numbers. Ordinary npm has its separate check sequence.
- A source requester never becomes the production owner. Cancellation detaches its lease;
  positively observed zero demand cancels the owned child. Unknown demand retains the child but
  cannot authorize admission. Teardown uses the original backend and a bounded cleanup context.
- Scheduler control uses an authenticated random incarnation, never a PID, executable digest,
  wake file or stop file as owner authority. Replacement drains before releasing its lease.
- A retained workspace's original ownership is checked before asking whether a new Apple build
  may start. Runtime admission cannot retarget retained evidence to the current backend.
- Freshness is not force-rebuild. Explicit failure cannot deliver historical success;
  [PF-D81](patched-forks.md#PF-D81) strict failure and
  [PF-D82](patched-forks.md#PF-D82) ordinary disclosed historical recovery remain distinct.
- A generated warm launcher validates its actual runtime before marker success or exec. This
  validation cannot install, materialize, mutate receipts or run a registry lookup.
- In a test of host exec, install the exec-capture fixture before calling the real front door.
  Otherwise syscall exec can replace the test process and falsely finish the suite with zero.
- Add new files to the reviewed index before Nix verification; its fileset does not see untracked
  source. Regenerate the completion-reader census and official binary digests after source changes.

## Remaining acceptance

Independent review must inspect the source/demand CAS boundary, plain pins, original-runtime
teardown, strict versus historical serving, and the actual HOST/launcher/MCP callers. Use
permanent consumer tests, not component passes or binary stamps as runtime equivalence.

After source stability, run the full landing gate and the slow integration suite once on the
combined commit. Build shipping binaries and launch the fresh binary by path from a private
throwaway workspace, pointed at the live candidate. Never install inside this jail or launch a
nested jail against the live workspace home.

A nested Podman launch proves neither rootless mapping/image-store delivery nor host-loopback
reachability. Those require a real rootless host or CI. Native macOS requires actual Apple and
Seatbelt observations, including the readable host-file denial test. Controlled cold/warm,
production capture and complete wall-clock measurements remain separate from synthetic source
fixtures. No acceptance check starts a coding agent or calls its API.
