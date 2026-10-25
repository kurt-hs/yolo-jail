---
title: "Your config, one screen: packs, logins, and which setup each agent runs on"
date: 2026-10-25
status: in-review
stage: DESIGN
next: "Rule OQ-RC14 and OQ-RC15; OQ-RC3, RC13 and RC16 follow"
tags: [config, packs, providers, profiles, broker, grants, conventions, design]
summary: "The user config as one annotated file, today and proposed, the principles it obeys, and the seven choices still open, each a choice between lines of that file. Part of the readable-config redesign; the ledger of rulings is in the front door and the evidence is in the appendix."
---

# Your config, one screen: packs, logins, and which setup each agent runs on

**Status:** 2026-10-25. Nothing is built. The "today" file is the maintainer's effective config as
this jail inherited it, condensed.

**Approved 2026-10-25:** the shape of the file in [§2](#2-the-file-proposed), lever by lever, and
`allow` as the logins section's name ([ledger](readable-config.md#decision-ledger)). **Still
open:** the five questions in [§6](#6-open-questions), each a choice between lines of that file;
the file is written with the leanings, and a `?` marks each line a question could change.

**Needs your ruling:** [OQ-RC3](#OQ-RC3), [OQ-RC13](#OQ-RC13), [OQ-RC14](#OQ-RC14),
[OQ-RC15](#OQ-RC15), [OQ-RC16](#OQ-RC16).

> **In short.** Each section of your config names what you decided, in a word you would use, and
> says it once. The file below is the whole proposal; the prose only says why.

**Reads with:** [`readable-config.md`](readable-config.md) (the front door, the principles, and
the ledger of rulings), [`config-project.md`](config-project.md) (a project's file, which asks
within what this one allows), [`config-commands.md`](config-commands.md) (the commands that edit
these files).

---

## 1. The file today

Condensed from the maintainer's effective config. The `// read as` comments are what a cold
reader took each line to mean.

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

```toml
# ~/.config/yolo-jail/config.toml
packs = ["pi", "local/pi-fork", "codex", "git", "github", "bedrock", "aws-auth",
         "local/matt", { git = "https://github.com/mschulkind-oss/matt-craft" }, "claude"]
         # the only switch; local/<name> is ~/.config/yolo-jail/packs/<name>        (?OQ-RC15)

[allow.git]                       # the most any project's jail may do with your logins
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

A project's own file asks for what it needs under `[needs.git]` and `[needs.github]`, within
what `allow` here permits ([`config-project.md`](config-project.md)).

On the maintainer's machines this is four files, not one: `config.toml`, `config.d/overrides.toml`,
and a work and a personal file that his dotfiles put in `config.d/` on the right machines
([appendix A.3](#a3-four-files-not-one)). The whole set is 45 non-blank lines.

### 2.1 Naming a pack that is not shipped

A pack lives in a source you named once, and a source is a directory tree of packs, wherever it
is: a folder on this machine, or a git repository, which is the same layout fetched (what
Homebrew calls a tap and Claude Code a marketplace). There is no bare path form.

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
`[allow."local/my-auth"]`. A shipped pack's table stays bare, `[allow.aws-auth]`.

## 3. Principles

1. **Name the decision, not the mechanism.** `push = ["main"]`, not `{"kind": "branch", …}` under
   `brokered`.
2. **Say each fact once.** A model's key is its id; the maker follows from the id; facts every
   model shares sit in `every_model`.
3. **Write nothing that is the default.**
4. **One switch, one place.** `packs` is the switch. A settings table selects nothing and stands
   for nothing; a table whose pack is not selected is an error, offered as a fix first
   ([§4](#4-the-nudge-when-a-table-and-a-pack-disagree)).
5. **No value is a reserved string.** `true`, `false`, a number, a name, or a list of names;
   never a phrase yolo has to recognize.
6. **Two words for two scopes.** `allow` here is the ceiling; `needs` in a project is the ask,
   and must fit inside it.
7. **Plural for a collection you fill, singular for one thing.** `packs`, `providers.<name>`,
   `mcp_servers.<name>`; `run_on`, `allow.git`.

## 4. The nudge, when a table and a pack disagree

`allow.<pack>` is valid only when `<pack>` is in `packs`. The check offers the fix first; declined,
it fails and a launch refuses.

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

A convention is a default that follows from something you already did, so the line that would
say it is never written. Three rules bound every one: the explicit key still wins when written;
`yolo config show` prints what a convention supplied, as `packs = [..., "local/matt"]   # by
convention: local/ is ~/.config/yolo-jail/packs`; and no convention widens what a jail may do
with a login. These six are proposed; [OQ-RC13](#OQ-RC13) takes or vetoes each. A heading or a
declaration never stands for a value: a settings table does not select its pack
([§4](#4-the-nudge-when-a-table-and-a-pack-disagree)), and a lone provider does not pick itself
as everyone's setup.

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
by rule ([OQ-NC9](../plans/notch-convergence.md#7-decision-ledger), 2026-09-28), because dotfile
managers deploy exactly this shape; a fetched pack does not. If you keep both spellings and list
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

The `tag-work/` directory is from the path of the maintainer's own work file: the layout rcm's
`rcup -t work` uses, where a `tag-<name>/` directory is linked into `~` only on machines where the
tag is on. If that is what puts `work.jsonc` on work machines today, moving it to
`tag-work/config/yolo-jail/config.d/work.toml` puts it in `config.d/` there and nowhere else, and
the include line is unnecessary. If the dotfiles link differently, C2 still holds for any file
placed in `config.d/`; only the "no line at all" part depends on the linking.

```toml
# C5  a project needs no file                                 (config-project.md §2)
# C6  a pack needs no manifest                                (pack-manifests.md §3)

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

## 6. Open questions

1. 💬 <a id="OQ-RC3"></a>**OQ-RC3: Should you be able to pick a provider by its own name, without a profile?**

   The maintainer runs every agent on Bedrock, starting on Sol. Today that is three `bedrock`s. Seven
   of the nine shipped profiles are named after the provider they pick, so `-p bedrock` names one
   of two things. The providers doc leans toward keeping profiles as the only selectable thing,
   because a provider has no place for the starting model; B gives it one. The spelling of a
   variant is [OQ-RC14](#OQ-RC14)'s; B shows its option A.

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
   [providers.bedrock.variants.opus]              # defining one: the same provider, a different start
   model = "global.anthropic.claude-opus-5-5"
   [providers.bedrock.variants.bridge]            # or a different route to it
   via = "wire-bridge"
   [run_on]
   default = "bedrock"                            # using them: the provider by name, …
   claude = "bedrock/opus"                        #   a variant as provider/variant, and -p bedrock/opus

   # C  only providers; options ride the flag:  -p bedrock:model=opus
   ```

   <!-- vantage: question id=OQ-RC3 leaning="B: the common case is one name, and the rare case keeps the profile that already works." -->

   _Leaning:_ B: the common case is one name, and the rare case keeps the profile that already
   works.

   **Answer:**
   > _(empty — fill in when decided)_

2. 💬 <a id="OQ-RC13"></a>**OQ-RC13: Which of the six conventions go in?**

   A colleague sets up a work machine and writes the file in [§2](#2-the-file-proposed). The six
   conventions of [§5](#5-lines-you-never-write) take lines off it that follow from what they
   already did: the pack-sources line (C1), the include line (C2), a project file with nothing to
   say (C5), a manifest for a pack that is only files (C6), facts the catalog already knows (C7),
   and a `ref` that is the default branch (C9). Each is still writable, and `yolo config show`
   prints what a convention supplied.

   - **A — All six.**
   - **B — Some.** Name the ones to drop; each stands alone.
   - **C — None.** Every line is written.

   <!-- vantage: question id=OQ-RC13 leaning="A: each of the six reads only what the user already wrote, and none lets a heading or a declaration stand for a value." -->

   _Leaning:_ A: each of the six reads only what the user already wrote, and none lets a heading
   or a declaration stand for a value.

   **Answer:**
   > _(empty — fill in when decided)_

3. 💬 🤷 <a id="OQ-RC14"></a>**OQ-RC14: What are the selection and the declarations called?**

   Two keys mean different things: one maps each agent to a setup, the other describes a setup.
   Today they are `profile` and `profiles`, one letter apart. Under [OQ-RC3](#OQ-RC3)'s B a setup
   is usually a provider, and a declared one is a variant of it.

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

4. 💬 <a id="OQ-RC15"></a>**OQ-RC15: How is a pack that yolo does not ship named in your config?**

   A colleague publishes a pack called `tools`, and so does someone on another team. You list
   both. Today each is named by the last segment of its address, so the second is refused with
   "give one an explicit `name`", and you rename it. Homebrew and Claude Code never reach that
   step ([appendix A.5](#a5-how-other-tools-name-things-from-several-sources)).

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

5. 💬 <a id="OQ-RC16"></a>**OQ-RC16: May your config let a jail fetch from any repository, not only the project's own?**

   You write `[allow.git] fetch = true`. Today that reaches the project's remotes as approved at
   launch; an agent that clones a second repository to read it is refused with the scope.
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
