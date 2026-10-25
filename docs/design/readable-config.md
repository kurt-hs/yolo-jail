---
title: "Config that reads right the first time: the shape is the problem, and JSONC is a smaller one"
date: 2026-10-24
status: in-review
stage: DESIGN
next: "Rule OQ-RC1 (which shape changes go in) and OQ-RC2 (which file format); OQ-RC3 to OQ-RC6 follow, and none of them needs code first"
tags: [config, packs, manifest, providers, profiles, broker, grants, format, toml, readability, design]
summary: "The maintainer's own config takes 178 lines to say about 20 things, and lines such as `grant read *`, `grant_kinds` and three `bedrock`s in a row are misread by the person who wrote them. Measured on that file, the shape (written defaults, repeated facts, mechanism-named keys, one switch spread over three keys) accounts for nearly all of the length: the same content in today's JSONC, reshaped, is about 58 lines, and in TOML about 53. The format changes how noisy each line looks, not how many there are or what they mean. So this doc proposes a vocabulary built on what the user is deciding (what to install, what models the agents use, what a jail may do with your logins), the same rules applied to pack manifests, and TOML as the recommended format, with JSONC kept as the fallback. It absorbs the open manifest-format questions and the provider-versus-profile question."
vantage:
  status-chip: true
---

# Config that reads right the first time: the shape is the problem, and JSONC is a smaller one

**Status:** 2026-10-24. Nothing is built. The measurements use the effective user config this jail
inherited at launch (`~/.config/yolo-jail/config.jsonc` inside the jail, generated
2026-10-25T00:30:59Z), this repository's [`yolo-jail.jsonc`](../../yolo-jail.jsonc), and the 21 packs
staged under `/ctx/packs/` at `758e4c759`. Working files are kept under the durable directory and are
not cited here.

**Needs your ruling:** [OQ-RC1](#OQ-RC1) to [OQ-RC6](#OQ-RC6).

> **In short.** A config a stranger can read correctly comes from its shape far more than from its
> file format. Each key should name what the user decides, in their own words, and say it once. The
> format choice comes second and is smaller: it decides how much punctuation sits on each line.

**Why it matters.** The maintainer, 2026-10-24: *"I want configs that can be read, without much
foreknowledge, and be understood the right way … look at my configs, they're SUPER verbose and
impossible for a human."* The same day, two lines in his own config were misread by him: a user
read grant (`{"kind": "read", "repo": "*"}`) looked like a grant on every repository anywhere,
and `grant_kinds: ["branch"]` beside it looked like *"granting read on branches"*. A config its
author misreads is a security problem, not only a style one. The broker's approval prompt is
where he decides what a jail may do, and it showed `+ grant read * (from your user config, for
every workspace)`.

**The shape.** The user config gets sections named for what the user decides: what to install,
which models the agents use, what a jail may do with your logins, and the machine's resources.
Defaults are never written, a fact is stated once, and a pack's switch is its settings. Pack
manifests follow the same rules: big content lives in files laid out the way they land, not in
one list entry per file. [§4](#4-the-proposed-vocabulary) is the whole proposal.

**Cost.** Every config file and every pack manifest is rewritten once, with no aliases: an old
spelling is refused, naming the new one, and `yolo config migrate` does the rewrite. A yolo older
than the change does not read the new files ([OQ-RC6](#OQ-RC6)).

**Start at:** [§2](#2-where-todays-config-misleads), the misreadings, then
[§5](#5-the-same-content-in-four-formats), the formats side by side.

**Reads with:** [`manifest-language.md`](manifest-language.md) and
[`pack-conventions.md`](pack-conventions.md), whose open questions this doc takes over
([§8](#8-what-this-doc-takes-over)); [`providers-and-profiles-redesign.md`](providers-and-profiles-redesign.md),
whose [OQ-PP1](providers-and-profiles-redesign.md#OQ-PP1) [OQ-RC3](#OQ-RC3) answers; and
[`boundary-broker.md`](boundary-broker.md), where today's grant rules are ruled.

---

## Defined terms

- **Cold read** (coined here): someone who has never opened `yolo config-ref` reads a line and says
  what it does. A line **passes** when what they say is right. It **fails** when they say nothing,
  or, worse, something plausible and wrong.
- **Shape**: which keys exist, how they nest, which values are written down and which follow from
  others. Independent of the file format.
- **Format**: the syntax the shape is written in: JSONC (JSON with comments, today's), TOML, YAML
  or KDL.
- **Provider**: a place models are served from (`bedrock`, `openai-codex`), with its address,
  region and model list. **Profile**: today, a named choice of one provider plus options such as
  the starting model; it is what `-p <name>` names.

## 1. The test every line must pass

A config line is right when a cold read of it is right. Four rules follow, and every proposal below
is checked against them:

1. **Name the decision, not the mechanism.** `push = ["main"]` passes, and `{"kind": "branch",
   "branch": "main"}` under `brokered.git.grants` fails, because `brokered` and `kind` name how yolo
   implements the permission, not what the user chose.
2. **Say each fact once.** A model's id written as its key and again as `id`, its maker written as
   `vendor` when the id already says `anthropic`, the same three facts on each of six models: every
   repeat is a place the copies can disagree, and a reader has to check that they don't.
3. **Write nothing that is the default.** A value matching the default makes a reader ask why it
   was written. This repository's `"network": {"mode": "bridge"}` is the default.
4. **One switch, one place.** Turning `aws-auth` on today takes the pack in `packs`,
   `loopholes.aws-auth.enabled: true` and its `settings`. A reader who sees only one of the three
   gets the state wrong.

A fifth rule is about the format, not the shape: **the punctuation should not outweigh the content.**
[§5](#5-the-same-content-in-four-formats) measures it.

## 2. Where today's config misleads

Every line below is from the maintainer's own effective config or this repository's. The middle
column is what the maintainer, or a reasonable cold reader, took the line to mean.

| Line today | Read cold as | What it means |
| :--- | :--- | :--- |
| `"brokered": {"git": {"grants": [{"kind": "read", "repo": "*"}]}}` | read access to every repository anywhere | fetch from each workspace's own approved remotes |
| `"grant_kinds": ["branch"]` beside that read grant | "granting read on branches" | the most a project may ask for: pushing to branches it names |
| `"brokered"` | nothing; the maintainer: *"I don't understand 'brokered'"* | things a jail may do using your host logins |
| `"profile": {"*": "bedrock", "claude": null}` | `null` reads as "unset" or "default" | every agent uses `bedrock` except claude, which keeps its own login |
| `"profiles": {"bedrock": {"provider": "bedrock", "model": …}}` | three `bedrock`s: is that one thing or three? | a profile named after the provider it picks, plus a starting model |
| `"loopholes": {"aws-auth": {"enabled": true, "settings": {…}}}` with `"aws-auth"` also in `packs` | two switches, either of which might be the real one | the pack is installed by `packs`, and its service is off until `enabled` says otherwise |
| `"input": ["text", "image"]` beside `"cost": {"input": 10}` | two meanings of one word | the first is what the model accepts, the second is its price per million input tokens |
| each model's `"id"` equal to its key; `"vendor": "anthropic"` beside `global.anthropic.…` | possibly different values | the same value, written twice |
| `"network": {"mode": "bridge"}` in this repository | this repository needs bridge networking | the default, restated |
| 120 commented-out lines in this repository's config, 12 live lines | the comments are settings | a reference template `yolo init` wrote |

**Measured size.** The effective user config is 170 lines of JSON, plus about 8 lines of broker grants
that the jail's copy leaves out, so 178 in all. It sets one MCP server, three mise tools, 21 packs,
one loophole, two broker grants, a profile selection and six models. The models alone are 109 lines,
and in them `id`, `input`, `max_tokens` and `reasoning` are each written six times, every time with
the same value except `id`, which repeats its key. This repository's config is 133 lines, of which
12 are live.

**A caveat on the measurement.** The jail sees a generated, fully expanded dump of the host files
(`config.jsonc`, `overrides.jsonc` and the work machine's `work.jsonc`), not the files themselves.
The host files may be laid out more compactly. The content is the same, and the misreadings above
are about content.

## 3. Shape or format? Measured on the maintainer's config

The maintainer's question: *"I don't know if jsonc is wrong, or just our shape."* The way to answer
it is to rewrite the same content in the [§4](#4-the-proposed-vocabulary) shape, then write that
shape in each format, and count.

| The same content | Lines |
| :--- | ---: |
| Today's shape, JSONC (the effective config, plus broker grants) | 178 |
| The [§4](#4-the-proposed-vocabulary) shape, JSONC, laid out by hand (short objects on one line) | 58 |
| The [§4](#4-the-proposed-vocabulary) shape, JSONC, fully expanded the way a formatter writes it | 133 |
| The [§4](#4-the-proposed-vocabulary) shape, TOML, with four explanatory comments | 53 |

**The shape removes about two-thirds of the lines whatever the format.** Going from JSONC to TOML
saves a few more lines. More of the difference is per line: no quoted keys, no closing braces or
commas, and section headings a reader can scan. **The format matters most when a formatter expands
it.** JSONC that a tool rewrites grows back toward 133 lines, while TOML's inline tables stay as
written. That counts in practice: yolo-generated files, `yolo config dump` and this jail's
inherited copy all expand.

The packs show the same pattern ([§6](#6-packs-the-same-rules)): of the `matt` pack's 860 lines,
271 are 39 `files` entries, 32 of which only copy a file or folder to the same path, and 425 are
`config-overlay` contents, which are pi's own settings, written inside the manifest.

## 4. The proposed vocabulary

The user config gets sections named for decisions. Each section says which files may hold it.
Today's scope rules carry over unchanged: what decides where inference goes, or what a jail may do
with your logins, stays user scope only, because a workspace file is the agent's to edit.

| Section | Holds | Replaces today | Scope |
| :--- | :--- | :--- | :--- |
| `packs`, `pack_folders` | what the jail is built from; folders where a bare pack name is also looked up | `packs` (full paths written out) | user |
| `install.nix`, `install.mise` | extra tools | `packages`, `mise_tools` | both |
| `mcp.<name>` | MCP servers | `mcp_servers` | both |
| `as_you.<pack>` | what a jail may do with your host logins: git, GitHub, AWS | `brokered.*`, `loopholes.<name>.enabled` and `settings` for login-serving packs | user for the most it may do, workspace for what a project asks |
| `use` | which models each agent runs on | `profile` | user |
| `providers.<name>` | where models come from, the starting model, the model list | `providers`, the common case of `profiles` | user (models and region from either) |
| `profiles.<name>` | a second configuration of one provider, when you need one | `profiles` | user |
| `machine` | resources, GPU, devices, network | `resources`, `gpu`, `devices`, `kvm`, `network` | both |

A key not listed keeps its name. Most of the 53 top-level keys `yolo config-ref` documents (4 of
them `REMOVED`) are host-integration settings that a typical config never sets.

### 4.1 The levers, each one syntax-free

Each lever below can be adopted or vetoed on its own ([OQ-RC1](#OQ-RC1)).

- **L1. Defaults are never written, and `yolo init` writes a short file.** It writes the few lines
  a project needs and one comment naming `yolo config-ref`. The 120-line template goes. `yolo
  check` warns about a value equal to its default and names the line.
- **L2. A key is the value it names.** A model's key is its id unless an `id` says otherwise. On a
  provider whose platform encodes the maker in the id (Bedrock's `global.anthropic.…`), `vendor`
  follows from the id unless it is written.
- **L3. Shared facts are stated once.** A provider's `every_model` table holds the facts each of
  its models shares unless the model says otherwise. This is per provider, unlike today's
  `model_facts` and `extends`, which share facts across routes to the same model.
- **L4. A pack's settings are its switch.** Writing `[as_you.aws-auth] profile = …` turns the
  service on. A pack whose service needs no settings is on when the pack is selected, unless
  `off = true`. This amends [R1 and R2](../reference/loophole-system.md#principles) ("presence
  never activates"; "absent `default_enabled` means off"). It keeps their reason: the service runs
  because the user said so, and the settings are where they said it. `aws-auth` is off by default
  today precisely because it is useless without a profile.
- **L5. Words carry their meaning.** `brokered` → `as_you`, `grants` and `grant_kinds` → `fetch`,
  `push` and `read` ([§4.2](#42-what-a-jail-may-do-with-your-logins)), `profile` → `use` with
  `everyone` for `*` and `"own login"` for `null`, a model's `input` → `accepts`, `cost` → `price`,
  `context_window` → `context`.
- **L6. Your own pack folders are named once.** `pack_folders = ["~/.dotfiles/yolo-packs"]`, and
  `"matt"` finds `~/.dotfiles/yolo-packs/matt`. A bare name that matches both a shipped pack and a
  folder pack is refused, naming both, so a folder never silently shadows a shipped pack.

### 4.2 What a jail may do with your logins

This is the shape proposed in chat on 2026-10-24, reworked; [OQ-RC4](#OQ-RC4) names the section. The user config states the
most any project may get. A project states what it asks for, and every ask is a line in the launch
approval prompt.

```toml
# your config: the most any project's jail may do
[as_you.git]
fetch = true                      # or false, or a list of repositories
push = "when a project asks"      # or "never" (the default)

[as_you.github]
read = true

# a project's yolo-jail.toml: what this project asks for
[as_you.git]
push = ["main"]                   # branch names, or "new branches"
```

The repositories are always the project's own remotes plus the ones its `as_you.github.repos`
names. Both kinds are approved at launch exactly as today, and `*` disappears from the vocabulary.
Approval prompt lines become `+ git: fetch from mschulkind-oss/yolo-jail` and `+ git: push to main`.
A GitHub `write` setting arrives once a jail's write request can reach `yolo approve`, which is
not built (`internal/ghbroker/window.go` says so); until then the key is refused with that reason.

### 4.3 Models: one name for the common case

Today the maintainer writes a provider `bedrock`, a profile `bedrock` that picks it plus a starting
model, and a selection `{"*": "bedrock"}`. The proposal ([OQ-RC3](#OQ-RC3), option B):

```toml
[use]
everyone = "bedrock"              # a provider or a profile name
claude = "own login"

[providers.bedrock]
region = "us-east-1"
model = "global.openai.gpt-6.1-sol"   # where agents start
```

A provider is selectable by its own name, and it starts on its `model`. A profile is needed only
for a second configuration of one provider (`bedrock-opus`, `bedrock-bridge`). Provider and profile
names share one namespace, and a collision is refused. A shipped pack's profile that only repeats
its provider's name (`bedrock`'s `bedrock`) is deleted. A profile with a `via` stays. `-p`
names whatever `use` names.

## 5. The same content in four formats

The same excerpt each time: the logins section and one model. Full renderings of the maintainer's
config are in [Appendix A](#appendix-a-the-maintainers-config-whole).

**JSONC**, today's format in the new shape:

```jsonc
{
  "as_you": {
    "git": {"fetch": true, "push": "when a project asks"},
    "aws-auth": {"profile": "hssandbox-BedrockInference", "unnarrowed": true}
  },
  "use": {"everyone": "bedrock", "claude": "own login"},
  "providers": {"bedrock": {
    "region": "us-east-1",
    "models": {"global.openai.gpt-6.1-sol": {"name": "Sol 6.1", "context": 272000}}
  }}
}
```

**TOML:**

```toml
[as_you.git]
fetch = true
push = "when a project asks"

[as_you.aws-auth]
profile = "hssandbox-BedrockInference"
unnarrowed = true

[use]
everyone = "bedrock"
claude = "own login"

[providers.bedrock]
region = "us-east-1"
models."global.openai.gpt-6.1-sol" = { name = "Sol 6.1", context = 272_000 }
```

**YAML:**

```yaml
as_you:
  git: {fetch: true, push: when a project asks}
  aws-auth: {profile: hssandbox-BedrockInference, unnarrowed: true}
use: {everyone: bedrock, claude: own login}
providers:
  bedrock:
    region: us-east-1
    models:
      global.openai.gpt-6.1-sol: {name: Sol 6.1, context: 272000}
```

**KDL:**

```kdl
as-you {
    git fetch=#true push="when a project asks"
    aws-auth profile="hssandbox-BedrockInference" unnarrowed=#true
}
use everyone="bedrock" claude="own login"
provider "bedrock" region="us-east-1" {
    model "global.openai.gpt-6.1-sol" name="Sol 6.1" context=272000
}
```

| | JSONC | TOML | YAML | KDL |
| :--- | :--- | :--- | :--- | :--- |
| Known without learning | everyone | most developers: Cargo, `pyproject.toml`, and `mise.toml`, which this config already feeds | most developers: GitHub Actions, Kubernetes | few |
| Punctuation per line | high: quoted keys, braces, commas | low | lowest | low |
| A formatter expands it | yes, to 133 lines | no: inline tables stay | depends on the tool | no |
| Misreads values | no | no | **yes.** `python: 3.10` is the number 3.1, and `no` is false in older parsers. A mise tool version is exactly this kind of value | no |
| Indentation is meaning | no | no | yes: an indentation slip moves a key silently | no |
| Deep nesting | fine | headings grow long (`[providers.bedrock.models]`) | fine | fine |
| Go library | vendored today | one new vendored dependency | one new vendored dependency | one new, less used, dependency |
| Comments | yes | yes | yes | yes |

**TOML is the recommendation** ([OQ-RC2](#OQ-RC2)). It is the one format with all three: low noise,
no value misreadings, and familiar to the people who will read these files. Its weak spot, long
section headings for deep nesting, is exactly what [§4](#4-the-proposed-vocabulary)'s flatter shape
avoids. **JSONC in the new shape is a respectable second.** If the maintainer would rather not add
a format, nearly all of the gain is still there. YAML loses on the version-number misread, which
this config would meet on its first Python tool. KDL fails the cold read for a writer: easy to
read, but almost nobody has written it.

**Files yolo writes stay JSON**: the per-workspace switch files, lock files, the jail's inherited copy
and `yolo config dump --json`. A person reads those through a yolo command, and machine writing
gains nothing from TOML. Only files a person writes change.

## 6. Packs: the same rules

The maintainer: *"and my packs."* His twelve packs, copied verbatim on 2026-10-24 (`matt-craft`, a
git pack with no manifest, aside), have 1,688 manifest lines, 394 of them comments. `matt` is 860
lines and `meta-muse` 512. The other ten run from 9 to 74 lines. Seven of them are the same,
byte for byte, as the copies staged in this jail. By contribution kind, `matt` is:

| Kind | Entries | Lines | Of them comments |
| :--- | ---: | ---: | ---: |
| `config-overlay` | 7 | 425 | 81 |
| `files` | 39 | 271 | 22 |
| `config` | 1 | 57 | 21 |
| `autonomy` | 1 | 42 | 6 |
| the other five kinds | 8 | 59 | 5 |

**Most comments explain yolo, not the pack.** `agy-capture` gives 24 of its 74 lines to two
warnings. Its launch flags must restate `--dangerously-skip-permissions`, because a later pack's
flag list replaces an earlier one's whole (`launchFlagClaims` in `internal/packload/packload.go`
assigns, it does not append). Delete that flag, and agy prompts for every tool in a jail. Its
`statusLine` overlay restates the whole object, because overlays replace objects. `meta-muse`'s
comments spend paragraphs on which layer (`defaults` or `managed`) wins a render. When a
pack author has to explain the merge rules on every entry, the merge rules are the shape problem.

**Models are written twice, in two formats.** `meta-muse` writes six model rows straight into pi's
own `models.json` through a `pi/models` overlay. It repeats the same seven-level `thinkingLevelMap`
six times. Each `[200k]` row copies every fact of the row it shadows and adds a `wireId`, then
the ids appear again in a `pi/settings` overlay for compaction. yolo has `wire_id` and
`model_facts` for exactly that pair. The pack cannot use them, because yolo's model facts have no
thinking levels, image limits or wire API: those reach pi only through `base`, from a catalog
entry these models do not have (`internal/config/config.go`, the model-facts comment). So any
model pi's catalog lacks has to be described in pi's file format, once per agent that wants it.

Six levers cover most of it:

- **P1. A `home/` folder in a pack is laid into the home directory, file by file.** It replaces
  `files` entries that only copy a file or folder to the same path: 32 of `matt`'s 39 (the other 7
  fetch, patch or build), 6 in `matt-mods` and 1 in `agy-capture`. File by file
  means it merges with other packs' and the user's own files in a shared folder such as
  `~/.pi/agent/extensions`. Two packs claiming one file still refuse the launch, as today. A comment
  that explained an entry moves into the file, or into the pack's README. A `files` entry stays for
  what a folder cannot say: a `source` to fetch, `patches`, a `build`.
- **P2. An overlay's content lives in its own file, in the agent's own format.** `overlays/pi-settings.jsonc`
  holds what is now the `config` object of a `config-overlay` entry, and the manifest names the
  surface and, if any, the profile. The 425 lines move out of the manifest rather than shrinking.
  What shrinks is what a reader has to wade through to see what the pack does. The repeated tool
  lists across pi's subagent roles are pi's own settings shape, and no yolo format change removes
  them. A format with variables would, which is the one case for a programmable manifest
  ([`manifest-language.md`](manifest-language.md#OQ-M3)).
- **P3. Group by kind, and state the pack's identity once.** This is
  [`manifest-language.md`](manifest-language.md)'s model change, already argued there: `[[program]]`
  entries together, no repeated `"kind"` and no repeated `"agent": "claude"`.
- **P4. A contribution states only what it changes.** A later pack's launch flags add to the list,
  and an overlay merges into an object key by key. Replacing must be said in as many words
  (`replace = true`). `agy-capture` then writes `--model gemini-3.8-flash-medium` and nothing else,
  and losing the permission bypass by deleting a line stops being possible.
- **P5. A model is declared once, in yolo's model shape, for every agent.** yolo's model facts
  gain what `meta-muse` needs (thinking levels, image limits, wire API). Its six rows become two
  models with a `[200k]` twin each through `wire_id`, plus the shared facts once. Each agent's
  derive writes its own file from them. The `pi/models` overlay is then only for what no agent
  shape covers.
- **P6. The obvious is not written.** An install hint that is the same on every package manager
  is one value: `install = "jq"`, or `install = { all = "fd", apt = "fd-find", dnf = "fd-find" }`.
  `matt-fzf`'s three programs take 30 lines today for two exceptions. A `config` entry's `codec`
  follows from its path's extension, `state`'s `scope = "workspace"` is the default, and
  `run` defaults to `bin`. Each is still writable.

**This overturns a measured premise.** [`pack-conventions.md`](pack-conventions.md#appendix-a-counts)
counted 10 single-file `files` entries in `matt` on 2026-10-04 and concluded the folder conventions
save "about five lines in all". There are 39 such copy-only entries across his packs today. Its rejections of a one-value
`install_hints` and of a provider implying its profile were that document's own verdicts, not the
maintainer's rulings, so this doc re-opens both ([§4.3](#43-models-one-name-for-the-common-case)
re-opens the second). Its one-value rejection rested on a test that every shipped hint cite its
source per manager. P6 keeps that test for shipped packs, which write the per-manager form.

`matt`'s `board` program and two `files` entries, today and under P1 to P3 in TOML:

```jsonc
{"kind": "files", "from": "files/extensions/turn-bell.ts", "into": ".pi/agent/extensions/turn-bell.ts"},
{"kind": "files", "from": "files/extensions/versions.ts",  "into": ".pi/agent/extensions/versions.ts"},
{"kind": "program", "bin": "board", "via": "pack", "from": "programs/board", "run": "board"}
```

```toml
# the two extensions now sit at home/.pi/agent/extensions/ in the pack; no entry needed
[[program]]
bin = "board"
from = "programs/board"           # via = "pack" and run = bin are the defaults
```

`matt-fzf`, whole, today 56 lines, under P1 to P6:

```toml
name = "matt-fzf"
description = "Matt's fd|fzf custom file finder for Claude Code's @-file completion."
# bin/ is at home/.claude/bin/ in the pack

[[program]]
bin = "fd"
install = { all = "fd", apt = "fd-find", dnf = "fd-find" }

[[program]]
bin = "fzf"
install = "fzf"

[[program]]
bin = "jq"
install = "jq"

[[overlay]]
surface = "claude/settings"
set.fileSuggestion = { type = "command", command = "~/.claude/bin/file-suggestion.sh" }
```

## 7. Behavior in every case

- **Both `config.toml` and `config.jsonc` exist in one place.** yolo refuses, naming both and
  `yolo config migrate`, which keeps one. It never merges two formats of one file.
- **An old key, `brokered` say, in a new file.** `yolo check` and the launch refuse, naming the
  replacement line already respelled, as the `use_profiles` refusal does today. There are no aliases.
- **`yolo config migrate`** rewrites every file it can name (user, the conventional overrides, the
  project's two files, `include_if_found` files it can reach), shows the diff, writes on `y`, and
  keeps each original as `<name>.pre-migrate`. It never edits a file outside those, and it names
  each file it could not reach, such as a dotfiles-managed include. A workspace file stays an agent's
  to migrate: the command works there too.
- **A pack manifest in the old format.** Refused, naming `yolo pack migrate <path>`, which rewrites
  the manifest and moves single-file `files` entries into `home/`. A fetched pack is its author's
  to migrate: until it is, it is refused with the author named.
- **A yolo older than this change** ([OQ-RC6](#OQ-RC6)).
- **What done looks like.** The maintainer's config, migrated, reads correctly cold, line by line,
  against [§2](#2-where-todays-config-misleads)'s table, and is under 60 lines. This repository's is
  under 15. `matt`'s manifest holds no single-file `files` entry and no overlay body, `agy-capture` restates no
  flag it does not change, and `meta-muse` declares each model once. The approval
  prompt has no `*` and no `kind`.

## 8. What this doc takes over

These questions are answered by this doc's rulings, and their documents are updated when the
rulings land:

- [OQ-M1](manifest-language.md#OQ-M1) (the model change first) by [OQ-RC5](#OQ-RC5);
  [OQ-M2](manifest-language.md#OQ-M2) (which syntax) by [OQ-RC2](#OQ-RC2).
  [OQ-M3](manifest-language.md#OQ-M3) (a manifest as code) stays there.
- [OQ-PC1](pack-conventions.md#OQ-PC1) to [OQ-PC3](pack-conventions.md#OQ-PC3) by
  [OQ-RC5](#OQ-RC5)'s `home/` folder.
- [OQ-PP1](providers-and-profiles-redesign.md#OQ-PP1) (what `-p` names) by [OQ-RC3](#OQ-RC3).
- The broker grants' shape proposed in chat on 2026-10-24, by [§4.2](#42-what-a-jail-may-do-with-your-logins)
  and [OQ-RC4](#OQ-RC4). Whether your config may allow write kinds in every workspace is answered
  by `push = "when a project asks"`.

**One ruling this re-opens on purpose:** [PP-D10](providers-and-profiles-redesign.md#PP-D10) renamed
`use_profiles` to `profile` on 2026-09-29, so the config key would mirror `-p`. [L5](#41-the-levers-each-one-syntax-free)
renames it to `use`. The reason for that ruling still holds (the key mirrors the flag, form for
form); only the word changes, because `profile` beside `profiles` and `providers` is the confusion
the maintainer named.

## 9. Open questions

1. 💬 <a id="OQ-RC1"></a>**OQ-RC1: Which of the six shape changes go in?**

   A colleague copies the maintainer's config to get Bedrock. They find `bedrock` three times,
   `"claude": null`, and an `aws-auth` that appears in `packs` and again under `loopholes` with
   `enabled: true`. They delete the `loopholes` block as a duplicate, and their jail starts with no
   AWS credentials and no message they can connect to it. Each lever removes one such trap, and each
   can be taken alone:

   - **L1:** `yolo init` writes a short file, and `yolo check` flags a written default.
   - **L2:** a model's key is its id, and the maker follows from a Bedrock id.
   - **L3:** facts every model of a provider shares are written once, in `every_model`.
   - **L4:** a pack's settings are its switch, and a pack needing none is on when selected. This
     amends the loophole rules "presence never activates" and "absent default means off".
   - **L5:** the renames: `as_you`, `use`, `accepts`, `price`, `context`.
   - **L6:** `pack_folders`, so your own packs are listed by bare name.

   <!-- vantage: question id=OQ-RC1 leaning="All six. L4 is the only one that changes a ruled rule, and it keeps that rule's reason: the user still says so, in the settings." -->

   _Leaning:_ All six. L4 is the only one that changes a ruled rule, and it keeps that rule's
   reason: the service runs because the user said so, and the settings are where they said it.

   **Answer:**
   > _(empty — fill in when decided)_

2. 💬 <a id="OQ-RC2"></a>**OQ-RC2: TOML, or keep JSONC in the new shape?**

   The maintainer opens his config to add a model. In JSONC each model row is a quoted key, a
   brace, and quoted field names, and once any yolo command rewrites the file a formatter expands it
   back to about 133 lines. In TOML the same row is one line, and it stays one line. Either way the
   reshape takes the file from 178 lines to under 60.

   - **A — TOML** for every file a person writes: configs and pack manifests. One new vendored
     library. Files yolo writes stay JSON.
   - **B — JSONC, reshaped.** No new format and no new library. Hand-laid files read nearly as
     well. Anything a tool rewrites grows back.
   - **C — YAML.** The least punctuation, but `3.10` reads as 3.1 and indentation carries meaning.
     A mise version is exactly that kind of value.

   <!-- vantage: question id=OQ-RC2 leaning="A, TOML: low noise, no value misreads, already familiar from mise.toml and Cargo; the flatter shape avoids its one weakness, long headings." -->

   _Leaning:_ A, TOML. It has low noise, it misreads no values, and its readers already know it from
   `mise.toml` and Cargo. The flatter shape avoids its one weakness, long section headings.

   **Answer:**
   > _(empty — fill in when decided)_

3. 💬 <a id="OQ-RC3"></a>**OQ-RC3: Should you be able to pick a provider by its own name, without a profile?**

   The maintainer runs every agent on Bedrock, starting on Sol. To say that today he writes a
   provider `bedrock`, a profile `bedrock` (provider `bedrock`, model Sol), and `"*": "bedrock"`.
   Seven of the nine shipped profiles are named after the provider they pick. So a reader of `-p
   bedrock` cannot tell which of the two `bedrock`s they named, and that open question has been
   sitting in the providers redesign since 2026-09-25.

   - **A — Keep profiles as the only thing you select.** Explain the two nouns better in the docs.
     The three `bedrock`s stay.
   - **B — A provider is selectable by name and starts on its own `model`.** A profile exists only
     for a second setup of one provider (`bedrock-opus`, or `bedrock-bridge` with its `via`). The
     names share one list, and a clash is refused. Same-named shipped profiles are deleted.
   - **C — Only providers, with options on the flag** (`-p bedrock:model=opus`). No profiles
     at all. The flag gets longer, and the `via` profiles need a new home.

   <!-- vantage: question id=OQ-RC3 leaning="B: the common case is one name, and the rare case (two setups of one provider) keeps the profile that already works." -->

   _Leaning:_ B. The common case becomes one name, and the rare case, two setups of one provider,
   keeps the profile that already works.

   **Answer:**
   > _(empty — fill in when decided)_

4. 💬 🤷 <a id="OQ-RC4"></a>**OQ-RC4: What should the section for "what a jail may do with your logins" be called?**

   The maintainer reads `brokered.git.grants` and says *"I don't understand 'brokered'."* The
   section holds git fetch and push, GitHub reads, and the AWS login `aws-auth` serves: everything a
   jail does using a host credential it never sees. Its contents are
   [§4.2](#42-what-a-jail-may-do-with-your-logins)'s, and only the name is open:

   - **A — `as_you`**: `[as_you.git] push = ["main"]`. Short, and says whose identity is used.
   - **B — `your_logins`**: names the credential, not the act.
   - **C — `on_your_behalf`**: the plainest English, and the longest.
   - **D — `access`**: familiar, but says nothing about whose login.

   <!-- vantage: question id=OQ-RC4 leaning="A, as_you — reads as a sentence with what follows (as you: git push main), and it is short enough to type." -->

   _Leaning:_ A, `as_you`. It reads as a sentence with what follows ("as you: git, push main"), and
   it is short enough to type.

   **Answer:**
   > _(empty — fill in when decided)_

5. 💬 <a id="OQ-RC5"></a>**OQ-RC5: Should pack manifests change in the same breaking change?**

   The maintainer opens `matt/pack.json` to see what the pack does. It is 860 lines. 271 of them are
   39 `files` entries, 32 of them plain copies to the same path, and 425 are pi's settings written
   inline. In `agy-capture`, deleting one restated flag would make agy prompt for every tool, because
   a later pack's flags replace the earlier list whole. `meta-muse` writes the same model facts six
   times in pi's own format, because yolo's model shape cannot hold them.

   - **A — Yes, together.** All six pack levers (`home/`, overlay files, grouping by kind, merge
     by default, models in yolo's shape, one-value install hints), with `yolo pack migrate` moving
     the files. One migration for configs and packs. Any lever can be vetoed alone.
   - **B — Configs first, packs next.** Two migrations. The configs land sooner.
   - **C — Packs stay as they are.** The 860-line manifest stays.

   <!-- vantage: question id=OQ-RC5 leaning="A: one break, and the migrate command does the moving; packs are the bigger file and the same rules apply." -->

   _Leaning:_ A. It is one break, and the migrate command does the moving. Packs are the bigger
   files, and the same rules apply.

   **Answer:**
   > _(empty — fill in when decided)_

6. 💬 <a id="OQ-RC6"></a>**OQ-RC6: What happens on a machine still running an older yolo?**

   A colleague migrates their config with the new yolo, then runs a jail from an older yolo
   installed elsewhere on the machine, or the maintainer's Mac is a release behind. The older yolo
   looks for `config.jsonc`, finds none, and launches with no packs, saying only that the jail has
   no coding agent.

   - **A — Migrate keeps the old file** beside the new one, read-only and marked as superseded. An
     older yolo still works, and drifts. The new yolo refuses while both exist, unless the old one
     carries the marker.
   - **B — Migrate removes the old file.** An older yolo starts empty and says it has no packs.
     Simple, and plain to see.
   - **C — Ship a release that knows the new file first**, then the migration. An old-enough yolo
     refuses, naming the upgrade. One extra release.

   <!-- vantage: question id=OQ-RC6 leaning="C: one release that recognizes config.toml and refuses with 'upgrade yolo', then the migrating release; no silently empty jail, no drifting copy." -->

   _Leaning:_ C. One release recognizes `config.toml` and refuses with "upgrade yolo", then the
   migrating release follows. No jail starts silently empty, and no stale copy drifts.

   **Answer:**
   > _(empty — fill in when decided)_

## Decision Ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |

## Appendix A: the maintainer's config, whole

In the proposed shape, TOML, 53 lines. It replaces the 178 lines of
[§2](#2-where-todays-config-misleads). The repositories for `as_you.git` come from each
project's remotes, so none is listed here.

```toml
# ~/.config/yolo-jail/config.toml (your machine-wide settings)

pack_folders = ["~/.dotfiles/yolo-packs"]   # a bare name below is also looked for here
packs = [
  "pi", "codex", "claude", "copilot",
  "bedrock", "aws-auth", "git", "github",
  "guardrails", "chrome-devtools", "journal", "host-processes", "serial",
  "pi-fork", "matt", "matt-fzf", "matt-mods", "matt-agentwork",
  "git+https://github.com/mschulkind-oss/matt-craft?ref=main",
  "luna-worker", "claude-bedrock-global",
]

[install.mise]
neovim = "nightly"
"pipx:swarf" = "latest"

[mcp.tavily]
command = ["npx", "-y", "tavily-mcp@latest"]
env = { TAVILY_API_KEY = "${TAVILY_API_KEY}" }

# What jails may do with your own logins. Each project asks for what it needs.
[as_you.git]
fetch = true
push = "when a project asks"

[as_you.github]
read = true

[as_you.aws-auth]
profile = "hssandbox-BedrockInference"
unnarrowed = true

# Which models the agents run on.
[use]
everyone = "bedrock"
claude = "own login"

[providers.bedrock]
region = "us-east-1"
model = "global.openai.gpt-6.1-sol"

[providers.bedrock.every_model]            # shared by each model below unless it says otherwise
accepts = ["text", "image"]
max_tokens = 128_000
reasoning = true

[providers.bedrock.models]
"global.anthropic.claude-fable-5-1" = { name = "Fable 5.1", context = 1_000_000, price = { input = 10, output = 50, cache_read = 0.25, cache_write = 12.5 } }
"global.anthropic.claude-opus-5-5"  = { name = "Opus 5.5",  context = 1_000_000, price = { input = 4,  output = 20, cache_read = 0.2,  cache_write = 5 } }
"global.anthropic.claude-haiku-5-5" = { name = "Haiku 5.5", context = 1_000_000 }
"global.openai.gpt-6-astra"         = { name = "Astra 6",   context = 272_000,   price = { input = 10, output = 50, cache_read = 1,    cache_write = 12.5 } }
"global.openai.gpt-6-luna"          = { name = "Luna 6",    context = 272_000,   price = { input = 0.1, output = 0.5, cache_read = 0.01, cache_write = 0.125 } }
"global.openai.gpt-6.1-sol"         = { name = "Sol 6.1",   context = 272_000,   price = { input = 2,  output = 10, cache_read = 0.1,  cache_write = 2.5 } }
```

Not yet checked: whether pi's own model catalog already carries the Fable, Opus and Haiku facts. If
it does, those three rows shrink to their names, since a fact left out falls back to the catalog.

This repository's `yolo-jail.toml`, replacing 133 lines:

```toml
# This project's jail. Every key: `yolo config-ref`.
[install]
nix = ["just"]                    # runs this repo's Justfile recipes
mise = { lua = "5.4.9" }          # syntax-checks the packs' derive.lua files

[as_you.git]
push = ["main"]
```
