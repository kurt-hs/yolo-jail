---
title: "Config that reads right the first time: the shape is the problem, and JSONC is a smaller one"
date: 2026-10-24
status: in-review
stage: DESIGN
next: "Rule OQ-RC1 (which shape changes go in) and OQ-RC2 (which file format); OQ-RC3 to OQ-RC9 follow, and none of them needs code first"
tags: [config, packs, manifest, providers, profiles, broker, grants, format, toml, readability, design]
summary: "The maintainer's own config takes 178 lines to say about 20 things, and lines such as `grant read *`, `grant_kinds` and three `bedrock`s in a row are misread by the person who wrote them. Measured on that file, the shape (written defaults, repeated facts, mechanism-named keys, one switch spread over three keys) accounts for nearly all of the length: the same content in today's JSONC, reshaped, is about 58 lines, and in TOML about 53. The format changes how noisy each line looks, not how many there are or what they mean. So this doc proposes a vocabulary built on what the user is deciding (what to install, what models the agents use, what a jail may do with your logins), the same rules applied to pack manifests, and TOML as the recommended format, with JSONC kept as the fallback. It absorbs the open manifest-format questions and the provider-versus-profile question."
vantage:
  status-chip: true
---

# Config that reads right the first time: the shape is the problem, and JSONC is a smaller one

**Status:** 2026-10-24. Nothing is built. The measurements use the effective user config this jail
inherited at launch (`~/.config/yolo-jail/config.jsonc` inside the jail, generated
2026-10-25T00:30:59Z), this repository's [`yolo-jail.jsonc`](../../yolo-jail.jsonc), and the 21 packs
staged under `/ctx/packs/` at `758e4c759`, plus the maintainer's twelve packs copied verbatim on
2026-10-24. Working files are kept under the durable directory and are not cited here.

**Needs your ruling:** [OQ-RC1](#OQ-RC1) to [OQ-RC9](#OQ-RC9).

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
than the change does not read the new files ([OQ-RC6](#OQ-RC6)), and the rewrite meets a standing
ruling that yolo never edits the user config ([OQ-RC9](#OQ-RC9)).

**Start at:** [§2](#2-where-todays-config-misleads), the misreadings, then
[§5](#5-the-same-content-in-four-formats), the formats side by side. The compression exercise is
[§7](#7-measuring-the-repetition), the transition [§8](#8-moving-to-the-new-config), the further
problems [§9](#9-other-problems-this-could-solve), and more example configs
[Appendix B](#appendix-b-more-configs-today-and-proposed).

**Reads with:** [`manifest-language.md`](manifest-language.md) and
[`pack-conventions.md`](pack-conventions.md), whose open questions this doc takes over
([§10](#10-what-this-doc-takes-over)); [`providers-and-profiles-redesign.md`](providers-and-profiles-redesign.md),
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
own `models.json` through a `pi/models` overlay: three models and a `[200k]` twin of each, 199 lines
once expanded. It writes a seven-level `thinkingLevelMap` six times, in two variants that differ
only in `max`. Each `[200k]` row copies every fact of the row it shadows and adds a `wireId`, then
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
  [§6.1](#61-why-ten-files-take-fifty-lines) is why this is needed, and the list form it adds.
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
  gain what `meta-muse` needs (thinking levels, image limits, wire API). Its six rows become three
  models with a `[200k]` twin each through `wire_id`, plus the shared facts once
  ([Appendix B.8](#b8-meta-muses-models-under-p5)). Each agent's
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

### 6.1 Why ten files take fifty lines

The maintainer: *"putting 10 files somewhere requires like 35 lines each repeating every source and
dest path, rather than just a list of a few files."* Measured: `matt` copies 25 files into
`~/.pi/agent/extensions/` with 25 `files` entries, 127 lines. Each entry writes its file's path
twice: once under `files/extensions/`, once under `.pi/agent/extensions/`. Across `matt`'s 39
`files` entries, 1,169 bytes are an `into` whose last part repeats its `from`.

**The cause is a mechanism, not the format.** A `files` entry is a read-only mount over its whole
destination, and two entries claiming one path refuse the launch
([`pack-system.md`](../reference/pack-system.md), the `files` section). So a pack cannot hand over
the folder `~/.pi/agent/extensions/` when `matt-mods` adds `tok-stats.ts` to the same folder: each
file needs its own mount, and so its own entry. TOML alone takes the 127 lines to about 75. The
mechanism is what writes 25 entries.

**For pi, the fix has already shipped, and these packs predate it.**
[OQ-PR1](../reference/pack-system.md#oq-pr1), ruled 2026-10-05 in the maintainer's words *"we need
to be able to consolidate our pack files, they repeat way too much,"* lets a pack reach pi with one
entry, `{"kind": "files", "agents": ["pi"], "from": "files/pi"}`. pi loads that folder as a
package: every top-level file in its `extensions/`, `themes/` and `prompts/`, with no list
([pi packages](../reference/pack-system.md#pi-packages)). Moving `files/extensions` to
`files/pi/extensions` and `files/themes` to `files/pi/themes` turns 26 of `matt`'s entries into one,
today, with no yolo change. `yolo pack migrate` should make that move, and `pack lint` should name
a pack that copies files one by one into a folder pi loads as a package.

**For every other folder, P1 and a list form.** Files laid out under `home/` land file by file, so
two packs and the user can share a folder. Where the pack's own layout should not mirror the home
(a folder shared with other tools, or files picked from a larger tree), one entry takes a list:

```toml
[[files]]
from = "files/extensions"
into = ".pi/agent/extensions"
only = ["turn-bell.ts", "versions.ts", "board", "work-control"]   # or omit `only` for every file
```

Each listed name is mounted on its own, so the file-by-file merge and the two-claims refusal both
still hold. Measured on the 25 extensions, the list form is 30 lines against 127.

## 7. Measuring the repetition

The maintainer: *"We went through this exercise once with the metaphor of checking how well the
config files will compress as a proxy for how repetitive they are. We should probably go through
that full exercise again with my packs."* The first run is
[`manifest-language.md` §1](manifest-language.md) (2026-09-20, re-measured 2026-09-22). This is the
second, on 2026-10-24, over the twelve packs and the user config.

**Method.** Each file has its comments removed and is minified (`json.dumps` with no spaces), so
indentation and comments do not count. That leaves gzip −9 and xz −9e over the result. **Ratio**
is compressed bytes over minified bytes: lower means more of the file was predictable from the rest of
it. The **prose baseline** is the first N bytes of
[`what-yolo-is.md`](../reference/what-yolo-is.md), cut to each file's minified size, because a
small file compresses worse than a large one whatever it holds. Working files are kept under the
durable directory.

| File | Raw bytes | Without comments | Minified | gzip bytes | gzip ratio | xz ratio | Prose, same size |
| :--- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `matt` | 34,402 | 24,708 | 15,264 | 3,669 | **0.24** | 0.24 | 0.40 |
| `meta-muse` | 28,135 | 10,508 | 4,513 | 1,149 | **0.25** | 0.27 | 0.43 |
| `agy-capture` | 3,011 | 1,414 | 801 | 421 | 0.53 | 0.64 | 0.60 |
| `matt-mods` | 1,649 | 1,649 | 1,224 | 455 | **0.37** | 0.43 | 0.56 |
| `matt-fzf` | 1,102 | 1,102 | 667 | 314 | 0.47 | 0.58 | 0.61 |
| `luna-worker` | 677 | 677 | 464 | 301 | 0.65 | 0.82 | 0.66 |
| `coordinator-reads-blocked` | 688 | 688 | 574 | 341 | 0.59 | 0.77 | 0.64 |
| `claude-bedrock-global` | 653 | 653 | 519 | 349 | 0.67 | 0.82 | 0.65 |
| `pi-fork` | 608 | 608 | 435 | 280 | 0.64 | 0.86 | 0.66 |
| `matt-agentwork` | 416 | 416 | 290 | 193 | 0.67 | 0.91 | 0.70 |
| `workspace-questions` | 366 | 366 | 331 | 226 | 0.68 | 0.89 | 0.69 |
| `deepseek-flash-only` | 254 | 254 | 203 | 151 | 0.74 | 1.06 | 0.74 |
| shipped `packs/claude` | 10,393 | 8,352 | 6,110 | 1,649 | 0.27 | 0.28 | 0.41 |
| shipped `packs/pi` | 14,531 | 9,281 | 5,582 | 1,903 | 0.34 | 0.35 | 0.41 |

Sections of `matt` and `meta-muse`, measured the same way:

| Section | Minified | gzip bytes | gzip ratio |
| :--- | ---: | ---: | ---: |
| `matt`'s 25 extension copies | 2,794 | 419 | **0.15** |
| `meta-muse`'s `pi/models` overlay (six rows) | 2,608 | 473 | **0.18** |
| `matt`'s seven overlay bodies | 7,089 | 1,864 | 0.26 |

**What it says.**

- **The small packs are not repetitive.** Seven of them compress within 0.05 of prose of the
  same size. Their trouble is the per-line noise of [§5](#5-the-same-content-in-four-formats), not
  repetition. `matt-fzf` sits 0.14 under prose, and that is its five-manager install hints, the
  P6 case.
- **The repetition is where the eye says it is.** `matt`, `meta-muse` and `matt-mods` sit 0.16 to
  0.19 below prose. Inside them, the extension copies (0.15) and the six model rows (0.18) are the
  most repetitive text measured in either exercise, lower than any whole manifest. Those are
  [§6.1](#61-why-ten-files-take-fifty-lines) and P5.
- **Comments are 28% of `matt`'s bytes and 63% of `meta-muse`'s.** Their size is the
  "comments explain yolo" finding of [§6](#6-packs-the-same-rules), measured.

**Before and after.** Lines are without comments or blank lines. Bytes are minified for JSON and
without comments for TOML:

| The same content | Lines | Bytes | gzip bytes | gzip ratio |
| :--- | ---: | ---: | ---: | ---: |
| user config, today (JSON) | 170 | 2,447 | 769 | 0.31 |
| user config, proposed (TOML, [Appendix A](#appendix-a-the-maintainers-config-whole)) | 40 | 1,818 | 781 | 0.43 |
| `matt-fzf`, today | 56 | 667 | 314 | 0.47 |
| `matt-fzf`, proposed (TOML, [§6](#6-packs-the-same-rules)) | 14 | 391 | 227 | 0.58 |
| `matt`'s 25 extension copies, today | 127 | 2,794 | 419 | 0.15 |
| the same as one list entry ([§6.1](#61-why-ten-files-take-fifty-lines)) | 30 | 637 | 298 | 0.47 |
| `matt` whole, today | — | 15,264 | 3,666 | 0.24 |
| `matt` without its plain `files` entries (P1) | — | 11,845 | 3,216 | 0.27 |
| …and with its overlay bodies moved to files (P2) | — | 5,315 | 1,598 | 0.30 |

**Why the ratio cannot be the target, again.** The proposed user config is a quarter of the lines,
and gzip gives it *more* bytes than today's (781 against 769). The ratio rose from 0.31 to 0.43
because the repeats gzip was already discarding are gone. What remains is the information, and the
shape change kept all of it. That is
[`manifest-language.md`](manifest-language.md)'s finding: the compressed size measures what a file
says, and the reshape is meant to keep what it says while dropping how often it says it. So a
manifest that improves *rises* toward the prose line. Hitting a ratio number would push in exactly the
wrong direction, by minifying or by removing information.

**So the exercise is a detector, and three numbers are the scorecard:**

1. **Gzip ratio below the prose line, by section.** It finds where to look: anything 0.10 or more
   under prose of its size is repeating itself. `pack lint` could report it per contribution.
2. **Lines a person reads**, without comments or blanks. This is the cost the maintainer named.
3. **Facts written more than once:** a path repeated in `from` and `into`, a model fact on every row,
   a flag restated because lists replace. Each is a place two copies can disagree, which is the
   [§1](#1-the-test-every-line-must-pass) rule 2 failure. They are counted by hand today. The
   compressed size (the numerator, not the ratio) should stay flat while the other two fall, which
   is the proof that a rewrite lost nothing.

## 8. Moving to the new config

### 8.1 The order of releases

[OQ-RC6](#OQ-RC6)'s leaning, made concrete:

1. **Release N recognizes the new files and refuses them.** A `config.toml`, `yolo-jail.toml` or
   `pack.toml` found by a yolo that does not read them stops the launch with "this file is for a
   newer yolo: run `yolo update` (or `just install`)". It changes nothing else, so it is safe to
   ship long before the rest.
2. **Release N+1 reads only the new format** and refuses an old file, naming `yolo config migrate`.
   A launch that finds only old files offers the migration (`y/N`), shows the diff, and on `y`
   writes and continues. That follows the [happy-path principle](../reference/happy-path-principle.md):
   the stop does the next step when asked.
3. **Every machine that shares dotfiles upgrades before any of them migrates.** The maintainer's
   user config is synced across work and personal machines through `~/.dotfiles`, so one machine's
   migration rewrites the file every other machine reads. Release N is what makes the order safe:
   an older machine refuses the new file by name, rather than starting with no packs.

### 8.2 What `yolo config migrate` does

- **It proves it lost nothing.** It computes the effective config before (`yolo config dump`, the
  canonical JSON) and from the files it would write, and refuses to write unless the two are equal.
  Any difference is printed, key by key.
- **It keeps comments.** The packs have 394 comment lines, and the configs have more. A comment moves
  with the key it sits above. One whose key is gone (a default no longer written, a fact folded into
  `every_model`) is kept in place as a comment with `# (was on: <old key>)`, never dropped silently.
  JSONC→TOML needs a parser that keeps comments attached to keys. That library choice belongs in the
  implementation plan.
- **It writes nothing it cannot name.** It rewrites the user config, the conventional
  `overrides` file, the project's two files and every `include_if_found` file it can reach. A file
  reached through a symlink is written through the link, so the dotfiles copy changes and the
  diff shows the real path. A file it cannot reach is listed with the command to run where it can.
  Each original is kept as `<name>.pre-migrate`.
- **It drops written defaults** (L1) and **folds repeated facts** (L2, L3) only when the equality
  check above still holds. Each fold appears in the diff as its own hunk, so the user sees why a
  line went.

### 8.3 Behavior in every case

- **Both `config.toml` and `config.jsonc` exist in one place.** yolo refuses, naming both and
  `yolo config migrate`, which keeps one. It never merges two formats of one file.
- **An old key, `brokered` say, in a new file.** `yolo check` and the launch refuse, naming the
  replacement line already respelled, as the `use_profiles` refusal does today. There are no aliases.
- **A workspace file** is an agent's to edit, so `yolo config migrate` works there too, and an
  agent in the jail may run it on the project's files. A jail sees only a generated copy of the
  user config, never the file itself, so the user config is migrated on the host.
- **Shipped packs** are migrated in the same commit that changes the reader, so no released yolo
  ever ships a pack it refuses.
- **A local pack in the old format** (a path in `packs`, or a folder `pack_folders` names) is
  refused, naming `yolo pack migrate <path>`. That command rewrites the manifest, keeps comments
  as `yolo config migrate` does, and moves plain-copy `files` entries into `home/` or a pi package
  folder ([§6.1](#61-why-ten-files-take-fifty-lines)). It moves files with `git mv` when the pack is a
  git checkout, so history follows.
- **A fetched pack in the old format** (`git+https://…?ref=main`): [OQ-RC8](#OQ-RC8).
- **A yolo older than this change** ([OQ-RC6](#OQ-RC6), [§8.1](#81-the-order-of-releases)).
- **Files yolo writes** stay JSON ([§5](#5-the-same-content-in-four-formats)), and so does the
  internal canonical form: the boot baseline `config drift` compares against, `config dump --json`
  and the approval record. Only their vocabulary changes, so a drift report and an approval prompt
  say `as_you.git.push`, never `brokered.git.grants`. The jail's inherited copy of the user config
  becomes TOML, so what an agent in the jail reads matches what the user wrote.
- **An error in a new file** names `file:line:col` and the key, as JSONC errors do today. The TOML
  library must report key positions; that is a selection criterion for the plan, not a design
  choice.
- **The repository's own references.** Over 400 Go files under `internal/`, `integration/` and
  `cmd/` name `yolo-jail.jsonc` or `config.jsonc`, and 51 user-guide and reference files mention
  JSONC. Test fixtures that write config should go through one helper that writes the new format,
  so the next format change touches one place. `yolo config-ref` is regenerated in the new
  vocabulary ([§9](#9-other-problems-this-could-solve)).
- **What done looks like.** The maintainer's config, migrated, reads correctly cold, line by line,
  against [§2](#2-where-todays-config-misleads)'s table, and is under 60 lines. This repository's is
  under 15. `matt`'s manifest holds no single-file `files` entry and no overlay body, `agy-capture` restates no
  flag it does not change, and `meta-muse` declares each model once. The approval
  prompt has no `*` and no `kind`.

## 9. Other problems this could solve

The maintainer asked for the issues this will meet and the ones it could fix while it is open.
Each item below is either covered by a question or offered as an idea. An idea needs no ruling
unless someone wants it in.

**Problems the change will meet.**

- **TOML has no `null`, and today's config uses `null` for four different things.** It means "keep
  your own login" (`"claude": null`), "remove an inherited MCP server", "unset an environment
  variable" (`env_sources`) and, in includes, "disable what a lower layer set". `yolo config-ref`
  mentions `null` on 27 lines. Each of those needs a word: [OQ-RC7](#OQ-RC7).
- **String mini-languages fail the cold read.** Several values are small languages packed into
  strings, and each has a trap:
  - `mounts`: `"~/x:/ctx/x:ro"` is one host path literally named `~/x:/ctx/x:ro`. It does not
    exist, so the mount is skipped with only a warning (config-ref, `mounts`).
  - `ports` is `"HOST:JAIL"`, and `forward_host_ports` beside it is `JAIL:HOST`. The config-ref
    example has to say "Note the reversed order."
  - A git pack is `git+https://…?ref=main`.

  The proposal: each becomes a table whose keys say the direction, such as
  `mount = { from = "~/lib", at = "/ctx/lib" }`, `ports = [{ host = 8000, jail = 8000 }]` and
  `forward = [{ jail = 5432, host = 5432 }]`, and `{ git = "https://…", ref = "main" }`. A bare
  string stays legal only where it has one reading: a path with no colon, or a shipped pack name.
  This is L5 applied to values, and it belongs in [OQ-RC1](#OQ-RC1)'s L5 if ruled in.
- **Mixed-type lists.** `env_sources` mixes file paths and inline maps, and `mounts` mixes strings and
  objects. TOML arrays can hold both, but a reader cannot tell what an entry does from its type.
  Splitting each into named keys (`env.files = [...]`, `env.set = {...}`) says it. This is part of
  the same L5.
- **`${VAR}` looks like interpolation, and yolo does none.** `TAVILY_API_KEY = "${TAVILY_API_KEY}"`
  reaches the agent's MCP config literally, for the agent to resolve (config-ref, `mcp_servers`). A
  cold reader assumes yolo expands it. Either yolo expands it at render time from the jail's
  environment, or `yolo check` notes the literal pass-through on that line. Idea; the first changes
  what reaches the agent, so it would need its own question.
- **`yolo config` already means something else.** Today it is the agent-config composition
  pipeline (`ls`, `render`, `diff`), not yolo's own config. Adding `yolo config migrate` and
  `yolo config why` to it puts two meanings under one verb. Either the composition commands move
  (`yolo surfaces …`) or the new ones go elsewhere. The plan picks the spelling; the help text
  must not mix them.
- **"yolo never edits the user config" is a standing ruling.** [OQ-BB12](boundary-broker.md#15-decision-ledger)
  (2026-10-01): *"I don't want to do anything that edits the user config directly."*
  `yolo config migrate` rewrites it. Whether a one-time rewrite the user asks for, with a diff and a
  backup, falls inside that ruling is the maintainer's to say: [OQ-RC9](#OQ-RC9).
- **Fetched packs in the old format**: [OQ-RC8](#OQ-RC8).
- **Seven layers, and no way to see which one set a line.** These are the user config, its includes,
  the per-workspace file yolo writes, the project config, `yolo-jail.local`, the selected packs'
  contributions, and the profile. A reader looking at one file cannot tell what the others changed.
  Idea: `yolo config why <key>` prints each layer that set the key, in order, with
  `file:line`. `yolo config show` prints the effective config in the new format with a
  `# from: <file>:<line>` comment per section.

**Opportunities.**

- **Scope in the section name.** A key that only user scope may set is refused in a project file
  today, but nothing in the file says so. The section table in [§4](#4-the-proposed-vocabulary)
  states scope per section. `yolo check` can name the scope in its refusal, and `yolo init`'s comment
  can say "these sections only work in your user config".
- **A published JSON Schema.** Taplo (the TOML language server) and VS Code's JSON support read a
  JSON Schema, giving completion, hover text and red squiggles in the editor. Generated from the same
  Go declarations `yolo check` validates against, it cannot drift. config-ref's 3,201 lines could
  then be generated per section from the schema's descriptions, rather than written by hand.
- **"Did you mean" for unknown keys.** A typo (`as-you`, `provider`) gets the nearest valid key.
  An old key gets its respelling ([§8.3](#83-behavior-in-every-case)).
- **Machine tags without include files.** The maintainer's work-only packs reach a machine through
  `include_if_found` of a dotfiles file. A conditional section would say it in the file a reader
  opens: `[when.tag.work] packs = ["luna-worker", "claude-bedrock-global"]`, with the tag set per
  machine. Idea, not a question: includes keep working, and this only matters if the include
  indirection is what confuses.
- **`yolo init` writes the minimum.** L1's short file, with the one comment pointing at
  `yolo config-ref` and the schema.

## 10. What this doc takes over

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

## 11. Open questions

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

7. 💬 <a id="OQ-RC7"></a>**OQ-RC7: How does a TOML file say "remove" or "not this one", with no `null`?**

   The maintainer's user config adds the `tavily` MCP server. In one project they don't want it, so
   today that project's config writes `"mcp_servers": {"tavily": null}`. The same `null` keeps
   claude on its own login (`"claude": null`), unsets `AWS_PROFILE` in `env_sources`, and disables a
   lower layer's value in an include. TOML has no `null`, so each of these needs a spelling, and a
   cold reader has to understand it.

   - **A — A word where each case lives.** `off = true` on anything that can be switched off
     (`[mcp.tavily] off = true`, the same word L4 gives a pack), `unset = ["AWS_PROFILE"]` under
     `env`, and `"own login"` in `use`. Each spelling says what it does, and there are three of them
     to learn.
   - **B — One reserved string, `"off"`,** valid wherever a value may be removed:
     `mcp.tavily = "off"`, `env.AWS_PROFILE = "off"`. One rule, but the string `"off"` can then never be a
     real value, and `"off"` for "keep your own login" reads wrong.
   - **C — One top-level list of removals:** `remove = ["mcp.tavily", "env.AWS_PROFILE"]`. Every
     removal is in one place, far from the thing it removes.

   <!-- vantage: question id=OQ-RC7 leaning="A: a word at the place, each saying what it does; off = true matches L4, and the cases are few." -->

   _Leaning:_ A. The word sits where the thing is and says what it does. `off = true` is already
   L4's word, and there are only three cases.

   **Answer:**
   > _(empty — fill in when decided)_

8. 💬 <a id="OQ-RC8"></a>**OQ-RC8: What happens to a pack fetched from someone else's repository that still has an old `pack.json`?**

   A colleague publishes a pack on GitHub, and the maintainer lists it as `git+https://…?ref=main`.
   The maintainer upgrades yolo to the release that reads only `pack.toml`. The colleague has not
   migrated. Today the one fetched pack in the maintainer's list, `matt-craft`, has no manifest at
   all, so it is not affected. A colleague's pack would be.

   - **A — yolo keeps an old-manifest reader for fetched packs only,** converting in memory. Nothing
     breaks, and yolo carries two readers, the second path this project prefers to delete.
   - **B — Refuse, naming the pack's repository and the two ways forward:** ask its author to run
     `yolo pack migrate`, or fork it and run it yourself. One reader, and a fetched pack breaks
     until someone migrates it.
   - **C — B, after a grace period of one release in which A applies with a warning.**

   <!-- vantage: question id=OQ-RC8 leaning="B: one reader; no fetched pack in use today has a manifest, and the refusal names both ways forward." -->

   _Leaning:_ B. One reader. No fetched pack in use today has a manifest to break, and the
   refusal names both ways forward.

   **Answer:**
   > _(empty — fill in when decided)_

9. 💬 <a id="OQ-RC9"></a>**OQ-RC9: May `yolo config migrate` rewrite your user config, given the ruling that yolo never edits it?**

   On 2026-10-01 the maintainer ruled that the per-project switch goes in a file beside the user
   config: *"I don't want to do anything that edits the user config directly … because it always
   leaves that a mess"* ([OQ-BB12](boundary-broker.md#15-decision-ledger)). A colleague now runs
   `yolo config migrate`. It wants to replace their hand-written `config.jsonc` with a `config.toml`.
   The ruling was about yolo editing the file as a side effect of a command. This is a rewrite the
   user asked for, with a diff first and a backup after, but that reading is this doc's, not the
   maintainer's.

   - **A — Yes, on request only.** The command writes `config.toml`, renames the old file to
     `config.jsonc.pre-migrate`, and shows the diff before either. A launch offers the command
     (`y/N`) and never runs it unasked.
   - **B — It writes the new file beside the old one and touches nothing else.** The new yolo
     refuses while both exist and prints the one `mv` that finishes the job. The ruling holds
     literally, at the cost of one command the user types.
   - **C — It prints the new file and writes nothing.** The user saves it by hand.

   <!-- vantage: question id=OQ-RC9 leaning="A: the ruling's complaint was a command's side effect leaving a mess; an asked-for whole-file rewrite with a diff and a backup is not that, but it is the maintainer's to say." -->

   _Leaning:_ A. The ruling's complaint was a command's side effect leaving a mess. A whole-file
   rewrite the user asks for, shown first and backed up, is not that. But it is the maintainer's
   ruling to read, not this doc's.

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

## Appendix B: more configs, today and proposed

Each example is in the proposed shape, in TOML. Where a today's version exists in a form yolo
reads, it is shown first. Key names follow [§4](#4-the-proposed-vocabulary) and are as open as
the questions that decide them.

### B.1 A colleague on a Claude subscription, nothing else

Today, `~/.config/yolo-jail/config.jsonc`:

```jsonc
{"packs": ["claude"]}
```

Proposed, `~/.config/yolo-jail/config.toml`:

```toml
packs = ["claude"]
```

The shape already works for the smallest config. What the colleague meets next is the 120-line
template in their first project, which L1 removes.

### B.2 A work machine: pi on Bedrock through AWS SSO, claude on its own login

Today:

```jsonc
{
  "packs": ["claude", "pi", "bedrock", "aws-auth"],
  "loopholes": {
    "aws-auth": {"enabled": true, "settings": {"profile": "my-sso-profile"}}
  },
  "profile": {"*": "bedrock", "claude": null}
}
```

Proposed:

```toml
packs = ["claude", "pi", "bedrock", "aws-auth"]

[as_you.aws-auth]                 # serves your AWS SSO login to the jail; writing this turns it on
profile = "my-sso-profile"

[use]
everyone = "bedrock"
claude = "own login"
```

The double switch is gone (L4), and `"own login"` replaces `null` ([OQ-RC7](#OQ-RC7)).

### B.3 pi on OpenRouter and Z.ai at once, keys from a file

Today:

```jsonc
{
  "packs": ["pi", "openrouter", "zai"],
  "profile": {"pi": ["zai", "openrouter"]},
  "env_sources": ["~/.config/yolo-jail/secrets.env"]
}
```

Proposed:

```toml
packs = ["pi", "openrouter", "zai"]

[use]
pi = ["zai", "openrouter"]        # both available in pi, switchable there

[env]
files = ["~/.config/yolo-jail/secrets.env"]   # the API keys these providers read
```

`env_sources`' mix of paths and maps becomes `env.files` and `env.set`
([§9](#9-other-problems-this-could-solve)).

### B.4 A team project

Today, `yolo-jail.jsonc` in the repository:

```jsonc
{
  "packages": ["postgresql", "just"],
  "mise_tools": {"node": "22", "python": "3.12"},
  "mounts": ["~/code/acme-shared"],
  "network": {"ports": ["8000:8000"], "forward_host_ports": [5432]},
  "gpu": {"enabled": true},
  "resources": {"memory": "16g", "cpus": 8},
  "brokered": {"github": {"repos": ["acme/shared"]}}
}
```

Proposed, `yolo-jail.toml`:

```toml
[install]
nix = ["postgresql", "just"]
mise = { node = "22", python = "3.12" }

[[mount]]                          # read-only unless `write = true`, which only your user config may say
from = "~/code/acme-shared"

[machine]
gpu = true                         # warns and starts without one on a machine that has none
resources = { memory = "16g", cpus = 8 }
ports = [{ host = 8000, jail = 8000 }]       # the host reaches the jail's :8000
forward = [{ jail = 5432, host = 5432 }]     # the jail reaches the host's postgres at localhost:5432

[as_you.github]
repos = ["acme/shared"]            # beyond this repository's own remotes

[as_you.git]
push = "new branches"              # a new branch, never main; each is approved at launch
```

`python = "3.12"` stays the string it is. The directions are keys, not positions in a string.

### B.5 The project's uncommitted local file

`yolo-jail.local.toml`, auto-merged as `yolo-jail.local.jsonc` is today:

```toml
# This machine only; not committed.
[mcp.postgres]
command = ["npx", "-y", "@modelcontextprotocol/server-postgres", "postgresql://localhost/acme"]

[mcp.tavily]
off = true                         # the user config adds it; not wanted here (OQ-RC7, option A)

[machine]
resources = { memory = "32g" }
```

### B.6 Work and personal machines from one dotfiles repository

Today the maintainer's user config includes a file that exists only on work machines:

```jsonc
{"include_if_found": ["~/.dotfiles/tag-work/config/yolo-jail/work.jsonc"]}
```

That keeps working, as `include_if_found = [...]`. The idea in
[§9](#9-other-problems-this-could-solve) puts the difference in the file a reader opens instead:

```toml
[when.tag.work]                    # this machine's tags are set once, per machine
packs = ["luna-worker", "claude-bedrock-global"]
```

### B.7 A small personal pack: `luna-worker`

Today, 23 lines. Its job is one setting:

```toml
name = "luna-worker"
description = "Work machines: pi's worker subagent runs on GPT-6 Luna when the session is on Bedrock."

[[overlay]]
surface = "pi/settings"
set.managed.subagents.agentOverridesByProvider.amazon-bedrock.worker.model = "amazon-bedrock/global.openai.gpt-6-luna"
```

A dotted key in TOML says the whole path on one line, so the nesting stops costing a line per level.
`coordinator-reads-blocked` is the same shape: one `set.managed.workControl.allowReads = false`.

### B.8 `meta-muse`'s models under P5

Today, 199 lines of pi's `models.json` format plus three compaction rows in `pi/settings`.
Proposed, with the facts yolo's model shape does not have today marked as new:

```toml
[providers.meta.every_model]
api = "openai-responses"           # new fact: the wire API
reasoning = true
accepts = ["text", "image"]
thinking = ["minimal", "low", "medium", "high", "xhigh", "max"]   # new fact: the levels it takes
max_tokens = 128_000
price = { input = 0, output = 0, cache_read = 0, cache_write = 0 }

[providers.meta.models."muse-spark-1.3-contributor"]
name = "Spark 1.3"
context = 1_048_576
max_tokens = 131_072
thinking = ["minimal", "low", "medium", "high", "xhigh"]
images = { max_width = 2000, max_height = 2000, max_bytes = 4_718_592, jpeg_quality = 80 }   # new fact
price = { input = 0.1, output = 0.2, cache_read = 0.002, cache_write = 0 }

[providers.meta.models]
"muse-wonder-1.0-contributor"      = { name = "Wonder 1.0",      context = 1_007_997 }
"muse-wonder-full-1.0-contributor" = { name = "Wonder Full 1.0", context = 1_007_997 }

# The [200k] twins: the same model on the wire, compacted at 200k.
"muse-spark-1.3-contributor[200k]"       = { twin_of = "muse-spark-1.3-contributor",       compact_at = 200_000 }
"muse-wonder-1.0-contributor[200k]"      = { twin_of = "muse-wonder-1.0-contributor",      compact_at = 200_000 }
"muse-wonder-full-1.0-contributor[200k]" = { twin_of = "muse-wonder-full-1.0-contributor", compact_at = 200_000 }
```

`twin_of` and `compact_at` are new too. `twin_of` takes every fact of the named row and sends its
id as the twin's `wire_id`. Today's `extends` is close, but it names a top-level `model_facts`
entry rather than a sibling row, and `wire_id` must be written. 21 lines replace 199 plus the three
compaction rows. Each agent's render writes its own file from these rows, so pi gets its six
`models.json` rows and the compaction overrides, and no pack writes either by hand.
