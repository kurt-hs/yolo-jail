---
title: "macos-user's model: a second account, or a sandbox that starts from nothing"
date: 2026-10-22
status: in-review
stage: DESIGN
next: "Prototype a run-as-you, deny-by-default profile on a Mac (keychain, ssh agent, Apple Events, signals and loopback denied; one agent session works), then rule OQ-MM1"
tags: [macos-user, seatbelt, design, isolation, parity, safehouse, sandvault]
summary: "Today macos-user runs the agent as a second Mac account under a Seatbelt profile that allows everything and denies a short list (SandVault's model). The maintainer's 2026-10-22 direction is Safehouse's model instead: the agent runs as you under a profile that denies everything and allows a list, which is the closest macOS gets to podman's start-from-nothing container. This doc lays the three models side by side, maps podman's pieces onto the Safehouse model, and names what the second account actually buys and costs."
vantage:
  status-chip: true
---

# macos-user's model: a second account, or a sandbox that starts from nothing

**Status:** 2026-10-22 — written for the maintainer to weigh the models. Nothing here is built.
The maintainer's direction, in his words: the Safehouse model *"seems like … the right direction"*,
*"the separate user causes issues and doesn't actually solve much in the way of any security
concerns, except for user readable files"*, and the parity model with podman is *"the right model in
general"*. The prototype in [§6](#6-what-a-prototype-must-show) is what turns that direction into a
ruling.

**Needs your ruling:** [OQ-MM1](#OQ-MM1), [OQ-MM2](#OQ-MM2), [OQ-MM3](#OQ-MM3), [OQ-MM4](#OQ-MM4).

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
| `mounts` (read-only or read-write) | read, or read-write, allowed on each listed path | yes |
| `host_files` copied into the home | rendered into that `HOME` the same way | yes |
| Shared caches (mise, tool caches) | read-write allowed on the machine cache directories | yes |
| PID namespace: no host processes visible | cross-process inspection denied (built today); signals to processes outside the sandbox denied | yes |
| Network bridge with its own loopback | network open, and **the host's loopback is shared** | **gap**, see [OQ-MM3](#OQ-MM3) |
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

**What it buys.** Three things, and under a deny-by-default profile, policy replaces two of them:

1. **A backstop under the profile.** If the profile wrongly allows a read, Unix permissions still
   protect your owner-only files. Under the Safehouse model a missed rule exposes them. This is the
   one benefit nothing replaces, and it is the maintainer's *"except for user readable files"*.
2. **Separate per-user services.** As a different user, the agent can't reach your keychain,
   clipboard or Apple Events (which can drive your other apps). Running as you, a deny-by-default
   service list does the same; Safehouse denies the keychain unless asked.
3. **Separate privacy grants.** The second account has none of your Terminal's privacy grants. A
   Seatbelt deny holds regardless of those grants, so the profile covers this too.

**What it costs.** Every row of [§2](#2-the-three-models) marked "yes" in the SandVault column: the
admin setup, access grants, the projects folder and its open questions, the shared account home that
the two-workspace plan exists to work around, the credential bridges across accounts, and the
root-owned pack trees ([JD-10](../reference/macos-user-provisioning.md#JD-10)). It also refuses the
most natural place to put a project: your home folder.

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
- your loopback services, depending on [OQ-MM3](#OQ-MM3).

## 6. What a prototype must show

On a Mac, with a hand-written profile and no yolo changes:

1. An agent session (Claude) logs in from a home under the workspace, edits files and runs a build.
2. As the agent: reading `~/.ssh`, the login keychain (`security find-generic-password`), the ssh
   agent, `osascript` driving another app, the clipboard, and `kill` against one of your processes
   all fail.
3. `/opt/src/clientX` is unreadable; `/opt/homebrew` and the nix store are readable.

That run decides [OQ-MM1](#OQ-MM1). The existing macos-user policy suite
([§8.1](../research/agent-safehouse.md#81-a-policy-assertion-suite-for-macos-user--highest-value-moderate-cost))
is where its checks become permanent tests.

## 7. Open questions

1. <a id="OQ-MM1"></a>💬 **[`OQ-MM1`](#OQ-MM1) — Does macos-user move to the Safehouse model and retire the second account?**

   <!-- vantage: question id=OQ-MM1 leaning="Yes, once the prototype shows the keychain, ssh agent, Apple Events, clipboard and signals stay out; the hybrid is the fallback if they don't." -->

   It deletes the setup, access grants, projects folder, cross-account credential bridges and most
   of the two-workspace plan, and gives the podman mental model. It gives up the Unix-permission
   backstop under the profile.

   _Leaning:_ yes, after the [§6](#6-what-a-prototype-must-show) prototype passes; the hybrid if it
   does not.

   **Answer:**
   > _(empty — fill in when decided)_

2. <a id="OQ-MM2"></a>💬 **[`OQ-MM2`](#OQ-MM2) — Borrow Safehouse's profiles, or write our own allow list?**

   <!-- vantage: question id=OQ-MM2 leaning="Start from Safehouse's layered profiles (Apache-2.0, with attribution), adapted for nix and narrowed where they read wider than yolo wants, such as all of /opt." -->

   Their profiles are Apache-2.0 and split by layer (base, system runtime, network, toolchains,
   agents). They assume Homebrew rather than nix, and their system layer reads all of `/opt`, which
   would leave `/opt/src/clientX` readable.

   _Leaning:_ start from theirs, narrowed, with attribution.

   **Answer:**
   > _(empty — fill in when decided)_

3. <a id="OQ-MM3"></a>💬 **[`OQ-MM3`](#OQ-MM3) — Can the agent reach services on your loopback?**

   <!-- vantage: question id=OQ-MM3 leaning="Deny loopback by default and allow the ports yolo's own services use, matching podman, where the jail has its own loopback." -->

   On podman the jail has its own loopback, so your local database or dev server is out of reach
   unless yolo forwards it. On macOS the agent shares yours today, in every model. Seatbelt can deny
   outbound connections to loopback and allow named ports.

   _Leaning:_ deny by default, allow yolo's own service ports.

   **Answer:**
   > _(empty — fill in when decided)_

4. <a id="OQ-MM4"></a>💬 **[`OQ-MM4`](#OQ-MM4) — What happens to a Mac already set up for the second account?**

   <!-- vantage: question id=OQ-MM4 leaning="Launches stop using the account at once; yolo macos-setup gains a removal step that deletes the account and its grants on request, and a launch names that command once." -->

   Existing users have the `_yolojail` account, its passwordless switching, access grants on
   `/Users/Shared/yolo` and workspace state under it. This is a breaking change by design.

   _Leaning:_ stop using it at once, offer a removal command, and say so once at launch.

   **Answer:**
   > _(empty — fill in when decided)_

## Decision Ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| MM-R1 | The risk that an access grant on a home folder exposes `~/.ssh` is acceptable, not a blocker. | 2026-10-22 | [§4](#4-what-the-second-account-buys-and-what-it-costs) | — |
