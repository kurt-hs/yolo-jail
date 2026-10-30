---
title: "Enabling a provider is selecting its pack — and a vendor's agent selects its own"
date: 2026-10-29
status: accepted
stage: BUILT
next: "The owner reviews the three build decisions this doc gained (PE-D7, PE-D8, PE-D9); then graduate the shipped rule into providers.md and delete this doc"
tags: [providers, profiles, packs, copilot, claude, codex, pi, subscriptions]
summary: "One rule for GitHub Copilot, the Claude subscription, the OpenAI subscription and pi's forty-odd built-in providers: a provider is usable only while a pack declaring it is selected, every subscription provider has exactly one pack named for the vendor's agent (`claude-auth`, `codex-auth`, `copilot-auth`) with a profile named for the agent (`claude`, `codex`, `copilot`), and a vendor's agent pack brings its own vendor's pack in through `needs`, because that agent's default is that provider. The long tail gets no packs: the undeclared-profile refusal prints the two-line declaration."
vantage:
  status-chip: true
---

# Enabling a provider is selecting its pack — and a vendor's agent selects its own

**Status:** 2026-10-29. Ruled and built: every ruling and design decision below is ✅ in the [Decision ledger](#decision-ledger), with the tests that pin it. The build added [PE-D7](#PE-D7), [PE-D8](#PE-D8) and [PE-D9](#PE-D9), which the owner has not reviewed yet; PE-D8 departs from the [long tail](#the-long-tail)'s wording. The [Current state](#current-state) table describes the tree at `84d0d4fee`, before the build.

> **In short.** A provider becomes usable by exactly one act, selecting the pack that declares it, and a vendor's agent pack performs that act for its own vendor's pack through `needs`, since Claude Code, Codex and the Copilot CLI each default to that provider anyway. The packs are named for the agents people know the subscriptions by, `claude-auth`, `codex-auth` and `copilot-auth`, and the profile is the bare agent name: `-p pi=claude`. Everything else, pi's forty built-ins included, stays one user declaration away, and the refusal types it for you.

**Why it matters.** Today the three subscriptions are enabled three different ways: the Copilot pack is named for the service and its profile with it, the `claude` pack carries the Anthropic credential broker but no provider, and the OpenAI provider lives in a pack the `codex` agent deliberately does not bring in. A user cannot predict which `-p` names work from which packs they selected, and the names that do work are long to type.

**The shape.** One *provider pack* per subscription provider, `copilot-auth`, `claude-auth` and `codex-auth`, each declaring the provider, a profile named for the agent and any credential service; each vendor agent pack `needs` its own; the generic built-in rule, no row and no warning, covers the vendor CLI on its own profile.

**Cost.** One new pack and two renames with their refusals; one profile rename; `codex` launches gain a host daemon; `claude-oauth-broker` moves into `claude-auth`; one recorded stance is revised.

**Start at [Recommendation](#recommendation).** [Options considered](#options-considered) is the argument for it.

**Needs your ruling:** nothing. [OQ-PE1](#OQ-PE1) to [OQ-PE5](#OQ-PE5) are ruled in the [Decision ledger](#decision-ledger). **Needs your review:** [PE-D7](#PE-D7) to [PE-D9](#PE-D9), the decisions the build made.

**Reads with:** [`providers.md`](../reference/providers.md) (the provider and profile system this doc arranges packs around), [`pi-codex-provider-shadowing.md`](pi-codex-provider-shadowing.md) (the built-in provider rule and the landed Copilot pack, BI-D14), [`wire-bridge.md`](../reference/wire-bridge.md) (`needs`, and the openai-auth stance this doc revises).

---

## Summary

The earlier Copilot ruling, generalized. **A provider is usable only while a pack that declares it is selected**, which is already the rule for profiles ([OQ-CS6](../reference/providers.md#profiles-and-options)). **Each subscription provider has exactly one pack**, named `<agent>-auth` for the vendor's agent ([OQ-PE3](#OQ-PE3)), holding its provider, a profile named for the agent, and any credential service it needs, the Claude broker included ([OQ-PE5](#OQ-PE5)). **A vendor's agent pack `needs` its own vendor's pack** ([OQ-PE1](#OQ-PE1), [OQ-PE2](#OQ-PE2)), because selecting Claude Code, Codex or the Copilot CLI is already the decision to use that vendor's subscription; no other provider is brought in by anything. **The long tail ships no packs**: a built-in provider with no special handling is one user declaration, and the undeclared-profile refusal prints that declaration verbatim when the name is one a selected agent has built in ([OQ-PE4](#OQ-PE4)). This revises the recorded stance that `codex` does not bring in the OpenAI credential pack and keeps the Copilot ruling, renaming its pack and profile.

## Terms

- **Provider**: a service an agent can talk to, declared with a `platform` (what service it is, such as `github-copilot`), optional endpoints, key names and model lists ([`providers.md`](../reference/providers.md)).
- **Profile**: a name a user selects per agent that picks one provider plus options; several profiles for one agent form an **active set** ([`providers.md`](../reference/providers.md#an-active-set-several-profiles-for-one-agent)). Declaration is mandatory: a selected name no selected pack or user config declares is refused.
- **Built-in provider**: one an agent ships its own client, list and login for, named in the agent pack's `built_in_providers` by the id the agent's own config keys it under; yolo writes it no catalog row and the agent runs it on its own client ([`providers.md`](../reference/providers.md#a-provider-the-agent-has-built-in)).
- **Subscription provider**: a built-in provider backed by an interactive subscription login rather than a key ([`pi-codex-provider-shadowing.md`](pi-codex-provider-shadowing.md)). The three here are GitHub Copilot, the Claude subscription and the OpenAI (ChatGPT) subscription.
- **Provider pack** *(coined here)*: a shipped pack whose purpose is one provider: its `provider` and `profile` contributions and, where the provider needs one, its credential service. It installs no program. The three subscription packs are named `<agent>-auth`. Not an agent pack, and not a gateway pack such as `openrouter`, though those are provider packs by this definition too.
- **Vendor agent pack** *(coined here)*: the agent pack whose program defaults to a subscription provider: `claude` for `claude-auth`, `codex` for `codex-auth`, `copilot` for `copilot-auth`.
- **Credential service**: a host-side `loophole` that owns a subscription login for every jail on the machine, such as `openai-auth-broker` and `claude-oauth-broker`.
- **`needs`**: a pack dependency, resolved host-side at selection as a transitive closure and printed with its cause ([`wire-bridge.md`](../reference/wire-bridge.md#needs--a-conditional-pack-dependency)).
- **Pinned endpoint** *(coined here)*: a provider address a user layer may not add or change, because the agent sends a subscription bearer token to whatever address it is given.

## Current state

| Provider | Where declared today | Credential service | Vendor CLI on its own profile |
| :--- | :--- | :--- | :--- |
| `github-copilot` | `packs/github-copilot` (provider + profile `github-copilot`, landed `84d0d4fee`, `packs/github-copilot/pack.json`, now [`packs/copilot-auth/pack.json`](../../packs/copilot-auth/pack.json)); `copilot` `needs` it unconditionally; `packs/pi` keeps only the name in `built_in_providers` ([BI-D14](pi-codex-provider-shadowing.md#BI-D14)) | none (each agent's own `/login`) | the Copilot CLI runs it on its own client, no warning |
| `openai-codex` | `packs/openai-auth`, with profile `codex` and the one model list (`packs/openai-auth/pack.json`, now [`packs/codex-auth/pack.json`](../../packs/codex-auth/pack.json)) | `openai-auth-broker`, in the same pack; `codex` only carries an `env` gated on it ([`packs/codex/pack.json`](../../packs/codex/pack.json)) | `codex` without the pack runs on its own login with no profile and no broker |
| Claude subscription | nowhere; no provider of platform `anthropic` exists | `claude-oauth-broker`, in `packs/claude` ([`packs/claude/pack.json`](../../packs/claude/pack.json)) | no profile can name it, so an active set cannot hold it |
| pi's other ~40 built-ins, opencode's and oh-omp's | nowhere; the names are in each pack's `built_in_providers` | none | `-p pi=groq` is refused as undeclared, listing the declared names only |

The undeclared-profile refusal today reads *no profile named "X" is declared — a profile name must be declared by a selected pack's manifest or by your config's `profiles` key (declared: …)*, and, when a shipped pack declares the name, adds *the `<pack>` pack yolo ships declares it: add `"<pack>"` to your config's `packs` list* (`ShippedProfileDeclarers`, [`internal/packload/profiles.go`](../../internal/packload/profiles.go)). For a name an agent has built in and no pack declares, it types no declaration.

## Options considered

| Option | What `packs: ["pi"]` lets `-p pi=<name>` select | Clarity | Long-tail cost | Special handling | Credential channel | Verdict |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **A — one pack per provider, selected explicitly; agent packs bring nothing** | nothing until a pack is added | high: the `packs` list is the whole answer | one pack per built-in anyone wants | a natural home | the pack's loophole | **Rejected as written**: `packs: ["claude"]` could not name the subscription Claude Code runs on by default, so an active set cannot hold it; and `codex` users stay one unknown pack away from the broker that keeps their login alive |
| **B — every agent-built-in provider is implicitly declared for that agent** | all 45 of pi's | low: declared set differs per agent; 45 names in every disclosure | zero | none: a built-in needing a list, a plan, a broker or a pin still needs a pack, so two enablement rules coexist | none | **Rejected**: reverses the mandatory-declaration ruling and gives nothing-by-default a 45-name exception |
| **C — A, plus a vendor agent pack `needs` its own vendor's provider pack** | nothing (pi is not a vendor agent) | high: selecting an agent enables exactly the provider it defaults to, printed as `+ claude-auth (needed by claude)` | as A for packs, but see the long tail: a recognized built-in is refused with its declaration typed out | the pack | the pack's loophole, present whenever the vendor CLI is | **Chosen**. The earlier Copilot ruling, applied to all three |
| **D — a generic `enable_providers` config key** | the names listed | medium: a second list beside `packs` | zero | still needs packs | still needs packs | **Rejected**: `packs` already is the one list of what is active; a second list that cannot carry a loophole duplicates it without replacing it |

## Recommendation

### The rule

1. **A provider is usable by an agent only while a selected pack, or the user's config, declares it**, and a selected profile must be declared (unchanged). Nothing is declared by an empty `packs` list.
2. **Every subscription provider has exactly one shipped pack, named `<agent>-auth`** for the vendor's agent ([Names](#names)). It declares the `provider` (with its `platform`), one `profile` named for the agent, the provider's model list where yolo owns one, and the credential service where the subscription needs one. It installs no program, so pi, opencode or oh-omp users select it alone to run on their own client.
3. **A vendor agent pack `needs` its vendor's pack unconditionally**, and lists that provider in its program's `built_in_providers` where its derive would otherwise write a row for it. The generic built-in rule then does the rest: no row, no env, no warning; the vendor CLI runs the profile on its own login. The `needs` is also what binds the platform for the agent (`bindsPlatform`, [BI-D14](pi-codex-provider-shadowing.md#BI-D14)), which is all the Copilot CLI needed. No other pack is brought in by any agent pack.
4. **No other built-in provider gets a pack** unless it needs special handling: a credential service, a yolo-owned model list (`yolo_lists`), a plan mapping (`plans`), a pinned endpoint, or a platform two agents must agree on. Everything else is a user declaration ([The long tail](#the-long-tail)).
5. **A subscription provider's endpoint is pinned** ([Security](#security)).

Core switches on no name in any of this: `needs`, `built_in_providers`, `platform` and the pin are manifest facts.

### Names

People know these subscriptions by the vendor's agent, so the agent is the key ([OQ-PE3](#OQ-PE3)). Three names per vendor, each with one rule:

- **The pack is `<agent>-auth`**: `claude-auth`, `codex-auth`, `copilot-auth`. Short, and predictable from the agent's name.
- **The profile is the bare agent name**: `-p pi=claude`, `-p pi=copilot`, `-p codex=codex`. The profile is the one name a user types. `openai-auth` already ships profile `codex` over `openai-codex`, so a profile that equals an agent name is legal today; `-p copilot` (a bare profile for every agent) and `-p copilot=<profile>` (the Copilot CLI's selection) are told apart by the `=`. This withdraws BI-D13's reason for `github-copilot` over `copilot` as the profile name.
- **The provider keeps the id the agents' own clients key it by**, which is also its `platform`: `anthropic`, `openai-codex`, `github-copilot` ([PE-D6](#PE-D6)). `built_in_providers.names` is matched against the yolo provider's *name* (`BuiltInProviderFor`, [`internal/packload/builtinproviders.go`](../../internal/packload/builtinproviders.go)), so a provider named `claude` would need a `plans` remap in every agent pack with the built-in (pi, opencode and oh-omp for `anthropic` and `github-copilot`; pi and opencode for `openai-codex`) and would move the `yolo_lists` and `plans` keys in `packs/pi`, `packs/opencode` and `packs/codex` with it. A user meets the provider name only in a `providers` override and in the launch's profile line.

**Migration.** The old pack names `openai-auth` and `github-copilot`, and the old profile name `github-copilot`, are refused naming the new one ([Happy Path](#happy-path)); nothing is aliased. `codex` is already the agent name and stays.

### Per vendor

| Vendor | Provider pack | Declares | Brought in by | Vendor CLI on its profile | Other agents |
| :--- | :--- | :--- | :--- | :--- | :--- |
| GitHub Copilot | `copilot-auth` (landed as `github-copilot`, `84d0d4fee`; renamed 2026-10-29) | provider `github-copilot`, platform `github-copilot`, no endpoint, no key, pinned; profile `copilot` (was `github-copilot`) | `copilot` `needs` it (built) | own client and login, no warning (built) | pi, opencode, oh-omp: their own clients and `/login`; claude, codex: *reaches nothing* |
| Claude subscription | `claude-auth` (new) | provider `anthropic`, platform `anthropic`, no endpoint in v1 ([PE-D3](#PE-D3)); profile `claude`; `claude-oauth-broker`, moved from `packs/claude` ([OQ-PE5](#OQ-PE5)) | `claude` `needs` it ([OQ-PE1](#OQ-PE1)); no `built_in_providers` entry, because claude's derive writes no row for `anthropic` ([PE-D1](#PE-D1)) | own client and login, no `ANTHROPIC_*` written | pi, opencode, oh-omp: their own `anthropic` clients (pi's OAuth acceptance UNMEASURED); codex, copilot: *reaches nothing* |
| OpenAI subscription | `codex-auth` (was `openai-auth`; renamed 2026-10-29) | unchanged: provider `openai-codex` with its endpoint and list, `openai-auth-broker`; profile `codex` | `codex` `needs` it ([OQ-PE2](#OQ-PE2)) | own client and login (its `plans` already maps it) | pi, opencode: own client, yolo's list; oh-omp: own list; claude: through the bridge, as today |

**The earlier Copilot ruling stands** in substance: its pack and profile take the new names, nothing else about it moves. **The openai-auth stance is revised** under [OQ-PE2](#OQ-PE2).

### What this does not propose

No change to how a profile is selected, to active sets, to derives, or to the bridge's routes; no new config key; no pack for any provider in [rule 4](#the-rule)'s long tail; no fetched pack gaining `needs`; no alias for an old name.

## The long tail

Pi's other built-ins, opencode's and oh-omp's stay packless. The supported path is the user declaration a platform-only provider already permits, two lines in the user config, `~/.config/yolo-jail/config.jsonc`, because `profiles` is read at user scope only ([PE-D8](#PE-D8)):

```jsonc
"providers": { "groq": { "platform": "groq" } },
"profiles":  { "groq": { "provider": "groq" } }
```

Under [OQ-PE4](#OQ-PE4) the refusal prints exactly those lines when the undeclared name is in a selected agent pack's `built_in_providers`, and names the shipped pack instead when one declares the profile (the pack hint is built, `ShippedProfileDeclarers`). A long-tail provider earns a pack only on [rule 4](#the-rule)'s grounds, and the pack's manifest is the record of what special handling it carries. The recognition reads selected packs' manifests and, for the pack hint, the shipped set, in the refusal path only; it reserves nothing ([`AGENTS.md`](../../AGENTS.md) forbids reading the embedded set to reserve a name, not to word a message).

## Happy Path

| Stop | Line (shape; final wording is the implementer's) |
| :--- | :--- |
| Undeclared profile a shipped pack declares (built) | `packs: profile "copilot" selected for pi: no profile named "copilot" is declared — … (declared: bedrock); the copilot-auth pack yolo ships declares it: add "copilot-auth" to your config's packs list` |
| Undeclared profile a selected agent has built in, no pack | `packs: profile "groq" selected for pi: no profile named "groq" is declared — pi has groq built in, so declare it in your user config (~/.config/yolo-jail/config.jsonc):` then the two lines above, then `(declared: bedrock)` |
| Undeclared built-in name a shipped pack carries under another profile name | `… no profile named "anthropic" is declared — pi has anthropic built in, and the claude-auth pack yolo ships declares it as the profile "claude": select "claude" instead, and add "claude-auth" to your config's packs list (declared: …)` |
| Undeclared profile, neither | today's message, unchanged |
| Vendor CLI on its own profile | the launch's profile line: `claude → claude: its own client and login`; no warning |
| Profile over a provider an agent has no client for | today's one line: `Warning: profile "copilot" reaches nothing for claude: …` with the `-p claude=` next step, unchanged |
| A `needs` addition | today's closure line, `+ codex-auth (needed by codex)`, unchanged in shape |
| Endpoint added to a pinned provider | `providers.anthropic.endpoints: refused — "anthropic" is a subscription provider and its address is pinned by the claude-auth pack, because the agent sends the subscription's bearer token to whatever address it is given. Drop endpoints and base_url from your providers.anthropic entry; to reach another host, declare a provider under another name with its own api_key_env_name` |
| Old pack name after a rename | `config.packs[0].source: "openai-auth" is now "codex-auth"; write "codex-auth"`, and the same for `"github-copilot"` and `"copilot-auth"` |
| Old profile name after the rename | `packs: profile "github-copilot" selected for pi: no profile named "github-copilot" is declared — it was renamed "copilot", which the copilot-auth pack declares: select "copilot" instead`, plus `, and add "copilot-auth" to your config's packs list` when the pack is not selected |
| `yolo check` over any of the profile stops above | the same line as a `[FAIL]`, from the launch's own code path (`config.UndeclaredProfileProblems`) |

## Security

- **Pinned endpoints.** A Claude subscription bearer follows `ANTHROPIC_BASE_URL` (measured in a prior session), so any provider that could re-point Claude Code while it holds that token is an exfiltration channel. The provider contribution gains a boolean `pinned_endpoint` *(coined here)*: with it set, a user layer may not add or change an `endpoints` entry, and the refusal names the fix. All three subscription packs set it. This is the provider flag [OQ-BR2](pi-codex-provider-shadowing.md#10-decision-ledger)'s ledger deferred *until another subscription provider exists*; there are now three.
- **Endpoint-less by default.** `copilot-auth` and `claude-auth` declare no endpoint, so there is nothing to re-point and a profile `via` over them is inert, as the built-in rule already says. `codex-auth` keeps its endpoint because the bridge routes claude through it; it is pinned.
- **The derive invariant stays.** A derive never writes `ANTHROPIC_BASE_URL` for claude without `ANTHROPIC_AUTH_TOKEN` (today's dummy token on a routed launch, [`providers.md`](../reference/providers.md#per-agent-delivery)), so a routed claude never spends the subscription bearer off-host. A profile on `anthropic` writes neither.
- **Credentials cross only through loopholes.** A provider pack carries its credential service as a `loophole`, `openai-auth-broker` in `codex-auth` and `claude-oauth-broker` in `claude-auth`; no pack `env` carries a token. Under rule 3 the service is present whenever the vendor CLI is, closing the multi-jail logout a `codex` user hits today with no broker. The one-file bargain, that pi's `auth.json` shares every provider's login machine-wide, is unchanged by this doc.

## Decision ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| <a id="OQ-PE1"></a>[OQ-PE1](#OQ-PE1) | *Ruling.* **Selecting a vendor agent pack enables the one provider that agent defaults to**: the agent pack `needs` its vendor's provider pack, so `"packs": ["claude", "pi"]` with `"profile": {"pi": ["bedrock", "claude"]}` works as written and the launch prints `+ claude-auth (needed by claude)`. Selecting Claude Code is already the decision to use the Claude subscription; the pack only makes that default nameable, and the credential broker rides along where it is needed | 2026-10-29 | [The rule](#the-rule) | ✅ 2026-10-29: `packs/claude` and `packs/codex` gain `needs` (`TestEachVendorAgentBringsItsOwnProviderPack`, `TestABareClaudeProfileRunsClaudeCodeOnItsOwnLogin`) |
| <a id="OQ-PE2"></a>[OQ-PE2](#OQ-PE2) | *Ruling.* **Every Codex launch runs the OpenAI credential broker**: `codex` `needs` `codex-auth`, revising the recorded stance that the pack joins only when selected. Parity with claude, and the broker exists for exactly the two-jail logout; an idle daemon costs less than a lost login | 2026-10-29 | [Per vendor](#per-vendor) | ✅ 2026-10-29 (`TestEachVendorAgentBringsItsOwnProviderPack`, `TestCodexPackNeedsCodexAuth`) |
| <a id="OQ-PE3"></a>[OQ-PE3](#OQ-PE3) | *Ruling.* **Names key on the vendor's agent, not the company or the service**: the packs are `claude-auth`, `codex-auth` and `copilot-auth` (`openai-auth` and the landed `github-copilot` renamed; the proposed `anthropic` pack never made), and the profile is the bare agent name, `claude`, `codex`, `copilot`. People know these subscriptions by the agent, and the names must be short, predictable and consistent; the old pack and profile names are refused naming the new one | 2026-10-29 | [Names](#names) | ✅ 2026-10-29: packs renamed, profiles `claude`, `codex`, `copilot` (`TestTheProviderPacksDeclareAProfileNamedForTheAgent`); refusals through [PE-D7](#PE-D7) (`TestAFormerPackNameIsRefusedNamingTheNewOne`, `TestTheLaunchRefusesTheFormerCopilotProfileNamingTheNewOne`, `TestHostUndeclaredCopilotProfileNamesItsPack`) |
| <a id="OQ-PE4"></a>[OQ-PE4](#OQ-PE4) | *Ruling.* **The refusal types out the two-line declaration** when the undeclared name is in a selected agent's `built_in_providers`, and names the shipped pack when one declares the profile. The Happy Path Principle asks for the exact next step, and the manifests already hold every fact the message needs | 2026-10-29 | [The long tail](#the-long-tail) | ✅ 2026-10-29: pack hint `84d0d4fee` (`ShippedProfileDeclarers`); the typed declaration in `packload.UndeclaredProfileHelp` at the launch, `yolo host` and `yolo check` (`TestTheLaunchTypesOutTheDeclarationForABuiltInName`, `TestAnUndeclaredBuiltInNameTypesOutItsDeclaration`, `TestCheckTypesTheDeclarationForABuiltInName`), at the user config per [PE-D8](#PE-D8) |
| <a id="OQ-PE5"></a>[OQ-PE5](#OQ-PE5) | *Ruling.* **`claude-oauth-broker` moves into `claude-auth`**, beside its provider, as `openai-auth-broker` sits in `codex-auth`; `yolo loopholes list` attributes it there, and the docs and `AGENTS.md` naming it as claude's are repointed. The move changes no behavior | 2026-10-29 | [Security](#security) | ✅ 2026-10-29: `packs/claude-auth/loopholes/claude-oauth-broker` (`TestTheClaudeOAuthBrokerIsClaudeAuthsLoophole`, `TestEveryShippedLoopholeHasARow`, `TestEveryShippedLoopholePackSurvivesThePreflight`); `AGENTS.md` and the user guide repointed |
| <a id="PE-D1"></a>PE-D1 | *Design decision.* A vendor agent pack lists its vendor's provider in `built_in_providers` where its derive would write a row for it, so the vendor CLI on its own profile is covered by the existing built-in rule (no row, no env, no warning) and no new code path per agent; the `needs` binds the platform (`bindsPlatform`), which alone covered the Copilot CLI | 2026-10-29 | [The rule](#the-rule) | ✅ 2026-10-29, with no new listing: codex already maps `openai-codex` through its `plans` and `yolo_lists`, and claude's derive writes no row or env for `anthropic`, so the condition "where its derive would write a row" holds for neither (`TestClaudeOnTheClaudeProfileStaysOnItsOwnLogin`, `TestCopilotOnTheCopilotProfileStaysOnItsOwnLogin`). The [Per vendor](#per-vendor) table's "claude lists anthropic" is corrected to match |
| <a id="PE-D2"></a>PE-D2 | *Design decision.* A long-tail built-in provider earns a pack only for special handling: a credential service, a yolo-owned list, a plan mapping, a pinned endpoint, or a platform two agents must share | 2026-10-29 | [The long tail](#the-long-tail) | ✅ 2026-10-29: no long-tail pack shipped |
| <a id="PE-D3"></a>PE-D3 | *Design decision.* The `anthropic` provider declares no endpoint in v1; the bridge-carried subscription arm ([OQ-BR18](wire-bridge-gateway.md#decision-ledger)) adds one under the pin when it is built | 2026-10-29 | [Security](#security) | ✅ 2026-10-29: `packs/claude-auth` declares none |
| <a id="PE-D4"></a>PE-D4 | *Design decision.* A boolean `pinned_endpoint` on the provider contribution refuses any user-layer endpoint add or change, naming the fix; the three subscription packs set it | 2026-10-29 | [Security](#security) | ✅ 2026-10-29: `packdecl.Contribution.PinnedEndpoint`, enforced in `packload`'s provider composition (`TestASubscriptionProvidersEndpointIsPinned`, `TestPinnedEndpointIsAProviderKey`); scope per [PE-D9](#PE-D9) |
| <a id="PE-D5"></a>PE-D5 | *Design decision.* The Copilot declaration lives in its own pack, brought in by `copilot` through `needs`; pi keeps only the name in `built_in_providers` | 2026-10-29 | [Per vendor](#per-vendor) | ✅ 2026-10-29 `84d0d4fee`, as `packs/github-copilot` ([BI-D14](pi-codex-provider-shadowing.md#BI-D14)); renamed to `copilot-auth` under [OQ-PE3](#OQ-PE3), 2026-10-29 |
| <a id="PE-D6"></a>PE-D6 | *Design decision.* The provider's name stays the id the agents' own clients key it by, which is its `platform` (`anthropic`, `openai-codex`, `github-copilot`), because `built_in_providers.names` matches the yolo provider's name and an agent-named provider would need a `plans` remap in every agent pack with the built-in; the agent's name is carried by the pack and the profile, the names a user types | 2026-10-29 | [Names](#names) | ✅ 2026-10-29: provider ids unchanged |
| <a id="PE-D7"></a>PE-D7 | *Design decision, made in the build.* A former name is recorded by the pack that took it over, in a top-level manifest key `formerly` *(coined in the build)*: `{"packs": [...], "profiles": {"<old>": "<new>"}}`. The unknown-`packs`-entry refusal and the undeclared-profile refusal read it from the shipped manifests to name the new name; nothing is aliased. A manifest fact rather than a core table because core switches on no pack name, and top-level beside `needs` so an older reader skips it instead of dropping a contribution (`packs/releasedecode_test.go` lists it) | 2026-10-29 | [Names](#names) | ✅ 2026-10-29 (`TestAFormerlyRecordRoundTrips`, `TestAFormerlyRecordNoRefusalCouldUseIsRefused`, `TestTheFormerPackNamesNameTheirNewPacks`) |
| <a id="PE-D8"></a>PE-D8 | *Design decision, made in the build; departs from this doc's wording.* The typed declaration names the user config, `~/.config/yolo-jail/config.jsonc`, not `yolo-jail.jsonc`, because the `profiles` key is read at user scope only ([OQ-CS5](../reference/providers.md#profiles-and-options)): two lines in the workspace file would declare nothing. If the owner wants the workspace file, `profiles` first has to become a workspace key | 2026-10-29 | [The long tail](#the-long-tail) | ✅ 2026-10-29 (`TestAnUndeclaredBuiltInNameTypesOutItsDeclaration`, which declares it through the user config and selects it) |
| <a id="PE-D9"></a>PE-D9 | *Design decision, made in the build.* The pin guards ADDRESSES: after the user layer merges, every `base_url` the provider has must be one the pack declared, so adding or moving an address is refused while removing one (`"endpoints": null`) is allowed, since removing sends the token nowhere new. Tests that moved the subscription endpoint to a fake upstream now do it at the launch service's input (`repointCodexUpstream`, `internal/cli`) or through a local fork of `codex-auth` (`integration/wirebridgecodex_test.go`) | 2026-10-29 | [Security](#security) | ✅ 2026-10-29 (`TestASubscriptionProvidersEndpointIsPinned`) |

## Appendix: files read

- `/workspace/.yolo/durable/provider-packs-20261029/fable-brief.md`, `/workspace/.yolo/durable/provider-packs-20261029/fable-rulings-brief.md`
- `/workspace/.vantage.toml`
- `/workspace/packs/pi/pack.json`, `/workspace/packs/openai-auth/pack.json`, `/workspace/packs/github-copilot/pack.json`, `/workspace/packs/copilot/pack.json`, `/workspace/packs/codex/pack.json`
- `/workspace/packs/claude/pack.json` (header, `needs`, and provider-key lines), `/workspace/packs/opencode/pack.json` and `/workspace/packs/omp/pack.json` (`built_in_providers`), `/workspace/packs/cerebras/pack.json` and `/workspace/packs/zai/pack.json` (contribution kinds only)
- `/workspace/internal/cli/host.go` (the declaration check and the reaches-nothing refusal), `/workspace/internal/packload/profiles.go` (`UndeclaredProfileMessage`, `ShippedProfileDeclarers`), `/workspace/internal/packload/profiledisclosure.go` (`Warnings`, `bindsPlatform`), `/workspace/internal/packload/builtinproviders.go` and `/workspace/internal/packdecl/builtinproviders.go` (`BuiltInProviderFor`, `Names`, `Plans`, `YoloLists`), `/workspace/internal/packload/agentnamecollisions_test.go`
- `/workspace/docs/reference/providers.md` (headings, per-agent delivery, the built-in provider section, provider reach)
- `/workspace/docs/design/gateway-provider-packs.md` (header), `/workspace/docs/design/claude-login-without-interception.md` (header), `/workspace/docs/design/pi-codex-provider-shadowing.md` (BI-D13, BI-D14), `/workspace/docs/design/wire-bridge-gateway.md`, `/workspace/docs/design/providers-and-profiles-redesign.md`, `/workspace/docs/design/active-provider-sets.md`, `/workspace/docs/design/bedrock-plumbing.md` (searched for prior rulings on an Anthropic or subscription provider)
