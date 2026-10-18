---
title: "Plan: October 18 candidate recovery and serial landing"
status: accepted
stage: DECIDED
next: "Complete native-Mac verification and repair the separately blocked model and patch candidates before any serial landing"
summary: "Records which October 18 outcomes are integrated, which remain isolated or blocked, and the evidence needed for a safe serial landing."
---

# Plan: October 18 candidate recovery and serial landing

**Written against main `1a9ded40b`; candidate merge `48b78e3d3`.** This checkout is isolated. Main remains at `1a9ded40b`; nothing here merges, pushes, tags or deploys.

## Mac candidate integration

Original Mac commits `98715e67b` and `41dd11e3f` are preserved as the second-parent history of candidate merge `48b78e3d3`; their authorship and commit IDs are intact. The conflict resolution kept main's [§6.2 doorway](../design/macos-user-suite-on-a-developer-mac.md#62-another-sessions-doorway-was-read-as-this-sessions-survivor) and [§6.3 housekeeping](../design/macos-user-suite-on-a-developer-mac.md#63-the-housekeeping-tests-slot-was-not-due-because-its-stamp-is-the-machines) test diagnoses and fixes, and compacted the answered DM1–DM4 cards into the design's Decision Ledger. The floor discussion now reflects the mise-wins ruling. The branch is a candidate, not main. No native-Mac runtime result is claimed.

The repair commit was rechecked proportionately against its review fixes: both-UID process listings, exact-count sudo allowances, launch-directory crossings, and a log skip that does not mislabel an unrefused empty result. No new review panel was started. There is no root-grant setup or log-policy bypass.

## Recovery and ownership

- The main checkout was clean at `1a9ded40b` when recovered. The original Mac worktree is read-only and unchanged; this isolated candidate is the only worktree changed here.
- The prior children were interrupted at **2026-10-18 20:26:37 UTC**. No Claude process appeared in this process namespace when checked. That is only a local observation: host-wide worker status is unknown, and it does not authorize resuming a child.
- The separate model lane still has 11 dirty tracked files, preserved in its isolated worktree: `CHANGELOG.md`, `docs/design/model-lists-and-pickers.md`, the model-reference implementation and test files in `internal/cli/check`, `internal/entrypoint`, `internal/packdecl`, and `internal/packload`, plus `packs/pi/pack.json`. Do not reset, cherry-pick only its clean commits, or overwrite those repairs.
- The separate patch lane remains blocked at `505de4ea7` with five tracked files modified. Its independent review found two P1s: relative locators do not bind cached resolutions/builds to the declaring clone, so one clone may consume another's checked result; and the candidate replaced the requested record-backed offline verdict with a mandatory fresh fetch, so private/offline users cannot report an existing verdict. Both require repair before that candidate can be accepted. No patch-lane files were changed here.
- The shared Go build cache reported 435G. It was left intact; no cache-clearing command ran.

## Candidate dispositions

| Outcome | State at this checkpoint |
| :--- | :--- |
| Per-session profiles, clearer messages, doorway and housekeeping fixes | Already integrated on main by `1a9ded40b`; the doorway/housekeeping text in this change preserves their main-tree account. |
| macos-user privilege, unified-log skip, mise precedence and measurement rulings | Integrated into candidate merge `48b78e3d3`; portable and darwin compile checks pass, native-Mac runtime remains unverified. |
| Model-reference candidate | Still isolated; committed base plus the 11 dirty repair files remain unaccepted and must be preserved/reviewed. |
| Relative patch-branch candidate | Still isolated and blocked by the two P1 findings above. |
| Live config-apply and other unmerged branches | Not integrated or reassessed in this bounded recovery. |

## Validation and limits

- `just format` passed.
- `go test -short ./integration` passed; `go vet ./integration` and `GOOS=darwin GOARCH=arm64 go vet ./integration` passed; `GOOS=darwin GOARCH=arm64 go test -c -o /tmp/macosuser-integration-darwin.test ./integration` passed.
- `uvx vantage-check docs/design/macos-user-suite-on-a-developer-mac.md docs/plans/roadmap.md docs/plans/BACKLOG.md` passed: three files checked.
- `go test -short ./...` failed in unrelated `internal/entrypoint` Pi/model-extension assertions, including model-list registration and durable-worktree config expectations. The Mac candidate changes only integration tests and documentation; no baseline rerun or diagnosis of those failures was attempted.
- `python3 scripts/completion-census.py` was rerun after the merge. The `integration` package's reader entry did not change; the broad generated census drift was restored rather than committed. The changed tests read package-local Go sources or temporary fixtures, not repository files outside `integration/`.
- The original Mac candidate's recorded gates are Linux and darwin compilation evidence, not native-Mac runtime proof. Re-run `YOLO_TEST_MACOS_USER=1 go test -count=1 -timeout 0 -run '^TestMacosUser' ./integration` on a Mac with only the installed run-as rule; keep log-store skips countable and do not add root privileges.

## Serial landing

1. Keep `main` at `1a9ded40b` until the candidate review and required native-Mac run are accepted; this merge commit already preserves the original Mac commits, so do not cherry-pick them a second time.
2. Preserve and finish the model lane's full dirty snapshot before integrating it. Repair and review the patch lane's clone-identity and record-backed verdict blockers before considering its candidate.
3. Integrate each accepted candidate one at a time onto a clean current-main checkout, resolving shared docs after each merge. Run the reached tests and landing gate on the combined tree. Native Mac, rootless-host and other runtime claims remain distinct from Linux candidate checks.
4. This run performed no main merge, push, tag, deployment, root-grant change, log-policy bypass or shared-cache cleanup.
