---
title: "The Mac Claude login session: the credential view works on the Mac where a sandbox can read it, and every macOS /login and /logout path misses it"
date: 2026-10-25
status: open
tags: [lab-report, macos, macos-user, claude, credentials, oauth, broker, evidence]
summary: "The runbook mac-claude-login-session.md, run on the maintainer's Mac. Account mode, `yolo host` and (with two workarounds) self mode all read the credential view and adopt a refreshed or corrected file mid-session, so Claude on a Mac reads the file and not the Keychain when yolo points it there. Self mode as shipped cannot start Claude, and writes the view where Claude does not read it. A sandbox /login cannot save in account mode, a host /login goes to a Keychain entry named for the store, and a Mac /logout deletes the view file, which the broker does not count as a sign-out."
---

# The Mac Claude login session, 2026-10-24 to 25

A [lab report](README.md) for [the runbook](../plans/runbooks/mac-claude-login-session.md), whose
[results table](../plans/runbooks/mac-claude-login-session.md#results) has one row per step.

**Host:** the maintainer's Mac, macOS 26.5 (25F71), arm64, Apple Container 1.1.0, podman 6.1.4 on
applehv (rootless).

**yolo:** Steps 0 to 5 ran on `9a1e0d9c9` from `just deploy`. Phase A ran on `482763948`,
redeployed for PR 62.

**Claude Code:** 2.1.310 in account mode and on the host, and 2.1.304 in self mode, as each
sandbox installed it.

**Subscription:** Team. No token was printed: fingerprints come from `yolo claude-auth`, and
Keychain entries were read by name and date only.

## What it settles

**When yolo points Claude on a Mac at a credentials file, Claude reads that file and not the
Keychain.** That held in account mode (M11), in self mode once its two defects were worked
around, and at `yolo host`. On the host it held even with the maintainer's own Keychain login
present. A store holding a deliberately wrong token failed, so the file was the source.

**A running session adopts a rewritten file** (3a), and **re-reads it on a 401** (3b), in both
modes.

**The broker adopts a refresh made inside a self-mode sandbox, and fans it out** (Phase A):

```text
canonical: adopted a newer login from the shared credentials file (rt 96ac6130 -> 74a4c6c4, at -> d9726cdb)
```

That was followed by a `view: wrote` line for each view.

**Reading works and sign-in does not.** A Mac cannot create or remove the machine's login through
a view:

- **C1:** a sandbox `/login` cannot save.
- **H4:** a host `/login` saves to the Keychain.
- **C2:** a `/logout` is not recognized.

Today the shared login gets onto this Mac only through a Linux jail's `/login`.

## Defects, each with the decision it needs

### 1. Self mode cannot start Claude: `/tmp/claude-<uid>` is denied

```text
EPERM: operation not permitted, mkdir '/tmp/claude-501'
```

Claude Code makes `/tmp/claude-<uid>` whatever `TMPDIR` says. The run-as-you profile closes the
shared `/tmp` ([OQ-MM8](../design/macos-user-model.md#OQ-MM8)), and the uid is yours.

On this Mac, `/tmp/claude-501` is the directory of the maintainer's **own** Claude Code session. The
denial is right: the alternative is a sandbox writing into the host agent's scratch space.

Claude reads `CLAUDE_CODE_TMPDIR` (it appears 50 times in the 2.1.304 binary), and setting it to
the session's `TMPDIR` fixes the start. `rg CLAUDE_CODE_TMPDIR internal packs` finds nothing.

**Decision:** whether the claude pack sets `CLAUDE_CODE_TMPDIR` for macos-user, in self mode or in
both modes.

### 2. Self mode writes the view where Claude does not read it

| | Path |
| :--- | :--- |
| The broker registers and writes | `<ws>/.yolo/home/claude/.credentials.json` (the account-mode layout) |
| Self-mode Claude reads, with `HOME=<ws>/.yolo/home` and no `CLAUDE_SECURESTORAGE_CONFIG_DIR` | `<ws>/.yolo/home/.claude/.credentials.json` |

The bootstrap says `shared_credentials[claude]: .claude/.credentials.json -> …: credential view: not
linked; the host broker writes this file`, but the broker writes the other path. Claude answers
`Not logged in · Please run /login`.

`CLAUDE_SECURESTORAGE_CONFIG_DIR=$HOME/claude` made it answer from the view. This was still true at
`482763948`: PR 62 sets the variable only for the view-off path.

**Decision:** whether the view registration follows the mode's layout, or the launch points the
store at the view.

### 3. A sandbox `/login` cannot save in account mode, with the view on or off (C1)

```text
OAuth error: Couldn't save your login. If your Mac's keychain is locked, unlock it and log in again.
```

Claude runs as `_yolojail`, which has no unlocked login keychain, and it did not fall back to the
file. Unlocking the maintainer's keychain changed nothing.

This makes runbook Step 0.3 ("`/login` once in a sandbox `claude`") impossible in account mode. The
machine was enrolled through a podman jail instead.

**Decision:** whether account mode's documented way in is a Linux jail's `/login`, or whether
`_yolojail` needs a keychain.

### 4. A host `/login` with the view on goes to the Keychain (H4)

The launch printed the right line:

```text
yolo host: this machine has no shared Claude login yet; /login in this session enrolls the machine…
```

The `/login` then created a Keychain entry, `Claude Code-credentials-5d0efea8`, modified at
2026-10-25T16:04:09Z, the moment of the login. The store directory stayed empty and the broker
logged no enrollment.

Reading uses the file and saving uses a Keychain entry named for the moved store. That is the fail
signature [Part H](../plans/runbooks/claude-credential-view-measures.md#h5-without-the-switch-nothing-changed-and-the-macs-fail-signature)
names for H1, met here on the save side.

Before the `/login`, with the store empty, the session's banner read `API Usage Billing`, so it had
found some other credential. Which one was not traced.

That entry is still in the maintainer's login keychain. This session did not delete it.

**Decision:** whether the host enrollment line is wrong on macOS, or yolo should enroll from that
Keychain entry.

### 5. A Mac `/logout` deletes the view, and the broker rewrites it (C2)

Claude's `/logout` in an account-mode view jail deleted
`cv-mac/.yolo/home/claude/.credentials.json`. No `was signed out by /logout` line followed.

The next `yolo claude-auth refresh` wrote a fresh view, so the workspace came straight back
`live`. The cause is in `maintainViewLocked` (`internal/oauthbroker/views.go:386`): a sign-out
requires `!st.missing && st.root != nil && st.oauth == nil`, a file that remains with its
`claudeAiOauth` key gone. A deleted file reads as `missing`.

**Decision:** whether a view that the broker wrote and later found missing counts as a `/logout`
on darwin.

## Smaller findings

- **Interactive first run in a fresh sandbox home stops at "Select login method".** In both modes,
  even with a live view, while `claude -p` in the same home answers. Esc does not leave the screen.
  With the maintainer's approval, `hasCompletedOnboarding: true` and `theme` were set in the
  sandbox's `claude.json` (no credential field), and the session then ran on the view.
- **3b had to change order.** With a wrong token at start-up, the Team org's managed-settings
  policy exits Claude at once (*"Your organization requires remote managed settings to load"*).
  The run went good → wrong with a new mtime → good with the wrong file's mtime. That still
  isolates the 401 re-read.
- **`yolo claude-auth refresh` writes views without logging `view: wrote`.** The broker's own
  writes do log it (05:21 and 11:31 on 2026-10-25). The runbook names that line as 3a's and H3's
  witness, so after the CLI refresh the evidence was the fingerprint change instead.
- **The runbook's `~/tmp/cv-mac*` cannot be an account-mode workspace,** because it is inside the
  home folder. `/Users/Shared/yolo/cv-mac*` was used instead.
- **A workspace that macos-user used cannot launch on Apple Container.** Its `.yolo/home` keeps
  `.claude-shared-credentials` as a symlink into `/Users/_yolojail`, and the container mount fails:
  `mount failed with errno 17: failed to create directory '.claude-shared-credentials'`.
- **Account mode runs one session at a time across workspaces,** so Phase A cannot run there as
  written.
- **The maintainer's packs blocked launches unrelated to Claude:**
  - `pi-subagents`' series no longer applies to v0.82.0.
  - A stale copy of the `matt` pack named `?ref=carry/pi-manage-todo-list`, which `a786c3b7`
    renamed.
  - The `board` program's only capture is linux/arm64, and yolo tried to materialize it on darwin
    for `yolo host` and self mode (`capture materialize: entry c5dafe0445128f70 is a linux/arm64
    capture and this is darwin/arm64`).

  Launches used `YOLO_SKIP_EXTENSIONS` and `YOLO_ALLOW_MISSING_PROGRAMS=1`.
- **Environment problems:**
  - The data volume was at 99% (5.8 GiB free) when the Apple Container image delivery failed. The
    maintainer cleared about 250 GiB: old Apple Container and podman images, nix GC, the Go cache,
    benchmarks and old dev bundles. The CI runner runs as the maintainer, and its
    `apple-container.yml` jobs leave a `yolo-jail` image per commit, which nothing prunes.
  - Apple Container jails had no outbound network (TCP timeouts).
  - Podman's DNS failed until `podman machine stop && start`.

## State left on the Mac

- `~/.local/bin/yolo` is a deployed launcher at `482763948`. It was the **dev** launcher before
  this session; `just deploy-dev` restores that.
- The Keychain entry `Claude Code-credentials-5d0efea8` from H4.
- The machine's shared login is restored through a podman-jail `/login`
  ([below](#the-machines-login-afterwards)).
- Test workspaces `/Users/Shared/yolo/cv-mac`, `cv-mac-self`, `cv-mac-a`, `cv-mac-b`,
  `cv-mac-login`, `cv-mac-login-p` and `cv-mac.env` are removed at the end of the session.
- `~/.config/yolo-jail/overrides.jsonc` was created for the self-mode steps and removed after each.

## The machine's login afterwards

After C3 and the failed H4, the maintainer ran `/login` in a podman jail. The canonical login came
back with refresh token `daca92cf` and an 8 h access token, and the shared file matched.

One difference was not explained. The canonical file kept only `accessToken, refreshToken,
expiresAt, refreshTokenExpiresAt, scopes`, while the shared file also held `subscriptionType`,
`subscriptionTypeSource` and `rateLimitTier`. Before C3, both carried `subscriptionType` and
`rateLimitTier`. Views project from the canonical file, so this may matter wherever Claude reads the
plan type from the view.
