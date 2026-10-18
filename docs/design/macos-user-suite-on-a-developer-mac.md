---
title: "What may a macos-user test assume about its host?"
date: 2026-10-18
status: in-review
stage: DESIGN
next: "Rule OQ-DM1 and OQ-DM2 (the two undeclared privileges), then OQ-DM3 (floor vs mise shim); the two defects in §6 need no ruling"
tags: [design, macos-user, testing, privilege, seatbelt, measurement]
summary: "The macos-user suite's gate probes one privilege — the run-as rule a launch needs — but twelve of its 148 tests need two more that no gate names: passwordless root sudo, and a readable unified log store. On the GitHub runner both are free, so the suite is green there and the dependency is invisible; on a Mac that grants yolo only what yolo asks for, those twelve FAIL rather than skip countably, and a developer cannot tell a broken change from an unprivileged host. The first full run on such a Mac is recorded here: 128 passed, 3 skipped, 12 failed, of which nine are the host's privileges and three are candidate defects."
---

# What may a macos-user test assume about its host?

**Status:** DESIGN, 2026-10-18. Nothing built. Evidence measured on this machine
2026-10-17 (full suite, at `245a2203d`) and 2026-10-18 (the twelve re-run, at `4d3248ded`).
Host of record: macOS 26.5, arm64, uid 501, `_yolojail` present, `/Users/Shared/yolo`
present, the run-as grant passwordless after `sudo -k`, **root `sudo` not passwordless**,
and `log show` refused.

> **In short.** The suite gates on the one privilege a macos-user *launch* needs and then
> quietly spends two more that only a CI runner hands out. That is not a test-tidiness
> problem: it means the backend's confinement claims are measured exclusively on a host far
> more privileged than the backend asks for, and nobody has checked whether that matters.

**Why it matters.** This suite is the only instrument for a darwin-only, privilege-bearing
backend, and [`integration/macosusergate_test.go`](../../integration/macosusergate_test.go)
opens by saying why it exists: *"A SUITE THAT SKIPS MUST NOT BE INDISTINGUISHABLE FROM A SUITE
THAT PASSES."* The inverse now holds and is unguarded — a suite that FAILS for a privilege it
never declared is indistinguishable from a suite that caught a regression. Twelve tests, every
run, deterministically.

**The shape.** One gate today (`requireMacosUser`, probing five host facts). The proposal adds
the two privileges the body of the suite actually spends as *declared* preconditions beside it,
each with the same countable skip, so the ledger says `skipped=N (no root sudo)` instead of a
red run.

**Cost.** Three behaviours stop being measured anywhere but a privileged host. That is already
true; stating it is the change.

**Start at [§3](#3-the-three-classes)** — the classification is the whole argument;
[§5](#5-the-proposed-shape) and [§6](#6-two-defects-and-one-design-question-this-run-found) fall out
of it.

**Needs your ruling:** [OQ-DM1](#8-open-questions), [OQ-DM2](#8-open-questions),
[OQ-DM3](#8-open-questions), [OQ-DM4](#8-open-questions).

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

### 3.1 Class A — six tests need passwordless root `sudo`

Each fails with `sudo: a password is required`, in a fixture or a *control* probe rather than
in the behaviour under test.

| Test | What needs root |
| :--- | :--- |
| `TestMacosUserCaptureSeatbeltProfileDeniesTheSharedHome` | `sudo /bin/rm -rf` of a staged capture tree |
| `TestMacosUserTrustsSystemKeychainCA` | `security add-trusted-cert` into the System keychain, and its cleanup |
| `TestMacosUserHostFloorIsTheHostUsersAlone` | the **control** probe, which must first prove `_yolojail` *can* list an ancestor |
| `TestMacosUserLaunchDeniesReadOfHost0644File` | fixture setup |
| `TestMacosUserMacosLogAsTheSandboxAccountMeasurement` | fixture setup (also class B) |
| `TestMacosUserServiceProbeRefusesAnEndpointTheSandboxCannotRead` | fixture setup |

> [!NOTE]
> **`4d3248ded` is the first commit to attack this class** — *"test(macos-user): unseal the
> staged overlay fixtures before cleanup, so a non-root run removes them"* — and it is the
> right shape: remove the need, don't declare it. It did not move any of the six above.

### 3.2 Class B — two tests need a readable unified log store

- `TestMacosUserMacosLogBridgeScopesToTheSandbox`: the user-scope `show` returned nothing
  (`SHOW_RC=77`), so the sandbox account's entry could not be looked for.
- `TestMacosUserSeatbeltProfileEnforcesItsRules`: **only its `macos-log-deny` control.** Every
  confinement subtest in it passed — workspace write, workspace read, staging-root read,
  ancestor stat, same-sandbox process info, pty ioctls, declared-device ioctl, and both
  relocation ancestor stats. The profile itself is not in question; the test cannot establish
  that the host could read the log it then proves the sandbox cannot.

The test's own comment anticipates this (*"the runner is an admin, so …"*), which is the
dependency being noticed but not declared.

### 3.3 Class C — one test could not reproduce its own precondition

`TestMacosUserSweepsAKilledSessionsFiles` reports
*"NOT OBSERVED: the killed session left no env file … or profile … so there was nothing for the
next launch to sweep"* ([`integration/macosuserspawnlock_test.go:934`](../../integration/macosuserspawnlock_test.go)).
Either the kill no longer leaves those files — in which case the sweep has nothing to do and
the test is stale — or this host's timing differs from the runner's. **It is not classified
until somebody looks**, and it is the one failure that might be hiding a product change.

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

## 5. The proposed shape

Two new host facts, probed where the other five are, each with a skip reason and a ledger
label — not a new gate function per privilege, and not a new reporting channel.

- **`rootSudoQuiet`** — `sudo -n /usr/bin/true`. A test that needs it calls
  `requireRootSudo(t)` after `requireMacosUser(t)`; the skip reason names what the test would
  have done and that `yolo macos-setup` deliberately does not grant it.
- **`unifiedLogReadable`** — `log show --last 1m --style json` returning an `eventMessage`.
  `requireUnifiedLog(t)` likewise, and for `SeatbeltProfileEnforcesItsRules` it gates **the one
  subtest**, so the other nine still assert.

**The vacuity check keeps working unchanged.** A declared macos-user run that executes zero
tests still exits non-zero; what changes is that `skipped=12` can now carry
`(no root sudo)` and `(no readable unified log)` instead of a red run with no verdict.

**Where the privileges are spent is itself a result.** Six tests needing root is a number worth
printing: it is the gap between what the suite asks of a host and what the backend asks, and
P3 says that gap should shrink. `4d3248ded` shrank it by one fixture pattern.

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

### 6.2 Another session's doorway was read as this session's survivor

`TestMacosUserOpensTheAWSDoorwayOutsideTheSandbox` reported *"the doorway survives the session
(pids 81026)"*, on both runs. A *doorway* is
[HS-D15](host-notch-services.md#HS-D15)'s word for the thin host-side adapter an agent's client talks
to, which checks the launch's caller token and forwards to a credential service.

**The teardown is right; the check was not.** On macos-user the workspace's keeper opens the
doorway, stops it when the last session leaves (`endKey`), and the last session's quit waits
for that keeper to be gone before the launch returns (`streamKeeperTeardown`), so the test's 10 s
wait is slack rather than the bound. What failed was the check's selection: it ran
`pgrep -f "internal daemon aws-credential-adapter --listen"` machine-wide, and every AWS doorway
has that argv whichever launch opened it, short of its port. A developer who runs Claude on
Bedrock through `yolo host -- claude`, or in another macos-user workspace, holds one for as long
as that session runs. CI's runner has none, which is why only a developer Mac failed. That
reading is inferred from the code: the run did not record the surviving process's port or start
time.

**Fixed in the test.** The doorway checks now judge only the pids the launch itself named on its
`Opened the … doorway (pack …, pid N)`, `Started the … service (pack …, pid N)` or
`… is back (pid N)` lines. A survivor of this launch's still fails, and another process with the
same argv is printed beside it as "not this launch's, so not judged". The codex doorway and the
two wire-bridge tests share that check. `doorwaysightings_test.go` runs the §6.2 shape under
`-short` on Linux.

**The Mac re-runs** `-run '^TestMacosUserOpensThe(AWS|Codex)DoorwayOutsideTheSandbox$'`. A pass
closes this item. A failure now prints the survivor's full listing line, with its uid and
`--listen` port, which tells a leak in the teardown apart from another session's doorway.

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

1. 💬 **OQ-DM1: Should passwordless root `sudo` become a declared precondition, or should the six tests stop needing it?**

   This is the closure question for the whole doc: it decides whether a developer Mac is a
   supported instrument for this suite or only CI is. [P3](#1-recommendation-and-principles)
   leans toward removing the need where a fixture can be unsealed instead, which `4d3248ded`
   has started.

   - **A — Declare it.** `requireRootSudo(t)`, countable skip, ledger label. Cheap; the six keep
     measuring on CI; the gap is printed every run.
   - **B — Remove the need.** Rewrite each fixture to work with the run-as grant alone, as
     `4d3248ded` did for the staged overlay. Strictly better, unbounded work.
   - **C — Keep failing.** Treat a Mac without root `sudo` as unsupported for the suite.

   <!-- vantage: question id=OQ-DM1 leaning="A now, B as the standing direction: declare it this week so a developer Mac gives a clean verdict, and keep removing the need fixture by fixture." -->

   _Leaning:_ A now, B as the standing direction — declare it so a developer Mac gives a clean
   verdict immediately, and keep removing the need one fixture at a time. C costs the project
   its only non-CI instrument.

   **Answer:**
   > _(empty — fill in when decided)_

2. 💬 **OQ-DM2: Is a readable unified log store a precondition, and does it gate a whole test or one subtest?**

   Decides whether `SeatbeltProfileEnforcesItsRules` — nine passing confinement assertions —
   stays red on every managed Mac. The log store is the host's policy, not yolo's, so there is
   nothing to remove here, unlike [OQ-DM1](#8-open-questions).

   - **A — Probe it; gate the one `macos-log` subtest.** The other nine keep asserting.
   - **B — Probe it; skip the whole test.** Simpler, loses nine assertions on such a host.

   <!-- vantage: question id=OQ-DM2 leaning="A — the control is one subtest, and a managed Mac should still get the other nine confinement assertions." -->

   _Leaning:_ A. The control belongs to one subtest; a managed Mac should still measure the
   other nine rules.

   **Answer:**
   > _(empty — fill in when decided)_

3. 💬 **OQ-DM3: On macos-user, does the nix floor outrank a mise shim, or the other way round?**

   [§6.1](#61-the-nix-floor-loses-to-a-mise-shim) is a red test either way; which thing is
   wrong depends on this. [OQ-PD27](program-delivery.md#decision-ledger) ruled mise shims above
   `~/.local/bin` and said nothing about the floor, and `sandboxpathorder_test.go` pins the
   order as written rather than as ruled.

   - **A — The floor outranks the shims.** The floor is what the backend guarantees; a workspace
     `mise_tools` entry should not silently replace the `node` yolo provisioned. Product fix.
   - **B — The shims outrank the floor, as today.** A user who declares a tool means it. Then
     the test is stale and its message is wrong twice.

   <!-- vantage: question id=OQ-DM3 leaning="B for a tool the config declares, A for a floor name the config never mentioned; the test currently asks about jq, which nothing declared." -->

   _Leaning:_ Split it — B for a tool the workspace actually declares, A for a floor name the
   config never mentioned. The failing probe is `jq`, which nothing in this config declares,
   so today's behaviour is wrong under either half.

   **Answer:**
   > _(empty — fill in when decided)_

4. 💬 **OQ-DM4: Does a developer Mac clear a `NOT MEASURED on a Mac` claim, or only a CI run?**

   [§7](#7-measurements-this-run-produced) clears five such claims and answers Q3. If only CI
   counts, those lines stay as they are and this doc is the record instead. The host here is
   *managed* (a closed log store, an MDM CA), so it is not a neutral Mac — but it is a real
   one, and it measured things CI cannot.

   - **A — Yes, with provenance.** The line records the host and the date, and says CI has not
     confirmed it.
   - **B — No.** Only `macos-user.yml` clears such a line; a local run is evidence in a design
     doc.

   <!-- vantage: question id=OQ-DM4 leaning="A with provenance — a measurement nobody records is one somebody repeats, and the provenance makes the weaker claim visible." -->

   _Leaning:_ A, with the host named. A measurement nobody writes down gets repeated, and
   naming the host keeps the weaker claim honest. I have written the five lines this way
   already; reverting them is one commit if you rule B.

   **Answer:**
   > _(empty — fill in when decided)_

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
3. Take [§6.2](#62-another-sessions-doorway-was-read-as-this-sessions-survivor) and
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
