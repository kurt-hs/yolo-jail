---
status: current
stage: CURRENT
verified: 2026-10-16
verified_commit: 8e65f3444
covers:
  - internal/cli/run/contracttags.go
  - internal/cli/run/attachskew.go
  - internal/cli/run/attachskewbriefing.go
  - internal/entrypoint/contracttags.go
  - internal/jailcontent/attachskewsection.go
  - packs/releasedecode_test.go
tags: [attach, skew, versioning, contracts, packs, briefing]
---

# Attach skew — the contract gate between a newer host and an older jail

**Status:** Verified 2026-10-16 against `8e65f3444`. Graduated from the attach-skew and
contract-guardrails design (2026-09-26), whose history git keeps. MEASURED on a real podman,
against a stand-in older jail (a container whose frozen environment an older launch would have
left): without a terminal the attach refuses and counts the live sessions
(`TestAttachRefusesAJailThatCannotReceiveTheSelection`); at a real pty, answering `y` stops the
jail and the same launch starts a fresh one and runs the command
(`TestAttachRestartsAnOlderJailAtATerminal`); an acknowledged attach writes the skew section into
the briefing (`TestAnAcknowledgedAttachNamesTheSkewInTheBriefing`). MEASURED 2026-10-01 against
a jail v0.11.0 itself launched ([the run](#an-attach-to-a-jail-v0110-launched)). UNMEASURED: an
agent session reading the acknowledged-attach section in such a jail, and the restart prompt at a
real terminal against a jail a real older release launched; both need an agent session or a
terminal, which no automated test may start.

A running jail keeps the binaries it launched with. An **attach** — re-entering a running
container with `podman exec` (`attachExisting`) rather than launching a fresh one — may be made
by a newer host `yolo`, and an attach is an entry event, not a bare reconnect: it writes the
per-entry provider/profile channel and re-runs the jail's boot. The **contract gate** asks,
before the attach writes anything, whether the running jail's binaries can receive what this
entry would hand them. When they cannot, the attach never proceeds on its own: it offers a
restart at a terminal, refuses elsewhere, and goes ahead only under one explicit
acknowledgment, which it then discloses in the agent's briefing.

| Component | Lives in |
| :--- | :--- |
| Tag vocabulary, the launch's frozen set, both halves of the comparison, the disposition and the restart | `internal/cli/run` (`contracttags.go`: `launchContractTags`, `jailContractTags`, `attachContractFor`, `restartJailForAttach`, `jailSessionCount`) |
| The cross-boundary variable name | `internal/entrypoint` (`ContractTagsEnv`) |
| The informational version-skew line and the restart remedy | `internal/cli/run` (`attachskew.go`: `warnIfJailIsOlderThanTheLauncher`, `stopRemedy`) |
| The acknowledged-attach briefing section | `internal/cli/run` (`attachskewbriefing.go`), rendered by `internal/jailcontent` (`AttachSkew`, `attachSkewSection`) |
| The release-decode check | `packs/releasedecode_test.go` (`TestShippedPacksDecodeUnderTheLastRelease`, `knownReleaseBreaks`) |

**Reads with:** [`pack-system.md`, concurrent launches](pack-system.md#concurrent-launches-of-one-workspace)
and [`OQ-PK2`](pack-system.md#oq-pk2) (the per-launch pack trees that close pack-contract skew),
[`providers.md`'s credential gate](providers.md#the-credential-gate) (the per-agent env files a
tag guards), [`image-staging-vs-baking.md`](image-staging-vs-baking.md) (the mounted prefix and
its generations).

---

## Terms

- **Attach.** Re-entering a container an earlier invocation started, through `attachExisting`,
  instead of launching a fresh one.
- **Prefix generation.** One immutable flake-bundle directory whose linux binaries a launch
  bind-mounts at `/opt/yolo-jail/bin`. A running jail's generation cannot change.
- **Attach skew.** The host `yolo` making the attach is a different build from the binaries
  inside the running jail.
- **Contract skew** *(coined by the design this graduated from)*. Attach skew where what the
  attach delivers needs something the jail's binaries do not implement.
- **Contract tag** *(coined by its build)*. The name of one thing a later attach may need a
  running jail to do, frozen into the container at launch. "Tag", not "capability", because the
  pack system's `required_capabilities` already owns that word.
- **The acknowledgment.** `YOLO_ALLOW_ATTACH_SKEW` set to any non-empty value: the one way an
  attach proceeds into a jail it knows cannot take this entry.

## Principles and invariants

- **A running jail's binaries are immutable.** A bind mount pins an inode, so restaging or
  mutating a live generation would pull pid1 out from under the jail. Nothing re-binds or
  mutates `/opt/yolo-jail/bin` in a live container; the only way to new binaries is a restart.
- **Never silent** ([`OQ-SK1`](#OQ-SK1)). An attach that cannot be made compatible never
  proceeds on its own, and an acknowledged one is loud on stderr and in the briefing.
- **No other override implies the acknowledgment.** The gate reads `YOLO_ALLOW_ATTACH_SKEW` and
  nothing else; `TestNoOtherHatchAcknowledgesAttachSkew` sets every other `YOLO_*` variable the
  tree spells, and every override-style `Options` field such as `AcceptConfigChanges`.
- **Cosmetic drift never refuses.** A version difference with no missing contract attaches with
  only the dim informational line ([below](#the-version-line)).
- **Cannot prove, do not refuse.** An inspect that returns no environment proves nothing, and
  the jail is treated as current.
- **Named tags, no version matrix** ([`OQ-SK2`](#OQ-SK2)). A tag is added by the change that
  adds the contract and read by the attach that depends on it, with no central counter for two
  branches to collide on.

## How the gate works

### Contract tags

Every container launch freezes `YOLO_CONTRACT_TAGS` — the comma-separated tags this build's jail
implements — into the container's environment. The launch writes its own set because the jail
it starts runs binaries built from the same source, which the source-skew gate enforces. Nothing
in the jail reads the variable; it is for the next host `yolo` that attaches. The current tags
and what each means are in [Current values](#current-values).

A present `YOLO_CONTRACT_TAGS` is authoritative, even empty. A jail launched before the tags
existed gets each of the two oldest tags inferred from the marker its contract left
([`SK-D2`](#SK-D2)): `agent-env-files` from the credential gate's `YOLO_AGENT_ENV_FILES=1`, and
`entry-channel` from the absence of a frozen `YOLO_PROVIDERS`. Newer tags have no inference: a
jail that predates the tag list predates them too.

Adding a contract is up to three edits, all in `contracttags.go`: the tag's constant, its entry
in the launch's set, and — only when some attach depends on it — a need in `attachContractFor`.
A contract no attach asks about still gets a tag, because the tag is what a later build reads.

### What an attach needs

`attachContractFor` computes the tags this attach depends on from the channel it would deliver
([`SK-D3`](#SK-D3)):

- `entry-channel` whenever it delivers a profile selection;
- `profile-sets` whenever a selection names a set of more than one profile for an agent (a set
  of one crosses as the plain string it always did);
- `agent-env-files` whenever the credential gate scoped any value to one agent.

A **plain re-entry** into a jail launched before per-entry delivery — with the selection it
froze at launch, or with none — needs nothing and is written nothing, because its launch-time
delivery stands in for this entry's. It must stay silent: a first cut that refused there broke
every plain attach of a config carrying a persistent selection.

`session-hangup` is a tag no attach *needs*: an attach to a jail without it runs as before, and
its signal arm says it cannot hang the session up ([`jail-home.md`](jail-home.md)).

### The disposition

The gate runs in `attachExisting` after the one inspect and the launch line, before anything is
written, with the workspace launch lock held ([`SK-D5`](#SK-D5)). The same three arms take every
difference the attach knows about: a missing tag, and the pack-tree skews of
[`OQ-PK2`](pack-system.md#oq-pk2) (a running jail's tree this build cannot read, or a selection
that fails over the packs the jail has):

| Context | What happens |
| :--- | :--- |
| `YOLO_ALLOW_ATTACH_SKEW` set | The attach proceeds, prints on stderr what differs and the name of each withheld item, and **writes no part of this entry's channel** ([`SK-D9`](#SK-D9)), so the jail keeps what its last entry gave it. It then writes the [briefing section](#the-acknowledged-attach-in-the-briefing) |
| A terminal on stdin and stdout | `Restart jail now? [Y/n]`, naming the sessions the stop ends. Enter or yes stops the jail and the same launch continues as a fresh one; no, or end of input, refuses ([`SK-D8`](#SK-D8)) |
| Anything else | A refusal naming the restart — `yolo stop` from the workspace, then a launch — and the acknowledgment |

The prompt and its account go to stdout; refusals and the acknowledgment's disclosure go to
stderr ([`SK-D10`](#SK-D10)). Writing only the shared half of the channel under the
acknowledgment would strip every scoped credential while naming agents as its recipients, which
is why the degraded attach writes none of it.

> [!WARNING]
> Do not add an in-jail filter for contracts an old jail does not know. An old jail's tolerant
> decoder drops unknown fields, so it cannot filter what it cannot see; any degradation happens
> on the host ([`OQ-SK3`](#OQ-SK3)).

### The restart

A restart keeps the launch lock, which is what makes the stopped jail's teardown leave its
host-services directory to the relaunch. It runs the teardown's bounded stop, waits a bounded
time for the container's `--rm` removal, falls back to the fresh path's stale removal for a
stopped leftover, and refuses with `did not stop` if the container is still running rather than
create a second one beside it ([`SK-D6`](#SK-D6)). It then waits for the stopped jail's keeper to
finish its teardown before the fresh launch spawns its own, so one keeper exists per container
name at every instant ([`jail-lifetime-last-session-wins.md`](../design/jail-lifetime-last-session-wins.md#JL-D26)).
The fresh launch continues at each of `runContainer`'s three attach sites: the first look, the
raced re-check, and the stale removal's wait.

The session count the prompt names is one per live exec, plus one when the jail's main process
is its first session rather than a hold ([`SK-D7`](#SK-D7)). A count the runtime does not answer
— including Apple Container's, never measured — reads as "cannot tell", and the prompt then names
`every session in it` with no number.

On Apple Container the attach reads the jail's environment from `container inspect`'s JSON, not
podman's template ([`SK-D14`](#SK-D14)). The remedy named is `yolo stop` on every runtime.

### The version line

Separately from the gate, an attach whose jail records a different yolo version than the
launcher prints one dim line saying so and naming the restart. It is informational and never
refuses, and it is silent unless both versions are known and differ. Before the gate, this line
was the only skew signal, and a full-screen agent erased it before anyone read it.

### The acknowledged attach in the briefing

An attach that proceeds under the acknowledgment hands the briefing refresh it already runs
(`refreshJailBriefings`, host-side, into the running jail's staging) an account of what it
acknowledged, and that refresh writes a section near the top of every briefing the running
jail's packs declare, beside the provisioning-failed one ([`SK-D15`](#SK-D15),
[`SK-D16`](#SK-D16)). It names:

- the version the jail was launched with, and the attaching yolo's;
- each contract tag the jail lacks, with what a jail without it cannot do;
- every other difference the acknowledgment covered: a pack set the selection cannot be composed
  over, or a profile-served daemon the jail's launch never started;
- what this entry withheld, by name, never by value;
- the restart that ends the difference, for the agent to pass to the user, since an agent cannot
  run a host command.

The account crosses through `Options.attachSkewNotice`, set just before the refresh and cleared
after it, so no other briefing carries it. Every other entry's refresh writes no section, so it
lasts until the next entry into the jail. At most one per attach: the first acknowledgment
withholds the whole channel and nothing after it asks again.

The acknowledgment's last stderr line says that the briefing carries the account, or why none
does ([`SK-D17`](#SK-D17)): the jail's packs could not be read (nothing is refreshed then), they
declare no briefing destination, or the jail is on Apple Container, where a session reads the
briefing its launch copied into the jail's home and a re-entry's refresh reaches only the staging.

> [!NOTE]
> The footer is not a skew channel: an old jail's footer runs its own frozen binary and cannot
> learn a new segment. The host-written briefing is the one channel an old jail's session shows
> ([`OQ-SK4`](#OQ-SK4)).

## Pack-contract skew, and the release-decode check

Packs are the other contract a newer host could hand an older jail: a manifest field, hook or
kind the old reader does not know. An attach used to re-stage the host's packs and re-run the
jail's boot, and v0.10.0's in-jail loader treats any decode problem as fatal, so a jail that
release launched would have refused to boot on attach. That class is **closed** by
[`OQ-PK2`](pack-system.md#oq-pk2)'s per-launch pack trees: an attach writes into no pack tree,
so the boot it re-runs reads the tree the jail booted with, and a jail launched before per-launch
trees binds a shared tree nothing writes any more. The attach composes its channel over the
jail's own tree. No contract tag guards this, because an attach that writes no pack tree asks
nothing of an older jail's binaries.

`TestShippedPacksDecodeUnderTheLastRelease` still checks what the next release ships
([`SK-D11`](#SK-D11), [`SK-D13`](#SK-D13)). It builds the last release tag's in-jail reader from
`git archive` and decodes this tree's embedded packs with it through the three decode passes that
release's boot runs: manifest load under the tolerant decoder, each pack's surfaces under both
postures, and the overlay collection over the whole set. Every known break is in an allowlist
(`knownReleaseBreaks`), keyed by release, with the guard that keeps it from reaching a jail and an
optional `pinnedBy` test that must exist. An unlisted break fails the short suite, and so does a
listed one that no longer occurs. A break `encoding/json` refuses outright carries a `repair` that
puts the field back in the old shape, so the probe keeps decoding past it.

> [!WARNING]
> The check finds the module root from the package directory `go test` runs it in, never from
> `git rev-parse --show-toplevel`: inside a git hook, git exports `GIT_DIR` without a work tree
> and `--show-toplevel` answers the wrong directory ([`SK-D12`](#SK-D12)).

### An attach to a jail v0.11.0 launched

MEASURED 2026-10-01 in nested podman jails (`--net=host` and `--userns=host`, which do not touch
what this reads). The older jail was v0.11.0's own — binaries built from `git archive v0.11.0`,
launched by that `yolo` with its own flake — and every attach was a later tree's build with no
terminal:

| Attach | Result |
| :--- | :--- |
| Plain | Proceeded with the version line and the [`OQ-PK2`](pack-system.md#oq-pk2) notice; the jail kept reading its own packs |
| A profile v0.11.0's own packs carry | Delivered, composed over the jail's own tree |
| A profile only the newer packs carry | Refused, naming `yolo stop`, the running-session count and the acknowledgment |
| The same, acknowledged | `This entry delivers nothing`, and the jail's claude briefing opened with the skew section |

## What this does not do

- No live mutation or re-binding of a running jail's binaries.
- No re-baking yolo into the image to avoid skew; the mounted prefix stays.
- No refusal on cosmetic version drift.
- No pack-level `requires_capabilities` declaration. A pack contract declared twice is a second
  thing to keep true, and per-launch pack trees mean an attach hands an old jail no new manifest.
- No suppression wrappers (`… || true`) around pack commands as a skew defense: they hide real
  breakage and do not reach entrypoint flags or daemon protocols.

## Why it's this way

| ID | Ruling | Why it holds |
| :--- | :--- | :--- |
| <a id="OQ-SK1"></a>[OQ-SK1](#OQ-SK1) | Never silent: in a terminal, `Restart jail now? [Y/n]` (declining refuses); elsewhere a refusal naming the restart; `YOLO_ALLOW_ATTACH_SKEW=1` the one acknowledgment, and no other override implies it. Ruled 2026-09-26 | The maintainer: *"not crazy to restart jails after an app upgrade"*, and an override *"shouldn't just silently ride along, even if there's another similar override flag"* |
| <a id="OQ-SK2"></a>[OQ-SK2](#OQ-SK2) | Named contract tags, not a monotonic epoch; the credential gate's `YOLO_AGENT_ENV_FILES=1` is folded in as `agent-env-files`. Ruled 2026-09-26 | Tags evolve independently across branches and packs with no central counter |
| <a id="OQ-SK3"></a>[OQ-SK3](#OQ-SK3) | Degrade only under the acknowledgment, and only on the host. Ruled 2026-09-26 | An old jail's tolerant decoder cannot filter what it does not know; proceeding into a known difference without it is the "dangerous" ride-along the ruling forbids |
| <a id="OQ-SK4"></a>[OQ-SK4](#OQ-SK4) | The briefing is the in-session channel for an acknowledged attach; no footer segment. Implementation decision, reversible (2026-09-30) | The host-written briefing is the one channel an old jail's session shows |
| <a id="SK-D1"></a>SK-D1 | Tags cross as `YOLO_CONTRACT_TAGS`; vocabulary and both halves of the comparison live in one file; the launch writes its own set | One writer, one reader; the source-skew gate makes the launch's set the jail's |
| <a id="SK-D2"></a>SK-D2 | A present variable is authoritative even empty; an absent one infers the two oldest tags from their legacy markers; new launches stop writing `YOLO_AGENT_ENV_FILES` | One spelling per tag. Cost: an older host attaching to a newer jail reads it as pre-gate and refuses a scoped delivery, the safe direction |
| <a id="SK-D3"></a>SK-D3 | Needs are computed from what the attach delivers; a plain re-entry into a pre-change jail needs nothing | Refusing that re-entry broke every plain attach of a config with a persistent selection (measured 2026-09-05) |
| <a id="SK-D4"></a>SK-D4 | The pre-change jail's config-side drift arm ([`OQ-CS6`](providers.md#oq-cs6)) and the credential gate's CN-D18 arm take the gate's disposition; typed and config-only selections are treated alike | Both used to warn and proceed, the ride-along [`OQ-SK1`](#OQ-SK1) forbids |
| <a id="SK-D5"></a>SK-D5 | The gate sits after the inspect and before any write, under the launch lock; a restart keeps the lock and continues at all three attach sites | The lock is what keeps the stopped jail's teardown off the relaunch's host-services dir |
| <a id="SK-D6"></a>SK-D6 | A restart is a bounded stop, a bounded wait for `--rm` removal, then stale removal; a still-running container refuses with `did not stop` | Never create a second container beside a live one |
| <a id="SK-D7"></a>SK-D7 | Session count is live execs plus the main process when it is a session; anything else names no number | A guessed number is worse than none |
| <a id="SK-D8"></a>SK-D8 | The prompt needs a terminal on stdin and stdout; read by the shared `tty.Confirm`; enter is yes, end of input is no | A closed stdin is not a person accepting the default |
| <a id="SK-D9"></a>SK-D9 | Under the acknowledgment, no part of this entry's channel is written; any non-empty value counts | Writing the shared half alone strips scoped credentials while naming their recipients |
| <a id="SK-D10"></a>SK-D10 | Prompt and account to stdout; refusals and disclosure to stderr | Matches the config-change prompt and the credential gate's refusals |
| <a id="SK-D11"></a>SK-D11 | The release-decode check runs in the short suite against the previous release tag, from `git archive`, built hermetically; the allowlist is keyed by release, each entry with a guard; an unlisted or stale entry fails; under GitHub Actions a missing tag fails rather than skips | A release break is caught before the release, not in a user's jail |
| <a id="SK-D12"></a>SK-D12 | Each v0.10.0 entry's `pinnedBy` names a test that stages the last release's own packs; the module root comes from the package directory | A synthetic tree the new build can read is not the path a real old jail takes; `--show-toplevel` is wrong inside a git hook |
| <a id="SK-D13"></a>SK-D13 | The probe runs all three decode passes the release's boot runs, with a `repair` for breaks `encoding/json` refuses | A manifest-only probe passed breaks that stop a boot, and one refused field hid every other break in that manifest |
| <a id="SK-D14"></a>SK-D14 | On Apple Container the environment is read from `container inspect`'s JSON (`runtime.EnvFromContainerInspectJSON`); the remedy is `yolo stop` on every runtime | podman's template read nothing there, so every Apple Container jail looked current |
| <a id="SK-D15"></a>SK-D15 | An acknowledged attach writes a skew section into the briefing it already refreshes; other attaches write none | stderr scrolls away before the agent starts; the briefing survives and both agent and human read it |
| <a id="SK-D16"></a>SK-D16 | Every skew the acknowledgment covers reaches the section, handed through `Options.attachSkewNotice`, at most one per attach | A missing pack or unstarted daemon leaves the session as short of what it selected as a missing tag |
| <a id="SK-D17"></a>SK-D17 | The acknowledgment's last stderr line says whether the briefing carries the account, naming each case where none does, Apple Container included | The account always ends by saying where else it is recorded |

## Current values

Verified at `8e65f3444`. The prose above explains what each of these is for; this table is the
only place the values themselves are stated.

| Value | Setting | Defined in |
| :--- | :--- | :--- |
| Tags variable | `YOLO_CONTRACT_TAGS`, comma-separated | `entrypoint.ContractTagsEnv` |
| Tags a launch freezes | `entry-channel`, `agent-env-files`, `profile-sets`, `session-hangup` | `run.launchContractTags` |
| Legacy markers | `YOLO_AGENT_ENV_FILES` (implies `agent-env-files`); no frozen `YOLO_PROVIDERS` (implies `entry-channel`) | `run.jailContractTags` |
| Acknowledgment | `YOLO_ALLOW_ATTACH_SKEW`, any non-empty value | `run.AllowAttachSkewEnv` |
| Restart removal wait | 75 polls at 200 ms (15 s) | `run.restartPollAttempts`, `run.restartPollInterval` |
| Session-count inspect | `inspect --format '{{len .ExecIDs}}'`, 3 s bound | `run.jailSessionCount` |
| Release baseline | `git describe --tags --abbrev=0 --match 'v[0-9]*' HEAD^` | `packs/releasedecode_test.go` (`lastRelease`) |
