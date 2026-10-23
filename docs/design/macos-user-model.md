---
title: "macos-user's model: a second account, or a sandbox that starts from nothing"
date: 2026-10-22
status: accepted
stage: DECIDED
next: "Prototype the deny-by-default profile on a Mac in both modes (§6), then seed the plan"
tags: [macos-user, seatbelt, design, isolation, parity, safehouse, sandvault]
summary: "Today macos-user runs the agent as a second Mac account under a Seatbelt profile that allows everything and denies a short list (SandVault's model). Ruled 2026-10-22: macos-user moves to Safehouse's model, the agent running as you under a profile that denies everything and allows a list, the closest macOS gets to podman's start-from-nothing container; the second account is kept from day one as an optional mode, off by default, on the same profile. This doc lays the models side by side, maps podman's pieces onto the Safehouse model, and names what the second account buys and costs."
vantage:
  status-chip: true
---

# macos-user's model: a second account, or a sandbox that starts from nothing

**Status:** 2026-10-22 — the model is ruled ([§7](#7-the-ruled-model-two-modes-one-profile)); nothing
is built. The [§6](#6-what-a-prototype-must-show) prototype is the check before building, not a
reopening of the ruling. No question remains.

**Needs your ruling:** none.

**Reads with:** [Agent Safehouse research](../research/agent-safehouse.md) (the evidence for every
Safehouse claim here), [SandVault and Safehouse compatibility](sandvault-safehouse-compat.md), and
[the configurable workspace root](configurable-workspace-root.md), whose questions this doc puts on
hold.

## 1. The problem in one example

You keep client code in `/opt/src/clientX` and your project in `/Users/Shared/yolo/alpha`. You run
`yolo -- claude` in alpha on macos-user. The agent can read `/opt/src/clientX`, and anything else
outside `/Users` that macOS lets every user read. It can't write there. Wherever the agent launches
makes no difference.

The reason is the profile's shape, not a missing rule: it begins with `(allow default)` and denies
reads only under `/Users`, on external disks, of raw disks and of keychains
([`seatbelt.go`](../../internal/macosuser/seatbelt.go)). Every new place a secret can live needs
another deny. Podman has no such list: a container starts empty and sees only what yolo puts in it.

## 2. The three models

| | **SandVault** (yolo today) | **Hybrid** | **Safehouse** |
| :--- | :--- | :--- | :--- |
| Who the agent runs as | a second account, `_yolojail` | a second account | **you** |
| Profile base | allow everything, deny a list | deny everything, allow a list | deny everything, allow a list |
| What people picture | "a second user who can't see other homes" | "podman, plus a second user" | "podman: it sees only what yolo lists" |
| Workspace inside your home | refused | refused | **works** |
| One-time admin setup | yes ([privilege lifecycle](macos-user-privilege-lifecycle.md)) | yes | **none** |
| Access grants on project folders | yes (`macos-fix-permissions`) | yes | **none**: the files are yours |
| A projects folder | yes ([configurable root](configurable-workspace-root.md)) | yes | **none** |
| Two workspaces at once | needs [a plan](macos-user-concurrent-workspaces.md): every workspace shares one account home | same | **each launch gets its own profile and home** |
| Agent logins | bridged across accounts ([Claude refresh](macos-user-claude-refresh.md), [keychain from a jail](keychain-from-a-jail.md)) | same | rendered into the launch's own home, as on podman |
| What stops a missed rule | Unix permissions on your 0600/0700 files | Unix permissions | nothing: the profile is the whole wall |

SandVault and yolo share the same file rules: yolo's profile calls itself *"SandVault-parity"*, so
both expose `/opt/src/clientX` in the same way. SandVault additionally denies mounting and three
system services; yolo plans to adopt those
([A2](sandvault-safehouse-compat.md)).

## 3. Podman, mapped onto the Safehouse model

The parity model: every piece of the podman jail gets the nearest macOS equivalent, and where there
is none the doc says so.

| Podman jail | Safehouse-model equivalent | Same? |
| :--- | :--- | :--- |
| Empty root filesystem from an image | `(deny default)` | yes |
| The image's `/usr`, `/bin`, `/nix/store`, read-only | read allowed on the system and nix paths tools need | yes |
| Workspace bound read-write at `/workspace` | read-write allowed on the workspace **at its real path** | **no path relocation** |
| `/home/agent`, backed by `.yolo/home` | `HOME` set to `<workspace>/.yolo/home`, read-write allowed | yes, at a real path |
| `mounts` (read-only or read-write), bound at `/ctx/<name>` | a link at `$YOLO_CONTEXT_DIR/<name>`, with read, or read-write, allowed on the original path because the link needs it (what macos-user already does, [context mounts](../reference/context-mounts.md)) | yes: `$YOLO_CONTEXT_DIR/<name>` is the one supported path on every backend ([OQ-MM5](#OQ-MM5)) |
| `host_files` copied into the home | rendered into that `HOME` the same way | yes |
| Shared caches (mise, tool caches) | read-write allowed on the machine cache directories | yes |
| PID namespace: no host processes visible | cross-process inspection denied (built today); signals to processes outside the sandbox denied | yes |
| Network bridge with its own loopback | network open; **the host's loopback denied** except yolo's own service ports and ports the workspace config allows ([OQ-MM3](#OQ-MM3)) | yes |
| An environment yolo builds from scratch | the same: the launcher builds the environment | yes |
| A user that is not you | **you** | **the trade, see [§4](#4-what-the-second-account-buys-and-what-it-costs)** |
| No host services | system services denied by default, allowing a short list (Safehouse allows 16) | yes |

**The two real differences.**
- **No path relocation.** Seatbelt only allows or denies real paths. There is no unprivileged bind
  mount on macOS, so the agent sees `/Users/you/code/alpha`, not `/workspace`. Anything that assumes
  `/workspace` or `/home/agent` must read `$PWD` and `$HOME` instead. (A root-level `/workspace`
  through `/etc/synthetic.conf` is per machine and needs a reboot, so it can't serve two
  workspaces at once.)
- **Same identity.** Covered next.

## 4. What the second account buys, and what it costs

**What it buys.** Three things. Only the first is lost by moving to the Safehouse model; a
deny-by-default profile replaces the other two, so they are no reason to keep the account:

1. **A backstop under the profile — lost, and small.** Under deny-by-default a *missing* rule fails
   closed: a tool that needs a path breaks, and nothing is exposed. Exposure needs an allow rule
   that is too broad (a parent folder instead of the one subfolder a tool needs, such as all of
   `/opt` instead of `/opt/homebrew`) or a Seatbelt bug. Only then would Unix permissions on your
   owner-only files still have stopped the read under a second account. Both are reviewable: the
   allow list is short and each allow is tested ([§6](#6-what-a-prototype-must-show)). This is the
   one benefit nothing replaces, and it is the maintainer's *"except for user readable files"*.
2. **Separate per-user services — replaced.** As a different user, the agent can't reach your
   keychain, clipboard or Apple Events (which can drive your other apps). Running as you, a
   deny-by-default service list does the same; Safehouse denies the keychain unless asked.
3. **Separate privacy grants — replaced.** Running as you, the agent would inherit macOS privacy
   grants you gave your terminal, such as Full Disk Access, which a second account does not have.
   That should not matter: macOS checks a Seatbelt deny separately from those grants, so a file the
   profile denies stays denied even under Full Disk Access. This is expected, not measured;
   [§6](#6-what-a-prototype-must-show) checks it.

**What it costs.** Every row of [§2](#2-the-three-models) marked "yes" in the SandVault column: the
admin setup, access grants, the projects folder and its open questions, the shared account home that
the two-workspace plan exists to work around, the credential bridges across accounts, and the
root-owned pack trees ([JD-10](../reference/macos-user-provisioning.md#JD-10)). It also refuses the
most natural place to put a project: your home folder.

It costs every user a second mental model on top of the sandbox: two accounts, access grants
(ACLs) that let the agent's account reach a folder, and which folders are shared between the two.
Podman has none of that, and neither does the Safehouse model, where the files are simply yours
and the profile is the only thing to understand.

**The stray-grant risk is not a blocker.** The maintainer ruled on 2026-10-22 that the risk of an
access grant on a home folder exposing `~/.ssh` is *"manageable, acceptable, not a blocker"*. Under
the Safehouse model no such grant exists anyway.

## 5. What the Safehouse model must deny explicitly

Running as you, everything the second account used to keep out must be named in the profile:

- your home folder, apart from the workspace if it sits there (deny by default covers it);
- the keychain: the files and the security service lookup;
- the ssh agent socket, by its file path and its network spelling (Safehouse has a module for exactly
  this);
- Apple Events, the clipboard, accessibility and notification services;
- signals to your processes, and inspecting them;
- your loopback services, except yolo's own and the ports the workspace allows ([OQ-MM3](#OQ-MM3)).

## 6. What a prototype must show

On a Mac, with a hand-written profile and no yolo changes:

1. An agent session (Claude) logs in from a home under the workspace, edits files and runs a build.
2. As the agent: reading `~/.ssh`, the login keychain (`security find-generic-password`), the ssh
   agent, `osascript` driving another app, the clipboard, and `kill` against one of your processes
   all fail.
3. `/opt/src/clientX` is unreadable; `/opt/homebrew` and the nix store are readable.
4. With Full Disk Access granted to the terminal, a file the profile denies is still unreadable.
5. Nothing escapes through a service running as you: `open -a Terminal`, `osascript -e 'do shell
   script …'` and `launchctl submit` all fail from inside.
6. Claude logs in and stays logged in with the keychain denied to the sandbox.
7. Steps 1 to 3 pass again with the same profile run as the second account.

The [prototype runbook](../plans/runbooks/macos-user-model-prototype/README.md) is that profile
and a script that runs these checks in both modes. That run is the gate before building. The existing macos-user policy suite
([§8.1](../research/agent-safehouse.md#81-a-policy-assertion-suite-for-macos-user--highest-value-moderate-cost))
is where its checks become permanent tests.

## 7. The ruled model: two modes, one profile

Ruled 2026-10-22 ([OQ-MM1](#OQ-MM1), [MM-R2](#decision-ledger)):

- **One profile, deny by default.** Every macos-user launch runs under a deny-by-default Seatbelt
  profile covering files, system services, signals and process inspection, built as
  [§3](#3-podman-mapped-onto-the-safehouse-model) maps podman's pieces. Which user runs it is a
  parameter of the profile, not a second profile.
- **Default mode: the agent runs as you.** No account, admin setup, access grants or projects
  folder. A workspace may be anywhere, including your home folder. Everything [§5](#5-what-the-safehouse-model-must-deny-explicitly)
  lists is denied.
- **Optional mode: the second account, off by default, built from day one.** The same profile run
  as `_yolojail`. The mode alone brings the account's setup and switching, and the access grants
  that let the account read and write the workspace, because Unix permissions still decide file
  access under a different user. The maintainer's reason for building it now: *"we already have a
  lot of this machinery built, and I think it's going to help us smooth out the rest of it."*
- **Shared by both modes:** a per-launch `HOME` under the workspace's `.yolo/home`, so two
  workspaces run at once in either mode without a shared account home; mounts at their original
  path with a context-dir link; the host keychain reached only through the loophole the
  [keychain design](keychain-from-a-jail.md#41-macos-user-no-seam-and-probably-no-keychain) rules
  ([OQ-KC4](keychain-from-a-jail.md#8-decision-ledger): *"always the host's keychain"*), never by the sandbox.
- **The profile starts from Safehouse's** layered profiles (Apache-2.0, credited), adapted to nix
  and narrowed wherever they read wider than yolo wants: install trees such as `/opt/homebrew`,
  never a parent such as `/opt` ([OQ-MM2](#OQ-MM2)).
- **Loopback is denied by default**, as podman's own loopback keeps your local services out of
  reach. yolo's own service ports are allowed. A workspace may allow more ports in its config; a
  change to that list goes through the same config-change approval as any other workspace config
  edit ([config safety](../reference/config-safety.md)). The key's name is the plan's to choose
  ([OQ-MM3](#OQ-MM3)).
- **Mounts are reached at `$YOLO_CONTEXT_DIR/<name>`** on every backend. Containers do not also bind
  them at the original host path. On macOS the profile allows the original path only because the
  link resolves there; a script that uses the original path directly works on macOS by accident,
  and nothing promises it ([OQ-MM5](#OQ-MM5)). Every backend also puts a `ctx` link in the agent's
  home pointing at `$YOLO_CONTEXT_DIR`, so mounts are browsable from where the agent starts;
  `$YOLO_CONTEXT_DIR` stays the documented path ([OQ-MM6](#OQ-MM6)).
- **No transition for Macs already set up for the second account.** Launches move to the default
  mode, and nothing migrates, cleans up or announces the old account. Reusing an existing account
  for the optional mode is welcome if it falls out of the build, but is not required
  ([OQ-MM4](#OQ-MM4)).
- **No per-language home grants.** Unlike Safehouse's toolchain files, the profile does not open
  your home's toolchain folders: tools run with the agent's own home. Package caches are shared
  with your real caches by the general rule for every backend
  ([OQ-BF11](../reference/disk-footprint.md#OQ-BF11)), which needs the default mode's
  ownership; their config files, which hold registry tokens, never are.
- **Selecting the mode** is a user-scope setting; a workspace config cannot choose it. Its name and
  spelling are the plan's to choose.
- **What done looks like:** with no setting, `yolo -- claude` in `~/code/alpha` launches with no
  admin step, and the agent cannot read `/opt/src/clientX`, `~/.ssh` or the keychain; with the
  optional mode set on a Mac where setup ran, the same launch runs as `_yolojail` with the same
  denies; two workspaces launch at once in either mode.

## 8. Open questions

None. Every question this doc raised is ruled; see the [Decision Ledger](#decision-ledger).

## Decision Ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| MM-R1 | The risk that an access grant on a home folder exposes `~/.ssh` is acceptable, not a blocker. | 2026-10-22 | [§4](#4-what-the-second-account-buys-and-what-it-costs) | — |
| <a id="OQ-MM1"></a>[OQ-MM1](#OQ-MM1) | macos-user moves to Safehouse's model: a deny-by-default profile, the agent running as you by default. The [§6](#6-what-a-prototype-must-show) prototype is the gate before building. | 2026-10-22 | [§7](#7-the-ruled-model-two-modes-one-profile) | pending |
| MM-R2 | The second account is kept from day one as an optional mode, off by default, running the same profile; it alone carries account setup and workspace access grants. | 2026-10-22 | [§7](#7-the-ruled-model-two-modes-one-profile) | pending |
| <a id="OQ-MM2"></a>[OQ-MM2](#OQ-MM2) | Start from Safehouse's layered profiles (Apache-2.0, credited), adapted for nix and narrowed where they read wider than yolo wants, such as all of `/opt`. | 2026-10-22 | [§7](#7-the-ruled-model-two-modes-one-profile) | pending |
| <a id="OQ-MM3"></a>[OQ-MM3](#OQ-MM3) | Deny loopback by default and allow yolo's own service ports, matching podman; a workspace config may allow more ports, through the config-change approval. | 2026-10-22 | [§7](#7-the-ruled-model-two-modes-one-profile) | pending |
| <a id="OQ-MM4"></a>[OQ-MM4](#OQ-MM4) | No transition for Macs already set up for the second account; reusing the account for the optional mode is welcome, not required. | 2026-10-22 | [§7](#7-the-ruled-model-two-modes-one-profile) | pending |
| <a id="OQ-MM6"></a>[OQ-MM6](#OQ-MM6) | A `~/ctx` link to `$YOLO_CONTEXT_DIR` in the agent's home on every backend; `$YOLO_CONTEXT_DIR` stays the documented path. | 2026-10-22 | [§7](#7-the-ruled-model-two-modes-one-profile) | built: podman via the home skeleton (skipped when a config entry claims `ctx`); Apple Container and macos-user via the `link_context_dir` boot step (macos-user retargets its own link per workspace); macos-user is unit-tested only, not yet run on a Mac; Apple Container is untested |
| <a id="OQ-MM5"></a>[OQ-MM5](#OQ-MM5) | Containers do not also bind mounts at their original host path; `$YOLO_CONTEXT_DIR/<name>` is the one supported path, and macOS allows the original path only for the link. | 2026-10-22 | [§7](#7-the-ruled-model-two-modes-one-profile) | — |
