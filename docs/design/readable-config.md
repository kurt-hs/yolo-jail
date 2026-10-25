---
title: "Config that reads right the first time: the front door"
date: 2026-10-24
status: in-review
stage: DESIGN
next: "Fourteen questions remain, each at the end of the section it is about; the maintainer rules by comment in the docs"
tags: [config, packs, manifest, providers, profiles, broker, grants, format, toml, migration, editing, readability, design]
summary: "The front door to the readable-config redesign: the thesis, the principles, the five documents (each one annotated config file with its questions), and the ledger of rulings made so far. The maintainer's config takes 178 lines to say about 20 things and is misread by its author; reshaped it is under 60, yolo can edit it one hunk at a time, and one command converts every file."
vantage:
  status-chip: true
---

# Config that reads right the first time: the front door

**Status:** 2026-10-25. Nothing is built. Nineteen rulings recorded ([ledger](#decision-ledger));
fourteen questions open, each in the document that shows its file. Evidence verified at
`fa13f42d9` against this repository, the effective user config this jail inherited, the 21
shipped packs, and the maintainer's twelve packs.

**Needs your ruling:** fourteen questions, listed per document in [§2](#2-the-five-documents).

> **In short.** A config a stranger reads correctly comes from its shape, not its file format, and
> a config yolo can edit without a mess comes from touching only the bytes of one value. Neither
> needs a custom format. Both need the file name to say which yolo wrote it.

**Why it matters.** The maintainer, 2026-10-24: *"I want configs that can be read, without much
foreknowledge, and be understood the right way."* The same day he misread two lines of his own
config, and asked for automatic conversion of everyone's files and for commands that edit a
config file mechanically instead of *"places where we ask agents to edit config files."*

**How to read this.** Each document below opens with the config file it is about, annotated,
then takes it one section at a time. A section that still needs a decision ends with its
question, each option written as the lines you would type; a section with no question is
approved. The arguments and measurements are in each document's appendix, there to check and
otherwise skippable.

**Reads with:** [`readable-config-plan.md`](readable-config-plan.md) (the implementation plan's
seed; incomplete while any question is open).

---

## 1. Principles

1. **Name the decision, not the mechanism.** `push = ["main"]`, not `{"kind": "branch", …}`
   under `brokered`.
2. **Say each fact once.** A key is the value it names; shared facts sit in one place.
3. **Write nothing that is the default.** And a convention is a line you never write at all.
4. **One switch, one place.** `packs` is the switch; a settings table with no pack is a nudge
   naming the pack and the command.
5. **No value is a reserved string.** `true`, `false`, a number, a name, a list of names.
6. **Two words for two scopes.** Your config says what every project gets and what one may request; a project requests. A request outside that is skipped and reported, never an error.
7. **An edit yolo makes changes only the bytes it was asked to.**
8. **The file name is the version**, and one command converts everything, proving it lost nothing.

## 2. The five documents

| Document | The file it shows | Its questions |
| :--- | :--- | :--- |
| [`config-user.md`](config-user.md) | `~/.config/yolo-jail/config.toml` and `config.d/`: packs, logins, which setup each agent runs on, the conventions | [OQ-RC14](config-user.md#OQ-RC14) the two profile names · [OQ-RC16](config-user.md#OQ-RC16) fetch from any repository |
| [`config-project.md`](config-project.md) | `yolo-jail.toml` and `.local`: install, requests, mounts, ports, env, turning things off | [OQ-RC17](config-project.md#OQ-RC17) the file's shape · [OQ-RC7](config-project.md#OQ-RC7) saying "off" with no `null` |
| [`config-commands.md`](config-commands.md) | the command each "edit your config" message prints, what it does to the file, and the format underneath | [OQ-RC9](config-commands.md#OQ-RC9) may yolo write your file · [OQ-RC2](config-commands.md#OQ-RC2) TOML or JSONC · [OQ-RC10](config-commands.md#OQ-RC10) the verbs · [OQ-RC12](config-commands.md#OQ-RC12) what `yolo config` means |
| [`config-migration.md`](config-migration.md) | the new file names, the release order, the migrate transcript | [OQ-RC11](config-migration.md#OQ-RC11) name or version key · [OQ-RC6](config-migration.md#OQ-RC6) older yolos · [OQ-RC18](config-migration.md#OQ-RC18) what migrate does · [OQ-RC8](config-migration.md#OQ-RC8) fetched packs |
| [`pack-manifests.md`](pack-manifests.md) | the maintainer's twelve packs, before and after | [OQ-RC19](pack-manifests.md#OQ-RC19) the manifest shape · [OQ-RC5](pack-manifests.md#OQ-RC5) manifests in the same break |

## 3. What this takes over, and what it reverses

- [OQ-M1](manifest-language.md#OQ-M1) and [OQ-M2](manifest-language.md#OQ-M2) by [OQ-RC5](pack-manifests.md#OQ-RC5) and
  [OQ-RC2](config-commands.md#OQ-RC2); [OQ-M3](manifest-language.md#OQ-M3) stays there.
- [OQ-PC1](pack-conventions.md#OQ-PC1) to [OQ-PC3](pack-conventions.md#OQ-PC3) by the `home/`
  folder.
- [OQ-PP1](providers-and-profiles-redesign.md#OQ-PP1) by [OQ-RC3](#decision-ledger), ruled.
- [OQ-BB23](boundary-broker.md#OQ-BB23) by `push = true` at user scope: a ceiling, never a grant.
- **Reversed on purpose:** [MN-D1](manifest-language.md#MN-D1) (2026-09-30), that the configs keep
  JSONC and moving them "would be a proposal of its own". [OQ-RC2](config-commands.md#OQ-RC2) is that proposal.
- **Amended:** [OQ-BB12](boundary-broker.md#15-decision-ledger), read as being about side effects
  and messes; [OQ-RC9](config-commands.md#OQ-RC9) is where that reading is ruled on. [PP-D10](providers-and-profiles-redesign.md#PP-D10)
  stands as to the key's job; [OQ-RC14](config-user.md#OQ-RC14) is about its name.

## 4. Non-goals

- **No custom format.** TOML and JSONC each carry the shape, and a splice works on either.
- **No manifest as code.**
- **No change to what the approval gate approves**, or to which scope may hold which key.
- **No aliases, ever.** An old spelling is refused naming the new one.
- **No automatic migration.** A launch offers it; a person answers.

## Decision Ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| OQ-RC15 | **A pack yolo does not ship is named by its source.** Ruled by taking the leaning: *"A: the collision cannot happen, so there is nothing to rename; it is Homebrew's model, and the shipped set is the only bare namespace, as core is."* `local/<name>`, `<source>/<name>`, `<owner>/<repo>`; no bare path form | 2026-10-25 | [`config-user.md` §4](config-user.md#4-naming-a-pack-that-is-not-shipped) | — |
| OQ-RC13 | **All six conventions go in.** Ruled by taking the leaning: *"A: each of the six reads only what the user already wrote, and none lets a heading or a declaration stand for a value."* C1 pack sources, C2 `config.d/`, C5 no project file, C6 no manifest, C7 catalog facts, C9 default branch | 2026-10-25 | [`config-user.md` §8](config-user.md#8-lines-you-never-write) | — |
| OQ-RC3 | **A provider is selectable by its own name and starts on its `model`; a variant is a second configuration of one provider.** Ruled by taking the leaning: *"B: the common case is one name, and the rare case keeps the profile that already works."* Same-named shipped profiles are deleted; answers [OQ-PP1](providers-and-profiles-redesign.md#OQ-PP1) | 2026-10-25 | [`config-user.md` §5](config-user.md#5-which-setup-each-agent-runs-on) | — |
| <a id="RC-D14"></a>RC-D14 | **An agent's own login is a provider its pack declares; no special value means "keep your own login".** Review comment on `codex = false`: *"let's have a special value here instead … false seems like it has nothing turned on."* A reserved word would break RC-D6, and a name already exists: `codex = "openai-codex"`, `claude = "anthropic"` | 2026-10-25 | [`config-user.md` §5](config-user.md#5-which-setup-each-agent-runs-on) | — |
| <a id="RC-D13"></a>RC-D13 | **One shape under `allow`: two lists, `always` and `on_request`.** The maintainer: *"so now we have two different shapes for default enabled and requestable?"* A boolean per granted capability beside a list of requestable ones was two shapes for one kind of thing | 2026-10-25 | [`config-user.md` §6](config-user.md#6-what-a-jail-may-do-with-your-logins) | — |
| <a id="RC-D12"></a>RC-D12 | **A project `requests`; `allow` lists what is `requestable`; an unmet request is skipped, never an error.** Review comments: *"requests? … Needs implies it's a fatal failure and I'm not sure this is a fatal failure"* (two people on one repository may allow different things), and on `fetch = true` beside `push = true`: *"this fetch is like a default on and then push is not a default on, but it can ask. That's as confusing as it could be."* The two meanings are now two keys | 2026-10-25 | [`config-user.md` §6](config-user.md#6-what-a-jail-may-do-with-your-logins) | — |
| <a id="RC-D11"></a>RC-D11 | **A declaration never selects or chooses.** The maintainer, on seeing a section-selects-its-pack convention still offered: *"Didn't we already decide that the pack does have to be listed, but we're going to help you add it?"* Yes: RC-D7 and RC-D9 settle it, and RC-D10 settles the lone-provider-is-everyone's case the same way. Neither is an option under the conventions ruling below | 2026-10-25 | [`config-user.md` §8](config-user.md#8-lines-you-never-write) | — |
| OQ-RC4 | **The logins section is `allow`.** Ruled by taking the leaning: *"A, allow: it reads as a sentence with what follows, and the command verb and the section are then one word."* A project's side is `needs` | 2026-10-25 | [`config-user.md` §2](config-user.md#2-the-file-proposed) | — |
| OQ-RC1 | **The shape of the user config is approved, all six levers.** Ruled by taking the leaning: *"All six. L4 is the only one that changes ruled rules, and the name in packs is the user saying so, which is what those rules protect."* L1 no written defaults; L2 a key is the value it names; L3 shared facts once; L4 `packs` is the only switch; L5 the renames; L6 a non-shipped pack named by its source | 2026-10-25 | [`config-user.md` §2](config-user.md#2-the-file-proposed) | — |
| <a id="RC-D10"></a>RC-D10 | **An empty section means nothing.** Review comment on convention C4: *"you're saying an empty section is meaningful?"* Withdrawn: presence never stands for a value; `fetch = true` stays written | 2026-10-25 | [`config-user.md` §8](config-user.md#8-lines-you-never-write) | — |
| <a id="RC-D9"></a>RC-D9 | **A settings table for an unselected pack is an error.** Review comment: *"if you say no, then this should turn into a fatal error because an allow.<foo> should error if not valid."* The nudge offers `yolo pack add`; declined, the check fails and a launch refuses. A table naming no pack at all is an error with "did you mean" | 2026-10-25 | [`config-user.md` §7](config-user.md#7-one-switch-and-the-nudge) | — |
| <a id="RC-D8"></a>RC-D8 | **Two words for two scopes.** Review comment: *"we're using the exact same language here, and it means two different things in two different places."* The user-config ceiling is `allow`; a project's ask is `needs`, and must fit inside it | 2026-10-25 | [`config-user.md` §3](config-user.md#3-principles) | — |
| <a id="RC-D7"></a>RC-D7 | **A section without its pack is a nudge, not a selection.** Review follow-up: *"maybe we can infer which pack you would need and give you a happy path nudge?"* `yolo check` names the pack and offers `yolo pack add <pack>` with a `y`; the reverse case offers the setting | 2026-10-25 | [`config-user.md` §7](config-user.md#7-one-switch-and-the-nudge) | — |
| <a id="RC-D6"></a>RC-D6 | **No value is a reserved string.** Review comment on `"when a project asks"`: *"some weird string like this is just weird for a config file."* A value is `true`, `false`, a number, a name or a list of names; `push = true` at user scope is the ceiling, `new_branches = true` replaces `"new branches"` | 2026-10-25 | [`config-user.md` §3](config-user.md#3-principles) | — |
| <a id="RC-D5"></a>RC-D5 | **`false`, not `"own login"`,** for an agent yolo gives no setup. Review comment: *"don't like"* | 2026-10-25 | [`config-user.md` §2](config-user.md#2-the-file-proposed) | — |
| <a id="RC-D4"></a>RC-D4 | **`default`, not `everyone`,** for today's `"*"`. Review comment: *"don't like the name"* | 2026-10-25 | [`config-user.md` §2](config-user.md#2-the-file-proposed) | — |
| <a id="RC-D3"></a>RC-D3 | **Plural for a collection the user fills, singular for one thing.** Review comment: *"why is this plural and mcp singular?"* `mcp.<name>` becomes `mcp_servers.<name>` (today's name), `[[mount]]` becomes `[[mounts]]`; all-singular would collide the selection with the declarations | 2026-10-24 | [`config-user.md` §3](config-user.md#3-principles) | — |
| <a id="RC-D2"></a>RC-D2 | **Not `as_you`.** Review comment: *"I hate the name 'as_you'."* The section's name was then ruled `allow` ([OQ-RC4](#decision-ledger)) | 2026-10-24 | [`config-user.md` §2](config-user.md#2-the-file-proposed) | — |
| <a id="RC-D1"></a>RC-D1 | **The `profile` key keeps its job.** Review comment on the draft's `use`: *"awful name … isn't this a profile?"* It is the agent-to-setup selection, and PP-D10's reason (the key mirrors `-p`) stands; its name is [OQ-RC14](config-user.md#OQ-RC14) | 2026-10-24 | [`config-user.md` §2](config-user.md#2-the-file-proposed) | — |
