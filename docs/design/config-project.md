---
title: "A project's file: what it installs, what it asks for, and what it turns off"
date: 2026-10-25
status: in-review
stage: DESIGN
next: "Rule OQ-RC7 (how a file says off, with no null)"
tags: [config, workspace, install, grants, mounts, design]
summary: "The project config as annotated files, today and proposed: install, needs, mounts, ports, environment, and the local file. One question: how a TOML file says 'remove' with no null. Part of the readable-config redesign."
---

# A project's file: what it installs, what it asks for, and what it turns off

**Status:** 2026-10-25. Nothing is built. The "today" files are this repository's and a
composite team project's.

**Needs your ruling:** [OQ-RC7](#OQ-RC7).

> **In short.** A project file says what the project needs, inside what your own config allows,
> and most projects need no file at all.

**Reads with:** [`readable-config.md`](readable-config.md) (front door and ledger),
[`config-user.md`](config-user.md) (the ceiling a project's asks fit inside),
[`config-commands.md`](config-commands.md) (the commands an agent runs instead of editing this file).

---

## 1. This repository's file, today and proposed

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

[needs.git]                       # what this project asks for, within your [allow.git]
push = ["main"]
```

And the uncommitted `yolo-jail.local.toml`, unchanged in meaning from today's:

```toml
[[mounts]]
host = "~/.local/share/yolo-jail/logs"
at = "/ctx/host-yolo-logs"
optional = true                   # a backend that cannot deliver it skips it
```

## 2. No file at all

Most projects need nothing. `yolo` in a folder with no `yolo-jail.toml` runs with defaults;
`yolo init` writes the three-line pointer file only when asked, and `.yolo/` goes into
`.git/info/exclude` rather than the project's `.gitignore`. This is convention C5 of
[`config-user.md`](config-user.md#5-lines-you-never-write).

```text
# before                                   # after
acme/                                      acme/
  yolo-jail.toml   (the 133-line template)
  .gitignore       (+ ".yolo/")
```

## 3. A team project

Today:

```jsonc
{
  "packages": ["postgresql", "just"],
  "mise_tools": { "node": "22", "python": "3.12" },
  "mounts": ["~/code/acme-shared"],
  "network": { "ports": ["8000:8000"], "forward_host_ports": [5432] },   // HOST:JAIL, then JAIL:HOST
  "gpu": { "enabled": true },
  "resources": { "memory": "16g", "cpus": 8 },
  "brokered": { "github": { "repos": ["acme/shared"] } }
}
```

Proposed:

```toml
[install]
nix = ["postgresql", "just"]
mise = { node = "22", python = "3.12" }   # "3.12" stays a string, in TOML and in YAML's absence

mounts = ["~/code/acme-shared"]           # read-only; a bare path with no colon is still legal

[machine]
gpu = true                                # warns and starts without one on a machine that has none
resources = { memory = "16g", cpus = 8 }
ports = [{ host = 8000, jail = 8000 }]           # the host reaches the jail's :8000
forward = [{ jail = 5432, host = 5432 }]         # the jail reaches the host's postgres at localhost:5432

[needs.github]
repos = ["acme/shared"]                   # beyond this repository's own remotes

[needs.git]
new_branches = true                       # branches of its own, never main; each is approved at launch
```

Asking past the ceiling is refused at `yolo check`, naming both lines:

```console
$ yolo check
yolo-jail.toml:16: [needs.git] new_branches = true asks to push, and your config's allow.git.push is false.
  Allow it for every project: yolo allow git push        (or leave it, and this project cannot push)
```

## 4. Values that were strings

```jsonc
"mounts": ["~/x:/ctx/x:ro"]                    // one host path named "~/x:/ctx/x:ro", skipped with a warning
```

```toml
[[mounts]]
host = "~/x"
at = "/ctx/x"                                  # read-only unless `write = true`, which only your config may say
```

```jsonc
"env_sources": ["~/.config/yolo-jail/secrets.env", { "AWS_PROFILE": null, "PAGER": "cat" }]
```

```toml
[env]
files = ["~/.config/yolo-jail/secrets.env"]
set = { PAGER = "cat" }
unset = ["AWS_PROFILE"]
```

## 5. The local file, and turning something off

`yolo-jail.local.toml` is this machine's and uncommitted, merged after the project file as today.
It is where a project turns off something your own config added:

```toml
# This machine only; not committed.
[mcp_servers.postgres]
command = ["npx", "-y", "@modelcontextprotocol/server-postgres", "postgresql://localhost/acme"]

[mcp_servers.tavily]
off = true                        # your config adds it; not wanted here        (OQ-RC7, option A)

[machine]
resources = { memory = "32g" }
```

## 6. Principles

1. **A project asks; your config allows.** `needs` here must fit inside `allow` there, and every
   ask is a line in the launch approval prompt, as today.
2. **A project file is the agent's to edit**, so nothing that decides where inference goes or
   what a jail may do with your logins is accepted here; `yolo check` refuses it naming the file
   it belongs in.
3. **A direction is a key, not a position in a string.** `{ host = 8000, jail = 8000 }`.
4. **Off is a word at the place**, never a `null` ([OQ-RC7](#OQ-RC7)).
5. **No file is the common case.**

## 7. Open questions

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
   [run_on]
   claude = false
   [needs.github]
   repos = []                        # an empty list clears a list

   # B  one reserved string, valid wherever a value may be removed  (against principle 5 of config-user.md)
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
