---
title: "Enabling a provider is selecting its pack — and a vendor's agent selects its own"
date: 2026-10-29
status: in-review
stage: DESIGN
next: "Rule OQ-PE1 — the enablement model; OQ-PE2 to OQ-PE5 follow from it"
tags: [providers, profiles, packs, copilot, claude, codex, pi, subscriptions]
summary: "One rule for GitHub Copilot, the Claude subscription, the OpenAI subscription and pi's forty-odd built-in providers: a provider is usable only while a pack declaring it is selected, every subscription provider has exactly one pack, and a vendor's agent pack brings its own vendor's provider pack in through `needs`, because that agent's default is that provider. The long tail gets no packs: the undeclared-profile refusal prints the two-line declaration."
vantage:
  status-chip: true
---

# Enabling a provider is selecting its pack — and a vendor's agent selects its own

**Status:** 2026-10-29. Nothing built. Facts verified against the tree at `371118e10`; the Copilot pack lane the body assumes is in flight and not landed.

> **In short.** A provider becomes usable by exactly one act, selecting the pack that declares it, and a vendor's agent pack performs that act for its own vendor's provider through `needs`, since Claude Code, Codex and the Copilot CLI each default to that provider anyway. Everything else, pi's forty built-ins included, stays one user declaration away, and the refusal types it for you.

**Why it matters.** Today the three subscriptions are enabled three different ways: pi declares Copilot itself, the `claude` pack carries the Anthropic credential broker but no provider, and the OpenAI provider lives in a pack the `codex` agent deliberately does not bring in. A user cannot predict which `-p` names work from which packs they selected.

**The shape.** One *provider pack* per subscription provider (`github-copilot`, `anthropic`, `openai-codex`), each declaring provider, profile and any credential service; each vendor agent pack `needs` its own; agent packs list their vendor's provider in `built_in_providers`, so the generic built-in rule, no row and no warning, covers the vendor CLI on its own profile.

**Cost.** Two new packs and one rename; `codex` launches gain a host daemon; the landed Copilot declaration moves out of `packs/pi`; one recorded stance is revised.

**Start at [Recommendation](#recommendation).** [Options considered](#options-considered) is the argument for it.

**Needs your ruling:** [OQ-PE1](#OQ-PE1), [OQ-PE2](#OQ-PE2), [OQ-PE3](#OQ-PE3), [OQ-PE4](#OQ-PE4), [OQ-PE5](#OQ-PE5).

**Reads with:** [`providers.md`](../reference/providers.md) (the provider and profile system this doc arranges packs around), [`pi-codex-provider-shadowing.md`](pi-codex-provider-shadowing.md) (the built-in provider rule and the landed Copilot declaration, BI-D13), [`wire-bridge.md`](../reference/wire-bridge.md) (`needs`, and the openai-auth stance this doc revises).

---

## Summary

I recommend the owner's leaning with one refinement, which turns out to be the earlier Copilot ruling generalized. **A provider is usable only while a pack that declares it is selected**, which is already the rule for profiles ([OQ-CS6](../reference/providers.md#profiles-and-options)). **Each subscription provider has exactly one pack**, holding its provider, its profile and any credential service it needs. **A vendor's agent pack `needs` its own vendor's provider pack**, because selecting Claude Code, Codex or the Copilot CLI is already the decision to use that vendor's subscription; no other provider is brought in by anything. **The long tail ships no packs**: a built-in provider with no special handling is one user declaration, and the undeclared-profile refusal prints that declaration verbatim when the name is one a selected agent has built in. This revises the recorded stance that `codex` does not bring in the OpenAI credential pack ([OQ-PE2](#OQ-PE2)) and keeps the Copilot ruling as written.

## Terms

- **Provider**: a service an agent can talk to, declared with a `platform` (what service it is, such as `github-copilot`), optional endpoints, key names and model lists ([`providers.md`](../reference/providers.md)).
- **Profile**: a name a user selects per agent that picks one provider plus options; several profiles for one agent form an **active set** ([`providers.md`](../reference/providers.md#an-active-set-several-profiles-for-one-agent)). Declaration is mandatory: a selected name no selected pack or user config declares is refused.
- **Built-in provider**: one an agent ships its own client, list and login for, named in the agent pack's `built_in_providers`; yolo writes it no catalog row and the agent runs it on its own client ([`providers.md`](../reference/providers.md#a-provider-the-agent-has-built-in)).
- **Subscription provider**: a built-in provider backed by an interactive subscription login rather than a key ([`pi-codex-provider-shadowing.md`](pi-codex-provider-shadowing.md)). The three here are GitHub Copilot, the Claude subscription and the OpenAI (ChatGPT) subscription.
- **Provider pack** *(coined here)*: a shipped pack whose purpose is one provider: its `provider` and `profile` contributions and, where the provider needs one, its credential service. It installs no program. Not an agent pack, and not a gateway pack such as `openrouter`, though those are provider packs by this definition too.
- **Vendor agent pack** *(coined here)*: the agent pack whose program defaults to a subscription provider: `claude` for `anthropic`, `codex` for `openai-codex`, `copilot` for `github-copilot`.
- **Credential service**: a host-side `loophole` that owns a subscription login for every jail on the machine, such as `openai-auth-broker` and `claude-oauth-broker`.
- **`needs`**: a pack dependency, resolved host-side at selection as a transitive closure and printed with its cause ([`wire-bridge.md`](../reference/wire-bridge.md#needs--a-conditional-pack-dependency)).
- **Pinned endpoint** *(coined here)*: a provider address a user layer may not add or change, because the agent sends a subscription bearer token to whatever address it is given.

## Current state

| Provider | Where declared today | Credential service | Vendor CLI on its own profile |
| :--- | :--- | :--- | :--- |
| `github-copilot` | `packs/pi` (provider + profile, landed `23e607331`, [`packs/pi/pack.json`](../../packs/pi/pack.json)); a lane is moving it to `packs/github-copilot` with `copilot` `needs` it | none (each agent's own `/login`) | the Copilot CLI warns *reaches nothing*; the lane makes it run on its own client |
| `openai-codex` | `packs/openai-auth`, with profile `codex` and the one model list ([`packs/openai-auth/pack.json`](../../packs/openai-auth/pack.json)) | `openai-auth-broker`, in the same pack; `codex` only carries an `env` gated on it ([`packs/codex/pack.json`](../../packs/codex/pack.json)) | `codex` without the pack runs on its own login with no profile and no broker |
| Claude subscription | nowhere; no `anthropic` provider exists | `claude-oauth-broker`, in `packs/claude` ([`packs/claude/pack.json`](../../packs/claude/pack.json)) | no profile can name it, so an active set cannot hold it |
| pi's other ~40 built-ins, opencode's and oh-omp's | nowhere; the names are in each pack's `built_in_providers` | none | `-p pi=groq` is refused as undeclared, listing the declared names only |

The undeclared-profile refusal today reads *no profile named "X" is declared — a profile name must be declared by a selected pack's manifest or by your config's `profiles` key (declared: …)* ([`internal/packload/profiles.go`](../../internal/packload/profiles.go)). It names no pack and no declaration.

## Options considered

| Option | What `packs: ["pi"]` lets `-p pi=<name>` select | Clarity | Long-tail cost | Special handling | Credential channel | Verdict |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **A — one pack per provider, selected explicitly; agent packs bring nothing** | nothing until a pack is added | high: the `packs` list is the whole answer | one pack per built-in anyone wants | a natural home | the pack's loophole | **Rejected as written**: `packs: ["claude"]` could not name the subscription Claude Code runs on by default, so an active set cannot hold it; and `codex` users stay one unknown pack away from the broker that keeps their login alive |
| **B — every agent-built-in provider is implicitly declared for that agent** | all 45 of pi's | low: declared set differs per agent; 45 names in every disclosure | zero | none: a built-in needing a list, a plan, a broker or a pin still needs a pack, so two enablement rules coexist | none | **Rejected**: reverses the mandatory-declaration ruling and gives nothing-by-default a 45-name exception |
| **C — A, plus a vendor agent pack `needs` its own vendor's provider pack** | nothing (pi is not a vendor agent) | high: selecting an agent enables exactly the provider it defaults to, printed as `+ anthropic (needed by claude)` | as A for packs, but see the long tail: a recognized built-in is refused with its declaration typed out | the pack | the pack's loophole, present whenever the vendor CLI is | **Chosen**. The earlier Copilot ruling, applied to all three |
| **D — a generic `enable_providers` config key** | the names listed | medium: a second list beside `packs` | zero | still needs packs | still needs packs | **Rejected**: `packs` already is the one list of what is active; a second list that cannot carry a loophole duplicates it without replacing it |

## Recommendation

### The rule

1. **A provider is usable by an agent only while a selected pack, or the user's config, declares it**, and a selected profile must be declared (unchanged). Nothing is declared by an empty `packs` list.
2. **Every subscription provider has exactly one shipped pack**, named for the provider. It declares the `provider` (with its `platform`), one `profile` of the same name, the provider's model list where yolo owns one, and the credential service where the subscription needs one. It installs no program, so pi, opencode or oh-omp users select it alone to run on their own client.
3. **A vendor agent pack `needs` its vendor's provider pack unconditionally**, and lists that provider in its program's `built_in_providers`. The generic built-in rule then does the rest: no row, no env, no warning; the vendor CLI runs the profile on its own login. No other pack is brought in by any agent pack.
4. **No other built-in provider gets a pack** unless it needs special handling: a credential service, a yolo-owned model list (`yolo_lists`), a plan mapping (`plans`), a pinned endpoint, or a platform two agents must agree on. Everything else is a user declaration ([The long tail](#the-long-tail)).
5. **A subscription provider's endpoint is pinned** ([Security](#security)).

Core switches on no name in any of this: `needs`, `built_in_providers`, `platform` and the pin are manifest facts.

### Per vendor

| Vendor | Provider pack | Declares | Brought in by | Vendor CLI on its profile | Other agents |
| :--- | :--- | :--- | :--- | :--- | :--- |
| GitHub Copilot | `github-copilot` (new; the in-flight lane) | provider `github-copilot`, platform `github-copilot`, no endpoint, no key; profile `github-copilot` | `copilot` `needs` it; `copilot` lists it in `built_in_providers` | own client and login, no warning | pi, opencode, oh-omp: their own clients and `/login`; claude, codex: *reaches nothing* |
| Claude subscription | `anthropic` (new) | provider `anthropic`, platform `anthropic`, no endpoint in v1 ([PE-D3](#decision-ledger)), profile `anthropic`; `claude-oauth-broker` if [OQ-PE5](#OQ-PE5) moves it | `claude` `needs` it; `claude` lists `anthropic` in `built_in_providers` | own client and login, no `ANTHROPIC_*` written | pi, opencode, oh-omp: their own `anthropic` clients (pi's OAuth acceptance UNMEASURED); codex, copilot: *reaches nothing* |
| OpenAI subscription | `openai-auth`, renamed `openai-codex` under [OQ-PE3](#OQ-PE3) | unchanged: provider `openai-codex` with its endpoint and list, `openai-auth-broker`; profile `openai-codex` (and `codex` kept as a second profile over it) | `codex` `needs` it under [OQ-PE2](#OQ-PE2) | own client and login (its `plans` already maps it) | pi, opencode: own client, yolo's list; oh-omp: own list; claude: through the bridge, as today |

**The earlier Copilot ruling stands** as written; this doc generalizes it rather than revising it. **The openai-auth stance is revised** if [OQ-PE2](#OQ-PE2) is ruled as recommended.

### What this does not propose

No change to how a profile is selected, to active sets, to derives, or to the bridge's routes; no new config key; no pack for any provider in [rule 4](#the-rule)'s long tail; no fetched pack gaining `needs`.

## The long tail

Pi's other built-ins, opencode's and oh-omp's stay packless. The supported path is the user declaration a platform-only provider already permits, two lines in `yolo-jail.jsonc`:

```jsonc
"providers": { "groq": { "platform": "groq" } },
"profiles":  { "groq": { "provider": "groq" } }
```

Under [OQ-PE4](#OQ-PE4) the refusal prints exactly those lines when the undeclared name is in a selected agent pack's `built_in_providers`, and names the shipped pack instead when one declares the profile. A long-tail provider earns a pack only on [rule 4](#the-rule)'s grounds, and the pack's manifest is the record of what special handling it carries. The recognition reads selected packs' manifests and, for the pack hint, the shipped set, in the refusal path only; it reserves nothing ([`AGENTS.md`](../../AGENTS.md) forbids reading the embedded set to reserve a name, not to word a message).

## Happy Path

| Stop | Line (shape; final wording is the implementer's) |
| :--- | :--- |
| Undeclared profile a shipped pack declares | `packs: profile "github-copilot" selected for pi: no profile named "github-copilot" is declared. The shipped pack "github-copilot" declares it: add "github-copilot" to packs in yolo-jail.jsonc (declared: bedrock)` |
| Undeclared profile a selected agent has built in, no pack | `packs: profile "groq" selected for pi: no profile named "groq" is declared. pi has groq built in; declare it in yolo-jail.jsonc:` then the two lines above, then `(declared: bedrock)` |
| Undeclared profile, neither | today's message, unchanged |
| Vendor CLI on its own profile | the launch's profile line: `anthropic → claude: its own client and login`; no warning |
| Profile over a provider an agent has no client for | today's one line: `Warning: profile "github-copilot" reaches nothing for claude: …` with the `-p claude=` next step, unchanged |
| A `needs` addition | today's closure line, `+ openai-codex (needed by codex)`, unchanged |
| Endpoint added to a pinned provider | `providers.anthropic.endpoints: refused — "anthropic" is a subscription provider and its address is pinned, because the agent sends the subscription's bearer token to whatever address it is given. To reach another host, declare a provider under another name with its own api_key_env_name` |
| Old pack name after the rename | `packs: "openai-auth" is now "openai-codex"; write "openai-codex"` |

## Security

- **Pinned endpoints.** A Claude subscription bearer follows `ANTHROPIC_BASE_URL` (measured in a prior session), so any provider that could re-point Claude Code while it holds that token is an exfiltration channel. The provider contribution gains a boolean `pinned_endpoint` *(coined here)*: with it set, a user layer may not add or change an `endpoints` entry, and the refusal names the fix. All three subscription packs set it. This is the provider flag [OQ-BR2](pi-codex-provider-shadowing.md#10-decision-ledger)'s ledger deferred *until another subscription provider exists*; there are now three.
- **Endpoint-less by default.** `github-copilot` and `anthropic` declare no endpoint, so there is nothing to re-point and a profile `via` over them is inert, as the built-in rule already says. `openai-codex` keeps its endpoint because the bridge routes claude through it; it is pinned.
- **The derive invariant stays.** A derive never writes `ANTHROPIC_BASE_URL` for claude without `ANTHROPIC_AUTH_TOKEN` (today's dummy token on a routed launch, [`providers.md`](../reference/providers.md#per-agent-delivery)), so a routed claude never spends the subscription bearer off-host. A profile on `anthropic` writes neither.
- **Credentials cross only through loopholes.** A provider pack carries a credential service as a `loophole`; no pack `env` carries a token. Under rule 3 the service is present whenever the vendor CLI is, closing the multi-jail logout a `codex` user hits today with no broker. The one-file bargain, that pi's `auth.json` shares every provider's login machine-wide, is unchanged by this doc.

## Open questions

1. 💬 **OQ-PE1: Should selecting an agent pack enable the one provider that agent defaults to?**

   Dana writes `"packs": ["claude", "pi"]` and `"profile": {"pi": ["bedrock", "anthropic"]}` so pi can fall back from Bedrock to her Claude subscription. Today that is refused as undeclared, because no pack declares the Claude subscription at all.

   - **A — The vendor agent pack `needs` its provider pack.** Dana's config works as written; the launch prints `+ anthropic (needed by claude)`. Every `codex` launch also gains a host daemon.
   - **B — Nothing brings a provider pack in.** Dana reads the refusal, which names the pack, adds `"anthropic"` to `packs`, and relaunches. The `packs` list is the whole story, at the cost of one extra line in every config that runs a vendor CLI.

   <!-- vantage: question id=OQ-PE1 leaning="A — selecting Claude Code is already the decision to use the Claude subscription; the pack only makes that default nameable, and the credential broker rides along where it is needed." -->

   _Leaning:_ A — selecting Claude Code is already the decision to use the Claude subscription; the pack only makes that default nameable, and the credential broker rides along where it is needed.

   **Answer:**
   > _(empty — fill in when decided)_

2. 💬 **OQ-PE2: Should every Codex launch run the OpenAI credential broker?**

   Sam runs `"packs": ["codex"]` in two workspaces. Each jail's Codex refreshes the one single-use ChatGPT refresh token itself, so the second jail's refresh logs the first out, and nothing told Sam that a pack named `openai-auth` exists to prevent it. The recorded stance is that `openai-auth` joins a launch only when selected.

   - **A — `codex` `needs` the provider pack.** Sam's two jails share one broker; the launch prints `+ openai-codex (needed by codex)`. A Codex user on Bedrock gets a broker nothing calls.
   - **B — Keep the stance.** Sam keeps hitting the logout until the refusal or a guide points at the pack; the `codex` pack stays daemon-free.

   <!-- vantage: question id=OQ-PE2 leaning="A — parity with claude, and the broker exists for exactly Sam's case; an idle daemon costs less than a lost login." -->

   _Leaning:_ A — parity with claude, and the broker exists for exactly Sam's case; an idle daemon costs less than a lost login.

   **Answer:**
   > _(empty — fill in when decided)_

3. 💬 **OQ-PE3: Should the OpenAI pack and profile take the provider's name?**

   Lee sees `packs: ["openai-auth"]` and `-p pi=codex` beside `packs: ["github-copilot"]` and `-p pi=github-copilot`, and asks why one pack is named for its service and one profile for an agent. `-p codex=codex` is legal and reads as a typo. The pi pack already names its Copilot profile `github-copilot`, not `copilot`, to keep agent names out of profile names.

   - **A — Rename pack to `openai-codex`, add profile `openai-codex`, keep `codex` as a second declared profile over the same provider.** Lee's old `packs` entry is refused with the new name; `-p codex` keeps working.
   - **B — Rename the pack only.** Same refusal; profile stays `codex`.
   - **C — Leave both.** No churn; the asymmetry stays.

   <!-- vantage: question id=OQ-PE3 leaning="A — pack, provider and profile share one name for all three vendors, and the kept `codex` profile costs one line." -->

   _Leaning:_ A — pack, provider and profile share one name for all three vendors, and the kept `codex` profile costs one line.

   **Answer:**
   > _(empty — fill in when decided)_

4. 💬 **OQ-PE4: Should the refusal type out the declaration for a provider the agent has built in?**

   Priya runs `-p pi=groq`. pi has `groq` built in and would run it on its own client and key, but the launch refuses: *no profile named "groq" is declared … (declared: bedrock)*. She has to find in a reference that a platform-only provider plus a profile is two lines of config, and spell them herself.

   - **A — Print the two-line declaration** when the name is in a selected agent's `built_in_providers`, and the pack name when a shipped pack declares the profile. Priya pastes two lines and relaunches.
   - **B — Today's message**, and a guide section listing the two lines.

   <!-- vantage: question id=OQ-PE4 leaning="A — the Happy Path Principle asks for the exact next step, and the manifests already hold every fact the message needs." -->

   _Leaning:_ A — the Happy Path Principle asks for the exact next step, and the manifests already hold every fact the message needs.

   **Answer:**
   > _(empty — fill in when decided)_

5. 💬 **OQ-PE5: Does the Claude credential broker move to the `anthropic` pack?**

   Today `claude-oauth-broker` is a contribution of `packs/claude`, and it serializes refreshes of Claude Code's own credential file; pi's Anthropic login lives in pi's `auth.json`, which the broker does not read. Under the rule a provider pack holds its provider's credential service, which is where `openai-auth-broker` already is.

   - **A — Move it.** Both subscription brokers live beside their providers; `yolo loopholes list` attributes it to `anthropic`. Several design docs and `AGENTS.md` that name it as claude's are repointed.
   - **B — Leave it in `claude`.** The rule reads "a provider pack holds a credential service that more than one agent spends"; the OpenAI broker qualifies, Claude's does not until pi's login shares it.

   <!-- vantage: question id=OQ-PE5 leaning="A — one placement rule beats a rule with a counting clause, and the move changes no behavior." -->

   _Leaning:_ A — one placement rule beats a rule with a counting clause, and the move changes no behavior.

   **Answer:**
   > _(empty — fill in when decided)_

## Decision ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| <a id="PE-D1"></a>PE-D1 | *Design decision.* A vendor agent pack lists its vendor's provider in `built_in_providers`, so the vendor CLI on its own profile is covered by the existing built-in rule (no row, no env, no warning) and no new code path per agent | 2026-10-29 | [The rule](#the-rule) | — |
| <a id="PE-D2"></a>PE-D2 | *Design decision.* A long-tail built-in provider earns a pack only for special handling: a credential service, a yolo-owned list, a plan mapping, a pinned endpoint, or a platform two agents must share | 2026-10-29 | [The long tail](#the-long-tail) | — |
| <a id="PE-D3"></a>PE-D3 | *Design decision.* The `anthropic` provider declares no endpoint in v1; the bridge-carried subscription arm ([OQ-BR18](wire-bridge-gateway.md#decision-ledger)) adds one under the pin when it is built | 2026-10-29 | [Security](#security) | — |
| <a id="PE-D4"></a>PE-D4 | *Design decision.* A boolean `pinned_endpoint` on the provider contribution refuses any user-layer endpoint add or change, naming the fix; the three subscription packs set it | 2026-10-29 | [Security](#security) | — |
| <a id="PE-D5"></a>PE-D5 | *Design decision.* The landed `packs/pi` Copilot declaration moves to `packs/github-copilot` whole; pi keeps only the name in `built_in_providers` | 2026-10-29 | [Per vendor](#per-vendor) | — |

## Appendix: files read

- `/workspace/.yolo/durable/provider-packs-20261029/fable-brief.md`
- `/workspace/.vantage.toml`
- `/workspace/packs/pi/pack.json`, `/workspace/packs/openai-auth/pack.json`, `/workspace/packs/copilot/pack.json`, `/workspace/packs/codex/pack.json`
- `/workspace/packs/claude/pack.json` (header, `needs`, and provider-key lines), `/workspace/packs/opencode/pack.json` and `/workspace/packs/omp/pack.json` (`built_in_providers`), `/workspace/packs/cerebras/pack.json` and `/workspace/packs/zai/pack.json` (contribution kinds only)
- `/workspace/internal/cli/host.go` (the declaration check and the reaches-nothing refusal), `/workspace/internal/packload/profiles.go` (`UndeclaredProfileMessage`), `/workspace/internal/packload/profiledisclosure.go` (`Warnings`)
- `/workspace/docs/reference/providers.md` (headings, per-agent delivery, the built-in provider section, provider reach)
- `/workspace/docs/design/gateway-provider-packs.md` (header), `/workspace/docs/design/claude-login-without-interception.md` (header), `/workspace/docs/design/pi-codex-provider-shadowing.md`, `/workspace/docs/design/wire-bridge-gateway.md`, `/workspace/docs/design/providers-and-profiles-redesign.md`, `/workspace/docs/design/active-provider-sets.md`, `/workspace/docs/design/bedrock-plumbing.md` (searched for prior rulings on an Anthropic or subscription provider)
