---
title: "macos-user launch refused over pi; materialize receipts refused; three red unit packages"
date: 2026-10-16
status: open
tags: [macos-user, patched-forks, capture, testing, lab-report]
summary: "A macos-user launch of this workspace was refused because the patched fork pi-fork has no darwin build. Its materialize receipts were also refused (fixed in bff4741cf). The landing gate is red in three packages this session did not touch."
---

# macos-user launch refused over pi, 2026-10-16

A [lab report](README.md). Host: macOS (Darwin 25.5.0), workspace `/Users/Shared/yolo/yolo-jail`,
backend `macos-user`, HEAD at the start `7ba9c7433`.

## Observed

A `yolo` launch in this workspace ran `mise install` and materialized codex, copilot and claude
from the capture store. Then provisioning refused the launch (exit 78):

```text
yolo: REFUSING to start this jail: a program a selected pack declares could not be installed.
      program pi (pack pi): its install exited 1
            ⚠ pi is not available in this jail: pi is not delivered on macos-user: fork pi-fork/pi is a
            patched fork, whose upstream a fresh launch on a container backend checks and whose patch
            series it replays and builds in a capture jail — which no macos-user launch runs, and no
            macos-user launch can read the capture store yet (install-capture.md hand-off H4); run it on
            a container backend: YOLO_RUNTIME=container (Apple Container) or YOLO_RUNTIME=podman
```

Each of the three materializes also printed:

```text
yolo: (the materialize receipt could not be written: refusing to create /Users/Shared/yolo/yolo-jail/.yolo:
the workspace /Users/Shared/yolo/yolo-jail CONTAINS yolo's own state directory
(/Users/Shared/yolo/yolo-jail/.yolo/home/local/share/yolo-jail), ...)
```

The user config (`~/.config/yolo-jail/config.jsonc`) selects `pi` and
`~/.dotfiles/yolo-packs/pi-fork`.

## Fixed and committed

**`bff4741cf` — fix(macos-user): a relaunch's capture materialize writes its receipt.** On
macos-user the sandbox account's `~/.local` links into `<ws>/.yolo/home/local`. `yolo internal
capture-materialize` judged `.yolo` creation by the process HOME, so the workspace appeared to
contain yolo's state directory. It now calls `paths.GuardScopeUnder(darwinBootstrapScopeHome())`
when it runs as the sandbox account, as `yolo internal log-append` (`internal/cli/logsink.go`) and
`darwin-bootstrap` already did. Regression test:
`TestInternalCaptureMaterializeWritesTheReceiptOfAMacosUserRelaunch`
(`internal/cli/capturematerialize_test.go`). It failed with the exact message above before the
fix and passes after it. Not yet re-measured on a real macos-user launch.

## Open

### 1. The refusal message is stale about hand-off H4

`macosUserForkWhy` (`internal/cli/run/forkbuild.go`) says "no macos-user launch can read the
capture store yet (install-capture.md hand-off H4)". H4 was answered (b) and landed 2026-10-05
([`provisioner-evidence.md`](../design/provisioner-evidence.md), row 7 and the "Auto-capture
cannot run on macos-user" row, retired 2026-10-05). This very launch materialized three programs
from the store. The same clause also appears in
[`forked-programs-as-packs.md`](../design/forked-programs-as-packs.md) OQ-FP9 and FP-D3.
**Decision needed:** reword the reason to name only what is still missing (next section), and
update the tests that pin the old wording (`macosuserforks_test.go`, `guestcontainerstep_test.go`).

### 2. No macOS build path for a patched fork (the real reason pi is missing)

[FP-D24](../design/forked-programs-as-packs.md#FP-D24) builds a **plain** fork for darwin in a
sealed macos-user capture, but says: "A patched fork on a Mac still has none." A patched fork's
advance (check the upstream, replay the series, build) runs only in the fresh launch's build slot,
and the macos-user arm of the run pipeline returns before that slot. The pieces mostly exist: the
host-side replay, and the sealed darwin build act (`macosuser.RunForkBuildAct`). **Decision
needed:** whether to wire the patched-fork advance to the darwin build act on the macos-user path,
which is design-sized work for [`patched-forks.md`](../reference/patched-forks.md) and
forked-programs-as-packs.

### 3. Whether a structurally undeliverable fork should refuse the launch at all

[JR-D6](../reference/program-readiness.md#JR-D6) refuses a launch for a fork with no host build.
[JR-D5](../reference/program-readiness.md#JR-D5) keeps only a warning for a program with no build
for this platform ("no install can fix it, and the pack is more than its program"). A fork on
macos-user is the JR-D5 kind: no retry on this backend can fix it. Today every macos-user launch
with a fork pack selected is refused unless `YOLO_ALLOW_MISSING_PROGRAMS=1` is set. **Decision
needed (the maintainer's):** amend JR-D6 so a macos-user fork is a JR-D5 skip, or keep refusing.
Moot if thread 2 lands. Workarounds now: `YOLO_ALLOW_MISSING_PROGRAMS=1`, or
`YOLO_RUNTIME=container` / `podman`.

### 4. The landing gate is red, in three packages this session did not touch

`just done` ran the full `just check-ci` on `bff4741cf` (1216.6 s) and failed:

- **`internal/cli` timed out** after the 20-minute package limit, while
  `TestAMoveNeverReapsABuildAnotherAdvanceHasNotSettled` had been running 3 s. No test reported
  FAIL. A separate `-timeout 900s -v` run also timed out with zero `--- FAIL` lines. The slowest
  passes were `TestBackgroundPatchFailureRecoveryUsesCurrentCheckAuthority` (45.9 s) and
  `TestBackgroundAdvanceRepairTokenRefusesOtherPersistedFailureIdentityChanges` (29.8 s). Related
  to [`test-suite-speed.md`](../plans/test-suite-speed.md). So `internal/cli` as a whole is
  **unverified** on this commit: only the materialize, log-append and darwin-bootstrap tests were
  run and pass.
- **`internal/logcap` is flaky:** `TestAppendBeneathSerializesTrimAndConcurrentAppends` failed
  with `openat receipts.jsonl.lock: no such file or directory` and "kept 300 records, want 400".
  Run alone, it passed 1 time in 3.
- **`internal/packsrc` fails every time:**
  `TestTheRebaseRecipeRunsAsPrintedAndItsExportWaitsForTheRebase` (`rebaserecipe_test.go:91`):
  `git am` rejects the exported series ("git diff header lacks filename information when removing
  1 leading pathname component"). The last commits touching that area are `b06d0c900` (retire
  `yolo pack rebase` for a printed git recipe) and `49607bfaf`. Local git is Apple Git 2.50.1, and
  `diff.noprefix` and `diff.mnemonicPrefix` are unset. Not yet bisected, and not checked on Linux.

## Evidence

```console
$ go test -short -run TestInternalCaptureMaterializeWritesTheReceiptOfAMacosUserRelaunch ./internal/cli/   # before the fix
--- FAIL: ... a macos-user relaunch's materialize wrote no receipt
$ go test -short -run 'CaptureMaterialize|CaptureLookup|LogAppend|DarwinBootstrap' ./internal/cli/      # after
ok  	github.com/mschulkind-oss/yolo-jail/internal/cli	1.588s
$ go test -short -count=1 -run TestAppendBeneathSerializesTrimAndConcurrentAppends ./internal/logcap/    # x3
ok / FAIL / FAIL
$ go test -short -count=1 -run TestTheRebaseRecipeRunsAsPrintedAndItsExportWaitsForTheRebase ./internal/packsrc/
FAIL
```

Full gate output: `.git/yolo-completion/last.log` on the machine that ran it. The launch's own logs
are `.yolo/launch.log` and `.yolo/startup.log` in this workspace.
