---
title: "Plan: October 18 candidate recovery and serial landing"
status: accepted
stage: DECIDED
next: "Fast-forward the verified combined tip to unchanged main, run just done, and retain native-Mac, rootless-host and real-broker checks as outstanding"
summary: "Records the authorized October 18 recovery scope, ownership boundaries, review dispositions, evidence and outstanding platform checks."
---

# Plan: October 18 candidate recovery and serial landing

**Written against current main `1a9ded40b`; combined candidate is isolated.** The authorized objective is the smallest verified slice: preserve and land the accepted model-reference candidate `aebeb0e6e` and Mac/recovery candidate `aded77a14` onto current main only. This is not authorization to land the patch-series or live-config candidates. Main remains `1a9ded40b` until the final verified tip is fast-forwarded.

## Recovery ownership and scope

- **This serialized landing:** owns integration, reconciliation of shared records, candidate gates and one fast-forward of the final verified tip. The original candidate worktrees remain read-only.
- **Mac/recovery lane:** original Mac implementation commits `98715e67b` and `41dd11e3f` remain in the preserved history through merge `48b78e3d3`; recovery-plan commits `a34f22b66` and `aded77a14` retain the recovered ownership snapshot. The Mac changes are documentation and integration-test changes. They were independently reviewed before the committed fixes and proportionately rechecked; no new review panel was started. Native-Mac runtime remains unverified.
- **Model lane:** the complete model implementation and its environment-isolation repair are committed through `aebeb0e6e`, with the underlying implementation ancestry retained. Its independent review returned **ACCEPT with notes** and identified a P2 in the all-profile host fixture. This candidate adds a qualified, known scope reference and an unknown-reference control for every profile, requiring a positive checked-reference result and a matching unresolved warning respectively; no assertion was loosened.
- **Patch-series lane:** not owned by this landing. The initial review found P1s for cross-clone reuse of relative-locator resolution/build state and for requiring a fresh fetch instead of using the recorded offline verdict. Follow-up repairs addressed clone isolation and record-only reporting, and added host-to-guest identity mapping. The latest caller audit still **BLOCKS**: default `series check` does not hydrate the mapped host identity, so a VM-side workspace-relative pack can reject a valid host record as stale. This lane is excluded and is not claimed landed.
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
| Relative patch-branch behavior | Excluded; latest caller audit remains blocked by missing host-identity hydration in default `series check`. |
| Live config apply and mount injection | Excluded; repair review found a P1 in the successful mount-record lifecycle. |
| Rootless-host verification | Outstanding; not established by this Linux environment. |
| Real broker end-to-end verification | Outstanding; not established by candidate tests. |

## Validation and limits

- `just format` passed before the integration-resolution commit.
- `just check-ci` passed on the combined candidate: Linux vet, short tests, Linux/Darwin static analysis, changelog and documentation gates, and lint.
- `go test ./internal/cli/check -run '^TestCheckResolvesEveryShippedPiProfileModelReference$' -count=1` passed after adding the known qualified scope reference and unresolved control for each Pi profile.
- `YOLO_NO_PROGRAM_READINESS=1 go test -count=1 -timeout 0 ./integration` passed on the combined candidate in 762.205s. Integration `TestMain` builds the CLI from this checkout; no stale-image override was used. `podman info --format '{{.Host.Security.Rootless}}'` returned `false`, so this run is not rootless-host evidence. Readiness was disabled for the suite; no agent API calls were made.
- The model candidate's recorded nested smoke used its freshly built candidate binary and candidate `YOLO_REPO_ROOT`, not `/workspace`'s main binary. It remains prior model-slice evidence; the combined candidate's full integration run above is the final-runtime evidence for this landing.
- `uvx vantage-check` passed for the Mac design, model design, roadmap, backlog and this recovery plan. `git diff --check` passed.
- `python3 scripts/completion-census.py` was rerun. It produced broad environment-dependent census churn; comparison showed no new out-of-package reader for the added host-check test, so the generated drift was restored and the tracked census remains unchanged.
- The shared Go cache at `/home/agent/.cache/go-build` was inspected (root-owned, mode `755`, reported 441G) and preserved. The Mac candidate's recorded Linux/darwin compile checks remain portable evidence only.
- Native-Mac runtime, rootless Linux, real-broker widening/narrowing and read-only remount resistance remain outstanding. No `YOLO_ALLOW_*` stale-image bypass, root grant, log-policy bypass or cache cleanup was used.

## Serial landing boundary

1. Current main was observed clean at `1a9ded40b` before landing. Fast-forward only the fully gated combined tip; if main moved, stop and reconcile/regate rather than force or reset it.
2. The model and Mac/recovery slice is the only authorized landing. The patch-series and live-config candidates remain excluded and blocked by the P1s recorded above.
3. The final update retains the shared changelog's two user-facing model-reference entries; the Mac work is test/documentation work and adds no separate user-facing release note. The Mac Decision Ledger IDs and section anchors remain stable.
4. After the fast-forward, run `just done` on clean main. Native-Mac, rootless-host, real-broker and remount-resistance evidence remains separately owed.
5. No push, tag, release, deployment, root grant, log-policy bypass or shared-cache cleanup is part of this task.
