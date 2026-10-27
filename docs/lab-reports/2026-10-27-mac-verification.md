---
title: "Mac verification of what landed 2026-10-25 to 2026-10-27: the macos-user suite green, self mode's view path fixed, and Apple Container with no egress"
date: 2026-10-27
status: open
tags: [lab-report, macos, macos-user, claude, credentials, apple-container, podman, testing, evidence]
summary: "One debug-only session on the maintainer's Mac against 12acfa691: the full macos-user suite passes (162, 0 failures), 829717e46 fixes the self-mode view path while self mode as shipped still cannot start Claude and says nothing about it, darwin `just load` and the podman git-broker and auto-capture tests pass, and Apple Container jails have had no egress since 2026-10-24 with host IP forwarding off."
---

# Mac verification, 2026-10-27

A [lab report](README.md).

- **Host:** the maintainer's Mac, macOS 26.5 (25F71), arm64, Command Line Tools only, Apple
  Container 1.1.0, podman 6.1.4 on applehv (rootless).
- **Source:** `origin/main` at `12acfa691`. That covers the 83 commits after the last Mac reports
  ([10-24](2026-10-24-mac-verification-after-gitbroker.md) and
  [10-25](2026-10-25-mac-claude-login-session.md)).
- **Installed yolo:** `just deploy` of that commit.
- **Debug and document only:** nothing below is fixed.
- **No credential or API call by a test.** Two `claude -p` calls were made by hand, in
  [section 2](#2-self-mode-the-view-path-is-fixed-the-tmp-denial-is-not).

## 1. The macos-user integration suite: all green

`YOLO_TEST_MACOS_USER=1 go test -count=1 -timeout 0 -v -run '^TestMacosUser' ./integration`:
**162 passed, 0 failed, 4 skipped (904 s)**; ledger `executed=109 skipped=18`. On 2026-10-23 the
same suite had 3 failures.

- **CA trust.** `TestMacosUserTrustsSystemKeychainCA` passed in both modes against this Mac's real
  Jamf CA. This is the first run with `74fa7fa51`'s bundle merge. `TestMacosUserKeychainProbe` and
  its stand-in tests passed too.
- **The four skips are opt-ins or this Mac's environment:**
  - `TestMacosUserMacosLogBridgeScopesToTheSandbox`: the unified log store is closed to a non-admin
    user on this managed Mac.
  - `TestMacosUserPasswordlessStartAndExit`: needs `YOLO_TEST_MACOS_USER_NARROW_SUDO=1`.
  - The two relocation-volume measurements: need `YOLO_TEST_MACOS_USER_RELOCATION_VOLUMES`.

## 2. Self mode: the view path is fixed, the `/tmp` denial is not

`829717e46` registers the self-mode view under Claude's home. Measured with the deployed build,
`YOLO_CLAUDE_CREDENTIAL_VIEW=1` and `macos_user_mode: "self"`:

| Run | Result |
| :--- | :--- |
| `yolo -- claude -p 'Reply with the single word: ok'` (as shipped) | rc 1: `EPERM: operation not permitted, mkdir '/tmp/claude-501'` |
| `yolo -- bash -c 'CLAUDE_CODE_TMPDIR=$TMPDIR claude -p …'` | rc 0, `ok`. `CLAUDE_SECURESTORAGE_CONFIG_DIR` is unset, and the view is at `.yolo/home/.claude/.credentials.json` |

So [the 10-25 report's defect 2](2026-10-25-mac-claude-login-session.md#2-self-mode-writes-the-view-where-claude-does-not-read-it)
is fixed: the view is now where self-mode Claude reads it, with no workaround.
[Defect 1](2026-10-25-mac-claude-login-session.md#1-self-mode-cannot-start-claude-tmpclaude-uid-is-denied)
is still open, as the commit's own warning text says ("a session-TMPDIR workaround, which this
launcher does not yet set").

**A gap in that warning.** The new self-mode lines print only when registration fails, or when
the machine has no login. In the ordinary case (registered, logged in), the as-shipped launch
printed nothing about `/tmp` or `CLAUDE_CODE_TMPDIR`, and Claude exited on the EPERM. A user sees
Claude's own error with no next step from yolo.

**Decision:** whether the launch says so in every self-mode launch with the view on until
`CLAUDE_CODE_TMPDIR` is set, or sets it now.

## 3. Podman on darwin: `just load`, the git broker and auto-capture

- **`YOLO_RUNTIME=podman just load` works on darwin.** Exit 0 in 28 s, through an archive
  (`90a6241e0`). On 2026-10-24 it failed in 7 s.
- **`YOLO_RUNTIME=podman go test -count=1 -timeout 0 -v -run
  '^(TestAutoCaptureInstallerStdoutReachesParentLaunchLog|TestGitBroker)' ./integration`: all five
  pass** on rootless podman with `pasta`:
  - `TestAutoCaptureInstallerStdoutReachesParentLaunchLog` (7.4 s)
  - `TestGitBrokerFetchAndRefusal`
  - `TestGitBrokerWithNoGrantRefusesTheFetch`
  - `TestGitBrokerCreateBranch`
  - `TestGitBrokerConfigBranch`
- **No egress warning appeared** in any of those launches. The false "no outbound network" warning
  of 10-24 needed the podman machine's DNS to be slow, and its DNS has been healthy since the
  machine restart on 10-24.

## 4. Apple Container jails have no egress, and host IP forwarding is off

Since at least 2026-10-24, every Apple Container container on this Mac has had no outbound
traffic, and that was still true today. Measured from fresh containers:

```text
container run --rm curlimages/curl … https://claude.ai     → 000 after 10 s (TCP timeout)
container run --rm curlimages/curl … https://1.1.1.1       → 000
container run --rm curlimages/curl … http://192.168.64.1   → 000 (the network's own gateway)
podman run --rm curlimages/curl … https://claude.ai        → 403 in 0.05 s (reachable)
```

On the host:

- The container network is `192.168.64.0/24` on `bridge101`.
- `sysctl -n net.inet.ip.forwarding` prints **0**.
- `socketfilterfw --getglobalstate` reports the application firewall **enabled**.

Podman's `gvproxy` forwards in user space, which fits podman working while Apple Container does
not. *Hypothesis, not tested:* vmnet's NAT needs IP forwarding, and something (the VPN client, MDM
or a reboot) left it off. Nothing on the host was changed: setting the sysctl needs sudo and is
the maintainer's call.

The CI runner's `apple-container.yml` runs on this Mac and still passes (the last six, through
`40429991369`). Its log has no egress line either way, so whether those jobs had network is
unknown.

This blocked the Apple Container half of this session: `89057f63c` (briefings after a refresh) and
the auto-capture test on that runtime were not run.

**Decision:** the maintainer checks `net.inet.ip.forwarding` and the firewall. If forwarding is
the cause, should a launch's egress probe on Apple Container name it?

## 5. Unit tests on darwin

`go test -short -count=1` over every package the 83 commits touched passed, except where noted.
The default TMPDIR (`/var/folders/…`) was used, which three tests failed under on 10-24
(`fbea9ef42` fixed them).

| Package | Result |
| :--- | :--- |
| `capture`, `catrust`, `changetiming`, `cli/run` (514 s), `config`, `configdoc`, `gitbroker`, `hostrun`, `hostskills`, `jailcontent/...`, `loopholes`, `macosuser`, `packdecl`, `paths`, `testconfig`, `tomlx`, `claudeview` | ok |
| `cli` (1168 s), `entrypoint` (631 s) | ok, rerun with `-timeout 0`: the first run used Go's default 10-minute limit while the macos-user suite ran beside it, and both timed out |
| `ttyproxy` | not a defect: Linux-only, so naming it on darwin is a setup error |
| `execmonitor` | no test files on darwin: `f0dd57e54`'s recovery is Linux-only (`recover_linux.go`, with a stub in `recover_other.go`) |

## 6. Not done, and why

- **`TestNestedMechanicsTracksAppliedLaunchAndLiveMounts`** skips everywhere but Linux podman.
- **Anything on Apple Container**, because of section 4.
- **A real-login re-run of the 10-25 enrollment tests.** No login fix landed beyond the view
  path, which section 2 covers without a `/login`.

## 7. State changed on this Mac

- The installed `yolo` is `12acfa691` (`just deploy`).
- Podman's `localhost/yolo-jail:latest` was re-loaded by `just load`; it is the same image as before.
- `~/.config/yolo-jail/overrides.jsonc` was created for section 2 and removed.
- `/Users/Shared/yolo/cv-mac-self` and `cv-mac.env` are removed at the end of the session.
