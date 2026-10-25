---
title: "A project's file: what it installs, what it asks for, and what it turns off"
date: 2026-10-25
status: in-review
stage: DESIGN
next: "Rule OQ-RC17 (§1, the file's shape) and OQ-RC7 (§3, saying off with no null)"
tags: [config, workspace, install, grants, mounts, design]
summary: "The project config as annotated files, today and proposed: install, needs, mounts, ports, environment, and the local file. Each section ends with its question; a section with none is approved. Part of the readable-config redesign."
---

# A project's file: what it installs, what it asks for, and what it turns off

**Status:** 2026-10-25. Nothing is built. The "today" files are this repository's and a composite
team project's.

**How to read this.** [§1](#1-the-file) is the file and ends with the question that approves its
shape; [§2](#2-no-file-at-all) is approved already; [§3](#3-the-local-file-and-turning-something-off)
ends with the one question about spelling "off".

**Needs your ruling:** [OQ-RC17](#OQ-RC17) in [§1](#1-the-file), [OQ-RC7](#OQ-RC7) in
[§3](#3-the-local-file-and-turning-something-off).

> **In short.** A project file says what the project needs, inside what your own config allows,
> and most projects need no file at all.

**Reads with:** [`readable-config.md`](readable-config.md) (front door and ledger),
[`config-user.md`](config-user.md) (the ceiling a project's asks fit inside),
[`config-commands.md`](config-commands.md) (the commands an agent runs instead of editing this file).

---

## 1. The file

### This repository's

Today, `yolo-jail.jsonc`: 133 lines, of which 121 are the template's comments and 12 are live.

```jsonc
{
  "packages": ["just"],
  "network": { "mode": "bridge" },                     // the default, restated
  "mise_tools": { "lua": "5.4.9" },
  "brokered": { "git": { "grants": [
    { "kind": "branch", "repo": "github.com/mschulkind-oss/yolo-jail", "branch": "main" }
  ] } }
}
```

Proposed, `yolo-jail.toml`:

```toml
[install]
nix = ["just"]                    # runs this repo's Justfile recipes
mise = { lua = "5.4.9" }          # syntax-checks the packs' derive.lua files

[requests.git]                    # what this project requests, from your [allow.git] on_request list
push = ["main"]
```

### A team project's

Today:

```jsonc
{
  "packages": ["postgresql", "just"],
  "mise_tools": { "node": "22", "python": "3.12" },
  "mounts": ["~/code/acme-shared", "~/x:/ctx/x:ro"],  // the second is one host path named "~/x:/ctx/x:ro", skipped with a warning
  "network": { "ports": ["8000:8000"], "forward_host_ports": [5432] },   // HOST:JAIL, then JAIL:HOST
  "gpu": { "enabled": true },
  "resources": { "memory": "16g", "cpus": 8 },
  "env_sources": ["~/.config/yolo-jail/secrets.env", { "AWS_PROFILE": null, "PAGER": "cat" }],
  "brokered": { "github": { "repos": ["acme/shared"] } }
}
```

Proposed:

```toml
[install]
nix = ["postgresql", "just"]
mise = { node = "22", python = "3.12" }   # "3.12" stays a string

mounts = ["~/code/acme-shared"]           # read-only; a bare path with no colon is still legal

[[mounts]]
host = "~/x"
at = "/ctx/x"                             # read-only unless `write = true`, which only your config may say

[machine]
gpu = true                                # warns and starts without one on a machine that has none
resources = { memory = "16g", cpus = 8 }
ports = [{ host = 8000, jail = 8000 }]           # the host reaches the jail's :8000
forward = [{ jail = 5432, host = 5432 }]         # the jail reaches the host's postgres at localhost:5432

[env]
files = ["~/.config/yolo-jail/secrets.env"]
set = { PAGER = "cat" }
unset = ["AWS_PROFILE"]

[requests.github]
repos = ["acme/shared"]                   # beyond this repository's own remotes

[requests.git]
new_branches = true                       # branches of its own, never main; each is approved at launch
```

A direction is a key, not a position in a string: `{ host = 8000, jail = 8000 }` where today's
`"8000:8000"` and `forward_host_ports` run opposite ways. A project requests; your config says
what may be requested. A request outside that is not an error: two people on one repository may
allow different things, so the one who does not sees it reported and skipped, and the jail runs
without it:

```console
$ yolo check
yolo-jail.toml:22: requests.git.new_branches asks to create branches; your config does not list
  new_branches under allow.git.on_request, so this project will not. To allow it: yolo allow git on_request new_branches
```

Nothing that decides where inference goes or what a jail may do with your logins is accepted
here, because this file is the agent's to edit; `yolo check` refuses such a key naming the file
it belongs in.

1. 💬 <a id="OQ-RC17"></a>**OQ-RC17: Is the project file's shape approved?**

   An agent in a jail opens the project's file to add a tool, or a teammate opens it to see what
   the project needs. Today they meet 121 lines of template comments around 12 live ones, two
   port lists that run in opposite directions, a mount string that is parsed on colons, and a
   `brokered` block. The file above is the proposal, section by section; each can be vetoed
   alone.

   ```toml
   [install]          # nix and mise, where today's packages and mise_tools were
   mounts / [[mounts]] # a bare path, or a table with host and at
   [machine]          # gpu, resources, ports, forward, where today's four top-level keys were
   [env]              # files, set, unset, where today's one mixed list was
   [requests.*]       # what this project requests, from your allow's on_request list
   ```

   <!-- vantage: question id=OQ-RC17 leaning="Approve: each section names what the project decides, and the two string traps become keys." -->

   _Leaning:_ Approve: each section names what the project decides, and the two string traps
   become keys.

   **Answer:**
   > _(empty — fill in when decided)_

## 2. No file at all

Approved 2026-10-25 as convention C5 ([`config-user.md` §8](config-user.md#8-lines-you-never-write)).
Most projects need nothing. `yolo` in a folder with no `yolo-jail.toml` runs with defaults;
`yolo init` writes the three-line pointer file only when asked, and `.yolo/` goes into
`.git/info/exclude` rather than the project's `.gitignore`.

```text
# before                                   # after
acme/                                      acme/
  yolo-jail.toml   (the 133-line template)
  .gitignore       (+ ".yolo/")
```

## 3. The local file, and turning something off

`yolo-jail.local.toml` is this machine's and uncommitted, merged after the project file as today.
It is where a project turns off something your own config added:

```toml
# This machine only; not committed.
[mcp_servers.postgres]
command = ["npx", "-y", "@modelcontextprotocol/server-postgres", "postgresql://localhost/acme"]

[mcp_servers.tavily]
off = true                        # your config adds it; not wanted here

[machine]
resources = { memory = "32g" }
```

Config lists merge by union, so a lower layer can add to a list and never remove from it; "off"
has to be a word on the thing. What that word is, and how the other places `null` is used today
are spelled, is the question.

1. 💬 <a id="OQ-RC7"></a>**OQ-RC7: How does a TOML file say "remove" or "not this one", with no `null`?**

   Your config adds the `tavily` MCP server. In one project you don't want it, so today that
   project writes `"mcp_servers": {"tavily": null}`. The same `null` today keeps claude on its own
   login, unsets `AWS_PROFILE`, clears a `repos` list, removes a model fact, and disables a lower
   layer's value in an include. TOML has no `null`, so each needs a spelling a cold reader
   understands, and `yolo config unset` needs one it can write.

   ```toml
   # A  a word where each thing lives
   [mcp_servers.tavily]
   off = true
   [env]
   unset = ["AWS_PROFILE"]
   [inference_provider]
   claude = false
   [requests.github]
   repos = []                        # an empty list clears a list

   # B  one reserved string, valid wherever a value may be removed  (a reserved string, which principle 5 rules out)
   mcp_servers.tavily = "off"
   env.AWS_PROFILE = "off"

   # C  one top-level list of removals, far from the things removed
   remove = ["mcp_servers.tavily", "env.AWS_PROFILE"]
   ```

   <!-- vantage: question id=OQ-RC7 leaning="A: a word at the place, each saying what it does; off = true is the same word a workspace uses to turn a pack off, and an unset command has one thing to write per case." -->

   _Leaning:_ A: a word at the place, each saying what it does; `off = true` is the same word a
   workspace uses to turn a pack off, and an unset command has one thing to write per case.

   **Answer:**
   > _(empty — fill in when decided)_

## 4. Principles

1. **A project requests; your config says what may be requested.** Every request is a line in the
   launch approval prompt, granted or skipped; a request outside your ceiling is never an error.
2. **A project file is the agent's to edit**, so nothing user-scope is accepted here, and the
   refusal names the file it belongs in.
3. **A direction is a key, not a position in a string.**
4. **Off is a word at the place**, never a `null`.
5. **No file is the common case.**

## Appendix: evidence

- **The string traps are documented traps.** `config-ref`'s `mounts` section says a `~/x:/ctx/x:ro`
  string is parsed as one literal host path and skipped with a warning; its `ports` section says
  "Note the reversed order" of `forward_host_ports`. The table form of a mount already exists:
  this repository's `yolo-jail.local.jsonc` writes `{ "host": …, "at": …, "mode": "ro",
  "optional": true }`.
- **`null` has more than four meanings today.** `yolo config-ref` mentions it on 27 lines: an
  agent's own login, an MCP server removed, an environment variable unset, an include disabling a
  lower layer, a model-fact tombstone, a `repos` list cleared, a provider or adapter override
  dropped. The shipped prism meets the same problem on TOML surfaces, where a managed `null`
  deletes the key ([`pack-system.md`](../reference/pack-system.md)).
- **Scope is enforced per key today**, each validator re-reading the workspace config and
  refusing a user-only key there (`internal/config/validate.go` and its siblings); there is no
  central list. The new shape states scope per section, so the refusal can name it.
- **Config lists merge by union** (`internal/config/load.go`), so a lower layer can add to a list
  and never remove from it; that is why `off = true` is a word on the thing and not a list edit.
