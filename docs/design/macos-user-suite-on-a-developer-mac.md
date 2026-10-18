---
title: "What may a macos-user test assume about its host?"
date: 2026-10-18
status: accepted
stage: DECIDED
next: "Run `-run '^TestMacosUser'` on a Mac that grants only the run-as rule, to confirm the rewritten tests pass there; then the two defects in §6"
tags: [design, macos-user, testing, privilege, seatbelt, measurement]
summary: "The macos-user suite may assume only what `yolo macos-setup` installs: every step that acts as the sandbox account crosses through the one run-as rule, every other step is the invoking user's, and a guard test refuses a sudo the suite spells itself. The host's unified log store is the Mac's own policy, so its two tests skip countably where it is closed. Mise shims outrank the floor, as in the jail. A developer-Mac measurement counts, with its host and date. The first full run on a Mac that grants only the run-as rule (128 passed, 3 skipped, 12 failed) is the evidence."
---

# What may a macos-user test assume about its host?

**Status:** DECIDED, 2026-10-18: all four questions were ruled that day ([the ledger](#decision-ledger)),
and the test changes they call for are built. **They have not run on a Mac yet**: they were
written in a Linux jail, where `GOOS=darwin go vet` and the `-short` build are the only checks.
Evidence measured on this machine 2026-10-17 (full suite, at `245a2203d`) and 2026-10-18 (the
twelve re-run, at `4d3248ded`). Host of record: macOS 26.5, arm64, uid 501, `_yolojail`
present, `/Users/Shared/yolo` present, the run-as grant passwordless after `sudo -k`, **root
`sudo` not passwordless**, and `log show` refused.

> **In short.** The suite may assume only what the backend itself asks a host for. A test that
> needs another account crosses through the one rule `yolo macos-setup` installs, like a launch;
> a fact that is the Mac's own policy, like a closed log store, is a countable skip; and a red
> run on a developer Mac then means a broken change, never an unprivileged host.

**Why it matters.** This suite is the only instrument for a darwin-only, privilege-bearing
backend, and [`integration/macosusergate_test.go`](../../integration/macosusergate_test.go)
opens by saying why it exists: *"A SUITE THAT SKIPS MUST NOT BE INDISTINGUISHABLE FROM A SUITE
THAT PASSES."* The inverse now holds and is unguarded — a suite that FAILS for a privilege it
never declared is indistinguishable from a suite that caught a regression. Twelve tests, every
run, deterministically.

**The shape.** One gate (`requireMacosUser`, probing five host facts) and no new privilege
beside it. Every crossing into the sandbox account goes through one helper built on the launch's
own argv builder, a guard test refuses any other sudo, and one host-fact probe, the log store's,
skips the two tests that read it with its reason in the suite's ledger.

**Cost.** The System-keychain CA test now uses a CA the Mac already trusts and skips where there
is none, the GitHub runner included, so no Mac measures a TLS handshake through the session's
bundle any more. And a cleanup can leave a file the sandbox account wrote where that account
cannot reach it; the test then prints the one command an administrator runs.

**Start at [§3](#3-the-three-classes)** — the classification is the whole argument;
[§5](#5-the-shape) and [§6](#6-two-defects-and-one-design-question-this-run-found) fall out
of it.

**Needs your ruling:** None.

**Reads with:** [`macos-user-privilege-lifecycle.md`](macos-user-privilege-lifecycle.md) (what
privilege the backend itself takes, and when), [`macos-user-build-step-threat-model.md`](macos-user-build-step-threat-model.md)
(Q3, answered on hardware for the first time in [§7](#7-measurements-this-run-produced)),
[`install-capture.md`](../reference/install-capture.md) (hand-off H4, whose landing made this
run possible).

---

## 1. Recommendation and principles

**Declare the two privileges and skip countably. Do not weaken a single assertion.** The six
root-`sudo` tests and the two unified-log tests are measuring real things; the defect is that
they ask a host for something no gate told the host it needed.

- **P1. A test may assume only what its gate probed.** The gate is the contract. A test that
  spends an unprobed privilege has no contract, and its failure carries no information.
- **P2. A skip is countable or it is a lie.** The existing ledger
  (`executed=98 skipped=12`) is the mechanism; two more skip reasons join it rather than a
  second reporting channel.
- **P3. The suite's privilege floor should be the product's, and where it is not, that gap is
  a documented finding rather than an accident.** Today the suite needs strictly more than
  `yolo macos-setup` installs.
- **P4. A developer Mac is a first-class instrument.** It is the only host that can answer
  questions a GitHub runner structurally cannot ([§7](#7-measurements-this-run-produced) has
  two), so "it works in CI" is not a sufficient standard for this suite.

## 2. What the gate probes today, and what the suite spends

`requireMacosUser` ([`integration/macosusergate_test.go:344`](../../integration/macosusergate_test.go))
probes five facts through `macosUserHost.gate()`: `goos == darwin`, `euid != 0`,
`sandbox-exec` on PATH, the `_yolojail` account, `/Users/Shared/yolo`, and `runAsQuiet` — the
launch's own readiness probe, `sudo -n --user=_yolojail /usr/bin/env /usr/bin/true`.

That list is deliberate and documented: a launch needs *only* the run-as rule, and the
narrow-sudo CI step takes root `sudo` away on purpose, so probing root would skip the very
tests that measure a launch without it.

| Privilege | Probed by the gate | Spent by the suite | Installed by `yolo macos-setup` |
| :--- | :--- | :--- | :--- |
| `sudo -n --user=_yolojail …` (the run-as rule) | yes | yes | yes |
| Passwordless **root** `sudo` | **no** | **yes, 6 tests** | **no** |
| A readable unified log store (`log show`) | **no** | **yes, 2 tests** | n/a — the host's own policy |

The third row is not a privilege yolo grants or asks for at all: on a managed Mac the unified
log store is closed to a non-admin user, and `log show` answers
`Could not open local log store: Operation not permitted`.

## 3. The three classes

The full suite was run with CI's own command —
`YOLO_TEST_MACOS_USER=1 go test -count=1 -timeout 0 -run '^TestMacosUser' ./integration` —
and every one of the twelve failures reproduced on a re-run at a later commit. **No failure in
classes A or B says anything about yolo's code.**

### 3.1 Class A — six tests spent a privilege `yolo macos-setup` does not install

Each failed with `sudo: a password is required`, in a fixture or a *control* probe rather than
in the behaviour under test. **Only half of them wanted root.** The other three spelled a
crossing off the rule's shape: sudo matches the command's path, and the rule names
`/usr/bin/env`, so `sudo -u _yolojail /bin/ls` is a different command that only a broader grant
admits. Per [OQ-DM1](#decision-ledger), none of the six needs more than setup installs now.

| Test | What it spent | What it does now |
| :--- | :--- | :--- |
| `TestMacosUserCaptureSeatbeltProfileDeniesTheSharedHome` | root `sudo` for the capture plan's prepare steps, and `sudo rm` at cleanup | the prepare steps as the invoking user, exactly as `RunCapturePlan` runs them; cleanup through the sandbox account's own removal |
| `TestMacosUserTrustsSystemKeychainCA` | root `sudo security add-trusted-cert` into the System keychain, and its cleanup | reads the System keychain as the launch does and takes the CAs the launch's own composition keeps; skips, saying so, where there are none; never writes a keychain |
| `TestMacosUserHostFloorIsTheHostUsersAlone` | `sudo -u _yolojail /bin/ls`, off the rule's shape, in the **control** | the control through the rule |
| `TestMacosUserLaunchDeniesReadOfHost0644File` | `sudo --user=_yolojail /bin/bash`, off the rule's shape | through the rule |
| `TestMacosUserMacosLogAsTheSandboxAccountMeasurement` | `sudo --user=_yolojail /bin/sh`, off the rule's shape | through the rule; it records the account's view and asserts nothing about the host's store, so it is not class B |
| `TestMacosUserServiceProbeRefusesAnEndpointTheSandboxCannotRead` | root `sudo` for the launch's endpoint ACL grant | the grant as the invoking user, who owns the endpoint, as the launch applies it |

The same pass removed every root `sudo` left in the suite's cleanups, diagnostic listings and
process kills, each now the invoking user's act or the sandbox account's through the rule.

> [!NOTE]
> **`4d3248ded` was the first commit to attack this class** — *"test(macos-user): unseal the
> staged overlay fixtures before cleanup, so a non-root run removes them"* — and its shape is
> the one the ruling took: remove the need, don't declare it.

### 3.2 Class B — two tests need a readable unified log store

- `TestMacosUserMacosLogBridgeScopesToTheSandbox`: the user-scope `show` returned nothing
  (`SHOW_RC=77`), so the sandbox account's entry could not be looked for.
- `TestMacosUserSeatbeltProfileEnforcesItsRules`: **only its `macos-log-deny` control.** Every
  confinement subtest in it passed — workspace write, workspace read, staging-root read,
  ancestor stat, same-sandbox process info, pty ioctls, declared-device ioctl, and both
  relocation ancestor stats. The profile itself is not in question; the test cannot establish
  that the host could read the log it then proves the sandbox cannot.

The store is the Mac's policy, so there is nothing to remove. Per [OQ-DM2](#decision-ledger)
both skip countably where the invoking user cannot read it: the log-bridge test whole, and of
`SeatbeltProfileEnforcesItsRules` only the `macos_log_store_read_refused` case
([§5](#5-the-shape)).

### 3.3 Class C — one root-`sudo` probe that read as a missing precondition

`TestMacosUserSweepsAKilledSessionsFiles` reported
*"NOT OBSERVED: the killed session left no env file … or profile … so there was nothing for the
next launch to sweep"* ([`integration/macosuserspawnlock_test.go`](../../integration/macosuserspawnlock_test.go)).
**It is class A, and neither stale nor timing.** Its existence probe was root
`sudo -n /bin/test -e`, from when a session's files were root's. On a Mac without passwordless
root `sudo` that probe answered "absent" for every path, so the control reported a kill that
left nothing. The same probe answered the later sweep check, where "absent" is a pass. A
session's files are now the invoking user's, in the staging root it owns, so a plain `lstat` is
the probe, and the sandbox's sleep is stopped by the sandbox account itself, through the rule.

## 4. Why CI cannot see any of this

| Fact about the GitHub runner | Consequence |
| :--- | :--- |
| Passwordless root `sudo` | class A never appears |
| An admin user with an open log store | class B never appears |
| A clean home: no mise tools, no user config, no installed agents | [§6.1](#61-the-nix-floor-loses-to-a-mise-shim) cannot appear |
| Every nix path substituted from `cache.nixos.org` | Q3 is VOID there, every run |

The workflow is honest about the last one and added a `--rebuild` step to force a real build.
**That step has still never produced an answer on a runner**; it did here
([§7](#7-measurements-this-run-produced)).

## 5. The shape

**The suite's privilege floor is the product's** ([OQ-DM1](#decision-ledger)). The maintainer
ruled it on 2026-10-18: *"I'm very unlikely to grant this for tests ... There's no reason why we
need to get around restrictions like this."* So there is no root-`sudo` probe and no
`requireRootSudo`; the gap [P3](#1-recommendation-and-principles) names is closed rather than
printed.

- **One way into the sandbox account.** A step that acts as `_yolojail` runs through the
  suite's `sandboxCommand`, which builds its argv with the launch's own
  `macosuser.RunAsSandboxArgv` (`/usr/bin/sudo -n --user=_yolojail /usr/bin/env <argv…>`, the
  one rule setup installs) and starts it in `/`, as the launch's runner starts every crossing.
  A test that runs a production argv, such as the service probe's, runs it as built.
- **Everything else is the invoking user's.** Fixture setup, the capture plan's prepare steps,
  the launch's ACL grants, existence checks in the staging root: the launch and `yolo capture`
  run these as the invoking user, so the tests do too.
- **Cleanup never escalates to root.** A fixture the sandbox account wrote into is removed as
  the invoking user, then by that account's own `chmod -R u+w` and `rm -rf` through the rule
  (the guest pass a capture's cleanup runs), then as the invoking user again. What that leaves,
  where the account cannot reach it, is logged with the one command an administrator runs.
- **The guard.** `TestMacosUserSuiteSpendsNoRootSudo` reads every Go file in `integration/` and
  fails on any string literal that is a sudo command name (`sudo`, `/usr/bin/sudo`). It runs in
  the Linux unit gate, so a regression is red before any Mac sees it. Two allowances are named in
  it, and each takes no privilege: the narrow-sudo test's `sudo -k` and its probe that root
  `sudo` must fail, and a stub named `sudo` on a temporary PATH. ⚠ It cannot see a sudo inside a
  shell script's text; no host-side script has one.
- **The System-keychain CA test reads, never writes.** It takes the CAs the launch's own
  composition (`ComposeCATrust`) keeps from the System keychain. Where there are none it skips,
  naming that it only reads the keychain; that includes the GitHub runner.

**The unified log store is a countable skip** ([OQ-DM2](#decision-ledger)). The suite probes
it once per run, as the invoking user, with the seatbelt control's own store read (`log show`
returning an `eventMessage`). Where the store is closed, `requireUnifiedLog` skips the log-bridge
test and the one `macos_log_store_read_refused` case of `SeatbeltProfileEnforcesItsRules`; every
other rule in that test still asserts. Each skip enters the suite's ledger with the policy
reason, as a gate skip does, and a test's own reason is never overwritten by the gate's generic
one.

**Mise shims outrank the floor, as in the jail** ([OQ-DM3](#decision-ledger)): *"parity with the
jail should certainly be the goal, which I think is the mise ones always win."* `SandboxPath`'s
order stays. The floor test asks two lookups per binary: the agent's, on the whole PATH, and the
floor's, on that PATH with every directory under the sandbox's `$HOME` taken out. The sandbox
home is one account home shared by every workspace and test on the Mac, so a shim another
workspace's mise install left is no fault of this launch's; the second lookup is the floor's own
reach, and only it can fail. A mise shim or another home directory winning the first lookup is
logged as by design, and path_helper and Homebrew are blamed only when the second lookup lost. ⚠
The container jail has the same exposure, through the machine-wide `/mise/shims`
([backlog](../plans/BACKLOG.md#stale-mise-shims)).

**A developer-Mac measurement counts** ([OQ-DM4](#decision-ledger)). A line it clears stays
cleared, recording the host and the date and saying `macos-user.yml` has not confirmed it.

**The vacuity check is unchanged.** A declared macos-user run that executes zero tests, or a gate
nothing got through, still exits non-zero.

**What only a Mac can confirm.** Every rewritten test passing on a Mac that grants only the
run-as rule; the CA test on a Mac whose System keychain holds a TLS CA; the guest-pass cleanup;
and the `ps` the jail-daemon test now runs as the sandbox account, whose listing is diagnostic.

## 6. Two defects and one design question this run found

These are the only three failures that are about yolo. None is fixed here.

### 6.1 The nix floor loses to a mise shim

`TestMacosUserFloorReachesTheSandboxPath` fails because `node`, `npm` and `jq` resolve to
`/Users/_yolojail/.yolo/mise/shims/<name>` instead of into the nix profile
([`integration/macosuserfloor_test.go:68`](../../integration/macosuserfloor_test.go)).

**The failure message misdiagnoses it.** It says *"macOS's path_helper or a Homebrew prefix is
ahead of the login rc files' re-prepend"* — but neither won here; the mise shim directory did,
and that directory is `SandboxPath`'s by design. `entrypoint.HomePathDirs` is shared between
`BootPath` and `macosuser.SandboxPath` precisely so *"a mise shim outranks a `~/.local/bin`
tool on every backend"* ([OQ-PD27](program-delivery.md#decision-ledger)). What was never ruled
is whether it outranks **the floor**, and this is the first host with mise tools installed in
the sandbox home to ask. That is [OQ-DM3](#8-open-questions).

### 6.2 A doorway process outlives its session

`TestMacosUserOpensTheAWSDoorwayOutsideTheSandbox` reports *"the doorway survives the session
(pids 81026)"* ([`integration/macosuserdoorway_test.go:211`](../../integration/macosuserdoorway_test.go)),
reproducibly, 14.7 s and 26.7 s on two runs. A host-side process yolo opened for a session was
still running after that session ended. No ruling needed: either the teardown misses it or the
test's wait is too short, and the pid makes it answerable.

### 6.3 Housekeeping reaps nothing and writes no log

`TestMacosUserHousekeepingReapsRetiredLoopholeStateDuringTheSession`: after the session the
retired generations were all still present, and
`<ws>/.yolo/housekeeping.log` did not exist
([`integration/macosuserhousekeeping_test.go:65`](../../integration/macosuserhousekeeping_test.go)).
⚠ The generation list it printed includes timestamps minted by *this suite's own earlier
tests*, so shared state between tests is a live alternative explanation and should be ruled out
first.

## 7. Measurements this run produced

**Five claims the corpus carries as `NOT MEASURED on a Mac` passed**, on the host of record:

| Test | Claim it clears |
| :--- | :--- |
| `TestMacosUserSeatbeltSealedBuildProfileDeniesTheLoopbackAndTheNixDaemon` | [FP-D24](forked-programs-as-packs.md#FP-D24) |
| `TestMacosUserHostFloorBuildsAForkFixtureForTheMac` | [FP-D24](forked-programs-as-packs.md#FP-D24) |
| `TestMacosUserLaunchBuildsAndRunsAPatchedFork` | [`macosuserpatchedfork_test.go`](../../integration/macosuserpatchedfork_test.go), [§9](../reference/patched-forks.md) |
| `TestMacosUserLaunchRefusesAConflictingSeriesAndNamesTheHatches` | as above |
| `TestMacosUserCachedGoodServesTheDarwinGoodBuild` | as above |

**The cache-relocation measurement, with volumes attached** (APFS with ownership *disabled*,
and ExFAT; an ownership-*enabled* APFS volume needs `diskutil enableOwnership`, which needs
root, so that third case is still unmeasured):

- **Both delivered.** The sandbox wrote through the link and the file was at the target on both
  volumes ([CX-D5](../reference/context-mounts.md#CX-D5), [OQ-CX8](../reference/context-mounts.md#oq-cx8)).
- **The grant is per-target, not per-volume.** Reading a *sibling* directory on the same volume
  was refused under the profile on both — including on ExFAT, where the target
  *"was NOT granted (the volume took no access entries)"* and delivery still worked. The
  confinement holds where the filesystem cannot help it.

**Every vendor-install cell passes on darwin — all nine, the whole `install` matrix of
[`macos-user.yml`](../../.github/workflows/macos-user.yml).** The `via: npm` half had never run
on a Mac: `opencode` (29.4 s), `pi` (29.1 s), `omp` (19.1 s), `chrome-devtools` (6.4 s). The
`via: installer` half followed: `claude` (71.7 s), `copilot` (28.7 s), `codex` (30.3 s), `agy`
(22.5 s), `muse-code` (56.6 s). Each installs the vendor's **darwin** build through the generated
launcher into the sandbox account's home and probes `--version` — the platform no other CI job
installs.

> [!NOTE]
> **Three packs declare a `project_dirs` row the installed bundle does not witness**, logged by
> each cell without failing it: `opencode` (`.claude/skills`, `.agents/skills` — named only as
> home dirs), `copilot` 1.0.88 and `codex` 0.157.0 (`.agents/skills`). Informational in this
> test, and worth a look on its own: a declaration no bundle backs is a surface yolo renders for
> nobody.

**Q3, answered for the first time** ([`macos-user-build-step-threat-model.md`](macos-user-build-step-threat-model.md)):
`nixpkgs#jq^*` rebuilt with `--option sandbox true` **built under the sandbox** in 91 s;
nothing was refused by the sandbox policy; the output differs from the substituted one
(`may not be deterministic`), which that doc's own classification calls a reproducibility
difference and not a sandbox refusal. The sandbox setting was **honoured** rather than ignored,
because this host's `trusted-users` includes the invoking user — the condition CI cannot
guarantee. The floor build itself substituted every path and so remains VOID, exactly as on the
runner.

## 8. Open questions

None. All four were ruled on 2026-10-18 and are compacted into the ledger below.

### Decision Ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| OQ-DM1 | **The suite never needs passwordless root `sudo`** (option B, remove the need). The maintainer: *"I'm very unlikely to grant this for tests ... There's no reason why we need to get around restrictions like this."* Every privileged step goes through what yolo itself installs, the one run-as rule, built by `macosuser.RunAsSandboxArgv`, or is the invoking user's own; a capture plan's prepare steps run exactly as production runs them; the CA test uses a CA the System keychain already holds and never adds one; a guard test fails on a sudo the suite spells itself | 2026-10-18 | [§3.1](#31-class-a--six-tests-spent-a-privilege-yolo-macos-setup-does-not-install), [§3.3](#33-class-c--one-root-sudo-probe-that-read-as-a-missing-precondition), [§5](#5-the-shape) | ✅ not yet run on a Mac |
| OQ-DM2 | **An honest skip.** The host's unified log store being closed is the Mac's policy: probe it once (`log show` as the invoking user); the log-bridge test skips naming the policy; `SeatbeltProfileEnforcesItsRules` skips only its `macos-log-deny` control; both skips are counted in the suite's ledger, as the gate's are | 2026-10-18 | [§3.2](#32-class-b--two-tests-need-a-readable-unified-log-store), [§5](#5-the-shape) | ✅ not yet run on a Mac |
| OQ-DM3 | **Mise shims outrank the floor, for parity with the jail** (option B). The maintainer: *"parity with the jail should certainly be the goal, which I think is the mise ones always win."* `SandboxPath`'s order stays; the floor test asks for the floor's reach past the sandbox home's shared directories, and blames path_helper or Homebrew only when that lost | 2026-10-18 | [§5](#5-the-shape) | ✅ not yet run on a Mac |
| OQ-DM4 | **A developer Mac clears a `NOT MEASURED on a Mac` line, with provenance** (option A). The maintainer: *"I can guess we can count it."* The lines [§7](#7-measurements-this-run-produced) cleared stay, each recording the host and the date and saying `macos-user.yml` has not confirmed it | 2026-10-18 | [§5](#5-the-shape) | ✅ |

## 9. What this does not cover

- **The narrow-sudo cell.** CI narrows `/etc/sudoers.d` to the enrolled rule and re-runs three
  tests. That edits a machine's privilege configuration and needs root, so it was not run here;
  it is not part of this proposal.
- **Whether any class A or B test is itself correct.** This doc classifies why they could not
  run, not what they assert.
- **`internal/cli`'s runtime**, which belongs to [`test-suite-speed.md`](../plans/test-suite-speed.md).
  ⚠ It is worth naming here anyway, because it blunts [P4](#1-recommendation-and-principles):
  **`just done` cannot go green on this Mac at all.** Measured 2026-10-18 on a docs-only commit —
  the gate ran the full `just check-ci` (no verified ancestor for this toolchain) and
  `internal/cli` panicked at `test timed out after 20m0s`, `1201.046s`, with no `--- FAIL` of its
  own; every other package passed, including `internal/logcap` and `internal/packsrc`, both of
  which were red here two days earlier and have since been fixed. So a developer Mac can run the
  macos-user suite but not the landing gate, and the two problems are independent.

## 10. Alternatives considered

| Alternative | Verdict |
| :--- | :--- |
| Grant the developer Mac passwordless root `sudo` so the suite passes | **Rejected.** It makes the measuring instrument less confined than the thing measured, and [P3](#1-recommendation-and-principles) is the opposite. |
| Delete the six root-`sudo` tests | **Rejected.** They assert real confinement; the privilege is in their fixtures, not their claims. |
| One `requireMacosUserPrivileged(t)` covering both new facts | **Rejected for now.** The two have different dispositions — root `sudo` should shrink ([OQ-DM1](#8-open-questions)), the log store never will — so one reason would hide which. |
| A `paths:`-filtered PR trigger for `macos-user.yml` | **Out of scope here**, and the workflow already explains why it is not wired: nobody has measured what a first macos-user launch costs. |

## 11. Sequencing

1. Rule [OQ-DM1](#8-open-questions) and [OQ-DM2](#8-open-questions). Both are small and both
   block a clean local verdict.
2. Add the two probes and their skip reasons beside the existing five facts; label them in the
   ledger. One commit, no assertion touched.
3. Take [§6.2](#62-a-doorway-process-outlives-its-session) and
   [§6.3](#63-housekeeping-reaps-nothing-and-writes-no-log) as ordinary defects, the latter after
   ruling out cross-test state.
4. Rule [OQ-DM3](#8-open-questions); fix whichever side it condemns, and fix the failure
   message either way — it names two causes that were both innocent here.
5. Keep removing root-`sudo` fixtures in the direction `4d3248ded` set.

## 12. What done looks like

- On a Mac with only the run-as grant, `-run '^TestMacosUser'` **exits 0**, and the ledger
  prints the two new skip reasons with their counts.
- The same command on CI still executes every one of those tests — the skip is the host's
  answer, never the suite's default.
- `TestMacosUserSeatbeltProfileEnforcesItsRules` asserts its nine runnable rules on a host with
  no readable log.
- The three items in [§6](#6-two-defects-and-one-design-question-this-run-found) are each
  closed, re-classified, or carried as a named open defect.
