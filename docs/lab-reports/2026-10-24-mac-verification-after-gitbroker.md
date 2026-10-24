---
title: "Mac verification of what landed 2026-10-23 to 2026-10-24: the self-mode build witness, three darwin unit failures, and the rustls mise build cost"
date: 2026-10-24
status: open
tags: [lab-report, macos, macos-user, seatbelt, gitbroker, testing, evidence]
summary: "One debug-only session on the maintainer's Mac: the self-mode pack-tree build failure has its witness and a cause read from the code, and a build failure's text then refuses the whole self-mode launch; three new unit tests fail only under macOS's /var/folders TMPDIR; the cold rustls mise build (27 min) blows a macos-user test's 30-minute budget; and the git broker passes in a real rootless podman jail, once the image is delivered by hand because `just load` cannot do it on darwin."
---

# Mac verification, 2026-10-24

A [lab report](README.md). Host: the maintainer's Mac, macOS 26.5 (25F71), arm64, Command Line Tools only (no
Xcode.app), Apple Container 1.1.0, podman 6.1.4 on applehv (rootless, `pasta`). Source: `origin/main` at
`0291b3bc7`, which covers the commits since the previous Mac report
([2026-10-23](2026-10-23-native-mac-verification.md), last commit `21724526c`). The session ran from Claude
Code directly, **not** under `yolo host`, with no `YOLO_*` variable inherited. **This session was told to
debug and document only, so nothing below is fixed.** No agent was started by a test, and no credential or
API call was used.

## 1. The self-mode pack-tree build: witness collected, cause read from the code

The previous report left
`TestMacosUserPackTreeProgramBuildsOnceAndRebuildsOnChange/self` and
`TestMacosUserPackTreeProgramOverrideBuildsTheCheckout/self` failing with *"capture bootstrap failed — the
staging home has no generated launcher"* and no cause. The refusal now carries the bootstrap's stderr.

**Run:** `YOLO_TEST_MACOS_USER=1 go test -count=1 -timeout 0 -v -run '^TestMacosUserPackTreeProgram' ./integration`,
with the tool profile already built (see [section 3](#3-the-rustls-mise-costs-27-minutes-cold-and-a-macos-user-test-has-30)).

| Test | account | self |
| :--- | :--- | :--- |
| `…BuildsOnceAndRebuildsOnChange` | PASS (16.3 s) | FAIL (6.0 s) |
| `…OverrideBuildsTheCheckout` | PASS (16.4 s) | FAIL (5.9 s) |
| `…BuildFailureRefusesOnlyIt` | PASS (16.1 s) | **FAIL (9.4 s)**, passed on 2026-10-23 |
| `…UsesThePrebuiltWithoutSources` | SKIP | SKIP |

### 1a. The witness

Every self-mode build jail exits 1 before its build line, and its stderr ends with:

```text
yolo internal darwin-bootstrap: cannot read the sandbox account's home /Users/_yolojail (stat /Users/_yolojail: operation not permitted); this command runs only as that account, inside a macos-user launch — run `yolo` in the workspace instead
```

That message is `darwinBootstrapIdentityRefusal` (`internal/cli/internal.go:506`). It stats
`darwinBootstrapBoundaryHome()`, which is the run-as-you real home when `paths.GuestRealHome()` returns one,
and **otherwise the second account's home, `/Users/_yolojail`** (`internal/cli/internal.go:458`). So in self
mode the bootstrap took the account-mode branch.

### 1b. Why, as read from the code (not yet confirmed by a fix)

`paths.GuestRealHome` (`internal/paths/workspacescope.go:178`) returns `""` unless `YOLO_DARWIN_REAL_HOME` names
a directory the process's euid owns.

- **A launch's bootstrap sets it.** `internal/macosuser/runplan.go:810`, under `if self`, sets
  `paths.DarwinRealHomeEnv` to `homes.Real`, along with the machine and shared homes and `MISE_DATA_DIR`.
- **A capture's bootstrap does not.** `internal/macosuser/capture.go:386` builds `bootstrapEnv` with
  `buildBootstrapEnv(...)`, and the self-mode branch at `capture.go:426` changes the profile, the staging
  commands and the argv, but sets no run-as-you home variable. `rg DarwinRealHomeEnv internal/macosuser`
  finds no use in `capture.go`.
- **The plan check does not require it.** `selfCapturePlanProblems` (`capture.go:908`) checks that
  `plan.RunAs.Homes.Real` is absolute, but not that the bootstrap env passes it on.

Two things about a fix are predictions that this session did not test:

- With the variable set, `GuestRealHome` has to `os.Stat` the real home from inside
  `SeatbeltSelfCaptureProfile`, which is deny-by-default. If that profile refuses metadata reads on
  `$HOME`, the variable alone changes nothing.
- `darwinBootstrapSelfHomeRefusal` (`internal/cli/internal.go:470`) runs once a real home is named. It
  requires `HOME`, `JAIL_HOME` and the sidecar to equal `<YOLO_DARWIN_WORKSPACE>/.yolo/home`. A capture's
  `HOME` is its staging home, so that check may be the next refusal.

### 1c. A self-mode build failure now refuses the whole launch, hatch included

The build failure should cost only the one program. Instead every self-mode launch was then refused with:

```text
macos-user run plan is not viable:
  ✗ a run-as-you argv names the second account _yolojail: /usr/bin/env -i HOME=… YOLO_FORK_BUILDS={"mupacktree-x":{"reason":"… was not started: …","cause":{"lines":["The end of its stderr:","yolo internal darwin-bootstrap: cannot read the sandbox account's home /Users/_yolojail …
```

The launch argv carries the failed build's description in `YOLO_FORK_BUILDS`, and that text quotes the
bootstrap's stderr, which names `/Users/_yolojail`. The self-mode viability check
(`internal/macosuser/runplan.go:2143`) refuses any argv word that **contains** the substring `_yolojail`, so
a diagnostic string counts as naming the account. This is why
`BuildFailureRefusesOnlyIt/self` went from PASS to FAIL: the test fails with *"the refusal does not name the
hatch"* and then *"rc 1: the hatch did not start the sandbox"*. `YOLO_ALLOW_MISSING_PROGRAMS=1` cannot get
past a plan refusal.

It is a separate defect from 1b. Any self-mode build or capture failure whose text mentions the account
would trigger it, and fixing 1b only removes today's trigger.

**Decision needed:** whether the viability check should look only at the argv words that run something
(the program and its flags, and the env keys that name paths), or whether build diagnostics should stay out
of the launch argv altogether.

## 2. Three unit tests fail only under macOS's TMPDIR

`go test -short -count=1` over every package the new commits touched, except `internal/cli/run` and
`internal/entrypoint` (see [section 6](#6-not-done-and-why)). Everything passed except these three. **All
three pass with `TMPDIR=/private/tmp/claude-501/ytmp` or `TMPDIR=/tmp/`, and fail every time with the default
`/var/folders/j6/…/T/`.** None of them is a product defect.

| Test | Failure | Cause |
| :--- | :--- | :--- |
| `internal/hostrun` `TestWaitUnnamedTakesOnlyANewRun` (`wait_test.go:111`) | prints the correct `fresh` result, then fails | It asserts `!strings.Contains(out, "old")`, and the output's `full output:` line holds the temp path, where `/var/**folders**/` contains `old`. |
| `internal/gitbroker` `TestCredentialFillRunsInAnEmptyBrokerDir` (`https_test.go:422`) | `fill ran in "/private/var/folders/…/broker/git/credential-…", not a directory under "/var/folders/…/broker/git"` | The darwin path-resolution class that AGENTS.md describes. `newFixtureOpts` sets `HOME` to a bare `t.TempDir()` (`broker_test.go:171`) and resolves `root` two lines later. The fake git records `pwd`, and because the broker passes an explicit `cmd.Env` with no `PWD`, that is the resolved `/private/var/…`. |
| `internal/config` `TestAPlainReadKeepsNoRecord` (`sources_test.go:463`) | `LoadConfig allocates 6198 per read, LoadConfigWithSources 6850` | The test requires `plain < 0.9 × recorded`, and 6198/6850 = 0.905. It is length or depth, not the symlink: the resolved `/private/var/folders/…/T/` fails too (6191 against 6836). *Hypothesis:* allocations that grow with the number of path components (an ancestor walk) are paid by both readers, so a deeper temp dir moves the ratio toward 1. That was not measured. |

**Decision needed:** whether to fix each test where its path is created (AGENTS.md's rule: `EvalSymlinks(t.TempDir())`
at the mint for the second; a check that cannot match a path for the first; a fixed-depth workspace or a
delta rather than a ratio for the third).

## 3. The rustls mise costs 27 minutes cold, and a macos-user test has 30

`bfe6a81b3` builds the macOS tool profile's mise from source with rustls. On a Mac where no earlier run had
built it, the first macos-user launch in the suite paid for that build inside its own budget:

- `TestMacosUserPackTreeProgramBuildsOnceAndRebuildsOnChange/account` failed after 30 m with
  `yolo timed out after 30m0s` (`macosuserprograms_test.go:152`), killed while cargo was still compiling
  mise's last crates (`rattler_solve`, `aws-sdk-s3`). The killed build stored nothing, so the `/self` subtest
  started the same build from the beginning. This session stopped the run at that point.
- A separate build, with nothing else running and no timeout, took **1632 s** (27.2 min):
  `nix build --impure --no-link 'path:.#packages.aarch64-darwin.yoloNoncontainerProfile'`, ending in
  `/nix/store/c4xkimsmafy3as7qyq6nli4qx3m4xwvp-yolo-noncontainer-profile`. Once that build existed, each
  test's `Building the sandbox's tools with nix` step took 2.1 s.

For about 10 of the timed-out test's 30 minutes, the progress line stopped while `nix-daemon __build-remote 3`
was the only child the harness's dump shows. The output for those minutes was not kept, so whether the remote
builder (`ssh-ng://builder@linux-builder aarch64-linux`) delayed an `aarch64-darwin` build is not known.

**Decision needed:** whether the macOS CI job has the rustls mise in a binary cache. If it does not, its first
macos-user test after any change to mise's derivation will time out the same way. That is the misattribution
`warmJail`'s comment (`integration/harness_test.go:188`) describes, and `warmJail` returns early on darwin.

## 4. Git broker on a real rootless podman jail

AGENTS.md says a nested jail cannot settle reachability, so the git broker was run in a real jail:
rootless podman on the Mac (`podman info`: `Rootless true`, `RootlessNetworkCmd pasta`).

**All three pass:** `YOLO_RUNTIME=podman go test -count=1 -timeout 0 -v -run '^TestGitBroker' ./integration`

| Test | Result |
| :--- | :--- |
| `TestGitBrokerFetchAndRefusal` | PASS (9.7 s) |
| `TestGitBrokerOffLeavesGitUntouched` | PASS (6.7 s) |
| `TestGitBrokerCreateBranch` | PASS (22.3 s): grant, approve, revoke with `--workspace`, and three launches |

Getting there turned up two problems that are not in the broker.

### 4a. On darwin podman, the stale-image refusal names commands that cannot work

The first attempt was refused with `STALE JAIL IMAGE`. Both fix commands it prints for podman, `YOLO_RUNTIME=podman
just load` and the hand-written `go run ./cmd/yolo internal image-copy --runtime podman …`, fail on this Mac
within 7 s:

```text
yolo internal image-copy: writes podman's store on Linux only (runtime "podman" on darwin takes an archive, as a launch does)
error: recipe `load` failed with exit code 1
```

The `load` recipe (`justfile:168`) sends only `container` down the archive branch. Every other runtime,
including podman on darwin, goes to `image-copy`, which refuses there. The image was delivered by hand for this
session, the way the recipe's container branch does: `skopeo copy nix:… oci-archive:…`, then `podman load -i`.
AGENTS.md's [Workflow](../../AGENTS.md#workflow) step 1 and the refusal itself both point at `just load`.

**Decision needed:** whether the darwin podman case belongs in the `load` recipe, in `image-copy`, or in the
refusal's text. As it stands, AGENTS.md's rule that every stop names its next step is broken here.

### 4b. A false "no outbound network" warning on every launch

Every launch in this run printed `warning: this jail has no outbound network access` with `github.com:443: the
name did not resolve (DNS: lookup timed out)`. The jail was online. Measured in a fresh container from the
same image:

```text
169.254.1.1   rc=1 4044ms 1 answers      (nslookup -timeout=10 github.com <ns>)
192.168.127.1 rc=1 4061ms 1 answers
curl https://github.com → 200
```

The egress probe's whole budget is `egressBudget = 3 * time.Second` (`internal/entrypoint/egress.go:73`), so a
4 s first lookup through either nameserver counts as no network. This Mac was on the AWS VPN client, the same
setup as [the pants DNS report](2026-10-19-pants-dns-under-vpn.md). `nslookup` exits 1 after returning one A
answer, which fits that report's AAAA failure, but the AAAA answer itself was not captured. Where the 4 s goes,
whether pasta, gvproxy, or the VPN's resolver, was not measured.

**Decision needed:** whether a DNS timeout under the probe's budget should be reported as "slow" rather than
"no network".

## 5. Smaller observations

- **`TestMacosUserDenyDefaultProfileEnforcesItsRules` passes**, including the new
  `user_preferences_read_refused` case. Its new `xcode_shim_make_runs` case **skips** here: this Mac has only
  the Command Line Tools, so `/Library/Preferences/com.apple.dt.Xcode.plist` does not exist. `9568b866b`'s
  Xcode license allow is still unmeasured on hardware with Xcode installed.
- **The unified log store is closed to this user.** `macos_log_store_read_refused` skips with `log: Could not
  open local log store: Operation not permitted` (a managed Mac). So the previous report's witness command,
  `log show … 'sender == "Sandbox"'`, cannot collect Seatbelt denies on this Mac without an admin.
- **`ps -E` shows no child environment outside `yolo host` too.** The previous report could not tell whether
  `TestStopHomeProcesses*` failed because of the session it ran in or because of macOS 26.5. In this session
  (Claude Code's own shell, no `yolo host`, `$HOME` writable, so not Seatbelt-confined),
  `HOME=/tmp/probe sleep 30 & ps -E -ww -o command= -p $!` printed no `HOME=`, and `ps -E` on the shell
  itself printed only its argv. That rules out yolo's host layer. A plain Terminal run is still the remaining
  control.
- **Six orphaned `signal-nix` loops** (`/bin/sh -c trap 'touch "$1"; exit 0' INT; … while :; do sleep 1;
  done signal-nix …/TestSignalExitRemovesSelectedPackTree…/tmp-SIG{INT,HUP,TERM}/…`) were left with ppid 1
  from two earlier runs that started at 2026-10-23 16:58. They come from the stand-in nix in
  `internal/cli/check/signalcleanup_test.go:284`. This session's run of that package passed and left none, so
  the leak was not reproduced. The test's own comment says a failed case's deferred SIGKILL skips every
  cleanup by design. These loops were left running.
- **AGENTS.md is stale on image skew.** Its Testing section says the stale-image check auto-downgrades on
  darwin. `integration/imageskew_test.go:60` says *"THE DOWNGRADE IS GONE"*, and this session's podman run
  was refused (`STALE JAIL IMAGE`), not warned.

## 6. Not done, and why

- `internal/cli/run` and `internal/entrypoint` short suites: commits touched them, but the previous report
  measured them at about 1000 s and 655 s on this Mac. The machine was busy with the mise build and the
  integration runs.
- The full macos-user suite: only the pack-tree and deny-default tests ran.
- `just done`: this session adds only this report, so it has no code to gate.

## 7. Evidence

All runs were against `0291b3bc7`.

```text
go test -short -count=1 ./internal/{brokeraudit,brokercore,brokerscope,brokerstore,cli/check,config,ghbroker,
  gitbroker,gitproto,gitremote,hostrun,hostwrap,internaldaemon,loopholedecl,loopholes,macosuser,packload,
  packsrc,render} ./packs/...
→ FAIL internal/config (TestAPlainReadKeepsNoRecord), internal/gitbroker
  (TestCredentialFillRunsInAnEmptyBrokerDir), internal/hostrun (TestWaitUnnamedTakesOnlyANewRun); all others ok

YOLO_TEST_MACOS_USER=1 go test -count=1 -timeout 0 -v -run '^(TestMacosUserPackTreeProgram|TestMacosUserDenyDefault)' ./integration
→ DenyDefault: PASS (xcode_shim_make_runs, macos_log_store_read_refused SKIP)
→ PackTree …/account: yolo timed out after 30m0s (cold rustls mise build); run stopped

nix build --impure --no-link 'path:.#packages.aarch64-darwin.yoloNoncontainerProfile'   real 1631.81

YOLO_TEST_MACOS_USER=1 go test -count=1 -timeout 0 -v -run '^TestMacosUserPackTreeProgram' ./integration
→ account ×3 PASS, self ×3 FAIL (section 1), UsesThePrebuiltWithoutSources SKIP ×2

YOLO_RUNTIME=podman just load                         → exit 1 (section 4a)
skopeo copy nix:… oci-archive:… && podman load -i …   → localhost/yolo-jail:latest bdb2673ad3dc
YOLO_RUNTIME=podman go test -count=1 -timeout 0 -v -run '^TestGitBroker' ./integration
→ PASS ×3
```

## 8. State changed on this Mac

- podman's `localhost/yolo-jail:latest` now points at `bdb2673ad3dc`, built from `0291b3bc7`.
- The nix store holds the rustls mise and `yolo-noncontainer-profile` (`c4xkimsmafy3as7qyq6nli4qx3m4xwvp`).
- Nothing in the maintainer's config changed. The test runs used their own isolated homes.
