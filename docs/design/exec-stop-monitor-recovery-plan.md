---
status: accepted
stage: DECIDED
next: "Complete the scoped checks, commit implementation/docs separately from typed-stop fix, then request the one HIGH read-only review"
depends-on:
  - exec-stop-monitor-recovery.md
---

# Plan: force-only Podman exec-monitor recovery

**Status:** 2026-10-26. Completed against the tree; written against `bf248c490b64704811d40672585d2b4c51028a2e`.
**Design:** [`exec-stop-monitor-recovery.md`](exec-stop-monitor-recovery.md).

**Precedence:** The design wins on behavior. The tree wins on fact; cosmetic drift only. This plan is advice and the first thing to be wrong. Stop and report if the design's call site or test behavior does not match the tree, if a gate fails outside scope, or if a change needs a file outside the map.

## Map

| Path | Change |
| :--- | :--- |
| `internal/cli/stop.go` | Pass explicit user force intent to one local-Podman recovery attempt after unresolved stop/removal; keep existence as the only success proof. |
| `internal/cli/stopmonitor_linux.go` | New — bridge the existing stop runner to the Linux helper and inject only fakeable proc/pidfd seams. |
| `internal/cli/stopmonitor_other.go` | New — unsupported-platform bridge that declines. |
| `internal/execmonitor/metadata.go` | New — strict, duplicate-key-rejecting Podman 5.8.9 info and typed-inspect parsers. |
| `internal/execmonitor/recover_linux.go` | New — bounded proc identity scan, exhaustive direct-child witness, pidfd pin, final revalidation, one pidfd-only SIGKILL. |
| `internal/execmonitor/recover_other.go` | New — unsupported-platform implementation that declines. |
| `internal/execmonitor/metadata_test.go` | New Linux-only parser and helper tests. |
| `internal/cli/stopmonitor_test.go` | New Linux-only public-stop caller tests for unresolved running-stop and leftover-removal routes. |
| `internal/cli/stopmonitor_negative_test.go` | New Linux-only caller-level refusal controls. |
| `internal/cli/stopmonitor_normal_test.go` | New Linux-only ordinary-stop control; its shared `stopWorkspace` fixture stubs teardown. |
| `internal/cli/stopforce_test.go` | Adjust the force-stop fake runner for the new post-removal probes; keep unknown state unsuccessful. |
| `internal/cli/stopruntime_test.go` | Answer the extra typed-absence probe in the no-op fixture. |
| `docs/research/exec-monitor-recovery.md` | New — version-fenced source facts, portable upstream citations and source-byte hashes. |
| `docs/design/exec-stop-monitor-recovery.md` | New — settled behavior and honest unmeasured boundary. |
| `docs/design/exec-stop-monitor-recovery-plan.md` | This implementation map, test contract, and separately unperformed host plan. |

## Reuse

- `stopJail` in `internal/cli/stop.go` already owns the user `force` bit, selected runtime, stop runner and success decision. Never infer intent from an internal `rm --force` argv.
- `endLeftoverJail`, `stopContainerGone`, `probeJailPresence` and `waitForJailContainerGone` already implement removal and typed absence. Use the selected runner and require the existing post-removal absence check to succeed.
- `stopWorkspace` in `internal/cli/stopruntime_test.go` enters an isolated temp workspace and stubs `finishStop`; `useStopExec` installs the fake runtime runner. `shrinkStopGone` bounds the existing removal wait.
- `stopRun` in `internal/cli/stop_test.go` is the scripted fixture for legacy stop tests. Update only the fixture whose number of typed runtime answers changed.
- `runtime.FromWorkspace` in `internal/runtime/naming.go` remains the source of the container name; do not add a new name format or runtime-selection path.

## Traps

- Podman 5.8.9 info JSON has top-level lowercase `version` but nested uppercase `Version.Version`; host/conmon fields are lowercase. A plausible `Version`-only fixture would test the wrong schema. Source evidence and hashes are in the research doc.
- `ExecIDs` lists stored-session keys, not running processes; `--no-session` need not be stored. Presence only associates an exec UUID.
- Podman's STOPPING state is `Running=false`; neither that result, successful signal, nor `rm` status releases the name. Only typed answered absence does.
- A test of the helper parser alone does not pin the stop/removal call site. The two front-door tests below must fail when either recovery call is removed.
- Ordinary stop can issue an internal `rm --force`. Pass the original user intent separately; that normal path never authorizes a host signal.
- Do not run container, proc-scan, or signal operations in automated tests. All recovery tests use a temp proc tree, scripted runner and fake pidfd callbacks.
- This changes `internal/` logic, but nested-jail execution is not authorized for this task: it would perform real container operations. Do not substitute a rootful or nested run for rootless-host evidence.

## Task 1 — guarded caller and Linux predicate

- **Settled — do not reopen:** Recovery is a last resort only after unresolved explicit `yolo stop --force` on local Podman. It accepts exact Podman `5.8.9` info with explicit local-client provenance and conmon `2.2.1`, exact full CID/exec UUID association, authentic executable identity, caller UID, non-self PID/nonzero starttime, pidfd, typed STOPPING recheck, and exactly one owned direct zombie child with complete enumeration and no live sibling. It sends only SIGKILL through that pidfd. Every missing, ambiguous, remote, unsupported, changing-at-revalidation or unobservable fact declines; final typed container absence remains the only success. **Residual:** Podman has no atomic state lock across final inspect and pidfd signal, so container state can change after the read; pidfd prevents PID reuse, not state changes.
- **Preconditions:** Work only in the assigned worktree; before source mutation or gates assert absolute root/cwd and expected HEAD `bf248c490b64704811d40672585d2b4c51028a2e`. Keep shared `HOME`, `GOCACHE`, `GOMODCACHE` and `GOENV` unchanged. No runtime commands, images, host `/proc` scans, host signals, deploy or primary-checkout/index changes.
- **Tests first:** `internal/cli/stopmonitor_test.go` — `TestRunStopForceRecoversUnresolvedRunningContainerThroughPinnedExecMonitor` and `TestRunStopForceRecoversUnresolvedTypedLeftoverThroughPinnedExecMonitor`. `internal/cli/stopmonitor_negative_test.go` — monitor/child/state change at final revalidation and fake pidfd `ESRCH`/`EPERM` decline controls. The public-call red before wiring is preserved at `/workspace/.yolo/durable/execstop-stop-caller-red.log`; reproduce with `go test -run 'TestRunStopForceRecoversUnresolved' ./internal/cli`.
- **Change:** Implement the caller bridge and Linux helper only in the mapped source files; pass user intent separately; keep unsupported platforms a decline. Keep the authorized typed-stop fix in its own already-landed commit `bf248c490b64704811d40672585d2b4c51028a2e`.
- **Gates:** `gofmt`/`just format`; `go test -count=1 ./internal/execmonitor`; the selected caller suite `go test -count=1 ./internal/cli -run 'TestRunStopForceRecovers|TestRunStopForceRecoveryDeclines|TestRunStopNormalInternal|TestStopJailDoesNotTreatUnknown|TestANoOpStopNamesTheOtherRuntimeWhenItCannotKnow|TestProbeJailRunning'`. A whole-package run already exposed unrelated `programcapturehost*`/`hostfloor*` failures; do not attribute or repair them under this fence. Nested runtime validation is prohibited by the task's no-container-operations constraint.
- **Fence:** Do not touch config schema, pack manifests, the typed-stop implementation, runtime selection, other stop behavior, image/deploy code, or unrelated test fixtures. Adjust only `internal/cli/stop*test.go` fixtures required by the typed absence probes.
- **Commit:** `feat(cli): recover stalled local podman exec monitors` (separate from the typed-stop commit).
- **Report:** Exact red and green commands, changed files, the whole-package result without overclaiming, no staged files, candidate commit/ref SHAs, and the exact rootless limitation. After the final commits, create only the unused local refs `execstop-typed-candidate-20261026` and `execstop-reviewed-candidate-20261026`; refuse a conflicting name rather than moving it. Provide the exact implementation commit/range to one HIGH read-only reviewer.

## Ships with

- Unit: `internal/execmonitor/metadata_test.go` — schema casing/version, duplicate/trailing JSON, malformed typed inspect. Fake-proc caller tests cover main monitor, wrong identity, nonlocal/unknown schema, changed STOPPING state, missing pidfd, no/multiple/live/foreign/unreadable children and PID reuse.
- Caller: `internal/cli/stopmonitor_test.go` proves both public force-stop/removal routes; `stopmonitor_normal_test.go` proves the internal normal-stop `rm --force` cannot authorize recovery; `stopmonitor_negative_test.go` pins refusal at the caller boundary.
- Integration: none added or run; the permitted test environment has no real container or process operations.
- Docs: research, design and this plan are the only intended docs. `AGENTS.md` prohibits adding a landing changelog entry.
- Config, CLI help, generated files and pack surfaces: unchanged.

## Don't

- Do not add numeric-PID fallback, process-group signaling, broad killing, privilege elevation, remote override, version guessing, or a host-process process-name registry.
- Do not call runtime info when force is false or runtime is not Podman.
- Do not claim a real rootless-host recurrence, successful host recovery, or runtime conformance based on source fixtures.
- Do not copy the unapproved `/tmp`/private-HOME/XDG/store skeleton from the separate host-plan artifact. Do not set private `HOME`, XDG paths, Podman storage, or alternate Go caches.

## Rootless-host verification plan (NOT PERFORMED)

This is a concrete plan for a future separately authorized run on an owner-controlled disposable **rootless Linux Podman host**. It is not evidence that a recovery was witnessed. A rootful or nested Podman run cannot prove this host behavior. Never start this plan without separate authorization for the runtime/container/process operations.

1. Require `$YOLO_DURABLE_DIR` to name the approved durable root. Create only `$YOLO_DURABLE_DIR/execstop-rootless-candidate/{bin,tmp,workspace,evidence}`. Use the assigned candidate worktree as the source and build the candidate executable to `.../bin/yolo`; keep every scratch workspace, `TMPDIR`, command transcript and result under that durable directory. Create `.../bin/podman` as a symlink to the already-approved `command -v podman` target and put only this run's `bin/` first in `PATH`; record the resolved link target. Do not copy, replace, deploy or reconfigure the host Podman installation.
2. Set only `TMPDIR="$YOLO_DURABLE_DIR/execstop-rootless-candidate/tmp"`. Preserve the actual shared `HOME`, `GOCACHE`, `GOMODCACHE`, `GOENV`, XDG environment and rootless Podman storage unchanged; record their existing values (redacting secrets) to `evidence/environment.txt`. Do not create a private HOME, private Podman store/profile or `/tmp` scratch area.
3. Before any fixture creation, save `podman --version`, the exact `podman info --format json`, and `podman info --format '{{.Host.Security.Rootless}}'`. Continue only when the output is rootless and the selected versions are Podman `5.8.9` / conmon `2.2.1`. If not, stop without trying alternate runtime flags or connections.
4. Use an owner-reviewed image already present in the user's existing rootless store and record its immutable image ID. Do not pull, build, load or remove an image. If no approved inert image is already present, stop and request separate authorization rather than adding a prerequisite.
5. Create a fresh empty workspace under the durable run directory with the minimal `yolo-jail.jsonc` runtime selection. Derive its jail container name using the existing `runtime.FromWorkspace` function. First prove that exact name is unused; then create only a uniquely named disposable fixture from the pre-existing image, with an inert long-running command. Record the full CID, state, ExecIDs, conmon path, caller UID, and the exact selected workspace/name in evidence.
6. Exercise persistent exec and `--no-session` separately only on fixtures created for this run. Attempt `yolo stop --force` from the disposable workspace using the built candidate. If the stop/removal resolves normally or the helper's full witness does not arise, record “recurrence not reproduced” and stop; do not manufacture a zombie, modify `/proc`, weaken a predicate, or manually signal any process. For any candidate signal, the candidate binary must itself be the only actor and its implementation must use the validated pidfd predicate.
7. Capture the exact command result and prove any reported success with `podman container exists <fixture-name>` returning the typed absent result. If the fixture remains, preserve it for owner review or remove only that exact fixture after authorization; never clean unrelated containers, storage or images.
8. Record versions, rootless fact, image ID, exact commands/statuses, candidate commit SHA, and whether the full witness occurred in `$YOLO_DURABLE_DIR/execstop-rootless-candidate/evidence/`. Remove only resources created by this run, and report the result as host evidence only for this exact environment.

**Status:** Not run. No runtime or process operations were performed for this candidate. Actual shared HOME and Go defaults remain unchanged. Local saved Podman source reports remain outside this repository: `/workspace/.yolo/durable/handoff-20261026/execstop-upstream-source/report.md` and `/workspace/.yolo/durable/execstop-v589-info-source/report.md`.
