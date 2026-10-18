---
title: "One jail, several profiles: each session reads its own agent config"
date: 2026-10-18
status: accepted
stage: DESIGN
next: "Build per the implementation plan: the jail slices, then the host slice (OQ-SP4)"
tags: [design, profiles, attach, sessions, pi, pi-subagents, providers, concurrency]
summary: "An attach renders the attaching entry's profile into files every running pi session reads later, so a second session on another provider (or a bare attach of an unrelated agent on the default profile) changes what the first session's pi-subagents children run on, while the first session's provider policy still forbids that provider. The fix keys every profile-dependent read on the session: each exec already carries a session id that its whole process tree inherits, so the attach writes its environment into a per-session entry snapshot, and the session's own boot pass renders a pack's profile-dependent files into a per-session copy of the program's config dir, which stock pi follows through PI_CODING_AGENT_DIR. Shared state (logins, session history, packages, caches) stays one copy, linked into every session's dir."
---

# One jail, several profiles: each session reads its own agent config

**Status:** 2026-10-18. Accepted: [OQ-SP1](#11-decision-ledger) to [OQ-SP4](#11-decision-ledger) ruled; nothing built. Evidence read at `8bf1afce9` and in stock pi 1.0.1 and
pi-subagents 0.79.1. MEASURED: stock pi 1.0.1 reads `models.json` from a directory named by
`PI_CODING_AGENT_DIR`, and creates `auth.json` and `models-store.json` there when they are absent
(`pi --list-models`, offline, 2026-10-18). UNMEASURED: everything a session does with the design.

> **In short.** A profile is a property of a session's process tree, not of the jail. A pi session's
> profile breaks today because every later entry re-renders the files its children read. Key each
> of those reads on the session id every exec already carries, and no later entry can change them.

**Why it matters.** Two sessions on two providers in one jail deadlock the older one's subagents.
A bare `yolo -- agy` on the default profile does the same to a running pi, though agy shares nothing
with pi ([§2](#2-what-an-attach-rewrites-today)).

**The shape.** Two per-session artifacts under one key: the **entry snapshot** (the attach's env,
host-written) and the **session home** (a pack's profile-dependent files, rendered in the jail),
plus links back to the one shared copy of everything else.

**Cost.** One small directory per session for the jail's lifetime; pi's in-session setting changes
reach other sessions only through capture, at the next entry ([OQ-SP2](#11-decision-ledger)); jails launched before it keep today's
behavior.

**Start at [§4](#4-the-design-session-homes-over-entry-snapshots).** [§2](#2-what-an-attach-rewrites-today)
is the cause, file by file.

**Needs your ruling:** None.

**Reads with:** [`per-session-profiles-plan.md`](per-session-profiles-plan.md) (the implementation
sketch; incomplete while questions are open),
[`attach-daemon-activation.md`](attach-daemon-activation.md) (coins the entry snapshot this design
builds on, and leaves rendered config out of its scope),
[`simultaneous-auth-and-pack-isolation.md`](simultaneous-auth-and-pack-isolation.md) (the
process-local provider policy), [`model-lists-and-pickers.md`](model-lists-and-pickers.md) (the host's
per-process `-p` and its pi-subagents gap).

---

## 1. Recommendation and principles

Build **session homes** over **entry snapshots**. The maintainer's report (2026-10-18, paraphrased
by the lead): a pi on provider A, then `yolo -p B -- pi` attached; B works, A's main session works,
A's new subagents pick up B's model restrictions while A's process may not call B, and relaunching on
A flips which one breaks. With B the default, a bare `yolo -- agy` breaks pi the same way. *"We've
discussed this before and still need a solution."*

1. <a id="SP-P1"></a>**P1. A profile belongs to a session's process tree.** The key is the session id
   every exec already carries ([§4.1](#41-terms)). A child inherits its parent's key; nothing else
   chooses it.
2. <a id="SP-P2"></a>**P2. No entry writes anything another session reads after it started.** An
   attach may write its own session's files and the jail's shared compatibility files; nothing a
   running session reads is among the latter once this ships.
3. <a id="SP-P3"></a>**P3. Shared state stays one copy.** Logins, session history, installed packages,
   caches and run records are linked, never copied, so a login in one session serves every other.
4. <a id="SP-P4"></a>**P4. Core stays agent-blind and fork-free.** A pack declares which directory its
   program reads its config from and the variable that relocates it; core knows no agent and no fork.
   For pi the variable is stock pi's own `PI_CODING_AGENT_DIR`.
5. <a id="SP-P5"></a>**P5. Independence, not detection.** Core cannot tell `agy` from `pi`, so the design
   never asks which command an attach runs. A session whose command never starts pi still gets a pi
   session home, rendered for its own profile, and nobody else reads it.

## 2. What an attach rewrites today

Every attach, with or without `-p`, and whatever command it runs, composes the channel from the
current config plus its `-p` (no `-p` means the configured `profile`), writes it into shared files,
and then execs the entrypoint, whose **session pass** re-runs the boot's render from those files.

### 2.1 The writers, file by file

| # | File (jail path) | Written by, on every attach | Profile-dependent content |
| :--- | :--- | :--- | :--- |
| H1 | `~/.config/yolo-user-env.sh` | host, `deliverChannel` ([`agentenvfiles.go:66-75`](../../internal/cli/run/agentenvfiles.go#L66-L75)), called at [`run.go:3070`](../../internal/cli/run/run.go#L3070) → [`:3227`](../../internal/cli/run/run.go#L3227) | the wire tables `YOLO_PROVIDERS`, `YOLO_PROFILES`, `YOLO_USE_PROFILES`, plain-form ([`profilechannel.go:541-558`](../../internal/cli/run/profilechannel.go#L541-L558)) |
| H2 | `~/.config/yolo-agent-env/pi.sh` | host, same call, replaced whole | pi's env derive: `YOLO_PI_PROVIDER_POLICY`, `AWS_REGION`, the OpenAI-login prelaunch switches ([`derive.lua:2040`](../../packs/pi/derive.lua#L2040)) |
| H3 | `~/.config/yolo-agent-env/provider-keys/pi.sh` | host, same call | provider variables exactly as the profile says, others unset |
| J1 | `~/.pi/agent/settings.json` | jail, the session pass's `configure_pack_surfaces` ([`bootsteps.go:439-449`](../../internal/entrypoint/bootsteps.go#L439-L449)), after `hydrate_user_env` read H1 ([`:161`](../../internal/entrypoint/bootsteps.go#L161), [`boot.go:205-231`](../../internal/entrypoint/boot.go#L205-L231)) | `defaultProvider`, `defaultModel`, `enabledModels`, and the whole pi-subagents `subagents` block: `defaultModel`, `modelScope.allow`, role overrides ([`derive.lua:1208`](../../packs/pi/derive.lua#L1208), [`:1313`](../../packs/pi/derive.lua#L1313), [`:1664`](../../packs/pi/derive.lua#L1664)) |
| J2 | `~/.pi/agent/models.json` | jail, same pass | a via row for the selected provider; profile options on rows ([`derive.lua:786`](../../packs/pi/derive.lua#L786)) |
| J3 | `~/.pi/agent/yolo-model-lists.json` | jail, same pass | each provider's `enforce` switch from the active set ([`derive.lua:1757`](../../packs/pi/derive.lua#L1757)) |
| J4 | `~/.pi/agent/yolo-openai-codex-models.json` | jail, same pass | `enforce` from the governing profile ([`derive.lua:1710`](../../packs/pi/derive.lua#L1710)) |
| J5 | `~/.pi/agent/mcp.json`, `~/.config/mcp/mcp.json` | jail, same pass | servers gated on variables only pi's env file carries ([`mcp.go:290-316`](../../internal/entrypoint/mcp.go#L290-L316)) |

The session pass reads the selection from its environment, which `hydrate_user_env` has just
overwritten from H1 ([`surfaceloop.go:213`](../../internal/entrypoint/surfaceloop.go#L213)). So J1-J5
always hold the **latest entry's** profile. Two docs say otherwise and are wrong against the code: "a
running jail is never re-rendered" ([`pack-system.md`](../reference/pack-system.md#open-rulings-3)) and
"generators … read only that frozen environment"
([the composed-file permissions reference](../reference/composed-file-permissions.md#L391)). Both are recorded for
correction in [§9](#9-sequencing).

### 2.2 The readers, and which read per child

Stock pi-subagents runs a foreground child **in the parent's process**, and a background child in a
detached runner that inherits the parent's environment; neither goes through yolo's launcher
(pi-subagents 0.79.1, `src/runs/shared/child-session.js`, `async-execution.js:504-522`).

| Reader | Reads | When |
| :--- | :--- | :--- |
| pi, at start | J1, J2; H2 and H3 through its launcher | once; J1 again only on `/reload`; J2 again on a model-runtime refresh |
| yolo's pi extensions, at load | J3, J4 at a hard-coded `~/.pi/agent` path ([`yolo-model-lists.js:36`](../../packs/pi/extensions/yolo-model-lists.js#L36), [`yolo-openai-auth.js:110`](../../packs/pi/extensions/yolo-openai-auth.js#L110)) | at every pi start **and every child**, which loads extensions afresh |
| pi-subagents, per spawn | J1's `subagents` block | **every subagent call**: `discoverAgents` re-reads `settings.json` whenever its size or mtime changed |
| each child session | J1, J2, extensions from `getAgentDir()` | at its own start |
| a new `pi` typed later in the same session | H2, H3, then J1-J5 | at its start |
| the provider policy | `YOLO_PI_PROVIDER_POLICY` from the process environment ([`yolo-provider-policy.js:169-181`](../../packs/pi/extensions/yolo-provider-policy.js#L169-L181)) | inherited, so a child keeps its **parent's** |

### 2.3 The deadlock, step by step

1. Session A starts pi on profile A. Its environment holds A's provider policy; J1 holds A's
   `subagents` block.
2. Session B attaches with `-p B`. Its pass rewrites J1-J4 for B.
3. A's pi-subagents spawns a child. It re-reads J1: default model `b/…`, scope `b/*`. The child
   inherits A's policy, which refuses provider B. Every subagent fails.
4. A new session on A rewrites J1 for A, and B's children fail the same way.
5. `yolo -- agy` with no `-p` composes the configured profile for **every** agent, pi included, and
   step 2 happens with the default profile.

Without a `subagents` default, pi-subagents gives a child the parent's in-memory model on purpose,
so that another session's change to the shared file does not leak in
(pi-subagents `model-resolution.js:313-348`, upstream issue 266). yolo's render is what puts the
shared default back.

### 2.4 What is already process-local

- **The provider policy**, in the environment ([§3.1 of the isolation design](simultaneous-auth-and-pack-isolation.md#31-the-request-boundary)).
- **Each session's own wire tables**: the session pass `os.Setenv`s what it hydrated
  ([`boot.go:229`](../../internal/entrypoint/boot.go#L229)), so a session's shell holds its entry's
  `YOLO_USE_PROFILES`, until a nested interactive shell sources H1 again
  ([`shell.go:248`](../../internal/entrypoint/shell.go#L248)).
- **The session id**: every exec, the first session's included, carries `YOLO_SESSION_ID`
  ([`run.go:3113-3114`](../../internal/cli/run/run.go#L3113-L3114),
  [`jailmain.go:269`](../../internal/cli/run/jailmain.go#L269)), and every descendant inherits it.

## 3. Prior rulings this rests on

No ruling covers two profiles in one jail. These bound the design:

| Source | What it says | Bearing |
| :--- | :--- | :--- |
| [OQ-MM5 / MM-D30](model-lists-and-pickers.md#MM-D30) (ruled 2026-10-05) | *"The vehicle is per-process flags, never a file, so the user's own config files are untouched and concurrent host launches cannot collide."* | the goal (no collision); its letter is host-only, and in a jail MM-D30 *"does nothing"* |
| [MM-D42](model-lists-and-pickers.md#MM-D42) (2026-10-09, amended 2026-10-16) | a host `-p` must govern pi-subagents' defaults and scope; *"yolo shouldn't depend on my custom pi fork in any way, or even know it exists."* | the fork-free constraint |
| [roadmap](../plans/roadmap.md#immediate-continuation) | *"Do not invent a yolo settings overlay or rewrite shared settings."* | written about the host's pi-subagents gap; [OQ-SP1](#11-decision-ledger) asks whether it covers this |
| [isolation §3.1](simultaneous-auth-and-pack-isolation.md#31-the-request-boundary) (PAS-D2) | *"The policy belongs to the process invocation … A launch does not change another process already running."* | P1 extends this from the policy to the config |
| [isolation §3.1](simultaneous-auth-and-pack-isolation.md#31-the-request-boundary) | *"Do not infer one from … a previous invocation's persistent selection."* | the deadlock is exactly that inference |
| [attach-daemon-activation §4.2](attach-daemon-activation.md#42-publication-and-concurrent-attaches) and [§6](attach-daemon-activation.md#6-the-jail-owns-daemons-sessions-own-commands) (unbuilt) | entry snapshots so launchers read *"its files, not whichever attach last rewrote the shared files"*; *"Shared pack-rendered configuration remains the existing system's limitation, explicitly outside this proposal."* | this design is that missing half |
| [OQ-ES5](credential-sources-separation.md#10-decision-ledger) (ruled 2026-10-05) | an attach `-p` still delivers its profile's key into the jail | unchanged: a snapshot is readable by the jail's uid, like H2 |
| [active-provider-sets §4.10](active-provider-sets.md#410-state-that-already-exists) | *"an attach rewrites the agent's env file whole"* | replaced for snapshot-aware jails |

## 4. The design: session homes over entry snapshots

### 4.1 Terms

- **Session**: one yolo invocation and the command tree it runs
  ([jail lifetime §1.1](jail-lifetime-last-session-wins.md#11-terms)); here identified by its
  `YOLO_SESSION_ID`.
- **Entry snapshot**: one immutable set of an entry's channel and per-agent env files, coined in
  [attach-daemon-activation](attach-daemon-activation.md#terms-used-below). This design needs only its
  environment half.
- **Session home** *(coined here)*: a per-session copy of the one directory a pack's program reads its
  config from, holding that session's render of the pack's surfaces in that directory and links to
  everything else in it. It is **not** a private agent home: logins, history and packages are the
  shared copies.
- **Session-home declaration** *(coined here)*: a pack's statement of that directory and of the
  variable that relocates the program to another one. For pi: `.pi/agent` and `PI_CODING_AGENT_DIR`.

### 4.2 Who writes what

| State | Sole writer | Readers |
| :--- | :--- | :--- |
| Entry snapshot of session S | the host launcher of S, before S's exec, under the launch lock | S's session pass; launchers and new shells in S |
| Session home of S | S's session pass (render and links); then the program in S | every process of S, children included |
| Shared H1-H3 | each entry, as today (compatibility for jails and processes without a session id) | old-jail sessions only |
| Shared J1-J5 inside a session home's directory | the jail's own boot (pid 1) only | nothing in a snapshot-aware jail; kept as the capture target and the default |
| The shared overlay (captured edits) | the render pass that captures, under the render lock ([§4.5](#45-pis-own-writes)) | every later render |

### 4.3 An entry, in order

1. **Compose and write the snapshot.** The launcher composes the channel as today and writes it to
   the snapshot for this exec's session id, complete, before the exec. It still writes H1-H3.
2. **Exec with the id.** Unchanged: `YOLO_SESSION_ID` rides the exec.
3. **Hydrate from the snapshot.** The session pass reads its own snapshot's shared file instead of
   H1. No snapshot for the id (an older launcher): H1, as today.
4. **Render session homes.** For every selected pack with a session-home declaration, the pass
   creates the session home, renders the pack's surfaces whose paths lie in the declared directory
   into it, and links the rest ([§4.4](#44-what-a-session-home-holds)). Surfaces outside every declared
   directory keep today's every-pass render.
5. **Export the relocation.** The pass exports the declared variable, pointing at the session home,
   into the session's environment before it execs the shell. Every process of the session inherits
   it: the agent, a pi typed in its shell, pi-subagents' in-process children and its detached runner.
6. **Launchers and new shells read the snapshot.** An agent's launcher sources its env and
   provider-keys files from `$YOLO_SESSION_ID`'s snapshot when one exists, H2/H3 otherwise;
   `.bashrc` does the same for H1.

A fresh launch's first session is an entry like any other. The jail's own boot (pid 1) renders the
shared copies as today.

### 4.4 What a session home holds

- **Rendered:** each surface the pack declares inside the directory. For pi: `settings.json`,
  `models.json`, `yolo-model-lists.json`, `yolo-openai-codex-models.json`, `mcp.json`. The render is
  the full file, the shared overlay's captured edits included, so a user key (theme, `packages`) is
  there as it is in the shared file.
- **Linked:** every other entry present in the shared directory when the home is created (`auth.json`,
  `sessions/`, `extensions/`, `skills/`, `npm/`, `git/`, `AGENTS.md`, `models-store.json`, …), each
  as a symlink to the shared path, so the pack's own hooks (the shared-credentials link) stay the
  one authority.
- **Linked though absent:** each name the declaration lists as shared. A directory is created in the
  shared directory first; a file is linked dangling, so its first write creates the shared file. This
  rule is load-bearing: stock pi creates `auth.json` and `models-store.json` at startup when absent
  (measured), and a session home without the link would start logged out.
- **Pi's `~/.agents` rule.** With `PI_CODING_AGENT_DIR` set, pi-subagents reads user agents from
  `<dir>/agents` and no longer from `~/.agents` (pi-subagents 0.79.1, `agents.js:2435`). So the pi
  pack declares `agents` as a link to `~/.agents` when that exists, else to the shared `agents/`,
  which is stock behavior exactly.
- **Never:** a copy of a shared entry.

A **stray** *(coined here)* is an entry the program created in a session home that the declaration
did not foresee, or a link the program replaced with a regular file by renaming over it. Strays stay
where they are, so nothing is lost, and the next render pass names each one in one warning line,
saying the pack's declaration should list it.

### 4.5 Pi's own writes

Pi writes `settings.json` itself: the "set default" model action, `/settings` toggles,
`lastChangelogVersion`, `pi install`; pi-subagents writes agent overrides. With a session home these
land in the session's copy, merged under pi's own lock. They reach later sessions by capture
([OQ-SP2](#11-decision-ledger)):

- **Every render pass captures** each session home's rendered files into the shared overlay, by the
  existing capture rule for that surface, before it renders. The pass holds an in-jail render lock
  across capture and render, so two attaches capture and render one at a time.
- A file that does not parse (pi mid-write) is skipped this pass, never treated as a deletion.
- Two sessions that changed the same key: the one captured last wins.
- A running session keeps its own copy; the capture reaches sessions entered after it.
- The fresh launch's boot captures every session home left by the previous container, then deletes
  them.

### 4.6 Lifetime and failure

- **Lifetime.** Session homes and snapshots live until the jail is known gone, never reaped while it
  runs: a background runner can outlive its session. They sit beside the declared directory in the
  pack's own writable state (`~/.pi/.yolo-sessions/<id>/agent`), so a container restart keeps them for
  the next boot's capture.
- **The user's own value wins.** A session whose environment already sets the declared variable
  (an `env_sources` entry, a shell `export`) gets no session home for that pack, and the pass says so
  in one line.
- **A home that cannot be made** (disk full, a link refused): the pass refuses the session with the
  path and the error, as a failed generator refuses today (A12). Falling back to the shared directory
  would silently bring the deadlock back.
- **Older jails.** A jail without the new contract tag keeps today's last-writer behavior. An attach
  to one prints one line: a running pi session in it may now read this entry's profile, and
  `yolo stop` then a fresh launch removes the hazard. No restart is forced.
- **Backends.** Podman and Apple Container exec every session and carry the id today. macos-user
  launches each session as its own sandbox and does not carry `YOLO_SESSION_ID`; there the launch
  mints one per invocation and hands it in the session env. UNMEASURED: needs a Mac.

### 4.7 The host

Ruled by [OQ-SP4](#11-decision-ledger): `yolo host` gives each launch the same session home, so
host children follow the launch's `-p` like a jail session's do.

- The **shared directory** is the user's real one (`~/.pi/agent` on the host). Nothing in it is
  written: the session home only links to it.
- The **session home** is a throwaway directory per launch, holding the launch's render of the
  pack's surfaces in the declared directory, and links to everything else, by the rules of
  [§4.4](#44-what-a-session-home-holds).
- The declared variable is exported into **that launch's** environment only.
- The launch removes its home when it ends; a home whose launch died is reaped by a later launch.
- The user's own value of the variable wins, as in a jail ([SP-D5](#SP-D5)).

## 5. What changes in the pi pack

- A session-home declaration: directory `.pi/agent`, variable `PI_CODING_AGENT_DIR`, shared names
  `auth.json`, `models-store.json`, `mcp-auth.json`, `sessions`, `bin`, `tools`, and the `agents`
  rule above.
- `yolo-model-lists.js` and `yolo-openai-auth.js` resolve their list files under
  `PI_CODING_AGENT_DIR`, as `yolo-provider-policy.js` and `yolo-durable-worktrees.js` already do.
- The prelaunch refresh's `due_on_change` and `only_if` files resolve in the session home.
- Nothing names a fork, and no flag is handed: stock pi and stock pi-subagents read everything
  through `getAgentDir()`.

## 6. Alternatives considered

| Alternative | Verdict |
| :--- | :--- |
| Hand the selection per process (`launch_selection` argv, the list files by variable) in the jail launcher | **Rejected as insufficient.** It moves the parent's model but not the `subagents` block, which stock pi-subagents reads only from `settings.json` ([§2.2](#22-the-readers-and-which-read-per-child)). |
| `launch_computed` per session in the jail | **Rejected.** Needs a flag stock pi-subagents does not register; MM-D42 rules out depending on the fork that does. |
| Strip the `subagents` block and selection from the shared file in jails | **Rejected** ([OQ-SP1](#11-decision-ledger)): children would follow the parent's model, but a profile's `subagents` declarations would stop working in every jail. |
| A settings overlay file pi reads by variable | **Impossible:** stock pi has no `--settings` flag or settings variable; the agent dir is the only lever. |
| Per-session mount namespaces binding files over `~/.pi/agent` | **Rejected**: `podman exec` cannot give an exec its own mount namespace, and a nested unshare needs privileges a jail does not have. |
| Refuse an attach whose profile differs from a running session's | **Rejected**: it forbids the maintainer's actual use, and core cannot tell whether the attach runs pi at all (P5). |
| Detect the attach's command and skip unrelated agents | **Rejected** by P5: `yolo -- bash` then `pi` defeats it. |

## 7. What this does not cover

- **Agents other than pi.** Their packs declare nothing yet, so their profile-dependent files stay
  last-writer. Codex (`CODEX_HOME`), Claude (`CLAUDE_CONFIG_DIR`) and opencode each need their own
  check before declaring one. UNMEASURED.
- **`~/.config/mcp/mcp.json`**, the cross-tool file pi-subagents reads, lies outside `~/.pi/agent`;
  it stays last-writer. It varies by profile only through an MCP server's `requires_env`.
- **An already-serving wire-bridge route** stays its first entry's
  ([wire-bridge](../reference/wire-bridge.md)); this design moves config, not traffic.
- **Credential confidentiality between sessions.** A snapshot is readable by the jail's uid, as H2 is
  ([OQ-ES5](credential-sources-separation.md#10-decision-ledger)).

## 8. Risks

| Risk | Mitigation |
| :--- | :--- |
| Pi writes a shared top-level file by rename, replacing the link | Strays are kept and named ([§4.4](#44-what-a-session-home-holds)); the plan measures pi's writers for every shared name first |
| A future pi creates a new top-level state file | Same: a stray, named at the next pass, then declared |
| Capture reads a half-written file | Unparseable files are skipped, never captured as deletions |
| Session homes pile up in a long-lived jail | A handful of small files each; reaped at the next fresh boot |
| A nested interactive shell resets the session's tables from H1 | `.bashrc` sources the snapshot ([§4.3](#43-an-entry-in-order) step 6) |

## 9. Sequencing

1. Entry snapshots, environment half: the launcher writes them, the pass hydrates from them,
   launchers and `.bashrc` source them. Useful alone: a `pi` started later in session A gets A's env.
   Shared with [attach-daemon-activation](attach-daemon-activation.md); built here if that is not
   built first.
2. The session-home declaration and the pass that renders and links, with the pi pack's
   declaration and extension paths. This closes the reported deadlock.
3. Capture from session homes ([OQ-SP2](#11-decision-ledger)).
4. macos-user's per-launch id; then other agents' declarations, one measured agent at a time.

Correct the two stale claims in [§2.1](#21-the-writers-file-by-file) when step 2 lands, since step 2
changes what a session pass renders.

## 10. Open questions

None. Every question is ruled in the [decision ledger](#11-decision-ledger).

## 11. Decision ledger

The maintainer's rulings, then implementation decisions (reversible, made while drafting). None is built.

| ID | Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| OQ-SP1 | **Each pi session gets its own config dir (`PI_CODING_AGENT_DIR`), its profile-dependent files rendered there and everything else linked to the shared copy.** *"Yes, this sounds like the right solution."* It moves the dir stock pi already reads and rewrites no shared settings, so the roadmap's "no settings overlay" line does not apply | 2026-10-18 | [§4](#4-the-design-session-homes-over-entry-snapshots) | — |
| OQ-SP2 | **A pi setting saved in one session (a `pi install`, a theme, a default model) is captured into the shared layer and reaches sessions entered later**; running sessions keep their own copy. *"Yes, it would be nice if this is possible."* | 2026-10-18 | [§4.5](#45-pis-own-writes) | — |
| OQ-SP3 | **An attach with no `-p` uses the configured `profile`, as today.** *"We're not changing anything about how profiles are selected."* | 2026-10-18 | [§4.3](#43-an-entry-in-order) | — |
| OQ-SP4 | **`yolo host -p` gives pi a per-launch session home too**: links to the user's real `~/.pi/agent` shared state, the profile-dependent files rendered in a throwaway dir, `PI_CODING_AGENT_DIR` exported to that launch only, the user's own files untouched; removed when the launch ends, reaped later if it dies. *"If we can use these throwaway things on the host to make the host also act similarly, that would be great ... So then, yes."* *"The principle should always be parity. It doesn't matter if I do or do not use it. Somebody else may or may not use it, and we need parity here."* | 2026-10-18 | [§4.7](#47-the-host) | — |
| <a id="SP-D1"></a>SP-D1 | The key is `YOLO_SESSION_ID`, inherited by every descendant; no new id | 2026-10-18 | [§4.3](#43-an-entry-in-order) | — |
| <a id="SP-D2"></a>SP-D2 | A session pass renders surfaces inside a declared directory only into its session home; the shared copies are the jail boot's alone | 2026-10-18 | [§4.2](#42-who-writes-what) | — |
| <a id="SP-D3"></a>SP-D3 | Link every entry present plus every declared shared name; never copy; strays are kept and named | 2026-10-18 | [§4.4](#44-what-a-session-home-holds) | — |
| <a id="SP-D4"></a>SP-D4 | The relocation variable is exported into the session's environment by the pass, not by a launcher, so children and typed commands inherit it | 2026-10-18 | [§4.3](#43-an-entry-in-order) | — |
| <a id="SP-D5"></a>SP-D5 | A user's own value of the variable wins; one line says the session has no session home | 2026-10-18 | [§4.6](#46-lifetime-and-failure) | — |
| <a id="SP-D6"></a>SP-D6 | A session home that cannot be made refuses the session; no fallback to the shared dir | 2026-10-18 | [§4.6](#46-lifetime-and-failure) | — |
| <a id="SP-D7"></a>SP-D7 | Retained until the jail is known gone, in the pack's own state beside the declared dir; captured and deleted by the next fresh boot | 2026-10-18 | [§4.6](#46-lifetime-and-failure) | — |
| <a id="SP-D8"></a>SP-D8 | An older jail keeps last-writer behavior; the attach prints one line naming `yolo stop` and a fresh launch | 2026-10-18 | [§4.6](#46-lifetime-and-failure) | — |
| <a id="SP-D9"></a>SP-D9 | yolo's pi extensions resolve their files under `PI_CODING_AGENT_DIR` | 2026-10-18 | [§5](#5-what-changes-in-the-pi-pack) | — |

## 12. What done looks like

1. A jail launched with `-p A` runs pi with subagents. `yolo -p B -- pi` attaches. A's new subagents
   run on A's default; B's on B's.
2. `yolo -- agy` with no `-p` and B the default: A's subagents still run on A.
3. In session A, quitting pi and starting `pi` again starts on A, with A's provider policy.
4. A `/login` in session B works in session A's next request without a restart.
5. `pi -c` in any session lists every session's history.
6. A `pi install npm:foo` in A is in `packages` for a session entered afterwards ([OQ-SP2](#11-decision-ledger)).
7. An attach to a jail launched before this prints the one-line warning and otherwise behaves as
   today.
