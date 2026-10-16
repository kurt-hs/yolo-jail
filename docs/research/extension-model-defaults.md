---
title: "Models that follow you into pi's extensions — without yolo owning the extensions"
date: 2026-09-25
status: accepted
stage: BUILT
next: "Graduate into providers.md's tier-aliases section (system-doc), which already describes yolo.model_for and the role variables: fold the XM rulings into its why-appendix and keep this doc for the extension survey"
tags: [pi, extensions, models, subagents, workflows, tiers, defaults, research]
summary: "pi extensions that spawn agents mostly inherit the session's model, so yolo's default already follows them. The ones that don't keep their own role config (tiers, model classes, per-agent overrides) in files yolo never renders, and pi has no model-role concept to aim at. The proposal: core exposes yolo's existing tier aliases to pack derives, extensions get ADAPTER PACKS that someone other than yolo ships, the one adapter yolo ships today (pi-subagents' block in pi's derive) moves out, and a model-roles setting is proposed upstream to pi. Ruled and built 2026-09-28: the helper and a fourth alias, frontier; the block stays in pi's derive and is written for every provider; no upstream proposal. The role variables, YOLO_MODEL_<ROLE> per agent, were built 2026-10-01 once per-agent environment existed."
---

# Models that follow you into pi's extensions — without yolo owning the extensions

**The question this doc answers:** pi extensions (workflows, sub-agent runners) pick models for the
agents they launch. How does the default model a user carries everywhere reach those child agents,
when yolo can configure pi but not each extension, and must not take an opinion on which
extensions to support?

**Status:** 2026-10-01 — every ruling is built. On 2026-09-28: [OQ-XM1](#7-decision-ledger)'s helper
(`yolo.model_for`), [OQ-XM2](#7-decision-ledger)'s `frontier` and [OQ-XM3](#7-decision-ledger)'s `subagents` block for
every provider. On 2026-10-01: [OQ-XM4](#7-decision-ledger)'s role variables, `YOLO_MODEL_<ROLE>` per agent,
composed by core in the env-derive runner (`packload.ModelRoleVars`, called from
`packload.AgentEnv`) and delivered by the per-agent env files of
[OQ-CN6](../reference/providers.md#oq-cn6), the macos-user session and the host
exec. The implementation decisions are in the [Decision Ledger](#7-decision-ledger).
**MEASURED:** the model-selection code of eight
extensions and of pi 0.87.1, read from the published packages (versions in
[Appendix A](#appendix-a-evidence)); the role variables by tests only, through the credential gate,
a container launch's per-agent env files, `yolo host --`'s exec, and a real podman jail whose
files bash sources (`TestEachAgentReadsItsOwnProvidersTiersInTheJail`, a nested jail here, so
rootful and `--net=host`, neither of which this reads).
**UNMEASURED:** no extension was run, so none has been seen reading a role variable; no agent was
started; macos-user's delivery is by reading alone (it delivers the same composed list,
`AgentDelivery.Shape`, that the tested vehicles do).

**Rulings (all 2026-09-28, in review):** [OQ-XM1](#7-decision-ledger) (build the core helper), [OQ-XM2](#7-decision-ledger) (add `frontier`), [OQ-XM3](#7-decision-ledger) (the `subagents` block, for every provider, never crossing providers), [OQ-XM4](#7-decision-ledger) (role variables only once per-agent environment exists), [OQ-XM5](#7-decision-ledger) (no upstream proposal). Nothing here awaits a ruling.

**Reads with:** [`model-lists-and-pickers.md` §6](../design/model-lists-and-pickers.md#6-tier-aliases-default-fast-balanced)
(the tier aliases this reuses), [`providers.md`'s pi shortlist](../reference/providers.md#pi-enabledmodels-is-a-shortlist) (the default-model question of pi-model-selection-ux.md, since deleted,
which this reframes), [`providers.md`'s OQ-CN6](../reference/providers.md#oq-cn6)
(why a jail-wide env var was the wrong vehicle, and the per-agent env files the role variables
now ride).

---

## Defined terms

- **Inherit** — an extension that, when nothing names a model, gives the child agent the parent
  session's current model (pi's `ctx.model`).
- **Role** — a capability name an extension asks for instead of a model id: `fast`, `balanced`,
  `small`, `big`. pi has none; several extensions invent their own.
- **Tier alias** — yolo's existing name for the same idea on a provider: `default`, `fast`,
  `balanced` and, since [OQ-XM2](#7-decision-ledger), `frontier` in the provider's `models` map
  ([model-lists-and-pickers §6](../design/model-lists-and-pickers.md#6-tier-aliases-default-fast-balanced)).
- **Adapter pack** *(coined here)* — an ordinary yolo pack whose only job is to render yolo's
  resolved tier aliases into ONE extension's own config file. It needs no new pack kind: a
  `config` surface plus a derive does it (see [§4](#4-what-already-exists-to-build-on)).

## 1. The answer in short

- **Most extensions already follow the user.** Six of the eight read inherit when nothing names a
  model, and yolo already writes pi's `defaultProvider`/`defaultModel`/`enabledModels`. A user who
  never configured an extension's models gets yolo's default in every child agent today.
- **The failure is extensions with their own role config.** It lives in a file yolo never writes
  (`~/.pi/workflows/model-tiers.json`, `~/.pi/agent/config/pi-task-models/config.json`, pi-subagents'
  `subagents.agentOverrides`). A value set there does not move when the profile moves. And because
  `~/.pi` is per-workspace state in a jail, it doesn't follow the user to the next workspace either.
- **pi has no role concept to aim at.** Extensions get `ctx.model`, `ctx.scopedModels` and the
  registry, and nothing named "fast". So every extension that wants a cheaper child model invents
  a vocabulary: tiers `small`/`medium`/`big`, classes `fast`/`balanced`/`frontier`/`fav`.
- **yolo already solves this for claude.** Claude Code has roles built in (`opus`/`sonnet`/`haiku`,
  subagent `model: inherit`), and yolo's claude derive maps the provider's tier aliases onto them
  through `ANTHROPIC_DEFAULT_*_MODEL`. pi needs the same mapping. The difference is that pi's roles
  live in each extension rather than in the agent.

## 2. How the extensions pick models

Every row was read from the published package; paths and lines are in
[Appendix A](#appendix-a-evidence).

| Extension | When nothing names a model | Its own model config | Follows yolo's default? |
| :--- | :--- | :--- | :--- |
| `pi-subagents` 0.35.1 / 0.71.0 | inherits the parent session model | `subagents.defaultModel`, `modelScope`, `agentOverrides` in pi's `settings.json`; agent frontmatter `model:`. Bundled agents pin no model | **yes**, until the user sets `defaultModel` or an override |
| `pi-archimedes` subagent | agent `model` → call `model` → the active session model | per-agent overrides in `~/.pi/agent/agents.local.json` | **yes**, until a local override |
| `@quintinshaw/pi-dynamic-workflows` 3.13.0 | explicit → `tier` → session model when no tiers file; untagged → `medium` tier if configured | tiers in `~/.pi/workflows/model-tiers.json`, a per-project overlay | **yes** with no tiers file; **no** once tiers are set |
| `@arhen/pi-core-subagent` 1.3.56 | `ctx.model` | agent files' `model` | **yes** |
| `@bacnh85/pi-subagent` 0.22.6 | the authenticated parent model | explicit only | **yes** |
| `pi-prompt-template-model` 0.12.3 | the inherited model | a template's `models` list | **yes** |
| `@henryqw/pi-subagent` 22.2.0 | a `modelClass` route (`fast` by default) | via `@henryqw/pi-task-models` | **no**: needs the shared config |
| `@henryqw/pi-task-models` 7.0.2 | a class with no config is "missing" and warns | `~/.pi/agent/config/pi-task-models/config.json` | **no** |

`@henryqw/pi-task-models` is worth singling out. It is the ecosystem's own attempt at the missing
layer: a shared control plane where extensions declare tasks with a default class, and the user
maps each class to a model once. Today only its author's extensions consume it.

## 3. What pi offers, and what other agents do

**pi 0.87.1 has no roles.** Its model settings are `defaultModel`, `enabledModels` and
`modelThinkingLevels` (`docs/settings.md`). An extension's `ExtensionContext` carries `model`
(the current model), `scopedModels` (`enabledModels` resolved against the catalogue) and
`modelRegistry`. So "inherit" is well supported, and "give me the fast model" is not expressible.

**The other agents put roles in the agent:**

| Agent | Roles | Inherit | yolo today |
| :--- | :--- | :--- | :--- |
| Claude Code | `opus`, `sonnet`, `haiku` (and `fable`); a subagent's `model:` field | `inherit` from the main conversation; `CLAUDE_CODE_SUBAGENT_MODEL` forces one | maps tier aliases → `ANTHROPIC_DEFAULT_*_MODEL` |
| opencode | `small_model` for side tasks | a subagent with no model uses the invoking primary agent's | maps an alias → `small_model` |
| pi | none | via `ctx.model`, per extension | writes the default and the scope only |

So claude plugins that ship agents saying `model: haiku` already follow the user's provider,
because the role is resolved by the agent. That's why [§5](#5-options-with-verdicts)'s
mechanism doesn't need to reach claude or opencode plugins today.

## 4. What already exists to build on

- **The vocabulary.** Providers declare tier aliases (`default`, `fast`, `balanced`) in their
  `models` map; claude's and opencode's derives already consume them.
- **The delivery path.** A pack derive receives `ctx.providers`, `ctx.selected_provider` and
  `ctx.profile` ([pack-system reference](../reference/pack-system.md)). So a pack that declares a
  `config` surface for an extension's file can, today, resolve the selected provider's aliases and
  write them in that extension's format. An adapter is an ordinary pack. Not verified: that a
  non-pi pack may declare a surface under `~/.pi` without a reservation conflict; the collision
  rules suggest yes.
- **Git packs fetch at launch** (`packsrc.Store.Refresh`, pinned by `TestLaunchFetchesANeverInstalledGitPack`), so an adapter living in the extension author's own
  repo reaches a user by adding one `packs` entry.
- **The one adapter yolo already ships.** `packs/pi/derive.lua` writes pi-subagents' `subagents`
  block (`defaultModel`, a strict `modelScope`) in its `openai-codex` branch, added 2026-09-15
  (`d3360c00`, "constrain codex workflow models"). It is exactly an adapter, embedded in the
  agent's own pack and naming one extension. The pi default-model question (from the deleted
  pi-model-selection-ux.md; answered by [OQ-XM3](#7-decision-ledger)) asks what it should do for other providers. *Since 2026-09-28 ([OQ-XM3](#7-decision-ledger)) it stays
  in pi's derive and is written for every provider ([XM-D3](#XM-D3)).*

## 5. Options, with verdicts

| Option | Who writes what | An extension nobody adapted | Verdict |
| :--- | :--- | :--- | :--- |
| **(a) Adapter packs.** Core resolves roles for derives; an adapter pack renders them into one extension's file | core: a small derive helper. Adapters: the extension's author (a pack in their repo) or the user's local pack. yolo ships none | inherits (most do), or keeps its own config | **Take it**, as the main layer |
| **(b) A published env convention** (`YOLO_MODEL_FAST`, …) that extensions may read | core: the vars. Extensions: opt in | unaffected | **Defer.** A jail-wide var is wrong when two agents run different profiles, until per-agent env exists ([OQ-CN6](../reference/providers.md#oq-cn6)). *Built per agent on 2026-10-01 ([XM-D8](#XM-D8)).* |
| **(c) Upstream: a model-roles setting in pi** (and `ctx.modelFor(role)`) | pi. Then yolo's pi derive writes one key | follows once it adopts the setting | **Propose it**; the long-run fix, not ours to ship. *Ruled no on 2026-09-28 ([OQ-XM5](#7-decision-ledger)): yolo proposes nothing upstream, and adapters stay the way yolo reaches each extension's own settings.* |
| **(d) Inherit-first only.** yolo writes the default and scope; extensions that inherit follow | nothing new | follows if it inherits; diverges if configured | **Keep as the floor**, already true; not enough alone |
| **(e) Document and stop** | docs | diverges | **No.** The maintainer wants something layered |
| **(f) yolo ships adapters for chosen extensions** | yolo, per extension | — | **No.** It's the opinion and the responsibility the maintainer declined |

**Leaning: (d) as the floor, (a) as the layer, (c) proposed upstream, (b) later.** *Ruled
2026-09-28: (c) is not proposed ([OQ-XM5](#7-decision-ledger)), and piece 3 went the other way
([OQ-XM3](#7-decision-ledger)).* The pieces:

1. **Core, generic, small:** a derive helper that resolves a tier alias for the selected provider
   to a full `provider/model` id, the same resolution claude's derive does by hand. With it, an
   adapter is about ten lines of Lua. Core names no extension.
2. **yolo ships no adapters.** It documents the adapter contract and one worked example. The
   example lives in docs, not in `packs/`.
3. **The `subagents` block stays in pi's derive, written for every provider** ([OQ-XM3](#7-decision-ledger),
   ruled 2026-09-28; the leaning was to move it into an adapter pack yolo does not select by
   default). *"I want always the default to switch to the default. If it's not otherwise
   specified, it should switch to the default of that provider or whatever config … unless
   there's like some very clear exception stated. And regardless, it shouldn't be able to cross
   providers once we separate the providers per launch."* So `subagents.defaultModel` is the
   active profile's default model for the selected provider, and `subagents.modelScope` is strict
   and enforced over that provider's configured models: a child may name another model of the
   same provider, and never one of another provider ([XM-D3](#XM-D3), [XM-D4](#XM-D4)). That also
   answers the pi default-model question.
4. **Adapters compose with the default-models pack and `only`.** An adapter reads the same resolved
   aliases the pickers render, so a company pack's `only` narrows what an adapter can write too.
   Nothing reads a second list.

**What this does not fix.** An extension whose user edits its model config by hand in the jail
keeps that edit: an adapter's surface follows the same capture rules as any composed file, so an
in-jail edit is the user's. That's the intended behavior, not a gap.

## 7. Decision Ledger

The maintainer's rulings, all made in review on 2026-09-28, then the implementation decisions the
build took under them, each one the builder's to make.

| ID | Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| OQ-XM1 | **Ruled, as leaned:** core exposes tier resolution to pack derives. A derive helper resolves a tier alias for the selected provider to `provider/id`. It is the one core change, it names no extension, and claude's and opencode's derives can adopt it in place of their hand-written copies | 2026-09-28 | [§5](#5-options-with-verdicts) | ✅ `yolo.model_for` ([XM-D1](#XM-D1)); claude's and opencode's derives have not adopted it ([XM-D7](#XM-D7)) |
| OQ-XM2 | **Ruled, as leaned:** keep `default`, `fast` and `balanced`, and add `frontier` as a fourth conventional alias under the same warn-don't-refuse rule. Each adapter maps yolo's names onto its extension's; core maps none | 2026-09-28 | [Defined terms](#defined-terms) | ✅ `luahook.ConventionalModelAliases` ([XM-D2](#XM-D2)) |
| OQ-XM3 | **Ruled, amending the options:** the `subagents` block stays in the pi pack, written for every provider, none of (a) move it to an optional adapter pack, (b) move it to the maintainer's pack or (c) keep it for codex alone. *"regardless, it shouldn't be able to cross providers once we separate the providers per launch."* `defaultModel` is the active profile's default for the selected provider; `modelScope` is strict and enforced over that provider's configured models. It also answers the pi default-model question | 2026-09-28 | [§5](#5-options-with-verdicts), piece 3 | ✅ [XM-D3](#XM-D3) to [XM-D6](#XM-D6) |
| OQ-XM4 | **Ruled, as leaned:** no role environment variables until per-agent environment exists ([OQ-CN6](../reference/providers.md#oq-cn6)); then `YOLO_MODEL_<ROLE>`, set per agent | 2026-09-28 | [§5](#5-options-with-verdicts), option (b) | ✅ 2026-10-01: `YOLO_MODEL_DEFAULT`, `_FAST`, `_BALANCED`, `_FRONTIER` ([XM-D8](#XM-D8), [XM-D9](#XM-D9)) |
| OQ-XM5 | **Ruled, against the leaning:** no. yolo does not propose a model-roles setting to pi upstream; adapters stay the way yolo reaches each extension's own settings | 2026-09-28 | [§5](#5-options-with-verdicts), option (c) | — nothing to build |
| <a id="XM-D1"></a>XM-D1 | *Implementation decision.* **The helper is `yolo.model_for(alias)`, it answers for the SELECTED provider only, and it returns two values**: the model as `<provider>/<id>` and the bare id, or `nil`. It reads the same providers table `ctx.providers` is built from, so it needs no ctx argument. Another provider declaring the alias is never borrowed from, since a child handed that model would cross providers. The bare id is returned because agents spell a model differently (pi's own `defaultModel` is bare, pi-subagents' and opencode's are qualified) and a derive may normalize an id before qualifying it, as pi's does for kilo; the id is never parsed, so kilo's `vendor/model` ids survive whole. With no provider selected, or one the table has no row for, it returns `nil` silently. Like every `yolo.*` member it is a version boundary: an entrypoint older than it reads it through the tolerant guard, whose stub returns nothing, so a shipped derive treats `nil` as "resolved nothing" | 2026-09-28 | [OQ-XM1](#7-decision-ledger) | ✅ `e8820505` |
| <a id="XM-D2"></a>XM-D2 | *Implementation decision.* **Only the four conventional aliases warn, and the warning travels through a new `DeriveCtx.Warn`.** A missing `default`, `fast`, `balanced` or `frontier` is a warning naming the provider, the alias and the fix; any other name is open vocabulary and is silent when absent, because a derive may probe a name no provider is expected to declare (pi probes the profile's `model`, which may be an exact id). The four are one Go list, `luahook.ConventionalModelAliases`. The two surface-rendering paths, the boot loop and `yolo check`'s dry run, route `Warn` to the boot's once-per-message warning; the via-pointer scan and the host env composition leave it nil, as they already leave unknown-API notes to the boot | 2026-09-28 | [OQ-XM1](#7-decision-ledger), [OQ-XM2](#7-decision-ledger) | ✅ `e8820505` |
| <a id="XM-D3"></a>XM-D3 | *Implementation decision.* **One function writes the `subagents` block for every provider, codex included**, and the block keeps codex's shape: `defaultProvider`, `defaultModel` as `<provider>/<id>`, and `modelScope` `{enforce: true, strict: true, allow}` over the provider's configured ids, exactly ([ML-D5](../design/model-lists-and-pickers.md#ML-D5)). For a provider with a model list, `allow` is the same distinct ids `enabledModels` holds, kilo's normalized. The child's default is the model the chat selection starts on, so the two cannot drift. codex's default stays `codexDefault`, the one rule its consumers share, because the declared list carries no `default` alias; every other provider resolves the profile's `model` alias, then `default`, through `yolo.model_for`, asked only of a provider that declares a model list, since a provider with none has no alias to miss. The rendered codex block is byte-identical to before | 2026-09-28 | [OQ-XM3](#7-decision-ledger) | ✅ `58fc65ce` |
| <a id="XM-D4"></a>XM-D4 | *Implementation decision.* **A provider with no configured models gets the provider-level scope `<provider>/*`**, strict and enforced, rather than no scope or an empty `allow`. The options: no scope lets a child name any provider's model, which the ruling forbids ("regardless, it shouldn't be able to cross providers"); an empty `allow` is refused by pi-subagents 0.35.1 as a settings error (`parseModelScopeConfig`: "expected a non-empty array of patterns"), and an enforced scope with none would refuse every child. `<provider>/*` keeps the one guarantee the ruling makes unconditional and allows the rest of the ruling's exception, another model of the same provider. pi-subagents turns `*` into `.*`, so it also matches ids with slashes of their own (`kilo/deepseek/deepseek-v4.1-flash`). It applies to openrouter and kilo as shipped, to a kilo profile that names a model (the scope is the provider, not that one model: yolo does not know kilo's models), and to `openai-codex` with its list removed, where it replaces [ML-D5](../design/model-lists-and-pickers.md#ML-D5)'s "an empty list writes no `modelScope`" | 2026-09-28 | [OQ-XM3](#7-decision-ledger) | ✅ `58fc65ce` |
| <a id="XM-D5"></a>XM-D5 | *Implementation decision.* **When yolo can name no default, `subagents.defaultModel` is tombstoned, not omitted.** That is a provider with no model list whose profile names no model, or a list with no alias the profile resolves to. Omitted, a lower layer's value would stand: `pi/settings` reads the host's `settings.json`, and a host that keeps a codex policy would start every openrouter child on a codex model, which pi-subagents only warns about for an inherited model (`checkModelScope`, severity `warn`). Deleted, the child inherits the parent session's model, which is on the selected provider. The rest of a host's `subagents` object, such as `disableBuiltins`, is kept. At the host nothing changes: `yolo host apply` renders no derive's content ([`host-agent-environment.md`'s computed layer at the host](../reference/host-agent-environment.md#what-yolo-host-apply-renders-into-a-derived-surface)), and the proposal that would let it drops a tombstone before writing ([HC-D10](../design/host-computed-layer.md#HC-D10)) | 2026-09-28 | [OQ-XM3](#7-decision-ledger) | ✅ `58fc65ce` |
| <a id="XM-D6"></a>XM-D6 | *Implementation decision.* **The block stays a computed key, and a profile switch needs no clearing logic.** The selection mechanism lifts scalars and arrays of scalars only ([Selection](../reference/providers.md#selection-write-on-activation-clear-only-what-yolo-wrote)), so [OQ-PSW2](../reference/providers.md#oq-psw2)'s deselect rule does not govern it. A computed key is re-asserted every boot while a profile is active, and each switch rewrites every leaf yolo names, the `allow` array whole; a deselect writes no block, and the file is recomposed from its layers without it. Measured through the boot render, codex → zai → codex → zai and codex → openrouter → codex → openrouter, then a deselect and a reselect: each boot leaves exactly the new provider's block | 2026-09-28 | [OQ-XM3](#7-decision-ledger) | ✅ `58fc65ce` |
| <a id="XM-D7"></a>XM-D7 | *Implementation decision.* **claude's and opencode's derives do not adopt the helper in this change**, because neither is a clean swap. claude reads the vendor aliases `sonnet` and `haiku`, and moving it to `balanced`/`fast` is [OQ-PSW1](../design/model-lists-and-pickers.md#OQ-PSW1), open when this was written and decided on 2026-09-30 as [MM-D17](../design/model-lists-and-pickers.md#MM-D17) (not built). opencode's `small_model` tries `haiku`, `fast` and `small` in turn, so its lookup is a chain of names rather than one tier | 2026-09-28 | [OQ-XM1](#7-decision-ledger) | — |
| <a id="XM-D8"></a>XM-D8 | *Implementation decision.* **Core composes the role variables, for every agent with a profile, in the env-derive runner, and they ride that runner's output to every vehicle.** `packload.AgentEnv` adds `packload.ModelRoleVars` beside its pack's `yolo.env` output, so the per-agent env file, the macos-user session and `yolo host --`'s exec deliver them with no new reader, and an agent whose pack registers no `yolo.env` gets them too: the vocabulary (`luahook.ConventionalModelAliases`) and the lookup are core's, and a relay each agent pack had to remember is one each could forget. The names are `YOLO_MODEL_` plus the alias upper-cased, for the four conventional aliases only: an open-vocabulary alias is a name a derive may probe, not a role yolo publishes ([OQ-XM2](#7-decision-ledger)). The value is `<provider>/<id>`, `yolo.model_for`'s qualified return, read through the same lookup (`luahook.ModelAliasID`), so the two cannot disagree. The provider is the agent's PRIMARY's, never a later entry of its active set, as `yolo.model_for` answers with no provider named. A variable of the same name the agent's own pack sets, a tombstone included, wins, since the pack is the more specific statement about its own agent's process. `yolo check` composes none, because its gate input runs no derive (`ScopeInput.NoDerives`) | 2026-10-01 | [OQ-XM4](#7-decision-ledger) | ✅ built 2026-10-01 |
| <a id="XM-D9"></a>XM-D9 | *Implementation decision.* **A role the agent's provider does not name is removed, and only when some provider in the launch's table names it; a missing role is silent.** An agent started by another agent inherits that one's environment, so a `YOLO_MODEL_FAST` left by an agent on another provider would hand a child a model across providers, which [XM-D1](#XM-D1) forbids; the per-agent file writes the removal only for a value yolo set elsewhere, so a value the user typed stays ([OQ-CN8](../reference/providers.md#oq-cn8)), and the host exec removes it outright, as it applies every composed value. A role no provider in the table names is not touched: nothing the launch composes could have set it. Nor is an agent with no profile: it selects no provider, the gate composes no delivery for it, and so it writes no file and its launch removes nothing, which leaves one started by a profiled agent holding that agent's values, as [CN-D8](../design/provider-credential-scope.md#CN-D8) accepts for every inherited value (found in review). And unlike `yolo.model_for`, nothing warns: no derive asked for the role, so a warning would name a gap nobody relies on, at every launch | 2026-10-01 | [OQ-XM4](#7-decision-ledger) | ✅ built 2026-10-01 |

## Appendix A: evidence

Read 2026-09-25. npm packages from their published tarballs; git extensions from the checkouts in
this jail's `~/.pi/agent/git` (read-only).

- **pi-subagents 0.35.1** (installed): `src/runs/shared/model-fallback.ts:214-237`
  (`resolveEffectiveSubagentModel`: explicit, else agent model, else the parent model);
  `src/agents/agents.ts:748-779` (`subagents.defaultModel`, `modelScope`, `agentOverrides`);
  `agents/*.md` set `thinking` only, no `model`.
- **pi-subagents 0.71.0** (npm latest): `agents/*.md` pin no `model`.
- **pi-archimedes** (`c7878f0`): `packages/subagent/src/spawn.ts:207-208` ("agent.model >
  options.model > options.activeModel"); `local-config.ts:19` (`agents.local.json`).
- **@quintinshaw/pi-dynamic-workflows 3.13.0** (`bd9245d`): `src/agent.ts:212-236`
  (`resolveAgentModelSpec`); `src/model-tier-config.ts:66` (`~/.pi/workflows/model-tiers.json`),
  `:185-209` (defaults ranked from the registry).
- **@arhen/pi-core-subagent 1.3.56**: `src/manager.ts:140` (`resolveChildModel` returns `ctx.model`).
- **@bacnh85/pi-subagent 0.22.6**: `extensions/model.ts:9,68` (falls back to the authenticated
  parent model).
- **pi-prompt-template-model 0.12.3**: `subagent-step.ts:264` (the inherited model).
- **@henryqw/pi-subagent 22.2.0**: `README.md:173` (`modelClass`), `CONTEXT.md:67` (route precedence).
- **@henryqw/pi-task-models 7.0.2**: `README.md` Config section
  (`~/.pi/agent/config/pi-task-models/config.json`), `:119,127` (a missing file is `"missing"`).
- **pi 0.87.1** (`@earendil-works/pi-coding-agent`): `dist/core/extensions/types.d.ts`
  (`ExtensionContext.model`, `.scopedModels`, `.modelRegistry`); `docs/settings.md:12-16`.
- **Claude Code docs** (`docs.claude.com/en/docs/claude-code/sub-agents`, fetched 2026-09-25):
  built-in subagents inherit the main conversation's model unless `CLAUDE_CODE_SUBAGENT_MODEL` is set.
- **opencode docs** (`opencode.ai/docs/agents`, fetched 2026-09-25): a subagent with no model
  uses the model of the primary agent that invoked it.
- **yolo:** `packs/claude/derive.lua:162-169,296-306`; `packs/opencode/derive.lua:178-187`;
  `packs/pi/derive.lua:492-509` (the `subagents` block, added in `d3360c00`).
