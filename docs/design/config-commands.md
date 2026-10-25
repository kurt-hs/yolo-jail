---
title: "Who writes the file: one command instead of 'edit your config', and the format it writes"
date: 2026-10-25
status: in-review
stage: DESIGN
next: "Rule OQ-RC9 (§3, may yolo write your file), then OQ-RC10 and OQ-RC12 (§4, the verbs) and OQ-RC2 (§5, the format)"
tags: [config, cli, editing, toml, jsonc, design]
summary: "About sixty places tell a person or an agent to hand-edit a config file. This doc shows the command each would print instead, what it does to the file (one hunk, every other byte untouched), the verbs, and the format underneath. Each section ends with its question. Part of the readable-config redesign."
---

# Who writes the file: one command instead of "edit your config", and the format it writes

**Status:** 2026-10-25. Nothing is built. The sessions below are what a person or an agent would
see under the leanings; the spellings change with [§4](#4-the-verbs)'s questions.

**How to read this.** [§1](#1-what-youd-see) is the transcripts; [§2](#2-the-sixty-places-this-replaces)
is what they replace; [§3](#3-what-a-command-does-to-the-file), [§4](#4-the-verbs) and
[§5](#5-the-format-underneath) each end with their question.

**Needs your ruling:** [OQ-RC9](#OQ-RC9) in [§3](#3-what-a-command-does-to-the-file),
[OQ-RC10](#OQ-RC10) and [OQ-RC12](#OQ-RC12) in [§4](#4-the-verbs), [OQ-RC2](#OQ-RC2) in
[§5](#5-the-format-underneath).

> **In short.** A command you typed changes the bytes of one value in the file you named, and
> nothing else. That is what makes editing your config not a mess, and it works in TOML or JSONC.

**Reads with:** [`readable-config.md`](readable-config.md) (front door and ledger),
[`config-user.md`](config-user.md) and [`config-project.md`](config-project.md) (the files these
commands write), [`config-migration.md`](config-migration.md) (the one command that rewrites a
whole file, once).

---

## 1. What you'd see

Adding a pack, on the host:

```console
$ yolo pack add local/matt
~/.config/yolo-jail/config.toml
@@ -4,4 +4,5 @@
   "git", "github", "chrome-devtools",
-  "local/matt-fzf", "local/matt-mods", "local/matt-agentwork",
+  "local/matt-fzf", "local/matt-mods", "local/matt-agentwork",
+  "local/matt",
   { git = "https://github.com/mschulkind-oss/matt-craft" },
Added "local/matt". It is read at the next launch.
```

Removing one, and what happens to its comment:

```console
$ yolo pack remove journal
~/.config/yolo-jail/config.toml
@@ -2,6 +2,5 @@
 packs = [
-  # journal: read the host's systemd journal from inside the jail
-  "pi", "local/pi-fork", "codex", "journal", "host-processes",
+  "pi", "local/pi-fork", "codex", "host-processes",
Removed "journal". A comment above it was removed with it:
  # journal: read the host's systemd journal from inside the jail
```

An agent in a jail asking for a tool, which today is a paragraph in the briefing:

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

The same agent touching a user-scope key:

```console
$ yolo config add allow.git.always fetch
allow.git.always is a user-config key, and a jail sees only a generated copy of your user config.
Run it on the host:
    yolo host-run -C /home/matt/code/yolo-jail --name allow-git-fetch -- yolo allow git fetch
```

Granting a read on the host, and the approval that follows, which today is two steps:

```console
$ yolo allow git fetch
~/.config/yolo-jail/config.d/work.toml
@@ -3,2 +3,5 @@
 packs = ["bedrock", "aws-auth", "copilot", "claude"]
+
+[allow.git]
+always = ["fetch"]
Written to config.d/work.toml, the file that already holds [allow].
This takes effect when a workspace approves it. Approve now for this workspace? [y/N] y
  + git: fetch from mschulkind-oss/yolo-jail (from your user config, for every workspace)
Approved. The running jail's broker takes it at its next call.
```

Turning something off in one project:

```console
$ yolo config unset mcp_servers.tavily --local
/workspace/yolo-jail.local.toml
@@ -1,2 +1,5 @@
 # This machine only; not committed.
+[mcp_servers.tavily]
+off = true
Written. Your config adds tavily; this project's jail will not see it.
```

Where did this value come from:

```console
$ yolo config why active_provider.except
active_provider.except = ["claude"]
  set in  ~/.config/yolo-jail/config.d/overrides.toml:9
```

## 2. The sixty places this replaces

Grouped by the edit asked for, most frequent first ([appendix A.1](#a1-the-inventory) has the
sites):

1. add a pack to `packs` (about 14 messages, most pack READMEs)
2. add the broker read grant `{"kind": "read", "repo": "*"}` (one message from about 11 call
   sites, plus the git and gh briefings)
3. turn a loophole on, or set its settings (about 10)
4. add `packages`, `mise_tools` or `resources` (the briefing on every launch)
5. move a user-scope-only key out of the project file (about 7)
6. add or remove a `branch` or `create-branch` grant (about 6)
7. set an `aws-auth` setting (about 6)
8. add a repo to `brokered.github.repos`
9. `programs.overrides`, `runtime`, a provider model, and nine one-off keys

Only the per-workspace loophole switch has a command today (`yolo loopholes enable`), and it
writes its own file. Nothing else in yolo writes a file a person also writes.

## 3. What a command does to the file

A **splice** (coined here) parses the file only to find the byte range of one value, rewrites that
range, and leaves every other byte as it was. It is not a round trip through a parser and a
printer, which is what reformats a file.

1. A new key goes at the end of its table; a new table at the end of the file after one blank
   line; a new list item after the last, with the list's own separator and line-breaking copied
   from its neighbors; a removed key takes its trailing comma or newline and the comment lines
   directly above it, and prints those comments.
2. Indentation is copied from the nearest sibling; in an empty file, two spaces for JSONC and
   none for TOML.
3. It writes to a temporary file and renames, as the per-workspace file does. Two commands at
   once serialize on one lock; the second re-reads and re-splices.
4. After the write it re-parses the result and compares the effective config with the one it meant
   to produce. A mismatch restores the original bytes and refuses, printing both.
5. An item already present is a no-op that says so; so is a key absent on removal.
6. It never reorders, never reformats a line it did not write, never writes a key it cannot
   validate, never touches a file it did not name in its output.
7. `--dry-run` prints the hunk and writes nothing. Without it the command writes and prints the
   hunk.

Where it writes: a user-scope-only key to the user config, in the file that already holds its
section ([`config-user.md` A.3](config-user.md#a3-four-files-not-one)); a key both scopes accept
to the project config; `--user`, `--project`, `--local` and `--file` override; in a jail, a
user-scope key is refused with the `host-run` line. What happens next is what happens today: a
project edit lands at the next `yolo config apply` or launch through the same y/N gate; a
user-config edit is trusted, except a grant, which offers its approval.

Whether yolo may do this to your file at all is the question, because on 2026-10-01 you ruled
that it never edits the user config: *"it always leaves that a mess."* The splice is written to be
the absence of a mess; whether that answers the ruling is yours to say.

1. 💬 <a id="OQ-RC9"></a>**OQ-RC9: May yolo write your config file when you ask it to, and how?**

   A colleague is told, in about sixty places, to add `{"kind": "read", "repo": "*"}` to a list in
   their config, then run `yolo config apply`. Read literally, the ruling leaves yolo printing the
   lines. Read as being about the mess and the side effect, `yolo allow git fetch` can change one
   hunk of their file and leave every other byte alone.

   ```console
   # A  splice on request: the file you named, one hunk, printed
   $ yolo allow git fetch
   ~/.config/yolo-jail/config.toml
   +[allow.git]
   +always = ["fetch"]

   # B  companion file only: your file is never written; removals stay by hand
   $ yolo allow git fetch
   ~/.config/yolo-jail/managed.toml        (written whole by yolo; do not edit)
   [allow.git]
   always = ["fetch"]

   # C  print the edit
   $ yolo allow git fetch
   Add to ~/.config/yolo-jail/config.toml:
   [allow.git]
   always = ["fetch"]
   ```

   <!-- vantage: question id=OQ-RC9 leaning="A: the ruling's complaint was a mess left by a side effect; a one-hunk edit you asked for is neither, and B cannot remove anything because lists merge by union." -->

   _Leaning:_ A: the ruling's complaint was a mess left by a side effect; a one-hunk edit you asked
   for is neither, and B cannot remove anything because lists merge by union.

   **Answer:**
   > _(empty — fill in when decided)_

## 4. The verbs

Two spellings are on the table, and both can exist. Named verbs, one per decision:

```console
$ yolo pack add local/matt                 # user config: packs += "local/matt"
$ yolo pack remove journal
$ yolo install nix just                    # project config: install.nix += "just"
$ yolo install mise python@3.12 --user
$ yolo allow git fetch                     # user config: [allow.git] always += "fetch"
$ yolo request git push main               # project config: [requests.git] push += "main"
$ yolo request github repos acme/shared
$ yolo allow git on_request push           # user config: [allow.git] on_request += "push"
$ yolo active-provider default bedrock     # user config: [active_provider] default = "bedrock"
$ yolo active-provider except claude
```

One generic verb, keyed by path:

```console
$ yolo config add packs local/matt
$ yolo config add allow.git.always fetch
$ yolo config add active_provider.except claude
$ yolo config add requests.git.push main --project
$ yolo config unset mcp_servers.tavily --local
$ yolo config set --dry-run machine.resources.memory 32g
```

Today `yolo config` already means something else: `ls`, `render`, `diff`, `reset`, `promote` and
`capture` are about the agent settings files yolo composes (`~/.claude/settings.json` and the
like), while `apply`, `drift` and `dump` are about yolo's own config. Two questions follow.

1. 💬 <a id="OQ-RC10"></a>**OQ-RC10: Named verbs, one generic `set`, or both?**

   An agent reads "edit `yolo-jail.jsonc` (`packages` / `resources`)" and adds a list item by hand;
   a person reads "add `{"kind": "read", "repo": "*"}`" and pastes it under the wrong key. Either
   could type one line. The question is which line the sixty messages print and which a person
   remembers.

   ```console
   # A  named verbs only; a key with no verb has no command
   $ yolo allow git fetch

   # B  one generic verb only; every key has a command, and a message prints it from the key it knows
   $ yolo config add allow.git.always fetch

   # C  both: B underneath, A as sugar; messages print B, people type A
   ```

   <!-- vantage: question id=OQ-RC10 leaning="C: a message knows the key, a person knows the decision, and the sugar costs little once the generic verb exists." -->

   _Leaning:_ C: a message knows the key, a person knows the decision, and the sugar costs little
   once the generic verb exists.

   **Answer:**
   > _(empty — fill in when decided)_

2. 💬 <a id="OQ-RC12"></a>**OQ-RC12: What does `yolo config` mean once it edits your config?**

   A person who types `yolo config set` expects yolo's own config; the help page today shows the
   agent-settings pipeline under the same verb.

   ```console
   # A  move the six composition commands; yolo config is yolo's config
   $ yolo surface render claude/settings
   $ yolo config add allow.git.always fetch

   # B  leave yolo config alone; the new verbs go elsewhere
   $ yolo config render claude/settings
   $ yolo add allow.git.always fetch

   # C  both meanings under one verb, two sections in the help text
   ```

   <!-- vantage: question id=OQ-RC12 leaning="A: config should mean the user's config, and six retired spellings that print their replacement cost one release of friction." -->

   _Leaning:_ A: `config` should mean the user's config, and six retired spellings that print
   their replacement cost one release of friction.

   **Answer:**
   > _(empty — fill in when decided)_

## 5. The format underneath

The same excerpt in each format:

```jsonc
{ "allow": { "git": { "always": ["fetch"], "on_request": ["push"] } },
  "active_provider": { "default": "bedrock", "except": ["claude"] },
  "providers": { "bedrock": { "region": "us-east-1",
    "models": { "global.openai.gpt-6.1-sol": { "name": "Sol 6.1", "context": 272000 } } } } }
```

```toml
[allow.git]
always = ["fetch"]
on_request = ["push"]

[active_provider]
default = "bedrock"
except = ["claude"]

[providers.bedrock]
region = "us-east-1"
models."global.openai.gpt-6.1-sol" = { name = "Sol 6.1", context = 272_000 }
```

```yaml
allow: { git: { always: [fetch], on_request: [push] } }
active_provider: { default: bedrock, except: [claude] }
providers:
  bedrock:
    region: us-east-1
    models: { global.openai.gpt-6.1-sol: { name: Sol 6.1, context: 272000 } }
```

What decides it, in four lines: the shape removes two-thirds of the lines whatever the format;
TOML keeps its one-line tables when a tool rewrites it and JSONC grows back to 133 lines; YAML
reads `python: 3.10` as the number 3.1, which a mise version is; and both TOML and YAML are already
vendored and used (`internal/tomlx`, `internal/agentcfg/codec`), so neither is a new dependency.
Files only yolo reads stay JSON. A ruling of 2026-09-30 ([MN-D1](manifest-language.md#MN-D1))
said the configs keep JSONC unless a proposal says otherwise; this is that proposal.

1. 💬 <a id="OQ-RC2"></a>**OQ-RC2: TOML, or keep JSONC in the new shape?**

   You open your config to add a model. Either way the reshape takes the file from 178 lines to
   under 60, both formats are in the tree, and a splice edits either.

   ```toml
   # A  TOML, for every file a person writes; a model row stays one line when a tool rewrites it
   models."global.openai.gpt-6.1-sol" = { name = "Sol 6.1", context = 272_000 }
   ```

   ```jsonc
   // B  JSONC, reshaped; the splice is nearest to built (json5.Locate exists); a formatter expands this
   "models": { "global.openai.gpt-6.1-sol": { "name": "Sol 6.1", "context": 272000 } }
   ```

   ```yaml
   # C  YAML; the vendored library is frozen upstream, and a version number can read as a float
   mise: { python: 3.10 }        # 3.1
   ```

   <!-- vantage: question id=OQ-RC2 leaning="A, TOML: low noise, no value misreads, familiar from mise.toml and Cargo, already vendored; the flatter shape avoids its one weakness." -->

   _Leaning:_ A, TOML: low noise, no value misreads, familiar from `mise.toml` and Cargo, already
   vendored; the flatter shape avoids its one weakness.

   **Answer:**
   > _(empty — fill in when decided)_

## 6. Principles

1. **An edit changes the bytes it was asked to and no others.** Comments, indentation, blank
   lines and key order survive because they were never read as anything but bytes.
2. **A command you typed may write your file; a side effect never does.** State yolo writes on
   its own, such as the per-workspace switch, goes in a file yolo owns beside yours.
3. **Every stop prints the command.** Each of the sixty messages prints the one line that does
   the edit, or the `host-run` line when it must happen elsewhere.
4. **A write finishes the step.** It runs `yolo check` after itself, and a grant offers its
   approval.
5. **What a person reads is in a person's format; what only yolo reads is JSON.**

## Appendix: evidence

### A.1 The inventory

About sixty sites in `internal/` and `cmd/`, two always-on briefing sections (the git and gh
packs'), the two built-in skills, and the pack READMEs. The read-grant message is one function,
`BrokerGrantAdvice` in `internal/config/brokered.go`, reached from the broker daemons, the
loophole commands and the validators; the tool-request paragraph is `internal/jailcontent/briefing.go`;
the gh-repo message is `internal/ghbroker/scope.go`. `yolo init` refuses to touch an existing
file, and `yolo config promote` refuses a `pack.jsonc` outright because rewriting it through a
JSON encoder would drop every comment.

### A.2 What exists for a splice

- `json5.Locate` (`internal/json5/locate.go`) returns the byte range of a value by key path; its
  own comment says it exists for in-place edits; nothing outside its tests calls it.
- `internal/entrypoint/tomltrivia.go` scans TOML comments by position for the prism's
  read-modify-write and reattaches them, unless the value under a comment changed; `yamltrivia.go`
  does the same through the node tree.
- Every JSON write in yolo rebuilds the whole file.
- Config lists merge by union (`internal/config/load.go`), which is why a companion file can add
  and never remove.
- Parse errors today name a byte offset only (`internal/json5/json5.go`); the index a splice
  needs is the same index that gives `line:col`.

### A.3 Libraries, if one were wanted

No Go library edits TOML the way Cargo's `toml_edit` does. `creachadair/tomledit` keeps comments
but is pre-1.0, says it is not production-ready, and reformats spacing on its first write (Cosmos
SDK's `confix` uses it). `pelletier/go-toml/v2/unstable` reports positions with comments kept and
has no writer, which is all a splice needs. `tailscale/hujson` round-trips JSONC and applies
patches, but its `Format` is opinionated. `yaml.v3` is frozen upstream. None is required for the
splice; one position-reporting parse per format is.

### A.4 The ruling this meets

[OQ-BB12](boundary-broker.md#15-decision-ledger), 2026-10-01: *"I don't want to do anything that
edits the user config directly. We can edit a file that lives next to it of a different name or
whatever … I don't want to do anything ugly like trying to edit the user's configuration because
it always leaves that a mess."* Built as the per-workspace switch file; five sibling docs restate
it. [OQ-RC9](#OQ-RC9) is where the maintainer says whether a splice falls inside it.

### A.5 Format facts

`BurntSushi/toml` v1.6.0 and `gopkg.in/yaml.v3` are in `go.mod` and used by `internal/tomlx`,
`internal/agentcfg/codec` and the prism. Measured on the maintainer's config: today's shape 178
lines; the new shape 58 in hand-laid JSONC, 133 formatter-expanded, 53 in TOML.
