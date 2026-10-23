---
status: current
verified: 2026-10-22
summary: "yolo defends against a misguided, over-helpful agent, not a malicious one. Treating the agent as an adversary means defending a pile of host-execution channels no development tool can close, so the goal is narrower: keep credentials and wider access out of an eager agent's reach, and make anything it should not do visibly wrong, so that it stops."
tags: [principle, trust, security, threat-model, workspace, credentials]
---

# The misguided-agent principle: defend against an eager helper, not an adversary

**Status:** PRINCIPLE, adopted 2026-10-22.

**Author:** Matt Schulkind, the maintainer. In his words: *"We will never protect against a
malicious adversary. We will protect against misguided idiots, basically"*, and *"we're trying to
make it hard enough for an agent to do the wrong thing that they realize it's the wrong thing and
they won't do it."*

**Audience:** anyone deciding whether yolo should build a defense, a refusal, a lock or a gate, or
whether to share something between a jail and the host. Read
[host execution from the workspace](host-execution-from-the-workspace.md) with it: that audit is
the evidence, and this is the conclusion drawn from it.

**Sibling principles:** [`happy-path-principle.md`](happy-path-principle.md) (every stop names the
next step), [`gate-placement-principle.md`](gate-placement-principle.md) (whether a prompt earns
its place).

## The principle

**The agent yolo defends against is misguided, not malicious.** It is a proactive helper. It goes
looking for a credential to finish the task, reaches for wider access because it would help, or
edits something outside its task because it seems useful. It is not trying to escape, and when an
action is plainly something it should not do, it stops.

So a defense has one job: **put the wrong thing out of easy reach, or make it visibly wrong.**
That means one of three things:

1. **Absent.** The thing is not in the jail at all. A credential an agent cannot find, it cannot
   misuse, however helpful it means to be.
2. **Refused, with a reason.** The attempt fails with a message that says this is off limits and
   what to do instead ([the happy-path principle](happy-path-principle.md)).
3. **Clearly a line.** Reaching it takes a step no well-meaning agent would mistake for part of its
   task, such as editing a lock it was told not to touch.

A defense does **not** have to hold against an agent working to defeat it.

## Why: the adversary pile

[The host-execution audit](host-execution-from-the-workspace.md) inventories how a jail session can
leave something on the host that later runs as you. Against an adversary, every one of them needs a
defense:

- **Every source file you build or run on the host** ([the product cell](host-execution-from-the-workspace.md#the-two-axes)).
  Developing in a jail and running the result is the product.
- **Files that run when you open or enter the folder**: `.envrc`, `mise.toml`, editor and agent
  settings.
- **Files you never look at**: `.git/hooks`, `.git/config`, `.git/info/exclude`, which run on
  `git status` or `git commit`.
- **Folders host tools load without asking**: `node_modules`, `.venv`, build output.
- **Host-side watchers** that run on every save, before anyone reads the change.

Closing all of them means a read-only workspace, no host-side tools and no development, which is no
longer the product. **Against a determined adversary yolo cannot win and stay a development tool,
so it does not try.** It spends its effort where a misguided agent actually goes.

## How to apply it

**The test:** *would a well-meaning agent reach this by accident, or on plausible initiative while
trying to help?*

- **Yes:** close it. Make it absent, refused, or clearly a line.
- **Only by deliberate subversion:** out of scope. Do not build a mechanism for it, and do not
  warn about it.

**What it keeps:**

- **The credential boundary stays structural.** An eager agent uses whatever credential is at
  hand, so tokens stay out of the jail entirely: yolo brokers access, and a jail never holds a
  publish token, an ssh key or your keychain. This is the principle's main application, not an
  exception to it.
- **Disclosure stays.** What a launch exposes is printed, because the human deciding is the one it
  serves.
- **Fail-closed defaults stay** where they cost nothing, such as a deny-by-default sandbox profile,
  where a missing rule breaks a tool instead of exposing a file.

**What it rules out:**

- **Warnings about channels only an adversary uses.** Telling a developer that building their own
  checkout runs its code is noise; that is what building means.
- **Mechanisms that must enumerate an upstream list to work**, such as a git wrapper clearing every
  config key that can run a program. They protect only against a deliberate attacker, and only
  until the list grows.
- **Defaults that make the common workflow worse to stop a deliberate act**, such as locking
  `.git` in every workspace by default.

**Prompt injection** turns an agent malicious, so it falls outside this principle. What still
limits it is the structural part: an injected agent finds no credentials in the jail either.

## Applications so far

| Decision | How the principle decides it |
| :--- | :--- |
| Locking the `.git` control plane is a config choice, not a default ([OQ-HX2](host-execution-from-the-workspace.md#oq-hx2)) | Planting a git hook is deliberate; the default would cost every workspace for an adversary case. |
| The source-update prompt no longer warns that updating runs the checkout's code (2026-10-22) | Running what you develop is the product; the warning was aimed at an adversary. |
| macos-user denies by default and runs as you ([macos-user's model](../design/macos-user-model.md#7-the-ruled-model-two-modes-one-profile)) | The profile makes off-limits files absent; a second account's extra layer guarded mostly against deliberate escapes. |
| Credentials reach a jail only through brokers and loopholes, never as files | An eager agent would use any token it finds. |
| Package caches are shared with the host, their config files never ([OQ-BF11](disk-footprint.md#OQ-BF11)) | Poisoning a cache takes intent; a config file's publish token is what a helpful agent would reach for. |
