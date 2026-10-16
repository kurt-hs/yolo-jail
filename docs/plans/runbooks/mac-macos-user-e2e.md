# RUNBOOK — macos-user backend end-to-end (you-drive, agent-advises)

**Status:** CURRENT — the repeatable procedure, and the M1 gate: PASSED on real hardware
2026-07-21.

**Who runs this:** YOU, on the Mac. The agent interprets output; **you run each
`yolo` command** (`yolo macos-setup` self-escalates and asks for your password
once). **Run them as your normal admin user, NOT under `sudo`.**
**Privilege:** only `yolo macos-setup` and `yolo macos-teardown` need admin: setup
creates a hidden macOS user and installs one sudo rule,
`/etc/sudoers.d/yolo-jail`, that lets you run commands as that user, never as
root, without a password. Launches then write what they stage as you, under
`/Users/Shared/yolo-staged`, and ask for no password. All bounded + reversible.

> ⚠️ **Do NOT prefix `yolo` with `sudo`.** These commands self-escalate; running
> the whole command as root makes `getpass.getuser()` return `root`, so the
> shared-workspace group ACL would be granted to `root` instead of you and
> host↔sandbox file sharing would silently break. The CLI now **refuses to run
> under sudo** (`_refuse_if_root`, added 2026-07-17 in response to this runbook's
> review) with a clear message — so this is enforced, not just advised. Run
> everything as plain `yolo`.

**What it proves:** the native no-VM backend actually runs an agent as the
hidden `_yolojail` user under Seatbelt, with `packages:` materialized via native
aarch64-darwin nix — the acceptance bar. Everything up to the real launch is
already unit-tested on Linux; this is the real-hardware confirmation.

---

## 0. ROLLBACK FIRST (know the exit before the entrance)
Everything this creates is removed by ONE command. If anything looks wrong at
any step, stop and run:
```
yolo macos-teardown               # deletes the _yolojail user + group + home, the rule and the staging dir
```
Blast radius, for your peace of mind (from the code):
- creates a **hidden** service user `_yolojail` (uid ≥600, `IsHidden 1`, stripped
  from `staff`) — never appears on the login screen.
- creates `/Users/Shared/yolo-staged`, owned by you, where each launch stages
  the yolo binary, the per-session Seatbelt profile, env files and trees,
  read-only to the sandbox.
- installs ONE sudo rule, `/etc/sudoers.d/yolo-jail`:
  `<you> ALL=(_yolojail) NOPASSWD: /usr/bin/env`, checked by `visudo` before it
  is put in place. It runs commands as `_yolojail`, never as root. It does NOT
  touch `/etc/nix`.
- `macos-teardown` reverses the user/group/home, removes the rule, and removes
  the staging directory when it is yours; `macos-unshare <dir>` strips the ACL
  from a shared workspace. A `/var/yolo-jail` an older yolo left is not touched;
  `yolo stores` lists it. Nothing of yours outside the workspace is touched.

## 1. Preflight — inspect readiness WITHOUT changing anything (no sudo)
```
yolo check                         # look for the "macOS-user backend" section
```
Report what it says for each row: macOS, not running as root, sandbox-exec,
sandbox user (expected: FAIL, not provisioned yet, with the two rows that need it
reported as SKIP), sandbox home, the workspace outside every home and shared with
the sandbox, **nix present**, **flake.lock present**. Each row above nix is one
of the conditions the launch itself refuses without (`macosuser.LaunchPreconditions`,
one list for both), and each FAIL names its fix. A clean section means no such
condition refuses the launch; the nix build and the bootstrap come after it and
are what [§4](#4-first-real-run-under-seatbelt) tests.

## 2. Dry-run the plan — still NO sudo, nothing executes
Put `runtime: "macos-user"` in a scratch workspace's `yolo-jail.jsonc` (or
`YOLO_RUNTIME=macos-user`), then:
```
cd /Users/Shared/yolo/some-test-project     # NEUTRAL ground (not under ~) — required
YOLO_RUNTIME=macos-user yolo --dry-run
```
This prints the FULL run plan — the Seatbelt profile, the bootstrap script, the
launch argv, the darwin `packages:` it WOULD materialize — and runs its
invariant checks, executing nothing. **Report the plan + any invariant
violations.** A clean dry-run is the gate before touching sudo.

> If the workspace is under your home dir, the plan will (correctly) refuse —
> the backend only shares neutral ground like `/Users/Shared/yolo`. Move it.

## 3. One-time setup — the privileged step (plain `yolo`, NOT `sudo`)
```
yolo macos-setup                    # do NOT prefix with sudo — it self-escalates
```
Expect one prompt for your admin password. It: picks a free uid/gid, creates the
hidden `_yolojail` user, sets a random password (piped, never in argv),
provisions the shared root with an inheriting ACL, makes the staging directory
yours, installs the run-as rule, proves it with an empty sudo cache, and prints
readiness checks (sandbox-exec, nix). **Report the full output + the final verdict
(green ✓ or the ⚠ list).**

## 4. First real run under Seatbelt
Start WITHOUT packages first (isolate the sandbox launch from the nix build):
```
YOLO_RUNTIME=macos-user yolo -- bash -lc 'whoami; pwd; echo HELLO-FROM-SANDBOX'
```
Expect: `whoami` → `_yolojail`, `pwd` → your workspace, the echo prints. This
proves the sudo→env-i→sandbox-exec launch works, with no password prompt.
**Report output + any sandbox-exec error.**

## 5. The acceptance bar — packages materialized natively
Add a package to the workspace config: `"packages": ["jq"]`, then:
```
YOLO_RUNTIME=macos-user yolo -- bash -lc 'which jq && jq --version'
```
Expect: `jq` resolves from a `/nix/store/...` path (the native aarch64-darwin
buildEnv), NOT `/usr/local/bin` (Homebrew) or `/usr/bin`. **Report the path
`which jq` prints** — a `/nix/store/*/bin/jq` is the acceptance bar met on real
hardware.

> **Fixed since the first run (2026-07-17):** the first attempt returned
> `/usr/local/bin/jq` because a login shell (`bash -lc`, and the default `zsh
> -l` REPL) re-runs macOS `path_helper`, which shoved Homebrew's PATH ahead of
> the nix store. The bootstrap now writes `~/.zprofile`/`~/.zshrc`/
> `~/.bash_profile` that re-prepend the sandbox PATH *after* path_helper, so the
> nix-store jq wins. If it STILL shows `/usr/local/bin/jq`, paste
> `echo $PATH` from inside the sandbox — that's a real remaining bug.

> **Build progress is now visible:** the first run showed no nix output
> (silent `capture_output`); materialize now streams `--print-build-logs` to
> the terminal, so a from-source darwin build shows phases/fetches. First run
> may build/download the closure (slow once) — you'll now see it happen.

## 6. Real agent (optional, once 4–5 pass)
```
YOLO_RUNTIME=macos-user yolo -- claude
```
Confirm the agent starts, sees the workspace, and — the #1 footgun check —
does NOT see your host `~/.gitconfig`/`~/.ssh` (scrubbed HOME). Report.

## 7. Cleanup
```
yolo macos-teardown                # removes the _yolojail user + home
# if you ACL'd a workspace and want it plain again:
yolo macos-unshare /Users/Shared/yolo/some-test-project
```

## 8. Passwordless start and stop: the owner's check

**Needs the owner's explicit authorization before it runs**: step 1 installs a
sudo rule on a real machine ([MP-B1](../../design/macos-user-privilege-lifecycle.md#decision-ledger)
authorizes the source change only). This is the check
[the plan](../../design/macos-user-privilege-lifecycle-plan.md#the-owners-mac)
names. Run it on a Mac set up by an older yolo, with one old session left
running in another terminal.

1. `yolo macos-setup`: one password prompt. It prints
   `Installed /etc/sudoers.d/yolo-jail; visudo accepts it.`, then the probe
   `/usr/bin/sudo -n --user=_yolojail /usr/bin/env /usr/bin/true` running with
   no password and an empty sudo cache, and its ready verdict ends
   "Launches start and stop with no password from now on." The old session
   keeps running.
2. `sudo -k; yolo check --no-build`: these four rows pass in the macOS-user
   section:
   - `Staging directory /Users/Shared/yolo-staged is present`
   - `Staging directory /Users/Shared/yolo-staged is yours`
   - `Staging directory /Users/Shared/yolo-staged is a directory only you can write, with no access list`
   - `sudo runs commands as _yolojail without a password (/etc/sudoers.d/yolo-jail)`
3. `sudo -k; yolo -- /bin/sh -c 'id -un' </dev/null`: prints `_yolojail`, no
   prompt.
4. In a project with a pack whose program installs by capture:
   `sudo -k; yolo capture <bin>`: no prompt.
5. `sudo -k; yolo -- sleep 600 &`, then from a second terminal in the same
   project `sudo -k; yolo stop`: the session ends, no prompt, and
   `ls /Users/Shared/yolo-staged/env` shows none of its files.
6. End the old session. If it ended without removing its files (killed, or its
   sudo could not authenticate at exit), the next `yolo` launch warns
   `Warning: a macos-user session from an older yolo left root-owned files in /var/yolo-jail`
   and names `yolo macos-setup`; running that removes them and prints
   `Removed the root-owned files of 1 ended session(s) an older yolo left in /var/yolo-jail.`
   If the old session removed its own files, there is no warning, and that is a
   pass too.
7. Record each result, with the macOS version, in the
   [design doc's status line](../../design/macos-user-privilege-lifecycle.md).

---

## What to report per step (any failure is a precise bug for me)
- [§1](#1-preflight--inspect-readiness-without-changing-anything-no-sudo)/[§2](#2-dry-run-the-plan--still-no-sudo-nothing-executes): the check output + dry-run plan + any invariant violations.
- [§3](#3-one-time-setup--the-privileged-step-plain-yolo-not-sudo): setup output + verdict.
- [§4](#4-first-real-run-under-seatbelt): does it launch as `_yolojail`? any sandbox-exec error verbatim.
- [§5](#5-the-acceptance-bar--packages-materialized-natively): **the `which jq` path** (the acceptance-bar signal) + whether it built.
- [§6](#6-real-agent-optional-once-45-pass): agent starts? host creds invisible?
- [§8](#8-passwordless-start-and-stop-the-owners-check): each step's result, any password prompt and the command that asked.

## Likely rough edges I'd expect (so they're not surprises)
- **A sudo prompt during a launch is a bug:** every sudo a launch runs is
  `sudo -n` through the rule, which fails rather than prompting. If one asks for
  a password, report the command it named.
- **`/nix` not found / daemon not trusted:** the darwin build needs nix + a
  trusted user; if [§5](#5-the-acceptance-bar--packages-materialized-natively) fails there, paste the error — it's the same
  trusted-users wiring as the container path.
- **darwin build of a package with no aarch64-darwin build:** should warn-and-skip
  (not crash). If a package vanishes silently, report which.
