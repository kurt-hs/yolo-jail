---
status: current
verified: 2026-09-09
verified_commit: 40915b60
covers:
  - internal/cli/host.go
  - internal/cli/hostapply.go
  - internal/cli/hostapplygate.go
  - internal/hostwrap/
  - internal/hostpath/
  - internal/cli/check/section_hostwrappers.go
  - internal/cli/hostinputs.go
  - internal/config/hostpath.go
  - internal/cli/hostfloor.go
  - internal/cli/hostlaunchpath.go
  - internal/cli/applyhostdepgate.go
  - internal/cli/check/section_hostlaunchpath.go
  - internal/cli/configrenderhost.go
  - internal/entrypoint/hostinputs.go
  - internal/entrypoint/hostcomputed.go
  - internal/entrypoint/hostleafrecord.go
  - internal/render/leafrecord.go
tags: [host, env, packs, profiles, wrappers]
summary: "The two channels that deliver a pack's environment to an agent running on the HOST — configuration into the agent's native config surface, environment into the process via `yolo host` — plus the opt-in wrapper directory that makes both reachable from a bare command or an absolute path."
---

# Delivering environment to host agents — two channels and a wrapper directory

**Status:** CURRENT as of 2026-09-09, verified against `40915b60`. The credential disclosure's
remedies for an ad-hoc command (execution flow step 2, and the paragraph after it) are newer, from
2026-09-27 ([ES-D2 to ES-D5](../design/credential-sources-separation.md#10-decision-ledger), with
the remedy's corrections in ES-D10 to ES-D12), and are pinned by unit tests through `hostMain`;
since 2026-09-28 a bare `-p` reaches agent CLIs only here, as in a jail
([OQ-NC5](../plans/notch-convergence.md#7-decision-ledger), which retired ES-D1's `-p` grant). So is
the `--with-credentials` grant beside it, from 2026-09-27
([OQ-ES5](../design/credential-sources-separation.md#10-decision-ledger)'s host half, ES-D13 to ES-D17), and
the refusal of a profile the in-jail bridge would serve (ES-D18 to ES-D20). The removal of
`yolo host apply --shell-init` is newer still, ruled 2026-09-27 ([HE-D1](#he-d1)), and is pinned
by unit tests through `hostMain`; so is the one-row-per-cause shape of the `yolo check` section
([HE-D2](#he-d2)), pinned by the section's own unit tests. Execution flow step 3's `host_path`
folders and the launch PATH every host check reads are from 2026-09-30
([HE-DIR1](#he-dir1)), pinned by unit tests through `hostExec`, `checkDepsMain`, `applyHost`
and the launch gate; [the launch PATH section](#the-launch-path-and-which-copy-of-a-program-runs)
and its rulings were written against `d4e435a3` on 2026-10-01, when the host launch PATH design
graduated into them; its implementation decisions are in
[the host launch PATH ledger](#the-host-launch-path-ledger), moved here on 2026-10-16, where the
design's `HE-D1` and `HE-D2` are renumbered [HE-D16](#he-d16) and [HE-D17](#he-d17) because this
doc's own `HE-D1` and `HE-D2` came first. Step 3's wire
tables are from 2026-09-30 ([FT-D2](../design/agent-footer.md#FT-D2)), pinned by unit tests
through `hostMain`. [What `yolo host apply` renders into a derived surface](#what-yolo-host-apply-renders-into-a-derived-surface)
was written against `d4e435a3` on 2026-10-01, when the host computed-layer design graduated
into it, with [`OQ-HC1`](#oq-hc1) to [`OQ-HC3`](#oq-hc3) in [Why it's this way](#why-its-this-way);
its implementation decisions are in [the host computed-layer ledger](#the-host-computed-layer-ledger),
moved here on 2026-10-16 with the one item still open,
[host pi with auto mode](#host-pi-open), which waits on the maintainer.

Inside a jail, injecting environment is trivial: yolo controls the process spawn, so it passes
`-e KEY=VAL` and PID 1 has the exact environment. On the host it controls nothing — the user
types `claude` in a shell yolo exited from hours ago, or an IDE spawns a binary by absolute
path. And it cannot be avoided: yolo's provider architecture deliberately puts only a variable
*name* in config (`api_key_env_name`), so **something must populate the process environment or
BYOK does not work on the host at all**.

So there are **two channels, always both present, split by what they carry**:

- **Configuration** — endpoints, model aliases, `wire_api`, permissions, MCP wiring — goes into
  the agent's **native config surface**, written by `yolo host apply`. Universal on
  *invocation*: it works for an IDE, a cron job, another process, an absolute path.
- **Environment** — secrets, feature flags, and **unsets** — goes into the **process
  environment**, composed by `yolo host -- <cmd>`. Universal on *payload*: it can carry a
  credential and can delete a variable, which no config file can.

Neither is universal, and they are partial along different axes. That is why both always apply,
and why "just use wrappers for everything" does not collapse the problem.

| Component | Lives in |
| :--- | :--- |
| The `yolo host` verb tree and the exec half | `internal/cli` (`hostMain`, `hostExec`, `hostEnv`, `hostWrappers`) |
| Environment composition, and the resolution order | `internal/cli` (`host.go`: the pack env fold, `hostScopedEnvSources`) |
| Wrapper generation, contents, and the plan | `internal/hostwrap` (`Body`, `Bins`, `Plan`, `OnPath`, `Precedence`) |
| The apply stage that writes them | `internal/cli` (`applyHostWrappers`) |
| The refusal left where `--shell-init` was | `internal/cli` (`refuseShellInit`) |
| The every-run `PATH`, precedence and completeness observations | `internal/cli/check` (`section_hostwrappers.go`) |
| The launch PATH's one resolver, and the miss line | `internal/hostpath` (`Resolve`, `Launch`, `MissLine`, `Hint`, `ManagedDirs`); `host_path` in `internal/config` (`HostPathFolders`, `HostPathRefusals`, `validateHostPath`) |
| Which copy of a program a host launch runs, and the child's PATH | `internal/cli` (`resolveHostLaunchTarget`, `hostChildPath`, `hostfloor.go`) |
| The host's derive inputs, composed once per invocation | `internal/cli` (`composeHostInputs`, `hostinputs.go`); the render side `internal/entrypoint` (`HostInputs`, `JailPathsIn`) |
| The host render of a derive's output: the per-key write, the selection, the computed-leaf record | `internal/entrypoint` (`hostrender.go`, `hostcomputed.go`, `hostleafrecord.go`), `internal/render` (`leafrecord.go`) |
| Where the directory lives | `internal/paths` (`WrapDir`, `WrapDirUnder`, `GeneratedBinDir`) |

**Reads with:** [`providers.md`](providers.md) (what a provider declares, and which agent reads
which endpoint), [`agent-credentials.md`](agent-credentials.md#where-a-relative-env_sources-path-resolves) (what a relative
`env_sources` path points at — the secret channel this composition hydrates),
[`../design/host-render-target.md`](../design/host-render-target.md) (the host as one notch of
the confinement dial), [`pack-system.md`](pack-system.md) (the contribution model),
[`../design/host-tool-provisioning.md`](../design/host-tool-provisioning.md) (the host agent floor,
which supplies every agent a selected pack delivers to a host launch). For the
`host_wrappers` key and every flag, run `yolo config-ref` and `yolo host --help`.

---

## Principles

1. **P1 — Split by payload type, never by agent.** Two channels, both always present for every
   pack, carrying different things. There is no per-agent method selection: selecting per agent
   *was* the fragility, not the fix for it. A pack that needs neither channel declares neither.
2. **P2 — The env channel is mandatory, not a fallback.** `api_key_env_name` and `env_sources`
   put only a variable *name* in config; nothing else populates it on the host. **Any BYOK
   provider is unusable on the host without this channel** — for every agent, not just the one
   with no config file. The deciding fact is not a property of the agent at all: whether the env
   channel is needed is a property of the **provider**. Bedrock needs AWS credentials in the
   environment whether the agent is `claude` or `codex`; a first-party subscription needs
   nothing.
3. **P3 — No silent shell profile pollution.** `yolo host apply` never mutates a shell rc to
   export agent variables. Session-wide exports break tool isolation, leak secrets across
   unrelated commands, and make per-command profile switching impossible. A single `PATH` entry
   is a smaller and different claim — but it is still the user's file, so yolo prints the line
   and never writes it ([HE-D1](#he-d1)).
4. **P4 — One env-composition implementation, two front doors.** `yolo host -p <profile> --
   <agent>` is the mechanism. A generated wrapper is a three-line `exec` into it, never a second
   implementation to drift. That is what makes keeping wrappers affordable.
5. **P5 — The PATH claim is opt-in; the wrapper set is not conditional on anything.** One
   user-level decision (`host_wrappers`) enables the directory. After that a wrapper exists for
   **every host program a selected pack installs**, unconditionally — never per-agent opt-in,
   and never gated on the resolved environment.
6. **P6 — Blocker, launcher, wrapper: three mechanisms, three words, and "shim" is retired.**
   They sit at different `PATH` positions for different reasons, and the directory names say
   which is which: `bin/block` and `bin/launch` in a jail, `bin/wrap` on the host, and on the
   host a `bin/block/<digest>` per set of blockers, first on the PATH of the one program a
   `yolo host --` launch starts ([HE-D11](#he-d11)).

## The vocabulary

Three generated script directories exist, and conflating them is the mistake P6 exists to
prevent. Each is defined by what its scripts *do*, not by where they sit:

- **Blocker** — a script that refuses a command and prints an alternative (`exit 127`). In the
  jail, first on `PATH`, because interception is its whole job; at the host, first on the PATH of
  the program `yolo host --` starts, from the user scope and the selected packs only.
- **Launcher** — a script that installs or updates a tool on use, then `exec`s the real binary.
  In the jail, **second** on `PATH` — ahead of every install prefix, or it becomes unreachable
  the moment its own install succeeds.
  [program-delivery.md §3.5](../design/program-delivery.md#35-the-second-axis-who-the-dependency-serves-amendment-2026-09-03)
  is the authority for that position and for the collision check that replaced the old one.
- **Wrapper** — a script that composes a host environment and `exec`s into `yolo host`. On the
  host, in its own directory, **prepended** to `PATH`.

> [!WARNING]
> **Blockers and launchers are gathered in the filesystem, never on `PATH`.** They share one
> parent (`~/.yolo/bin` in the jail, a single bind-mount anchor), and nothing may put that
> parent on `PATH` — a launcher reachable from the blockers' position defeats both mechanisms.
> Both are cleared **contents-only**: the directory itself may be a mount or a captured `PATH`
> entry, so `RemoveAll` is wrong. The host wrap directory follows the same rule for the same
> reason.

## The wrapper directory

A wrapper only works if it is found *before* the real binary, and the real `claude` is installed
by its own installer into `~/.local/bin/claude`.

> [!CAUTION]
> **`~/.local/bin` is not a placement option, and the reason is stronger than `PATH` ordering —
> it is a FILE collision.** A wrapper named `claude` in the directory that holds the real
> `claude` is the same path: one overwrites the other, and `claude update` re-running the
> installer either clobbers the wrapper or fails against it.

So wrappers get their own directory, prepended ahead of `~/.local/bin`. Four consequences of
that placement:

1. **Prepend, not append.** Appending puts the wrapper behind the real binary, where it never
   runs. Prepending means everything in that directory shadows the user's tools — a standing
   claim, which is why the directory holds only generated wrappers and is reset contents-only.
2. **It is an rc edit, and P3 says yolo does not make it.** `apply` prints the line, `yolo check`
   repeats it until it takes effect, and the user adds it. `yolo host apply --shell-init`, which
   used to append it, is removed and refuses, printing the line ([HE-D1](#he-d1)).
3. **It is one decision, not one per agent** — the `host_wrappers` config key, top level beside
   `host_files`. Not opted in means no directory, no wrappers, and no messages at all;
   `yolo host --` still works.

   ⚠ **And since 2026-09-22 that decision is usually DERIVED rather than made.** An unset
   `host_wrappers` is **ON** when `host_management` is `"own"`, because `own` means yolo composes the
   agent's config file whole — and a config file cannot carry a credential, so without a wrapper on
   PATH a bare `claude` gets the config and none of the environment it assumes. Declaring `own` and
   nothing else left exactly that half-configured host, and `yolo doctor` was silent about it because
   [the section that reports this state](#apply-reports-actions-check-reports-state) short-circuits on the opt-in.
   An explicit `false` still wins. ⚠ Only a WRITTEN `"own"` derives; the unset key (`"none"` since
   the `assert` retirement) does not — deriving from it would put a PATH claim on every machine that
   declared nothing.

   **`yolo host wrappers enable`/`disable` are DELETED** (they now refuse, naming the derivation). A
   verb whose whole effect was writing one boolean into the user's config made yolo a second writer
   of a file the user owns, for a line they can type themselves — and with the default derived, the
   common case needs no line at all. `yolo host wrappers status` survives, because it only reads.
4. **The wrapper is three lines and holds no logic** — a header comment and
   `exec <yolo> host -- <program> "$@"`, naming the yolo that wrote it by absolute path
   ([HE-D14](#he-d14)). One env-composition implementation (P4).

### What a wrapper does not cover

| Bypass | Consequence |
| :--- | :--- |
| Invocation by absolute path to the real binary | wrapper skipped |
| An IDE extension or desktop launcher with a configured binary path | wrapper skipped — **and the inversion is the fix**: point it at `<wrap dir>/claude`. The wrapper starts yolo by absolute path, so no `PATH` is consulted to reach yolo ([HE-D14](#he-d14)); `yolo host` then finds a program outside the floor (one whose pack the user-scope `host_floor` leaves out, for one) on the launcher's `PATH` and `host_path`, so that launcher needs `host_path` naming the program's folder, which `yolo check` names ([HE-D15](#he-d15)); any other declared program the floor has no entry for is refused rather than looked up ([HNR-D2](../design/host-notch-readiness.md#HNR-D2)) |
| A shell function of the same name | **beats `PATH` outright** — it has to be deleted either way |
| A process that sanitizes `PATH` before spawning | wrapper skipped |

A generated wrapper is therefore a governance win over a hand-written `.bashrc` function
(versioned, uniform, reviewable, removable) and — addressed by absolute path — a coverage win
beyond it. When `PATH` is not consulted, `yolo host -- claude` is a documented answer rather
than a mystery, and `<wrap dir>/claude` is the same answer in file form.

## Why every program gets a wrapper

The wrap directory is an **addressable launch surface**, the way a mise or asdf shim directory
is: the shim for every installed tool exists unconditionally, so a script or an IDE can point at
`<wrap dir>/<program>` and rely on getting a correct environment regardless of shell config.

There is **no trigger and nothing computes one**. The only gate is structural: a pack that
installs no program gets no wrapper. What varies by config is not *whether* the wrapper exists
but what its launch composes, and that is resolved at launch time from live state.

> [!WARNING]
> **Do not gate generation on the resolved environment.** "Generate only where the composed env
> is non-empty" fails twice. A gate computed at *apply* time gates a file whose payload is
> composed at *launch* time, so the wrapper set becomes a function of user config — unstable
> across machines and config edits. And it destroys the addressable-surface property: a
> conditionally-existing `<wrap dir>/agy` is a path nothing can rely on, which is to say, not a
> surface. The related objection — "a wrapper that injects nothing is a lie" — argues about a
> wrapper this design does not have: **no wrapper injects anything**, for any pack.

Nothing in a manifest can declare the trigger either, which is why there isn't one. Whether an
agent needs the env channel depends on whether *the user* configured a provider carrying an
`api_key_env` — a fact in the user's config, not in the pack's. A manifest declaration would
systematically under-generate. Three sources feed the composition, and only the first is
anything a manifest states: a pack's static `env` (or its active profile's `env` block), the
`api_key_env_name` of every provider the pack projects, and **removals** (a `null` value, i.e.
an `unset`, which has no config-surface equivalent at all).

> [!NOTE]
> **The honest cost of unconditional generation:** with wrappers enabled, yolo becomes a hard
> runtime dependency of every wrapped launch — a broken `yolo` or an unparseable config takes
> down an agent that needed nothing from yolo. It is bounded by the bypass table above: the real
> binary still sits behind the wrap dir on `PATH`, and invoking it by absolute path still works.

## The command surface

`yolo host` is the host-side counterpart of `yolo run`, and after P4 it is the **only** place a
host process environment is composed. Two spellings of the apply operation remain and the third
was removed outright:

| Spelling | Role |
| :--- | :--- |
| `yolo apply --at host` | The systematic form. The notch stays one value of the `confinement` dial, so every notch is spelled the same way |
| `yolo host apply` | The ergonomic form, and where the host-**only** verbs live: `yolo host env`, `yolo host wrappers status`, and the exec half `yolo host -- <cmd>` |
| `yolo apply --host` | **Removed.** Not deprecated with a message |

Removal does not re-special-case the host. The dial governs *what the notch is*; `yolo host`
governs *where its ergonomics live*, and it earns a namespace for a reason no other notch can
match: only the host has a user shell and a `PATH` to claim, so `yolo host env` and
`yolo host wrappers status` have no `jail` or `guest` counterpart and nowhere else to go.

**A host apply refuses inside a jail**, at both spellings and every posture (`--assert`,
`--revert`, and `yolo apply` under `confinement: host`): it renders into the home of whoever runs
it, and a jail's home is the jail's own, which its launch already rendered. It exits 1, writes
nothing, and names the command to run on the host: `yolo host apply --assert`, or
`yolo host apply --revert --assert` for a revert. The jail's config is rendered again by its next
launch. `yolo host --` and `yolo host env` still run in a jail
([DP-I7](../design/declaration-parity.md#DP-I7)).

**The exec half has the same two spellings**, and the front door decides between them once
(`cli.routeArgv`): every launch carrying `--at host`, wherever the flag sits, is `yolo host`.
`yolo --at host -- <cmd>`, `yolo run --at host -- <cmd>`, `yolo --at host run -- <cmd>` and a
bare `yolo --at host` all route there; the bare one runs nothing and prints `yolo host`'s usage.
The last `--at` typed wins in either order, and every `--at` is consumed on the way, so
`yolo --at jail --at host -- <cmd>` runs at the host as `yolo --at host --at jail -- <cmd>` runs
in the jail. `yolo host` takes `--at host` as a no-op and refuses any
other notch, and a jail-launch flag with no meaning at the host (`--dry-run`, `--network`,
`--accept-config-changes`) is refused by name rather than ignored. `--timing` has a meaning at
both notches and is taken: it times the host launch's stages
([perf-logging.md, The host notch](perf-logging.md#the-host-notch)). The config key
`confinement: host` is not an `--at` spelling and still refuses a launch
([OQ-DP3](../design/declaration-parity.md#decision-ledger)).

**`--` is parsed before any verb**, and that ordering is the whole grammar: the exec half takes
flags before the separator (`yolo host -p bedrock -- claude`), so the first argument is routinely
a flag rather than a verb, and a verb switch running first would have to re-implement flag
parsing to discover whether a verb was present at all. With no `--`, a first argument that is a flag
(other than `--help`) is still an exec flag, since no verb starts with a dash: `yolo host -p zai`
and `yolo --at host --profile=` get the exec half's refusals, exit 2, and flags that parse are
refused for naming no command, the host verb having no default one.

### Execution flow

1. **Locate the target binary.** A bare name of a program a selected pack delivers is **yolo's
   floor copy**, `~/.local/share/yolo-jail/host-floor/bin/<name>`, installed first when it is
   missing, whatever PATH the caller held
   ([`host-tool-provisioning.md`](../design/host-tool-provisioning.md), HP-DIR4). A target given
   as a path is exec'd as given. A selected pack's program the floor cannot hold on this machine
   is refused, naming the floor's reason and its fix
   ([HNR-D2](../design/host-notch-readiness.md#HNR-D2)). Anything else, and a selected pack's
   program that is not the floor's to hold (`host_floor` leaves its pack out, the `provisioners`
   order gives it to a manager, or its vendor publishes no build here; said on one line), is looked up on the child's PATH
   (step 3), **skipping yolo-managed directories** and the floor's own `bin/`: a name there that no
   selected pack delivers is an entry the floor no longer keeps, which is never run, and `yolo host
   apply --assert` removes it. Before any of this, the launch installs every other program a
   selected pack declares and stops when one cannot install
   ([HNR-D1](../design/host-notch-readiness.md#HNR-D1)). A program whose pack declares a pre-launch `refresh` (pi's extension
   update) then runs it against the copy that resolved, under the hourly rule a jail's launcher
   keeps; a failure is a line and the launch goes on
   ([HP-D19](../design/host-tool-provisioning.md#HP-D19)).
2. **Resolve the pack configuration** — the active profile for the launched command, and its
   effective `env` for the active workspace. The profile is a typed `-p`, else the command's
   entry in the `profile` key, else its `"*"` (or the key's string form) when a selected pack
   installs the command. A typed `-p` for a command no selected pack installs is refused, naming
   `--with-credentials` ([OQ-NC5](../plans/notch-convergence.md#7-decision-ledger)), and a `profile`
   key no resolvable pack installs is refused with the validator's message
   ([ES-D5](../design/credential-sources-separation.md#10-decision-ledger)), so no profile
   selects for such a command.
3. **Compose the process environment** — start from the current environment, then apply the one
   ordered composition every notch serializes ([notch-convergence NC-D72](../plans/notch-convergence.md#NC-D72)):
   the selected packs' `env`, then `env_sources` (the secret channel) and its **removals** (a `null`
   is an `unset`, not an empty string, and takes out the shell's value and a pack's), then the
   resolved profile's provider environment, which beats both; then set the three **wire tables** a
   jail launch also carries (`YOLO_PROVIDERS`, `YOLO_PROFILES`, and `YOLO_USE_PROFILES` holding
   this one command's profile, `{}` when it has none), which no pack `env`, `env_sources` value or
   `null` replaces or removes. The tables are set on every launch, so a launch started inside
   another replaces what it inherited, and an agent's footer names the profile this launch runs on
   ([FT-D2](../design/agent-footer.md#FT-D2)). PATH is overlaid last: the caller's PATH, then each
   folder of the user-scope `host_path` list not already on it, then the floor's `bin/`, which
   holds agent names only, so an agent's own commands see the caller's PATH first. A caller with no
   PATH at all (`env -i`) gets the system directories the floor's installers run with in its place,
   so an agent's commands still resolve. A dependency probe answers a program the floor delivers by
   its floor entry; every other PATH check reads the **launch PATH** (the caller's PATH, then
   `host_path`'s folders: [HE-DIR1](#he-dir1)) through one
   resolver, `internal/hostpath`, and a miss prints one line naming the PATH searched and the
   `host_path` fix ([the miss line](#the-miss-line)).
4. **Say what starts** — one line naming the target and where it came from (yolo's floor copy,
   your PATH, or as given) — and **exec** it with that environment.

> [!WARNING]
> **Step 1's skip is load-bearing.** It is what lets `<wrap dir>/claude` be
> `exec <yolo> host -- claude "$@"` ([HE-D14](#he-d14)) without calling itself. Narrow that skip
> and the wrapper front door breaks first, and loudly.

`eval "$(yolo host env)"` is a third front door onto the same composition, for direnv and mise
users. It emits POSIX `export` lines by default, with a JSON format for tooling. The script is
**one agent's slice** (`--agent`, default `claude`): a provider credential another agent's
profile claims is not in it ([the credential gate](providers.md#the-credential-gate)), and the
gate's disclosure goes to stderr, which an eval'ing shell does not read, naming what was
withheld.

**`-p` reaches agent CLIs only, as it does in a jail.** `yolo host -p zai -- pi` runs pi on the
zai profile, because a selected pack installs pi. `yolo host -p zai -- curl …` is refused before
anything runs, naming `yolo host --with-credentials zai -- curl …`, the grant for an ad-hoc
command ([OQ-NC5](../plans/notch-convergence.md#7-decision-ledger)).
`eval "$(yolo host env --with-credentials zai)"` puts the same keys in the current shell. Each
withheld line in the disclosure names the fix: the grant for an ad-hoc command, and for an agent
a `-p` with a declared profile resolving to the claiming provider that the agent can run on, or a
note to declare one. When an agent cannot run on that profile, the line names the grant for an
ad-hoc `bash` instead; on an agent that can, it says the `-p` replaces the agent's own profile. A
withheld name the invoking shell already exports is disclosed as not added by yolo, and one a
pack's `env` sets as held from that source, since the command holds either anyway
([`providers.md`](providers.md#the-credential-gate)).

**`--with-credentials` hands a command keys by provider, several at once.**
`yolo host --with-credentials <provider[,provider...]|all> -- <cmd>` gives the one command the
named providers' claimed `env_sources` values, keys only. `all` means every composed provider
that claims a value, which is what a usage bar that pings every subscription needs from the one
store the user keeps. It selects no profile and re-points nothing, and it combines with `-p`: an
agent keeps its profile and receives the granted keys too.
`eval "$(yolo host env --with-credentials all)"` is the shell spelling. There, with neither
`--agent` nor `-p`, the script is an ad-hoc command's slice, so no agent's provider shape reaches
the shell. With `-p` it is the slice `-p` composes, the verb's default agent's, plus the keys
([ES-D21](../design/credential-sources-separation.md#10-decision-ledger)). Every run given the
flag prints a `Credential grant` block, names only, and a named provider with nothing to hand
over is reported. On such a run, a withheld line names the same run with the claimant added to
the grant, such as `yolo host --with-credentials zai,cerebras -- usage-bar`, rather than a `-p`,
which would replace the typed profile and drop the grant
([ES-D23](../design/credential-sources-separation.md#10-decision-ledger)). An unknown provider
refuses, naming the known ones. Only the typed flag grants
([OQ-ES5](../design/credential-sources-separation.md#10-decision-ledger);
[ES-D13 to ES-D16](../design/credential-sources-separation.md#10-decision-ledger)). A jail launch
takes the same flag since 2026-10-05, and the jail holds the set for its whole life
([§5.2](../design/credential-sources-separation.md#52-the-jail-half---with-credentials-at-a-jail-launch-built)).

**A profile the wire bridge serves starts the bridge for that launch.** `yolo host -p cerebras --
claude` and `yolo host -p codex -- claude` start the bridge's host half as the launch's own
child, on a loopback port it picked, answering only that launch's caller token, which claude gets
as `ANTHROPIC_AUTH_TOKEN`; the launch stays resident and stops the bridge when claude exits,
by any route. `yolo host env` refuses such a profile, naming the `yolo host --` spelling, and
`yolo host apply` renders no address for a bridged selection in the `profile` key and says so. A
bridge the launch cannot start refuses, naming why and `yolo -p claude=<profile> -- claude` as
the container launch where the profile works. An agent that also speaks the provider's own wire
runs on it directly
([`wire-bridge.md`](wire-bridge.md#at-the-host-notch),
[`host-notch-services.md`](../design/host-notch-services.md)).

## What `yolo host apply` renders into a derived surface

<a id="the-computed-layer-at-the-host"></a>

The configuration channel writes more than the layers a manifest declares. **The host runs the
jail's own derives** (a pack's `yolo.derive` functions, whose output is a surface's **computed
layer**) over inputs composed at user scope, and lands their output in the real files
([`OQ-HC1`](#oq-hc1): *"we're trying for host parity with the same handling"*). There is no
per-surface opt-in and no derive branches on the notch: what differs between the notches is only
how the inputs are composed. So host pi registers yolo's `openai-codex` list, and your
`mcp_servers`, `lsp_servers` and providers reach the real agent files as they reach a jail's.

**One composition per invocation, read by every host-target reader** (`composeHostInputs`,
`internal/cli`): `yolo host apply`'s dry run, its `--assert` and `--format json`, the
[launch gate](host-apply-staleness.md#the-launch-gate) a wrapped `yolo host --` runs, and
`yolo config render --at host`; `yolo config ls --at host` lists each derived surface's
computed layer from the derive registrations. A reader that skipped the composition would render
catalogs without providers, and the launch gate would undo an apply's rows or report them as
drift at every wrapped launch. The inputs travel as the jail's own wire tables, in the host
environment's variables, so the readers a jail's boot uses read the host composition unchanged.

**The inputs**, every one from your user config and the selected packs, never from a workspace:

| Input | At the host |
| :--- | :--- |
| `providers` | your `providers` entries over the selected packs' provider facts, the table `yolo host --` composes, with no address a pack's own service serves; the packs' `models` contributions shape the lists |
| `profiles`, `profile` | your profiles resolved over that table, every `via` address cleared, since no jail daemon serves one here; the selection is the `profile` key alone, since host apply has no `-p`. A selection the host cannot serve (a profile only the wire bridge reaches, an active set one of whose entries the host refuses) is left out and named |
| `mcp_servers`, `lsp_servers` | your own entries, merged over the selected packs' `mcp` entries joined to your home (MCP only), less any whose command or arguments name a jail-only path, each named. A FETCHED pack's `mcp` entry is left out and named: its command would run unconfined as you ([HC-D26](#HC-D26)). An MCP preset is never expanded: its command is a wrapper only a jail's boot writes, and the report names the pack that ships a server of its name, `chrome-devtools` for that preset, or else says to declare the server under `mcp_servers` ([HC-D27](#HC-D27)). The same `lsp_servers` table renders Claude's `yolo-lsp` plugin (below the surface table) |

<a id="what-each-surface-gets-at-the-host"></a>

What each shipped derived surface gets at the host, as built (`358f877d`; the per-surface
account the design recorded):

| Surface | What the host render writes |
| :--- | :--- |
| `pi/models` | a row per pi-reachable provider in the composed table, never `openai-codex`, as in a jail |
| `pi/codex-models` | the declared `openai-codex` list, which the delivered extension registers |
| `pi/mcp` | your `mcp_servers`, filtered per agent, into pi's own `~/.pi/agent/mcp.json` per server (rule 2 below), so a server you added with `pi mcp add` stays; a server dropped from `mcp_servers` leaves an emptied entry pi reports and skips ([pi's MCP files](mcp-configuration.md#pis-mcp-files)) |
| `copilot/mcp`, `agy/mcp` | your `mcp_servers`, filtered per agent |
| `copilot/lsp` | your `lsp_servers` |
| `oh-omp/models` | provider rows |
| `claude/config` | your `mcp_servers` as `mcpServers`, so yolo owns `~/.claude.json`'s user-scope server list at the host as in a jail: a server added with `claude mcp add` is a loss the first apply confirms |
| `claude/settings` | `env.ENABLE_LSP_TOOL` beside your own variables |
| `codex/config`, `opencode/config` | your MCP servers, a row for each provider the agent can reach, and the selected profile's model |
| `pi/settings` | the selected profile's `defaultProvider` and `defaultModel`, and the pi-subagents policy |

**Claude's language servers arrive as a plugin, beside the derived surfaces.** Claude takes an LSP
server only from a plugin, which no derive writes, so `yolo host apply` renders your composed
`lsp_servers` as the `yolo-lsp` plugin into every skills destination the selected packs compose:
the bytes a launch stages, kept in sync, refused by name where a `yolo-lsp` it did not write
holds the name, and archived when the table empties or the destination is no longer composed
([at the host](mcp-configuration.md#at-the-host-yolo-host-apply-writes-it)). The
`claude/settings` row's `ENABLE_LSP_TOOL` is the switch; the plugin is what it switches on.

**An MCP server is filtered per agent, as in a jail.** Its `requires_env` is asked of the
environment `yolo host env --agent <agent>` would compose, so a provider credential scoped to
one agent ([the credential gate](providers.md#the-credential-gate)) writes the server for that
agent only, and the report names the agents that got it. Its `provides` is checked against each
agent's own native capabilities, so a `provides: "web_search"` server reaches neither claude's
nor agy's files. ⚠ A server written this way resolves its `${VAR}` from the agent's own
environment, so an agent started outside `yolo host`, from an IDE or directly, lacks a variable
that came from `env_sources` and that one server fails when the agent spawns it.

<a id="the-host-write-is-per-key"></a>

**A derive's output reaches a real file per key, never through a tombstone** ([HC-D10](#HC-D10)):

1. a table the derive declares in full (`ctx.in_full`) is yolo's, and is written wholesale;
2. any other object a derive returns asserts only the leaves it names, and the rest of that
   object stays as your file holds it: with an LSP server, `claude/settings` gains
   `env.ENABLE_LSP_TOOL` beside your own variables;
3. a tombstone is dropped before either arm sees it, because at the host the only layer below a
   derive is your real file, so a tombstone could only delete a key you wrote;
4. a leaf yolo stops asserting is **cleared** when the file still holds the value yolo wrote
   there. The rmw arm keeps a **computed-leaf record** (a term the design coined: the value yolo
   wrote at each leaf's RFC 6901 pointer, beside the provenance record), and records a leaf only
   when yolo's write is what put that value in the file. So moving claude off Bedrock removes the
   `CLAUDE_CODE_USE_BEDROCK` an earlier apply wrote, while a value you set before yolo asserted
   it, or changed after, stays. A launch reads the same record to tell a switch yolo wrote from
   one you wrote.

> [!WARNING]
> **Do not hand a derive's output to the jail's `rmw` table writer.** That writer
> (`regenerateManagedTables`) clears and rewrites every object-valued computed key, declared in
> full or not: given `claude/settings`' derive output over empty inputs, it turned a file's
> `{"env": {"MY_VAR": "x"}}` into `{"env": {}}` (measured while the design was argued). Rule 2 is
> an obligation on the writer, not a property of the old one.

**A jail path never reaches a real home.** The output of every derive is checked, keys and
values, for a jail-only root: the jail render target's home and workspace, and the mounts every
container launch supplies (the context root, the install prefix, `/run/yolo`, the service
endpoint directory). A root counts only as a whole path token, and one the rendered home lies at
or under is not a jail path there, which covers an account whose home is `/home/agent` and an
in-jail `yolo config render` of the host target (a host apply itself refuses in a jail). A surface whose
output names one is refused by name (`entrypoint.JailPathsIn`, which also omits a user's server
entry at composition, so one bad entry costs only that entry).

**The `profile` key's selection is written with the jail's edge-triggered rule**
([`OQ-HC3`](#oq-hc3)): yolo writes it when the chosen profile changes and clears only what it
wrote ([`OQ-PSW2`](providers.md#oq-psw2)), so a later `/model` pick of yours stands. At the host
the real file is the only layer below, so a file value the selection record does not hold is
outranked by the first activation, and a recorded key whose value differs is your own pick.
Under `own` the selection record lives in the capture store (the retired `assert`, which had no
store, kept it beside the provenance record).

**A launch's `-p` is handed to the program, never written**
([MM-D30](../design/model-lists-and-pickers.md#MM-D30)). The file above holds the `profile`
key's selection alone. When `yolo host -p <profile> -- <agent>` selects differently, a program
whose pack declares a `launch_selection` gets the `-p`'s selection for that process only, except
a launch whose first word is a subcommand the pack names (`yolo host -p zai -- pi update`), which
runs as typed and says so: codex as `-c` overrides right after `codex`, opencode in `OPENCODE_CONFIG_CONTENT` (merged over a value
you set there), pi and oh-omp as their own flags, with pi's two model-list files in the variables
its extensions read first. Each is disclosed; your own later flag still wins. With no `-p`, or a
`-p` that composes what the `profile` key does, the program starts on its file, so a model you
picked in it since stands. `yolo host env -p` exports a selection carried in a variable and, for
one that needs argv, names the `yolo host -p` launch instead.

**Under `host_management: own`, a `computed` surface renders through `stateful`**
([`OQ-HC2`](#oq-hc2)), so the first owned render adopts the file rather than replacing it; a
keyless `computed` surface is still refused.

**The catalogs change owner.** A provider catalog a derive declares in full is a yolo-owned table
at the host, so a provider added by hand to a real file is dropped at the next apply. The first
host apply that makes a table yolo's in a home asks before dropping an entry in it, and the
dropped-entry remedy names your `mcp_servers`, `lsp_servers` and `providers` first, with a
per-surface `config-overlay` as the one-agent alternative.

**Failures stay local where they can.** A derive that raises an error refuses that surface only,
naming the error, and leaves its file alone. A provider or profile table that cannot be composed
refuses the whole apply before anything is written, as `yolo host --` refuses on it.

**Reset and the overwrite report read the computed layer too (2026-10-04).** `yolo config reset
--at host` under `own` lands what the next apply lands — the computed layer, the overlays and the
`profile` selection, not the declared layers alone — and when the apply's own composition cannot
be built it says so and names `yolo host apply --assert`. A computed leaf that replaces a value of
yours is reported as an overwrite under both contracts, named by the input of yours it comes from
(`subagents.defaultModel (computed from your profile)`), with that input as the remedy. A key the
profile's selection writes is named apart (`defaultModel (selected by your profile)`): it is
replaced only on the profile's first activation, and a pick of your own after that stands. A leaf
computed from the same profile is written on every apply.

MEASURED after the build (2026-09-28): `yolo config render --at host` for `codex` and `opencode`
(codex's selected model, opencode's provider row, `model` and `small_model`), and each of the
build's tests failing with its call site removed. UNMEASURED: no agent has been started against a
file a host apply wrote. MEASURED 2026-10-04: before that day, `oh-omp/models` and `oh-omp/settings`
were refused under the since-retired `assert` ("no RMW encoder for codec yaml") and under `own` over
an existing file ("no RMW decoder"); the `rmw` arm now has a yaml reader and writer that keep your
keys, their order and the comments above what the render leaves alone, and refuse — untouched — a
file they could not write back as written (more than one document, an anchor or alias, a merge key,
a non-string key, an unquoted date or another non-core tag). Under `own` it renders through
`stateful`.

## The launch PATH, and which copy of a program runs

<a id="the-launch-path"></a>

**At `yolo host`, yolo's checks read the PATH yolo was started with, plus the folders the
user-scope `host_path` list adds, and look in no other folder** ([`HE-DIR1`](#he-dir1), the
maintainer's ruling: *"it's just not feasible to otherwise know these things … And then I think we
just get things from [PATH]."*). Whether a program a pack needs is present, and which copy of a
program no floor entry covers runs, are answered from that PATH. What stays fixed whoever starts
yolo is what yolo itself provides: the **host agent floor**'s copy of each agent a selected pack
delivers, and the floor's own Node (the floor is yolo's own host directory of the selected packs'
`program` binaries; [`host-tool-provisioning.md`](../design/host-tool-provisioning.md) coins the
term and owns it). The cost is accepted by the ruling: a launcher that passes a bare PATH (a
Waybar button, cron, a macOS hotkey launcher) can get a different answer from a terminal for a
program yolo does not provide, and the fix is one `host_path` line, which the
[miss line](#the-miss-line) prints.

The words this section uses, coined by the host launch PATH design it graduated from (in git
history, with [its ledger](#the-host-launch-path-ledger) moved here) unless it says otherwise:

- **Launch PATH**: the PATH yolo's checks read at a host launch. It is not the agent's PATH, which
  adds the floor's `bin/`, and not the user's interactive shell's PATH, which yolo sees only when
  that shell started it.
- **Delivers** (the rulings' word): a selected pack *delivers* a program when the floor holds, or
  can provision, an entry for it on this machine. One the floor can provision but has not yet is
  delivered: the launch installs it first. One the floor cannot hold here (configured out of the
  floor, handed to another provisioner, unpublished for this OS and architecture, or an installer
  agent this machine can capture neither in a jail nor on the host, for example on a Mac before
  `yolo macos-setup` or on Linux with no container runtime and no Landlock; host-tool-provisioning.md
  HP-D2 and HP-D18 name every case) is not.
- **Decision input** and **carried variable**: a variable yolo reads to decide something, and one
  it only hands to the child.

**The launch PATH is, in order:** the PATH yolo was started with, its entries in their order, when
it is set and not empty; then each `host_path` folder not already on it, in written order.
Duplicates keep their first occurrence. Started with no PATH, or an empty one, the launch PATH is
`host_path`'s folders alone, and the miss line says yolo was started with no PATH. Nothing else
joins it: no per-OS baseline, no folder a pack declares, no typed entry for a tool manager, no
`YOLO_*` variable. With `host_path` unset, the launch PATH is the PATH yolo was started with, which
is what every check read before the key existed. In a jail the resolver returns the process PATH
unchanged, since a jail's PATH is already composed and `host_path` is host-only.

**`host_path`** is a list of directory strings, each absolute or starting with `~/`. Validation
refuses a relative path, `~user/`, any `$` (an entry that expanded a variable would read the
launcher's environment a second time, behind a key that looks fixed) and any `:`. An empty list is
the same as unset. A declared folder that does not exist is kept, skipped by lookup as a shell
skips it, and reported absent by `yolo check`. No host verb validates the config before it reads
the key, so a refused entry reaches no PATH, and the miss line and `yolo check`'s host launch
section each name it with its fix. The key is user scope only: a workspace config is
agent-editable, and a PATH entry there would let a cloned repository choose the binary a host
agent runs. `yolo host env` emits no PATH line, because its output is `eval`'d into the caller's
own shell, where the caller owns PATH.

<a id="which-copy-runs"></a>

**Which copy of a program runs**, by three exec rules:

- **a program a selected pack delivers, named bare**, is looked up on no PATH. It execs from the
  floor by path, and an npm agent starts on the floor's own `node` by absolute path, with mise
  stripped;
- **a target given as a path** is exec'd as given, and still gets the composition its base name
  keys, so `yolo host -- ~/src/claude/dist/claude` runs that build with the claude pack's
  environment and launch flags;
- **a program a selected pack delivers that the floor cannot hold on this machine** is refused,
  naming the floor's reason and its fix: a copy on the PATH never stands in for it
  ([HNR-D2](../design/host-notch-readiness.md#HNR-D2), which reversed [`OQ-HE11`](#oq-he11) (a)).
  A program that is not the floor's to hold is the exception: a pack `host_floor` leaves out, a
  program the `provisioners` order gives to a manager, or one whose vendor publishes no build here
  is looked up on the PATH with one line saying so ([HNR-D4](../design/host-notch-readiness.md#HNR-D4));
- **any other target** is looked up on the child's PATH, as the user's shell would find it.

**The child's PATH is the launch PATH, then the floor's `bin/`**, duplicates removed, overlaid
last so no pack env or profile replaces it ([`OQ-HE10`](#oq-he10)). When the launch blocks a tool,
one more entry goes first, its block dir, and any other block dir an outer launch handed down is
taken out ([HE-D11](#he-d11)); with nothing blocked the PATH is
exactly this one. The floor's `bin/` holds
agent names only, so last means a lookup of an agent reaches the floor only where nothing of the
user's has one, and the agent's own commands see the user's PATH first. So for a launch started
with a PATH, a check and the agent search the same folders. ⚠ **A launch started with no PATH at
all is the exception:** the child keeps the floor's stand-in, the system folders the floor's
installers run with, ahead of `host_path`'s, while the checks search `host_path` alone. There a
check can read missing a tool the agent then finds in a system folder, and the gate's miss line
says *the PATH yolo searched* rather than *this launch's*.

<a id="one-resolver"></a>

**One resolver computes the launch PATH** (`hostpath.Resolve`, returning a `hostpath.Launch`):
the ordered entries, where each came from, and a `LookPath` that skips yolo's own managed
directories, so a wrapper in the wrap dir never reads as the program it wraps. Every host PATH
check asks it: the exec's target lookup, the child's PATH, the dependency probe (the launch gate's
survey, `yolo host apply`, `check-deps`, the re-probe after an install), the package-manager guess
behind a remedy, the install the dependency gate runs (whose `PATH` is set to the launch PATH, so
the re-probe reads the PATH the installer ran with), and `yolo check`'s host launch section, which
says it read the PATH of the shell `yolo check` runs in. A program a selected pack delivers is
checked by its floor entry instead, the copy that runs.

> [!WARNING]
> **No host-notch decision may read `os.Getenv("PATH")` or call a bare `exec.LookPath`.** Two
> readers of PATH can disagree, which is why there is one. Two readers are exempt by name, and
> read the PATH yolo was started with alone: the wrapper-precedence checks, whose question is
> whether a bare command typed in the caller's shell reaches the wrapper, and the floor's
> capture-runtime probe, whose question is about the jail launch a capture boots, which finds its
> runtime on that PATH like every jail-launch lookup.

<a id="the-miss-line"></a>

**The miss line** is one line yolo prints beside its verdict whenever a PATH check or a target
lookup at the host finds nothing: a `requires` tool, a selected pack's program the floor cannot
hold, or the target of `yolo host -- <cmd>`. From a Waybar widget whose PATH is `/usr/bin:/bin`,
with `rg` required by the guardrails pack and installed in `~/.cargo/bin`:

```text
yolo host: rg (required by the guardrails pack) is not on this launch's PATH, /usr/bin:/bin, the PATH yolo was started with. If rg is installed, add its folder to "host_path" in ~/.config/yolo-jail/config.jsonc; ~/.cargo/bin has one.
```

- **It prints the whole PATH**, never truncated, with `host_path`'s part named as such, because it
  answers "where did it look".
- **Its last clause appears only when a hint location holds the program.** The **hint locations**
  are a compiled list of folders where tools commonly live outside a launcher's PATH (Homebrew's
  prefixes, `~/.cargo/bin`, mise's default shims folder and a few more; `hostfloor.HintLocations`
  is the list). **A hint location never resolves anything**: one `stat` per folder, no recursion,
  no command run, and it changes only the line's text, never whether a check passes or what runs.
- **Where it prints:** the launch gate's report, under `yolo host apply`'s dependency blocker and
  in its JSON document, under `check-deps`' MISSING line, as the note of `yolo check`'s warning,
  and in place of the exec's lookup error, once per missing program per run. A launch says *this
  launch's PATH*, a verb that only checks says *the PATH yolo searched*. A program with no build
  for this host is no blocker, since nothing could install it, so it gets the line beside its
  no-build line instead.
- **It is a disclosure on the launch stream** ([`report-tiers.md`](report-tiers.md#the-launch-stream)),
  so no flag hides it, and it prints on every miss: yolo cannot tell a bare launcher from a
  terminal. At launch only the gate surveys dependencies, and the gate runs only with
  `host_apply_on_launch` on, so with it off a missing `requires` tool prints nothing at launch
  and the agent's first call to it fails. The exec's miss for the target prints either way.

**Tool managers get nothing of their own.** A user whose shell activates mise has mise's folders
on the PATH their terminal hands yolo; one who wants them from every launcher names the folder in
`host_path`. yolo runs no mise command at the host. Homebrew, npm's global prefix, pipx and
`~/.local/bin` put real binaries in a fixed folder, which a plain `host_path` string names for a
launcher that lacks it.

> [!WARNING]
> **A mise shim is found the way a shell finds it.** A check that finds `rg`'s shim reads
> *present* even when the version the cwd's mise config selects is not installed, and the agent's
> first `rg` then gets mise's own error or an install, as when the user types it. And **never run
> `mise env` or `mise activate` output**: it executes the user's mise config, including `[env]`,
> the directory-scoped credential channel [this reference refuses](#what-this-does-not-license).

**Everything else the launch inherits passes through.** `TERM`, `DISPLAY`, `WAYLAND_DISPLAY`,
`XDG_RUNTIME_DIR`, `DBUS_SESSION_BUS_ADDRESS`, `SSH_AUTH_SOCK`, the locale and `TZ` describe the
session the program was asked to run in, and yolo decides nothing from them. API keys, a region
and the variables the override check reads are decision inputs, and at the host they keep counting
from the shell that started yolo, beside what yolo composes, because the agent is handed that shell
and the check reads what the agent is handed ([HE-D10](#he-d10), reversible).

UNMEASURED: unit tests pin each call site, and no launch from a real bare-PATH launcher is
recorded since the build (2026-09-30).

## apply reports actions, check reports state

- **`apply` prints the `PATH` line when it created or changed the wrapper directory** — a
  completion notice about its own action ("I just wrote these wrappers; here is what makes them
  take effect"), which needs to know nothing about anyone's rc file or shell. It is silent on
  every apply that changed no wrappers, which after P5 means every apply that neither enabled
  the feature nor changed the selected pack set.
- **`yolo check` carries the `PATH` observation, every run**, because its job is "what is the
  state of my environment" and it is typically run from a fresh shell. A generated wrapper
  directory that nothing on `PATH` reaches is an inert-configuration warning, in the
  summary-counted channel — and its remedy **prepends**. Three more observations share that
  section and that channel, each a state `apply` cannot see:
  - **Precedence — on `PATH` is not winning.** Each wrapper's name is resolved the way a shell
    resolves it (first match wins, compared by file identity so a symlinked spelling of the
    directory still counts), and a wrapper something earlier on `PATH` shadows is a warning
    naming the binary that wins and the prepend line that fixes it. It is the
    [Prepend, not append](#the-wrapper-directory) rule, observed.
  - **Completeness.** A program a selected pack installs with no wrapper — a pack added since
    the last apply — is a warning naming it. Such a program never passes the
    [launch gate](host-apply-staleness.md#the-launch-gate) (the re-check `yolo host --` runs
    before exec'ing), so it is the one launch that never brings the host up to date by itself.
  - **The launch gate's reachability.** `host_apply_on_launch` being on is reported as working
    only when at least one wrapper wins on `PATH` and `host_management` is not `"none"`. With no
    wrapper, the directory off `PATH`, or every wrapper shadowed, no launch can reach the
    re-check, so the row that names that cause also says the key is on and cannot fire, rather
    than anything promising an automatic sync that cannot happen. Under `"none"` the gate does
    nothing even when a launch reaches it, since there is no render to re-check, so fixing
    `PATH` would not make the key fire. The `host_management` row says that, and no `PATH` row
    does.

  **One cause, one row** *(coined here, for [HE-D2](#he-d2))*: each warning in that section is
  one cause. Its headline names the cause, its note leads with the fix, and it then says what the
  cause breaks: a bare command running unwrapped, and the launch sync when that is the reason it
  cannot fire. No row points at another row for its fix. Two different causes are still two rows;
  a `host_management` of `"none"` and a directory off `PATH`, for example, have separate fixes.
  Under `"none"` a missing wrapper is folded into the `host_management` row, because the apply
  that would generate it is what `"none"` refuses. For a directory off `PATH` the section prints:

  ```text
  [WARN] wrapper directory is not on PATH, so no wrapper runs
       -> export PATH="/home/you/.local/share/yolo-jail/bin/wrap:$PATH"
          Add that line to your shell rc, below any line that puts ~/.local/bin on PATH, then open a new shell.
          Until then a bare claude or pi runs unwrapped, with no composed environment, though each wrapper works by absolute path.
          host_apply_on_launch is on but cannot fire, so no launch re-checks this host's render.
  ```

- **`apply` does not refuse.** It also writes the Channel 1 surfaces, which work regardless;
  refusing the half that works because the other half is unwired would be the wrong gate.

> [!WARNING]
> **`apply` must never condition on observing `PATH`.** "Already set" has two meanings and they
> disagree exactly when it matters. `apply` can only see *this process's* `PATH` — a fact about
> the shell that invoked it, not about the user's rc. False positive: the line is in the rc but
> yolo ran from a shell started before the edit, so it nags about something already done. False
> negative, the worse one: someone typed `export PATH=…` in one shell, yolo sees it present and
> says nothing, and every *new* shell has no wrappers. Reading rc files to disambiguate is not
> the fix — the line can live in any of several files or be built dynamically, and P3 says they
> are not yolo's territory. Conditioning on its own action removes the unreliable input
> entirely.

**The residual, named:** a user who enables `host_wrappers` and never pastes the line has a
working `yolo host --` and inert wrappers — not silently (`apply` said the line once, `check`
repeats it every run), but not working either. That is the cost of P3, and the only exit is the
line, pasted by the user. yolo offers no writer for it ([HE-D1](#he-d1)).

## What this does not license

- **Not a shell rc that exports agent variables.** One `PATH` entry, printed for the user to add,
  is the entire claim yolo makes on the user's shell, and yolo never writes it (P3,
  [HE-D1](#he-d1)).
- **Not per-agent channel selection.** A pack does not choose config-vs-env; the payload does
  (P1).
- **Not `mise` or `direnv` as the env channel.** Their coverage is a strict *subset* of the
  wrapper's (shell activation only, so they lose to the IDE too) while adding an external
  dependency; their env is **directory**-scoped where profile env is **agent**-scoped, so every
  process started from that directory would inherit the credentials. `mise` keeps the job it
  earns here: tool versions.
- **Not wrappers instead of config surfaces.** Wrappers are universal on payload and partial on
  invocation — the same partiality as config files, rotated 90°. Dropping Channel 1 trades a gap
  declarable at apply time for one the user discovers at runtime inside an IDE.
- **Not a second env fold.** Packs' `kind: "env"` contributions reach the host through the same
  fold the jail reduces (`packload.EnvFold`: each pack's static keys, then its profile-gated keys
  whose gate fires for the launched agent), and `hostfoldparity_test.go` pins the two notches to
  one winner per key.
- **Not a second credential gate.** Which `env_sources` values the host adds is the jail's own
  answer, `packload.ScopeCredentials` over this notch's one-agent table: a value a provider
  claims reaches the agent only when its profile selects that provider, and the launch says what
  it withheld ([`providers.md`](providers.md#the-credential-gate)). The shell the host notch
  inherits is the user's and passes through untouched. The one table is keyed by the launched
  command, so a typed `-p` makes an ad-hoc command a recipient; it is not an exemption from the
  gate, because the command receives only its profile's claimed values.

## Why it's this way

| Ruling | Why it holds |
| :--- | :--- |
| <a id="he-dir1"></a>[**HE-DIR1**](#he-dir1) — **at `yolo host`, yolo's checks read the PATH yolo was started with, when it has one, plus `host_path`, and no other folder** (maintainer, 2026-09-29: *"we can pick up the path if it's there because it's just not feasible to otherwise know these things … I just don't see any way around it. And then I think we just get things from [PATH]."*) | A fixed per-OS baseline replacing the caller's PATH was the design's own extension of [OQ-HE0](#oq-he0), never the maintainer's, and a folder yolo adds whenever it exists would be a source of yolo's own. A bare-PATH launcher getting a different answer from a terminal is the accepted cost, and the [miss line](#the-miss-line) is its fix. |
| <a id="oq-he0"></a>[**OQ-HE0**](#oq-he0) — `yolo host` depends on the environment it was launched in only where a `YOLO_*` variable or explicit config names the dependence (maintainer, 2026-09-25: *"`yolo host` should be as predictable an environment as possible"*); **revised for PATH by [HE-DIR1](#he-dir1)** | It still bars a folder or input of yolo's own guessing, such as appending mise's shims directory whenever it exists, and a new decision input needs config or a `YOLO_*` variable. The keys, region and override variables the shell exports keep counting, by [HE-D10](#he-d10) (reversible). |
| <a id="oq-he10"></a>[**OQ-HE10**](#oq-he10), ruled (c), 2026-09-29 — **the child's PATH is the launch PATH, then the floor's `bin/`**, and a bare name of a program a selected pack delivers execs from the floor by path | The commands a host agent runs see the user's own environment, and the floor's copy of a pack's agent runs from any launcher ([HP-DIR4](../design/host-tool-provisioning.md#HP-DIR4)). No baseline fills in for a bare launcher, because its contents were never ruled; one `host_path` line fixes the checks and the child at once. |
| <a id="oq-he11"></a>[**OQ-HE11**](#oq-he11), ruled (a), 2026-09-29 — **a selected pack's program the floor cannot hold runs from the child's PATH, and the launch says on one line why the floor has none** — **⚠ Superseded 2026-10-07 by [HNR-D2](../design/host-notch-readiness.md#HNR-D2), built 2026-10-08: a declared program with no floor entry refuses.** | It keeps `yolo host -- claude` working where the floor holds no copy of claude — a Mac before `yolo macos-setup`, a Linux machine with neither a container runtime nor Landlock (host-tool-provisioning.md [HP-D2](../design/host-tool-provisioning.md#HP-D2), [HP-D18](../design/host-tool-provisioning.md#HP-D18)) — departing from the floor rule only where the floor has nothing to run instead. |
| <a id="oq-he1"></a><a id="oq-he2"></a><a id="oq-he3"></a><a id="oq-he4"></a><a id="oq-he5"></a><a id="oq-he6"></a><a id="oq-he7"></a><a id="oq-he8"></a><a id="oq-he9"></a>**[OQ-HE1](#oq-he1) to [OQ-HE9](#oq-he9)** — retired or answered by [HE-DIR1](#he-dir1) (2026-09-29): an unset `host_path` is the caller's PATH alone, no pack declares a folder, `host_path` takes plain folders only, there is no `YOLO_HOST_PATH` and no "inherit PATH" entry, a jail launch's own lookups are unchanged, there is no macOS or other baseline, and nothing is staged; [OQ-HE6](#oq-he6) (API keys in the shell) is answered by [HE-D10](#he-d10), reversible | Each asked a question that only arose under the withdrawn reading of [OQ-HE0](#oq-he0). Their full text is in git history, in the host launch PATH design. |
| <a id="oq-hc1"></a>[**OQ-HC1**](#oq-hc1) — **the host runs the jail's derives over user-scope inputs**, in `yolo host apply` and in a wrapped launch's automatic apply, with no per-surface opt-in and no notch branch (maintainer, 2026-09-28: *"yes of course host apply and the auto one in a wrapper should generate this content. we're trying for host parity with the same handling."*) | The obstacle was the inputs, not the derives: only the MCP presets carried a jail path, so composing host inputs and checking the output for a jail path answers what a per-surface opt-in would have guarded. It superseded the earlier "for now" ruling that host apply renders no `openai-codex` list ([ML-D8](../design/model-lists-and-pickers.md#ML-D8)). |
| <a id="oq-hc2"></a>[**OQ-HC2**](#oq-hc2) — **under `own`, a `computed` surface renders through `stateful`**, adopting the file on the first owned render (2026-09-28, by [OQ-HC1](#oq-hc1)'s parity) | Without it those files have no host path once `assert` retires. |
| <a id="oq-hc3"></a>[**OQ-HC3**](#oq-hc3) — **host apply writes the `profile` key's selection with the jail's edge-triggered rule** (2026-09-28, by [OQ-HC1](#oq-hc1)'s parity) | So a direct or IDE launch of pi starts on the chosen provider and model, and a later `/model` pick of the user's still stands. Before it, the selection did nothing at the host for an agent that reads no yolo environment. |
| <a id="he-p1"></a>[**HE-P1**](#he-p1) — split by payload type, not by agent, and not as a preference order | The first design was a config-first ladder with process env as a fallback for the one agent that could do no better. A config file *routes* a credential and cannot *deliver* one, so the ladder had the load-bearing case backwards. |
| <a id="he-p2"></a>[**HE-P2**](#he-p2) — keep wrappers, as a three-line `exec` into `yolo host` | Consistency without a second env-composition implementation to drift. |
| <a id="oq-1"></a>[**OQ-1**](#oq-1) — copilot BYOK needs no per-agent advisory | Under P1 the need is a property of the provider, so copilot is the ordinary path rather than a special case; one statement covers every agent at once. |
| <a id="oq-2"></a>[**OQ-2**](#oq-2) — `yolo host -- <cmd>`, with `yolo --at host -- <cmd>` as the alias | The exec half needs a spelling that reads as a launch, and the dial keeps every notch equal. |
| <a id="oq-3"></a>[**OQ-3**](#oq-3) — `yolo host env` emits POSIX `export` by default, with JSON for tooling | Shell-specific emitters are not refused, just not built until asked for. |
| <a id="oq-4"></a>[**OQ-4**](#oq-4) — `apply` reports actions, `check` reports state, and `--shell-init` writes on request | An observation `apply` cannot make reliably must not gate what it prints; the actions-vs-state split is what the two commands are for. The `--shell-init` half is withdrawn by [HE-D1](#he-d1). |
| <a id="oq-5"></a>[**OQ-5**](#oq-5) — every host program a selected pack installs gets a wrapper, unconditionally | The wrap dir is an addressable launch surface; a path that exists on some machines and not others is not one. Overrules an earlier "only when the resolved env is non-empty" leaning. |
| <a id="oq-6"></a>[**OQ-6**](#oq-6) — the wrap dir is hardcoded under the existing host state root, not `XDG_DATA_HOME` | This repo follows the XDG *layout* and honors no XDG *variable* anywhere, so honoring one for a single new directory would make it the only path in the tree that moves when the variable is set. A cache dir would be worse: an evicted `PATH` entry is a silently broken `claude`. |
| <a id="oq-7"></a>[**OQ-7**](#oq-7) — `yolo apply --host` is REMOVED, not deprecated | Three spellings for one operation was the problem, and a deprecation message keeps the third spelling alive. Sweep prose with an allowlist, never with a blind substitution: docs that record what shipped *at the time* must keep the old spelling. |
| <a id="he-d2"></a>[**HE-D2**](#he-d2) — one cause, one row in `yolo check`'s host wrappers section (2026-09-27, maintainer ruling) | The ruling, verbatim, on a section printing two warnings for one wrapper directory missing from `PATH`: *"also why are ther emultiple wranings? this is very hard to read."* The second warning was the `host_apply_on_launch` row saying the sync cannot fire and pointing at "the rows below" for the fix. The key's state now rides on the row whose cause stops it, and that row still names everything the pair named: the cause, the fix line, the unwrapped bare command, the absolute-path fallback, and that the key is on. "One cause, one row" is the implementer's phrasing of the ruling and applies [report tiers' P1](report-tiers.md#principles), one fact once, to `yolo check`. The same audit applied it to two other `yolo check` findings that counted one cause twice: no container runtime answering, and a Claude OAuth broker missing both of the certificates `--init-ca` mints together. A stopped runtime used to be three rows: a `[WARN]` and a `[FAIL]` with the same start hint in the Container Runtime section, then a Merged Configuration `[FAIL]` saying no runtime was on `PATH`. A missing one was two, "No container runtime installed" and that same Merged Configuration `[FAIL]`. The Container Runtime section's `[FAIL]` is now the one finding, and Merged Configuration prints a dim line pointing back at it. A native runtime named on a host that cannot run it is a different cause and keeps its own `[FAIL]`. A review then found a third: a running jail whose host-services directory is gone printed a `[FAIL]` per loophole, with remedies that disagreed. It is one `[FAIL]` per jail now, naming the directory and the loopholes it takes down. |
| <a id="he-d1"></a>[**HE-D1**](#he-d1) — `yolo host apply --shell-init` is REMOVED (2026-09-27, maintainer ruling) | The ruling, verbatim: *"this shell init command apperas to do nothing, and I don't th8ink it's ever safe so we shoud reove it."* Both halves were measured before the removal. It did nothing on the spelling every remedy printed: bare `--shell-init` is a dry run, so it printed a "would append" line below the report's closing sentence and wrote nothing. It was unsafe on the spelling that wrote: it chose the rc file by guessing from `$SHELL` (`~/.zshrc` for zsh, `~/.bashrc` for anything else, `/bin/sh` included), and under `--assert` it appended even after the apply had refused and printed "Nothing was written." The flag now refuses with exit 2, writes nothing and prints the line. It refuses by name rather than as an unknown flag, the way `yolo host wrappers enable` does, because the people who type it are the ones a shipped message told to. |
| <a id="he-d13"></a>[**HE-D13**](#he-d13) — **a host apply refuses inside a jail** (2026-10-04, implementation decision under the maintainer's delegation, reversible; [DP-I7](../design/declaration-parity.md#DP-I7)) | An in-jail host apply re-rendered the jail's own home at the host notch (MEASURED: `defaultMode` rewritten to `default` under the agent using it). Nothing it writes there is right for the jail or reaches the host, so it stops above every stage, with no hatch. |
| <a id="he-d14"></a>[**HE-D14**](#he-d14) — **a host wrapper names the yolo that wrote it by absolute path** (2026-10-04, implementation decision under the maintainer's delegation ("make them and build it … adjust later"); reversible) | An IDE or desktop launcher starts a program with its own launcher's PATH, which no shell rc built, and the wrapper's `exec yolo` exited 127 there (MEASURED). The spelling is `hostwrap.Spelling` over the applier's `os.Executable()`: the first absolute PATH entry whose `yolo` is the same file, so a package manager's stable link (`/opt/homebrew/bin/yolo`) is baked rather than the versioned file an upgrade deletes (Linux's `os.Executable` resolves the link, darwin's does not), else the executable's own path, else bare `yolo` when the executable cannot be found. A wrapper naming another spelling of the same file is unchanged, not rewritten, and a wrapper an apply adds takes that spelling, so a gate apply from another PATH never rewrites the directory. A wrapper naming a yolo that is gone is rewritten by the next apply. The launchd PATH remedy is not offered: it needs sudo and claims PATH for every GUI app. ✅ Built 2026-10-04 (`TestAWrapperStartsYoloFromAPathWithoutYolo`, `TestHostApplyWrappersNameTheApplyingYoloByPath`, `TestHostApplyWrappersSpellTheRunningYoloThroughPath`) |
| <a id="he-d15"></a>[**HE-D15**](#he-d15) — **`yolo check` reports the yolo a wrapper names, and tells an IDE where to point** (2026-10-04, implementation decision under the maintainer's delegation; reversible) | One `[WARN]` names each wrapper whose yolo is gone, cannot run, or is named bare (a wrapper written before [HE-D14](#he-d14)), with `yolo host apply --assert` as the fix; an upgrade that moves yolo would otherwise make every wrapper exit 127 with nothing saying so. When no wrapper that wins can start yolo, that row is the cause [HE-D2](#he-d2) puts the `host_apply_on_launch` sentence on. The row that ends the section with wrappers on disk points an IDE or desktop launcher at a wrapper's absolute path, whatever PATH says: the PASS row as a dim line naming `<wrap dir>/<program>`, and the off-PATH and shadowed rows in their note. The pointer names only the wrappers that `[WARN]` does not, so the section never sends a launcher to a wrapper it says cannot start yolo; when it names every wrapper, no pointer prints until the apply has rewritten them. The pointer adds one line for each program it names whose folder host_path does not already name and that the floor has no entry for, giving the folder this PATH finds it in. That condition is the floor's disposition, not the OS: it is exactly when `yolo host` looks for the program on the launcher's PATH ([OQ-HE11](#oq-he11)), whatever the reason the floor has no entry — claude on macOS today, and also, for one more, a native agent on a Linux host that holds no usable capture of it and has no container runtime to make one. ⚠ **Narrowed 2026-10-08 by [HNR-D2](../design/host-notch-readiness.md#HNR-D2) and [HNR-D4](../design/host-notch-readiness.md#HNR-D4):** a launch now refuses a declared program the floor has no entry for, so no folder helps it, and the pointer's host_path line prints only for a program outside the floor (`hostfloor.Floor.OutsideTheFloor`: a pack the user-scope `host_floor` leaves out, one the `provisioners` order gives to a manager, or one with no published build for the platform), the only kind `yolo host` still looks up on the launcher's PATH. Claude on macOS gets the line only when `host_floor` leaves the claude pack out. ✅ Built 2026-10-04 (`TestHostWrappersWarnsWhenAWrapperNamesAYoloThatIsGone`, `TestHostWrappersPointsNoLauncherAtAWrapperThatCannotStartYolo`, `TestHostWrappersOffPathAndShadowedRowsSayALauncherNeedsHostPath`) |

## Current values

Verified at `40915b60`, and the opt-in and verb rows re-verified 2026-09-22. The prose above
explains what each of these is for; this table is the only place the values themselves are stated.

| Value | Setting | Defined in |
| :--- | :--- | :--- |
| Opt-in key | `host_wrappers` (boolean, user scope) — **unset DERIVES from `host_management`: on at `"own"`, off otherwise.** An explicit `false` wins; the unset key (`"none"`) does not derive | `config.HostWrappersEnabled`, documented by `yolo config-ref` |
| Host wrapper dir | `<host state>/bin/wrap` | `paths.WrapDir`, `paths.WrapDirUnder` |
| Generated-bin parent | `<host state>/bin` | `paths.GeneratedBinDir` |
| Jail blocker dir (first on PATH) | `~/.yolo/bin/block` | `entrypoint.BootPath` |
| Jail launcher dir (second on PATH, ahead of every install prefix) | `~/.yolo/bin/launch` | `entrypoint.BootPath` |
| Host blocker dir (first on a `yolo host --` child's PATH, when anything is blocked) | `<host state>/bin/block/<digest of the scripts>`, written once and reused | `paths.HostBlockDir`; `cli.composeHostBlockers` |
| Wrapper body | `exec <yolo> host -- <bin> "$@"`, with a generated-by header; `<yolo>` is the applying yolo's absolute path, spelled as [HE-D14](#he-d14) rules | `hostwrap.BodyFor`, `hostwrap.Spelling`, `cli.hostWrapperYolo` |
| Wrapper set | every valid bin name a selected pack's `program` contributions install | `hostwrap.Bins` over `Pack.HonoredInstalls` |
| `yolo host` verbs | `apply`, `env`, `wrappers` (**`status` only** — `enable`/`disable` were deleted 2026-09-22 and now refuse), and the `--` exec half | `internal/cli` (`hostMain`) |
| `yolo host env` formats | `export` (default), `json` | `internal/cli` (`hostEnv`) |
| Profile flag | `--profile <name>` / `-p <name>`, keying the launched command by its basename, agent or not | `internal/cli` (`parseHostExecFlags`, `effectiveHostProfiles`) |
| rc line writer | **none.** `yolo host apply --shell-init` was removed 2026-09-27 and refuses with exit 2, printing the line ([HE-D1](#he-d1)) | `internal/cli` (`refuseShellInit`) |

<a id="host-pi-open"></a>

## Open: host pi with auto mode, waiting on the maintainer

**Open, and not a ruling this doc owes.** It moved here intact on 2026-10-16 from the design that
graduated into [what host apply renders](#what-yolo-host-apply-renders-into-a-derived-surface),
where it was section 8; the ledger it cites is [the host computed-layer ledger](#the-host-computed-layer-ledger).
It closes once the maintainer's debugging session settles it.

The maintainer reported that host pi with `@czottmann/pi-automode` "didn't even launch correctly".
Nothing below reproduces that: the host's state is unknown to us, and **the maintainer will debug
it with us**. What follows is what the tree does, split by how we know it.

<a id="host-pi-measured"></a>

### Measured

1. **`models.json` becomes `{}`, and pi shows an error at every start.** A host
   `yolo host apply --assert` into a home with no `~/.pi/agent/models.json` created it as `{}`.
   pi 0.87.1's `ModelsConfigSchema` requires `providers` (`dist/core/model-config.js`), its loader
   returned "Invalid models.json schema: providers: must have required properties providers",
   interactive mode prints it as `models.json error: …`, and pi continues with no custom providers.
   `{"providers": {}}` loads cleanly. **It happens in jails too**: a boot whose only agent pack is pi
   writes `{}` (re-run 2026-09-27), because since `92c20cc6`, in no release yet, pi's catalog never
   writes `openai-codex`, which leaves a pi-only jail no row. [HC-D1](#HC-D1) is the fix, built in
   `packs/pi/pack.json` and pinned by `internal/entrypoint/pimodelsproviders_test.go`: both now
   write `{"providers": {}}`, and the next boot or `--assert` repairs a `{}` an earlier one left.
   A `yolo host apply --revert --assert` removed that default again and left `{}`; since
   [HC-D12](#HC-D12) a revert keeps an empty declared default and names it
   (`entrypoint.keptShapeDefault`, pinned by `TestARevertKeepsPiModelsProviders`). Under `own` the
   surface is still refused, so nothing repairs it there ([OQ-HC2](#oq-hc2)).
2. **The report hides that write.** The dry run said `pi/models` was unchanged, and the assert
   counted it among the destinations already in sync, while the file was created. Only `--verbose`
   showed `pi/models rendered`. Since [HC-D4](#HC-D4) a file the apply creates is a change in both
   postures (`entrypoint.hostSurfaceWouldChange`, pinned by
   `TestAHostApplyCountsAFileItCreatesAsAChange`), and a later fix makes that include one created
   through a dangling link (`TestAHostApplyThroughADanglingLinkReportsItAsAChange`).
3. **A direct or IDE launch cannot refresh the `openai-codex` login.** The host-delivered
   `yolo-openai-auth.js`, loaded under node with no yolo environment and yolo on `PATH`, failed its
   refresh with "OpenAI credential service: openai-auth-client:
   YOLO_SERVICE_OPENAI_AUTH_BROKER_ENDPOINT is not set". Only `yolo host --` sets the host socket
   variable (`openaiauthhost.prepare`), and the error named the jail's variable instead of saying
   to launch through `yolo host`. Since the fix, with no route and outside a jail, it says pi
   was not started through `yolo host` and to launch it with `yolo host -- pi`, the client's words
   after it (`brokerFailure` in `yolo-openai-auth.js`, pinned by
   `TestPiOpenAIAuthOutsideAJailSaysToLaunchThroughYoloHost`). The extension counts either
   `YOLO_VERSION` or a `~/.yolo/bin` directory as a jail, so it cannot give that advice inside
   one, even with a scrubbed environment. MEASURED through pi
   0.87.1's own `discoverAndLoadExtensions` in a throwaway home, with no session and no network:
   a host's login and refresh both name `yolo host -- pi`, and with `YOLO_VERSION` set both keep
   the client's message.
4. **`use_profiles` changes nothing for host pi.** With `use_profiles: {"pi": "codex"}`, host
   `settings.json` got no `defaultProvider` or `defaultModel`, and `yolo host env --agent pi`
   composed only the two `YOLO_AUTH_PRELAUNCH_PI_*` variables the in-jail launcher reads. pi
   registers no `yolo.env` ([OQ-HC3](#oq-hc3)).
5. **Under `own`, pi's three `computed` surfaces are refused**, with text naming `assert` as the
   remedy, and the `pi` wrapper `own` writes is `exec yolo host -- pi "$@"`.
6. **Two things that are not the cause.** A pack with two `autonomy` contributions gets a clear
   refusal naming the fix. And the adapter refusal (ES-D18) cannot reach pi, since every shipped
   adapter targets `anthropic`; the credential gate refuses only a missing key, and names its hatch.

<a id="host-pi-read-from-source"></a>

### Read from source, not run

1. **`yolo host -- pi` needs an OpenAI login before pi starts**, for any pi invocation, `pi
   --version` included, whatever provider pi then uses. `openaiauthhost.prepare` gates on the
   program's basename being `codex` or `pi`, starts the broker singleton, asks its status and, when
   not logged in, requests a browser login, on port 1455 or on 1457 when 1455 is taken, that waits
   up to 15 minutes. Its failures exit 1 with "yolo host: prepare shared OpenAI authentication:
   …". Every wrapped `pi` takes this path. It has shipped since `6d118252`, in v0.9.0. Not run end to end, because that
   would reach this jail's live broker.
2. **pi installs a missing `npm:` package at startup, with no catch on that path**
   (`package-manager.js`'s `resolve` → `installParsedSource`, pi 0.87.1). So with npm or the
   registry unavailable on the host, the first pi start after the posture list lands fails
   outright. pi-automode 1.17.0 has no install scripts and one dependency.
3. **pi-automode with no classifier model configured uses the session model**
   (`resolveClassifier` in its `classifier.ts`), so it blocks only when there is no session model or
   that provider fails, which includes item 3 of [the measured findings](#host-pi-measured).

The last two correct two rows of
[the posture-list failure paths](pack-system.md#posture-list-failure-paths), which now say so.

### Suspected, and what would tell them apart

| Candidate | What you would see |
| :--- | :--- |
| [the measured findings](#host-pi-measured) item 1 | pi starts, with a red `models.json error` line |
| [the findings read from source](#host-pi-read-from-source) item 1 | `yolo host -- pi` or a wrapped `pi` opens a browser or exits with "prepare shared OpenAI authentication" before pi prints anything |
| [the findings read from source](#host-pi-read-from-source) item 2 | pi exits during startup with an npm error |
| [the measured findings](#host-pi-measured) item 3 with pi-automode | pi starts, and tool calls are blocked after a refresh failure |
| A host yolo older than the posture lists (`bbe5c878`), and so older than host verbs refusing a manifest with problems (`d4aa6a43`) | `yolo host apply --assert` and `yolo host --` exit 0 and read the pack declaring the posture list as an empty manifest, so pi's `packages` lacks pi-automode and nothing says why. Only `yolo check`, `yolo pack lint` and a jail launch refuse the manifest by name ([the posture-list failure paths](pack-system.md#posture-list-failure-paths)) |

What would settle it: the first screen pi printed; `yolo host apply --verbose`;
`cat ~/.pi/agent/models.json`; `yolo openai-auth status`; `yolo --version`; your
`host_management` value; and whether pi started through `yolo host`, a wrapper, an IDE or directly.
Until then no candidate gets a question here: the `models.json` one is fixed by [HC-D1](#HC-D1),
the login before exec belongs to [`agent-credentials.md`'s OpenAI service](agent-credentials.md#the-openai-subscription-credential-service), and the npm
install and the classifier belong to
[pack-system.md, posture lists](pack-system.md#posture-lists).

## Implementation-decision ledgers

The two designs this doc graduated from left their build records behind as stubs; on 2026-10-16
both ledgers moved here whole and the stubs were deleted (their argument is in git history:
`git log --follow -- docs/design/host-launch-environment.md` and
`git log --follow -- docs/design/host-computed-layer.md`). A row's wording is the design's; only
its links were repointed at this doc.

<a id="the-host-launch-path-ledger"></a>

### The host launch PATH ledger

The build record of [the launch PATH section](#the-launch-path-and-which-copy-of-a-program-runs).
**Two ids are renumbered, because this doc already had its own `HE-D1` and `HE-D2`:**

| The design's id | Its id here | What it decides |
| :--- | :--- | :--- |
| `HE-D1` | [HE-D16](#he-d16) | the floor's `bin/` goes last in the child's PATH |
| `HE-D2` | [HE-D17](#he-d17) | a lookup that misses prints the miss line |
| `HE-D3` to `HE-D12` | unchanged, [HE-D3](#he-d3) to [HE-D12](#he-d12) | as in each row below |

This doc's own [HE-D1](#he-d1) (`--shell-init` removed) and [HE-D2](#he-d2) (one cause, one row)
are unchanged, in [Why it's this way](#why-its-this-way). A comment or doc written before
2026-10-16 that cites `HE-D1` for the floor's place on the child's PATH, or `HE-D2` for the miss
line, means HE-D16 or HE-D17.

| Id | Date | Decision | Why it holds | Built |
| :--- | :--- | :--- | :--- | :--- |
| Id | Date | Decision | Why it holds | Built |
| :--- | :--- | :--- | :--- | :--- |
| <a id="he-d16"></a>**HE-D16** (the design's HE-D1) | 2026-09-29 | *Implementation decision under [OQ-HE10](#oq-he10):* the floor's `bin/` goes last in the child's PATH, after the launch PATH | HP-DIR4 puts the floor after the user's PATH. The floor holds only agent names, so last means a child's lookup of an agent reaches the floor only where nothing of the user's has one | **Built.** `hostChildPath`, the floor's `bin/` last |
| <a id="he-d17"></a>**HE-D17** (the design's HE-D2) | 2026-09-29 | *Implementation decision under HE-DIR1:* a PATH check or target lookup that misses prints the miss line: the program, the whole launch PATH searched (its `host_path` part marked), and the `host_path` fix, naming the folder when a hint location holds the program ([the miss line](#the-miss-line)). **The hint is found by a bounded look, and is a hint only, never a verdict:** one `stat` per folder of the compiled hint-location list, no recursion and no command run, for an executable of that name in a folder not already on the launch PATH. No check counts a program found there, and nothing runs it. Where a launch prints the line (the gate, the exec) it says *this launch's PATH*; where a verb only checks (`yolo host apply`, `check-deps`, `yolo check`) it says *the PATH yolo searched*, and so does the gate of a launch started with no PATH, whose child searches more ([HE-D4](#he-d4)). It names yolo's own folders the lookup skipped. It prints once per missing program per run: the gate leaves the program the launch starts to the exec's own line, and prints the others whether or not the home's render is in sync | HE-DIR1 accepts that a bare launcher can miss what a terminal finds. The line makes that miss self-explaining from the launcher's own output, and a hint location changes only its text. A miss in an in-sync home is still a miss ([the miss line](#the-miss-line): it prints on every miss), and a line printed twice for one program is noise | **Built.** `hostpath.Launch.MissLine`, with the hint from `hostpath.Launch.Hint` over `hostfloor.HintLocations`. Printed by the gate (`reportGateMisses`), under `yolo host apply`'s dependency blocker and as its JSON document's `miss` (`depBlockerGroups`), under `check-deps`' MISSING line, as the note of `yolo check`'s warning, and in place of the exec's lookup error (`resolveHostLaunchTarget`). `TestEveryHostCheckReadsHostPath` (the gate's in-sync branch among them), `TestHostExecLaunchesOverAnotherPacksMissingDependency` (the gate's decision branch), `TestTheExecReadsHostPathAndMissesWithTheMissLine`, `TestTheMissLineNamesTheProgramThePackThePathAndTheFix`, `TestHintIsAHintNeverAFolderOnThePath`. A selected pack's program whose vendor publishes no build for this host is no blocker, since nothing could install it, but it was looked up on the launch PATH and `yolo host -- <it>` runs what that PATH holds ([OQ-HE11](#oq-he11) (a)), so it gets the line too: beside `check-deps`' no-build line, above `yolo host apply`'s verdict in the default view (`printUnpublishedDepMisses`), from the gate, and as the note of `yolo check`'s no-build row (`TestAProgramWithNoBuildHereStillPrintsTheMissLine`, `TestCheckHostLaunchPathGivesAProgramWithNoBuildHereTheMissLine`). `yolo host apply --format json` carries a miss line only in a missing dependency's group. Each `host_path` entry the reader refused is named in the line with its fix (`hostpath.Launch.Refused`, from `config.HostPathRefusals`; `TestARefusedHostPathEntryIsNamedInTheMissLine`, `TestTheMissLineNamesEachRefusedHostPathEntry`), and in `yolo check`'s host launch section (`TestCheckHostLaunchPathNamesARefusedHostPathEntry`) |
| <a id="he-d3"></a>**HE-D3** | 2026-09-29 | *Implementation decision under HE-DIR1:* `host_path`'s folders go after the ambient PATH, in written order, duplicates keeping the first occurrence | HE-DIR1: `host_path` *adds* folders. After the ambient PATH, a folder fills in for a launcher that lacks it and never shadows what the caller's PATH already finds, the same order [OQ-HE10](#oq-he10) (c) ruled for the child | **Built.** `hostpath.New` (`TestTheLaunchPathIsTheAmbientPathThenHostPathsNewFolders`), and the child's PATH the same way (`hostChildPath`) |
| <a id="he-d4"></a>**HE-D4** | 2026-09-29 | *Implementation decision under HE-DIR1:* when yolo was started with no PATH, or an empty one, the launch PATH is `host_path`'s folders alone, and the miss line says yolo was started with no PATH. **The cost, stated:** the child keeps the floor design's stand-in ([HP-D12](../design/host-tool-provisioning.md#HP-D12)), so from such a launch a check can read missing a tool the agent then finds in a system folder; the gate's miss line therefore says *the PATH yolo searched*, not *this launch's* | HE-DIR1's "when it has one", read literally; supplying a default PATH would be a guess | **Built** for the checks (`hostpath.New`; `TestWithNoPathTheChecksSearchHostPathAlone`), and the gate's wording (`TestWithNoPathTheGateDoesNotCallHostPathAloneThisLaunchsPath`). The child's PATH and the exec's lookup keep HP-D12's stand-in (`hostpath.Launch.Child`), and the exec's miss line names it ([HE-D9](#he-d9)) |
| <a id="he-d5"></a>**HE-D5** | 2026-09-29 | *Implementation decision:* one resolver computes the launch PATH once per process; every host PATH check goes through its lookup, which skips `yoloManagedDirs()` as the exec's does | Two readers of PATH can disagree, and a check that did not skip the wrap dir would read a wrapper as the program it wraps | **Built.** `hostpath.Resolve`, asked by `probeHostDeps` once per run (the gate's survey and `yolo host apply`, which record it for the install and the miss lines), by `hostExec` once per launch, by `checkDepsMain` and by `yolo check`. It is computed from its two inputs each time it is asked rather than cached; neither changes in a process's life, so every check reads one value. Its `LookPath` skips `hostpath.ManagedDirs`, which `yoloManagedDirs` returns. `depcheck.Check`, `depcheck.Present` and the package-manager guess take the lookup from the caller. `TestEveryHostCheckReadsHostPath`, `TestTheManagerGuessReadsHostPath`, `TestAWrapperNeverReadsAsItsProgram`, `TestEveryProbeReadsTheCallersLookup`. Exempt by name ([one resolver](#one-resolver)): the wrapper-precedence checks and the floor's capture-runtime probe (`CaptureUnavailable`, `captureRuntime`), which read the ambient PATH alone because the question each answers is the caller's shell's or a jail launch's |
| <a id="he-d6"></a>**HE-D6** | 2026-09-29 | *Implementation decision:* an install the dependency gate runs gets `PATH` set to the launch PATH, and the re-probe reads the same value | The re-probe then asks the PATH the installer ran with, so an install that lands in a `host_path` folder is found | **Built.** `depInstallEnviron`; the seam is `depInstallRun(cmd, env, out)`, and the re-probe is `depcheck.Present(bin, lp.LookPath)` (`TestTheInstallRunsWithTheLaunchPathAndTheReprobeReadsIt`) |
| <a id="he-d7"></a>**HE-D7** | 2026-09-29 | *Implementation decision:* `yolo check`'s host launch section reads the PATH of the shell it runs in, plus `host_path`, and says so in the section | A check run from a terminal cannot see a widget's PATH, and a green section must not read as a promise about every launcher | **Built.** `sectionHostLaunchPath` (`TestCheckHostLaunchPathSaysWhichPathItRead`); the floor section reads the same launch PATH, for the copy that runs where it has no floor entry (`TestCheckFloorSectionFindsANoFloorEntryProgramInHostPath`) and for the other copies it names as not run (`TestCheckFloorSectionNamesACopyInHostPathAsNotRun`) |
| <a id="he-d8"></a>**HE-D8** | 2026-09-29 | *Implementation decision under HE-DIR1:* no staging and no migration notice for the PATH half. The design ships whole | With `host_path` unset, every check of a program no floor entry covers reads the PATH it reads today, so no PATH verdict breaks that a notice would have warned about. The report-first stages existed only for the withdrawn switch. The floor half does change what runs: a delivered agent runs from the floor, and its first install can fail offline (the design's staging section). That change is [HP-DIR4](../design/host-tool-provisioning.md#HP-DIR4)'s, and [HP-D3](../design/host-tool-provisioning.md#HP-D3) discloses the install on the launch line | **Built** by staging nothing: no notice, and no stages |
| <a id="he-d9"></a>**HE-D9** | 2026-09-29 | *Implementation decision under HE-DIR1 and [OQ-HE10](#oq-he10) (c):* the child's PATH has no per-OS baseline either. It is the launch PATH, then the floor's `bin/`. **The cost, stated:** (c) as asked said the baseline "fills in for a launcher whose PATH lacks it (Waybar)". A bare launcher's child now gets only what its launcher handed yolo, plus `host_path`'s folders. For example, a Mac hotkey launcher's agent no longer finds `/opt/homebrew/bin` unless `host_path` names it | One PATH for the checks and the child ([which copy runs](#which-copy-runs)), so, for a caller with a PATH, a check never reads missing a tool the agent then finds, or the reverse. A caller with no PATH is the exception the Built cell names: there the checks search `host_path` alone ([HE-D4](#he-d4)) and the child searches more. The baseline's contents were [OQ-HE1](#oq-he1)'s and [OQ-HE8](#oq-he8)'s open questions and never ruled, so (c) named a list nobody had decided. One `host_path` line fixes the checks and the child at once, and every delivered agent runs from the floor whatever the launcher's PATH holds ([HP-DIR4](../design/host-tool-provisioning.md#HP-DIR4)). Reversible: a baseline for the child alone would reopen [OQ-HE1](#oq-he1) and [OQ-HE8](#oq-he8) for the child | **Built** for a caller with a PATH (`hostChildPath`). A caller with no PATH at all keeps [HP-D12](../design/host-tool-provisioning.md#HP-D12)'s stand-in, the floor installer's system folders ahead of `host_path`'s: a floor decision built before this doc was restated, which this build left as it was |
| <a id="he-d10"></a>**HE-D10** | 2026-09-29 | *Implementation decision, reversible, answering [OQ-HE6](#oq-he6):* at `yolo host`, the shell that started yolo keeps counting for the three checks that read it today (the credential pre-flight, the region pre-flight and the [OQ-SSO8](agent-credentials.md#sso-decision-ledger) override check) and for the credential gate's delivery. Nothing is built. A refusal names where yolo looked, the launch environment among them | The check reads what the delivery reads (`checkProviderCredentials`: *"the check and the delivery cannot disagree"*), and at the host the delivery is the inherited shell plus what yolo composes ([CN-D13](providers.md#CN-D13), [BR-D2](../design/bedrock-plumbing.md#BR-D2)). Stopping only the check refuses launches the delivery serves. Stopping the delivery too drops `ANTHROPIC_AUTH_TOKEN` for `yolo host -p zai -- claude` when `ZAI_API_KEY` is only in the shell. [OQ-HE0](#oq-he0) was said about PATH, so reading it as covering keys is the widening HE-DIR1 corrected (the recorder's reading) | Nothing to build: today's behavior |
| <a id="he-d11"></a>**HE-D11** | 2026-10-04 | *Implementation decision, taken under the maintainer's 2026-10-04 delegation ("make them and build it … adjust later"); reversible.* **`yolo host -- <program>` applies blocked tools.** The selected packs' `blocked-tool` contributions (the guardrails pack's `grep` and `find`) and the user scope's `security.blocked_tools`, never a workspace's, are rendered by the jail's own writer into a directory under `~/.local/share/yolo-jail/bin/block/` named by its content. That directory goes **first** on the child's PATH, and any other `bin/block/` entry an outer launch handed down is taken out. With nothing to block, the child's PATH is [OQ-HE10](#oq-he10)'s exactly. A `replacement` is checked against the child's PATH, and the real `grep` and `find` the shims pass calls on to are found there by absolute path; a tool with neither is left unblocked, with a line saying so. Every launch that blocks says what it blocks and who asked, the `YOLO_BYPASS_SHIMS=1` hatch, and the two ways round a block a launch cannot close: a shell that resets PATH (a login shell on macOS runs `path_helper`, and Debian's `/etc/profile` sets PATH outright) and an agent's own shell function of the same name. When the environment the launch hands its program already carries `YOLO_BYPASS_SHIMS` (exported in the invoking shell, which passes through), the disclosure says every block lets its command through, rather than claiming blocks that do nothing. `yolo host apply` blocks nothing, since it starts no process. `intercept` stays refused at the host, now for [BB-D17](../design/boundary-broker.md#BB-D17)'s reason: the agent runs as the user and can run the real program by its path | The census refused the kind because "off-container yolo owns no PATH entry to put one in", and [HE-D16](#he-d16) made that false for this verb: yolo composes the child's PATH. **The interaction with the rulings, stated:** [OQ-HE10](#oq-he10) ruled the child's PATH as the launch PATH, then the floor's `bin/`; this adds one entry ahead of both, only when something is blocked, and moves none of the ruled entries, the same position a jail gives `~/.yolo/bin/block`. [HP-DIR2](../design/host-tool-provisioning.md#HP-DIR2)'s "we construct an environment. We do not sniff the command line" holds: the block is part of the environment, and yolo still never inspects the command. The workspace config is not read, for `UserScopeConfig`'s reason: a cloned repository does not choose what the user's own tools do outside every sandbox. **Measured in a jail, 2026-10-04:** Claude Code 2.1.289's Bash tool defines `grep` and `find` as shell functions that run its bundled search tools, so Claude's own commands skip the guardrails blocks in a jail as at the host; the disclosure names it | **Built.** `hostBlockedChildPath` (`internal/cli/hostblockers.go`), called in `hostLaunch` once the target has resolved; `entrypoint.RenderBlockers` is the one writer at both notches (`TestTheJailAndAHostLaunchRenderOneBlockerScript`). `TestHostLaunchPutsTheGuardrailsBlockersFirstOnTheChildsPath` and `TestHostLaunchBlocksAcrossARealExec` pin the call site. `TestHostLaunchSaysWhenTheShellAlreadyCarriesTheHatch` pins the inherited hatch's line, `TestHostLaunchWithNothingBlockedSaysNothingOfTheHatch` that the line comes only with a block, and `TestHostLaunchThatCannotWriteItsBlockDirBlocksNothing` a block dir that cannot be written (nothing blocked, the PATH unchanged). `TestHostLaunchFindsEveryBlockersProgramsOnTheHostPathFolders` pins that the replacement, the real `grep` and `find` and the program behind each block are read from the child's PATH, a `host_path` folder included. The census: `render.HostFields` honors `blocked-tool`, `render.HostAtLaunch` names it as delivered *at launch only* ([the report vocabulary](report-tiers.md#the-report-vocabulary)), and `security` is `KeyHonored`. UNMEASURED: a Mac login shell's `path_helper` dropping the block dir, as the disclosure says it can |
| <a id="he-d12"></a>**HE-D12** | 2026-10-04 | *Implementation decision, taken under the maintainer's 2026-10-04 delegation ("make them and build it … adjust later"); reversible.* **At the host, `YOLO_BYPASS_SHIMS=1` runs the real program behind every blocked name**, not only behind `grep` and `find`. The launch finds it the way [HE-D11](#he-d11) finds the real `grep`: on the child's PATH, skipping yolo's own folders. The shim `exec`s it past the refusal. With nothing found, the shim says no such program is installed behind the block and exits 127, which is the intercept shim's answer for the same case. Nothing else about an entry changes: a user's own entry is still an unconditional block, and its `block_flags` and `allow_flags` read as they did | [HE-D11](#he-d11)'s disclosure says the hatch lets a command through, as a jail's [`config-ref`](../../internal/cli/config_ref.txt) does. But the shared writer gave a real binary only to `grep` and `find`, so under the hatch any other blocked name skipped its refusal, ran nothing and exited 0, and `YOLO_BYPASS_SHIMS=1 curl …` in an agent's script "succeeded" with nothing fetched. **The jail has the same defect, older than this ledger, and it stays**: its boot passes no resolver, so a custom entry's hatch there still runs nothing. `shims_behavior_test.go`'s `unconditional_block_no_fallthrough` pins that script (no `exec` in a `curl` shim), and that file is outside this build. The jail fix is one resolver in `generateBlockers`: `lookPathIn` over the agent's PATH less the block dir, as `GenerateIntercepts` already does. It must leave a custom entry's `block_flags` alone; they are ignored today, and honoring them would loosen a block | **Built** at the host. `BlockerRender.Behind` (`internal/entrypoint/shims.go`) is set by `composeHostBlockers` (`internal/cli/hostblockers.go`). `TestHostLaunchsHatchRunsTheProgramBehindEveryBlock` pins the call site and `TestABlockerRunsWhatIsBehindItUnderTheHatch` the writer. The jail half is not built |

<a id="the-host-computed-layer-ledger"></a>

### The host computed-layer ledger

The build record of [what host apply renders](#what-yolo-host-apply-renders-into-a-derived-surface).
**For your review:** [HC-D26](#HC-D26)'s second half, which leaves a fetched pack's MCP server out
at the host ([MP-D4](../design/mcp-presets-removal.md#MP-D4)).

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| <a id="HC-D1"></a>HC-D1 | *Implementation decision.* **`pi/models` declares `"defaults": {"providers": {}}`.** pi 0.87.1 rejects a `models.json` without `providers`, and both a host apply into a fresh home and a pi-only jail wrote `{}` before it ([the host pi findings](#host-pi-measured) item 1). With the default, both render `{"providers": {}}`, and the `-short` suites of `internal/entrypoint`, `internal/cli`, `internal/packload` and `packs` still pass (MEASURED by the research pass in a scratch copy). A derive's rows replace it, since the catalog is declared in full; under `rmw` a default fills only an absent key, so an existing host file keeps its own `providers`. A regression test asserts the rendered file has an object-valued `providers` at both notches | 2026-09-27 | the design's fixes that waited on no ruling | ✅ `packs/pi/pack.json`, pinned by `internal/entrypoint/pimodelsproviders_test.go`; kept through `--revert` since [HC-D12](#HC-D12), `TestARevertKeepsPiModelsProviders` |
| <a id="HC-D2"></a>HC-D2 | *Implementation decision.* **Host apply's MCP remedy names a `config-overlay` per surface,** for example in the local pack, because that is the only declaration that reaches a host MCP table today ([OQ-HC1](#oq-hc1) item 2). Once a host-derivable surface consumes `mcp_servers`, the remedy names `mcp_servers` for that surface again. **Superseded 2026-09-28 by [HC-D20](#HC-D20)**: the host now runs every derive over the user's tables, so the remedy names `mcp_servers`, `lsp_servers` and `providers` first, and this row's per-surface `config-overlay` is the one-agent alternative | 2026-09-27 | the design's fixes that waited on no ruling | ✅ `mcpEntryRemedy`, pinned by `TestFollowingTheHostMCPRemedyKeepsTheEntry`; the example and keys from the tables that lost entries, `droppedTablesOf`, pinned by `TestTheDroppedEntryRemedyNamesTheTableThatLostTheEntry`; the [user guide](../../userguide/guides/migrating-to-packs.md#step-2-apply) and the [remedy contract](report-tiers.md#the-remedy-contract) |
| <a id="HC-D3"></a>HC-D3 | *Implementation decision.* **`config-ref`'s host-notch `provider` line says host-rendered files carry provider facts** and render without them because host apply composes no provider table; it changes again with [OQ-HC1](#oq-hc1) | 2026-09-27 | the design's fixes that waited on no ruling | ✅ `7d12abb0` |
| <a id="HC-D4"></a>HC-D4 | *Implementation decision.* **A host apply that creates a file reports it as rendered,** in the dry run and the assert, never as unchanged or in sync ([the host pi findings](#host-pi-measured) item 2) | 2026-09-27 | the design's fixes that waited on no ruling | ✅ `entrypoint.hostSurfaceWouldChange`, pinned by `TestAHostApplyCountsAFileItCreatesAsAChange`; through a dangling link, `TestAHostApplyThroughADanglingLinkReportsItAsAChange` |
| <a id="HC-D5"></a>HC-D5 | *Implementation decision.* **Under `own`, a loss line names only what the `stateful` write drops.** MEASURED: a first owned apply kept a hand-added `mcp_servers` entry in `codex/config` and `mcp` entry in `opencode/config` while the report named both as dropped ([the computed layer at the host](#the-computed-layer-at-the-host)). The loss list is computed by the mechanism that writes. The same change makes `own`'s `codex/config` header stop saying "composed at jail start" at the host | 2026-09-27 | the design's fixes that waited on no ruling | ✅ `entrypoint.hostMechanismTableLosses` and `render.Target.GeneratedHeader`, pinned by `TestAnOwnedHostApplyDoesNotReportAnEntryItKeeps` and `TestAnOwnedHostRenderNamesTheHostApplyInItsBanner`; a table of three or more entries is cleared whole, under both contracts and in a jail, by `entrypoint.regenerateManagedTables`, pinned by `TestAHostTableWriteKeepsOnlyTheConfiguredEntry` and `TestABootDropsEveryHandAddedServerFromClaudeJSON`; the JSON no-banner test reads the written files, `TestJSONSurfaceGetsNoGeneratedHeader` |
| <a id="HC-D6"></a>HC-D6 | *Implementation decision*, if [OQ-HC1](#oq-hc1) is not A. **The host `mcp_servers` input is your user-scope entries, with no preset expansion and no pack `mcp` declaration, and it is filtered per surface agent, as a jail filters it.** A pack's declaration stays out because [OQ-MP3](../design/mcp-presets-removal.md#15-open-questions) ruled it "reaches only the jail". Presets stay out because their node wrapper is written only by a jail's boot and their npm prefix is the jail's; the host can state those paths ([OQ-MP4](../design/mcp-presets-removal.md#decision-ledger) dissolved the premise that it cannot), but no real home has what they name, and [`mcp-presets-removal.md`](../design/mcp-presets-removal.md) retires them. A user entry, MCP or LSP, whose command or arguments name a jail path is omitted and named, since writing it would break [what host apply renders](#what-yolo-host-apply-renders-into-a-derived-surface)'s rule against jail-absolute values; which prefixes count is the implementer's list, and it includes the jail home, `/workspace`, `/opt/yolo-jail`, `/run/yolo` and `/ctx`. **Per surface agent:** `requires_env` is checked against the environment `yolo host env --agent <agent>` would compose at apply time, since that composition scopes a provider credential to the agent that selected it ([OQ-CN6](providers.md#oq-cn6)); and the capability filter runs with that agent's native capabilities (`packload.NativeCapabilities`), so a server whose `provides` the agent's own login performs is withheld from it. Without the capabilities the filter keeps every server, and a server a jail withholds from claude or agy would be written for them at the host, where the same config should render the same entries. A skipped server is named with its variables and with the agents that did get it, as the jail's notice names it | 2026-09-27 | [the computed layer at the host](#the-computed-layer-at-the-host) | ✅ `358f877d` |
| <a id="HC-D7"></a>HC-D7 | *Implementation decision*, if [OQ-HC1](#oq-hc1) is not A. **A derive error at the host refuses that surface only; a provider composition error refuses the apply before any write.** The first is one pack's defect, and the rest of the apply is independent of it. The second is an input every host-derivable surface shares, and `yolo host --` refuses on it too | 2026-09-27 | [what host apply renders](#what-yolo-host-apply-renders-into-a-derived-surface) | ✅ `358f877d` |
| <a id="HC-D8"></a>HC-D8 | *Implementation decision*, if [OQ-HC1](#oq-hc1) is not A and catalogs stay declared in full ([OQ-CO16](../design/config-ownership-and-promotion.md#OQ-CO16)). **The first host apply that makes a table yolo's in a home confirms before dropping an entry in it,** as a first apply does today (`confirmHostLosses`). Without it a home already asserted would lose a hand-added provider with a report and no prompt, because the prompt fires only on a first apply | 2026-09-27 | [the per-key write](#the-host-write-is-per-key) | ✅ `358f877d` |
| <a id="HC-D9"></a>HC-D9 | *Implementation decision*, if [OQ-HC1](#oq-hc1) rules B1. **The declaration is an option on the `yolo.derive` registration, not a manifest field.** The registration already is the computed-layer declaration (`packload.DerivedSurfaces`), and "its output is host-valid" is a fact about that function. Its spelling is the implementer's | 2026-09-27 | [OQ-HC1](#oq-hc1) | ⛔ not built: [OQ-HC1](#oq-hc1) ruled against a per-surface opt-in, so there is no registration option |
| <a id="HC-D10"></a>HC-D10 | *Implementation decision*, if [OQ-HC1](#oq-hc1) is not A. **A derive's output reaches a real file per key: a table declared in full is written wholesale, any other object asserts only its leaves, and a tombstone is dropped before either arm sees it** ([the per-key write](#the-host-write-is-per-key)). Leaves, because that is what `ctx.in_full` already means for a `stateful` surface and for the host's table probe ([CO13](../design/config-ownership-and-promotion.md#built-2026-09-25--what-shipped)), and today's `rmw` computed write would otherwise clear `claude/settings`' `env` (MEASURED, [the per-key write](#the-host-write-is-per-key)). No tombstone, because at the host the only layer below a derive is your real file, so a tombstone can only delete your key; and applying it under `own` alone would break [config-ownership §11](../design/config-ownership-and-promotion.md#11-success-criteria)'s switch criterion. For a surface a pack declares `rmw`, [OQ-CO15](../design/config-ownership-and-promotion.md#OQ-CO15) governs, and the host follows its ruling | 2026-09-27 | [the per-key write](#the-host-write-is-per-key) | ✅ `358f877d` |
| <a id="HC-D11"></a>HC-D11 | *Implementation decision*, if [OQ-HC1](#oq-hc1) is not A. **One host-input composition per invocation, shared by every host-target reader:** the apply's dry run, `--assert` and `--format json`, the launch gate `yolo host --` runs before a wrapped agent, and `yolo config ls` and `render` at `--at host`. The launch gate runs the apply's own code path with writes on (`hostApplyGateApply`), so a composition any of them skipped would render catalogs without providers and undo an apply's rows, or report them as drift, at every wrapped launch | 2026-09-27 | [the computed layer at the host](#the-computed-layer-at-the-host) | ✅ `358f877d` |
| <a id="HC-D12"></a>HC-D12 | *Implementation decision.* **`yolo host apply --revert` keeps a `defaults` key whose declared default is an empty object or array and that still holds exactly that, and names it as kept.** MEASURED: on a fresh home with packs `["pi"]`, `--revert --assert` after an `--assert` removed `pi/models`' `providers` default and left `{}` ([the host pi findings](#host-pi-measured) item 1). A revert empties a file rather than deleting it, and deleting a file yolo created was the other answer; it needs a record of creation yolo does not keep, and the revert refuses to guess it. An empty default holds nothing to withdraw: it is the shape the pack declares its file needs. A non-empty default still goes, and so does an empty one the user has since filled | 2026-09-27 | the design's fixes that waited on no ruling | ✅ `entrypoint.keptShapeDefault`, pinned by `TestARevertKeepsPiModelsProviders` and `TestHostRevertKeepsAnEmptyDefaultAndSaysSo` |
| <a id="HC-D13"></a>HC-D13 | *Implementation decision.* **The host inputs travel as the jail's own wire tables, in the host Env's variables** (`YOLO_PROVIDERS`, `YOLO_PROFILES`, `YOLO_USE_PROFILES`, `YOLO_MCP_SERVERS`, `YOLO_LSP_SERVERS`), so the readers a jail's boot uses (`LoadProviders`, `LoadProfiles`, `LoadUseProfiles`, `mcpServersWith`, `LoadLSPServers`, `surfaceSelectionFor`) read the host composition unchanged. That is the ruling's "same handling" as code: the one per-notch difference is the composition in `composeHostInputs` (internal/cli). `YOLO_MCP_PRESETS` is deleted from the variables even when a caller sets it | 2026-09-28 | [OQ-HC1](#oq-hc1) | ✅ `358f877d` |
| <a id="HC-D14"></a>HC-D14 | *Implementation decision.* **The jail-path check runs over the derive's output, keys and values, and refuses that surface by name.** The roots are derived from the jail render target the boot projects onto (its home and workspace), plus the four mounts a launch supplies to every container jail (the context root, the install prefix, `/run/yolo`, the service endpoint directory). A root counts only as a whole path token. The check reads the derive's output and not the file, because the file's other keys are the user's or the declared layers'. The same predicate (`entrypoint.JailPathsIn`) omits a user's `mcp_servers` or `lsp_servers` entry at composition ([HC-D6](#HC-D6)), so one bad entry costs that entry, and the output check is the backstop for a derive spelling a path itself. | 2026-09-28 | [OQ-HC1](#oq-hc1) | ✅ `358f877d` |
| <a id="HC-D15"></a>HC-D15 | *Implementation decision.* **A root the rendered home lies at or under is not a jail path there.** `yolo host apply` run inside a jail renders into that jail's `/home/agent`, and an account whose home is `/home/agent` is the same case: a path under the real home is host-valid | 2026-09-28 | [HC-D14](#HC-D14) | ✅ `358f877d` |
| <a id="HC-D16"></a>HC-D16 | *Implementation decision.* **MCP presets are excluded at the host, not composed for it, and each one is named in the report with its remedy.** A preset's command is the node wrapper only a jail's boot writes, run against the jail's npm prefix; composing a host spelling would mean yolo installing and maintaining npm packages in a real home, which [`mcp-presets-removal.md`](../design/mcp-presets-removal.md) retires the presets rather than grow. The report line says to declare the server under `mcp_servers` with a command the machine has | 2026-09-28 | [OQ-HC1](#oq-hc1), [HC-D6](#HC-D6) | ✅ `358f877d` |
| <a id="HC-D17"></a>HC-D17 | *Implementation decision.* **At the host, a file value the selection record does not hold is outranked by the first activation; a recorded key whose value differs is the user's own pick and stands.** The jail's rule outranks a value its host layer carried ([OQ-SW1](providers.md#oq-sw1)); at the host the real file is that layer and there is no other, so the record decides instead (`hostSelectionBaseline`). Without it a `defaultModel` the user set before choosing `use_profiles` would win forever and the selection would do nothing, the silent no-op [OQ-HC3](#oq-hc3) exists to end. Both contracts ask the same function | 2026-09-28 | [OQ-HC3](#oq-hc3) | ✅ `358f877d` |
| <a id="HC-D18"></a>HC-D18 | *Implementation decision.* **Under `assert` the selection record lives beside the provenance record** (`render.Target.SelectionPath`), since that contract keeps no capture store, as the config-list insert record already does. The rmw arm decides the edge over the file as it stands, writes only the values the new record holds, deletes a cleared key, and persists the record after the write. Under `own` the record stays in the capture store | 2026-09-28 | [OQ-HC3](#oq-hc3) | ✅ `358f877d` |
| <a id="HC-D19"></a>HC-D19 | *Implementation decision.* **The rmw arm takes the derive's leaves through `surfaceContribs`, applied after its table write and before `managed`.** Every rmw helper (the write, the preview, the change predicate, the loss and formatting probes) already takes that value, so the leaves reach each without a new parameter, and a jail never sets them. The provenance record labels each leaf's top-level key `computed`, at the record's top-level grain, so a leaf yolo stops asserting reads `retired:computed` ([the per-key write](#the-host-write-is-per-key) rule 4); since [HC-D25](#HC-D25) the leaf itself is also cleared when the file still holds yolo's value. Empty objects are pruned from the leaves, since writing one would add a key the user never had | 2026-09-28 | [HC-D10](#HC-D10) | ✅ `358f877d` |
| <a id="HC-D20"></a>HC-D20 | *Implementation decision.* **The dropped-entry remedy names the user config's tables again** (`mcp_servers`, `lsp_servers`, `providers`), with the per-surface `config-overlay` kept as the one-agent alternative, and groups on the user config. [HC-D2](#HC-D2) said it would, once a host surface consumed those tables | 2026-09-28 | [HC-D2](#HC-D2) | ✅ `358f877d` |
| <a id="HC-D21"></a>HC-D21 | *Implementation decision.* **The tables written wholesale are the key-name probe's and the real derive's in-full keys together.** The probe (`hostTableKeys`) keeps a table yolo's when its derive omits the key over empty input (codex's and opencode's `omitEmpty`), so removing the last server regenerates the table empty rather than leaving the file's copy. Each table holds the overlays' entries, then the derive's, then managed's | 2026-09-28 | [HC-D10](#HC-D10) | ✅ `358f877d` |
| <a id="HC-D22"></a>HC-D22 | *Implementation decision.* **A surface whose declared layers were all `${workspace}`-keyed is skipped only when its derive computes nothing either.** `claude/config` was skipped at the host for that reason and now renders its derive's `mcpServers`, so yolo owns `~/.claude.json`'s user-scope server list at the host as in a jail: a server added with `claude mcp add` is a loss the first apply confirms, and declaring it under `mcp_servers` keeps it | 2026-09-28 | [OQ-HC1](#oq-hc1) | ✅ `358f877d` |
| <a id="HC-D23"></a>HC-D23 | *Implementation decision.* **`provider` and `profile` leave the kinds the host notch names as not applying.** Both render into the files of the surfaces their facts reach, as `config-overlay` does, so the `--verbose` report names the composed provider table and the selection in one `input` line, and each input the composition leaves out gets a line of its own in every view. A composition that fails refuses the apply in both postures, before anything is written | 2026-09-28 | [HC-D7](#HC-D7) | ✅ `358f877d` |
| <a id="HC-D24"></a>HC-D24 | *Implementation decision.* **`own`'s `computed` → `stateful` is a stated coercion in the census** (`render.ModeSet.coerce`), since that census runs two composing mechanisms and `Mechanism`'s derived fallback coerces only onto a sole one. A keyless `computed` surface is still refused, by `hostStatefulRefusal`'s [OQ-CO9](../design/config-ownership-and-promotion.md#13-decision-ledger) carve-out | 2026-09-28 | [OQ-HC2](#oq-hc2) | ✅ `358f877d` |
| <a id="HC-D25"></a>HC-D25 | *Implementation decision, from a review; revises [the per-key write](#the-host-write-is-per-key) rule 4.* **At the host the rmw arm clears a computed leaf yolo wrote once its derive stops asserting it, when the file still holds yolo's value.** Measured: `yolo host apply` on `bedrock` wrote `CLAUDE_CODE_USE_BEDROCK` into the real `~/.claude/settings.json`, moving claude to `codex` and applying again left it, every later launch composed it as the user's host layer, and claude ran in Bedrock mode with no AWS credential while [PP-D1](../design/providers-and-profiles-redesign.md#PP-D1)'s line blamed the user; `ENABLE_LSP_TOOL` stayed the same way after the last LSP server went. The provenance record is per top-level key, and `env` reads `computed` whenever any leaf under it is yolo's, so a **computed-leaf record** (coined here) keeps, per leaf, its RFC 6901 pointer and the value yolo wrote, recorded only when yolo's write is what put that value in the file (`render.Target.LeafRecordPath`, beside the provenance record; `entrypoint.hostRMWLeafRecord`). A leaf the file already held with that value is the user's and is never recorded, and a recorded one the user changed is theirs; a clear removes the leaf from the file's own content before any layer writes, so a live layer asserting the same path (another pack's config-overlay, managed, a default) still sets it, and keeps its parent; the record is written after the write, never in a dry run, and a revert removes it. The selection namespace keeps its own record ([HC-D18](#HC-D18)). A launch reads the same record to tell a platform switch yolo wrote from one the user wrote (`render.HostLeafWrote`). Not covered: a value an older build wrote before the record existed reads as the user's, the direction that deletes nothing | 2026-09-29 | [HC-D10](#HC-D10), [HC-D19](#HC-D19) | ✅ `hostRMWLeafRecord`; pinned by `TestHostApplyClearsTheBedrockSwitchItWroteWhenClaudeLeavesBedrock` and `TestTheHostLeafRecordNamesTheSwitchYoloWrote` |
| <a id="HC-D26"></a>HC-D26 | *Implementation decision, taken under the maintainer's 2026-10-04 delegation ("make them and build it … adjust later"); reversible. Supersedes [HC-D6](#HC-D6)'s exclusion of a pack's `mcp` declaration.* **The host `mcp_servers` input is the selected packs' `mcp` entries, joined to the real home, with your user-scope entries merged over them** (`packload.ComposeMCPServers`, the composition a jail launch makes for the jail's home), then HC-D6's jail-path check and per-agent filters as before. HC-D6's reason, that [OQ-MP3](../design/mcp-presets-removal.md#15-open-questions) ruled the declaration "reaches only the jail", described the kind before the host could deliver what an entry runs; [OQ-HC1](#oq-hc1) asks for host parity with the same handling, and [roadmap](../plans/roadmap.md) item 44 records that the maintainer wants the Chrome DevTools server at `yolo host`. **Only a pack yolo ships or one at a path on this machine composes** (`Pack.MayRunHostHalf`): at the host an entry's command runs unconfined as you whenever the agent starts, which is host code, and [OQ-HS4](../design/host-notch-services.md#11-decision-ledger) refuses a fetched pack's host code by name, failing closed until a trust ruling exists; so a fetched pack's entry is left out and named, with the step that runs it. ⚠ That half is possibly the maintainer's call ([MP-D4](../design/mcp-presets-removal.md#MP-D4)) | 2026-10-05 | [OQ-HC1](#oq-hc1), [HC-D6](#HC-D6) | ✅ `composeHostInputs`, `hostMCPPacks`; pinned at the front door by `TestYoloHostApplyWritesTheChromeDevtoolsServerForEveryAgent` and, for the fetched pack, `TestYoloHostApplyLeavesAFetchedPacksMCPServerOutOfEveryAgentsFile`; `TestAFetchedPacksMCPEntryIsNamedAndLeftOutAtTheHost` pins the predicate |
| <a id="HC-D27"></a>HC-D27 | *Implementation decision, as HC-D26; revises [HC-D16](#HC-D16).* **Presets stay excluded at the host, and the omission line's next step names the pack that ships a server of the preset's name**: "pack chrome-devtools ships that server for the host too: add "chrome-devtools" to `packs`", or, with it selected, that its server is written instead. HC-D16's reason predates the host floor: yolo now does install and maintain a pack's npm program in a real home (the floor, docs/design/host-tool-provisioning.md), so what keeps the preset out is no longer that, but that the preset is retiring ([mcp-presets-removal.md §13](../design/mcp-presets-removal.md#13-what-i-would-build-in-order) step 3) and the pack is the route it retires into | 2026-10-05 | [HC-D16](#HC-D16) | ✅ `presetHostRemedy`; pinned by `TestThePresetLineNamesTheChromeDevtoolsPack` |
| <a id="HC-D28"></a>HC-D28 | *Implementation decision, as HC-D26.* **`yolo host -- <agent>` also makes sure yolo's floor holds every program a composed entry's `bin` names**, after the agent's own, since the agent starts that server whenever it starts and the host has no lazy launcher to install it on first use. A failed install, or a program the floor may not hold, is one line saying what then happens and naming `yolo host apply --assert`; the agent starts either way ([MP-D3](../design/mcp-presets-removal.md#MP-D3)). **⚠ Narrowed 2026-10-08 by [HNR-D6](../design/host-notch-readiness.md#HNR-D6):** the launch's readiness act installs a declared server program first and refuses when it cannot, so "either way" now holds only for a program the user leaves out of the floor | 2026-10-05 | [OQ-HC1](#oq-hc1) | ✅ `ensureMCPPrograms`; pinned by `TestAHostAgentLaunchInstallsTheProgramItsMCPServerRuns`, `TestAHostAgentLaunchSaysWhenItsMCPServersProgramCannotBeInstalled`, `TestAHostAgentLaunchWithNoFloorCopyStillInstallsItsMCPServersProgram` and, for a fetched pack's server, `TestAHostAgentLaunchInstallsNothingForAFetchedPacksMCPServer` |
