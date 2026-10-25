---
title: "Config that reads right the first time, and that yolo can edit without making a mess"
date: 2026-10-24
status: in-review
stage: DESIGN
next: "Rule OQ-RC9 (may yolo write your file, and how), OQ-RC11 (file name or version field), OQ-RC13 (conventions), OQ-RC14 (the two profile names) and OQ-RC15 (how your packs are named); the rest follow, and none needs code first"
tags: [config, packs, manifest, providers, profiles, broker, grants, format, toml, migration, editing, readability, design]
summary: "The maintainer's config takes 178 lines to say about 20 things, and lines such as `grant read *` are misread by the person who wrote them. Measured on that file, the shape accounts for nearly all of the length: the same content reshaped is about 58 lines in JSONC and 53 in TOML. So the vocabulary is the first proposal, TOML the second. The third and fourth are what the maintainer asked for on 2026-10-24: a transition that converts every file automatically, and commands that edit a config file mechanically, in place, without touching a byte they were not asked to. The file name is the version marker, a migrate command rewrites every file it can name and proves it lost nothing, and about sixty places where yolo today says 'add this to your config' print one command instead."
vantage:
  status-chip: true
---

# Config that reads right the first time, and that yolo can edit without making a mess

**Status:** 2026-10-24. Nothing is built. Evidence verified at `3aba3dad3` against this
repository, the effective user config this jail inherited at launch (generated
2026-10-25T00:30:59Z), the 21 shipped packs, and the maintainer's twelve packs under
`scratch/yolo-packs/`. The host files themselves (`config.jsonc`, `overrides.jsonc`,
`work.jsonc`, `personal.jsonc`) are not visible from the jail; their contents are known only
through the generated copy.

**Needs your ruling:** [OQ-RC1](#OQ-RC1) to [OQ-RC16](#OQ-RC16).

> **In short.** A config a stranger reads correctly comes from its shape, not its file format,
> and a config yolo can edit without a mess comes from touching only the bytes of one value.
> Neither needs a custom format. Both need the file name to say which yolo wrote it.

**Why it matters.** The maintainer, 2026-10-24: *"I want configs that can be read, without much
foreknowledge, and be understood the right way."* The same day he misread two lines of his own
config, and asked for two more things: *"It would be wonderful to be able to convert people's
configuration files automatically,"* and *"we have a bunch of places where we ask agents to edit
config files where it would be nice if we could have some mechanical commands."* About sixty such
places exist ([§8.1](#81-sixty-places-that-say-edit-your-config)).

**The shape.** Sections named for what the user decides ([§4](#4-the-proposed-vocabulary)) and
conventions that make the common line unnecessary ([§4.5](#45-lines-that-need-not-exist)); a
file per writer, in that writer's format ([§8](#8-who-writes-the-file)); a new file name per
format generation, and one migrate command that rewrites every file it can name
([§9](#9-moving-to-the-new-config)).

**Cost.** Every config file and pack manifest is rewritten once, with no aliases. One ruling is
reversed on purpose and two are amended ([§12](#12-what-this-doc-takes-over)).

**Start at** [§8](#8-who-writes-the-file) for the editing design and [§9](#9-moving-to-the-new-config)
for the transition; they are new. [§2](#2-where-todays-config-misleads) is the misreadings and
[§5](#5-the-same-content-in-four-formats) the formats side by side. The worked examples are
[Appendix A](#appendix-a-the-maintainers-config-whole) (the config, whole),
[Appendix B](#appendix-b-more-configs-today-and-proposed) (eight more configs) and
[Appendix C](#appendix-c-sessions) (nine command sessions).

**Reads with:** [`readable-config-plan.md`](readable-config-plan.md) (the implementation plan's
seed; incomplete while any question here is open);
[`manifest-language.md`](manifest-language.md) and [`pack-conventions.md`](pack-conventions.md),
whose open questions this doc takes over; [`providers-and-profiles-redesign.md`](providers-and-profiles-redesign.md)
([OQ-PP1](providers-and-profiles-redesign.md#OQ-PP1) is answered by [OQ-RC3](#OQ-RC3));
[`boundary-broker.md`](boundary-broker.md), where today's grants and the "never edit the user
config" ruling live.

---

## Defined terms

- **Cold read** (coined here): someone who has never opened `yolo config-ref` reads a line and
  says what it does. A line **passes** when what they say is right.
- **Shape**: which keys exist, how they nest, which values are written and which follow from
  others. Independent of the file format.
- **Format**: the syntax the shape is written in: JSONC (today's), TOML, YAML or KDL.
- **Splice** (coined here): an edit that parses a file only to find the byte range of one value,
  rewrites that range, and leaves every other byte as it was. It is not a round trip through a
  parser and printer, which is what reformats a file.
- **Companion file** (coined here): a file yolo owns, beside a file you own, in the same scope,
  composed with it. The per-workspace switch file under `~/.config/yolo-jail/workspaces/` is one
  today.
- **Provider** and **profile**: a provider is a place models are served from, with its address,
  region and model list; a profile is today a named choice of one provider plus options such as
  the starting model, and is what `-p <name>` names.

## 1. The test every line must pass

A config line is right when a cold read of it is right. Four rules follow, and every proposal
below is checked against them:

1. **Name the decision, not the mechanism.** `push = ["main"]` passes. `{"kind": "branch",
   "branch": "main"}` under `brokered.git.grants` fails, because `brokered` and `kind` name how
   yolo implements the permission, not what the user chose.
2. **Say each fact once.** A model's id as its key and again as `id`; `vendor: anthropic` beside an
   id that already says `anthropic`; the same three facts on each of six models. Every repeat is a
   place two copies can disagree.
3. **Write nothing that is the default.** This repository's `"network": {"mode": "bridge"}` is
   the default, restated.
4. **One switch, one place.** Turning `aws-auth` on today takes the pack in `packs`,
   `loopholes.aws-auth.enabled: true` and its `settings`. A reader who sees one of the three gets
   the state wrong.

A fifth rule is about the format: **the punctuation should not outweigh the content.** A sixth is
about the writer, and it is new: **an edit yolo makes changes only the bytes it was asked to.**
[§8](#8-who-writes-the-file) is that rule applied.

## 2. Where today's config misleads

Every line below is from the maintainer's effective config or this repository's. The middle
column is what the maintainer, or a reasonable cold reader, took the line to mean.

| Line today | Read cold as | What it means |
| :--- | :--- | :--- |
| `"brokered": {"git": {"grants": [{"kind": "read", "repo": "*"}]}}` | read access to every repository anywhere | fetch from each workspace's own approved remotes |
| `"grant_kinds": ["branch"]` beside that read grant | "granting read on branches" | the most a project may ask for: pushing to branches it names |
| `"brokered"` | nothing; the maintainer: *"I don't understand 'brokered'"* | things a jail may do using your host logins |
| `"profile": {"*": "bedrock", "claude": null}` | `null` reads as "unset" or "default" | every agent uses `bedrock` except claude, which keeps its own login |
| `"profiles": {"bedrock": {"provider": "bedrock", "model": …}}` | three `bedrock`s: one thing or three? | a profile named after the provider it picks, plus a starting model |
| `"loopholes": {"aws-auth": {"enabled": true, "settings": {…}}}` with `"aws-auth"` also in `packs` | two switches, either of which might be the real one | the pack is installed by `packs`; its service is off until `enabled` says otherwise |
| `"input": ["text", "image"]` beside `"cost": {"input": 10}` | two meanings of one word | what the model accepts; its price per million input tokens |
| each model's `"id"` equal to its key; `"vendor": "anthropic"` beside `global.anthropic.…` | possibly different values | the same value, written twice |
| `"network": {"mode": "bridge"}` in this repository | this repository needs bridge networking | the default, restated |
| 121 commented-out lines in this repository's config, 12 live lines | the comments are settings | a reference template `yolo init` wrote |

**Measured size.** The effective user config is 170 lines of JSON, plus about 8 lines of broker
grants the jail's copy leaves out, so 178. It sets one MCP server, three mise tools, 21 packs,
one loophole, two broker grants, a profile selection and six models. The models alone are 109
lines, and in them `id`, `input`, `max_tokens` and `reasoning` are each written six times, every
time with the same value except `id`, which repeats its key.

### 2.1 It is four files, not one

The jail's copy carries a `yolo-origins` line recording which host file set each key
(`internal/config/inherit.go` writes it). It says the "user config" is four files on the host:

| File | Sets | Reaches a machine by |
| :--- | :--- | :--- |
| `~/.config/yolo-jail/config.jsonc` | 15 packs, two mise tools | dotfiles, every machine |
| `~/.config/yolo-jail/overrides.jsonc` | `profile`, `mcp_servers` | `include_if_found` |
| `~/.dotfiles/tag-work/config/yolo-jail/work.jsonc` | `providers`, `profiles`, `loopholes`, six packs | `include_if_found`, present on work machines only |
| `…/personal.jsonc` | the `or` profile, two OpenRouter `@200k` models, `agy-capture`, `meta-muse`, `workspace-questions` | the same, personal machines only; not visible from this jail |

Two consequences for everything below. **A migration rewrites each file where it is and never
merges them**, because the split is how one dotfiles repository serves two kinds of machine. And
**"where did this value come from" is already answered per key**: the origins line is most of a
`yolo config why`, unbuilt only as a command.

## 3. Shape or format? Measured on the maintainer's config

The maintainer's question: *"I don't know if jsonc is wrong, or just our shape."* The answer is to
rewrite the same content in the [§4](#4-the-proposed-vocabulary) shape, then write that shape in
each format, and count.

| The same content | Lines |
| :--- | ---: |
| Today's shape, JSONC (the effective config, plus broker grants) | 178 |
| The [§4](#4-the-proposed-vocabulary) shape, JSONC, laid out by hand (short objects on one line) | 58 |
| The [§4](#4-the-proposed-vocabulary) shape, JSONC, fully expanded the way a formatter writes it | 133 |
| The [§4](#4-the-proposed-vocabulary) shape, TOML, with four explanatory comments | 53 |

**The shape removes about two-thirds of the lines whatever the format.** The format matters
most when a formatter expands it: JSONC that a tool rewrites grows back toward 133 lines, while
TOML's inline tables stay as written. Today that is theoretical, because nothing in yolo rewrites a
hand-written config file. Under [§8](#8-who-writes-the-file) it stops being theoretical, which is
why the editing design is a splice and not a round trip.

The packs show the same pattern ([§6](#6-packs-the-same-rules)): of the `matt` pack's 860 lines,
271 are 39 `files` entries, 32 of which only copy a file or folder to the same path, and 425 are
`config-overlay` contents, which are pi's own settings, written inside the manifest.

## 4. The proposed vocabulary

The user config gets sections named for decisions. Today's scope rules carry over unchanged: what
decides where inference goes, or what a jail may do with your logins, stays user scope only,
because a workspace file is the agent's to edit.

| Section | Holds | Replaces today | Scope |
| :--- | :--- | :--- | :--- |
| `packs`, `pack_folders` | what the jail is built from; a name for each folder your own packs come from, so `dotfiles/matt` names one | `packs` (full paths written out) | user |
| `install.nix`, `install.mise` | extra tools | `packages`, `mise_tools` | both |
| `mcp_servers.<name>` | MCP servers; the key keeps its name | `mcp_servers` | both |
| `allow.<pack>` | what a jail may do with your host logins: git, GitHub, AWS | `brokered.*`, `loopholes.<name>.enabled` and `settings` for login-serving packs | user for the most it may do, workspace for what a project asks |
| `profile` | which profile each agent uses; the key keeps its name | `profile` | user |
| `providers.<name>` | where models come from, the starting model, the model list | `providers`, the common case of `profiles` | user |
| `profiles.<name>` | a second configuration of one provider, when you need one | `profiles` | user |
| `machine` | resources, GPU, devices, network | `resources`, `gpu`, `devices`, `kvm`, `network` | both |
| `env.files`, `env.set`, `env.unset` | environment the agent gets | `env_sources` (a mixed list of paths and maps) | both |

A key not listed keeps its name. Most of the top-level keys `yolo config-ref` documents are
host-integration settings a typical config never sets.

**Plural or singular** ([RC-D3](#decision-ledger)). A key is plural when its value is a collection
the user fills: a list (`packs`, `mounts`, `ports`) or a table of named things
(`providers.<name>`, `mcp_servers.<name>`). It is singular when it is one thing or a fixed set of
settings (`install`, `machine`, `allow.git`, the agent-to-setup selection). All-singular fails on
one collision: the selection and the declarations would both be `profile.<something>`. The one
pair the rule leaves ugly, `profile` beside `profiles`, is what [OQ-RC14](#OQ-RC14) renames.

### 4.1 The levers, each one syntax-free

Each lever can be adopted or vetoed on its own ([OQ-RC1](#OQ-RC1)).

- **L1. Defaults are never written, and `yolo init` writes a short file.** It writes the few lines
  a project needs and one comment naming `yolo config-ref`. The 121-line template goes. `yolo
  check` warns about a value equal to its default and names the line.
- **L2. A key is the value it names.** A model's key is its id unless an `id` says otherwise. On a
  provider whose platform encodes the maker in the id (Bedrock's `global.anthropic.…`), `vendor`
  follows from the id unless it is written.
- **L3. Shared facts are stated once.** A provider's `every_model` table holds the facts each of
  its models shares unless the model says otherwise. This is per provider, unlike today's
  `model_facts` and `extends`, which share facts across routes to the same model.
- **L4. `packs` is the only switch.** A pack in `packs` is on; there is no second switch, so
  `loopholes.<name>.enabled` goes. A login-serving pack's settings live under `[allow.<pack>]`,
  and that table is settings, not a switch: it selects nothing and looks nothing up. **A table
  with no pack, or a pack with no table, is a nudge, not a selection** ([RC-D7](#decision-ledger)):
  yolo knows which pack a section belongs to, names it, and offers the one command.

  ```console
  $ yolo check
  ~/.config/yolo-jail/config.toml:12: [allow.aws-auth] is set, but aws-auth is not in packs, so nothing serves it.
    Add it to packs? [y/N]                       (or later: yolo pack add aws-auth)

  $ yolo check
  ~/.config/yolo-jail/config.toml:3: packs lists aws-auth, but [allow.aws-auth] names no profile, so it serves nothing.
    Set one: yolo config set allow.aws-auth.profile <aws profile>
  ```

  A pack a workspace wants off writes `off = true` under it, which is what the per-workspace
  switch file's `enabled: false` becomes. L4 amends four loophole rules and keeps each one's
  reason:
  - [R1](../reference/loophole-system.md#principles), "presence never activates": a manifest on
    disk still activates nothing; the name in `packs` is the user saying so, and nothing is listed
    by default (`run.warnIfNoPacks`).
  - R2, "absent `default_enabled` means off": the manifest key goes; the list decides.
  - R4, "host access is never on by default": `allow` is user scope only, so no project and no
    default can turn it on, and a listed login pack with no table serves nothing.
  - R5, "enable is either scope": a workspace may still turn a non-login pack *off*, and may still
    *ask* for host access it cannot grant.
- **L5. Words carry their meaning.** `brokered` → `allow`; `grants` and `grant_kinds` → `fetch`,
  `push` and `read` ([§4.2](#42-what-a-jail-may-do-with-your-logins)); `profile` keeps its name ([RC-D1](#decision-ledger)), and gets
  `default` for `*` and `false` for `null`; a model's `input` → `accepts`, `cost` → `price`,
  `context_window` → `context`; and the string mini-languages of [§4.4](#44-values-that-were-strings).
  **No value is a reserved string** ([RC-D6](#decision-ledger)): a value is `true`, `false`, a
  number, a name, or a list of names, never a phrase yolo has to recognize.
- **L6. A pack that is not shipped is named by where it comes from.** `dotfiles/matt`, where
  `[pack_folders] dotfiles = "~/.dotfiles/yolo-packs"` names the folder once; `local/<name>` for the
  conventional folder (C1); `acme/tools` for a git pack, from its URL's owner and repository. Only
  shipped packs are bare, and `yolo/<name>` is their explicit spelling. So no bare name is ever
  looked up outside the shipped set, and no collision can arise that a user has to rename their
  way out of: two authors' `tools` are `acme/tools` and `beta/tools` wherever they appear. Today a
  pack is already named by the last segment of its source address, the manifest's `name` is
  informational (`internal/packload/packload.go`, the `Pack.Name` comment), and two entries with
  one name are refused with "give one an explicit `name`" (`internal/config/packs.go`). That is
  the rename-on-collision model, and the maintainer's review of 2026-10-25 called it Python
  imports but worse. The qualified form is Homebrew's and Claude Code's ([§4.6](#46-how-other-tools-name-things-from-several-sources)),
  and [OQ-RC15](#OQ-RC15) rules on it.

### 4.2 What a jail may do with your logins

[OQ-RC4](#OQ-RC4) names the section. One key, two scopes: in the user config a key is the most any
project may get; in a project config the same key is what this project asks for, and every ask is
a line in the launch approval prompt. No value is a reserved string ([RC-D6](#decision-ledger)):
each is `true`, `false`, a name, or a list of names.

Today, the maintainer's user config and this repository's project config:

```jsonc
// ~/.config/yolo-jail/config.jsonc
"brokered": {
  "git":    { "grants": [{ "kind": "read", "repo": "*" }], "grant_kinds": ["branch"] },
  "github": { "grants": [{ "kind": "read", "repo": "*" }] }
}
// yolo-jail.jsonc
"brokered": { "git": { "grants": [
  { "kind": "branch", "repo": "github.com/mschulkind-oss/yolo-jail", "branch": "main" }
] } }
```

Proposed:

```toml
# ~/.config/yolo-jail/config.toml: the most any project's jail may do
[allow.git]
fetch = true                      # this project's own remotes, as approved at launch
push = true                       # a project may ask, branch by branch; never a grant by itself

[allow.github]
read = true
write = true                      # a project may ask for create-pr or create-issue

# yolo-jail.toml: what this project asks for
[allow.git]
push = ["main"]                   # branches it asks to push to; each is approved at launch
new_branches = true               # may create branches of its own

[allow.github]
write = ["create-pr"]
repos = ["acme/shared"]           # beyond this repository's own remotes
```

**What `fetch = true` reaches.** The project's own remotes, plus what `repos` names, approved at
launch exactly as today's `{"kind": "read", "repo": "*"}` is; it is not every repository the host
login can reach. The maintainer's review asked for that wider reading, and it is a scope change to
the broker's model, so it is [OQ-RC16](#OQ-RC16): the proposed spelling is a list of patterns in
the user config, `fetch = ["github.com/mschulkind-oss/*"]`, with `["*"]` for any.

The four ruled grant kinds ([OQ-BB19](boundary-broker.md#OQ-BB19)) map as `read` → `read`,
`create-branch` → `new_branches = true`, `branch` → `push = ["main"]`, and `create-pr` and
`create-issue` → `write`. Approval prompt lines become `+ git: fetch from
mschulkind-oss/yolo-jail` and `+ git: push to main`, and `*` leaves the vocabulary. A GitHub
`write` ask is accepted by the config and refused at use until a jail's write request can reach
`yolo approve`, whose delivery is not built (`internal/ghbroker/window.go` says so); the refusal
says that. `push = true` in the user config answers [OQ-BB23](boundary-broker.md#OQ-BB23) (may the
user config grant write kinds to every workspace) with "as a ceiling, never as a grant", which is
that question's leaning.

### 4.3 Models: one name for the common case

Today the maintainer writes a provider `bedrock`, a profile `bedrock` that picks it plus a starting
model, and a selection `{"*": "bedrock"}`. The proposal ([OQ-RC3](#OQ-RC3), option B):

```toml
[profile]
default = "bedrock"               # a provider or a profile name
claude = false                    # no setup from yolo: claude keeps its own login

[providers.bedrock]
region = "us-east-1"
model = "global.openai.gpt-6.1-sol"   # where agents start
```

A provider is selectable by its own name and starts on its `model`. A profile is needed only for a
second configuration of one provider (`bedrock-opus`, or `bedrock-bridge` with its `via`). The two
keys are different things, agent-to-setup and setup-to-provider, and their names are
[OQ-RC14](#OQ-RC14)'s; the body writes `profile` and `profiles` until it is ruled.
Provider and profile names share one namespace; a collision is refused. A shipped profile that only
repeats its provider's name is deleted; seven of the nine shipped profiles are that today. `-p`
and `profile` name the same thing, as [PP-D10](providers-and-profiles-redesign.md#PP-D10) ruled.

The standing leaning on [OQ-PP1](providers-and-profiles-redesign.md#OQ-PP1) is to keep profiles as
the only selectable thing, for the reason [OQ-AP1](active-provider-sets.md#11-decision-ledger)
gave: a provider loses a profile's options, the starting model among them. Option B answers that
reason directly: the provider carries `model`, which is the one option every same-named profile
exists to carry. The options a provider cannot carry (`via`, a second starting model) are exactly
what a profile is still for.

### 4.4 Values that were strings

Several values are small languages packed into strings, and each has a trap. Today and proposed,
each from a real project config:

**A mount.** Today the string form is parsed on colons, so a `~` path with a colon in it is taken
as one host path and skipped with a warning; this repository already uses the table form.

```jsonc
"mounts": ["~/code/acme-shared", "~/x:/ctx/x:ro"]
```

```toml
mounts = ["~/code/acme-shared"]                  # a bare path with no colon is still legal

[[mounts]]
host = "~/x"
at = "/ctx/x"                                    # read-only unless `write = true`
```

**Ports.** Today `ports` is `HOST:JAIL` and `forward_host_ports` beside it is `JAIL:HOST`;
`config-ref` has to say "Note the reversed order."

```jsonc
"network": { "ports": ["8000:8000"], "forward_host_ports": ["5432:5432"] }
```

```toml
[machine]
ports = [{ host = 8000, jail = 8000 }]           # the host reaches the jail's :8000
forward = [{ jail = 5432, host = 5432 }]         # the jail reaches the host's :5432
```

**A git pack.** Today a URL scheme that is not one.

```jsonc
"packs": ["git+https://github.com/mschulkind-oss/matt-craft?ref=main"]
```

```toml
packs = [{ git = "https://github.com/mschulkind-oss/matt-craft", ref = "main" }]
```

**Environment.** Today one list mixes file paths and maps, and `null` means unset.

```jsonc
"env_sources": ["~/.config/yolo-jail/secrets.env", { "AWS_PROFILE": null, "PAGER": "cat" }]
```

```toml
[env]
files = ["~/.config/yolo-jail/secrets.env"]
set = { PAGER = "cat" }
unset = ["AWS_PROFILE"]
```

**The broker grants** are [§4.2](#42-what-a-jail-may-do-with-your-logins), and the model rows
[Appendix A](#appendix-a-the-maintainers-config-whole).

### 4.5 Lines that need not exist

The maintainer, 2026-10-25: *"is there anywhere we can apply a healthy dose of convention over
configuration? I like that. happy paths everywhere."* A convention here is a default that follows
from something the user already did, so the line that would say it is never written. Each one
below is shown as the lines it removes; [OQ-RC13](#OQ-RC13) takes or vetoes each alone, and its
leaning takes six of the eight.

Three rules bound every one. The explicit key still exists and wins when written. `yolo config
show` prints what a convention supplied, so nothing is invisible:

```console
$ yolo config show
[allow.git]
fetch = true                      # by convention: [allow.git] is present
```

And no convention widens what a jail may do with a login: `allow` is user scope, and a convention
reads only what the user wrote there.

**C1. Your packs folder.** `~/.config/yolo-jail/packs/<name>` is `local/<name>`.

```toml
# before
pack_folders = { dotfiles = "~/.dotfiles/yolo-packs" }
packs = ["dotfiles/matt", "dotfiles/matt-fzf"]

# after, with ~/.config/yolo-jail/packs a symlink to ~/.dotfiles/yolo-packs
packs = ["local/matt", "local/matt-fzf"]
```

**C2. Machine files.** `~/.config/yolo-jail/config.d/*.toml` are read after `config.toml`, in
name order.

```toml
# before, in config.toml
include_if_found = ["overrides.toml", "../../.dotfiles/tag-work/config/yolo-jail/work.toml"]
```

```text
# after: no line; the folder says it, and a dotfile tag puts work.toml there on work machines only
~/.config/yolo-jail/
  config.toml
  config.d/
    overrides.toml
    work.toml -> ~/.dotfiles/tag-work/config/yolo-jail/work.toml
```

**C3. A section selects its pack.** Not in the leaning: it makes a heading a pack lookup. L4's
nudge covers the case instead: the section with no pack names the pack and offers to add it.

```toml
# before
packs = ["claude", "bedrock", "aws-auth", "git", "github"]
[allow.aws-auth]
profile = "my-sso-profile"

# after
packs = ["claude"]
[allow.aws-auth]                  # selects aws-auth; [providers.bedrock] and [allow.git] likewise
profile = "my-sso-profile"
```

**C4. A grant table's presence is its common grant.**

```toml
# before
[allow.git]
fetch = true

[allow.github]
read = true

# after
[allow.git]

[allow.github]
```

**C5. A project needs no file.**

```text
# before
acme/
  yolo-jail.toml                  # "# This project's jail. Every key: yolo config-ref"
  .gitignore                      # + ".yolo/"

# after: nothing; `yolo` runs with defaults, and .yolo/ goes into .git/info/exclude
acme/
```

**C6. A pack needs no manifest.** Folders say what they are: `home/`, `skills/`, `briefing/`,
`overlays/<surface>[@<profile>].<ext>`, `patches/`. `pack.toml` is for what a folder cannot say.

```text
# before                                  # after
luna-worker/                              luna-worker/
  pack.toml   (5 lines, B.7)                overlays/pi-settings.jsonc
  README.md                                 README.md

claude-bedrock-global/                    claude-bedrock-global/
  pack.toml   (19 lines today)              overlays/claude-settings@bedrock.jsonc
  README.md                                 README.md

workspace-questions/                      workspace-questions/
  pack.toml                                 briefing/workspace-questions.md
  briefing/workspace-questions.md
```

**C7. A model named without facts takes the catalog's.** Pending the check that pi's catalog
carries them.

```toml
# before
"global.anthropic.claude-opus-5-5" = { name = "Opus 5.5", context = 1_000_000, price = { input = 4, output = 20, cache_read = 0.2, cache_write = 5 } }

# after
"global.anthropic.claude-opus-5-5" = { name = "Opus 5.5" }
```

**C8. One declared setup is everyone's.** Not in the leaning: a declaration chooses.

```toml
# before
[run_on]
default = "bedrock"

[providers.bedrock]
region = "us-east-1"

# after
[providers.bedrock]               # the only setup this machine declares, so everyone's
region = "us-east-1"
```

The six of the leaning take the maintainer's files to
[Appendix A.2](#a2-the-same-with-the-conventions): no include line and no pack-folder line. C3
and C8 would also take four names out of `packs` and the selection line off both kinds of
machine, and they are left out because each acts at a distance: C3 makes every `allow.<pack>`
and `providers.<name>` heading a pack lookup, which the maintainer read as *"implying a search for
a pack through a lookup system"*; it is. His alternative is L4's nudge: the same inference, spent
on a question with a `y` rather than on a selection ([RC-D7](#decision-ledger)). The pack levers already carry the same idea: P1's
`home/`, P6's `codec` from the extension, and pi packages ([§6.1](#61-why-ten-files-take-fifty-lines)).
The commands of [§8](#8-who-writes-the-file) are conventions of the same kind: a write runs
`yolo check` after itself, a grant offers its approval, a launch offers the migration. The line
you did not write is the line you cannot misread.

### 4.6 How other tools name things from several sources

The maintainer, on the rename-on-collision model: *"there's like a de facto registry … This is
just like Python imports, but worse … Find me other tools that do something similar."* Checked
2026-10-25 against each tool's documentation where linked; the rest are stated from documentation
not re-read for this draft.

| Tool | A bare name means | A thing from another source | Two with one name | Model |
| :--- | :--- | :--- | :--- | :--- |
| Python imports | the first match on `sys.path` | the same bare name | silently shadowed | the failure to avoid |
| apt | one flat name across every repository | the same name | pin priorities decide, silently | the same failure, by policy |
| [Homebrew](https://rubydoc.brew.sh/TapFormulaAmbiguityError.html) | a core formula | `user/repo/name` from a tap | refused: "use the fully-qualified name" | **built-ins bare, everything else qualified** |
| [Claude Code plugins](https://code.claude.com/docs/en/discover-plugins) | — | `plugin@marketplace` | cannot meet; a marketplace name is unique, and adding one with the same name replaces it | the same, with `@` |
| Docker images | `library/ubuntu` | `user/image`, with a registry prefix | cannot meet | the same |
| Nix flakes, Terraform providers | — | every input has a local name you choose, plus its source | cannot meet: you named both | all handles, no bare names |
| Cargo | a crates.io name | `key = { git = …, package = "x" }`, the key being your name for it | renamed through the key | rename on collision |
| Go imports | — | the import path is the identity | an import alias | rename on collision |
| [lazy.nvim](https://git.kjan.de/jank/lazy.nvim/src/tag/v9.8.4/doc/lazy.nvim.txt) | — | `owner/repo`, local directory named by the repository | `name =` overrides | rename on collision |

Two models survive the table without a rename step: **qualified names with bare built-ins**
(Homebrew, Claude Code, Docker), and **all handles** (Nix, Terraform), where nothing is bare and
the config is a table of names you chose with their sources. The first costs one token on each
non-shipped pack (`dotfiles/matt`); the second costs a table entry for every shipped pack too
(`[packs.claude]` with nothing under it), which is what `packs = ["claude"]` exists to avoid.
[OQ-RC15](#OQ-RC15) chooses; L6 is written for the first.

## 5. The same content in four formats

The same excerpt each time: the logins section and one model. Full renderings are in
[Appendix A](#appendix-a-the-maintainers-config-whole).

**JSONC**, today's format in the new shape:

```jsonc
{
  "allow": {
    "git": {"fetch": true, "push": true},
    "aws-auth": {"profile": "hssandbox-BedrockInference", "unnarrowed": true}
  },
  "use": {"default": "bedrock", "claude": false},
  "providers": {"bedrock": {
    "region": "us-east-1",
    "models": {"global.openai.gpt-6.1-sol": {"name": "Sol 6.1", "context": 272000}}
  }}
}
```

**TOML:**

```toml
[allow.git]
fetch = true
push = true                       # a project may ask, branch by branch

[allow.aws-auth]
profile = "hssandbox-BedrockInference"
unnarrowed = true

[profile]
default = "bedrock"
claude = false

[providers.bedrock]
region = "us-east-1"
models."global.openai.gpt-6.1-sol" = { name = "Sol 6.1", context = 272_000 }
```

**YAML:**

```yaml
allow:
  git: {fetch: true, push: true}
  aws-auth: {profile: hssandbox-BedrockInference, unnarrowed: true}
use: {default: bedrock, claude: false}
providers:
  bedrock:
    region: us-east-1
    models:
      global.openai.gpt-6.1-sol: {name: Sol 6.1, context: 272000}
```

**KDL:**

```kdl
as-you {
    git fetch=#true push=#true
    aws-auth profile="hssandbox-BedrockInference" unnarrowed=#true
}
use default="bedrock" claude=#false
provider "bedrock" region="us-east-1" {
    model "global.openai.gpt-6.1-sol" name="Sol 6.1" context=272000
}
```

| | JSONC | TOML | YAML | KDL |
| :--- | :--- | :--- | :--- | :--- |
| Known without learning | everyone | most developers: Cargo, `pyproject.toml`, and `mise.toml`, which this config already feeds | most developers | few |
| Punctuation per line | high | low | lowest | low |
| A formatter expands it | yes, to 133 lines | no: inline tables stay | depends on the tool | no |
| Misreads values | no | no | **yes:** `python: 3.10` is the number 3.1; a mise tool version is exactly this value | no |
| Indentation is meaning | no | no | yes | no |
| Deep nesting | fine | headings grow long | fine | fine |
| Go library | `internal/json5`, yolo's own | `BurntSushi/toml`, vendored and used today (`internal/tomlx`) | `yaml.v3`, vendored and used today, but frozen upstream | one new dependency, unstable 0.x |
| Position of a key, for a splice | yes: `json5.Locate` exists for this, unused outside tests | not from BurntSushi; from `go-toml/v2/unstable` or yolo's own scanner | yes, from the node tree | yes |
| Comments survive yolo's writes today | never: every JSON write rebuilds the file | yes, through `tomltrivia.go`'s reattach, unless the value under a comment changed | yes, through `yamltrivia.go` | — |

**TOML is the recommendation** ([OQ-RC2](#OQ-RC2)). It is the one format with all three: low
noise, no value misreads, and familiar to the people who read these files. It is not a new
dependency; that claim in an earlier draft was wrong. Its one weak spot, long section headings for
deep nesting, is what [§4](#4-the-proposed-vocabulary)'s flatter shape avoids. **JSONC in the new
shape is a respectable second**, and it is the format whose splice is nearest to built. YAML loses
on the version-number misread. KDL fails the cold read for a writer.

**Files only yolo reads stay JSON**: the approval records, lock files, `config-boot.json`,
`config-assembled.json`, and `yolo config dump --json`. Files a person is expected to open are in
the person's format: the user and project configs, pack manifests, the jail's inherited copy of the
user config, and any companion file ([§8.2](#82-four-ways-yolo-could-write)).

## 6. Packs: the same rules

The maintainer's twelve packs have 1,688 manifest lines, 394 of them comments. `matt` is 860
lines and `meta-muse` 512. The other ten run from 9 to 74 lines. By contribution kind, `matt` is:

| Kind | Entries | Lines | Of them comments |
| :--- | ---: | ---: | ---: |
| `config-overlay` | 7 | 425 | 81 |
| `files` | 39 | 271 | 22 |
| `config` | 1 | 57 | 21 |
| `autonomy` | 1 | 42 | 6 |
| the other five kinds | 8 | 59 | 5 |

**Most comments explain yolo, not the pack.** `agy-capture` gives 24 of its 74 lines to two
warnings. Its launch flags restate `--dangerously-skip-permissions`, because a later pack's flag
list replaces an earlier one's whole (`launchFlagClaims` in `internal/packload/packload.go`
assigns, it does not append). Delete that flag and agy prompts for every tool in a jail. Its
`statusLine` overlay restates the whole object, because overlays replace objects. When a pack
author has to explain the merge rules on every entry, the merge rules are the shape problem.

**Models are written twice, in two formats.** `meta-muse` writes six model rows straight into pi's
own `models.json` through a `pi/models` overlay: three models and a `[200k]` twin of each, 199 lines
once expanded. It writes a seven-level `thinkingLevelMap` six times. yolo has `wire_id` and
`model_facts` for exactly that pair, and the pack cannot use them, because yolo's model facts have no
thinking levels, image limits or wire API.

Six levers cover most of it:

- **P1. A `home/` folder in a pack is laid into the home directory, file by file.** It replaces
  `files` entries that only copy a file or folder to the same path: 32 of `matt`'s 39, 6 in
  `matt-mods` and 1 in `agy-capture`. File by file means it merges with other packs' and the
  user's own files in a shared folder. Two packs claiming one file still refuse the launch. A
  `files` entry stays for what a folder cannot say: a `source` to fetch, `patches`, a `build`.
  [§6.1](#61-why-ten-files-take-fifty-lines) is why this is needed, and the list form it adds.
- **P2. An overlay's content lives in its own file, in the agent's own format.**
  `overlays/pi-settings.jsonc` holds what is now the `config` object of a `config-overlay` entry,
  and the manifest names the surface and, if any, the profile. The 425 lines move out of the
  manifest rather than shrinking. The repeated tool lists across pi's subagent roles are pi's own
  settings shape, and no yolo format change removes them.
- **P3. Group by kind, and state the pack's identity once.** This is
  [`manifest-language.md`](manifest-language.md)'s model change: `[[program]]` entries together,
  no repeated `"kind"` and no repeated `"agent": "claude"`.
- **P4. A contribution states only what it changes.** A later pack's launch flags add to the list,
  and an overlay merges into an object key by key. Replacing must be said in as many words
  (`replace = true`). `agy-capture` then writes `--model gemini-3.8-flash-medium` and nothing else.
- **P5. A model is declared once, in yolo's model shape, for every agent.** yolo's model facts
  gain thinking levels, image limits and wire API. `meta-muse`'s six rows become three models with
  a `[200k]` twin each ([Appendix B.8](#b8-meta-muses-models-under-p5)).
- **P6. The obvious is not written.** An install hint that is the same on every package manager
  is one value: `install = "jq"`, or `install = { all = "fd", apt = "fd-find", dnf = "fd-find" }`.
  A `config` entry's `codec` follows from its path's extension, `state`'s `scope = "workspace"` is
  the default, and `run` defaults to `bin`. Each is still writable. Shipped packs keep the
  per-manager form, which keeps [`pack-conventions.md`](pack-conventions.md)'s test that every shipped hint cites its
  source per manager.

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

[`pack-conventions.md`](pack-conventions.md#appendix-a-counts) counted ten single-file `files`
entries in `matt` on 2026-10-05 and found the folder conventions saved about five lines in all.
That count covered the pi-path entries only; this one covers every copy-only entry across his
packs, and there are 39. Its rejections of a one-value `install_hints` and of a provider implying
its profile were that document's own verdicts, not the maintainer's rulings, so this doc re-opens
both.

### 6.1 Why ten files take fifty lines

The maintainer: *"putting 10 files somewhere requires like 35 lines each repeating every source and
dest path."* Measured: `matt` copies 25 files into `~/.pi/agent/extensions/` with 25 `files`
entries, 127 lines. Each writes its file's path twice.

**The cause is a mechanism, not the format.** A `files` entry is a read-only mount over its whole
destination, and two entries claiming one path refuse the launch
([`pack-system.md`](../reference/pack-system.md), the `files` section). So a pack cannot hand over
the folder `~/.pi/agent/extensions/` when `matt-mods` adds `tok-stats.ts` to the same folder.

**For pi, the fix has already shipped, and these packs predate it.**
[OQ-PR1](../reference/pack-system.md#oq-pr1), ruled 2026-10-05, lets a pack reach pi with one
entry, `{"kind": "files", "agents": ["pi"], "from": "files/pi"}`: pi loads that folder as a
package, every top-level file in its `extensions/`, `themes/` and `prompts/`. Moving
`files/extensions` to `files/pi/extensions` turns 26 of `matt`'s entries into one, today, with no
yolo change. `yolo pack migrate` makes that move.

**For every other folder, P1 and a list form.** Where the pack's own layout should not mirror the
home, one entry takes a list:

```toml
[[files]]
from = "files/extensions"
into = ".pi/agent/extensions"
only = ["turn-bell.ts", "versions.ts", "board", "work-control"]   # or omit `only` for every file
```

Each listed name is mounted on its own, so the file-by-file merge and the two-claims refusal both
hold. Measured on the 25 extensions, the list form is 30 lines against 127.

### 6.2 Keys the derive overwrites without a word

Both large packs carry long comments on one trap: an overlay key that pi's derive also writes
(`subagents.defaultModel`, `defaultProvider`, `modelScope.allow`) loses at every boot, silently,
because the `computed` layer folds after every overlay. `matt`'s comment adds that
`yolo config render` cannot show it, since it omits the computed layer. That is a readability
problem of a different kind: the file reads right and does nothing. It is not a shape change, and
it is in scope here only as a `pack lint` finding: an overlay key the surface's derive writes is
named, with the layer that wins. The seed carries it.

## 7. Measuring the repetition

The first compression run is [`manifest-language.md` §1](manifest-language.md) (2026-09-20).
This is the second, on 2026-10-24, over the twelve packs and the user config.

**Method.** Each file has its comments removed and is minified, so indentation and comments do
not count. Then gzip −9 and xz −9e. **Ratio** is compressed bytes over minified bytes: lower means
more of the file was predictable from the rest of it. The **prose baseline** is the first N bytes
of [`what-yolo-is.md`](../reference/what-yolo-is.md), cut to each file's minified size.

| File | Minified | gzip bytes | gzip ratio | xz ratio | Prose, same size |
| :--- | ---: | ---: | ---: | ---: | ---: |
| `matt` | 15,264 | 3,669 | **0.24** | 0.24 | 0.40 |
| `meta-muse` | 4,513 | 1,149 | **0.25** | 0.27 | 0.43 |
| `matt-mods` | 1,224 | 455 | **0.37** | 0.43 | 0.56 |
| `agy-capture` | 801 | 421 | 0.53 | 0.64 | 0.60 |
| `matt-fzf` | 667 | 314 | 0.47 | 0.58 | 0.61 |
| `luna-worker` | 464 | 301 | 0.65 | 0.82 | 0.66 |
| `coordinator-reads-blocked` | 574 | 341 | 0.59 | 0.77 | 0.64 |
| `claude-bedrock-global` | 519 | 349 | 0.67 | 0.82 | 0.65 |
| `pi-fork` | 435 | 280 | 0.64 | 0.86 | 0.66 |
| `matt-agentwork` | 290 | 193 | 0.67 | 0.91 | 0.70 |
| `workspace-questions` | 331 | 226 | 0.68 | 0.89 | 0.69 |
| `deepseek-flash-only` | 203 | 151 | 0.74 | 1.06 | 0.74 |
| shipped `packs/claude` | 6,110 | 1,649 | 0.27 | 0.28 | 0.41 |
| shipped `packs/pi` | 5,582 | 1,903 | 0.34 | 0.35 | 0.41 |

Sections of `matt` and `meta-muse`, measured the same way: `matt`'s 25 extension copies, ratio
**0.15**; `meta-muse`'s `pi/models` overlay, **0.18**; `matt`'s seven overlay bodies, 0.26.

**What it says.** The small packs are not repetitive: seven compress within 0.05 of prose. Their
trouble is per-line noise. The repetition is where the eye says it is: the extension copies and
the six model rows are the most repetitive text measured in either exercise. Comments are 28% of
`matt`'s bytes and 63% of `meta-muse`'s.

**Before and after.** Lines are without comments or blanks; bytes are minified:

| The same content | Lines | Bytes | gzip bytes | gzip ratio |
| :--- | ---: | ---: | ---: | ---: |
| user config, today (JSON) | 170 | 2,447 | 769 | 0.31 |
| user config, proposed (TOML, [Appendix A](#appendix-a-the-maintainers-config-whole)) | 40 | 1,818 | 781 | 0.43 |
| `matt-fzf`, today | 56 | 667 | 314 | 0.47 |
| `matt-fzf`, proposed | 14 | 391 | 227 | 0.58 |
| `matt`'s 25 extension copies, today | 127 | 2,794 | 419 | 0.15 |
| the same as one list entry | 30 | 637 | 298 | 0.47 |
| `matt` whole, today | — | 15,264 | 3,666 | 0.24 |
| `matt` without its plain `files` entries (P1) | — | 11,845 | 3,216 | 0.27 |
| …and with its overlay bodies moved to files (P2) | — | 5,315 | 1,598 | 0.30 |

**The ratio cannot be the target.** The proposed user config is a quarter of the lines, and gzip
gives it *more* bytes than today's. The repeats gzip was discarding are gone; what remains is the
information, and the reshape kept all of it. So the exercise is a detector, and three numbers are
the scorecard: gzip ratio below the prose line, by section (anything 0.10 or more under prose is
repeating itself; `pack lint` can report it); lines a person reads; and facts written more than
once. The compressed size should stay flat while the other two fall.

## 8. Who writes the file

The maintainer, 2026-10-24: *"Another thing that we've put off is being able to modify people's
configuration files. It's real hard to modify a JSONC file and not just completely foul up all of
the indentation … we have a bunch of places where we ask agents to edit config files where it
would be nice if we could have some mechanical commands."*

### 8.1 Sixty places that say "edit your config"

About sixty sites in `internal/` and `cmd/`, two always-on briefing sections, the two built-in
skills, and the pack READMEs tell a person or an agent to hand-edit a config file. Grouped by the
edit asked for, most frequent first:

| Edit asked for | Sites | Target | Then |
| :--- | ---: | :--- | :--- |
| add a pack to `packs` | about 14 in Go, plus most pack READMEs | user config | restart |
| add the broker read grant `{"kind": "read", "repo": "*"}` | one message, `BrokerGrantAdvice`, from about 11 call sites, plus the git and gh briefings | user config | `yolo config apply`, approve |
| turn a loophole on, or set its settings | about 10 in Go, most pack READMEs | user config, or either scope | restart |
| add `packages`, `mise_tools` or `resources` | the briefing on every launch, about 5 in Go, the skill | project config | `yolo check`, restart |
| move a user-scope-only key out of the project file | about 7 | user config | `yolo check` |
| add, remove or move a `branch` or `create-branch` grant, or edit `grant_kinds` | about 6, plus the git briefing | project config; `grant_kinds` user | `yolo config apply` |
| set an `aws-auth` setting | about 6 | user config | restart |
| add a repo to `brokered.github.repos` | one generator, the gh briefing, the skill | project or local config | `yolo check --no-build`, `yolo config apply` |
| set `programs.overrides`, `runtime`, a provider model, and nine one-off keys | 1 to 4 each | mixed | mixed |

Only the per-workspace loophole switch has a command today, `yolo loopholes enable`, and it writes
its own file rather than `yolo-jail.jsonc`, because of the ruling in
[§8.5](#85-the-ruling-this-meets). Nothing else in yolo writes a file a person also writes:
`yolo init` refuses to touch an existing file, and `yolo config promote` refuses a `pack.jsonc`
outright because rewriting it through a JSON encoder would drop every comment.

### 8.2 Four ways yolo could write

| | What the person sees | Handles | Cost |
| :--- | :--- | :--- | :--- |
| **W1. Splice** | one hunk in their own file; every other byte identical; comments stay | add, change, remove, in either scope | position-reporting parse per format, and a few hundred lines of yolo-owned edit code per format |
| **W2. Companion file** | their file untouched; a second file beside it, written whole by yolo in canonical layout, with a header saying so | add; change only of what yolo itself wrote; remove only with a removal spelling | a "where did this come from" command, which the origins line already answers; list items cannot be removed, because lists merge by union |
| **W3. Library round trip** | their file rewritten in the library's layout on the first edit; comments mostly kept | add, change, remove | a new dependency per format (`tomledit` is pre-1.0 and reformats spacing; `hujson` keeps bytes but its `Format` is opinionated) |
| **W4. Print the edit** | the lines to paste, and the file to paste them into | nothing; today's behavior with better text | none |

**W1 is the recommendation, with W2 kept for what yolo writes on its own** ([OQ-RC9](#OQ-RC9)).
The reasons:

- **The ruling's complaint was the mess, and a splice is the absence of one.** A splice changes
  the bytes of one value. The person's indentation, comments, blank lines and key order survive
  because they were never read as anything but bytes. W3 is what the maintainer is describing when
  he says editing fouls up the indentation.
- **Half of it exists.** `json5.Locate` (`internal/json5/locate.go`) returns the byte range of a
  value by key path, and its own comment says it exists for in-place edits; nothing outside its
  tests calls it. For TOML, yolo's `tomltrivia.go` already scans comments by position.
- **W2 cannot remove.** Every list in the config merges by union (`internal/config/load.go`), so a
  companion file can add a pack and never remove one. Removal is where a mechanical command earns
  the most, because a removal by hand is where a reader deletes the wrong line.
- **W2 is already the right answer for one class**, and stays it: state yolo writes without being
  asked, in a command whose purpose is something else. The per-workspace switch file is that class,
  and so would be a per-machine tag ([§10](#10-other-problems-this-could-solve)). The distinction is
  who asked: a command the person typed to change their config edits their config; a side effect
  never does.

**What a splice does, exactly.** This is the behavior; the mechanism is the plan's.

- It parses the file for positions only. A new key goes at the end of its table; a new table at the
  end of the file, after one blank line; a new list item after the last item, with the list's own
  separator and line-breaking copied from its neighbors; a removed key takes its trailing comma or
  newline and the comment lines directly above it, and prints those comments.
- Indentation is copied from the nearest sibling; in an empty table or file, from the file's
  dominant indent; in an empty file, two spaces for JSONC and none for TOML.
- It writes to a temporary file and renames, as the per-workspace file does, so a reader never sees
  a half-written file. Two commands at once serialize on the same lock the broker store uses; the
  second re-reads and re-splices.
- After the write it re-parses the result and compares the effective config with the one it meant
  to produce. A mismatch restores the original bytes and refuses, printing both.
- An item already present is a no-op that says so. A key absent on removal is the same.
- It never reorders, never reformats a line it did not write, never writes a key it cannot
  validate, and never touches a file it did not name in its output.
- `--dry-run` prints the hunk and writes nothing. Without it the command writes and prints the
  hunk, so what changed is on the screen.

### 8.3 The commands

The decision for the maintainer is the spelling ([OQ-RC10](#OQ-RC10)). Whatever it is, each
command maps onto one section of [§4](#4-the-proposed-vocabulary), and every one of the sixty
messages prints the command instead of the JSON. Two spellings are on the table:

**Named verbs, one per decision:**

```console
$ yolo pack add matt                       # user config: packs += "matt"
$ yolo pack remove journal
$ yolo install nix just                    # project config: install.nix += "just"
$ yolo install mise python@3.12 --user
$ yolo allow git fetch                     # user config: [allow.git] fetch = true
$ yolo allow git push main                 # project config: [allow.git] push += "main"
$ yolo allow github repos acme/shared
$ yolo profile bedrock                         # user config: [profile] default = "bedrock"
$ yolo profile claude false
$ yolo mcp off tavily --local              # yolo-jail.local: [mcp_servers.tavily] off = true
```

**One generic verb, keyed by path:**

```console
$ yolo config add packs matt
$ yolo config set allow.git.fetch true
$ yolo config set profile.claude false
$ yolo config add allow.git.push main --project
$ yolo config unset mcp_servers.tavily --local     # writes off = true where the key would be
$ yolo config set --dry-run machine.resources.memory 32g
```

Both can exist: the named verbs as sugar over the generic one. The generic one is what the sixty
messages print, because a message knows the key; the named ones are what a person types.

**Where a command writes, by default.** A user-scope-only key goes to the user config; a key both
scopes accept goes to the project config; `--user`, `--project` and `--local` override; in a jail,
a user-scope key is refused with the `yolo host-run` line that does it on the host, as the skill
already instructs for brokered loopholes. A key with an `include_if_found` origin is edited in the
file that set it, which the origins record names; a key set in two files is refused, naming both,
until `--file` picks one.

**What happens next is what happens today.** A project-config edit lands at the next
`yolo config apply` or launch, through the same y/N diff gate; an agent in a jail runs
`yolo check --no-build` after it, as the briefing already says. A user-config edit is trusted and
never prompts, except `allow` grants, which go through the broker's approval as today; the command
that writes one offers to run `yolo config apply` next, so the two-step becomes one answer.

### 8.4 The verb `yolo config` is taken

Today `yolo config` is the agent-config composition pipeline: `ls`, `render`, `diff`, `reset`,
`promote`, `capture`. Beside them sit `apply`, `drift` and `dump`, which are about yolo's own
config. Adding `set`, `migrate` and `why` to the first group puts two meanings under one verb, and
the help text already shows both. [OQ-RC12](#OQ-RC12) decides whether the six composition
commands move (to `yolo surface …`, say) or the new verbs go elsewhere.

### 8.5 The ruling this meets

[OQ-BB12](boundary-broker.md#15-decision-ledger), 2026-10-01, the maintainer: *"I don't want to do
anything that edits the user config directly. We can edit a file that lives next to it of a
different name or whatever … I don't want to do anything ugly like trying to edit the user's
configuration because it always leaves that a mess."* It was built as the per-workspace switch
file, and five sibling docs restate it.

Both halves of this section rest on reading that ruling as being about the mess and the side
effect, not about the file. A splice is not a mess, and a command the person typed is not a side
effect. That reading is this doc's, and [OQ-RC9](#OQ-RC9) is where the maintainer says whether it
holds; if it does not, W2 is the whole answer and removals stay by hand.

## 9. Moving to the new config

The maintainer: *"It seems like if we change file names here that would allow us a pretty easy
transition plan. We can version it. I suppose we can put a version field in the JSON that we read
first."*

### 9.1 How a yolo knows which config it has

Three ways to mark a generation, and they are not equivalent ([OQ-RC11](#OQ-RC11)):

| | How an old yolo meets a new file | How a new yolo meets an old file | Works across a format change |
| :--- | :--- | :--- | :--- |
| **V1. The file name** (`config.toml`, `yolo-jail.toml`, `pack.toml`) | it does not look for it: it finds no config and launches a jail with no packs, or treats a pack folder as manifest-less and stages its files silently | it refuses `config.jsonc` by name and offers `yolo config migrate` | yes |
| **V2. A version key, same file name** (`yolo = 2` first in the file) | it reads the file and fails on the first unknown key, which is already a hard error on the host; the message names the key, not the upgrade | it reads the key first, then refuses or migrates | no: a `.jsonc` file cannot hold TOML |
| **V3. Both** | as V1, and the key is there for the generation after this one | as V1 | yes |

**V1 is the recommendation, and it is what the research found every surveyed tool converged on.**
ESLint's flat config is a new file name plus a migrate command; Docker Compose's `version` key
switched semantics between 2 and 3, confused users for years, and is now ignored with a warning;
Ruff kept reading its old keys forever with a warning and needed neither. A version key earns its
place only when the *same* file will change shape again within one format, and this change is a
format change. The name says it. The `.jsonc` extension says "old"; `.toml` says "new"; and if the
shape changes again inside TOML, that is a new name or a `yolo = 3` key added then, when the
reason exists.

The one thing V1 cannot do is make an old yolo say "upgrade": it does not know the new name. That is
what the first release is for.

### 9.2 The order of releases

1. **Release N recognizes the new names and refuses them.** A `config.toml`, `yolo-jail.toml` or
   a pack folder holding `pack.toml` stops the launch with "this file is for a newer yolo: run
   `yolo update`". It changes nothing else, so it ships long before the rest. The pack case matters:
   without it an older yolo treats a migrated pack as a manifest-less one and silently stages only
   its files.
2. **Release N+1 reads only the new format** and refuses an old file, naming `yolo config migrate`.
   A launch that finds only old files offers the migration (`y/N`), shows the diff, and on `y`
   writes and continues, which is the [happy-path principle](../reference/happy-path-principle.md).
3. **Every machine that shares dotfiles upgrades before any of them migrates.** The maintainer's
   user config reaches work and personal machines through `~/.dotfiles`, so one machine's migration
   rewrites the file every other machine reads. Release N is what makes that order safe: an older
   machine refuses the new file by name instead of starting with no packs.

### 9.3 What `yolo config migrate` does

- **It proves it lost nothing.** It computes the effective config before, from the old files, and
  after, from the files it would write, as canonical JSON in the new vocabulary, and refuses to
  write unless the two are equal. Any difference is printed key by key.
- **It rewrites each file where it is, and never merges.** `config.jsonc` → `config.toml`,
  `overrides.jsonc` → `overrides.toml`, each `include_if_found` target it can reach → its `.toml`
  twin, with the include entries respelled. A file reached through a symlink is written through the
  link, so the dotfiles copy changes. A file it cannot reach is listed with the command to run where
  it can. Each original is kept beside its replacement as `<name>.pre-migrate`.
- **It keeps comments.** A comment moves with the key it sits above; JSONC key positions come from
  the same index a splice uses. One whose key is gone (a default no longer written, a fact folded
  into `every_model`) stays in place with `# (was on: <old key>)`. Nothing is dropped silently.
- **It drops written defaults and folds repeated facts** (L1 to L3) only while the equality check
  holds, and each fold is its own hunk in the diff.
- **It rewrites the records only yolo reads, in the same run.** The approval records under
  `~/.local/share/yolo-jail/approvals/` and each workspace's `.yolo/config-boot.json` hold the
  canonical JSON; the renames change it, so without this step every migrated workspace meets a
  full-file approval prompt once, and a launch with no terminal is refused until someone answers it.
  Migrate respells every record it finds key for key. A record it cannot read is left, and that one
  workspace prompts once, which the output says.
- **It cleans one leftover.** A zero-byte `~/.config/yolo-jail/config.lua` is deleted and named;
  the Lua transform that read it is gone (`internal/config/inherit.go`). A non-empty one is refused,
  naming it, since nothing can run it.
- **It is one command, run once, on request.** It never runs unasked; the launch offers it. It runs
  on the host for the user files, and in a jail only for the project's files, because a jail sees
  only a generated copy of the user config.

### 9.4 Behavior in every case

- **Both `config.toml` and `config.jsonc` in one place.** Refuse, naming both and `yolo config
  migrate`, which keeps one. Formats are never merged.
- **An old key in a new file** (`brokered`, say). `yolo check` and the launch refuse, naming the
  replacement line already respelled, as the `use_profiles` refusal does today
  (`internal/config/validate.go`). No aliases.
- **A workspace file in a jail.** An agent may run `yolo config migrate` on the project's files;
  the user config is migrated on the host.
- **Shipped packs** are migrated in the commit that changes the reader.
- **A local pack in the old format** is refused, naming `yolo pack migrate <path>`, which rewrites
  the manifest, keeps comments, moves plain-copy `files` entries into `home/` or a pi package folder,
  and uses `git mv` when the pack is a git checkout.
- **A fetched pack in the old format**: [OQ-RC8](#OQ-RC8).
- **A yolo older than this change**: [OQ-RC6](#OQ-RC6) and [§9.2](#92-the-order-of-releases).
- **Files only yolo reads** stay JSON, in the new vocabulary. A drift report and an approval
  prompt say `allow.git.push`, never `brokered.git.grants`. The jail's inherited copy of the user
  config becomes TOML, so what an agent reads matches what the user wrote.
- **An error in a new file** names `file:line:col` and the key. Today's JSONC errors name a byte
  offset only (`internal/json5/json5.go`); the position index that serves a splice serves this too.
- **The repository's own references.** Over 400 Go files name `yolo-jail.jsonc` or `config.jsonc`,
  and only the workspace name has a shared constant. The seed carries the fixture helper that stops
  the next format change from touching 400 files.

### 9.5 What done looks like

The maintainer's four files, migrated, read correctly cold against [§2](#2-where-todays-config-misleads)'s
table and total under 60 lines. This repository's is under 15. `matt`'s manifest holds no
single-file `files` entry and no overlay body; `agy-capture` restates no flag it does not change;
`meta-muse` declares each model once. The approval prompt has no `*` and no `kind`. Every one of
the sixty messages prints a command, and running it changes one hunk of one file. A migrated
workspace launches with no new approval prompt.

## 10. Other problems this could solve

- **TOML has no `null`, and today's config uses `null` for many things.** "Keep your own login"
  (`claude: null`), remove an inherited MCP server, unset an environment variable, disable a lower
  layer's value in an include, remove a model fact, clear a `repos` list, drop a provider or an
  adapter override, and more; `yolo config-ref` mentions `null` on 27 lines. [OQ-RC7](#OQ-RC7)
  picks the spellings; the shipped prism already meets the same problem on TOML surfaces, where a
  managed `null` deletes the key ([`pack-system.md`](../reference/pack-system.md)).
- **`${VAR}` looks like interpolation, and yolo does none.** `TAVILY_API_KEY = "${TAVILY_API_KEY}"`
  reaches the agent's MCP config literally. Either yolo expands it at render time, or `yolo check`
  notes the pass-through on that line. Idea; the first changes what reaches the agent.
- **`yolo config why <key>`** prints each file that set the key, in order, from the origins record
  that already exists; `yolo config show` prints the effective config in the new format with a
  `# from: <file>` comment per section. Mostly built; only the commands are missing.
- **A published JSON Schema**, generated from the Go declarations `yolo check` validates against,
  gives editor completion and hover text through Taplo, and `config-ref` per section from the same
  descriptions.
- **"Did you mean"** for an unknown key: the nearest valid key, or an old key's respelling.
- **Machine tags without include files.** `[when.tag.work] packs = ["luna-worker"]`, with the tag
  set per machine in a companion file. Idea; includes keep working.

## 11. Non-goals

- **No custom format.** The maintainer: *"I'm a little hesitant for a full custom file format
  because that's a lot of weight to carry."* Nothing here needs one; TOML and JSONC each carry the
  shape, and a splice works on either.
- **No manifest as code.** [OQ-M3](manifest-language.md#OQ-M3) stays in its own doc.
- **No change to what the approval gate approves**, or to which scope may hold which key; only the
  spelling changes.
- **No aliases, ever.** An old spelling is refused naming the new one.
- **No automatic migration.** A launch offers it; a person answers.

## 12. What this doc takes over

- [OQ-M1](manifest-language.md#OQ-M1) and [OQ-M2](manifest-language.md#OQ-M2) by
  [OQ-RC5](#OQ-RC5) and [OQ-RC2](#OQ-RC2).
- [OQ-PC1](pack-conventions.md#OQ-PC1) to [OQ-PC3](pack-conventions.md#OQ-PC3) by
  [OQ-RC5](#OQ-RC5)'s `home/` folder.
- [OQ-PP1](providers-and-profiles-redesign.md#OQ-PP1) by [OQ-RC3](#OQ-RC3).
- [OQ-BB23](boundary-broker.md#OQ-BB23) by [§4.2](#42-what-a-jail-may-do-with-your-logins).

**One ruling this reverses on purpose.** [MN-D1](manifest-language.md#MN-D1)
(2026-09-30) ruled that the user and workspace config keep JSONC whatever the manifest syntax
becomes, and that moving them "would be a proposal of its own". This is that proposal, and
[OQ-RC2](#OQ-RC2) is where it is ruled on again.

**One ruling this amends.** [OQ-BB12](boundary-broker.md#15-decision-ledger) is read as being
about side effects and messes; [OQ-RC9](#OQ-RC9). [PP-D10](providers-and-profiles-redesign.md#PP-D10),
the `profile` key that mirrors `-p`, stands: an earlier draft renamed it, and the maintainer's
review of 2026-10-24 kept the name ([RC-D1](#decision-ledger)).

## 13. Open questions

1. 💬 <a id="OQ-RC1"></a>**OQ-RC1: Which of the six shape changes go in?**

   A colleague copies the maintainer's config to get Bedrock. They find `bedrock` three times,
   `"claude": null`, and an `aws-auth` that appears in `packs` and again under `loopholes` with
   `enabled: true`. They delete the `loopholes` block as a duplicate, and their jail starts with no
   AWS credentials and no message they can connect to it. Each lever removes one such trap, and
   each can be taken alone:

   - **L1:** `yolo init` writes a short file, and `yolo check` flags a written default.
   - **L2:** a model's key is its id, and the maker follows from a Bedrock id.
   - **L3:** facts every model of a provider shares are written once, in `every_model`.
   - **L4:** selecting a pack is its switch; a login-serving pack is on when its settings table is
     in your config. This amends four loophole rules, each keeping its reason.
   - **L5:** the renames (`profile` excepted; it keeps its name), and tables where strings carried two meanings.
   - **L6:** a non-shipped pack is named by its source, `dotfiles/matt`, never by a bare name.

   <!-- vantage: question id=OQ-RC1 leaning="All six. L4 is the only one that changes ruled rules, and selecting a pack by name is the user saying so, which is what those rules protect." -->

   _Leaning:_ All six. L4 is the only one that changes ruled rules, and selecting a pack by name is
   the user saying so, which is what those rules protect.

   **Answer:**
   > _(empty — fill in when decided)_

2. 💬 <a id="OQ-RC2"></a>**OQ-RC2: TOML, or keep JSONC in the new shape?**

   The maintainer opens his config to add a model. In JSONC each model row is a quoted key, a
   brace, and quoted field names; in TOML the same row is one line. Either way the reshape takes
   the file from 178 lines to under 60. Both formats are already in the tree, and a splice edits
   either. A ruling of 2026-09-30 said the configs keep JSONC unless a proposal says otherwise;
   this is that proposal.

   - **A — TOML** for every file a person writes: configs and pack manifests. No new library for
     reading; a position scanner for the splice. Files only yolo reads stay JSON.
   - **B — JSONC, reshaped.** No format change, and the splice is nearest to built
     (`json5.Locate` exists). Hand-laid files read nearly as well; a formatter expands them.
   - **C — YAML.** The least punctuation, but `3.10` reads as 3.1, indentation carries meaning, and
     the vendored library is frozen upstream.

   <!-- vantage: question id=OQ-RC2 leaning="A, TOML: low noise, no value misreads, familiar from mise.toml and Cargo, already vendored; the flatter shape avoids its one weakness." -->

   _Leaning:_ A, TOML: low noise, no value misreads, familiar from `mise.toml` and Cargo, already
   vendored; the flatter shape avoids its one weakness.

   **Answer:**
   > _(empty — fill in when decided)_

3. 💬 <a id="OQ-RC3"></a>**OQ-RC3: Should you be able to pick a provider by its own name, without a profile?**

   The maintainer runs every agent on Bedrock, starting on Sol. To say that today he writes a
   provider `bedrock`, a profile `bedrock` (provider `bedrock`, model Sol), and `"*": "bedrock"`.
   Seven of the nine shipped profiles are named after the provider they pick. The providers doc
   leans toward keeping profiles as the only selectable thing, because a provider has no place for
   the starting model; option B gives it one.

   - **A — Keep profiles as the only thing you select.** Explain the two nouns better. The three
     `bedrock`s stay.
   - **B — A provider is selectable by name and starts on its own `model`.** A profile exists only
     for a second setup of one provider (`bedrock-opus`, or `bedrock-bridge` with its `via`). Names
     share one list, and a clash is refused. Same-named shipped profiles are deleted.
   - **C — Only providers, with options on the flag** (`-p bedrock:model=opus`). The flag gets
     longer, and the `via` profiles need a new home.

   <!-- vantage: question id=OQ-RC3 leaning="B: the common case is one name, and the rare case keeps the profile that already works." -->

   _Leaning:_ B: the common case is one name, and the rare case keeps the profile that already
   works.

   **Answer:**
   > _(empty — fill in when decided)_

4. 💬 🤷 <a id="OQ-RC4"></a>**OQ-RC4: What should the section for "what a jail may do with your logins" be called?**

   The maintainer reads `brokered.git.grants` and says *"I don't understand 'brokered'."* The
   section holds git fetch and push, GitHub reads and writes, and the AWS login `aws-auth` serves:
   everything a jail does using a host credential it never sees. `as_you` was the first draft's
   name, and the maintainer rejected it on 2026-10-24 ([RC-D2](#decision-ledger)); the body uses
   `allow` as the placeholder until this is ruled.

   - **A — `allow`**: `[allow.git] push = ["main"]`. Reads as a sentence, and is the verb the
     command already uses (`yolo allow git push main`).
   - **B — `logins`**: `[logins.git] fetch = true`. Names what is lent, not the act.
   - **C — `host_logins`**: the same, saying whose.
   - **D — `on_your_behalf`**: the plainest English, and the longest to type.

   <!-- vantage: question id=OQ-RC4 leaning="A, allow: it reads as a sentence with what follows, and the command verb and the section are then one word." -->

   _Leaning:_ A, `allow`: it reads as a sentence with what follows, and the command verb and the
   section are then one word.

   **Answer:**
   > _(empty — fill in when decided)_

5. 💬 <a id="OQ-RC5"></a>**OQ-RC5: Should pack manifests change in the same breaking change?**

   The maintainer opens `matt/pack.json` to see what the pack does. It is 860 lines. 271 of them
   are 39 `files` entries, 32 of them plain copies to the same path, and 425 are pi's settings
   written inline. In `agy-capture`, deleting one restated flag would make agy prompt for every
   tool. `meta-muse` writes the same model facts six times in pi's own format. A yolo of 0.11.1 or
   earlier refuses any new manifest field outright, so a new manifest shape is a new file name
   however it is done.

   - **A — Yes, together.** All six pack levers, with `yolo pack migrate` moving the files. One
     migration for configs and packs. Any lever can be vetoed alone.
   - **B — Configs first, packs next.** Two migrations. The configs land sooner.
   - **C — Packs stay as they are.** The 860-line manifest stays.

   <!-- vantage: question id=OQ-RC5 leaning="A: one break, and the migrate command does the moving; packs are the bigger files and the same rules apply." -->

   _Leaning:_ A: one break, and the migrate command does the moving; packs are the bigger files
   and the same rules apply.

   **Answer:**
   > _(empty — fill in when decided)_

6. 💬 <a id="OQ-RC6"></a>**OQ-RC6: What happens on a machine still running an older yolo?**

   A colleague migrates their config with the new yolo, then runs a jail from an older yolo
   installed elsewhere, or the maintainer's Mac is a release behind. The older yolo looks for
   `config.jsonc`, finds none, and launches a jail with no packs, saying only that it has no
   coding agent; a migrated local pack it stages as if it had no manifest.

   - **A — Migrate keeps the old file** beside the new one, marked as superseded. An older yolo
     still works, and drifts. The new yolo refuses while both exist unless the old one carries the
     marker.
   - **B — Migrate removes the old file.** An older yolo starts empty and says it has no packs.
   - **C — Ship a release that knows the new names first**, then the migration. An old-enough yolo
     refuses, naming the upgrade. One extra release.

   <!-- vantage: question id=OQ-RC6 leaning="C: one release that recognizes the new names and refuses with upgrade yolo, then the migrating release; no silently empty jail, no drifting copy." -->

   _Leaning:_ C: one release that recognizes the new names and refuses with "upgrade yolo", then the
   migrating release; no silently empty jail, no drifting copy.

   **Answer:**
   > _(empty — fill in when decided)_

7. 💬 <a id="OQ-RC7"></a>**OQ-RC7: How does a TOML file say "remove" or "not this one", with no `null`?**

   The maintainer's user config adds the `tavily` MCP server. In one project they don't want it,
   so that project's config writes `"mcp_servers": {"tavily": null}`. The same `null` keeps claude
   on its own login, unsets `AWS_PROFILE`, clears a `repos` list, removes a model fact, and
   disables a lower layer's value in an include. TOML has no `null`, so each needs a spelling a
   cold reader understands, and a mechanical `unset` command needs one it can write.

   - **A — A word where each thing lives.** `off = true` on anything with a name (`[mcp_servers.tavily]
     off = true`, a pack, a provider), `unset = ["AWS_PROFILE"]` under `env`, `false` in
     `profile`, and an empty list to clear a list. Four spellings, each saying what it does.
   - **B — One reserved string, `"off"`,** valid wherever a value may be removed. One rule, but
     `"off"` can then never be a real value, and it is a reserved string, which [RC-D6](#decision-ledger) rules out.
   - **C — One top-level list of removals:** `remove = ["mcp_servers.tavily", "env.AWS_PROFILE"]`. Every
     removal in one place, far from the thing it removes.

   <!-- vantage: question id=OQ-RC7 leaning="A: a word at the place, each saying what it does; off = true is L4's word, and an unset command has one thing to write per case." -->

   _Leaning:_ A: a word at the place, each saying what it does; `off = true` is L4's word, and an
   unset command has one thing to write per case.

   **Answer:**
   > _(empty — fill in when decided)_

8. 💬 <a id="OQ-RC8"></a>**OQ-RC8: What happens to a pack fetched from someone else's repository that still has an old `pack.json`?**

   A colleague publishes a pack on GitHub, and the maintainer lists it as a git pack. The
   maintainer upgrades yolo to the release that reads only `pack.toml`. The colleague has not
   migrated. Today the one fetched pack in the maintainer's list, `matt-craft`, has no manifest at
   all, so it is not affected. A colleague's pack would be.

   - **A — yolo keeps an old-manifest reader for fetched packs only,** converting in memory.
     Nothing breaks, and yolo carries two readers.
   - **B — Refuse, naming the pack's repository and the two ways forward:** ask its author to run
     `yolo pack migrate`, or fork it and run it yourself. One reader.
   - **C — B, after a grace period of one release in which A applies with a warning.**

   <!-- vantage: question id=OQ-RC8 leaning="B: one reader; no fetched pack in use today has a manifest, and the refusal names both ways forward." -->

   _Leaning:_ B: one reader; no fetched pack in use today has a manifest, and the refusal names
   both ways forward.

   **Answer:**
   > _(empty — fill in when decided)_

9. 💬 <a id="OQ-RC9"></a>**OQ-RC9: May yolo write your config file when you ask it to, and how?**

   On 2026-10-01 you ruled that yolo never edits the user config: *"it always leaves that a
   mess."* Today a colleague is told, in about sixty places, to add `{"kind": "read", "repo":
   "*"}` to a list in their config, then run `yolo config apply`. With the ruling read literally,
   the best yolo can do is print the lines. With it read as being about the mess, `yolo allow git
   fetch` can change one hunk of their file and leave every other byte alone.

   - **A — Splice on request.** A command you typed edits the file it names, changing only the
     bytes of one value, and prints the hunk. A side effect never does; that stays in a companion
     file, as the per-workspace switch does today. Removals work.
   - **B — Companion file only.** Your file is never written. Commands add to a yolo-owned file
     beside it, written whole in canonical layout. Removing something you wrote stays by hand,
     because lists merge by union.
   - **C — Print the edit.** The command shows the lines and the file; you paste. Today, with
     better text.

   <!-- vantage: question id=OQ-RC9 leaning="A: the ruling's complaint was a mess left by a side effect; a one-hunk edit you asked for is neither, and B cannot remove anything." -->

   _Leaning:_ A: the ruling's complaint was a mess left by a side effect; a one-hunk edit you asked
   for is neither, and B cannot remove anything.

   **Answer:**
   > _(empty — fill in when decided)_

10. 💬 <a id="OQ-RC10"></a>**OQ-RC10: Named verbs, one generic `set`, or both?**

    An agent in a jail reads the briefing's "edit `yolo-jail.jsonc` (`packages` / `resources`)",
    opens the file, and adds `"just"` to a list, then runs `yolo check`. A person on the host
    reads "add `{"kind": "read", "repo": "*"}` to `brokered.git.grants`" and pastes it under the
    wrong key. Either could type one line instead. The question is which line the sixty messages
    print and which a person remembers.

    - **A — Named verbs**: `yolo pack add matt`, `yolo install nix just`, `yolo allow git fetch`,
      `yolo profile bedrock`. Each names the decision, as its section does. About ten verbs to learn,
      and a key with no verb has no command.
    - **B — One generic verb**: `yolo config set allow.git.fetch true`, `yolo config add packs
      matt`, `yolo config unset mcp_servers.tavily`. Every key has a command, and a message prints it from
      the key it knows. Longer to type, and the path is the thing the shape work made readable.
    - **C — Both**: B underneath, A as sugar for the common decisions. Messages print B; people
      type A.

    <!-- vantage: question id=OQ-RC10 leaning="C: a message knows the key, a person knows the decision, and the sugar costs little once the generic verb exists." -->

    _Leaning:_ C: a message knows the key, a person knows the decision, and the sugar costs little
    once the generic verb exists.

    **Answer:**
    > _(empty — fill in when decided)_

11. 💬 <a id="OQ-RC11"></a>**OQ-RC11: Is the file name the version, or a key inside the file?**

    You said: *"if we change file names here that would allow us a pretty easy transition plan. We
    can version it. I suppose we can put a version field in the JSON that we read first."* A
    colleague on last month's yolo pulls dotfiles that were migrated on another machine. With a new
    name, their yolo finds no `config.jsonc` and starts a jail with no packs, unless it is the
    release that knows the new name and says "upgrade". With a version key in a same-named file,
    their yolo reads it and fails on the first unknown key, naming the key.

    - **A — The file name.** `config.toml`, `yolo-jail.toml`, `pack.toml`. No key. A format change
      has to change the name anyway. The release before the change teaches old yolos the name.
    - **B — A version key**, `yolo = 2`, first in the file, same name. Only possible if the format
      stays JSONC. An old yolo's refusal names a key, not the upgrade.
    - **C — Both.** The name now, and a key reserved for the next shape change inside TOML.
      Compose's version key is the warning: a key nobody needs gets copied without meaning.

    <!-- vantage: question id=OQ-RC11 leaning="A: the name is the version, as ESLint did; a key is added the day a same-format shape change needs one, and not before." -->

    _Leaning:_ A: the name is the version, as ESLint did; a key is added the day a same-format shape
    change needs one, and not before.

    **Answer:**
    > _(empty — fill in when decided)_

12. 💬 <a id="OQ-RC12"></a>**OQ-RC12: What does `yolo config` mean once it edits your config?**

    Today `yolo config ls`, `render`, `diff`, `reset`, `promote` and `capture` are about the agent
    settings files yolo composes (`~/.claude/settings.json` and the like), while `yolo config
    apply`, `drift` and `dump` are about yolo's own config. A person who types `yolo config set`
    expects the second meaning; the help page shows both.

    - **A — Move the six composition commands** to a verb of their own (`yolo surface ls`), and
      `yolo config` is yolo's config: `set`, `add`, `unset`, `migrate`, `why`, `show`, `apply`,
      `drift`, `dump`. Six retired spellings print their replacement.
    - **B — Put the new verbs elsewhere** (`yolo set`, `yolo migrate`) and leave `yolo config` as
      it is. Nothing moves; the word `config` keeps meaning the agents' files.
    - **C — Both meanings under one verb**, grouped in the help text. Nothing moves and nothing is
      retired; the help page has two sections.

    <!-- vantage: question id=OQ-RC12 leaning="A: config should mean the user's config, and six retired spellings that print their replacement cost one release of friction." -->

    _Leaning:_ A: `config` should mean the user's config, and six retired spellings that print
    their replacement cost one release of friction.

    **Answer:**
    > _(empty — fill in when decided)_

13. 💬 <a id="OQ-RC13"></a>**OQ-RC13: Which of the eight conventions go in?**

    A colleague sets up a work machine. They list `bedrock`, `aws-auth`, `git` and `github` in
    `packs`, write `[allow.aws-auth]` with a profile, `[providers.bedrock]` with a region, and
    `[profile] default = "bedrock"`. Under the eight conventions the four names leave `packs`
    (writing the section selected each pack), the selection line goes (one provider is
    everyone's), and `fetch = true` under `[allow.git]` goes (the table is the grant). `yolo
    config show` prints each supplied line with `# by convention`. The line they did not write is
    one they cannot get wrong, and also one they cannot see without asking.

    - **A — All eight.** The shortest files, and every supplied line is shown on request.
    - **B — All but C3 and C8**, the two where a declaration acts at a distance. `packs` stays the
      one list of what is on; the selection stays a written line.
    - **C — None.** Every line is written, as today.

    <!-- vantage: question id=OQ-RC13 leaning="B: packs stays the one list of what is on and a heading never looks a pack up; the other six read only what the user already wrote." -->

    _Leaning:_ B: `packs` stays the one list of what is on and a heading never looks a pack up; the
    other six read only what the user already wrote.

    **Answer:**
    > _(empty — fill in when decided)_

14. 💬 🤷 <a id="OQ-RC14"></a>**OQ-RC14: What are the selection and the declarations called, if not `profile` and `profiles`?**

    Two keys differ by one letter and mean different things: `profile` maps each agent to a setup
    (`claude = false`, `default = "bedrock"`), and `profiles.<name>` describes a setup as a
    provider plus options. You ruled the first name on 2026-09-29 so it mirrors `-p`, and on
    2026-10-24 called the pair *"still kinda awful."* Under option B of the provider question, a
    setup is usually just a provider, and a `profiles` entry is a variant of one.

    - **A — `run_on` and `providers.<p>.variants.<v>`.** `[run_on] default = "bedrock"`, `claude
      = false`; `[providers.bedrock.variants.opus] model = "…"`, selected as `bedrock/opus`.
      The word "profile" disappears from the config; `-p` keeps its letter.
    - **B — `agents` and `setups.<name>`.** `[agents] claude = false` reads best of all, but
      `agents` is a retired key that today errors with "name the pack in `packs`", so a migrated
      file would meet the old refusal on an old yolo.
    - **C — Keep `profile`, rename the declarations `setups.<name>`.** The selection keeps the
      ruling's word; only the one-letter pair goes.

    <!-- vantage: question id=OQ-RC14 leaning="A: run_on reads as a sentence per agent, a variant is what a same-provider profile is, and the pair goes." -->

    _Leaning:_ A: `run_on` reads as a sentence per agent, a variant is what a same-provider profile
    is, and the pair goes.

    **Answer:**
    > _(empty — fill in when decided)_

15. 💬 <a id="OQ-RC15"></a>**OQ-RC15: How is a pack that yolo does not ship named in your config?**

    A colleague publishes a pack called `tools`, and so does someone on another team. You list
    both. Today each is named by the last segment of its address, so the second is refused with
    "give one an explicit `name`", and you rename it. You called that *"like Python imports, but
    worse"*. Homebrew and Claude Code never reach that step: a built-in is bare and anything else
    carries its source, `user/repo/name` or `plugin@marketplace`.

    - **A — Qualified by source.** Shipped packs are bare; yours are `dotfiles/matt` and
      `local/x`, a folder named once under `[pack_folders]`; a git pack is `acme/tools` from its
      URL. No bare lookup outside the shipped set, and no rename, ever. One token more per pack.
    - **B — Bare names, refusal on collision.** `"matt"` is found in the shipped set or a folder;
      a name in both is refused naming both, and you rename one. Shortest, and the step you
      dislike stays for the rare collision.
    - **C — Paths, as today.** `"~/.dotfiles/yolo-packs/matt"` and the full git URL. No names to
      design, and the longest lines in the file.

    <!-- vantage: question id=OQ-RC15 leaning="A: the collision cannot happen, so there is nothing to rename; it is Homebrew's model, and the shipped set is the only bare namespace, as core is." -->

    _Leaning:_ A: the collision cannot happen, so there is nothing to rename; it is Homebrew's
    model, and the shipped set is the only bare namespace, as core is.

    **Answer:**
    > _(empty — fill in when decided)_

16. 💬 <a id="OQ-RC16"></a>**OQ-RC16: May your config let a jail fetch from any repository, not only the project's own?**

    You write `[allow.git] fetch = true`. Today that reaches the project's remotes as approved at
    launch, nothing else; an agent that clones a second repository to read it is refused with the
    scope. You read the line as "any Git repo is allowed to be fetched by the jail, which would be
    nice if that's true." Widening it is a change to the broker's per-workspace scope, which the
    boundary-broker doc rules, and a fetch is read-only: what it risks is a private repository's
    contents reaching a jail whose project never named it.

    - **A — A list of patterns in the user config widens it.** `fetch = ["github.com/mschulkind-oss/*"]`,
      and `fetch = ["*"]` is any repository the host login can reach; `true` stays the project's
      own. Each pattern is one line in the approval prompt, once per workspace.
    - **B — The project's own remotes only**, as today. An agent that needs another repository
      adds it to the project's `repos`, which `yolo config apply` approves.
    - **C — Any repository, always,** when `fetch = true`. The simplest reading of the line, and
      no per-workspace approval of remotes for fetch at all.

    <!-- vantage: question id=OQ-RC16 leaning="A: the user config is trusted and a read is cheap to widen, but a line that names what it opens beats one that opens everything." -->

    _Leaning:_ A: the user config is trusted and a read is cheap to widen, but a line that names
    what it opens beats one that opens everything.

    **Answer:**
    > _(empty — fill in when decided)_

## Decision Ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| <a id="RC-D1"></a>RC-D1 | **The `profile` key keeps its name.** Review comment on the draft's `use`: *"awful name … isn't this a profile?"* It is, and PP-D10's reason (the key mirrors `-p`) stands. L5 no longer renames it | 2026-10-24 | [§4.3](#43-models-one-name-for-the-common-case) | — |
| <a id="RC-D7"></a>RC-D7 | **A section without its pack is a nudge, not a selection.** Review follow-up: *"perhaps we can just give some help if you allow something without the pack, maybe we can infer which pack you would need and give you a happy path nudge?"* `yolo check` names the pack and offers `yolo pack add <pack>` with a `y`; the reverse case offers the setting | 2026-10-25 | [§4.1](#41-the-levers-each-one-syntax-free) | — |
| <a id="RC-D6"></a>RC-D6 | **No value is a reserved string.** Review comment on `"when a project asks"`: *"some weird string like this is just weird for a config file."* A value is `true`, `false`, a number, a name or a list of names; `push = true` at user scope is the ceiling, `new_branches = true` replaces `"new branches"` | 2026-10-25 | [§4.2](#42-what-a-jail-may-do-with-your-logins) | — |
| <a id="RC-D5"></a>RC-D5 | **`false`, not `"own login"`,** for an agent yolo gives no setup. Review comment: *"don't like"* | 2026-10-25 | [§4.3](#43-models-one-name-for-the-common-case) | — |
| <a id="RC-D4"></a>RC-D4 | **`default`, not `everyone`,** for today's `"*"`. Review comment: *"don't like the name"* | 2026-10-25 | [§4.3](#43-models-one-name-for-the-common-case) | — |
| <a id="RC-D3"></a>RC-D3 | **Plural for a collection the user fills, singular for one thing.** Review comment: *"why is this plural and mcp singular?"* `mcp.<name>` becomes `mcp_servers.<name>` (today's name), `[[mount]]` becomes `[[mounts]]`; all-singular would collide the selection with the declarations | 2026-10-24 | [§4](#4-the-proposed-vocabulary) | — |
| <a id="RC-D2"></a>RC-D2 | **Not `as_you`.** Review comment: *"I hate the name 'as_you'."* The section's name stays open as [OQ-RC4](#OQ-RC4) with `allow` as the placeholder | 2026-10-24 | [§4.2](#42-what-a-jail-may-do-with-your-logins) | — |

## Appendix A: the maintainer's config, whole

In the proposed shape, TOML, as the four files it is on the host ([§2.1](#21-it-is-four-files-not-one)).
Together they replace the 178 lines of [§2](#2-where-todays-config-misleads). The personal file is
reconstructed from what the packs say about it and is marked where it is a guess.

`~/.config/yolo-jail/config.toml`, every machine:

```toml
# Machine-wide settings. Every key: `yolo config-ref`.
pack_folders = { dotfiles = "~/.dotfiles/yolo-packs" }   # names the folder once
packs = [
  "pi", "dotfiles/pi-fork", "codex", "journal", "host-processes", "serial", "guardrails",
  "git", "github", "chrome-devtools",
  "dotfiles/matt", "dotfiles/matt-fzf", "dotfiles/matt-mods", "dotfiles/matt-agentwork",
  { git = "https://github.com/mschulkind-oss/matt-craft", ref = "main" },   # named mschulkind-oss/matt-craft
]
include_if_found = ["overrides.toml", "../../.dotfiles/tag-work/config/yolo-jail/work.toml",
                    "../../.dotfiles/tag-personal/config/yolo-jail/personal.toml"]

[install.mise]
neovim = "nightly"
"pipx:swarf" = "latest"
```

`~/.config/yolo-jail/overrides.toml`:

```toml
[mcp_servers.tavily]
command = ["npx", "-y", "tavily-mcp@latest"]
env = { TAVILY_API_KEY = "${TAVILY_API_KEY}" }   # passed through literally; the agent expands it

[profile]
default = "bedrock"
claude = false
```

`~/.dotfiles/tag-work/config/yolo-jail/work.toml`, work machines:

```toml
packs = ["bedrock", "aws-auth", "copilot", "claude", "dotfiles/luna-worker", "dotfiles/claude-bedrock-global"]

# What jails may do with your own logins. Each project asks for what it needs.
[allow.git]
fetch = true
push = true                       # a project may ask, branch by branch

[allow.github]
read = true

[allow.aws-auth]                 # writing this turns the service on
profile = "hssandbox-BedrockInference"
unnarrowed = true

[providers.bedrock]
region = "us-east-1"
model = "global.openai.gpt-6.1-sol"

[providers.bedrock.every_model]   # shared by each model below unless it says otherwise
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

`…/tag-personal/config/yolo-jail/personal.toml`, personal machines (reconstructed; the
OpenRouter rows are a guess at shape, not content):

```toml
packs = ["agy-capture", "meta-muse", "workspace-questions"]

[profile]
default = "or"

[profiles.or]
provider = "openrouter"
model = "deepseek/deepseek-v4.1-flash-1017:floor"

[providers.openrouter.models]
"deepseek/deepseek-v4.1-flash-1017:floor@200k" = { twin_of = "deepseek/deepseek-v4.1-flash-1017:floor", context = 204_800 }
"deepseek/deepseek-v4.1-flash:nitro@200k"      = { twin_of = "deepseek/deepseek-v4.1-flash:nitro",      context = 204_800 }
```

Include paths are relative to the including file, as today; yolo refuses one that starts with `~`
or `/` (`internal/config/load.go`). Not yet checked: whether pi's own model catalog already carries
the Fable, Opus and Haiku facts; if it does, those three rows shrink to their names.

This repository's `yolo-jail.toml`, replacing 133 lines:

```toml
# This project's jail. Every key: `yolo config-ref`.
[install]
nix = ["just"]                    # runs this repo's Justfile recipes
mise = { lua = "5.4.9" }          # syntax-checks the packs' derive.lua files

[allow.git]
push = ["main"]
```

And its `yolo-jail.local.toml`, uncommitted, unchanged in meaning:

```toml
[[mounts]]
host = "~/.local/share/yolo-jail/logs"
at = "/ctx/host-yolo-logs"
optional = true                   # a backend that cannot deliver it skips it
```

### A.2 The same, with the conventions

Under the leanings: the six conventions of [OQ-RC13](#OQ-RC13)'s option B, with
`~/.config/yolo-jail/packs` a symlink to `~/.dotfiles/yolo-packs` (so `local/matt`) and the three
included files living in `config.d/` (the work and personal ones as dotfile-tag symlinks that
exist only on their machines); [OQ-RC14](#OQ-RC14)'s `run_on`; [OQ-RC15](#OQ-RC15)'s qualified
names. `packs` stays the one list of what is on, and the selection stays a written line.

`~/.config/yolo-jail/config.toml`:

```toml
packs = [
  "pi", "local/pi-fork", "codex", "journal", "host-processes", "serial", "guardrails",
  "git", "github", "chrome-devtools",
  "local/matt", "local/matt-fzf", "local/matt-mods", "local/matt-agentwork",
  { git = "https://github.com/mschulkind-oss/matt-craft", ref = "main" },
]

[install.mise]
neovim = "nightly"
"pipx:swarf" = "latest"
```

`config.d/overrides.toml`:

```toml
[mcp_servers.tavily]
command = ["npx", "-y", "tavily-mcp@latest"]
env = { TAVILY_API_KEY = "${TAVILY_API_KEY}" }

[run_on]
claude = false
```

`config.d/work.toml`, work machines:

```toml
packs = ["bedrock", "aws-auth", "copilot", "claude", "local/luna-worker", "local/claude-bedrock-global"]

[run_on]
default = "bedrock"

[allow.git]                       # fetch, by the table's presence
push = true                       # a project may ask, branch by branch

[allow.github]                    # read, by the table's presence

[allow.aws-auth]
profile = "hssandbox-BedrockInference"
unnarrowed = true

[providers.bedrock]
region = "us-east-1"
model = "global.openai.gpt-6.1-sol"

[providers.bedrock.every_model]
accepts = ["text", "image"]
max_tokens = 128_000
reasoning = true

[providers.bedrock.models]
"global.anthropic.claude-fable-5-1" = { name = "Fable 5.1" }     # the rest from the catalog (C7)
"global.anthropic.claude-opus-5-5"  = { name = "Opus 5.5" }
"global.anthropic.claude-haiku-5-5" = { name = "Haiku 5.5" }
"global.openai.gpt-6-astra"         = { name = "Astra 6",   context = 272_000, price = { input = 10, output = 50, cache_read = 1,    cache_write = 12.5 } }
"global.openai.gpt-6-luna"          = { name = "Luna 6",    context = 272_000, price = { input = 0.1, output = 0.5, cache_read = 0.01, cache_write = 0.125 } }
"global.openai.gpt-6.1-sol"         = { name = "Sol 6.1",   context = 272_000, price = { input = 2,  output = 10, cache_read = 0.1,  cache_write = 2.5 } }
```

`config.d/personal.toml`, personal machines (reconstructed):

```toml
packs = ["openrouter", "local/agy-capture", "local/meta-muse", "local/workspace-questions"]

[run_on]
default = "openrouter"

[providers.openrouter]
model = "deepseek/deepseek-v4.1-flash-1017:floor"

[providers.openrouter.models]
"deepseek/deepseek-v4.1-flash-1017:floor@200k" = { twin_of = "deepseek/deepseek-v4.1-flash-1017:floor", context = 204_800 }
"deepseek/deepseek-v4.1-flash:nitro@200k"      = { twin_of = "deepseek/deepseek-v4.1-flash:nitro",      context = 204_800 }
```

Forty-five non-blank lines across four files, against 178 today, with no include line and no pack-folder line.
C7's three short rows wait on the catalog check noted above.

## Appendix B: more configs, today and proposed

### B.1 A colleague on a Claude subscription, nothing else

Today: `{"packs": ["claude"]}`. Proposed: `packs = ["claude"]`. What the colleague meets next is
the 121-line template in their first project, which L1 removes.

### B.2 A work machine: pi on Bedrock through AWS SSO, claude on its own login

Today:

```jsonc
{
  "packs": ["claude", "pi", "bedrock", "aws-auth"],
  "loopholes": {"aws-auth": {"enabled": true, "settings": {"profile": "my-sso-profile"}}},
  "profile": {"*": "bedrock", "claude": null}
}
```

Proposed:

```toml
packs = ["claude", "pi", "bedrock", "aws-auth"]

[allow.aws-auth]                 # serves your AWS SSO login to the jail; writing this turns it on
profile = "my-sso-profile"

[profile]
default = "bedrock"
claude = false
```

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

[profile]
pi = ["zai", "openrouter"]        # both available in pi, switchable there

[env]
files = ["~/.config/yolo-jail/secrets.env"]   # the API keys these providers read
```

### B.4 A team project

Today, `yolo-jail.jsonc`:

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

[[mounts]]                          # read-only unless `write = true`, which only your user config may say
host = "~/code/acme-shared"

[machine]
gpu = true                         # warns and starts without one on a machine that has none
resources = { memory = "16g", cpus = 8 }
ports = [{ host = 8000, jail = 8000 }]       # the host reaches the jail's :8000
forward = [{ jail = 5432, host = 5432 }]     # the jail reaches the host's postgres at localhost:5432

[allow.github]
repos = ["acme/shared"]            # beyond this repository's own remotes

[allow.git]
new_branches = true                # branches of its own, never main; each is approved at launch
```

### B.5 The project's uncommitted local file

```toml
# This machine only; not committed.
[mcp_servers.postgres]
command = ["npx", "-y", "@modelcontextprotocol/server-postgres", "postgresql://localhost/acme"]

[mcp_servers.tavily]
off = true                         # the user config adds it; not wanted here

[machine]
resources = { memory = "32g" }
```

### B.6 Work and personal machines from one dotfiles repository

Today the user config includes a file that exists only on work machines, through
`include_if_found` with a path relative to the including file. That keeps working
([Appendix A](#appendix-a-the-maintainers-config-whole)). The idea in
[§10](#10-other-problems-this-could-solve) puts the difference in the file a reader opens:

```toml
[when.tag.work]                    # this machine's tags are set once, in a companion file
packs = ["luna-worker", "claude-bedrock-global"]
```

### B.7 A small personal pack: `luna-worker`

Today, 23 lines of manifest for one setting. Under P2 and P3 it is five lines of TOML:

```toml
name = "luna-worker"
description = "Work machines: pi's worker subagent runs on GPT-6 Luna when the session is on Bedrock."

[[overlay]]
surface = "pi/settings"
set.managed.subagents.agentOverridesByProvider.amazon-bedrock.worker.model = "amazon-bedrock/global.openai.gpt-6-luna"
```

Under C6 it is a folder with no manifest and one file, `overlays/pi-settings.jsonc`, in pi's own
format:

```jsonc
// Work machines: pi's worker subagent runs on GPT-6 Luna when the session is on Bedrock.
{"subagents": {"agentOverridesByProvider": {"amazon-bedrock": {"worker": {"model": "amazon-bedrock/global.openai.gpt-6-luna"}}}}}
```

The folder's name is the pack's name and the file's name is the surface. `coordinator-reads-blocked`
is the same shape, `workspace-questions` is a `briefing/` folder and nothing else, and
`claude-bedrock-global` is `overlays/claude-settings@bedrock.jsonc`, the `@bedrock` being the
profile gate its manifest carries today.

### B.8 `meta-muse`'s models under P5

Today, 199 lines of pi's `models.json` format plus three compaction rows. Proposed, with the facts
yolo's model shape lacks today marked as new:

```toml
[providers.meta.every_model]
api = "openai-responses"           # new fact: the wire API
reasoning = true
accepts = ["text", "image"]
thinking = ["minimal", "low", "medium", "high", "xhigh", "max"]   # new fact
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

`twin_of` takes every fact of the named row and sends its id as the twin's `wire_id`. 21 lines
replace 199 plus the compaction rows, and each agent's render writes its own file from them.

## Appendix C: sessions

Each session is what a person or an agent would see, under the leanings above. Spellings follow
[OQ-RC10](#OQ-RC10)'s option C and [OQ-RC12](#OQ-RC12)'s option A, and change with them.

### C.1 Adding a pack, on the host

```console
$ yolo pack add matt
~/.config/yolo-jail/config.toml
@@ -4,4 +4,5 @@
   "git", "github", "chrome-devtools",
-  "matt-fzf", "matt-mods", "matt-agentwork",
+  "matt-fzf", "matt-mods", "matt-agentwork",
+  "matt",
   { git = "https://github.com/mschulkind-oss/matt-craft", ref = "main" },
Added "matt" (found at ~/.dotfiles/yolo-packs/matt). It is read at the next launch.
```

One hunk; the list's own layout decided where the item went. The same command for a pack already
listed: `"matt" is already in packs (config.toml:7). Nothing written.`

### C.2 Removing one, and what happens to its comment

```console
$ yolo pack remove journal
~/.config/yolo-jail/config.toml
@@ -2,6 +2,5 @@
 packs = [
-  # journal: read the host's systemd journal from inside the jail
-  "pi", "pi-fork", "codex", "journal", "host-processes", "serial", "guardrails",
+  "pi", "pi-fork", "codex", "host-processes", "serial", "guardrails",
Removed "journal". A comment above it was removed with it:
  # journal: read the host's systemd journal from inside the jail
```

A comment whose key is gone is printed, never dropped silently. The same line is what `migrate`
keeps in place as `# (was on: …)`.

### C.3 An agent in a jail asks for a tool

```console
$ yolo config add install.nix just
/workspace/yolo-jail.toml
@@ -2,3 +2,3 @@
 [install]
-nix = ["postgresql"]
+nix = ["postgresql", "just"]
Added. This is a project-config change: run `yolo check --no-build`, then ask the human to run
`yolo config apply` on the host, in this workspace. A `nix` change also rebuilds the image.
```

The briefing's paragraph on editing `yolo-jail.jsonc` becomes this one command. The gate after it is
unchanged.

### C.4 A user-scope key from inside a jail

```console
$ yolo config set allow.git.fetch true
allow.git.fetch is a user-config key, and a jail sees only a generated copy of your user config.
Run it on the host:
    yolo host-run -C /home/matt/code/yolo-jail --name allow-git-fetch -- yolo allow git fetch
```

This is the stop the git briefing already describes in prose, as one line a human can paste.

### C.5 Granting a read, and the approval that follows

```console
$ yolo allow git fetch
~/.dotfiles/tag-work/config/yolo-jail/work.toml
@@ -3,2 +3,5 @@
 packs = ["bedrock", "aws-auth", "copilot", "claude", "luna-worker", "claude-bedrock-global"]
+
+[allow.git]
+fetch = true
Written through ~/.config/yolo-jail/config.toml → include work.toml, the file that holds [allow].
This grant takes effect when a workspace approves it. Approve now for this workspace? [y/N] y
  + git: fetch from mschulkind-oss/yolo-jail (from your user config, for every workspace)
Approved. The running jail's broker takes it at its next call.
```

Two steps become one answer. The table went to the file that already holds `allow`; with no such
file, the user config itself.

### C.6 Turning something off in one project

```console
$ yolo config unset mcp_servers.tavily --local
/workspace/yolo-jail.local.toml
@@ -1,2 +1,5 @@
 # This machine only; not committed.
+[mcp_servers.tavily]
+off = true
+
Written. The user config adds tavily; this project's jail will not see it.
```

`unset` in a lower-precedence scope cannot delete a higher layer's key, so it writes the removal
spelling [OQ-RC7](#OQ-RC7) picks. In the same scope as the key, `unset` deletes the lines.

### C.7 The migration, on the host

```console
$ yolo config migrate
Found 4 files in the old format:
  ~/.config/yolo-jail/config.jsonc                        (201 lines)
  ~/.config/yolo-jail/overrides.jsonc                     (24 lines, via include_if_found)
  ~/.dotfiles/tag-work/config/yolo-jail/work.jsonc        (131 lines, via include_if_found)
  ~/code/yolo-jail/yolo-jail.jsonc                        (133 lines)
Not found here (listed in config.jsonc): ~/.dotfiles/tag-personal/config/yolo-jail/personal.jsonc.
  Run `yolo config migrate` on a machine that has it.
Also found: ~/.config/yolo-jail/config.lua, 0 bytes. The transform that read it is gone; it will be deleted.

--- ~/.config/yolo-jail/config.jsonc
+++ ~/.config/yolo-jail/config.toml
  (170 lines → 17; 3 comments moved with their keys)
--- ~/.dotfiles/tag-work/config/yolo-jail/work.jsonc
+++ ~/.dotfiles/tag-work/config/yolo-jail/work.toml
  (131 lines → 29; 6 facts folded into [providers.bedrock.every_model]; "vendor" dropped on 6 models: it follows from the id)
--- ~/code/yolo-jail/yolo-jail.jsonc
+++ ~/code/yolo-jail/yolo-jail.toml
  (133 lines → 6; 121 template comment lines dropped; "network.mode": "bridge" dropped: it is the default)

Effective config before and after: identical (checked as canonical JSON, 2,447 bytes).
Records only yolo reads, respelled in place: 3 approval records, 2 boot baselines.
Write 4 files, keeping each original as <name>.pre-migrate? [y/N] y
Written. Nothing else on this machine changed. Next: `yolo check`.
```

The equality line is the proof. A difference prints key by key and nothing is written.

### C.8 An older yolo meets the new file

With the release that knows the names ([§9.2](#92-the-order-of-releases), release N):

```console
$ yolo
~/.config/yolo-jail/config.toml is for a newer yolo than this one (0.16.2).
Upgrade with `yolo update`, or `just install` from a checkout. Nothing was started.
```

Without it, the older yolo finds no `config.jsonc` and starts a jail with no coding agent, which is
why release N ships first and every dotfiles machine takes it before any of them migrates.

### C.9 Where did this value come from?

```console
$ yolo config why profile.claude
profile.claude = false
  set in  ~/.config/yolo-jail/overrides.toml:9   (via include_if_found from config.toml:8)
  default would be: the default setup, "bedrock"
```

The answer comes from the origins record the launch already writes; the command is the only new
part.
