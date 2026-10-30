---
title: "A pack's files inside another pack's state folder — why agent-addressed delivery is the fix, and what yolo owes the case it cannot address"
date: 2026-10-30
status: in-review
stage: DESIGN
next: "Run the agy layout check in §5.2 and rule OQ-FS1; rule OQ-FS2 to OQ-FS6"
tags: [packs, files, slots, state, macos-user, darwin-home-layout, agy, parity, happy-path]
summary: "The maintainer's matt-agentwork pack delivers an Antigravity subagent definition by path into ~/.gemini/config/agents, a folder the agy pack owns as state. On macos-user that creates a real ~/.gemini in the shared account home, locks every later agy launch out, and the refusal says to wipe the whole account. The fix on the pack side is agent-addressed files through a slot the agy pack declares; on the yolo side, a warning for the shape, a refusal that names only what is in the way, a boot that stops once the layout fails, and housekeeping for dangling layout links."
vantage:
  status-chip: true
---

# A pack's files inside another pack's state folder — why agent-addressed delivery is the fix, and what yolo owes the case it cannot address

**Status:** 2026-10-30. Nothing built. Every code claim was read against the tree at `e4345e69d`
on 2026-10-30; the Antigravity discovery layout is read from Google's own CLI documentation the same
day ([§5.1](#51-what-agy-reads-and-what-nobody-has-measured)) and is **unmeasured**.

> **In short.** A `files` contribution that names a path inside a folder another pack declares as
> `[[state]]` is addressing an agent by a path it does not own, and every defect in the
> 2026-10-30 lab report follows from that one shape. yolo already has the right spelling —
> `files` addressed to an agent, landing in a slot the agent's pack declares — and the agy pack
> is the one agent pack with no such slot. The rest of this design is what yolo owes the shape it
> cannot forbid: say it before it costs anything, refuse naming only what is in the way, and stop
> writing once the layout it writes into has failed.

**Why it matters.** The maintainer runs Antigravity's Argon as a design seat everywhere they work,
and its research child must be discoverable wherever `agy` runs under yolo
([FS-D1](#decision-ledger)). Today the file that makes that true is delivered in a way that, on
`macos-user`, bricks every later launch that selects the agent's own pack, with a next step that
destroys the whole sandbox account.

**The shape.** One slot in `packs/agy` (`[[files]] agent = "agy", into = ".gemini/config/agents"`),
one addressed contribution in `matt-agentwork`, `agy` selected wherever that pack is; plus four
yolo-side changes that are each a refusal, a warning or a stop with a next step.

**Cost.** The pack's file lands only when the agy pack is selected, so the maintainer's user config
has to select it; one open measurement decides whether the slot's default layout is enough.

**Start at [§4](#4-the-pack-side-change-address-the-agent)** and
[§5](#5-the-agy-slot-and-the-one-thing-nobody-has-measured); [§6](#6-the-foreign-parent-shape-say-it-early-forbid-it-nowhere-new) to
[§8](#8-dangling-layout-links-from-dead-workspaces) are what yolo owes every pack that keeps the old shape.

**Needs your ruling:** [OQ-FS1](#OQ-FS1), [OQ-FS2](#OQ-FS2), [OQ-FS3](#OQ-FS3),
[OQ-FS4](#OQ-FS4), [OQ-FS5](#OQ-FS5), [OQ-FS6](#OQ-FS6).

**Reads with:** the [lab report](../lab-reports/2026-10-30-mac-sandbox-home-collisions-and-check-nudges.md)
(the evidence: [its §2](../lab-reports/2026-10-30-mac-sandbox-home-collisions-and-check-nudges.md#2-dangling-layout-links-from-integration-runs)
and [§3](../lab-reports/2026-10-30-mac-sandbox-home-collisions-and-check-nudges.md#3-a-files-delivery-into-another-packs-state-folder)), [`macos-user-home-tiers.md`](../reference/macos-user-home-tiers.md)
(the layout, the overlay, and the no-migration ruling this design narrows),
[`pack-system.md`](../reference/pack-system.md#files) (the `files` kind and its slot mechanism),
[`slots-and-contributions.md`](slots-and-contributions.md) (the slot shape this extends),
[`backend-parity.md`](backend-parity.md) (the disposition vocabulary [§9](#9-parity-across-backends-and-notches) uses),
[`happy-path-principle.md`](../reference/happy-path-principle.md) (every message in [§10](#10-the-messages) is written to it).

---

## 1. The verdict

Four principles carry the design; later sections cite them by number.

- **P1. A path inside another pack's state folder is not a destination, it is an address.** The
  content pack means "give this to the agent that reads here", and yolo has a spelling for exactly
  that: `files` with `agents`, landing in a slot the agent's pack declares. Core never learns the
  agent's path, the content lands only while the agent's pack is selected, and it leaves when the
  pack is dropped ([`pack-system.md`](../reference/pack-system.md#files)).
- **P2. The shape cannot be forbidden, so it must be said early.** A pack delivering a helper
  script into `~/.claude` is a legitimate use `yolo host apply` was written for
  ([`hostfilestree.go`](../../internal/entrypoint/hostfilestree.go)'s header names
  `~/.claude/file-suggestion.sh`), and when the owning pack is selected the nested delivery works
  on every backend. The defect is the owner's *absence*, and the place to say so is `yolo check`
  and the launch, over the selected set alone ([§6](#6-the-foreign-parent-shape-say-it-early-forbid-it-nowhere-new)).
- **P3. A refusal names what is in the way, and only that.** The whole-account reset the layout
  step prints today is the right remedy for a home that predates the layout and the wrong one for
  a single folder a pack made ([§7](#7-the-layout-refusal-name-the-path-stop-the-boot)).
- **P4. Once the layout has failed, nothing writes.** Every later macos-user boot step writes into
  the home the layout lays; running them after it failed is how a refused launch filled the real
  `~/.gemini` with generated config ([§7.2](#72-stop-the-boot)).

## 2. What exists today, stated precisely

### 2.1 The two spellings of `files`

| Written | What it is | Where it lands | Lands when |
| :--- | :--- | :--- | :--- |
| `{kind: files, from: <tree>, into: <path>}` | path-addressed content | `~/<path>`, `:ro` in a jail, written at the host | the pack is selected |
| `{kind: files, agent: <bin>, into: <dir>}` | a **slot**: where content addressed to that agent lands; ships nothing | — | — |
| `{kind: files, from: <tree>, agents: [<bin>]}` | agent-addressed content | `<slot>/<contributing pack>` (`packload.SlotLanding`, [`mergedest.go`](../../internal/packload/mergedest.go)) | the agent's pack is selected **and** declares a slot |

Both notches resolve addressed content through one function, `packload.ResolveDestinations`:
the jail's mount planner (`packFilesTargets`, [`packfiles.go`](../../internal/cli/run/packfiles.go)),
`yolo host apply` ([`apply.go`](../../internal/cli/apply.go)) and a host session home
([`hostsessionhome.go`](../../internal/cli/hostsessionhome.go)). A slot may `register` each landing
in a list inside one of its own pack's config surfaces and may name the top-level entries it
`expects`, warned about and never refused ([`filesregister.go`](../../internal/packdecl/filesregister.go)).
`packs/pi` is the one slot in the tree (`.pi/agent/yolo-packs`, registering into `/packages`).

Two severities already govern addressing ([`agent-briefings.md`](../reference/agent-briefings.md#two-severities-an-unknown-name-is-fatal-an-unmatched-destination-is-reported)):
an `agents` name no selected pack provides is **fatal** at the launch pre-flight and at
`yolo host apply` (`packload.AgentAudienceProblems`), and a name whose owner declares no slot of
that kind is **reported**, never refused (`reportUnmatchedAudiences`,
[`unmatchedaudience.go`](../../internal/cli/run/unmatchedaudience.go)). [§4.2](#42-the-cost-agy-must-be-selected-wherever-the-pack-is)
is where the first severity bites this design.

### 2.2 How each backend materializes a path-addressed `files` destination

| Backend or notch | Delivery | What a destination's missing parent becomes |
| :--- | :--- | :--- |
| podman | `:ro` bind over the staged tree; a mountpoint is made in the per-launch home skeleton, or in the workspace state dir when the path is below a selected pack's state dir (`preparePackFiles`, [`packfiles.go`](../../internal/cli/run/packfiles.go)) | a root-owned directory in the skeleton, rebuilt every launch — this jail's `/home/agent/.gemini` is one |
| Apple Container | the same bind; the whole home is the writable workspace state dir | an ordinary directory in the per-workspace home |
| macos-user | the host composes a home overlay and the bootstrap's last generator installs it, creating missing parents beneath the account home or the workspace sidecar (`openOverlayParentIn`, [`darwinoverlay.go`](../../internal/entrypoint/darwinoverlay.go)) | **a real top-level directory in the account home every workspace shares** |
| `yolo host apply` | per-file write into the user's real home, refusing a path the user owns ([`hostfilestree.go`](../../internal/entrypoint/hostfilestree.go)) | a directory in the user's own home, which is what the agent reads there |

Only one row makes a persistent, shared, wrong-tier directory. That row is the lab report.

### 2.3 The layout, and what it refuses

On macos-user the `darwin_home_layout` step runs first ([`bootsteps.go`](../../internal/entrypoint/bootsteps.go))
and lays one symlink per selected pack's workspace-scope `[[state]]` dir from the account home into
`<workspace>/.yolo/home`. `ensureLayoutSymlink` ([`darwinhomelayout.go`](../../internal/entrypoint/darwinhomelayout.go))
repoints a link yolo wrote and **never removes a real path**: a real file or directory where a link
belongs is collected, and `occupiedLayoutError` prints every account-home occupant under the heading
*"In the SANDBOX ACCOUNT HOME, which predates this layout"* with one remedy,
`sudo rm -rf /Users/_yolojail`. That is [`OQ-HT2`](../reference/macos-user-home-tiers.md#oq-ht2)'s
no-migration ruling as code, written when the only way a real path could be there was a home older
than the layout.

A boot step's failure is recorded and the boot **continues**, so every problem is reported in one
boot (`genStepAbout`, [`boot.go`](../../internal/entrypoint/boot.go); the refusal record in
[`bootrefusal.go`](../../internal/entrypoint/bootrefusal.go)). The overlay install honors the same
rule per destination.

### 2.4 The packs

- **`packs/agy`** declares a program, a briefing destination, a skills destination with
  `project_dirs = [".agents/skills"]`, config surfaces under `~/.gemini/antigravity-cli/`, autonomy,
  `[[state]] at = ".gemini"`, a machine-scope `.gemini-shared-credentials` and the hook that links the
  OAuth token into it ([`pack.toml`](../../packs/agy/pack.toml)). **No `[[files]]` slot.**
- **`matt-agentwork`** (the maintainer's, outside this tree) ships five skills and one
  path-addressed `files` contribution, `from: files/gemini/config/agents` into
  `.gemini/config/agents`, holding `design-research.md`: frontmatter `subagent: true`,
  `mainAgent: false`, `model: flash`, `commandExecutionPolicy: sandbox`. Its manifest comment says
  why it is machine-wide: agy discovers agents from the home, and a workspace without the file is a
  seat that silently researches on Argon. The `paired-design` skill's brief tells the seat to stop
  on `subagent ... not found or not allowed to be invoked`.
- The maintainer's `matt` pack ships a pi `designer` agent into `~/.pi/agent/agents` the same
  way: path-addressed into pi's state folder. It has never collided only because `pi` is selected
  in every one of their launches. It is the same shape, and [§6](#6-the-foreign-parent-shape-say-it-early-forbid-it-nowhere-new)'s
  warning stays quiet for it exactly as long as that holds.

## 3. The diagnosis

Each finding of the lab report, and the one cause under it:

| # | Finding | Cause |
| :--- | :--- | :--- |
| 1 | A launch without agy creates a real `/Users/_yolojail/.gemini`; every later launch selecting agy is refused | the file is path-addressed into a folder whose owner is not selected (P1); macos-user materializes the parent in the shared tier ([§2.2](#22-how-each-backend-materializes-a-path-addressed-files-destination)) |
| 2 | The refusal says `sudo rm -rf /Users/_yolojail` when only `.gemini` is in the way | `occupiedLayoutError` has one remedy for every account-home occupant (P3) |
| 3 | The refused launch wrote agy's `settings.json`, `mcp_config.json`, `cache/onboarding.json` and the token link into the real folder | the boot continues after the layout step fails (P4) |
| 4 | Nothing warns ahead of time | no reader compares a `files` destination against the selected packs' state dirs (P2) |
| 5 | A dangling `.gemini` link from a deleted integration workspace refused a later matt-agentwork launch until `sudo rm` | a layout link this launch does not declare is left alone by design; a path-addressed delivery under it is refused as a link yolo did not lay ([§8](#8-dangling-layout-links-from-dead-workspaces)) |
| — | On Linux podman, a root-owned `/home/agent/.gemini` exists in a jail that does not select agy | the same shape, materialized per launch in the skeleton; harmless today, and the reason the warning in [§6](#6-the-foreign-parent-shape-say-it-early-forbid-it-nowhere-new) is not macOS-only |

Finding 1 is the design's center. Findings 2 to 5 are owed to every pack that keeps the shape, and
to the maintainer until theirs moves.

## 4. The pack-side change: address the agent

### 4.1 The contribution

`matt-agentwork` replaces its path-addressed entry with an addressed one:

```json
{"kind": "files", "agents": ["agy"], "from": "files/agy"}
```

and the manifest comment above it reads:

> The Flash research child the `agy` design seat delegates to. Addressed to the agent, not to a
> path: agy's own pack says where agent definitions live and lays that folder per workspace, so
> this lands there only in a launch that selects `agy`, and leaves when it is dropped. A workspace
> may still override it with its own `.agents/agents/` definition, which agy reads first.

What the tree under `files/agy` holds depends on [OQ-FS1](#OQ-FS1); the README's "Design with an
expensive seat" section replaces its `~/.gemini/config/agents/design-research.md` sentence with the
landing path that question settles, and says that the `agy` pack must be selected.

**Why not the per-workspace `.agents/agents/` path.** The maintainer runs Argon through `agy` in
every setting — on the host, inside jails, as a subprocess of another agent and directly — and in
every project ([FS-D1](#decision-ledger)). A copy in each project tree would have to be carried by
every repository they touch, including ones that are not theirs. It stays only as the override agy
already offers, which the comment above keeps.

### 4.2 The cost: `agy` must be selected wherever the pack is

An `agents` name no selected pack provides is fatal at the launch pre-flight and at
`yolo host apply` ([§2.1](#21-the-two-spellings-of-files)). Today the maintainer's config does not
select `agy` (the lab report's launch says `agy/settings has no owner`), and `agy` in their jails is
a hand-installed binary reading a real `~/.gemini`. With the change in [§4.1](#41-the-contribution),
every launch of theirs that does not select `agy` is refused with the existing message naming the
unknown agent. The remedy is one line in their user config, `yolo pack add agy`, and it is the
right one: yolo manages the environment the agent runs in, and a copy of the agent run outside
that management is not promised what the environment delivers. [OQ-FS6](#OQ-FS6) asks the maintainer to confirm that over the alternative of making an absent
audience survivable, which the standing two-severities ruling refuses.

## 5. The agy slot, and the one thing nobody has measured

### 5.1 What agy reads, and what nobody has measured

Google's Antigravity CLI documentation describes custom agents as a **directory per agent holding
`agent.md`**: the `/agents` panel's header shows the templates
`{workspace}/.agents/agents/{agent_name}/agent.md` and `~/.gemini/config/agents/{agent_name}/agent.md`,
its instructions say "create a directory matching your agent name and add an `agent.md` file with
YAML frontmatter", its example frontmatter carries `name: code-reviewer`, and its mistakes table
says the scanner "looks specifically inside `agents/` directories"
([Agents command](https://antigravity.google/docs/cli/commands/agents), read 2026-10-30). The
maintainer's flat `agents/design-research.md` works too, by their own report, so agy reads at
least two shapes. The documentation does not say whether the agent's name comes from the
directory or from the frontmatter `name`, and does not say whether the scan recurses.

That gap is exactly the question a slot raises. `SlotLanding` puts a contributing pack's tree at
`<slot>/<pack>`, so with a slot at `.gemini/config/agents`:

| The pack ships | agy sees | Loads if |
| :--- | :--- | :--- |
| `files/agy/design-research.md` (today's file) | `agents/matt-agentwork/design-research.md` | the scan recurses into a directory and reads flat files there — **the least likely reading of the docs** |
| `files/agy/agent.md` with `name: design-research` | `agents/matt-agentwork/agent.md` — the documented layout | the agent's name comes from frontmatter; if it comes from the directory, the agent is called `matt-agentwork` |
| `files/agy/design-research/agent.md` | `agents/matt-agentwork/design-research/agent.md` | the scan recurses one level |

### 5.2 The one check that settles it, and who runs it

No automated test may start an agent (the repository's rule: `--version` probes only), so this is
the maintainer's to run once, on any machine with `agy`:

1. Create `~/.gemini/config/agents/probe-pack/agent.md` with the frontmatter of
   `design-research.md` and `name: probe-by-frontmatter`.
2. Create `~/.gemini/config/agents/probe-pack/probe-nested/agent.md` with `name: probe-nested`,
   and `~/.gemini/config/agents/probe-pack/probe-flat.md` with `name: probe-flat`.
3. Start `agy`, open `/agents`, and read **Available Agents**.
4. Remove `~/.gemini/config/agents/probe-pack`.

What the panel lists decides the row, and the row decides the branch:

| Listed | Meaning | Branch |
| :--- | :--- | :--- |
| `probe-by-frontmatter` | the name comes from frontmatter | **A** — the plain slot is enough |
| `probe-pack` | the name comes from the directory | **B** — the slot needs a way to name the landing |
| `probe-nested` or `probe-flat` as well | the scan recurses | **C** — a pack may ship several agents under one tree |

### 5.3 The slot, in each branch

The slot itself is the same in every branch, and it is a change to a pack yolo ships, so it is a
yolo change ([FS-D3](#decision-ledger)):

```toml
[[files]]
# Where a pack lands an Antigravity agent definition: agy reads a directory per agent holding
# agent.md under this folder (Antigravity CLI docs, "Agents command"), and SlotLanding's
# <slot>/<pack> is that directory. Outside every folder agy's own config generators write.
agent = "agy"
expects = ["agent.md"]
into = ".gemini/config/agents"
```

- **Branch A.** Nothing else. A content pack ships one agent per tree with `agent.md` at the root;
  `expects` warns at `pack lint` when it is missing. One agent per contributing pack is the limit,
  and a pack wanting two ships two packs or waits for C.
- **Branch B.** The agent would be named for the contributing pack. Two ways out, and the design
  prefers the first:
  - **`as` on the addressed contribution** — `{kind: files, agents: ["agy"], from: ..., as: "design-research"}`
    replaces the contributing pack's name in the landing with the name given. It keeps every
    property the per-pack subdirectory exists for (nothing lands at the slot root; the owner's own
    tree is untouched), moves the one collision class from "impossible" to "refused by name" at
    the same pre-flight that refuses two `files` claims on one path today (`PackDestConflicts`),
    and feeds `register` the same landing. `as` is refused on a path-addressed contribution and
    on a slot, must be a single path component, and two addressed trees for one agent with one
    `as` across the selected set are refused naming both packs.
  - **`layout = "flat"` on the slot** — land each contributor's entries directly in the slot. It
    reaches more agents' conventions but gives up the subdirectory, so the pre-flight has to
    refuse any basename two contributors share, and `register` has nothing to list. Kept as the
    fallback for an agent whose convention has no subdirectory at all.
- **Branch C.** The plain slot, and a pack may ship `files/agy/<name>/agent.md` several times over.
  `expects` then names nothing, or the slot drops it.

`register` is not an option here: agy's `settings.json` is an alias table for models and names no
list of agent paths that anyone has found. If one exists it becomes branch A's refinement, not a
new branch.

## 6. The foreign-parent shape: say it early, forbid it nowhere new

**Foreign-parent delivery** *(coined here)* is a path-addressed `files` destination whose first
path component is a dot-directory that neither core's home skeleton (`paths.HomeSkeletonCoreDirs`,
[`homeskeleton.go`](../../internal/paths/homeskeleton.go)) nor the contributing pack's own
`[[state]]` declares. It is not a nested delivery into a selected pack's state, which works on
every backend and stays legal.

### 6.1 Three readers, one rule, no new refusal

| Reader | Knows | Says |
| :--- | :--- | :--- |
| `yolo pack lint <dir>` (no config) | the one pack | a **note**: the destination lies inside a dot-directory this pack does not declare as state; if another pack owns it, address its agent |
| `yolo check`, Packs section | the selected set | a **warning** when no selected pack declares that dot-directory as state ([§10](#10-the-messages) has the text); nothing when one does |
| the launch pre-flight (`stagePacks`) and `yolo host apply` | the selected set | the same warning, on stderr, beside `reportUnmatchedAudiences`'s line |

Why a warning and not a refusal ([OQ-FS2](#OQ-FS2) asks the maintainer to confirm): on podman and
Apple Container the shape is harmless, so a refusal would be a macos-user-only rule dressed as a
pack rule, against the parity principle; and at `yolo host apply` the shape is often the intent.
The warning is also what the maintainer's `matt` pack gets the day `pi` leaves one of their
launches, which is the right moment to hear it.

### 6.2 Reconciled with the name-reservation ruling

[`OQ-BH14`](../reference/jail-home.md#oq-bh14) rules that name reservation covers only selected
packs and an unselected pack is treated as if it does not exist, and `AGENTS.md` forbids reading
`packload.Embedded*` to reserve a name. This rule reads **only the selected set**, reserves
nothing and refuses nothing: it reports that a destination's parent is nobody's in *this*
selection, without naming any pack that is not there. It does not need to know agy exists to say
`~/.gemini` is unowned here. A test over the shipped set
(`TestNoShippedPackDeliversFilesIntoAnotherShippedPacksState`, [§11](#11-tests)) is the one place
that reads every shipped pack, and a test is not a reservation.

### 6.3 On macos-user: record what the overlay makes

The overlay install already records what it delivered (`writeOverlayInstallRecord`,
[`darwinoverlay.go`](../../internal/entrypoint/darwinoverlay.go)). It additionally records **each
top-level account-home directory it created** for a destination, with the pack and contribution
that asked for it. That record has two readers:

- `occupiedLayoutError` ([§7.1](#71-name-the-path)) names the pack beside the occupying path when
  the record proves what made it.
- The layout step, when a real directory sits where a link belongs **and** the record says the
  overlay made it **and** the directory holds nothing but what the record says was delivered,
  replaces it with the layout link and says so on one line ([§10](#10-the-messages)). Anything
  else in it — an agent's sessions, a hand-written file, the generated config a refused launch
  left — and the directory is left alone and refused by name. [OQ-FS3](#OQ-FS3) asks whether this
  narrows [`OQ-HT2`](../reference/macos-user-home-tiers.md#oq-ht2) acceptably: that ruling protects
  a user's home from migration, and a directory yolo made and nobody else wrote is not the user's.

## 7. The layout refusal: name the path, stop the boot

### 7.1 Name the path

`occupiedLayoutError` drops the whole-account remedy from the account-home group
([FS-D4](#decision-ledger)). Each occupying path is listed with what made it when the overlay
record says, and the remedy is one `sudo rm -rf <path>` per path after "move what you want to
keep". The sidecar and `host_files` groups are unchanged; they already name their paths. The
account-reset sentence survives only as a last line when **every** occupied path is one the record
cannot attribute and there are more than one, since that is the only case left that looks like a
home older than the layout.

### 7.2 Stop the boot

On macos-user, a `darwin_home_layout` failure **ends the bootstrap** after the refusal is written
([FS-D5](#decision-ledger), [OQ-FS4](#OQ-FS4) confirms). It is the first step, so nothing is lost
from the one-boot report, and every later step writes into tiers the layout did not lay. The
refusal record lists the layout failure alone, and the last line says why nothing else ran
([§10](#10-the-messages)). The container boot keeps its rule: its tiers are the launcher's mounts,
and no step there has this precondition.

## 8. Dangling layout links from dead workspaces

A layout link whose target sidecar no longer exists is left alone by a launch that does not
declare it (`TestDarwinHomeLayoutSurvivesAStaleLinkFromADeletedWorkspace`) and repointed by one
that does. Both are right. The defect is that nothing ever removes one, and the integration suite
leaves one per launch that selected a stateful pack.

- **Agent-addressed delivery removes the collision**: the file lands only when the owning pack is
  selected, and then the layout repoints the link before the overlay installs through it.
- **Housekeeping for what remains** ([OQ-FS5](#OQ-FS5)): `yolo check` on a macos-user host warns
  per dangling layout link in the account home, naming the workspace path it pointed at, and
  `yolo prune` lists them with `--apply` removing the link and never its target, in prune's
  dry-run-then-apply shape. The integration suite's macos-user helpers remove the links their own
  launches laid when the test ends, which is test hygiene and needs no ruling.

A target on an unmounted volume looks dangling too. Prune is explicit and names each target, so
the user decides; the layout never removes one on its own.

## 9. Parity across backends and notches

Dispositions are [`backend-parity.md`](backend-parity.md#3-the-dispositions--the-most-important-section)'s.

| Behavior | podman | Apple Container | macos-user | `yolo host apply` / `yolo host --` |
| :--- | :--- | :--- | :--- | :--- |
| Agent-addressed `files` lands at `<slot>/<pack>` when agy is selected | Honored (bind) | Honored (bind) | Honored (overlay) | Honored (per-file write) |
| Unknown `agents` name is fatal | Honored | Honored | Honored | Honored |
| Foreign-parent warning | Honored | Honored | Honored | Honored |
| Foreign parent materialized | HonoredBy: per-launch skeleton dir, gone at exit | HonoredBy: dir in the per-workspace home | **Warned**, and recorded ([§6.3](#63-on-macos-user-record-what-the-overlay-makes)) | NotApplicable: the user's home is the right tier |
| Layout refusal names the path | NotApplicable | NotApplicable | Honored | NotApplicable |
| Boot stops after layout failure | NotApplicable: no layout step | NotApplicable | Honored | NotApplicable |
| Dangling-link housekeeping | NotApplicable | NotApplicable | Honored | NotApplicable |

Every row a backend honors by a different mechanism names it, as the census requires, and the
annotation test (`backendparity_test.go`) covers any runtime-gated line the build adds.

## 10. The messages

Each is the whole message, with its next step on the following line, per the
[Happy Path Principle](../reference/happy-path-principle.md).

**`yolo check`, Packs section** (the launch prints the same as `Warning:` on stderr):

```text
  [WARN] pack matt-agentwork: files into ~/.gemini/config/agents lands inside ~/.gemini, a directory no selected pack declares as its state
       -> On macos-user this makes ~/.gemini a real directory in the account home every workspace shares, and a later launch that selects a pack keeping state there is refused. If that directory is an agent's, address the files to the agent instead and select its pack:
            {"kind": "files", "agents": ["<agent>"], "from": "files/gemini/config/agents"}
          If it is this pack's own, declare it:  [[state]] at = ".gemini"
          then: yolo check
```

**`yolo pack lint`** (a note, exit 0):

```text
  note  files into .gemini/config/agents lands inside .gemini, which this pack does not declare as state. If another pack owns it, address its agent ({"kind": "files", "agents": ["<agent>"], "from": "files/gemini/config/agents"}); if it is this pack's, declare [[state]] at = ".gemini".
```

**`darwin_home_layout`, an occupied account-home path** (replacing today's group):

```text
Error: darwin_home_layout: the per-workspace home layout cannot be laid: 1 path is real, where the workspace tier's symlink belongs.
There is no migration (macos-user-home-tiers.md OQ-HT2) — nothing here is copied, renamed or deleted for you.

In the SANDBOX ACCOUNT HOME:
  /Users/_yolojail/.gemini  (made by a launch of workspace /Users/Shared/yolo/launch-check on 2026-10-30, delivering matt-agentwork's files/gemini/config/agents; it now holds other files too)
Move what you want to keep out of each path, then remove it; the rest of the account is left alone:
  sudo rm -rf /Users/_yolojail/.gemini
then launch again.
```

**`darwin_home_layout`, reclaiming a directory the overlay made** ([§6.3](#63-on-macos-user-record-what-the-overlay-makes), one line, not an error):

```text
darwin_home_layout: replaced /Users/_yolojail/.gemini with this workspace's layout link — a launch of /Users/Shared/yolo/launch-check made it for matt-agentwork's files, and nothing else was ever written in it.
```

**The bootstrap, after a layout failure** (the last line of the refusal):

```text
darwin_home_layout failed, so no later step ran: every one of them writes into the home this layout lays. Fix the path above, then launch again.
```

**`yolo check` on a macos-user host, a dangling layout link:**

```text
  [WARN] /Users/_yolojail/.gemini is a layout link to /Users/Shared/yolo/yolo-it-1873416182/.yolo/home/gemini, which no longer exists
       -> A launch of a workspace that was since deleted laid it. Remove it (the target is already gone):
            yolo prune --apply
          then: yolo check
```

## 11. Tests

Each is named so that deleting the production call site fails it, and each says what it asserts.

| Test | Pins | Fails when |
| :--- | :--- | :--- |
| `TestAgyDeclaresAFilesSlotForAgentDefinitions` (`internal/packload`) | `packs/agy` loaded through `packload.Embedded()` declares one `files` slot at `.gemini/config/agents` expecting `agent.md` | the slot is removed or moved |
| `TestAnAgyAddressedTreeLandsAtBothNotches` (`internal/cli/run`, beside `filesslotparity_test.go`) | a fixture pack `{agents: ["agy"]}` lands at `.gemini/config/agents/<pack>` in `packFilesTargets` and in `RenderHostFiles` after `ResolveDestinations`; nothing lands without `agy` selected | either notch stops resolving through `SlotLanding` |
| `TestCheckWarnsOnFilesIntoAnUnselectedPacksStateDir` (`internal/cli/check`) | a fixture `files` into `.probe/x` with no selected pack declaring `.probe` warns with the [§10](#10-the-messages) text; selecting a fixture declaring `[[state]] at = ".probe"` silences it | the check is deleted or reads the shipped set |
| `TestLaunchWarnsOnAForeignParentDelivery` (`internal/cli/run`) | `stagePacks` prints the same warning once; the launch proceeds | the pre-flight line is deleted |
| `TestPackLintNotesAForeignParentDelivery` (`internal/cli`) | `yolo pack lint` prints the note and exits 0 | the note is deleted or becomes a refusal |
| `TestOccupiedLayoutRefusalNamesEachPathAndNotTheAccount` (`internal/entrypoint`) | the message carries `sudo rm -rf <path>` per occupant and not `sudo rm -rf <home>`, for one and for several | the account reset returns |
| `TestOccupiedLayoutRefusalNamesThePackThatMadeThePath` | with an overlay record attributing the directory, the line names pack, contribution and workspace | the record is not read |
| `TestTheOverlayRecordsATopLevelHomeDirectoryItCreates` | installing a destination whose top-level parent is absent writes the attribution | the record write is deleted |
| `TestTheLayoutReclaimsADirectoryOnlyTheOverlayWrote` / `…LeavesOneHoldingOtherFiles` | the two [§6.3](#63-on-macos-user-record-what-the-overlay-makes) outcomes, over a real temp home | the reclaim is deleted, or reclaims too much |
| `TestADarwinLayoutRefusalStopsTheBoot` (`internal/entrypoint`) | `runSteps` over a synthetic table with a failing `darwin_home_layout`: no later step runs on the darwin target, every later step runs on the container target, the last line is the [§10](#10-the-messages) text | the stop is deleted or leaks to the container boot |
| `TestCheckReportsADanglingLayoutLinkAndPruneRemovesIt` | the warning names link and target; `yolo prune` lists it; `--apply` removes the link and leaves a present target alone | either half is deleted |
| `TestNoShippedPackDeliversFilesIntoAnotherShippedPacksState` (`internal/packload`) | over the embedded set, no path-addressed `files` destination lies under another shipped pack's `[[state]]` | a shipped pack adopts the shape |
| `TestMacosUserAForeignParentDoesNotLockOutItsOwner` (`integration`, macos-user hardware) | launch A: a fixture pack `files` into `.probe/x`, no owner selected; launch B: a fixture declaring `[[state]] at = ".probe"`, exit 0 and `~/.probe` a link, or (without [OQ-FS3](#OQ-FS3)) a refusal naming only `~/.probe`; and A's launch log carries the warning | any of [§6](#6-the-foreign-parent-shape-say-it-early-forbid-it-nowhere-new) and [§7](#7-the-layout-refusal-name-the-path-stop-the-boot) regresses |
| `TestEveryHintedYoloCommandExists` (existing) | every `yolo …` in the new messages is a real command | a message names a verb that is not there |

Nothing here starts `agy`. The one measurement that needs it is [§5.2](#52-the-one-check-that-settles-it-and-who-runs-it)'s, and it is the maintainer's.

## 12. What this does not propose

- No change to how a nested `files` destination under a **selected** pack's state works; it works
  on every backend and this design relies on it.
- No reversal of the two-severities ruling: an unknown `agents` name stays fatal
  ([OQ-FS6](#OQ-FS6) only asks the maintainer to accept its consequence for their config).
- No `[[state]]` for `matt-agentwork` over `.gemini`: two packs claiming one state dir is a
  collision, not a fix.
- No general "deliver into the owning pack's location even when it is not selected": that is
  core learning which pack owns a path it was told not to know, and it resurrects the
  shipped-set reservation [`OQ-BH14`](../reference/jail-home.md#oq-bh14) removed.
- No change to the container boot's report-every-problem rule.
- No per-project `.agents/agents/` delivery by yolo ([FS-D1](#decision-ledger)).

## 13. Alternatives considered

| Alternative | Verdict |
| :--- | :--- |
| Keep path-addressed delivery and have the layout migrate a real `.gemini` into the sidecar | Rejected: [`OQ-HT2`](../reference/macos-user-home-tiers.md#oq-ht2) rules out migration of anything that might be the user's; [§6.3](#63-on-macos-user-record-what-the-overlay-makes) reclaims only what the record proves yolo alone wrote |
| Refuse any path-addressed `files` whose parent is another shipped pack's state | Rejected: reads the shipped set to reserve a name, against [`OQ-BH14`](../reference/jail-home.md#oq-bh14), and refuses a legitimate `yolo host apply` intent |
| Refuse the foreign-parent shape at the launch, over the selected set | Deferred to [OQ-FS2](#OQ-FS2); the design leans warn because the shape is harmless on two of three backends |
| A `register` into an agy settings list of agent paths | Not available: no such list is documented or found; becomes a refinement if one appears |
| Per-project `.agents/agents/` as the primary delivery | Rejected by the maintainer ([FS-D1](#decision-ledger)); kept as agy's own override |
| A soft audience (`agents_optional`) so the file is skipped where agy is absent | Rejected for this design: it reverses a standing ruling for one pack whose owner runs the agent everywhere; [OQ-FS6](#OQ-FS6) records the choice |
| A `flat` slot layout as the first answer to branch B | Deferred: `as` keeps the subdirectory's guarantees and solves the same case |

## 14. Risks

| Risk | Mitigation |
| :--- | :--- |
| agy's scanner names the agent for the directory (branch B) and `as` is not built yet | the paired-design brief names the agent `matt-agentwork` until it is; the file still loads |
| Selecting `agy` in the maintainer's user config changes every launch | selecting the pack lays its state dir, surfaces and the slot; whether and how the program installs is its own `[[program]]` declaration, which this design does not touch |
| The reclaim in [§6.3](#63-on-macos-user-record-what-the-overlay-makes) replaces a directory that held agent state the record did not see | the reclaim requires the directory's content to match the record exactly; one unknown entry leaves it alone |
| Stopping the boot hides a second, independent problem until the next launch | the layout is the first step, so the second problem was never visible in the same boot before either; the refusal says why nothing else ran |
| The warning fires on a pack whose foreign parent is deliberate at `yolo host apply` | it is a warning at exit 0, and declaring the dir as the pack's own state silences it |

## 15. Sequencing

In order, each a landable slice: the agy slot and its parity test; `matt-agentwork` switches to the
addressed form and the maintainer runs the [§5.2](#52-the-one-check-that-settles-it-and-who-runs-it)
check (branch B adds `as`); the per-path refusal and the boot stop; the overlay record and the
foreign-parent warning in lint, check and launch; the reclaim and the dangling-link housekeeping,
each behind its ruling. The implementation plan is not seeded here; the planner writes it once the
questions below are ruled.

## Open Questions

1. 💬 **OQ-FS1: Does the plain agy slot deliver the agent, or does the slot need to name the landing?**

   Your pack ships one Antigravity agent definition. With a slot at `.gemini/config/agents`, yolo
   lands the pack's tree at `agents/matt-agentwork/…`. Antigravity's docs show agents as
   `agents/<name>/agent.md` and do not say whether the name comes from the directory or the
   file's `name:` line. One `/agents` panel over the
   [§5.2](#52-the-one-check-that-settles-it-and-who-runs-it) probe files answers it.

   - **A — The plain slot.** The pack ships `agent.md` with `name: design-research`. One agent
     per pack.
   - **B — `as` on the contribution.** The landing is `agents/design-research/`; yolo gains one
     field and one refusal for two packs choosing one `as`.
   - **C — The plain slot, several agents.** Only if the panel lists the nested probe.

   <!-- vantage: question id=OQ-FS1 leaning="A if the panel lists probe-by-frontmatter; B if it lists probe-pack — the documented layout is a directory per agent, and A costs nothing to try first." -->

   _Leaning:_ A if the panel lists `probe-by-frontmatter`; B if it lists `probe-pack`. The
   documented layout is a directory per agent, and A costs nothing to try first.

   **Answer:**

   > _(empty — fill in when decided)_

2. 💬 **OQ-FS2: Should a pack's files into a folder nobody here owns warn, or refuse the launch?**

   A launch selects `matt-agentwork` and not `agy`. Today it runs and, on a Mac, quietly makes a
   real `~/.gemini` in the shared account home. On podman and Apple Container the same launch
   leaves nothing that outlives it.

   - **A — Warn.** `yolo check` and the launch print the [§10](#10-the-messages) warning and
     continue, on every backend alike.
   - **B — Refuse.** The launch stops with the same text as an error, on every backend, two of
     them over a harm they do not have.
   - **C — Refuse on macos-user only.** Stops where it hurts; a pack then passes on one backend
     and fails on another.

   <!-- vantage: question id=OQ-FS2 leaning="A — the shape is harmless on two backends and the intent at the host notch, so a refusal would be a macos-user rule in pack-rule clothing." -->

   _Leaning:_ A. The shape is harmless on two backends and the intent at the host notch, so a
   refusal would be a macos-user rule in pack-rule clothing.

   **Answer:**

   > _(empty — fill in when decided)_

3. 💬 **OQ-FS3: May the layout replace a directory yolo itself made for pack files, with nothing else in it?**

   A Mac launch without agy made `~/.gemini` to hold one delivered file. The next launch selects
   agy and needs a link there. The no-migration ruling says a real path is never removed for the
   user; this directory holds nothing the user wrote, and the overlay's record says so.

   - **A — Reclaim it.** The launch replaces the directory with the link and prints one line
     saying what it replaced and why it was safe. Anything unrecorded inside stops it.
   - **B — Never.** The launch refuses, names the one path and its maker, and gives
     `sudo rm -rf` of that path.

   <!-- vantage: question id=OQ-FS3 leaning="A — the ruling protects the user's data, and a directory yolo made whose every byte yolo delivered is not that; the content check keeps it from reaching anything else." -->

   _Leaning:_ A. The ruling protects the user's data, and a directory yolo made whose every byte
   yolo delivered is not that; the content check keeps it from reaching anything else.

   **Answer:**

   > _(empty — fill in when decided)_

4. 💬 **OQ-FS4: After the home layout fails on a Mac, should the bootstrap stop, or run on and report everything?**

   The refused launch in the lab report kept generating agy's config into the real `~/.gemini`
   after the layout step had refused. The container boot runs every step so one boot reports every
   problem; on macos-user the layout is the first step and every later one writes into what it lays.

   - **A — Stop.** The refusal is the layout's alone, with a last line saying nothing else ran.
   - **B — Run on, skipping only steps that write under the home.** More is reported; the skip
     list is a second thing to keep true.

   <!-- vantage: question id=OQ-FS4 leaning="A — the layout is the first step, so stopping loses nothing already reported, and every later failure would be the first one wearing a different label." -->

   _Leaning:_ A. The layout is the first step, so stopping loses nothing already reported, and
   every later failure would be the first one wearing a different label.

   **Answer:**

   > _(empty — fill in when decided)_

5. 💬 **OQ-FS5: Who removes a layout link whose workspace is gone?**

   Three links in the account home pointed at deleted integration workspaces; one refused a real
   launch until `sudo rm`. A launch that does not declare the link leaves it alone, by design.

   - **A — `yolo check` warns, `yolo prune --apply` removes the link.** Explicit, named, and in
     prune's dry-run shape; the target is never touched.
   - **B — The layout removes a dangling link it finds.** No step for the user, and a link to an
     unmounted volume is removed too.
   - **C — Leave it to the integration suite's cleanup alone.** Fixes the known maker and no
     other.

   <!-- vantage: question id=OQ-FS5 leaning="A, with the suite cleaning up its own links regardless — a dangling link is a judgment about a target yolo cannot see, which belongs in an explicit prune." -->

   _Leaning:_ A, with the suite cleaning up its own links regardless. A dangling link is a
   judgment about a target yolo cannot see, which belongs in an explicit prune.

   **Answer:**

   > _(empty — fill in when decided)_

6. 💬 **OQ-FS6: Will you select the agy pack everywhere matt-agentwork is, accepting that a launch without it is refused?**

   Addressing the file to `agy` makes every launch that selects `matt-agentwork` and not `agy`
   stop at the pre-flight: an `agents` name no selected pack provides is fatal, by a standing
   ruling. Your config does not select `agy` today.

   - **A — Select it.** `yolo pack add agy` in the user config; the agent's state, surfaces and
     the slot come with it on every notch.
   - **B — Keep the path-addressed delivery**, taking only the yolo-side fixes; the warning fires
     on every launch.
   - **C — A soft audience** that skips the file where the agent is absent, reversing the
     two-severities ruling for `files`.

   <!-- vantage: question id=OQ-FS6 leaning="A — you run agy everywhere, so the pack belongs everywhere, and an agent run outside yolo's management lacking the file is the 2026-09-28 ruling, not a defect." -->

   _Leaning:_ A. You run `agy` everywhere, so the pack belongs everywhere, and an agent run
   outside yolo's management lacking the file is the 2026-09-28 ruling, not a defect.

   **Answer:**

   > _(empty — fill in when decided)_

## Decision Ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| FS-D1 | The maintainer runs Argon through `agy` in every setting (host, jail, subprocess, direct) and every project, so the research child must be delivered wherever `agy` runs under yolo, on every notch and backend; a per-project `.agents/agents/` copy is rejected as the primary delivery and kept only as agy's own override. Their words: "I use argon in agy in all scenarios, host and jail subprocess and not. not sure how this could be per-project." | 2026-10-30 | [§4.1](#41-the-contribution) | — |
| FS-D2 | A path into another pack's state folder is an address, and the pack-side fix is `files` with `agents`; a nested delivery under a *selected* pack's state stays legal on every backend. Designer's decision. | 2026-10-30 | [§1](#1-the-verdict), [§4](#4-the-pack-side-change-address-the-agent) | — |
| FS-D3 | The agy `[[files]]` slot at `.gemini/config/agents` is a change to a pack yolo ships, so it is a yolo change, with `expects = ["agent.md"]`. Designer's decision. | 2026-10-30 | [§5.3](#53-the-slot-in-each-branch) | — |
| FS-D4 | `occupiedLayoutError` names each occupying account-home path with its own removal and its maker when known; the whole-account reset survives only when no path can be attributed and there are several. Designer's decision. | 2026-10-30 | [§7.1](#71-name-the-path) | — |
| FS-D5 | No new refusal of the foreign-parent shape on any backend; the same warning on all of them, and the macos-user overlay records what it creates. Designer's decision, confirmed by [OQ-FS2](#OQ-FS2). | 2026-10-30 | [§6](#6-the-foreign-parent-shape-say-it-early-forbid-it-nowhere-new), [§9](#9-parity-across-backends-and-notches) | — |

## Appendix: evidence, by symbol

Read 2026-10-30 at `e4345e69d`. Symbols rather than lines, so a move does not make this false.

- `files` slot and audience fields: `Contribution.Agent`, `Contribution.Agents`,
  `Contribution.Register`, `Contribution.Expects` in [`contributes.go`](../../internal/packdecl/contributes.go);
  `files` must name `into` or `agents`, never both (`validateContribution`); one slot per agent per
  pack and one addressed tree per agent per pack (`validateFilesDestinations`,
  `validateAddressedFiles`).
- Landing: `packload.SlotLanding` joins `<slot>/<pack>` for `files` only; `Registrations` and
  `ExpectsNotes` read it ([`registration.go`](../../internal/packload/registration.go)).
- Fatal unknown audience: `packload.AgentAudienceProblems`
  ([`agentaudience.go`](../../internal/packload/agentaudience.go)), called from `stagePacks` and
  `yolo host apply`; reported unmatched destination: `reportUnmatchedAudiences`.
- Jail mounts: `packFilesTargets` resolves through `ResolveDestinations`; `preparePackFiles` makes
  mountpoints in the skeleton or the workspace state dir; `PackDestConflicts` and
  `PackFilesShadowedSurfaces` are the existing `files` pre-flights, run at the launch and in
  `yolo check`'s Packs section ([`check/packs.go`](../../internal/cli/check/packs.go)).
- Host render: `RenderHostFiles` writes `into` contributions per file; a slot (`from` empty) is
  skipped there.
- macos-user: `InstallDarwinHomeLayout` runs as the first bootstrap step; `ensureLayoutSymlink`
  repoints yolo's links and never removes a real path; `occupiedLayoutError` prints the groups;
  `overlayLinks.route` refuses a link the layout did not lay with the text the lab report quotes;
  `openOverlayParentIn` creates a missing parent beneath the home or the sidecar;
  `writeOverlayInstallRecord` records what was delivered.
- Boot continues after a failed generator: `genStepAbout` warns and records; the refusal record
  is `BootRefusal` in [`bootrefusal.go`](../../internal/entrypoint/bootrefusal.go).
- Selection-only reservation: `resolveSelectedPacks`
  ([`selectedpacks.go`](../../internal/config/selectedpacks.go)) and its header's account of
  [`OQ-BH14`](../reference/jail-home.md#oq-bh14).
- Agy's discovery layout: Google's [Agents command](https://antigravity.google/docs/cli/commands/agents)
  page, as quoted in [§5.1](#51-what-agy-reads-and-what-nobody-has-measured); not measured against a
  running `agy`.
- This jail: `/home/agent/.gemini` is root-owned and holds only `config/agents/design-research.md`;
  `agy` is a root-owned binary in `~/.local/bin`; no agy pack is selected.
