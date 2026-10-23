---
title: "Git through the host's login, one ref at a time: the second brokered source"
date: 2026-10-23
status: in-review
stage: DESIGN
next: "Rule OQ-GB1, how git crosses the boundary; the broker-core factoring in §3 waits on nothing and can start now"
tags: [design, credentials, git, github, broker, approvals, loopholes, audit]
summary: "The second source behind the boundary broker, after gh: git fetch and push from a jail that holds no credential. Reads are standing; a push to a branch this workspace created through the broker is standing too; a push to any other ref asks a human, who grants that one ref for a window. Step 1 of the maintainer's 2026-10-23 path: it first factors the source-neutral broker core out of the github-broker, then adds git as a second source on it. Open: whether git crosses as a proxied CLI or as a brokered transport, and how long a created-branch grant lives."
vantage:
  status-chip: true
---

# Git through the host's login, one ref at a time: the second brokered source

**Status:** 2026-10-23. Nothing built. Split out of
[`boundary-broker.md`](boundary-broker.md) the day the maintainer laid out his five-step path
([§1.5 there](boundary-broker.md#15-the-maintainers-path-2026-10-23)), whose step 1 this doc
designs. Evidence verified against `f9fcd599a`.

> **In short.** A jail's git should fetch and push the way a person's does, through the host's
> own login, without the credential ever entering the jail. Fetching is free; pushing a branch
> the agent made is free; pushing anything else is one ref, shown to a human, granted for a
> window.

**Why it matters.** A jail has no git credential, so today `git fetch` and `git push` against a
private GitHub repository fail in every jail, and agents waste turns trying to work around it.
A colleague who built his own approval flow found the same thing and wrote a git broker of his
own; the maintainer called extending the broker to git *"definitely worth it"* (2026-10-23).

**The shape.** The **broker core** *(coined here)*: the source-neutral half of today's
github-broker (store, grants, audit, sets, scope, approval front-ends), factored out so a source
is a classifier plus an executor. **git** is the second source on it, with three sets: `read`,
`push-own`, and per-ref `push` ([§5](#5-the-git-sources-sets)).

**Cost.** A refactor of `internal/ghbroker` with no behavior change, a second service of the
github pack's broker, one line of jail git configuration, and a host-side record of the branches
a workspace created.

**Start at [§4](#4-how-git-crosses)**: the one architecture choice. The rest follows from it.

**Needs your ruling:** [OQ-GB1](#OQ-GB1), [OQ-GB2](#OQ-GB2).

**Reads with:** [`boundary-broker.md`](boundary-broker.md) (the design this extends: the request
store, grants, the notification, `yolo approve` and the audit log are all its), and
[`workspace-widening.md`](workspace-widening.md) (the scope a git call is checked against is the
same approved scope).

---

## 1. What the maintainer ruled, 2026-10-23

From a Slack thread with a colleague, which the maintainer pasted as the brief for this work.
The colleague's build proxies git: his host runs a fetch on request, a push is a separate request
he approves, and he grants push per branch, a branch the agent created being pushable without
asking. The maintainer's words on it:

> *"extending that github broker to support git over ssh is definitely worth it. how does that
> work though without allowing write access as well?"*

> *"interesting, so proxying the git CLI. I guess that's basically what I do with gh. maybe even
> worth making this a generic mechanism then. I think this is all the right approach in general"*

> *"add support for whatever loopholes you need/want with rw and ro support, maybe factoring out
> the CLI wrapping/proxy stuff"* (step 1 of his five)

| He says | Rules | Lands |
| :--- | :--- | :--- |
| *"support git"*, *"rw and ro support"* | git is a brokered source with a read set and a write set | [§5](#5-the-git-sources-sets) |
| *"factoring out the CLI wrapping/proxy stuff"*, *"a generic mechanism"* | The source-neutral half of the github-broker is factored out first | [§3](#3-the-broker-core-what-step-1-factors-out) |
| *"all the right approach"*, of per-branch grants with created branches free | Push is granted per ref, and a branch the workspace created is pushable without asking | [§5](#5-the-git-sources-sets) |
| *"proxying the git CLI"* | Read as the mechanism he had in mind, not a ruling on it: [OQ-GB1](#OQ-GB1) asks whether it holds | [§4](#4-how-git-crosses) |

The standing rule is unchanged: the credential never crosses
([BB-P1](boundary-broker.md#BB-P1)).

## 2. What exists today

Verified against `f9fcd599a` on 2026-10-23.

| Piece | Where | State |
| :--- | :--- | :--- |
| A jail git credential | — | **none**: the jail has no SSH key and no token unless the user delivers one |
| The jail's global git config | composed on the host and mounted read-only on the container backends, written in place on macos-user (`internal/entrypoint/identity.go`) | **shipped**; a place yolo already owns where a URL rewrite can go |
| GitHub remotes reduced to `owner/repo` | `brokerscope.RepoFromRemoteURL`, for the https, `ssh://` and `git@host:` forms | **shipped**, used by the github-broker's scope |
| The approved repository scope | the config-change gate's approval record plus the per-launch scope file, re-read per request, widened live by `yolo config apply` ([`live-config-apply.md`](live-config-apply.md)) | **shipped** |
| A request store with grants | `internal/brokerstore`, one directory per source (`paths.BrokerSourceDir`) | **built**; production files no request yet ([§7 there](boundary-broker.md#7-grants-and-the-request-store)) |
| An audit log with a `service` field | `internal/brokeraudit` | **shipped**; only `github` writes it |
| Jail-side interception of a command name | the `intercept` contribution kind (`packdecl.KindIntercept`) | **shipped**, used for `gh` |
| Anything for git | — | **missing**: no git source, forwarder or transport in `internal/` or `packs/` |

The github-broker is one package, `internal/ghbroker`, holding both the source-neutral machinery
and everything gh-specific. That is what step 1 factors.

## 3. The broker core: what step 1 factors out

**The factoring is a refactor with no behavior change**, and it comes first because both options
of [OQ-GB1](#OQ-GB1) sit on it.

| Broker core (source-neutral) | Per source |
| :--- | :--- |
| The source registry: a name, a store directory, its default sets | The jail doorway: an intercept forwarder (gh), or a transport ([§4](#4-how-git-crosses)) |
| The classifier contract: an action goes in; out come refused, out of scope, or a set, plus the repositories it touches, a canonical rendering for the human, and a digest for Allow once | The classifier itself |
| Set resolution, grants, use counting, Allow once, revocation, broker-lifetime binding | The executor |
| The repository-scope check against the approved scope | Which scope it reads; git reads GitHub's, unchanged |
| Request filing, duplicate joining, the bounded wait, status and wait | The exit codes or protocol errors the jail sees |
| Audit events, with `service` set to the source | Source-specific audit fields |
| The approval front-ends: notification text from the classifier's rendering, `yolo approve` across every source, the CLI surface ([§6.6 there](boundary-broker.md#66-one-store-several-front-ends-and-a-stable-surface-for-the-ones-yolo-does-not-ship)), and a `--service` filter on `yolo audit`, which has none today | — |

**Done looks like:** the github-broker's tests pass unchanged on the core plus a gh source, and a
second, fake source in a test files, grants and audits through the same core.

## 4. How git crosses

Git differs from gh in one way that decides the design: **a push carries objects that exist
only in the jail.** Whatever runs the push on the host has to get them, and that is where the
two shapes part.

**A — Proxy the git CLI** (the colleague's build, and the maintainer's framing). The `intercept`
kind puts a forwarder at `git`. It passes every local subcommand to the real git and sends
`fetch`, `pull`, `push` and `ls-remote` to the broker as argv. The host runs them with the host's
credentials in a **broker-owned bare mirror** *(coined here)* of the repository, never in the
workspace: git in a jail-writable checkout runs code through its config
([§4.1 there](boundary-broker.md#41-how-the-broker-runs-gh)). Objects cross between mirror and
workspace in both directions. The classifier parses git's push grammar (refspecs, `--force`,
`--delete`, `--tags`, push options).

**B — Broker git's transport** (the proxy tier that
[§13 there](boundary-broker.md#13-what-this-does-not-cover-and-the-other-two-tiers) called
B1b). The jail's git configuration rewrites every GitHub URL, https and SSH alike, to the
broker's endpoint, so the jail's own git does all the local work and talks git's smart HTTP
protocol to the broker. The broker checks each request and adds the host's credential on the
way out to GitHub:

1. **A fetch** (`git-upload-pack`) in scope is a `read`: forwarded, response streamed back.
2. **A push** (`git-receive-pack`) opens with one command line per ref: old id, new id, ref
   name. The broker reads those lines before forwarding anything and assigns each ref to a set
   ([§5](#5-the-git-sources-sets)). If every ref is held, the request goes through. If not,
   nothing is forwarded; the broker files a request and answers in git's own report format, so
   the agent sees `! [remote rejected] main -> main (needs approval: r-3f9a0c21 …)`.
3. **The credential** comes from the host's `gh` login, the one the github-broker already runs,
   and is held in the broker's memory only.

| | A — proxy the CLI | B — broker the transport |
| :--- | :--- | :--- |
| What the jail runs | a forwarder in front of every `git` call, local ones included | the real git, unchanged; one config line rewrites GitHub URLs |
| Host-side state | a bare mirror per repository: disk the size of the repository | none beyond the store |
| Objects crossing | mirror and workspace, both ways, by a mechanism the build chooses | git's own protocol, as with any remote |
| What policy reads | an argv, through git's push grammar | the protocol's ref commands: exact, with no grammar to get wrong |
| Tools that call git themselves (IDEs, agents' own git) | all pass through the forwarder | unaffected |
| What the agent sees on a refusal | an exit code, as for gh (64, 75, 77) | git's own `remote rejected` line; `git push` exits 1 |
| An SSH-only host, no `gh` login | works, over the host's SSH | out: the outbound credential is the `gh` login's |

The second-last row is B's real cost: a script cannot tell "pending" from "denied" by exit code.
The message carries the request id and the `yolo` command, which is what an agent reads.

## 5. The git source's sets

| Set | Mode | Admits |
| :--- | :--- | :--- |
| `read` | standing | fetch, `ls-remote`, the fetch half of `pull`, on any repository in scope |
| `push-own` | standing | creating a branch under `refs/heads/`; then updating, force-updating or deleting a branch this workspace created through the broker ([§5.1](#51-the-created-branch-record)) |
| `push` | windowed, **per ref** | any other update, deletion or creation under `refs/heads/` or `refs/tags/` |

**Refused, under every set** ([BB-P8](boundary-broker.md#BB-P8)): a ref outside `refs/heads/`
and `refs/tags/` (`refs/pull/…`, `refs/notes/…`, and the like), a push to a repository out of
scope, and, under option A, `--mirror`, `--all` and `--prune`, which name no explicit refs.

**A `push` grant names one ref.** The request shows each ref the push would change, old id to
new id, with `force` marked where the new id does not descend from the old. The three buttons
keep [OQ-BB3](boundary-broker.md#15-decision-ledger)'s shape: **Allow once** (this exact
push), **Allow push to `<ref>` 15 min**, **Deny**. A push that changes three refs, one held and
two not, files one request naming the two; the human's grant covers what the request named.
This narrows [OQ-BB2](boundary-broker.md#15-decision-ledger)'s set-whole rule for git, on the
maintainer's endorsement above: a set is still the unit the source defines; the git grant adds
the ref as its own narrowing.

**Long-lived push**, such as an agent that always pushes to `main` of its own fork, is the
workspace config's job, not a grant's
([§5.8 there](boundary-broker.md#58-long-lived-access-and-temporary-access)); which sets a
workspace may hold that way is [OQ-BB16](boundary-broker.md#OQ-BB16).

### 5.1 The created-branch record

A ref update whose old id is all zeros creates the branch. When the broker forwards one and
GitHub reports it accepted, the broker records `(workspace, owner/repo, ref)` in the git source's
store. A later push to that ref is `push-own`. The record goes when the broker deletes the
branch, or when `yolo approve --revoke` names it.

- **Only a creation through the broker counts.** A branch the user pushed from the host, or one
  another workspace created, is not this workspace's.
- **A creation the push lost** (GitHub rejected it, or the connection dropped before the report)
  records nothing; the next push is a creation again if the branch does not exist.
- **Two jails of one workspace** share the record, since it is keyed by workspace.

How long the record lives across launches is [OQ-GB2](#OQ-GB2).

## 6. Failure paths

| What fails | What happens |
| :--- | :--- |
| The broker is not running | A: the forwarder exits 69, as gh's does. B: git reports the endpoint unreachable, and the briefing names `yolo check` |
| The repository is out of scope | Refused at once, naming the scope and `brokered.github.repos`, as for gh ([§5.6 there](boundary-broker.md#56-the-repository-scope)); audited, no request |
| The host has no `gh` login (B), or no credential for the remote (A) | Refused naming `gh auth status` on the host; the broker never starts a login |
| A push needs a ref the jail does not hold | A request is filed; the bounded wait and the answer path are the store's ([§3.2 there](boundary-broker.md#32-a-write-and-the-bounded-wait)) |
| GitHub rejects a forwarded push (a protected branch, a non-fast-forward) | GitHub's own report reaches the agent unchanged; the grant's use is spent, as for any call that ran |
| A fetch exceeds the transfer cap | Not capped: a fetch is as large as the repository. The broker streams and holds no copy |

## 7. Security

- **No credential in the jail, under either option.** Under B the jail holds the endpoint's
  transport token, which reaches only the broker, as every loophole's does
  ([`loophole-transport.md`](../reference/loophole-transport.md)).
- **A pushed branch runs the repository's push-triggered workflows**, from the branch's own
  workflow files, with whatever secrets those workflows receive. `push-own` does not inspect what
  a push contains. This is the same exposure as a pull request the agent's branch would open, and
  intent is out of scope by [the misguided-agent principle](../reference/misguided-agent-principle.md);
  the audit log records every push with its refs and ids.
- **The audit line** for a push carries each ref, its old and new id, and the set or grant that
  covered it, so *"what exactly did it push with my access"* is one `yolo audit --service git`
  away.

## 8. What this does not cover

- **Hosts other than GitHub.** The scope reader reduces only GitHub remotes; another host is
  another source on the same core.
- **Git LFS, submodules, `git clone` of a repository the workspace does not have.** Each needs
  its own design once someone asks.
- **A credential source other than the host's `gh` login** under option B.

## 9. The first slice

1. **The broker-core factoring** ([§3](#3-the-broker-core-what-step-1-factors-out)), with no
   behavior change. Waits on nothing.
2. **The git source's `read` set**: fetch, `pull` and `ls-remote` from the jail, in scope,
   through the host's login, audited. Every push answers "needs approval, which this version
   cannot ask for", as gh's writes do today.
3. **`push-own`**: creating a branch and pushing to it again, standing, so an agent can publish
   its own branch with no human and no notifier.

Steps 2 and 3 wait on [OQ-GB1](#OQ-GB1). Step 3 builds the created-branch record bound to the
launch until [OQ-GB2](#OQ-GB2) is ruled, since that is the narrowest answer and every other one
extends it. The per-ref `push` set needs the request path, which waits on the notifier and the
CLI (steps 2 and 3 of [the path](boundary-broker.md#11-the-build-in-the-maintainers-five-steps)).

## 10. What done looks like

1. In a jail with the pack enabled and no credential, `git fetch origin` against a private
   in-scope repository brings its refs in, and `yolo audit --service git` shows one `read` line.
2. `git push origin HEAD:refs/heads/agent/x`, a new branch, succeeds with no human; a second push
   and a force-push to it succeed too, each audited as `push-own`.
3. `git push origin main` is refused, naming the request path and that nothing ran; GitHub
   receives nothing.
4. A fetch from a repository outside the scope is refused naming `brokered.github.repos`.
5. `git status`, `git commit` and every other local command behave exactly as without the
   broker.

## 11. Open questions

1. 💬 <a id="OQ-GB1"></a>**[OQ-GB1](#OQ-GB1): Does git cross as a proxied CLI, or through a
    brokered transport?** The maintainer framed it as *"proxying the git CLI"*. This decides
    what the jail runs, whether the host keeps a mirror, and whether a refusal is an exit code.
    The comparison in full: [§4](#4-how-git-crosses).

    - **A — Proxy the CLI.** A forwarder before every `git`, a host mirror per repository;
      gh's exit codes.
    - **B — Broker the transport.** The real git with one URL rewrite; the broker reads each
      push's ref commands and adds the credential; git's own error lines, and no SSH-only path.

    <!-- vantage: oq id=OQ-GB1 leaning="B: policy reads the protocol's exact ref commands instead of re-parsing git's push grammar, nothing intercepts the git every tool calls, and the host keeps no mirror; the lost exit codes are carried by the refusal's own text." -->

    _Leaning:_ B: policy reads the protocol's exact ref commands instead of re-parsing git's push grammar, nothing intercepts the git every tool calls, and the host keeps no mirror; the lost exit codes are carried by the refusal's own text.

    **Answer:**
    > _(empty — fill in when decided)_

2. 💬 <a id="OQ-GB2"></a>**[OQ-GB2](#OQ-GB2): How long may a workspace push to a branch it
    created?** An agent creates `agent/x` today; the jail restarts tomorrow. This decides
    whether its next push asks.

    - **A — For the launch.** Narrowest; a restart makes every push to its own branch ask once.
    - **B — Until the branch is deleted or the grant revoked**, across launches of that
      workspace. No re-asking; a branch abandoned for months stays pushable.
    - **C — B, with an age cap** (30 days since the last push through the broker).

    <!-- vantage: oq id=OQ-GB2 leaning="B: the branch is the workspace's own work, a restart is not a reason to ask, and yolo approve lists and revokes the records." -->

    _Leaning:_ B: the branch is the workspace's own work, a restart is not a reason to ask, and `yolo approve` lists and revokes the records.

    **Answer:**
    > _(empty — fill in when decided)_

## 12. Decision Ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| <a id="GB-D1"></a>GB-D1 | **Maintainer direction:** git is a brokered source with read and write support, after factoring the source-neutral broker machinery out of the github-broker; push is granted per branch and a branch the agent creates is pushable without asking. *"I think this is all the right approach in general"* | 2026-10-23 | [§1](#1-what-the-maintainer-ruled-2026-10-23) | — |
| <a id="GB-D2"></a>GB-D2 | *Implementation decision.* The git source is a second service of the github pack's broker: the same enablement, scope file and login, and its own store directory under the source name `git` | 2026-10-23 | [§3](#3-the-broker-core-what-step-1-factors-out) | — |
| <a id="GB-D3"></a>GB-D3 | *Implementation decision.* Under either option the host never runs git with the workspace as its repository; a jail-writable checkout's config can run code | 2026-10-23 | [§4](#4-how-git-crosses) | — |
| <a id="GB-D4"></a>GB-D4 | *Implementation decision.* A push changing several refs files one request naming the refs not held; a grant covers what its request named | 2026-10-23 | [§5](#5-the-git-sources-sets) | — |
