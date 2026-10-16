---
title: "How pi's extensions pick models for the agents they launch"
date: 2026-09-25
status: accepted
stage: GRADUATED
tags: [pi, extensions, models, subagents, workflows, tiers, research]
summary: "A survey of how eight pi extensions that spawn agents choose a child's model, and what pi 0.87.1 and the other agents offer. Most inherit the session model; the rest keep their own role config in files yolo never renders, and pi has no model-role concept. The rulings it led to (OQ-XM1 to XM5) and their build ledger (XM-D1 to XM-D9) graduated into providers.md."
---

# How pi's extensions pick models for the agents they launch

**Status:** GRADUATED, 2026-10-16. What yolo built from this survey, the `yolo.model_for` helper,
the `frontier` alias, pi-subagents' `subagents` block and the `YOLO_MODEL_<ROLE>` variables, and
its rulings [OQ-XM1](../reference/providers.md#oq-xm1) to
[OQ-XM5](../reference/providers.md#oq-xm5) with the
[XM-D build ledger](../reference/providers.md#the-extension-model-build-ledger), now live in
[`providers.md`](../reference/providers.md#tier-aliases). What stays here is the survey, read from
the published packages on 2026-09-25 (versions in [Appendix A](#appendix-a-evidence)). No
extension was run, so none has been seen reading a role variable.

## Defined terms

- **Inherit** — an extension that, when nothing names a model, gives the child agent the parent
  session's current model (pi's `ctx.model`).
- **Role** — a capability name an extension asks for instead of a model id: `fast`, `balanced`,
  `small`, `big`. pi has none; several extensions invent their own.
- **Tier alias** and **adapter pack** — yolo's names, defined in
  [`providers.md`'s tier aliases](../reference/providers.md#tier-aliases). An adapter pack is a
  term this survey coined.

## The finding in short

- **Most extensions follow the user already.** Six of the eight inherit when nothing names a
  model, so yolo's pi `defaultProvider`/`defaultModel` reaches their child agents.
- **The failure is extensions with their own role config**, in files yolo never writes
  (`~/.pi/workflows/model-tiers.json`, `~/.pi/agent/config/pi-task-models/config.json`,
  pi-subagents' `subagents.agentOverrides`). A value set there does not move when the profile
  moves, and since `~/.pi` is per-workspace state in a jail it does not follow the user to the
  next workspace either.
- **pi has no role concept to aim at.** Extensions get `ctx.model`, `ctx.scopedModels` and the
  registry, and nothing named "fast", so each extension that wants a cheaper child model invents a
  vocabulary: tiers `small`/`medium`/`big`, classes `fast`/`balanced`/`frontier`/`fav`.
- **Claude Code has roles built in** (`opus`/`sonnet`/`haiku`, subagent `model: inherit`), so
  the agent resolves them; pi's roles live in each extension instead.

## How the extensions pick models

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

## What pi offers, and what other agents do

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
because the role is resolved by the agent. That is why yolo's [adapter-pack contract](../reference/providers.md#extensions-that-launch-agents)
needs to reach no claude or opencode plugin.

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
