# `zai` — the official pack that ships z.ai as a provider

The first real consumer of the provider/profile pair, and the worked example of
[the provider system](../../docs/reference/providers.md), which is the authority for the
mechanism: z.ai's service facts as one `kind: "provider"` contribution, and the selection that
points the agents at them as one `kind: "profile"`. The pack installs **no CLI**. Your own
config shrinks to an API key: select the pack, set one variable, and every selected agent that
can reach z.ai runs on GLM, by whatever protocol it speaks.

The **GLM Coding Plan** (z.ai's coding-plan subscription; z.ai is Zhipu AI's international
brand, [docs.z.ai](https://docs.z.ai/devpack/tool/others)) speaks **both** wire protocols, each
on its own base URL, and **one API key from the z.ai console serves both**. That is the property
the `endpoints`-by-protocol schema exists for: one provider, one key, any shape an agent needs
([OQ-Z2](../../docs/reference/providers.md#oq-z2)).

**The OpenAI route speaks chat completions only.** Measured with an authenticated probe:
`POST /v4/responses` is 404 on both the plain and coding-plan routes, while
`/v4/chat/completions` answers on the same host
([OQ-Z1](../../docs/reference/providers.md#oq-z1)). The measurement covers those two routes and
makes no claim about any other GLM route.

> [!WARNING]
> **A provider's HTTP surface is not an agent's config vocabulary, and measuring one says nothing
> about the other.** The probe was once read as evidence for a *derive default*, which it is
> not: an omitted `wire_api` means the agent's own single accepted value where it has one, and
> the derive's choice where it does not
> ([the canonical vocabulary](../../docs/reference/providers.md#the-canonical-wire_api-vocabulary),
> [OQ-Z7](../../docs/reference/providers.md#oq-z7)).

Which agents the pack reaches is narrower than the protocol list suggests. **claude** and
**copilot** get it through their own packs' env derives. **pi**, **oh-omp** and **opencode**
each have a zai of their own and run it on that
([a provider the agent has built in](../../docs/reference/providers.md#a-provider-the-agent-has-built-in)).
**codex** is excluded, the pairing being unwireable, and **agy** and **muse-code** take no
provider at all
([provider reach](../../docs/reference/providers.md#provider-reach-which-agents-a-provider-can-reach-at-all)).

**What it ships, and what it deliberately does not:** facts and a pointer. The endpoints, the
wire protocols, the model aliases and *which variable holds the key* are the pack's; the key
itself is unrepresentable in a manifest — `api_key_env_name` carries a NAME, never a value.

## The user's entire setup

```jsonc
// ~/.config/yolo-jail/config.jsonc
{
  "packs": ["claude", "zai"]           // the provider pack beside the agents you use
}
```

```bash
# ~/.config/yolo-jail/env (untracked, 0600) — or the invoking environment:
ZAI_API_KEY=<key>                      # the ONLY secret, spelled once
```

Then `yolo -p zai` (or the persistent spelling, `"profile": "zai"`, in your user config).

## What lands where

| Agent | What it gets | Channel |
|---|---|---|
| claude | the full env block [Z.AI's Claude Code guide recommends](https://docs.z.ai/devpack/tool/claude): `ANTHROPIC_BASE_URL=https://api.z.ai/api/anthropic`, `ANTHROPIC_AUTH_TOKEN`, and `ANTHROPIC_DEFAULT_{OPUS,SONNET,HAIKU}_MODEL`. The selected real model ID supplies every tier unless a provider declares a tier-specific ID; each carries Claude Code's `[1m]` suffix because the default GLM-5.3 context window is 1,000,000. It also gets `CLAUDE_CODE_AUTO_COMPACT_WINDOW=1000000`, `API_TIMEOUT_MS=3000000`, and `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`. | the agent pack's own env derive (`yolo.env` in `packs/claude/derive.lua`), composed at launch — the endpoint is configuration, the token is relayed from the hydrated variable, and nothing is written to a file |
| pi | **no catalog entry**: pi's own `zai` already calls the coding plan, reading `ZAI_API_KEY`. A selected profile writes `defaultProvider = "zai"` and `enabledModels = ["zai/**"]`, and the profile's `model` option as pi's own id | its derive, reading the agent's own providers from `packs/pi` |
| opencode | **no catalog entry**: yolo's zai is opencode's own `zai-coding-plan` (opencode's `zai` is the metered `api.z.ai/api/paas/v4`), and the launch delivers the key to it as `ZHIPU_API_KEY`, the variable that provider reads. A selected profile writes `enabled_providers` and, from the profile's `model` option, `model` | its derive, and the `plans` entry in `packs/opencode` |
| oh-omp | **nothing in its model file**: oh-omp's own `zai` speaks Anthropic Messages at the plan's anthropic route | its derive, reading the agent's own providers from `packs/omp` |
| copilot | BYOK env: `COPILOT_PROVIDER_BASE_URL` = the **anthropic** route (the derive prefers it, [D-3](../../docs/reference/providers.md#reach-d-3)), `COPILOT_PROVIDER_TYPE=anthropic`, `COPILOT_MODEL=glm-5.3`, `COPILOT_PROVIDER_API_KEY` — no `WIRE_API`, which speaks only to the openai type | the copilot pack's env derive |
| codex | **nothing — no entry and no selection** | codex speaks `responses` only and z.ai's openai route speaks chat completions only, so no `wire_api` value makes the pairing work — the derive emits **nothing** for a canonical value its agent cannot spell, rather than inheriting a default that would hide the mismatch behind a 404 at the first request ([provider reach](../../docs/reference/providers.md#provider-reach-which-agents-a-provider-can-reach-at-all)), and the same reachability gate keeps the selection off with it. Codex has no z.ai route; the anthropic endpoint is claude's, via claude's env derive. |

The pack's model list reaches claude and copilot only: pi, oh-omp and opencode use their own
lists, and pi and opencode take only the profile's `model` option, as their own id.

A launch in which some agent's profile selects zai and that never hydrates `ZAI_API_KEY`
**refuses outright** (`yolo run`'s pre-flight) — the alternative is a jail whose first API
call fails somewhere unattributable. `YOLO_ALLOW_MISSING_PROVIDERS=1` continues loudly
instead. Selecting the pack with no agent on zai demands nothing: the key reaches only an agent
whose profile selects zai, never a shell or another agent
([the credential gate](../../docs/reference/providers.md#the-credential-gate)), so an
unselected provider's key would reach nobody.

## Selection

Presence is not selection: with the pack selected and no profile, nothing routes to zai. The
pack ALSO ships the selection that points the agents at it: one `kind: "profile"` contribution,
`{"name": "zai", "provider": "zai"}` — a named selection over the provider, which is the whole
kind ([providers.md](../../docs/reference/providers.md#declaring-and-selecting-a-profile)). The
selection is not a second channel: each agent's derive writes its own selection key, in that
agent's own dialect. This pack ships no per-agent config-overlay and no claude special case:
the overlay that carried claude's endpoint was deleted on 2026-09-02 with `env_shape`, which is
now an ordinary unknown key, and every agent's own derive translates the pack's facts
([OQ-PT3](../../docs/reference/providers.md#oq-pt3)). Overrides, such as an extra model
alias, are lines of `providers.zai` in your user config, composed over the pack's facts;
authoring a whole provider is never required.

Selecting it — `-p zai` before the `--`, or `-p pi=zai` for one agent, or the
persistent `"profile": {"pi": "zai"}` — is what makes each agent's derive write its own
selection key (pi's `defaultProvider`/`defaultModel`, opencode's `model`) from the
profile's `model` option. The Coding Plan catalog contains exactly `glm-4.6`, `glm-5.3`,
and `glm-5.3-flash`; its default is `glm-5.3`. These are **wire-true IDs**: Pi and
OpenCode send them verbatim. Claude alone appends its `[1m]` context-1M syntax to the
selected ID before it sends the request. GLM-5.3 and GLM-5.3 Flash both have a 1,000,000
token context window; GLM-4.6 has a 200,000-token window, so select it with a profile
that sets `context_window: "200000"` when using Claude.

Your own profile states the option rather than overriding the pack's:

```jsonc
// ~/.config/yolo-jail/config.jsonc (user scope — the key is refused at workspace scope)
{
  "profiles": {"zai-flash": {"provider": "zai", "model": "glm-5.3-flash"}},
  "profile": {"pi": "zai-flash", "opencode": "zai-flash"}
}
```

A profile that selects nothing does nothing, and deactivating one does not write the
agent's surface back — whatever a selection wrote stays until the agent's own choice
replaces it. Claude is selected by the same spellings (`-p zai`, `-p
claude=zai`, `"profile": {"claude": "zai"}`) and reads none of those keys — it has no
catalog and no selection key, and the env derive in the table above is its whole delivery —
but it is not outside the option: its `ANTHROPIC_DEFAULT_OPUS_MODEL` is the same alias
lookup the other two make.

## Verifying

```console
$ yolo pack lint packs/zai          # claims + the strict manifest read
$ yolo pack footprint zai           # both claims: the provider (two endpoints, the key by
                                    # name) and the selection over it
$ ZAI_API_KEY=x yolo -p zai         # pi and opencode answer to zai; claude gets the pair
$ yolo -p zai -- claude --version   # the per-invocation spelling, keyed on the CLI
```

The last two refuse with `ZAI_API_KEY is not set in this launch's environment` when the key
is missing — measured in a nested jail with a fresh image, refusal and composition both.
