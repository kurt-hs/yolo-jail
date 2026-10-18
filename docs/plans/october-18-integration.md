---
title: "Plan: October 18 candidate recovery and serial landing"
status: accepted
stage: DECIDED
next: "Review the isolated patch-series caller-fix candidate; no main merge, push or release is part of this checkpoint"
summary: "Records the authorized October 18 recovery scope, ownership boundaries, review dispositions, evidence and outstanding platform checks."
---

# Plan: October 18 candidate recovery and serial landing

**Updated against current main `6683038d2`, the observed model/Mac landing.** The accepted model-reference and Mac/recovery slices are already in that main commit. This isolated writer preserves the full retained patch-series candidate ancestry at `cd8cfdfab` and closes only its remaining `series check` caller gap; it does not mutate main. The live-config candidate remains excluded.

## Recovery ownership and scope

- **This isolated checkpoint:** owns preserving the candidate ancestry, reconciling shared records, running focused candidate gates and producing a reviewable merge candidate. It does not move current main; original candidate worktrees remain read-only.
- **Mac/recovery lane:** original Mac implementation commits `98715e67b` and `41dd11e3f` remain in the preserved history through merge `48b78e3d3`; recovery-plan commits `a34f22b66` and `aded77a14` retain the recovered ownership snapshot. The Mac changes are documentation and integration-test changes. They were independently reviewed before the committed fixes and proportionately rechecked; no new review panel was started. Native-Mac runtime remains unverified.
- **Model lane:** the complete model implementation and its environment-isolation repair are committed through `aebeb0e6e`, with the underlying implementation ancestry retained. Its independent review returned **ACCEPT with notes** and identified a P2 in the all-profile host fixture. This candidate adds a qualified, known scope reference and an unknown-reference control for every profile, requiring a positive checked-reference result and a matching unresolved warning respectively; no assertion was loosened.
- **Patch-series lane:** the retained candidate `cd8cfdfab` is integrated here with its full ancestry, not cherry-picked as a final-commit-only patch. Its earlier clone-isolation and record-only repairs are preserved; this checkpoint closes the remaining P1 by hydrating the host identity only when the selected tree's mapped source resolves to the exact explicitly named workspace-pack directory. The caller keeps the named manifest and rejects an unrelated clone. This isolated candidate is not a main landing.
- **Live-config-apply lane:** not owned by this landing. The initial review blocked on four P1s: mount-record write failure could falsely complete a removal; broker lock acquisition ignored deadlines; missing/unreadable broker scope could be treated as already applied; and process identity was checked after namespace mutation. The repair review confirmed those four areas were addressed, but found a new P1: successful mount changes invalidate their own mount record, preventing normal add/remove/replacement recovery. This lane remains **BLOCKED** and is excluded.
- **Interrupted-worker ownership:** the recovery record observed the earlier workers interrupted, with host-wide process status unknown. No worker was resumed and no unowned source tree or candidate was modified.

## Mac candidate integration

The conflict resolution keeps main's [§6.2 doorway diagnosis](../design/macos-user-suite-on-a-developer-mac.md#62-another-sessions-doorway-was-read-as-this-sessions-survivor) and [§6.3 housekeeping diagnosis](../design/macos-user-suite-on-a-developer-mac.md#63-the-housekeeping-tests-slot-was-not-due-because-its-stamp-is-the-machines), including their existing fixes. It preserves the stable DM1–DM4 IDs as rows in the [Decision Ledger](../design/macos-user-suite-on-a-developer-mac.md#decision-ledger), updates roadmap references, records the unchanged mise-wins behavior, and distinguishes candidate evidence from native-Mac evidence. No root grant or log-policy bypass was added.

The Mac repair was rechecked against its recorded review fixes: process listings include both relevant UIDs, sudo allowances are exact-count, path checks cover `/` crossings, and a refused log read is not mislabeled as an unrefused empty result. No additional review panel was run.

## Candidate dispositions

| Outcome | Disposition in this landing |
| :--- | :--- |
| Model-reference registry, resolution and host/boot reporting | Included from the independently accepted model candidate; the P2 host fixture weakness is repaired by positive known-reference and unknown-reference controls across all nine Pi profiles. |
| Mac privilege, unified-log skip, floor and recovery test fixes | Included from the recovered Mac candidate; candidate checks are not a native-Mac run. |
| Relative patch-branch behavior | Included in this isolated candidate from the retained full `cd8cfdfab` ancestry; exact-path caller hydration, unrelated-clone rejection, clone isolation, and no-fetch default reporting are covered. Not landed on main. |
| Live config apply and mount injection | Excluded; repair review found a P1 in the successful mount-record lifecycle. |
| Rootless-host verification | Outstanding; not established by this Linux environment. |
| Real broker end-to-end verification | Outstanding; not established by candidate tests. |

## Validation and limits

- The earlier model/Mac candidate's `just format`, `just check-ci`, full integration run, Pi-profile check, Vantage check and model nested smoke are historical evidence for the `6683038d2` landing, not runs of this patch-series candidate. The full slow integration suite was not rerun for this narrowed follow-up.
- `go test ./internal/cli -run '^(TestSeriesCheckHydratesOnlyTheExactSelectedWorkspacePackIdentity|TestSeriesCheckUsesTheRecordedPatchBranchVerdictAndNamesStaleRemedy|TestSeriesCheckUsesRecordedConflictWithoutWalking|TestSeriesCheckReportsUnavailableWhenNoRecordedMergeVerdictExists)$' -count=1`, the entrypoint separate-checkout mapping test, and the packsrc clone-isolation test passed on this candidate.
- `go test ./internal/cli/... ./internal/entrypoint ./internal/packload ./internal/packsrc ./internal/hostfloor ./internal/packdecl` passed. `just format` and `just build-go` passed.
- A nested attempt with the candidate binary but primary `/workspace` as `YOLO_REPO_ROOT` and the inherited outer `YOLO_VERSION` was refused by the source-skew guard; it is not candidate evidence. A correct candidate-bound attempt with the ambient pack config stopped at ordinary `chrome-devtools-mcp` preparation. One bounded retry with a throwaway HOME, `{"packs": []}`, no workspace packs/MCP, candidate `YOLO_REPO_ROOT`, and the candidate binary passed through `CANDIDATE_MINIMAL_NESTED_OK`. No package was installed or debugged, no project/user config was changed, and no shared cache was cleaned.
- `uvx vantage-check docs/plans/downstream-asks.md docs/plans/october-18-integration.md` passed. Checking the [patched-forks reference](../reference/patched-forks.md) reports seven pre-existing `ref/unlinked-oq` findings on its legacy question rows; the same rows exist on observed main `6683038d2` and were not broadened into this fix.

## Serial landing boundary

1. Current main was observed at `6683038d2`, which already contains the accepted model-reference and Mac/recovery slice. This writer preserves the retained patch-series candidate and closes its caller gap in an isolated merge candidate only.
2. Do not merge this candidate to main, push, tag, release, deploy or resume the excluded live-config candidate as part of this task. The live-config repair review's mount-record lifecycle finding remains outside scope.
3. The existing model-reference changelog entries and Mac Decision Ledger IDs/anchors remain as landed at `6683038d2`; the patch-series candidate's user-facing changelog entry is preserved with its ancestry.
4. Native-Mac, rootless-host, real-broker and remount-resistance checks remain outstanding. No root grant, log-policy bypass or shared-cache cleanup is part of this checkpoint.
