---
title: "A provider an agent already has: who may select it, and what a model is called"
date: 2026-10-12
status: in-review
stage: DESIGN
next: "Rule OQ-PR6 (is OQ-PR1's option C also needed?) and OQ-PR7 (what replaces the case the removed `null` plan served?) — every other question was ruled 2026-10-12, and nothing here is built"
tags: [providers, profiles, models, built-in-providers, display-names, packs]
summary: "Two questions the 2026-10-05 built-in-provider ruling opens, and they are one seam seen twice. A provider an agent ships its own client for is handled by that agent's derive and named on the launch line, but the provider-table gate every launch reduces through does not know what one is, so a profile that names it is refused. And a model the agent's own catalog defines has nowhere for a friendly display name to live, because no catalog row is written for that provider. The 2026-10-12 review ruled every question this doc first raised; two narrow ones remain, and nothing here is built."
vantage:
  status-chip: true
---

# A provider an agent already has: who may select it, and what a model is called

**Status:** 2026-10-12. The review of 2026-10-12 ruled every question this doc first raised; the two
narrow ones it left ([OQ-PR6](#OQ-PR6), [OQ-PR7](#OQ-PR7)) are still open, and **nothing here is
built**. Evidence read against the tree at `ab9cf2f90`. The two topics are one seam seen twice: yolo
writes nothing for a provider an agent has built in, so other mechanisms have to decide what that
means, and today they disagree.

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

**Needs your ruling:** [OQ-PR6](#OQ-PR6), [OQ-PR7](#OQ-PR7). Every other question was ruled on
2026-10-12 and is compacted into [§9](#9-decision-ledger).

**Reads with:** [`pi-codex-provider-shadowing.md`](pi-codex-provider-shadowing.md) (the ruling this
doc develops — its [OQ-4](pi-codex-provider-shadowing.md#OQ-4) is adjacent to [§5](#5-topic-b-where-a-name-has-nowhere-to-live)),
[`model-lists-and-pickers.md`](model-lists-and-pickers.md) (the aliases and display names this doc
builds on — it says "code claims cite a symbol, never a line"; this doc cites lines on purpose, as
a document whose citations no one has re-measured), and [`../reference/providers.md`](../reference/providers.md) (the
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
to the agent's own provider for that plan), and `yolo_lists` for a provider the agent runs on its
own client but on yolo's list
([`internal/packdecl/builtinproviders.go`](../../internal/packdecl/builtinproviders.go) lines
[18–43](../../internal/packdecl/builtinproviders.go#L18-L43); pi's list and its `yolo_lists` are
[`packs/pi/pack.json`](../../packs/pi/pack.json) lines
[16–30](../../packs/pi/pack.json#L16-L30)). The built declaration still permits a `null` plan; the
review **removed it** on 2026-10-12 ([OQ-PR3](#9-decision-ledger)), so a `plans` entry must resolve
to a concrete provider, and what replaces the case `null` served is [OQ-PR7](#OQ-PR7).

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

**Ruled 2026-10-12 — the gate reads the declaration ([OQ-PR1](#9-decision-ledger)).** A built-in
name resolves even when the composed table lacks it, exactly as the derive already assumes. This is
not "should yolo auto-detect providers" — nothing is discovered. The agent's pack declares the
names; core reads a declaration, which is the whole design of
[BI-D1](pi-codex-provider-shadowing.md#BI-D1). Whether the narrower plan-level form (option C) is
also needed is [OQ-PR6](#OQ-PR6).

---

## 3. Topic A: the consequence of closing it

**Every built-in name becomes selectable at once.** Under [BI-D2](pi-codex-provider-shadowing.md#BI-D2)
each pack lists every provider id its agent's code registers: pi 43, omp 49, opencode 225, codex 5
([`packs/pi/pack.json`](../../packs/pi/pack.json#L17-L26),
[`packs/omp/pack.json`](../../packs/omp/pack.json#L15-L24),
[`packs/opencode/pack.json`](../../packs/opencode/pack.json#L20-L56),
[`packs/codex/pack.json`](../../packs/codex/pack.json#L23)). A gate that admits a built-in name
makes all 317 selectable, in three agents, in one change.

**It is agent-relative, and stays so.** The set an agent can reach is exactly what its SELECTED
packs declare — its `built_in_providers` names, plus the providers the composed table holds. There
is no second detection list to maintain, and nothing is auto-detected. pi reaches `meta` through
its own client; claude cannot reach `meta` at all without an endpoint and a credential — and claude
declares no `built_in_providers` ([`packs/claude/pack.json`](../../packs/claude/pack.json) has none)
— so a profile naming `meta` for claude is **a fatal error at startup**, a launch refusal, not a
warning. The rule is per (agent, provider, plan), which is exactly what the declaration holds.

**The reader count is already wrong before this doc.** Two lists disagree about how many readers the
declaration has:
[BI-D3](pi-codex-provider-shadowing.md#BI-D3) names "the boot and `yolo check`, the via-pointer scan,
and the env derive"; [§6.5](pi-codex-provider-shadowing.md#65-what-the-build-decided) names "every
derive's `ctx.built_in_providers`, the launch's profile line, and the agent's environment". Neither
names the via gate's own read at
[`launchrefusal.go:117`](../../internal/wirebridged/launchrefusal.go#L117), so by one counting the
via gate is already an undeclared fourth. Adding the protocol gate makes it four by
[§6.5](pi-codex-provider-shadowing.md#65-what-the-build-decided)'s counting or five by
[BI-D3](pi-codex-provider-shadowing.md#BI-D3)'s. **This doc amends
[BI-D3](pi-codex-provider-shadowing.md#BI-D3)** to state one reader set, containing every core read,
rather than two lists that each claim three.

**Residual risk, stated plainly.** An endpoint-less built-in selected for an agent that cannot
actually reach it moves from a launch-time refusal to a **runtime auth failure**. Today, a profile
naming a provider the agent has but cannot use stops the launch. Afterward it starts the agent, and
the first request fails — for pi's `meta`, an authentication error the agent prints inside the
session, not a refusal yolo owns. The review settled which layer owns it ([OQ-PR4](#9-decision-ledger)):
the launch line. Three things follow:

- **the launch line owns it.** It must say an endpoint-less built-in is a **routing** statement, not
  an availability one: that nothing this launch configures delivers a credential to it, naming the
  login or variable the agent's own client reads. `profileReach`'s built-in branch
  ([`profiledisclosure.go:217`](../../internal/packload/profiledisclosure.go#L217), route at
  [:227](../../internal/packload/profiledisclosure.go#L227)) returns before the credential half, so
  it says the route and not whether a credential reaches it — unlike the catalogued-provider path,
  whose credential warning runs at
  [`profiledisclosure.go:281`](../../internal/packload/profiledisclosure.go#L281);
- [R3](pi-codex-provider-shadowing.md#8-risks-and-invariants) changes shape: under
  [OQ-3](pi-codex-provider-shadowing.md#OQ-3) a stale list costs only a collision with a provider of
  that name, but a gate that reads the list makes staleness decide **selectability** — a provider an
  agent added, not yet in its pack's list, would be refused. **This doc amends
  [R3](pi-codex-provider-shadowing.md#8-risks-and-invariants)** to say so;
- **ruled out ([OQ-PR4](#9-decision-ledger)):** an arm this doc does not propose — refusing at
  launch an endpoint-less built-in the launch cannot find a credential for. It would need a
  per-agent map from a built-in name to the credential it reads, which yolo does not have and
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

**This shares territory with a ruling still open elsewhere.** [OQ-4](pi-codex-provider-shadowing.md#OQ-4)
(open) asks whether the ruling reaches *a list* a pack declares for a built-in provider. The review
nonetheless settled the naming question on 2026-10-12 ([OQ-MN2](#9-decision-ledger)): **a name may
attach to a model the agent's own catalog owns, a built-in provider's included, and it attaches per
ENTRY, not per model id** — so two entries for one model id with different context windows are
named separately. If [OQ-4](pi-codex-provider-shadowing.md#OQ-4) says a pack may narrow or replace a
built-in provider's list, a name may ride that list, closing part of this gap without new machinery.

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
- **Not about auto-detection.** The word is refused on purpose: the concept is the **built-in
  provider** ([OQ-PR2](#9-decision-ledger)), the term
  [`pi-codex-provider-shadowing.md`](pi-codex-provider-shadowing.md#terms-in-plain-words) already
  coined. Nothing is discovered. The agent's pack declares; core reads.

---

## 7. Amendments and what is merely relied on

**This doc AMENDS (ruled 2026-10-12; the amendments are unbuilt):**

| Ruling | The amendment |
| :--- | :--- |
| [BI-D1](pi-codex-provider-shadowing.md#BI-D1) | the `null` plan it defines is removed: a `plans` entry must resolve to a concrete provider, so a program with no provider of its own for a plan says so without `null` ([§1](#1-one-seam-two-mechanisms), [OQ-PR3](#9-decision-ledger)); the built declaration and its reference still carry `null` |
| [BI-D3](pi-codex-provider-shadowing.md#BI-D3) | its "three ways" is two disagreeing lists; add the via gate's own read, and the protocol gate's, as one stated reader set ([§3](#3-topic-a-the-consequence-of-closing-it)) |
| [BI-D7](pi-codex-provider-shadowing.md#BI-D7) | its `null`-plan warning goes with the `null` plan ([OQ-PR3](#9-decision-ledger), [OQ-PR7](#OQ-PR7)) |
| [R3](pi-codex-provider-shadowing.md#8-risks-and-invariants) | a stale built-in provider list would also decide **selectability**, not only a collision ([§3](#3-topic-a-the-consequence-of-closing-it)) |
| [MM-D7](model-lists-and-pickers.md#MM-D7) | its reach: a display name currently has a home only where a row is written; naming a built-in provider's own model needs a home that is not a row, per model entry ([§5](#5-topic-b-where-a-name-has-nowhere-to-live), [OQ-MN2](#9-decision-ledger)) |
| [`../reference/providers.md`](../reference/providers.md) | gains one normative statement of the built-in rule, with each mechanism linking to it ([OQ-PR5](#9-decision-ledger)) |

**Merely relied on, unchanged:** [OQ-3](pi-codex-provider-shadowing.md#OQ-3) (the ruling itself),
[BI-D2](pi-codex-provider-shadowing.md#BI-D2),
[BI-D4](pi-codex-provider-shadowing.md#BI-D4) to [BI-D6](pi-codex-provider-shadowing.md#BI-D6),
[BI-D9](pi-codex-provider-shadowing.md#BI-D9) to [BI-D12](pi-codex-provider-shadowing.md#BI-D12),
[BI-D8](pi-codex-provider-shadowing.md#BI-D8) (which holds the naming-adjacent lists),
[ML-D1](model-lists-and-pickers.md#ML-D1), [ML-D6](model-lists-and-pickers.md#ML-D6),
[§7.1](model-lists-and-pickers.md#71-the-kind), [§7.3](model-lists-and-pickers.md#73-two-fields-the-entry-shape-is-missing),
[§14.1](model-lists-and-pickers.md#141-the-table), and [R1](pi-codex-provider-shadowing.md#8-risks-and-invariants)/[R2](pi-codex-provider-shadowing.md#8-risks-and-invariants).

---

## 8. Open Questions

Two narrow questions the rest of the review left open. Every other question this doc raised was
ruled on 2026-10-12 and is compacted into [§9](#9-decision-ledger).

1. 💬 <a id="OQ-PR6"></a>**OQ-PR6: is the gate's narrow form (option C) also needed?**

   [OQ-PR1](#9-decision-ledger) ruled that the provider-table gate treats a built-in name as
   reachable, reading the declaration. Option C was narrower: admit a built-in name only for a plan
   the declaration maps, and keep refusing one that is built in but whose plan names nothing.

   - **A — No, plain reachability is enough.** Every built-in name resolves; there is no second, per-plan gate.
   - **B — Also add the plan check.** A name with no plan the agent serves stays refused.

   <!-- vantage: question id=OQ-PR6 leaning="A — OQ-PR1 already made the whole name reachable, and a plan-level gate is a second place for the declaration to be read wrongly." -->

   _Leaning:_ **A** — [OQ-PR1](#9-decision-ledger) already made the whole name reachable, and a
   plan-level gate is a second place for the declaration to be read wrongly.

   **Answer:**
   > _(empty — fill in when decided)_

2. 💬 <a id="OQ-PR7"></a>**OQ-PR7: what replaces the case the removed `null` plan served?**

   The `null` plan said "the agent has this name built in for another plan and none of its own for
   this one" ([OQ-PR3](#9-decision-ledger) removed it). With it gone that case must resolve to a
   concrete plan or be refused, and the review did not say which.

   - **A — The profile is refused at startup.** A plan the agent has no provider of its own for is the "agent cannot reach it" case, a fatal launch error.
   - **B — The agent's own provider of that name is used anyway.** The name is built in, so the plan distinction is dropped.

   <!-- vantage: question id=OQ-PR7 leaning="A — it is the same shape as the fatal case the review did rule on, where a profile names a provider its agent cannot reach." -->

   _Leaning:_ **A** — it is the same shape as the fatal case the review did rule on, where a profile
   names a provider its agent cannot reach.

   **Answer:**
   > _(empty — fill in when decided)_

---

## 9. Decision Ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| OQ-PR1 | **The provider-table gate reads the declaration.** A built-in name resolves even when the composed table lacks it, exactly as the derive already assumes, so [OQ-3](pi-codex-provider-shadowing.md#OQ-3)'s "selects the agent's own provider" holds for a name no pack ships. Not "auto-detection": the pack declares, core reads. The one reader set this makes is [BI-D3](pi-codex-provider-shadowing.md#BI-D3)'s amendment. Option C's narrower plan-level gate is [OQ-PR6](#OQ-PR6) | 2026-10-12 | [§2](#2-topic-a-the-provider-table-gate-does-not-know) | — |
| OQ-PR2 | **The concept is the "built-in provider"**, the term [`pi-codex-provider-shadowing.md`](pi-codex-provider-shadowing.md#terms-in-plain-words) already coined. One agent may have MULTIPLE built-in providers; a built-in provider's credential is either an API key or obtained by the agent itself; sharing an agent-obtained credential across jails is an **accepted gap**, and its absence must not block. That sharing mechanism is BUILT and generic and needs no core code — a pack declares a machine-scope `state` dir plus the `shared_credentials` hook, as [`packs/claude`](../../packs/claude/pack.json) and [`packs/agy`](../../packs/agy/pack.json) do ([`internal/entrypoint/packhooks.go`](../../internal/entrypoint/packhooks.go) `HookSharedCredentials`) — so what does not exist is a per-agent **opt-in**: each pack must declare the pair itself, and most, [`packs/muse-code`](../../packs/muse-code/pack.json) included, do not. It is pack-declared ONLY: no config key expresses it, and a read-write `mounts` element cannot substitute, being refused outside `/ctx/` ([`internal/config/mounts.go`](../../internal/config/mounts.go)). Providers are adopted INCREMENTALLY, one at a time, not all of what an agent supports at once. ⚠ **Amended 2026-10-12 — that hook is not the only route to a shared credential.** A user-SUPPLIED one takes a different, already-built path a pack carries as a NAME, never a value: [`packs/zai`](../../packs/zai/pack.json) declares a provider whose `api_key_env_name` is `ZAI_API_KEY` (a pack's `kind: "env"` holds static literals only, [`packdecl`](../../internal/packdecl/contributes.go)), and the user supplies the value through `env_sources`, resolved on the HOST ([`internal/config/envsources.go`](../../internal/config/envsources.go)), conventionally at `~/.config/yolo-jail/env` (0600) — at either scope, a workspace unsetting an inherited name as `{"NAME": null}`. WHETHER such a name reaches an agent is decided by a CLAIM, not the hook: [`ScopeCredentials`](../../internal/packload/credentialscope.go) delivers a claimed name only to the agents whose profile selects a claimant, through that agent's own `~/.config/yolo-agent-env/<agent>.sh`, and an unclaimed one to every process ([the credential gate](../reference/providers.md#the-credential-gate) is where that rule and its delivery are stated). The claim is launch-wide and effectively exclusive: a selected pack's provider claims its variable for the whole launch even when no agent selects it, withholding it from every other process — which is why an unclaimed `META_API_KEY` serves the pi agent and the Muse Code CLI at once today, and why a Meta provider declaring one later would lock the Muse CLI out until Muse declares a claimant too. The value lands on the jail's disk (the shared `<workspace>/.yolo/home/yolo-user-env.sh`, 0600, bind-mounted; a claimed one in the agent's own 0600 env file beneath it) and never in `launch.log`, `boot.log`, the terminal or the `podman run` argv, while an INLINE `{"KEY":"value"}` entry additionally lands in `<workspace>/.yolo/config-assembled.json` at 0644, so a dotenv path is the preferred form | 2026-10-12 | [§6](#6-what-this-does-not-cover) | — |
| OQ-PR3 | **The `null` plan is removed** — a profile must resolve to a concrete plan. This amends [BI-D1](pi-codex-provider-shadowing.md#BI-D1)'s `plans` shape and [BI-D7](pi-codex-provider-shadowing.md#BI-D7)'s launch line; the built declaration ([`internal/packdecl/builtinproviders.go`](../../internal/packdecl/builtinproviders.go)), its reference ([`pack-system.md`](../reference/pack-system.md#built_in_providers)) and [`providers.md`](../reference/providers.md) still carry `null`, and the removal is unbuilt. What replaces the case `null` served is [OQ-PR7](#OQ-PR7) | 2026-10-12 | [§1](#1-one-seam-two-mechanisms) | — |
| OQ-PR4 | **The launch line owns an endpoint-less built-in, and the refusal does not.** The launch proceeds; the line says the route is the agent's own client and nothing this launch configures delivers a credential, naming the login the agent reads, so failure moves to the first request. A launch-time credential refusal is ruled out: it needs a per-agent name→credential map [BI-D1](pi-codex-provider-shadowing.md#BI-D1) keeps out of core | 2026-10-12 | [§3](#3-topic-a-the-consequence-of-closing-it) | — |
| OQ-PR5 | **The built-in rule is stated once, normatively, in [`../reference/providers.md`](../reference/providers.md)**, with each mechanism linking to it | 2026-10-12 | [§7](#7-amendments-and-what-is-merely-relied-on) | — |
| OQ-MN1 | **`name` is the surface.** No new field; a display name stays a fact on a model entry (`ModelEntry.Name`, `model_options.<alias>.name`) | 2026-10-12 | [§4](#4-topic-b-what-already-exists-for-a-model-name) | — |
| OQ-MN2 | **A name may attach to a model the agent's own catalog owns**, a built-in provider's included, and **per ENTRY, not per model id** — two entries for one model id with different context windows are named separately. Whether a name may ride a pack-declared list on a built-in provider stays [OQ-4](pi-codex-provider-shadowing.md#OQ-4)'s | 2026-10-12 | [§5](#5-topic-b-where-a-name-has-nowhere-to-live) | — |
| OQ-MN3 | **Naming follows the existing layering** ([§7.2](model-lists-and-pickers.md#72-composition-rules), [MM-D11](model-lists-and-pickers.md#MM-D11)): a pack's `models` applies in pack order, then `only`, then the user's `providers.<name>.models` last, and a later name wins. There is no naming-specific precedence | 2026-10-12 | [§4](#4-topic-b-what-already-exists-for-a-model-name) | — |
| OQ-MN4 | **A name is display-only, never a resolution key.** Only an alias or an id selects ([ML-D6](model-lists-and-pickers.md#ML-D6)) | 2026-10-12 | [§4](#4-topic-b-what-already-exists-for-a-model-name) | — |
