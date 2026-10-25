---
title: "Your config, one screen: packs, logins, and which setup each agent runs on"
date: 2026-10-25
status: in-review
stage: DESIGN
next: "Rule OQ-RC14 (the two profile names) and OQ-RC15 (how your packs are named); OQ-RC1, RC3, RC4, RC13 and RC16 follow"
tags: [config, packs, providers, profiles, broker, grants, conventions, design]
summary: "The user config as one annotated file, today and proposed, with the principles it obeys and the seven questions it raises. Part of the readable-config redesign; evidence is in the appendix."
---

# Your config, one screen: packs, logins, and which setup each agent runs on

**Status:** 2026-10-25. Nothing is built. The "today" file is the maintainer's effective config as
this jail inherited it, condensed; the evidence behind each claim is in [the appendix](#appendix-evidence).

**Needs your ruling:** [OQ-RC1](#OQ-RC1), [OQ-RC3](#OQ-RC3), [OQ-RC4](#OQ-RC4),
[OQ-RC13](#OQ-RC13), [OQ-RC14](#OQ-RC14), [OQ-RC15](#OQ-RC15), [OQ-RC16](#OQ-RC16).

> **In short.** Each section of your config names what you decided, in a word you would use, and
> says it once. The file below is the whole proposal; the prose only says why.

**Reads with:** [`readable-config.md`](readable-config.md) (the front door and the ledger of
rulings so far), [`config-project.md`](config-project.md) (a project's file),
[`config-commands.md`](config-commands.md) (format, and the commands that edit these files).

---

## 1. The file today

Condensed from the maintainer's effective config. The `// read as` comments are what the
maintainer, or a cold reader, took each line to mean on 2026-10-24.

```jsonc
{
  "packs": ["pi", "~/.dotfiles/yolo-packs/pi-fork", "codex", "git", "github", "bedrock", "aws-auth",
            "~/.dotfiles/yolo-packs/matt", "git+https://github.com/mschulkind-oss/matt-craft?ref=main", "claude"],
  "loopholes": {
    "aws-auth": { "enabled": true,                       // read as: a second switch; which one is real?
                  "settings": { "profile": "my-sso-profile", "unnarrowed": true } }
  },
  "brokered": {                                          // read as: "I don't understand 'brokered'"
    "git":    { "grants": [{ "kind": "read", "repo": "*" }],   // read as: read access to every repo anywhere
                "grant_kinds": ["branch"] },                   // read as: "granting read on branches"
    "github": { "grants": [{ "kind": "read", "repo": "*" }] }
  },
  "profile":  { "*": "bedrock", "claude": null },        // read as: null = unset? default?
  "profiles": { "bedrock": { "provider": "bedrock", "model": "global.openai.gpt-6.1-sol" } },
                                                         // read as: three bedrocks; one thing or three?
  "providers": { "bedrock": { "region": "us-east-1", "models": {
    "global.anthropic.claude-opus-5-5": {
      "id": "global.anthropic.claude-opus-5-5",          // the key, again
      "vendor": "anthropic",                             // the id, again
      "input": ["text", "image"], "max_tokens": 128000, "reasoning": true,   // the same on all six models
      "context_window": 1000000, "name": "Opus 5.5",
      "cost": { "input": 4, "output": 20, "cache_read": 0.2, "cache_write": 5 }
    }
    // … five more models, 18 lines each
  } } }
}
```

178 lines in all, to say about 20 things.

## 2. The file proposed

Under the leanings of every question below. Names marked `?` are the open ones.

```toml
# ~/.config/yolo-jail/config.toml
packs = ["pi", "local/pi-fork", "codex", "git", "github", "bedrock", "aws-auth",
         "local/matt", { git = "https://github.com/mschulkind-oss/matt-craft", ref = "main" }, "claude"]
         # the only switch; local/<name> is ~/.config/yolo-jail/packs/<name>        (?OQ-RC15)

[allow.git]                       # the most any project's jail may do with your logins   (?OQ-RC4)
fetch = true                      # this project's own remotes                            (?OQ-RC16)
push = true                       # a project may ask, branch by branch; never a grant by itself

[allow.github]
read = true

[allow.aws-auth]                  # settings; the switch is the name in packs
profile = "my-sso-profile"
unnarrowed = true

[run_on]                          # which setup each agent runs on                       (?OQ-RC14)
default = "bedrock"
claude = false                    # no setup from yolo: claude keeps its own login

[providers.bedrock]               # selectable by its own name                           (?OQ-RC3)
region = "us-east-1"
model = "global.openai.gpt-6.1-sol"

[providers.bedrock.every_model]   # shared by each model below unless it says otherwise
accepts = ["text", "image"]
max_tokens = 128_000
reasoning = true

[providers.bedrock.models]        # the key is the id; the maker follows from it
"global.anthropic.claude-opus-5-5" = { name = "Opus 5.5", context = 1_000_000, price = { input = 4, output = 20, cache_read = 0.2, cache_write = 5 } }
"global.openai.gpt-6.1-sol"       = { name = "Sol 6.1",  context = 272_000,   price = { input = 2, output = 10, cache_read = 0.1, cache_write = 2.5 } }
# … four more, one line each
```

### 2.1 Naming a pack that is not shipped

Under [OQ-RC15](#OQ-RC15)'s leaning a pack lives in a source you named once, and a source is a
directory tree of packs, wherever it is: a folder on this machine, or a git repository, which is
the same layout fetched (what Homebrew calls a tap and Claude Code a marketplace). There is no
bare path form.

```toml
[pack_sources]                                # named once; each is a tree of <pack>/ directories
dotfiles = "~/.dotfiles/yolo-packs"
src = "~/src"
acme = { git = "https://github.com/acme/yolo-packs" }       # the default branch, locked (C9)

packs = [
  "claude",                                   # shipped: bare; yolo/claude is the explicit spelling
  "local/matt",                               # ~/.config/yolo-jail/packs/matt; local is the one built-in source
  "dotfiles/matt-fzf",                        # ~/.dotfiles/yolo-packs/matt-fzf
  "src/tools",                                # ~/src/tools: a folder you named that happens to hold one pack
  "acme/tools",                               # acme/yolo-packs' tools/ directory, fetched and locked as today
  { git = "https://github.com/mschulkind-oss/matt-craft" },   # a repository that IS one pack:
]                                             #   named mschulkind-oss/matt-craft from its URL, as today
```

Two git forms, each with one reading: a repository *of* packs is a source, and a repository that
*is* a pack is listed directly. Both are fetched and locked as git packs are today
(`packs.lock.json`). `ref = "v2.1.0"` or `ref = "alice/review"` pins something other than the
default branch; with no `ref`, the default branch is read from the remote and locked (C9).

The name is used wherever yolo names the pack back to you: the launch's pack line, `yolo pack
ls`, `explain` and `footprint`, the nudge in [§4](#4-the-nudge-when-a-table-and-a-pack-disagree),
`pack lint`, and a settings table for a login-serving pack, which TOML then quotes:
`[allow."local/my-auth"]`. A shipped pack's table stays bare, `[allow.aws-auth]`. The staging
directory's spelling of a slash is the plan's.

On the maintainer's machines this is four files, not one: `config.toml`, `config.d/overrides.toml`,
and a work and a personal file that dotfile tags put in `config.d/` on the right machines
([appendix A.3](#a3-four-files-not-one)). The whole set is 45 non-blank lines.

## 3. Principles

Numbered so the questions can cite them.

1. **Name the decision, not the mechanism.** `push = ["main"]`, not `{"kind": "branch", …}` under
   `brokered`.
2. **Say each fact once.** A model's key is its id; the maker follows from the id; facts every
   model shares sit in `every_model`.
3. **Write nothing that is the default.**
4. **One switch, one place.** `packs` is the switch. A settings table selects nothing; a table
   with no pack, or a pack with no table, is a nudge naming the pack and the command
   ([RC-D7](readable-config.md#decision-ledger)).
5. **No value is a reserved string.** `true`, `false`, a number, a name, or a list of names;
   never a phrase yolo has to recognize ([RC-D6](readable-config.md#decision-ledger)).
6. **Two words for two scopes.** `allow` here is the ceiling; `needs` in a project is the ask,
   and must fit inside it ([RC-D8](readable-config.md#decision-ledger)).
7. **Plural for a collection you fill, singular for one thing.** `packs`, `providers.<name>`,
   `mcp_servers.<name>`; `run_on`, `allow.git` ([RC-D3](readable-config.md#decision-ledger)).

## 4. The nudge, when a table and a pack disagree

A settings table for a pack that is not selected is an error, not a warning
([RC-D9](readable-config.md#decision-ledger)): `allow.<pack>` is valid only when `<pack>` is in
`packs`. The nudge offers the fix first; declined, the check fails and a launch refuses.

```console
$ yolo check
~/.config/yolo-jail/config.toml:12: [allow.aws-auth] is set, but aws-auth is not in packs.
  Add it to packs? [y/N] n
error: config.toml:12: allow.aws-auth names a pack that is not selected. Add it (yolo pack add aws-auth)
  or delete the table.

$ yolo check
~/.config/yolo-jail/config.toml:12: [allow.aws-aut] names no pack. Did you mean aws-auth?
error: config.toml:12: allow.aws-aut: no such pack.
```

The reverse, a listed login pack whose table names no profile, is a warning: the pack installs,
its service serves nothing, and the check says what to set.

```console
$ yolo check
~/.config/yolo-jail/config.toml:3: packs lists aws-auth, but [allow.aws-auth] names no profile, so it serves nothing.
  Set one: yolo config set allow.aws-auth.profile <aws profile>
```

## 5. Lines you never write

A convention is a default that follows from something you already did. Three rules bound every
one: the explicit key still wins when written; `yolo config show` prints what a convention
supplied, as `packs = [..., "local/matt"]   # by convention: local/ is ~/.config/yolo-jail/packs`; and no convention widens
what a jail may do with a login. [OQ-RC13](#OQ-RC13) takes or vetoes each; its leaning takes all but
C3, C4 and C8.

```toml
# C1  your packs folder: ~/.config/yolo-jail/packs/<name> is local/<name>
pack_sources = { dotfiles = "~/.dotfiles/yolo-packs" }       # before
packs = ["dotfiles/matt"]
packs = ["local/matt"]                                        # after, with the folder linked:
```

```console
$ ln -s ~/.dotfiles/yolo-packs ~/.config/yolo-jail/packs     # or one link per pack, or real directories
```

`~/.config/yolo-jail/packs/` is an ordinary directory; a symlink in it, or the directory itself
being one, is a path like any other. A local pack already follows symlinks wherever they point,
by rule ([OQ-NC9](../plans/notch-convergence.md#7-decision-ledger), 2026-09-28), because dotfile managers deploy
exactly this shape; a fetched pack does not. No new handling. If you keep both spellings and list
both, `local/matt` and `dotfiles/matt` resolve to one directory, and yolo refuses the duplicate
naming both lines; list one.

```toml
# C2  machine files: config.d/*.toml are read after config.toml, in name order
include_if_found = ["../../.dotfiles/tag-work/config/yolo-jail/work.toml"]   # before
                                                              # after: no line; the file is in config.d/
```

```text
~/.config/yolo-jail/
  config.toml
  config.d/
    overrides.toml
    work.toml -> ~/.dotfiles/tag-work/config/yolo-jail/work.toml      # present on work machines only
```

"tag" is from the path of your own work file, `~/.dotfiles/tag-work/…`: the layout rcm's
`rcup -t work` uses, where a `tag-<name>/` directory is linked into `~` only on machines where the
tag is on. If that is what puts `work.jsonc` on work machines today, then moving it to
`tag-work/config/yolo-jail/config.d/work.toml` puts it in `config.d/` there and nowhere else, and
the include line is unnecessary. If your dotfiles link differently, C2 still holds for any file
you place in `config.d/`; only the "no line at all" part depends on the linking.

```toml

# C4  withdrawn 2026-10-25: an empty [allow.git] meaning "fetch" is presence standing for a value,
#     which no one reads cold; fetch = true stays written

# C7  a model named without facts takes the catalog's (pending the catalog check)
"global.anthropic.claude-opus-5-5" = { name = "Opus 5.5", context = 1_000_000, price = { … } }   # before
"global.anthropic.claude-opus-5-5" = { name = "Opus 5.5" }                                       # after

# C9  a git source or pack with no ref follows the repository's default branch, pinned by the lock
{ git = "https://github.com/acme/yolo-packs", ref = "main" }   # before (today: ?ref= is mandatory)
{ git = "https://github.com/acme/yolo-packs" }                 # after: read from the remote, locked at first fetch
```

C9 amends a stated rule: `internal/packsrc/addr.go` makes `?ref=` mandatory because "an unpinned
source silently changes under you." The lock file (`packs.lock.json`) records the commit, so an
omitted ref is pinned at first fetch and moves only on `yolo pack update`, as a git dependency
does in Cargo, npm and Go. The default branch is read with `git ls-remote --symref <url> HEAD`;
a remote that does not answer is refused naming the `ref` to write.

C5 (a project needs no file) and C6 (a pack needs no manifest) are in
[`config-project.md`](config-project.md#2-no-file-at-all) and
[`pack-manifests.md`](pack-manifests.md#3-no-manifest-at-all). Two more are not in the leaning,
because a declaration acting at a distance is what the maintainer read as *"a search for a pack
through a lookup system"*:

```toml
# C3  a section selects its pack (the nudge in §4 covers this case instead)
packs = ["claude", "bedrock", "aws-auth"]                     # before
packs = ["claude"]                                            # after: [allow.aws-auth] selected the rest

# C8  one declared setup is everyone's
[run_on]                                                      # before
default = "bedrock"
                                                              # after: no line; [providers.bedrock] is the only setup
```

## 6. Open questions

1. 💬 <a id="OQ-RC1"></a>**OQ-RC1: Which of the shape changes go in?**

   A colleague copies the maintainer's config to get Bedrock. They find `bedrock` three times,
   `"claude": null`, and an `aws-auth` that appears in `packs` and again under `loopholes` with
   `enabled: true`. They delete the `loopholes` block as a duplicate, and their jail starts with no
   AWS credentials and no message they can connect to it. Each lever below removes one such trap,
   and each can be taken alone.

   ```toml
   # L1  no written defaults; yolo init writes a short file   (the 121-line template goes)
   # L2  a key is the value it names
   "global.anthropic.claude-opus-5-5" = { name = "Opus 5.5" }      # no id, no vendor
   # L3  shared facts once
   [providers.bedrock.every_model]
   max_tokens = 128_000
   # L4  packs is the only switch                                 # loopholes.aws-auth.enabled goes
   # L5  words carry their meaning                                # brokered → allow/needs, cost → price,
                                                                  #   input → accepts, "*" → default, null → false
   # L6  a non-shipped pack is named by its source                # "local/matt", never a bare "matt"
   ```

   <!-- vantage: question id=OQ-RC1 leaning="All six. L4 is the only one that changes ruled rules, and the name in packs is the user saying so, which is what those rules protect." -->

   _Leaning:_ All six. L4 is the only one that changes ruled rules, and the name in `packs` is the
   user saying so, which is what those rules protect.

   **Answer:**
   > _(empty — fill in when decided)_

2. 💬 <a id="OQ-RC3"></a>**OQ-RC3: Should you be able to pick a provider by its own name, without a profile?**

   The maintainer runs every agent on Bedrock, starting on Sol. Today that is three `bedrock`s. Seven
   of the nine shipped profiles are named after the provider they pick, so `-p bedrock` names one
   of two things. The providers doc leans toward keeping profiles as the only selectable thing,
   because a provider has no place for the starting model; B gives it one.

   ```toml
   # A  profiles stay the only thing you select
   [profiles.bedrock]
   provider = "bedrock"
   model = "global.openai.gpt-6.1-sol"
   [run_on]
   default = "bedrock"

   # B  a provider is selectable by name and starts on its own model; a profile is only for a variant
   [providers.bedrock]
   model = "global.openai.gpt-6.1-sol"
   [run_on]
   default = "bedrock"

   # C  only providers; options ride the flag:  -p bedrock:model=opus
   ```

   <!-- vantage: question id=OQ-RC3 leaning="B: the common case is one name, and the rare case keeps the profile that already works." -->

   _Leaning:_ B: the common case is one name, and the rare case keeps the profile that already
   works.

   **Answer:**
   > _(empty — fill in when decided)_

3. 💬 🤷 <a id="OQ-RC4"></a>**OQ-RC4: What is the section for "what a jail may do with your logins" called?**

   The maintainer reads `brokered.git.grants` and says *"I don't understand 'brokered'."* The
   section holds git fetch and push, GitHub reads and writes, and the AWS login `aws-auth` serves.
   `as_you` was rejected ([RC-D2](readable-config.md#decision-ledger)); the project-side word is
   `needs` whatever this is.

   ```toml
   [allow.git]          # A: reads as a sentence; the command is `yolo allow git fetch`
   fetch = true

   [logins.git]         # B: names what is lent
   fetch = true

   [host_logins.git]    # C: the same, saying whose
   fetch = true
   ```

   <!-- vantage: question id=OQ-RC4 leaning="A, allow: it reads as a sentence with what follows, and the command verb and the section are then one word." -->

   _Leaning:_ A, `allow`: it reads as a sentence with what follows, and the command verb and the
   section are then one word.

   **Answer:**
   > _(empty — fill in when decided)_

4. 💬 <a id="OQ-RC13"></a>**OQ-RC13: Which of the nine conventions go in?**

   A colleague sets up a work machine and writes the file in [§2](#2-the-file-proposed). Under all
   nine conventions of [§5](#5-lines-you-never-write), `packs` loses `bedrock`, `aws-auth`, `git`
   and `github` (the sections selected them), the `[run_on] default` line goes (one provider is
   everyone's). `yolo config show` prints each
   supplied line with `# by convention`. The line they did not write is one they cannot get wrong,
   and one they cannot see without asking.

   - **A — All nine.**
   - **B — All but C3, C4 and C8**, where a declaration or a bare heading stands for a value.
     `packs` stays the one list of what is on; the selection and the grant stay written lines.
   - **C — None.**

   <!-- vantage: question id=OQ-RC13 leaning="B: packs stays the one list of what is on, and no heading looks a pack up or stands for a value; the rest read only what the user already wrote." -->

   _Leaning:_ B: `packs` stays the one list of what is on and a heading never looks a pack up; the
   other six read only what the user already wrote.

   **Answer:**
   > _(empty — fill in when decided)_

5. 💬 🤷 <a id="OQ-RC14"></a>**OQ-RC14: What are the selection and the declarations called, if not `profile` and `profiles`?**

   Two keys differ by one letter and mean different things: one maps each agent to a setup, the
   other describes a setup. You ruled the first name on 2026-09-29 so it mirrors `-p`, and on
   2026-10-24 called the pair *"still kinda awful."* Under [OQ-RC3](#OQ-RC3)'s B a setup is usually
   a provider, and a declared one is a variant of it.

   ```toml
   # A  run_on, and variants under their provider; the word "profile" leaves the config
   [run_on]
   default = "bedrock"
   claude = false
   [providers.bedrock.variants.opus]              # selected as bedrock/opus
   model = "global.anthropic.claude-opus-5-5"

   # B  agents, and setups  (agents is a retired key: an old yolo refuses it naming packs)
   [agents]
   claude = false
   [setups.bedrock-opus]
   provider = "bedrock"

   # C  keep profile, rename only the declarations
   [profile]
   claude = false
   [setups.bedrock-opus]
   provider = "bedrock"
   ```

   <!-- vantage: question id=OQ-RC14 leaning="A: run_on reads as a sentence per agent, a variant is what a same-provider profile is, and the pair goes." -->

   _Leaning:_ A: `run_on` reads as a sentence per agent, a variant is what a same-provider profile
   is, and the pair goes.

   **Answer:**
   > _(empty — fill in when decided)_

6. 💬 <a id="OQ-RC15"></a>**OQ-RC15: How is a pack that yolo does not ship named in your config?**

   A colleague publishes a pack called `tools`, and so does someone on another team. You list
   both. Today each is named by the last segment of its address, so the second is refused with
   "give one an explicit `name`", and you rename it; you called that *"like Python imports, but
   worse."* Homebrew and Claude Code never reach that step ([appendix A.5](#a5-how-other-tools-name-things-from-several-sources)).

   ```toml
   # A  qualified by source; only shipped packs are bare; no collision can happen
   pack_sources = { dotfiles = "~/.dotfiles/yolo-packs", acme = { git = "https://github.com/acme/yolo-packs" } }
   packs = ["claude", "dotfiles/matt", "local/x", "acme/tools"]

   # B  bare names, found in the shipped set or a folder; a name in both is refused and you rename one
   pack_sources = ["~/.dotfiles/yolo-packs"]
   packs = ["claude", "matt", "x", { git = "https://github.com/acme/tools", name = "acme-tools" }]

   # C  paths, as today
   packs = ["claude", "~/.dotfiles/yolo-packs/matt", "git+https://github.com/acme/tools?ref=main"]
   ```

   <!-- vantage: question id=OQ-RC15 leaning="A: the collision cannot happen, so there is nothing to rename; it is Homebrew's model, and the shipped set is the only bare namespace, as core is." -->

   _Leaning:_ A: the collision cannot happen, so there is nothing to rename; it is Homebrew's
   model, and the shipped set is the only bare namespace, as core is.

   **Answer:**
   > _(empty — fill in when decided)_

7. 💬 <a id="OQ-RC16"></a>**OQ-RC16: May your config let a jail fetch from any repository, not only the project's own?**

   You write `[allow.git] fetch = true`. Today that reaches the project's remotes as approved at
   launch; an agent that clones a second repository to read it is refused with the scope. You
   read the line as "any Git repo is allowed to be fetched by the jail, which would be nice."
   Widening it changes the broker's per-workspace scope; a fetch is read-only, so what it risks is
   a private repository's contents reaching a jail whose project never named it.

   ```toml
   # A  a list of patterns widens it; true stays the project's own
   [allow.git]
   fetch = ["github.com/mschulkind-oss/*"]        # or ["*"] for any repository the host login reaches

   # B  the project's own remotes only, as today; a project adds others under [needs.github] repos
   [allow.git]
   fetch = true

   # C  any repository, always, when fetch = true
   ```

   <!-- vantage: question id=OQ-RC16 leaning="A: the user config is trusted and a read is cheap to widen, but a line that names what it opens beats one that opens everything." -->

   _Leaning:_ A: the user config is trusted and a read is cheap to widen, but a line that names
   what it opens beats one that opens everything.

   **Answer:**
   > _(empty — fill in when decided)_

## Appendix: evidence

### A.1 Where today's config misleads

| Line today | Read cold as | What it means |
| :--- | :--- | :--- |
| `"brokered": {"git": {"grants": [{"kind": "read", "repo": "*"}]}}` | read access to every repository anywhere | fetch from each workspace's own approved remotes |
| `"grant_kinds": ["branch"]` beside that | "granting read on branches" | the most a project may ask for: pushing to branches it names |
| `"profile": {"*": "bedrock", "claude": null}` | `null` reads as "unset" or "default" | every agent uses `bedrock` except claude, which keeps its own login |
| `"profiles": {"bedrock": {"provider": "bedrock", …}}` | three `bedrock`s: one thing or three? | a profile named after the provider it picks, plus a starting model |
| `"loopholes": {"aws-auth": {"enabled": true, …}}` with `"aws-auth"` in `packs` | two switches | the pack is installed by `packs`; its service is off until `enabled` says so |
| `"input": ["text", "image"]` beside `"cost": {"input": 10}` | two meanings of one word | what the model accepts; its price per million input tokens |

The approval prompt showed the maintainer `+ grant read * (from your user config, for every
workspace)`, which is the first row.

### A.2 Measured size

The effective user config is 170 lines of JSON plus about 8 lines of broker grants the jail's copy
omits. The models are 109 lines; `id`, `input`, `max_tokens` and `reasoning` are each written six
times. The same content: 58 lines in reshaped JSONC laid out by hand, 133 fully expanded by a
formatter, 53 in TOML. The shape removes about two-thirds of the lines whatever the format.

### A.3 Four files, not one

The jail's copy carries a `yolo-origins` line recording which host file set each key
(`internal/config/inherit.go` writes it):

| File | Sets | Reaches a machine by |
| :--- | :--- | :--- |
| `~/.config/yolo-jail/config.jsonc` | 15 packs, two mise tools | dotfiles, every machine |
| `~/.config/yolo-jail/overrides.jsonc` | `profile`, `mcp_servers` | `include_if_found` |
| `~/.dotfiles/tag-work/config/yolo-jail/work.jsonc` | `providers`, `profiles`, `loopholes`, six packs | the same, work machines only |
| `…/personal.jsonc` | the `or` profile, two OpenRouter `@200k` models, three packs | the same, personal machines; not visible from this jail |

A migration rewrites each where it is and never merges them, and the origins line is most of a
`yolo config why`.

### A.4 What L4 amends

`packs` as the only switch amends four loophole rules and keeps each one's reason
([`loophole-system.md`](../reference/loophole-system.md#principles)): R1 "presence never
activates" (a manifest on disk still activates nothing; the name in `packs` is the user saying so,
and nothing is listed by default, `run.warnIfNoPacks`); R2 "absent `default_enabled` means off"
(the manifest key goes; the list decides); R4 "host access is never on by default" (`allow` is
user scope only, and a listed login pack with no table serves nothing); R5 "enable is either
scope" (a workspace may still turn a non-login pack *off* with `off = true`, which is what the
per-workspace switch file's `enabled: false` becomes, and may still *ask* under `needs`).

### A.5 How other tools name things from several sources

Checked 2026-10-25 where linked; the rest from documentation not re-read for this draft.

| Tool | A bare name means | A thing from another source | Two with one name | Model |
| :--- | :--- | :--- | :--- | :--- |
| Python imports | the first match on `sys.path` | the same bare name | silently shadowed | the failure to avoid |
| apt | one flat name across repositories | the same name | pin priorities decide, silently | the same, by policy |
| [Homebrew](https://rubydoc.brew.sh/TapFormulaAmbiguityError.html) | a core formula | `user/repo/name` from a tap | refused: "use the fully-qualified name" | **built-ins bare, the rest qualified** |
| [Claude Code plugins](https://code.claude.com/docs/en/discover-plugins) | — | `plugin@marketplace` | cannot meet | the same, with `@` |
| Docker images | `library/ubuntu` | `user/image` | cannot meet | the same |
| Nix flakes, Terraform providers | — | every input has a local name you choose, plus its source | cannot meet | all handles, no bare names |
| Cargo | a crates.io name | `key = { git = …, package = "x" }` | renamed through the key | rename on collision |
| Go imports | — | the import path is the identity | an import alias | rename on collision |
| [lazy.nvim](https://git.kjan.de/jank/lazy.nvim/src/tag/v9.8.4/doc/lazy.nvim.txt) | — | `owner/repo`, directory named by the repository | `name =` overrides | rename on collision |

Today a configured pack is named by the last segment of its source address, the manifest's `name`
is informational (`internal/packload/packload.go`, the `Pack.Name` comment), and two entries with
one name are refused with "give one an explicit `name`" (`internal/config/packs.go`).

### A.6 The grant kinds, mapped

The four ruled grant kinds ([OQ-BB19](boundary-broker.md#OQ-BB19)) become `read` → `read`,
`create-branch` → `new_branches = true`, `branch` → `push = ["main"]`, and `create-pr` and
`create-issue` → `write`. `push = true` at user scope answers
[OQ-BB23](boundary-broker.md#OQ-BB23) with "as a ceiling, never as a grant", its leaning. A GitHub
`write` ask is accepted by the config and refused at use until a jail's write request can reach
`yolo approve`, whose delivery is not built (`internal/ghbroker/window.go`). `fetch = true`
reaches what today's `{"kind": "read", "repo": "*"}` reaches: the project's remotes plus `repos`,
approved at launch.

### A.7 Models under the providers doc

The standing leaning on [OQ-PP1](providers-and-profiles-redesign.md#OQ-PP1) keeps profiles as
the only selectable thing, for [OQ-AP1](active-provider-sets.md#11-decision-ledger)'s reason: a
provider loses a profile's options, the starting model among them. [OQ-RC3](#OQ-RC3)'s B answers
that reason directly: the provider carries `model`. What it cannot carry (`via`, a second starting
model) is what a variant is still for. Provider and variant names share one namespace; a clash is
refused; a shipped profile that only repeats its provider's name is deleted.
