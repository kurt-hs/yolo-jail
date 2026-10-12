---
title: "A provider an agent already has: who may select it, and what a model is called"
date: 2026-10-12
status: draft
stage: DESIGN
next: "Rule OQ-PR1 (does the provider-table gate agree about a built-in provider?) and OQ-MN1 (is `name` the naming surface?) — each decides a whole topic below"
tags: [providers, profiles, models, built-in-providers, display-names, packs]
summary: "Two questions the 2026-10-05 built-in-provider ruling opens, and they are one seam seen twice. A provider an agent ships its own client for is handled by that agent's derive and named on the launch line, but the provider-table gate every launch reduces through does not know what one is, so a profile that names it is refused. And a model the agent's own catalog defines has nowhere for a friendly display name to live, because no catalog row is written for that provider. This doc asks the questions rather than settling them."
vantage:
  status-chip: true
---

# A provider an agent already has: who may select it, and what a model is called

**Status:** 2026-10-12. **DRAFT** — no question below is ruled and nothing here is proposed for build.
Evidence read against the tree at `ab9cf2f90`. The two topics are one seam seen twice: yolo writes
nothing for a provider an agent has built in, so other mechanisms have to decide what that means,
and today they disagree.

> **In short.** The 2026-10-05 [OQ-3](pi-codex-provider-shadowing.md#OQ-3) ruling made yolo write no
> model entry over any provider an agent has built in. The derives, the launch profile line and the
> via gate were all built to honor it; the provider-table gate that every launch reduces through was
> not. So a profile naming one of pi's 43 built-in providers — or omp's 49, or opencode's 225 — is
> refused before any of them runs. The same seam leaves a friendly model name with nowhere to live
> when the model comes from the agent's own catalog.

**Why it matters.** [OQ-3](pi-codex-provider-shadowing.md#OQ-3)'s ruling says a profile whose plan
needs another address selects *the agent's own provider for that plan*. Today that promise holds
only for built-in names a pack also declares. A name the agent has and no pack ships — pi's `meta`
is the example — cannot be selected at all, and the refusal is the wrong sentence: it says the
provider is missing, when the provider is right there in the agent.

**The shape.** [§1](#1-one-seam-two-mechanisms) states the ruling and the seam. [§2](#2-topic-a-the-provider-table-gate-does-not-know) and
[§3](#3-topic-a-the-consequence-of-closing-it) are the reachability topic; [§4](#4-topic-b-what-already-exists-for-a-model-name)
and [§5](#5-topic-b-where-a-name-has-nowhere-to-live) the naming topic. Questions are in
[§8](#8-open-questions); [§7](#7-amendments-and-what-is-merely-relied-on) says which existing
rulings each topic would amend.

**Cost.** Nothing here is free. Letting the gate admit every built-in name makes 317 names
selectable across three agents at once ([§3](#3-topic-a-the-consequence-of-closing-it)), and moves
some failures from launch time to the first request. A name that can attach to a model no row
describes needs a home the current composed-table shape does not have ([§5](#5-topic-b-where-a-name-has-nowhere-to-live)).

**Start at [§1](#1-one-seam-two-mechanisms)** — the seam. Both topics fall out of it.

**Needs your ruling:** [OQ-PR1](#OQ-PR1), [OQ-PR2](#OQ-PR2), [OQ-PR3](#OQ-PR3), [OQ-PR4](#OQ-PR4), [OQ-PR5](#OQ-PR5), [OQ-MN1](#OQ-MN1), [OQ-MN2](#OQ-MN2), [OQ-MN3](#OQ-MN3), [OQ-MN4](#OQ-MN4).

**Reads with:** [`pi-codex-provider-shadowing.md`](pi-codex-provider-shadowing.md) (the ruling this
doc develops — its [OQ-4](pi-codex-provider-shadowing.md#OQ-4) is adjacent to [§5](#5-topic-b-where-a-name-has-nowhere-to-live)),
[`model-lists-and-pickers.md`](model-lists-and-pickers.md) (the aliases and display names this doc
builds on — it says "code claims cite a symbol, never a line"; this doc cites lines on purpose, as
a draft nothing has verified yet), and [`../reference/providers.md`](../reference/providers.md) (the
provider and profile reference).

---

## 1. One seam, two mechanisms

[OQ-3](pi-codex-provider-shadowing.md#OQ-3) was ruled 2026-10-05, the broad reading, and built as
[BI-D1](pi-codex-provider-shadowing.md#BI-D1) to [BI-D12](pi-codex-provider-shadowing.md#BI-D12). In
the maintainer's words it settled three things:

- each agent pack declares the **names** of its built-in providers, "since core knows no agent:
  yolo keeps names, never models";
- yolo writes **no model entry** over one, so the agent runs it on its own client and model list;
- where a profile's plan needs a different address, yolo selects **the agent's own provider for that
  plan** when it has one, "and the launch says when it has none".

The declaration is `built_in_providers` on a program pack: `names`, `plans` (a yolo provider mapped
to the agent's own provider for that plan, or `null`), and `yolo_lists` for a provider the agent
runs on its own client but on yolo's list
([`internal/packdecl/builtinproviders.go`](../../internal/packdecl/builtinproviders.go) lines
[18–43](../../internal/packdecl/builtinproviders.go#L18-L43); pi's list and its `yolo_lists` are
[`packs/pi/pack.json`](../../packs/pi/pack.json) lines
[16–30](../../packs/pi/pack.json#L16-L30)).

Three readers were built downstream of composition:

| Reader | What it does | Where |
| :--- | :--- | :--- |
| the agent's own derive | writes no catalog row for a built-in name; selects the agent's own id for a plan | `piOwn` [`packs/pi/derive.lua`](../../packs/pi/derive.lua#L356), `opencodeOwn` [`packs/opencode/derive.lua`](../../packs/opencode/derive.lua#L58), `ompOwn` [`packs/omp/derive.lua`](../../packs/omp/derive.lua#L82), `codexOwn` [`packs/codex/derive.lua`](../../packs/codex/derive.lua#L299) |
| the launch's profile line | prints "through its own `<id>` client, with its own model list" | `profileReach` [`internal/packload/profiledisclosure.go`](../../internal/packload/profiledisclosure.go) lines [217](../../internal/packload/profiledisclosure.go#L217), [227](../../internal/packload/profiledisclosure.go#L227) |
| the via gate | says a via row repoints nothing, and why, with the way to route it anyway | [`internal/wirebridged/launchrefusal.go`](../../internal/wirebridged/launchrefusal.go#L117) |

And a fourth reader was not: **the provider-table gate**. `refuseUnspeakableProvider` asks whether
the composed table holds the selected provider, and on "no" calls `missingProvider`, which refuses
([`internal/packload/protocolresolution.go`](../../internal/packload/protocolresolution.go) lines
[561](../../internal/packload/protocolresolution.go#L561), [566–572](../../internal/packload/protocolresolution.go#L566-L572),
[247](../../internal/packload/protocolresolution.go#L247)). It never calls `BuiltInProviderFor`
([`internal/packload/builtinproviders.go`](../../internal/packload/builtinproviders.go#L69)). The
gate runs above the derive, in the one runner both notches reduce through
([`internal/packload/deriveenv.go`](../../internal/packload/deriveenv.go#L142)), so the derive's own
handling never gets to run.

**The seam, in one sentence.** Four mechanisms answer "what is a provider an agent has built in?";
three read the declaration, and the fourth answers from the composed table alone.

---

## 2. Topic A: the provider-table gate does not know

**Walk it with pi's `meta`.** `meta` is one of pi's names ([`packs/pi/pack.json`](../../packs/pi/pack.json#L21)).
No pack yolo ships declares a provider called `meta` — the shipped provider set is `openai-codex`
([`packs/openai-auth`](../../packs/openai-auth/pack.json)), `zai`, `openrouter`, `llamacpp`,
`cerebras`, `kilo` and `bedrock` (`rg '"kind": "provider"' packs/*/pack.json`).
So a launch with a profile selecting `meta` for pi composes a table that does not hold it, and:

1. config validation passes — a profile's `provider` is checked for shape, not existence
   ([`internal/config/profiles.go`](../../internal/config/profiles.go) lines
   [117](../../internal/config/profiles.go#L117), [138](../../internal/config/profiles.go#L138));
2. the gate refuses, before any derive runs ([`deriveenv.go:142`](../../internal/packload/deriveenv.go#L142));
3. the refusal is `MissingProviderError`'s default wording: "no selected pack ships it, no pack yolo
   ships declares it" ([`protocolresolution.go:207`](../../internal/packload/protocolresolution.go#L207),
   constructed at [:247](../../internal/packload/protocolresolution.go#L247)).

That sentence is false for pi, and the fix it names — "declare it under `providers`" — is exactly
what the ruling forbids yolo relying on.

> [!WARNING]
> **Only one reader handles a table-absent built-in name, not two.** `profileReach` warns "this
> launch's provider table does not hold it" and **returns** at
> [`profiledisclosure.go:204`](../../internal/packload/profiledisclosure.go#L204), so its built-in
> branch at [:217](../../internal/packload/profiledisclosure.go#L217) is only ever reached for a
> built-in name the table *does* hold. Only the derive would handle a table-absent name — and the
> gate refuses before the derive runs. So closing this is not making the gate agree with two working
> readers; it is making the gate and the launch line agree with the derive, and giving the launch
> line a branch it does not have.

**So the actual question is [OQ-PR1](#OQ-PR1):** should the provider-table gate treat a built-in
name as reachable, the way the derive does? Not "should yolo auto-detect providers" — nothing is
discovered. The agent's pack declares the names; core reads a declaration, which is the whole design
of [BI-D1](pi-codex-provider-shadowing.md#BI-D1).

---

## 3. Topic A: the consequence of closing it

**Every built-in name becomes selectable at once.** Under [BI-D2](pi-codex-provider-shadowing.md#BI-D2)
each pack lists every provider id its agent's code registers: pi 43, omp 49, opencode 225, codex 5
([`packs/pi/pack.json`](../../packs/pi/pack.json#L17-L26),
[`packs/omp/pack.json`](../../packs/omp/pack.json#L15-L24),
[`packs/opencode/pack.json`](../../packs/opencode/pack.json#L20-L56),
[`packs/codex/pack.json`](../../packs/codex/pack.json#L23)). A gate that admits a built-in name
makes all 317 selectable, in three agents, in one change.

**It is agent-relative, and must stay so.** pi reaches `meta` through its own client; claude cannot
reach `meta` at all without an endpoint and a credential — and claude declares no
`built_in_providers` ([`packs/claude/pack.json`](../../packs/claude/pack.json) has none), so for
claude the gate's refusal is still correct. The rule is per (agent, provider, plan), which is exactly what the declaration holds.

**The reader count is already wrong before this doc.** Two lists disagree about how many readers the
declaration has:
[BI-D3](pi-codex-provider-shadowing.md#BI-D3) names "the boot and `yolo check`, the via-pointer scan,
and the env derive"; [§6.5](pi-codex-provider-shadowing.md#65-what-the-build-decided) names "every
derive's `ctx.built_in_providers`, the launch's profile line, and the agent's environment". Neither
names the via gate's own read at
[`launchrefusal.go:117`](../../internal/wirebridged/launchrefusal.go#L117), so by one counting the
via gate is already an undeclared fourth. Adding the protocol gate makes it four by
[§6.5](pi-codex-provider-shadowing.md#65-what-the-build-decided)'s counting or five by
[BI-D3](pi-codex-provider-shadowing.md#BI-D3)'s. **This doc would amend
[BI-D3](pi-codex-provider-shadowing.md#BI-D3)** to state one reader set, containing every core read,
rather than two lists that each claim three.

**Residual risk, stated plainly.** An endpoint-less built-in selected for an agent that cannot
actually reach it moves from a launch-time refusal to a **runtime auth failure**. Today, a profile
naming a provider the agent has but cannot use stops the launch. Afterward it starts the agent, and
the first request fails — for pi's `meta`, an authentication error the agent prints inside the
session, not a refusal yolo owns. Three things follow, and the first is a question
([OQ-PR4](#OQ-PR4)):

- the launch line must say an endpoint-less built-in is a **routing** statement, not an availability
  one: that nothing this launch configures delivers a credential to it, naming the login or variable
  the agent's own client reads. `profileReach`'s built-in branch
  ([`profiledisclosure.go:217`](../../internal/packload/profiledisclosure.go#L217), route at
  [:227](../../internal/packload/profiledisclosure.go#L227)) returns before the credential half, so
  it says the route and not whether a credential reaches it — unlike the catalogued-provider path,
  whose credential warning runs at
  [`profiledisclosure.go:281`](../../internal/packload/profiledisclosure.go#L281);
- [R3](pi-codex-provider-shadowing.md#8-risks-and-invariants) changes shape: under
  [OQ-3](pi-codex-provider-shadowing.md#OQ-3) a stale list costs only a collision with a provider of
  that name, but a gate that reads the list makes staleness decide **selectability** — a provider an
  agent added, not yet in its pack's list, would be refused. **This doc would amend
  [R3](pi-codex-provider-shadowing.md#8-risks-and-invariants)** to say so;
- an arm this doc does not propose, recorded only to be ruled out explicitly: refusing at launch an
  endpoint-less built-in the launch cannot find a credential for. It would need a per-agent map from
  a built-in name to the credential it reads, which yolo does not have and
  [BI-D1](pi-codex-provider-shadowing.md#BI-D1) deliberately keeps out of core ("yolo keeps names").

---

## 4. Topic B: what already exists for a model name

Naming a model is not a new mechanism. Five facts are already in the tree, and the naming gap is
where the sixth would go.

| Fact | What it is | Where |
| :--- | :--- | :--- |
| Alias | a resolution key: `models` maps an alias to a wire id. The alias is what a profile's `model` may name | [`internal/packdecl/models.go`](../../internal/packdecl/models.go#L4), [ML-D1](model-lists-and-pickers.md#ML-D1), [§7.1](model-lists-and-pickers.md#71-the-kind) |
| Display name (pack) | `ModelEntry.Name` — "the display name a picker shows; absent, the agent shows its own catalog's name for the id" | [`internal/packdecl/models.go`](../../internal/packdecl/models.go) lines [35](../../internal/packdecl/models.go#L35), [47–49](../../internal/packdecl/models.go#L47-L49) |
| Display name (user) | the object form `providers.<name>.models.<alias>.name`, closed field set | [`internal/config/config.go:221`](../../internal/config/config.go#L221) (`knownModelKeys`), [`internal/config/validate.go:1409`](../../internal/config/validate.go#L1409) (`validateModelEntry`) |
| The composed shape | object-form facts lower into `model_options.<alias>`, the flat map every derive reads | `liftModelFacts` [`internal/packload/providers.go`](../../internal/packload/providers.go#L212) |
| The rule | a row's display name is the entry's own `name` fact, else absent so the agent's catalog name shows — **never the yolo alias** | [MM-D7](model-lists-and-pickers.md#MM-D7) |

**The `models` kind is not a catalog and does not need one.** It "SHAPES a provider's model list
without owning the provider", and it may name **any** provider
([`internal/packdecl/models.go`](../../internal/packdecl/models.go#L4)). That is the pack-side
surface, and it is why the maintainer's "no model catalog in core" constraint is already satisfied:
a name is declared on a provider entry, by a user or a pack, and core only shape-checks it
([`internal/config/validate.go`](../../internal/config/validate.go#L1409)).

**The visible field is each agent's own.** Where a row is written, the name lands as:

| Agent | Field | Where |
| :--- | :--- | :--- |
| pi | `models[].name` in the `models.json` row / registration | [`packs/pi/derive.lua:698`](../../packs/pi/derive.lua#L698) |
| opencode | `provider.<id>.models.<id>.name` | [`packs/opencode/derive.lua:684`](../../packs/opencode/derive.lua#L684) |
| oh-omp | the `models.yml` row's `name` | [`packs/omp/derive.lua:149`](../../packs/omp/derive.lua#L149) |
| codex | `display_name` in `model_catalog_json` | `model_menu.name` [`packs/codex/pack.json:70`](../../packs/codex/pack.json#L70), applied [`internal/modelmenu/modelmenu.go:113`](../../internal/modelmenu/modelmenu.go#L113) |
| claude | `modelPicker.options[].label` | [`packs/claude/derive.lua:644`](../../packs/claude/derive.lua#L644) |

**Can a user name a model today? Yes — for a provider whose row is written.** The surface is the
user's own `providers.<name>.models.<alias>` object form with `name`
([`config.go:221`](../../internal/config/config.go#L221), accepted by
[`internal/config/validate.go:1409`](../../internal/config/validate.go#L1409) and lowered by
[`liftModelFacts`](../../internal/packload/providers.go#L212)). The `kind: "models"` contribution is
pack-only — it is a pack manifest field, not a user config key. So the *ability* exists; what does
not is a name for a model whose provider yolo writes no row for. That is [§5](#5-topic-b-where-a-name-has-nowhere-to-live).

---

## 5. Topic B: where a name has nowhere to live

The root cause is [§1](#1-one-seam-two-mechanisms)'s: **a built-in provider gets no catalog row**, so
there is nothing for a `name` to attach to. pi's `models` derive skips the whole row for a built-in
name ([`packs/pi/derive.lua:645`](../../packs/pi/derive.lua#L645)), and `modelDisplayName`
([`packs/pi/derive.lua:299`](../../packs/pi/derive.lua#L299)) is only called from inside the row
builder. A user's `providers.openrouter.models.<id>.name` on pi therefore composes into the table and
is then **dropped**, because pi's own `openrouter` catalog is what shows.

> [!NOTE]
> **The one channel that does reach a built-in provider's list — and why it is not enough.** pi's
> `model-lists` registration *does* run for a built-in provider when the list was narrowed
> (`models_only`) or carries routing facts (`model_routing`):
> [`packs/pi/derive.lua:1552`](../../packs/pi/derive.lua#L1552) takes `own ~= false` and
> `type(own) == "table"` as usable, and the registration is gated on
> `prov.models_only == true or hasRouting` at
> [`packs/pi/derive.lua:1568`](../../packs/pi/derive.lua#L1568), writing `name` from the entry. That
> is the documented path for a user adding facts to pi's built-in `openrouter`, whose
> `openrouter_routing` fact a plain user config may declare. It requires declaring routing, or
> narrowing the menu, to carry a name — the wrong price for "call this model something nicer in the
> picker".

**The gap, precisely.** There is no surface on which a name attaches to a model that a built-in
provider's own catalog defines, without also changing the menu by narrowing it or declaring routing.
The maintainer wants the ability for users and for packs ("It could come from an upstream pack"), and
eschews shipping renames of the agent's own catalog. Both are satisfiable only if a name can live
somewhere other than a catalog row.

**This overlaps an open ruling, and this doc defers to it.** [OQ-4](pi-codex-provider-shadowing.md#OQ-4)
(open) asks whether the ruling reaches *a list* a pack declares for a built-in provider. Naming a
model on such a provider is the same territory. [OQ-MN2](#OQ-MN2) below should not be ruled before
[OQ-4](pi-codex-provider-shadowing.md#OQ-4); if [OQ-4](pi-codex-provider-shadowing.md#OQ-4) says a
pack may narrow or replace a built-in provider's list, a name may ride that list, and part of this
gap closes without new machinery.

---

## 6. What this does not cover

- **No model catalog in core.** Nothing here asks core to know a model name or an offering. Both
  topics store names a pack or the user declares; core reads a declaration. This is the maintainer's
  constraint and this doc's own.
- **Not a redesign of `providers`/`profiles`.** The
  [`providers-and-profiles-redesign.md`](providers-and-profiles-redesign.md) question is a sibling;
  this doc uses the surfaces as they exist.
- **Not [OQ-4](pi-codex-provider-shadowing.md#OQ-4).** Whether a list may land on a built-in
  provider is answered there, not here.
- **Not an implementation plan.** No file map, no build order, no mechanism chosen. That material
  belongs to a companion sketch once the questions in [§8](#8-open-questions) are ruled.
- **Not about auto-detection.** The word is avoided on purpose ([OQ-PR2](#OQ-PR2)): nothing is
  discovered. The agent's pack declares; core reads.

---

## 7. Amendments and what is merely relied on

**This doc would AMEND, if ruled:**

| Ruling | The amendment |
| :--- | :--- |
| [BI-D3](pi-codex-provider-shadowing.md#BI-D3) | its "three ways" is two disagreeing lists; add the via gate's own read, and the protocol gate's, as one stated reader set ([§3](#3-topic-a-the-consequence-of-closing-it)) |
| [R3](pi-codex-provider-shadowing.md#8-risks-and-invariants) | a stale built-in provider list would also decide **selectability**, not only a collision ([§3](#3-topic-a-the-consequence-of-closing-it)) |
| [MM-D7](model-lists-and-pickers.md#MM-D7) | its reach: a display name currently has a home only where a row is written; naming a built-in provider's own model needs a home that is not a row ([§5](#5-topic-b-where-a-name-has-nowhere-to-live)) |

**Merely relied on, unchanged:** [OQ-3](pi-codex-provider-shadowing.md#OQ-3) (the ruling itself),
[BI-D1](pi-codex-provider-shadowing.md#BI-D1), [BI-D2](pi-codex-provider-shadowing.md#BI-D2),
[BI-D4](pi-codex-provider-shadowing.md#BI-D4) to [BI-D7](pi-codex-provider-shadowing.md#BI-D7),
[BI-D9](pi-codex-provider-shadowing.md#BI-D9) to [BI-D12](pi-codex-provider-shadowing.md#BI-D12),
[BI-D8](pi-codex-provider-shadowing.md#BI-D8) (which holds the naming-adjacent lists),
[ML-D1](model-lists-and-pickers.md#ML-D1), [ML-D6](model-lists-and-pickers.md#ML-D6),
[§7.1](model-lists-and-pickers.md#71-the-kind), [§7.3](model-lists-and-pickers.md#73-two-fields-the-entry-shape-is-missing),
[§14.1](model-lists-and-pickers.md#141-the-table), and [R1](pi-codex-provider-shadowing.md#8-risks-and-invariants)/[R2](pi-codex-provider-shadowing.md#8-risks-and-invariants).

---

## 8. Open Questions

1. 💬 <a id="OQ-PR1"></a>**OQ-PR1: should the provider-table gate treat a built-in provider name as reachable?**

   The choice decides whether [OQ-3](pi-codex-provider-shadowing.md#OQ-3)'s "selects the agent's own provider" holds for a name no pack ships.

   - **A — Yes, the gate reads the declaration.** A built-in name resolves even when the table lacks it, as the derive already assumes.
   - **B — No, the table must hold it.** A built-in provider still needs a pack to declare it; the derive's handling stays reachable only for those.
   - **C — Only for a plan the declaration maps.** A name built in but with a `null` plan for the profile stays refused; a name with an agent's own provider resolves.

   <!-- vantage: question id=OQ-PR1 leaning="A or C — the derive already treats any built-in name as usable, so today's refusal contradicts a built reader, and C is the narrower first step." -->

   _Leaning:_ **A or C** — the derive already treats any built-in name as usable, so today's refusal
   contradicts a built reader, and C is the narrower first step.

   **Answer:**
   > _(empty — fill in when decided)_

2. 💬 <a id="OQ-PR2"></a>**OQ-PR2: what is this concept called?**

   The name becomes vocabulary in every later doc, so it is worth ruling now. It must not be "auto-detection": nothing is discovered.

   - **A — "the agent's own client"** — describes the route, but names no capability.
   - **B — "agent-declared reachability"** — names the fact (the pack declares it), heavier.
   - **C — "built-in provider"** — already coined in [`pi-codex-provider-shadowing.md`](pi-codex-provider-shadowing.md#terms-in-plain-words); reuse it with no new term.

   <!-- vantage: question id=OQ-PR2 leaning="C — 'built-in provider' is already coined and defined, and a second term for one fact is the drift naming discipline exists to prevent." -->

   _Leaning:_ **C** — "built-in provider" is already coined and defined, and a second term for one
   fact is the drift naming discipline exists to prevent.

   **Answer:**
   > _(empty — fill in when decided)_

3. 💬 <a id="OQ-PR3"></a>**OQ-PR3: must a `null` plan still refuse?**

   A plan may map a yolo provider to `null` — "the agent has the name built in for another plan and
   none of its own for this one" ([BI-D1](pi-codex-provider-shadowing.md#BI-D1)). Whether the gate
   admits it decides that case's behavior.

   - **A — Still refuse, with a built-in-specific sentence.** The launch line already has such a warning ([BI-D7](pi-codex-provider-shadowing.md#BI-D7)); the gate should reuse its reasoning.
   - **B — Admit it, and let the launch line warn.** The gate never refuses a built-in name; the profile line is the only channel that says a `null` plan reaches nothing.

   <!-- vantage: question id=OQ-PR3 leaning="A — a null plan says the profile reaches nothing, which is what the gate exists to catch, and BI-D7 already words the launch line." -->

   _Leaning:_ **A** — a `null` plan says the profile reaches nothing, which is what the gate exists
   to catch, and [BI-D7](pi-codex-provider-shadowing.md#BI-D7) already words the launch line.

   **Answer:**
   > _(empty — fill in when decided)_

4. 💬 <a id="OQ-PR4"></a>**OQ-PR4: for an endpoint-less built-in, does the launch line or the refusal own the case?**

   Once admitted, an endpoint-less built-in may be one the agent cannot actually reach. Which layer says so decides whether the failure is at launch or at the first request.

   - **A — The launch line owns it.** The launch proceeds; the line says the route is the agent's own client and nothing here delivers a credential, naming the login it reads. Failure moves to the first request.
   - **B — The refusal owns it when no credential can be found.** The gate refuses at launch, needing a per-agent map from a built-in name to the variable it reads — machinery [BI-D1](pi-codex-provider-shadowing.md#BI-D1) keeps out of core.

   <!-- vantage: question id=OQ-PR4 leaning="A — it keeps core to names, which is BI-D1's whole point, and B needs a name→credential map core does not have." -->

   _Leaning:_ **A** — it keeps core to names, which is [BI-D1](pi-codex-provider-shadowing.md#BI-D1)'s
   whole point, and B needs a name→credential map core does not have.

   **Answer:**
   > _(empty — fill in when decided)_

5. 💬 <a id="OQ-PR5"></a>**OQ-PR5: must the reference docs state the built-in rule once?**

   The rule is implied today by three mechanisms in three code sites plus a launch line, and stated
   in none of [`../reference/providers.md`](../reference/providers.md)'s sections. A reader has to
   infer it from behavior.

   - **A — One normative statement in the provider reference**, with each mechanism linking to it.
   - **B — Leave it implied**, on the grounds that the ruling's own doc is the statement.

   <!-- vantage: question id=OQ-PR5 leaning="A — three mechanisms implying one rule is how a reader forms the wrong one, and the provider reference is where such a rule is looked up." -->

   _Leaning:_ **A** — three mechanisms implying one rule is how a reader forms the wrong one, and the
   provider reference is where such a rule is looked up.

   **Answer:**
   > _(empty — fill in when decided)_

6. 💬 <a id="OQ-MN1"></a>**OQ-MN1: is `name` the surface, or should the `models` map be extended?**

   `ModelEntry.Name` and `model_options.<alias>.name` already carry a display name. The question is
   whether naming stays on them or grows a new field.

   - **A — `name` is the surface.** No new field; a name is a fact on a model entry, as today.
   - **B — Extend the `models` map.** A dedicated naming declaration, separate from the model entries.

   <!-- vantage: question id=OQ-MN1 leaning="A — a name is already a model fact with one rule (MM-D7), and a second surface would be a second vocabulary for one thing." -->

   _Leaning:_ **A** — a name is already a model fact with one rule
   ([MM-D7](model-lists-and-pickers.md#MM-D7)), and a second surface would be a second vocabulary
   for one thing.

   **Answer:**
   > _(empty — fill in when decided)_

7. 💬 <a id="OQ-MN2"></a>**OQ-MN2: may a name attach to a model the agent's own catalog owns?**

   This is the gap in [§5](#5-topic-b-where-a-name-has-nowhere-to-live), and it is adjacent to
   [OQ-4](pi-codex-provider-shadowing.md#OQ-4).

   - **A — Yes, for a built-in provider too.** A name needs a home that is not a catalog row; the maintainer's "the rename can happen from anywhere" points here.
   - **B — No.** A name may attach only where yolo writes a row; a built-in provider's names stay the agent's.
   - **C — Defer to [OQ-4](pi-codex-provider-shadowing.md#OQ-4).** If a pack may shape a built-in provider's list, a name rides that list and no new home is needed.

   <!-- vantage: question id=OQ-MN2 leaning="C, then A — OQ-4 is open and decides the same territory, and if it closes the list side the answer is A, because the maintainer asked for the ability for users and packs." -->

   _Leaning:_ **C, then A** — [OQ-4](pi-codex-provider-shadowing.md#OQ-4) is open and decides the
   same territory, and if it closes the list side the answer is A, because the maintainer asked for
   the ability for users and packs.

   **Answer:**
   > _(empty — fill in when decided)_

8. 💬 <a id="OQ-MN3"></a>**OQ-MN3: precedence when a user, a personal pack and an upstream pack all name one model?**

   The repo already has a layering rule; the question is whether naming follows it unchanged.

   - **A — Follow the existing layering.** A pack's `models` `add` applies in pack order, then `only`, then the user's `providers.<name>.models` last ([§7.2](model-lists-and-pickers.md#72-composition-rules), [MM-D11](model-lists-and-pickers.md#MM-D11)); a later name wins.
   - **B — A naming-specific rule.** Names resolve by a rule of their own, independent of the list's composition.

   <!-- vantage: question id=OQ-MN3 leaning="A — the layering exists and is built (7.2, MM-D11), and a separate precedence for names would re-open what MM-D11 settled." -->

   _Leaning:_ **A** — the layering exists and is built
   ([§7.2](model-lists-and-pickers.md#72-composition-rules),
   [MM-D11](model-lists-and-pickers.md#MM-D11)), and a separate precedence for names would re-open
   what [MM-D11](model-lists-and-pickers.md#MM-D11) settled.

   **Answer:**
   > _(empty — fill in when decided)_

9. 💬 <a id="OQ-MN4"></a>**OQ-MN4: does a name ever affect resolution, or only display?**

   [MM-D7](model-lists-and-pickers.md#MM-D7) says the name is a display fact and the alias is the
   resolution key. Confirming that a name never becomes a selectable string keeps a profile's `model`
   meaning one thing.

   - **A — Display only.** A name is never a resolution key; only an alias or an id selects ([ML-D6](model-lists-and-pickers.md#ML-D6)'s row rule).
   - **B — A name may also be selected.** A profile `model` could name an entry by its display name.

   <!-- vantage: question id=OQ-MN4 leaning="A — MM-D7 already implies display-only, and letting a name resolve would give one string two meanings and muddle ML-D6's alias facts." -->

   _Leaning:_ **A** — [MM-D7](model-lists-and-pickers.md#MM-D7) already implies display-only, and
   letting a name resolve would give one string two meanings and muddle
   [ML-D6](model-lists-and-pickers.md#ML-D6)'s alias facts.

   **Answer:**
   > _(empty — fill in when decided)_

---

## 9. Decision Ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| — | **Empty by design.** No question in this doc has been ruled; this is a draft to argue with. The rulings it develops are recorded in [`pi-codex-provider-shadowing.md`](pi-codex-provider-shadowing.md#10-decision-ledger) and [`model-lists-and-pickers.md`](model-lists-and-pickers.md#16-decision-ledger). | — | — | — |
