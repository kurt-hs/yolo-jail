---
title: "Pack manifests: the same rules, shown on the maintainer's own packs"
date: 2026-10-25
status: in-review
stage: DESIGN
next: "Rule OQ-RC19 (§4, the manifest shape) and OQ-RC5 (§5, in the same break as the configs?)"
tags: [packs, manifest, toml, conventions, design]
summary: "Each of the maintainer's twelve packs, before and after: a manifest-less folder for the simple ones, a short TOML manifest for the rest, models declared once, and flags that add instead of replace. Each section ends with its question; a section with none is approved. Part of the readable-config redesign."
---

# Pack manifests: the same rules, shown on the maintainer's own packs

**Status:** 2026-10-25. Nothing is built. The "today" manifests are the maintainer's twelve
packs, read on 2026-10-24.

**How to read this.** [§1](#1-a-small-pack-luna-worker) to [§4](#4-flags-that-add-and-a-model-said-once)
are the shape, pack by pack, and [§4](#4-flags-that-add-and-a-model-said-once) ends with the
question that approves it; [§5](#5-the-migration) ends with the question of when it lands.

**Needs your ruling:** [OQ-RC19](#OQ-RC19) in [§4](#4-flags-that-add-and-a-model-said-once),
[OQ-RC5](#OQ-RC5) in [§5](#5-the-migration).

> **In short.** A pack's folders say what it is; the manifest says only what a folder cannot.
> Four of the twelve packs need no manifest at all, and the two big ones lose four-fifths of
> their lines without losing a fact.

**Reads with:** [`readable-config.md`](readable-config.md) (front door and ledger),
[`config-migration.md`](config-migration.md) (`yolo pack migrate` and the release order),
[`manifest-language.md`](manifest-language.md) and [`pack-conventions.md`](pack-conventions.md)
(whose questions this takes over).

---

## 1. A small pack: `luna-worker`

Today, 23 lines for one setting:

```jsonc
{
  "name": "luna-worker",
  "description": "Work machines only (listed by tag-work's work.jsonc): pi-subagents' worker role runs on GPT-6 Luna whenever the parent pi session is on Amazon Bedrock; …",
  "contributes": [
    { "kind": "config-overlay", "surface": "pi/settings",
      "config": { "managed": { "subagents": { "agentOverridesByProvider": { "amazon-bedrock": {
        "worker": { "model": "amazon-bedrock/global.openai.gpt-6-luna" } } } } } } }
  ]
}
```

Proposed: a folder, no manifest, one file in pi's own format.

```text
luna-worker/
  overlays/pi-settings.jsonc
  README.md                         # its first line is the description
```

```jsonc
// Work machines: pi's worker subagent runs on GPT-6 Luna when the session is on Bedrock.
{"subagents": {"agentOverridesByProvider": {"amazon-bedrock": {"worker": {"model": "amazon-bedrock/global.openai.gpt-6-luna"}}}}}
```

`coordinator-reads-blocked` is the same shape; `workspace-questions` is a `briefing/` folder and
nothing else; `claude-bedrock-global` is `overlays/claude-settings@bedrock.jsonc`, the `@bedrock`
being the profile gate its manifest carries today.

## 2. A pack with programs: `matt-fzf`

Today, 56 lines: three programs with five install hints each, a files entry, an overlay.

```jsonc
{ "kind": "program", "bin": "fd",
  "install_hints": { "brew": "fd", "apt": "fd-find", "dnf": "fd-find", "pacman": "fd", "nix": "fd" } },
{ "kind": "program", "bin": "fzf",
  "install_hints": { "brew": "fzf", "apt": "fzf", "dnf": "fzf", "pacman": "fzf", "nix": "fzf" } },
{ "kind": "files", "from": "bin", "into": ".claude/bin" },
{ "kind": "config-overlay", "surface": "claude/settings",
  "config": { "managed": { "fileSuggestion": { "type": "command", "command": "~/.claude/bin/file-suggestion.sh" } } } }
```

Proposed, `pack.toml`, 14 lines, with `bin/` moved to `home/.claude/bin/`:

```toml
description = "Matt's fd|fzf custom file finder for Claude Code's @-file completion."

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

## 3. No manifest at all

Approved 2026-10-25 as convention C6 ([`config-user.md` §8](config-user.md#8-lines-you-never-write)).
Folders say what they are; `pack.toml` is for what a folder cannot say: a program to install, a
source to fetch and patch, a build, a model.

```text
<pack>/
  home/            laid into the home directory, file by file; merges with other packs' files
  skills/          as today
  briefing/        as today
  overlays/        <surface>[@<profile>].<ext>, in the agent's own format
  patches/         as today
  pack.toml        only if something above cannot say it
```

Under it, 25 of `matt`'s 39 `files` entries (127 lines) vanish into `home/.pi/agent/extensions/`,
and its seven overlay bodies (425 lines, pi's and claude's own settings) move to `overlays/`.
For pi, a shorter route already shipped: one entry `{"kind": "files", "agents": ["pi"], "from":
"files/pi"}` loads a folder as a pi package ([OQ-PR1](../reference/pack-system.md#oq-pr1), ruled
2026-10-05); `matt` predates it.

## 4. Flags that add, and a model said once

`agy-capture` today restates `--dangerously-skip-permissions` because a later pack's flag list
replaces an earlier one's whole; delete the line and agy prompts for every tool. Proposed, a
contribution states only what it changes, and replacing is said in as many words:

```toml
[[launch]]
bin = "agy"
flags = ["--model", "gemini-3.8-flash-medium"]     # adds to the agy pack's list; replace = true would not
```

`meta-muse` today writes six model rows into pi's `models.json` format, 199 lines, a seven-level
thinking map six times. Proposed, in yolo's model shape, for every agent:

```toml
[providers.meta.every_model]
api = "openai-responses"           # new fact: the wire API
thinking = ["minimal", "low", "medium", "high", "xhigh", "max"]   # new fact
accepts = ["text", "image"]
max_tokens = 128_000

[providers.meta.models."muse-spark-1.3-contributor"]
name = "Spark 1.3"
context = 1_048_576
images = { max_width = 2000, max_height = 2000, max_bytes = 4_718_592, jpeg_quality = 80 }   # new fact
price = { input = 0.1, output = 0.2, cache_read = 0.002, cache_write = 0 }

[providers.meta.models]
"muse-wonder-1.0-contributor"            = { name = "Wonder 1.0",      context = 1_007_997 }
"muse-wonder-1.0-contributor[200k]"      = { twin_of = "muse-wonder-1.0-contributor", compact_at = 200_000 }
```

`twin_of` takes every fact of the named row and sends its id on the wire; each agent's render
writes its own file from these rows.

1. 💬 <a id="OQ-RC19"></a>**OQ-RC19: Is the manifest shape approved, lever by lever?**

   You open `matt/pack.json` to see what the pack does and meet 860 lines, 271 of them 39 `files`
   entries and 425 of them pi's settings written inline; in `agy-capture`, deleting one restated
   flag would make agy prompt for every tool; `meta-muse` writes the same model facts six times.
   [§1](#1-a-small-pack-luna-worker) to [§4](#4-flags-that-add-and-a-model-said-once) show the
   six levers on those packs; each can be vetoed alone.

   ```toml
   # P1  home/ is laid into the home directory, file by file          (32 of matt's 39 files entries go)
   # P2  an overlay's content lives in its own file, in the agent's own format   (425 lines leave the manifest)
   # P3  grouped by kind, the pack's identity stated once
   [[program]]
   bin = "fd"
   # P4  a contribution states only what it changes; replace = true says otherwise
   # P5  a model is declared once, in yolo's shape, for every agent  (twin_of, thinking, images, api)
   # P6  the obvious is not written
   install = "jq"                                   # one hint when every manager agrees
   ```

   <!-- vantage: question id=OQ-RC19 leaning="Approve all six: the four smallest packs become folders, and the two largest keep every fact in a fifth of the lines." -->

   _Leaning:_ Approve all six: the four smallest packs become folders, and the two largest keep
   every fact in a fifth of the lines.

   **Answer:**
   > _(empty — fill in when decided)_

## 5. The migration

`yolo pack migrate <path>` rewrites the manifest, keeps comments as the config migration does,
moves plain-copy `files` entries into `home/` or a pi package folder, and uses `git mv` in a
checkout, so history follows. A local pack in the old format is refused naming the command; a
fetched one is [OQ-RC8](config-migration.md#OQ-RC8). A yolo of 0.11.1 or earlier refuses any new
manifest field outright, so a new manifest shape is a new file name however it is done.

1. 💬 <a id="OQ-RC5"></a>**OQ-RC5: Should pack manifests change in the same breaking change as the configs?**

   You upgrade yolo. If the manifests change with the configs, one release pair covers both and
   `yolo pack migrate` runs beside `yolo config migrate`; if the configs go first, your config is
   new while your packs are old for a release, and you migrate twice.

   ```text
   # A  together: one migration, one release pair
   release N    knows config.toml, yolo-jail.toml, pack.toml
   release N+1  reads only them; config migrate and pack migrate

   # B  configs first, packs next: two migrations; the configs land sooner
   release N    knows config.toml, yolo-jail.toml
   release N+1  reads only them
   release N+2  knows pack.toml …

   # C  packs stay as they are; the 860-line manifest stays
   ```

   <!-- vantage: question id=OQ-RC5 leaning="A: one break, and the migrate command does the moving; packs are the bigger files and the same rules apply." -->

   _Leaning:_ A: one break, and the migrate command does the moving; packs are the bigger files
   and the same rules apply.

   **Answer:**
   > _(empty — fill in when decided)_

## 6. Principles

1. **A folder is the declaration.** A manifest names only what a folder cannot.
2. **Big content lives in its own file, in its owner's format.** An overlay body is pi's or
   claude's settings, not yolo's.
3. **A contribution states only what it changes.** Lists add and objects merge; `replace = true`
   says otherwise.
4. **A model is declared once, in yolo's shape, for every agent.**
5. **The obvious is not written.** One install hint when every manager agrees; `codec` from the
   extension; `scope = "workspace"` and `run = bin` as defaults. Shipped packs keep the
   per-manager form, which keeps the test that every shipped hint cites its source.
6. **A pack is named by where it comes from**, never by its manifest
   ([`config-user.md` §4](config-user.md#4-naming-a-pack-that-is-not-shipped)).

## Appendix: evidence

### A.1 The twelve packs

1,688 manifest lines, 394 of them comments. `matt` is 860 lines and `meta-muse` 512; the other ten
run from 9 to 74. By contribution kind, `matt` is `config-overlay` 7 entries / 425 lines,
`files` 39 / 271, `config` 1 / 57, `autonomy` 1 / 42, the other five kinds 8 / 59.

Most comments explain yolo, not the pack: `agy-capture` gives 24 of its 74 lines to two warnings
about flag lists replacing (`launchFlagClaims` in `internal/packload/packload.go` assigns, it does
not append) and overlays replacing objects. Both large packs also carry long comments on a key
that pi's derive overwrites silently at every boot (`subagents.defaultModel` and its siblings),
which `yolo config render` cannot show because it omits the computed layer; that is a `pack lint`
finding, in the seed, not a shape change.

### A.2 Why ten files take fifty lines

A `files` entry is a read-only mount over its whole destination, and two entries claiming one
path refuse the launch ([`pack-system.md`](../reference/pack-system.md), the `files` section), so
a pack cannot hand over a folder another pack also writes into. `matt` copies 25 files into
`~/.pi/agent/extensions/` with 25 entries, 127 lines, each writing its path twice. The list form
for a folder that should not mirror the home:

```toml
[[files]]
from = "files/extensions"
into = ".pi/agent/extensions"
only = ["turn-bell.ts", "versions.ts", "board", "work-control"]
```

### A.3 The compression exercise

Comments removed and minified, then gzip −9; ratio is compressed over minified, lower meaning
more repetitive; the prose baseline is `what-yolo-is.md` cut to the same size.

| File | Minified | gzip ratio | Prose, same size |
| :--- | ---: | ---: | ---: |
| `matt` | 15,264 | **0.24** | 0.40 |
| `meta-muse` | 4,513 | **0.25** | 0.43 |
| `matt-mods` | 1,224 | **0.37** | 0.56 |
| `agy-capture` | 801 | 0.53 | 0.60 |
| `matt-fzf` | 667 | 0.47 | 0.61 |
| the seven smallest | 203–574 | 0.59–0.74 | 0.64–0.74 |
| `matt`'s 25 extension copies | 2,794 | **0.15** | — |
| `meta-muse`'s six model rows | 2,608 | **0.18** | — |

The small packs are not repetitive; their trouble is per-line noise. The repetition is the
extension copies and the model rows. Before and after: `matt-fzf` 56 → 14 lines; the 25
extension copies 127 → 30 as a list entry, or 0 under `home/`; `matt` whole 15,264 minified
bytes → 5,315 with P1 and P2. The ratio cannot be the target: a reshaped file compresses *worse*
because the repeats gzip was discarding are gone, so the scorecard is lines a person reads, facts
written more than once, and a compressed size that stays flat.

### A.4 What this takes over

[OQ-M1](manifest-language.md#OQ-M1) and [OQ-M2](manifest-language.md#OQ-M2) by
[OQ-RC5](#OQ-RC5) and [OQ-RC2](readable-config.md#decision-ledger), ruled TOML; [OQ-M3](manifest-language.md#OQ-M3)
stays there. [OQ-PC1](pack-conventions.md#OQ-PC1) to [OQ-PC3](pack-conventions.md#OQ-PC3) by the
`home/` folder. [`pack-conventions.md`](pack-conventions.md#appendix-a-counts) counted ten
single-file `files` entries in `matt` on 2026-10-05 and found about five lines saved; that count
covered pi-path entries only, where this one covers every copy-only entry across his packs, 39.
Its rejections of a one-value `install_hints` and of a provider implying its profile were its own
verdicts, not the maintainer's rulings; the second is now ruled the other way
([`config-user.md` §5](config-user.md#5-which-setup-each-agent-runs-on)).
