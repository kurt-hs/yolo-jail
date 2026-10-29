---
title: "Mac session 2026-10-29: CI runner disk cleanup and why it piles up, plus the native confirmation of the Mac Claude follow-ups"
date: 2026-10-29
status: open
tags: [lab-report, macos, macos-user, claude, credentials, apple-container, ci, image-retention, evidence]
summary: "One debug-only session on the maintainer's Mac against d159a57f0: the self-hosted CI runner's leftovers were reclaimed (166 to 195 GiB free) and traced to two causes, one of them a reaper that has never worked on Apple Container; a post-job hook is staged but not registered. The follow-up plan's native confirmation passes steps 2 to 5, and three new defects showed up on the way."
---

# Mac session, 2026-10-29

A [lab report](README.md).

- **Host:** the maintainer's Mac, macOS 26.5 (25F71), arm64, Command Line Tools only, Apple
  Container 1.1.0, podman 6.1.4 on applehv (rootless, `pasta`).
- **Source and installed yolo:** `just deploy` of `d159a57f0` (`yolo 0.15.1+705.gd159a57f0331`).
  Claude Code 2.1.310. `origin/main` gained one commit during the session, `2cae7d758`, an
  entrypoint symlink-loop fix for agy that touches nothing measured here.
- **Debug and document only:** no code was changed. Every human step (`/login`, `/logout`) was
  done by the maintainer in a tmux session.
- **No secrets recorded:** tokens appear only as the 8-hex fingerprints `yolo claude-auth`
  prints. "rt" is a refresh token, "at" an access token.

Two terms used below:

- **Credential view** (yolo's term, [the design](../design/claude-login-without-interception.md)):
  a per-workspace credentials file the host broker writes, holding the access token and its
  expiry but no refresh token. The broker keeps the one refresh token, in its canonical file.
- **Enrollment** (same design): a `/login` inside a workspace writes a full credentials file,
  and the broker adopts its refresh token as the machine's login.

## 1. The CI runner: what it left behind, and why

The self-hosted GitHub Actions runner at `~/actions-runner` runs `apple-container.yml` on this
Mac, which is never wiped between jobs.

### What was reclaimed

| What | Before | After |
| :--- | :--- | :--- |
| Apple Container images | 38 GiB | 25 GiB: removed `yolo-jail` tags `bedcd30e02fd239f` and `f390d3cb5dc13aef`; kept `3366b7f555d58d5d`, `37cfb9e1834ecd91`, `c2abbbe5483f0960` and `latest` |
| `yolo prune --apply`, container and podman | | 1.3 GiB of captures, 211 MiB of staging, 5 tool disks and their roots, 264 current-image pointers for workspaces that no longer exist |
| Dev bundles (`~/.local/share/yolo-jail/dev-bundles`) | 6.3 GiB | 519 MiB: 36 removed, the newest kept |
| `nix-collect-garbage` | | 8.6 GiB freed |
| **Free space on the data volume** | **166 GiB** | **195 GiB** |

### Why it piles up

1. **The integration harness turns the automatic reapers off** for every launch it makes
   ([`harness_test.go`](../../integration/harness_test.go#L175),
   [`harnessenv_test.go`](../../integration/harnessenv_test.go#L146)). That is deliberate for test
   isolation, but it means nothing in a CI job ever reaps.
2. **The image reaper has never worked on Apple Container.** The probe lists images with
   podman's syntax
   ([`probes.go`](../../internal/prune/probes.go#L402)):

   ```go
   res := run([]string{rt, "images", "--format", imageListFormat, paths.JailImageRepoShort}, psTimeout)
   ```

   Apple Container has no `images` verb, so `container images …` answers
   `Plugin 'container-images' not found`. Following the
   [reaper rule](../reference/image-retention.md#why-its-this-way) ("a reaper that cannot ask
   declines"), the reaper declines instead of deleting. The housekeeping log shows
   `reap DECLINED — could not list this runtime's images` on every run since September, and
   `YOLO_RUNTIME=container yolo prune --apply` exits 1 for the same reason. Apple Container's
   own spelling is `container image list`.
3. **Nothing reclaims dev bundles on macOS.** Every CI run that builds a dev bundle leaves one.
4. **A current-image pointer protects its image even after its workspace is gone**
   ([`currentimages.go`](../../internal/prune/currentimages.go#L200)). This Mac had 3905 such
   pointers in `build/current-images/`. `yolo prune` removed 264 of them for workspaces it
   could prove were gone.

### Prevention: a post-job hook, staged but not registered

`~/actions-runner/yolo-post-job-cleanup.sh` (outside the repo) runs after every job:

- keeps the 3 most recent loads recorded in `build/last-load-container`, plus `latest` and any
  image a container uses, and deletes the other `yolo-jail` tags with `container image delete`;
- runs `YOLO_RUNTIME=container yolo prune --apply`;
- keeps the newest 3 dev bundles plus any a running process uses, and deletes the rest;
- runs `nix-collect-garbage` only when free space is under 100 GiB;
- logs to `~/actions-runner/_diag/yolo-post-job-cleanup.log` and always exits 0, so a cleanup
  problem never fails a job.

Tested by hand: a no-op run took 8 s, and a dummy tag `yolo-jail:0000000000000000` was deleted
with nothing else touched.

**It is not registered.** Registering it edits the runner's `.env` and restarts its service,
which is the maintainer's call:

```sh
cd ~/actions-runner
echo "ACTIONS_RUNNER_HOOK_JOB_COMPLETED=$HOME/actions-runner/yolo-post-job-cleanup.sh" >> .env
./svc.sh stop && ./svc.sh start
```

**Decision:** the hook is a stopgap. Fixing the probe at `probes.go:402` to use
`container image list` on Apple Container would let yolo's own reaper do step 1. Whether the
harness should keep the reapers off, or have CI run `yolo prune` after each job, is open.

## 2. Native confirmation of the Mac Claude follow-ups

This is the "Focused native confirmation after parent publication" section of
[the follow-up plan](../design/mac-claude-lab-followups-plan.md#focused-native-confirmation-after-parent-publication),
run on `d159a57f0`. Each step used a disposable workspace under `/Users/Shared/yolo`.

The test env file set `YOLO_SKIP_EXTENSIONS` for four of the maintainer's pi extensions whose
patch series no longer apply upstream (`pi-subagents`, `pi-manage-todo-list`, and from this
session also `pi-archimedes` and `pi-session-search`). The host launch also needed
`YOLO_USE_INSTALLED=copilot` and `YOLO_ALLOW_MISSING_PROGRAMS=1` (see
[section 3](#3-defects-found-on-the-way)).

| Step | Result |
| :--- | :--- |
| 1. Versions, `yolo check --no-build` | yolo `0.15.1+705`, Claude 2.1.310. The first check had 1 FAIL: the shared credentials had expired 39 h earlier because no broker had run. The next launch refreshed them. |
| 2. Self mode, Claude direct and from plain bash | **Pass** |
| 3. Account mode, human `/login` | **Pass**, after working around a new refusal ([3.1](#31-account-mode-the-second-launch-of-a-workspace-is-refused)) |
| 4. Host, human `/login` with the view on | **Pass** |
| 5. Human `/logout` in one workspace | **Pass** |

### Step 2: self mode

Workspace `cv-conf-self`, `macos_user_mode: "self"` set through
`~/.config/yolo-jail/overrides.jsonc` (removed afterwards).

- `yolo -- claude -p 'Reply with the single word: ok'` answered `ok` with no workaround. On
  2026-10-27 the same command failed on `/tmp/claude-501`.
- From `yolo -- bash`, bash itself had `CLAUDE_CODE_TMPDIR` unset, while Claude started from that
  bash had `CLAUDE_CODE_TMPDIR` equal to `TMPDIR` (`…/T/yolo-<session>`). Claude's scratch went to
  `…/yolo-<session>/claude-501`, and the host's `/tmp/claude-501` did not change.
- In `~/.yolo/bin/launch/`, only `claude` mentions `CLAUDE_CODE_TMPDIR`. The `board`,
  `chrome-devtools-mcp`, `codex`, `copilot`, `pi` and `pnpm` launchers do not.

### Step 3: account mode `/login`

Workspace `cv-conf-acct2`, tmux `cv-acct`.

- The account adapter is on Claude's PATH: `~/.yolo/bin/block/claude-security/claude/security`,
  which runs `yolo internal claude-security account`.
- The maintainer's `/login` completed normally, with no "Couldn't save your login" message. The
  broker log:

  ```text
  enrollment: adopted the /login in /Users/Shared/yolo/cv-conf-acct2/.yolo/home/claude/.credentials.json as the machine's login (rt fc95cce1 -> 18b45db1, at -> 23184fb3)
  ```

- The view afterwards: refresh token absent. The session answered `Ok`.
- No Keychain was inspected or changed.

### Step 4: host, view on

Workspace `cv-conf-host`, tmux `cv-host`,
`YOLO_CLAUDE_CREDENTIAL_VIEW=1 yolo host -- claude`.

- The host adapter was generated at `~/.local/share/yolo-jail/bin/block/fcd28306958166fd/security`,
  scoped to the store's service `Claude Code-credentials-5d0efea8`.
- The maintainer's `/login` completed normally. The broker log:

  ```text
  enrollment: adopted the /login in /Users/matt/.local/share/yolo-jail/host-agents/claude/.credentials.json as the machine's login (rt d738d3fe -> 94c95c67, at -> c80ecbdb)
  ```

- The host view afterwards: refresh token absent, at `c80ecbdb`; the canonical refresh token is
  `94c95c67`.
- **The maintainer's Keychain did not change.** The only Claude entry is still the bare
  `Claude Code-credentials`, with the same modification date (`20261029145742Z`). In the
  [10-25 session](2026-10-25-mac-claude-login-session.md) the same `/login` created a new
  `Claude Code-credentials-5d0efea8` entry instead.

### Step 5: `/logout` in one workspace

The maintainer ran `/logout` in `cv-conf-acct2` (account mode). Then:

| Check | Result |
| :--- | :--- |
| The view file | removed by Claude's `/logout` |
| The broker's registration (`claude-views/38561f8796d6b76c.json`) | `written: true`, `logged_out: true` after the next refresh |
| `yolo claude-auth refresh` (forced) | new rt `94793fdc`, at `793f2aca`. The broker log says `…/cv-conf-acct2/.yolo/home/claude/.credentials.json was signed out by /logout in its jail; the broker writes it nothing more until its next launch`, and the file stays absent |
| Sibling views | `cv-conf-acct` and the host store were both rewritten to at `793f2aca`, refresh token absent |
| Next explicit launch in `cv-conf-acct2` | printed `Claude: /logout in this workspace's jail had signed it out of the machine's login; this launch signs it back in.` The view came back (at `793f2aca`, refresh token absent) and the registration went back to `logged_out: false`. The launch was then refused by [3.1](#31-account-mode-the-second-launch-of-a-workspace-is-refused), which reproduced on this relaunch |

One observation, not a step-5 failure: `yolo claude-auth status` before the refresh listed the
signed-out view as `no view file yet`, and the registration still said `logged_out: false`. The
background tick only rewrites views near expiry, so the logout was recorded only when the next
refresh ran. Until then, `status` cannot tell a signed-out view from one never written.

## 3. Defects found on the way

### 3.1 Account mode: the second launch of a workspace is refused

Relaunching an account-mode workspace with a JSONC user config is refused:

```text
refusing macos-user inherited preflight scope: the home-overlay installer cannot prove this file is its prior delivery could overwrite existing user file …/.yolo/home/config/yolo-jail/config.jsonc; preserve its contents and mode, move it aside only after review, then relaunch
```

The file is yolo's own delivery from the first launch.
[`refuseExistingScopeDestination`](../../internal/cli/run/inheritscope.go#L249) refuses when any
destination exists, without recognising a prior delivery. The check came in with `7e2d1ba93` and
`faf76e8f8` (2026-10-28). This affects the maintainer's daily use with the deployed yolo, not only
tests. Workaround used here: move the file aside, as the message says
(`.yolo/moved-aside-config.jsonc`).

### 3.2 Apple Container cannot start in a workspace macos-user used

By mistake, one relaunch of `cv-conf-acct2` left out `YOLO_RUNTIME=macos-user`, so it went to
Apple Container, the default here. It failed at container start:

```text
Error: mount failed with errno 17: failed to create directory '.claude-shared-credentials'
```

Errno 17 is `EEXIST`. The workspace's `.yolo/home` held the macos-user layout from earlier
launches. Not investigated further. Switching one workspace between backends is rare, but the
error does not name a next step.

### 3.3 Host launch: "invalid macos-user launch context"

`yolo host -- claude` printed:

```text
yolo: ERROR: invalid macos-user launch context; refusing this program because its native compatibility rule cannot be evaluated…
```

and the Copilot capture failed (`capturing copilot: yolo capture copilot exited 1`), so the
launch needed `YOLO_USE_INSTALLED=copilot`. *Hypothesis, not tested:* the Copilot compatibility
check from `adfde9db4` runs in a host context, where there is no macos-user launch context to
evaluate.

### 3.4 The self-mode view became a symlink to the shared file

After step 2, `cv-conf-self/.yolo/home/.claude/.credentials.json` is a symbolic link to
`../.claude-shared-credentials/.credentials.json`, the interception layout. The view is still
registered, so the broker refuses to write it on every pass:

```text
view: refusing to write /Users/Shared/yolo/cv-conf-self/.yolo/home/.claude/.credentials.json: … it is a symbolic link, and the jail can write the directory it is in, so the broker does not follow it
```

Step 2 ran without `YOLO_CLAUDE_CREDENTIAL_VIEW=1`, so a view-off launch put the interception link
over a registered view. Refusing to follow the link is correct. What is missing is the view-off
launch retiring the registration, or `status` saying which launch replaced it.

Related: this session's launches warn
`YOLO_CLAUDE_CREDENTIAL_VIEW is not a recognized launch setting`, yet the variable still changed
behaviour on the host (step 4). Whether it is still a supported dial is unclear from the launch
output.

## 4. Other findings

- **`board` has only a linux/arm64 capture**, so `yolo host` on this Mac needs
  `YOLO_ALLOW_MISSING_PROGRAMS=1`.
- **`security add-generic-password` hangs in a plain account-mode bash.** The account adapter is
  on Claude's PATH only, so `yolo -- bash` reaches `/usr/bin/security`, which hangs as
  `_yolojail` (no default keychain). This is by design (the adapter is Claude-scoped). The probe
  was killed after 10 minutes.
- **Apple Container still has no egress**, as on
  [10-27](2026-10-27-mac-verification.md#4-apple-container-jails-have-no-egress-and-host-ip-forwarding-is-off):
  `net.inet.ip.forwarding` is still 0. Not changed; it needs sudo.

## 5. Not done

- **The SecurityServer boundary audit** in
  [`macos-native-tls.md`](../design/macos-native-tls.md) was not started; it is the next Mac
  assignment.
- **Registering the CI hook** (section 1), and **turning on IP forwarding**: both are the
  maintainer's.

## 6. State changed on this Mac

- The installed `yolo` is a `just deploy` of `d159a57f0`, not the dev launcher. `just deploy-dev`
  restores the dev launcher.
- The machine's Claude login was re-enrolled twice (steps 3 and 4) and refreshed once (step 5).
  The current canonical refresh token is `94793fdc`.
- Removed: the images, bundles, prune targets and nix garbage in section 1.
- Added: `~/actions-runner/yolo-post-job-cleanup.sh`, not registered.
- Removed at the end: workspaces `cv-conf-self`, `cv-conf-acct`, `cv-conf-acct2`, `cv-conf-host`,
  `cv-mac.env`, tmux sessions `cv-acct` and `cv-host`, and `overrides.jsonc`.
