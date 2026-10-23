---
title: "Native Mac verification of the run-as-you model, the macos-user suite and CA trust"
date: 2026-10-23
status: open
tags: [lab-report, macos, macos-user, seatbelt, testing, evidence]
summary: "One session on the maintainer's Mac: both prototype modes pass against the generated profile, the macos-user suite goes from 92 failures to 2 real ones after a find-blocker fix, the CA trust test passes against a real MDM CA, and the internal/cli wall time is a quarter git shim."
---

# Native Mac verification, 2026-10-23

A [lab report](README.md). Host: the maintainer's Mac, macOS 26.5 (25F71), arm64, Apple Git 2.50.1, Apple
Container 1.1.0, podman on applehv. Source: `origin/main` plus this session's commits, ending at `406b5599a`
for the suite runs. The session ran inside `yolo host -p bedrock -- claude`, launched by an installed yolo
older than this source (`0.14.0+562`); that matters twice below. No agent was started by a test, and no
credential or API call was used.

## 1. The prototype, both modes, against the generated profile

`run-checks.sh --mode self|account --profile-from-yolo <HEAD yolo>`, after upstream's loopback and `/tmp`
rulings ([OQ-MM7](../design/macos-user-model.md#OQ-MM7), [OQ-MM8](../design/macos-user-model.md#OQ-MM8)):

| Mode | PASS | FAIL | SKIP |
| :--- | ---: | ---: | :--- |
| self | 43 | 0 | b7 (clipboard empty), g8 (no colima or Docker socket), f1 (opt-in) |
| account | 42 | 0 | e1 (self mode only), g8, f1 |

Both rulings hold as built: a write to the shared `/tmp` is refused and the hatch reopens it (a3t, a3h); a
0600 file of yours in `/tmp` is unreadable (b14); your loopback listener and the agent's own server are
reachable (d1, d3); a sibling session's endpoint file is not (g5). `a4t`, a Go client's TLS handshake, worked
in both. This meets precondition 2 of the plan's
[slice 11](../design/macos-user-model-plan.md#slice-11--the-rerun-gate-then-the-default-flips-after-6-to-10-human-mac-run).

**Account mode needs no reprovisioning.** This Mac grants only yolo's run-as rule,
`(_yolojail) NOPASSWD: /usr/bin/env`; the script needed root sudo, so it now detects that rule and grants
the account the workspace by ACL instead (`f6fda7802`).

Two profile findings from the same work, both fixed: the deny-default profile let a stat of `/dev/disk0`,
`/dev/rdisk0` and `/dev/bpf0` through (`97b75fca6`), and the first run's "Apple Events reached Finder" was a
false alarm, `get name` sending no event (`ba35937f1`).

## 2. The macos-user suite

`YOLO_TEST_MACOS_USER=1 go test -count=1 -timeout 0 -v -run '^TestMacosUser' ./integration`.

**First run: 69 passed, 92 failed, 4 skipped (172 s).** Nearly every launch fixture failed in about 0.15 s
at `yolo macos-fix-permissions`, because the permission scripts ran `find` by PATH and this session's PATH
starts with the guardrails pack's `find` blocker. That is a product bug, not a harness one: yolo run from a
`yolo host --` agent with guardrails selected hits it on every macos-user launch. Fixed in `406b5599a`
(`/usr/bin/find`, as the script already spells `/usr/bin/id`), with a test that runs both scripts under a
blocker.

**Second run: 158 passed, 3 failed, 4 skipped (765 s); ledger `executed=109 skipped=18`.**

- **Real: self-mode pack-tree builds fail.** `TestMacosUserPackTreeProgramBuildsOnceAndRebuildsOnChange/self`
  and `TestMacosUserPackTreeProgramOverrideBuildsTheCheckout/self` fail; both `account` halves pass, as does
  `TestMacosUserPackTreeProgramBuildFailureRefusesOnlyIt` in both modes. The build jail exits 1 with
  *"capture bootstrap failed — the staging home has no generated launcher"*, from
  the bootstrap call in `internal/macosuser/capture.go`; the bootstrap itself printed nothing the build log
  keeps, so the cause is not identified. It does not depend on the inherited `YOLO_JAIL` (fails with it
  unset). Both tests landed 2026-10-23 and had not run on macOS before.

  *Since this run:* the bootstrap's own output, its last 8 KiB, now goes to the act's writer, which the build
  log keeps, and the refusal carries its exit status and the end of its stderr. The next Mac run records the
  cause.

  *Hypothesis, read from the code on Linux:*

  - **The passing failure test proves less than it seems.** `BuildFailureRefusesOnlyIt/self` asserts only
    that the refusal names the program, the build log, the retry and the hatch, and a bootstrap failure
    names all four too. Every other macos-user capture and build test runs in account mode. So the
    self-mode capture bootstrap may never have succeeded on hardware.
  - **Only self mode confines this bootstrap.** In account mode the bootstrap crosses with `sudo -n` and
    runs unconfined; only the driver runs under `sandbox-exec`. In self mode the bootstrap runs under the
    build's own profile (`SeatbeltSealedSelfCaptureProfile`). That profile is deny-by-default and opens only
    the staging tree, yolo's staging root, your per-user temp dir and system paths. It sets no `Home`, and it denies the machine
    root, the shared store, the host loopback and the nix daemon socket. A self-mode launch's bootstrap runs
    under the session profile, which opens far more, so the launch tests passing does not cover this.
    Any read or write the bootstrap makes outside those roots, a seal deny included, fails it here and
    nowhere else.
  - **The `-D YOLO_HOME_CONTENT_DELIVERY=1` lift is not the cause.** The capture profiles carry no
    home-content forms, so nothing in them reads the parameter. A self-mode launch whose packs deliver no
    home content has no such forms either, and self-mode launches passed in this run with the same `-D`.

  *Witness to collect:* the `capture bootstrap` output block and stderr tail in the failing build's log, and
  the Seatbelt denies logged while it ran (`log show --last 10m --style compact --predicate 'sender ==
  "Sandbox"'`). If the deny names a path, the fix is to open that path to the bootstrap alone in the
  run-as-you capture profile, the way `-D` opens home content to a launch's bootstrap. Opening it to the
  driver as well would be wrong.
- **Environmental: `TestMacosUserPatchedExtensionConflictRefusesAndSkipStartsWithoutIt`** fails only because
  the old host notch exported `YOLO_JAIL=1`, which current yolo deliberately treats as unknown and warns
  about with a runtime hint the test refuses. It passes with `YOLO_JAIL` unset. At the time, the harness kept
  only its own `YOLO_TEST_*` dials out of launches. Since this run it drops an inherited `YOLO_JAIL` as well
  (`staleInheritedLaunchEnv` in `integration/harness_test.go`).

**CA trust, measured for the first time with a real MDM CA.** This Mac's System keychain carries
`Hyperscience JSS Built-in Certificate Authority` (a Jamf CA), so `TestMacosUserTrustsSystemKeychainCA`, which
skips on a Mac with none (CI's included), ran and passed in both modes, as did
`TestTheLoginKeychainReadNeverWaitsOnAPrompt` (0.02 s). That is the System keychain half of
[PS-D10](../design/provisioner-sets.md#PS-D10) and the run of [PS-D17](../design/provisioner-sets.md#PS-D17)'s
account half; a handshake through git, Node and uv against a server that CA signed is still unmeasured.

## 3. `internal/cli` and the darwin git shim

Recorded in [the test-suite speed record](../plans/test-suite-speed.md): `/usr/bin/git` costs about 20 ms a
start against about 10.6 ms for the Command Line Tools binary, and the package's short suite takes 1127 s
with the shim and 872 s without, all tests passing. `internal/entrypoint` takes 655 s.

## 4. Not done, and why

- **A real self-mode launch** (`yolo -- bash` with `macos_user_mode: self`), slice 11's precondition 3. With
  this source, the maintainer's user config is refused before any launch: it still sets `mcp_presets`, which
  `82eefb74c` removed. The fix is the one the refusal names (`chrome-devtools` into `packs`). The suite's
  launches use their own config, which is why section 2 ran.
- **`just install` on this Mac** ([installed-launcher provenance](../design/installed-launcher-provenance.md)),
  held for the same reason: the installed yolo would then refuse the maintainer's own config.
- The Apple Container builder exemption, the interrupted-provisioning wording, podman workspaces through
  `/tmp` and `yolo host --` placement remain unruled on main.

## 5. `just done` on this Mac

The full gate ran (no verified ancestor here), with the Command Line Tools git first on `PATH`;
`internal/cli` then took 980 s, under the package limit. Its failures, at the tree of `5fb441574`:

- `TestUsageListsEveryParsedFlag` (`yolo approve`, `yolo audit` flags missing from help): already passes at
  `5c394d3f4`.
- `TestCheckReportsAnOptionalMountItWillSkipAsInformation`: the check package read the developer's real
  `$HOME`, so the maintainer's refused `mcp_presets` ended the merged section early. Fixed by giving the
  package's `TestMain` a temporary `HOME`.
- `TestStopHomeProcesses*` (three) and `TestProcessEnvironReadsALiveChildsHome` in `integration`: no child's
  `HOME` was ever visible. `ps -E` cannot see the environment of a child this shell started either, so this
  is the environment the gate ran in (this agent session, or macOS 26.5 itself), not yolo. Which of the two
  is unmeasured; the same one-liner in a plain Terminal settles it:
  `HOME=/tmp/probe sleep 30 & ps -E -ww -o command= -p $! | tr ' ' '\n' | grep '^HOME='`.

## 6. Temporary state

`~/.config/yolo-jail/overrides.jsonc` was created to set `macos_user_mode: self` and removed at the end of the
session. Every prototype run removed its own directories and keychain item.
