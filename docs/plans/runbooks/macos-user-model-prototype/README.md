---
title: "The macos-user model prototype"
status: accepted
stage: CURRENT
next: "A human runs both modes on a Mac and pastes the output back; adjust profile.sb from what it reports"
tags: [runbook, macos-user, seatbelt, safehouse, prototype]
summary: "A hand-written deny-by-default Seatbelt profile and a check script that prove or disprove the ruled macos-user model on a Mac, with no yolo code involved"
---

# The macos-user model prototype

**Status:** written 2026-10-22 without a Mac and never run. The profile has not been loaded by a
kernel yet, so the first run is expected to need changes to its allow list. That is the point of
running it.

## What this proves

The [macos-user model](../../../design/macos-user-model.md) rules that macos-user moves to a
Seatbelt profile that denies everything and allows a list, run as you by default and optionally as
the second account. Its [§6](../../../design/macos-user-model.md#6-what-a-prototype-must-show)
names what a prototype must show before anything is built. This directory is that prototype:

- [`profile.sb`](profile.sb) is the profile, hand-written from
  [Agent Safehouse](https://github.com/eugene1g/agent-safehouse)'s layered profiles (Apache-2.0;
  the notice and a per-block source tag are at the top of the file). It takes its paths as
  `sandbox-exec -D` parameters and nothing else.
- [`run-checks.sh`](run-checks.sh) builds a throwaway workspace and agent home, runs each check
  under the profile and prints `PASS` or `FAIL` for each, with the command and its output.

No yolo binary or config is involved, and the script changes nothing on the Mac beyond what it
creates and removes again: its temp directory, a decoy file in your home, a decoy project directory,
a loopback listener and a sleeping process.

The checks, keyed to [§6](../../../design/macos-user-model.md#6-what-a-prototype-must-show):

| Check | What must happen |
| :--- | :--- |
| 0 | The profile loads and `/bin/bash` starts under it. If not, nothing else runs. |
| a1–a12 | The workspace, `$HOME` and temp dirs are writable; git init and commit work; a C build runs; a Homebrew and a nix binary run; HTTPS works, certificate trust included (`curl`, `gh`, `git ls-remote`); the system fonts are readable. |
| b1–b13 | Each fails: reading a decoy secret in your home and `~/.ssh`, writing in your home, the keychain (`security`, and its file), the ssh agent, the clipboard, Apple Events to Finder and System Events, `open -a TextEdit`, `launchctl submit`, `kill -0` on one of your processes, and reading that process's command line. |
| c1–c4 | A decoy project at `/opt/src/...` (or `/Library/Caches/...` where `/opt` is not writable) is unreadable; `/opt/homebrew` and the nix store are readable. |
| d1–d2 | A loopback listener the script starts is unreachable from inside, and reachable once its port is passed to the profile. |
| e1–e2 | With Full Disk Access granted to the terminal, profile-denied files stay unreadable. |
| f1 | Optional: Claude Code installs into the sandbox's own home and `claude --version` runs. |

Two `INFO` lines report what the model knowingly leaves open, without counting it:

- **b14:** the shared `/tmp`, where a file of yours is still readable.
- **d3:** a loopback server the agent itself starts is also refused, because Seatbelt cannot tell
  which process is listening. Podman's private loopback has no such limit.

## How to run it

Copy this directory to the Mac, or use a checkout, then from a terminal:

```console
$ ./run-checks.sh --mode self
```

Self mode runs the sandbox as you, the ruled default. It puts the workspace in your home
(`~/yolo-prototype-<nonce>`), because the model allows that.

Account mode runs the same profile as the second account, through `sudo -u`, on a Mac where yolo's
macOS setup already created it. Elsewhere it prints a skip message and exits 0:

```console
$ ./run-checks.sh --mode account --account _yolojail
```

Its workspace is under `/Users/Shared/yolo/prototype-<nonce>`. It asks for your password once for
`sudo`.

Options:

- `--allow-port PORT` adds a loopback port to the profile's allow list. Repeat it for up to three.
- `--with-claude` runs the native Claude Code installer inside the sandbox, into its own home, then
  `claude --version`. It makes network calls to download the installer, but never logs in and never
  calls the API.
- `--keep` keeps the workspace and home and prints the command for a sandboxed login shell under the
  same profile.

Before you run it, two things widen what it covers:

- **Copy any text to the clipboard.** Check b7 compares what `pbpaste` sees inside with what you
  copied. With an empty clipboard it skips, and it never writes to your clipboard.
- **Optionally, grant the terminal Full Disk Access** (System Settings, Privacy & Security), restart
  it and run self mode again for checks e1 and e2. The script prints whether access appears
  granted; confirm that in System Settings.

Checks b9 and b10 send Apple Events and ask LaunchServices to open TextEdit. If the profile is
wrong, TextEdit opens or macOS shows an automation prompt; deny the prompt, close TextEdit, and
report it, because that is a `FAIL`.

## Confirming Claude stays logged in

The script never logs in. To check [§6](../../../design/macos-user-model.md#6-what-a-prototype-must-show) item 6 by hand:

1. Run `./run-checks.sh --mode self --with-claude --keep`.
2. Paste the printed sandboxed shell command into a terminal.
3. Run `~/.local/bin/claude`, log in, then quit.
4. Start it again. It must not ask you to log in, and `~/.claude/.credentials.json` must exist
   under the sandbox's home.
5. Report whether it stayed logged in. Never paste the credentials file.
6. Remove the kept directory with the `rm -rf` line the script printed.

## Reading the output

Each check prints one line, then the command it ran and its output, indented:

```text
PASS b1   read the decoy secret in your home
    $ cat "/Users/you/yolo-prototype-decoy-123.txt"
    | cat: /Users/you/yolo-prototype-decoy-123.txt: Operation not permitted
    (control without the profile: allowed, so a refusal is the profile's)
```

- **`PASS`** means the expected thing happened: an allow worked, or a deny held.
- **`FAIL` on an a, c3–c4 or d2 check** means the allow list is too narrow: a tool broke. That is
  expected on a first run, and it is a fix to [`profile.sb`](./profile.sb).
- **`FAIL` on a b, c1–c2, d1 or e check** means something the model must deny got through. That is
  the finding that matters most.
- **`SKIP`** means the check could not run here (no Homebrew, no nix, no Command Line Tools, empty
  clipboard), and says what would let it run.
- **The control line.** Every deny check that is safe to run outside the sandbox runs once without
  it first. In account mode many controls are "also denied", because Unix permissions already keep
  the account out of your home. That is the backstop the second account adds, and it means that
  check does not test the profile in that mode.

The script exits non-zero when any check fails. On a failure it prints a `log show` command that
pulls the sandbox's own denial messages, which name the operation and path the profile refused.

## What to paste back

- The whole output of `--mode self`, and of `--mode account` if the Mac has the account.
- For every `FAIL`, the `log show` excerpt the script suggests.
- Whether Full Disk Access was granted, and the Claude login result if you ran it.
- The macOS version, which the script prints on its second line.

## What the profile decides

The decisions, each traced to a design ruling. The file comments every allow on its own line.

- **Read-only system and software trees:** `/usr` (with `/usr/local`), `/bin`, `/sbin`,
  `/opt/homebrew` (never all of `/opt`), `/Applications`, `/nix/store`, `/nix/var/nix/profiles`,
  `/System/Library`, a few `/Library` framework and font folders, and the Command Line Tools
  binaries Safehouse curates. `/opt` itself gets metadata only, so `/opt/homebrew` resolves while
  `/opt` cannot be listed.
- **Read-write:** the workspace, the agent's home (`<workspace>/.yolo/home`), the launch's own temp
  dir, the running user's `DARWIN_USER_TEMP_DIR`, and `/tmp`. Safehouse grants all of
  `/var/folders`; this grants one folder of it, and not the user cache folder beside it.
- **Your home:** nothing but three locale preference files, read-only. Every directory above the
  workspace is readable as a single directory (its listing, nothing below it), as Safehouse does
  for Claude Code's start-up walk. So with the workspace in your home, the agent can list your
  home's top-level names but read none of them. No per-language toolchain folder (`~/.nvm`,
  `~/.cargo` and the rest) is granted: the agent has its own `HOME`.
- **System services:** Safehouse's list minus LaunchServices, which `open` uses. Trust evaluation
  (`com.apple.trustd.agent`) stays, so HTTPS clients work. The keychain, clipboard, Apple Events
  and LaunchServices are also denied by name at the end.
- **Processes:** any program may run; signals and process inspection reach only processes inside
  the sandbox; another process's command line and environment are denied on both channels.
- **Network:** open, except loopback, which is refused except the listed ports. Unix sockets are
  refused, with the ssh agent and container runtime sockets also denied by name.

## What may need adjusting

These are the parts written blind, most likely first:

- **Start-up reads.** Under `(deny default)`, a framework that reads one file not on the list
  stops a tool cold. Safehouse's list is the starting point, but this profile drops some of it and
  yolo's tools differ. The `log show` excerpt names the path.
- **The `-D` parameter forms.** The profile builds rules from parameters with `(param …)`,
  `string-append` and `if`. Safehouse's profiles use `define` and `string-append`; `param` and a
  top-level `if` are recalled from Chromium's profiles, not checked here. A loopback allow
  built as `(remote ip (string-append "localhost:" …))` is the least certain. If the profile fails
  to load on one of those lines, replace the expression with a literal `"localhost:<port>"` and
  report it.
- **Whether `localhost` in a network rule covers both `127.0.0.1` and `::1`.** Check d1 tests only
  the first.
- **Whether `launchctl submit` is refused by Seatbelt or by launchd.** Check b11 judges by whether the
  job appears outside, either way.
- **The `security` exit codes.** Check b4 counts exit 0 (found) or 44 (searched and not found) as
  "reached the keychain". Any other refusal code reads as a pass, so read its output.
- **`osascript -e 'do shell script …'`** with no target application runs inside the sandbox and
  escapes nothing, so check b9 sends it to System Events instead.
