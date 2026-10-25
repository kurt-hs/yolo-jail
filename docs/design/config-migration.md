---
title: "Moving to the new config: the file name is the version, and one command converts everything"
date: 2026-10-25
status: in-review
stage: DESIGN
next: "Rule OQ-RC11 (§2), OQ-RC6 (§3), OQ-RC18 (§4) and OQ-RC8 (§5)"
tags: [config, migration, versioning, release, design]
summary: "How a yolo knows which config it has, the order of releases, what `yolo config migrate` does and proves, and what every machine sees along the way, shown as transcripts. Each section ends with its question. Part of the readable-config redesign."
---

# Moving to the new config: the file name is the version, and one command converts everything

**Status:** 2026-10-25. Nothing is built. The transcripts are what a person would see under the
leanings.

**How to read this.** [§1](#1-what-youd-see) is the transcripts; [§2](#2-how-a-yolo-knows-which-config-it-has)
to [§5](#5-every-case) each end with their question.

**Needs your ruling:** [OQ-RC11](#OQ-RC11) in [§2](#2-how-a-yolo-knows-which-config-it-has),
[OQ-RC6](#OQ-RC6) in [§3](#3-the-order-of-releases), [OQ-RC18](#OQ-RC18) in
[§4](#4-what-yolo-config-migrate-does), [OQ-RC8](#OQ-RC8) in [§5](#5-every-case).

> **In short.** `config.toml` beside no `config.jsonc` is the whole version scheme. One release
> teaches old yolos the new name; the next reads only it and offers the conversion, which refuses
> to write unless the config it produces is byte-equal to the one it read.

**Reads with:** [`readable-config.md`](readable-config.md) (front door and ledger),
[`config-user.md`](config-user.md) and [`config-project.md`](config-project.md) (what the files
become), [`pack-manifests.md`](pack-manifests.md) (the pack half of the same move).

---

## 1. What you'd see

The migration, on the host:

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
  (170 lines → 17; 3 comments kept, in place)
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

An older yolo meeting the new file, once it is the release that knows the name:

```console
$ yolo
~/.config/yolo-jail/config.toml is for a newer yolo than this one (0.16.2).
Upgrade with `yolo update`, or `just install` from a checkout. Nothing was started.
```

A new yolo meeting an old file:

```console
$ yolo
~/.config/yolo-jail/config.jsonc is in the old format. Convert it now? [y/N]
  (shows the diff above, writes on y, and continues the launch)
```

An old key in a new file:

```console
$ yolo check
~/.config/yolo-jail/config.toml:14: `brokered` is no longer a key. Its lines are now:
  [allow.git]
  always = ["fetch"]
```

## 2. How a yolo knows which config it has

Three ways to mark a generation, and they are not equivalent. Today there is no version field;
old files are recognized only by content digests, and an unknown top-level key is already a hard
error on the host.

```text
# A  the file name
~/.config/yolo-jail/config.toml            old yolo: "for a newer yolo: upgrade" (release N), or no config at all (older)
~/.config/yolo-jail/config.jsonc           new yolo: "old format; convert now?"

# B  a version key, first in a same-named file (only possible if the format stays JSONC)
{ "yolo": 2, ... }                         old yolo: `config.yolo: unknown key`, naming the key, not the upgrade

# C  both: the name now, and a key reserved for the next shape change inside TOML
yolo = 2
```

Every surveyed tool that changed its config shape converged on the file name: ESLint's flat config
is a new file name plus a migrate command; Docker Compose's `version` key switched semantics
between 2 and 3, confused users for years, and is now ignored with a warning; Ruff kept reading
old keys forever with a warning and needed neither. A version key earns its place only when the
*same* file will change shape again within one format.

1. 💬 <a id="OQ-RC11"></a>**OQ-RC11: Is the file name the version, or a key inside the file?**

   You said: *"if we change file names here that would allow us a pretty easy transition plan. We
   can version it. I suppose we can put a version field in the JSON that we read first."* A
   colleague on last month's yolo pulls dotfiles migrated on another machine. With a new name,
   their yolo finds no `config.jsonc` and starts a jail with no packs, unless it is the release
   that knows the new name and says "upgrade". With a version key in a same-named file, their
   yolo reads it and fails on the first unknown key, naming the key.

   - **A — The file name.** No key. A format change has to change the name anyway. The release
     before the change teaches old yolos the name.
   - **B — A version key**, `yolo = 2`, first in the file, same name. Only possible if the format
     stays JSONC.
   - **C — Both.** The name now, and a key reserved for the next shape change inside TOML.

   <!-- vantage: question id=OQ-RC11 leaning="A: the name is the version, as ESLint did; a key is added the day a same-format shape change needs one, and not before." -->

   _Leaning:_ A: the name is the version, as ESLint did; a key is added the day a same-format shape
   change needs one, and not before.

   **Answer:**
   > _(empty — fill in when decided)_

## 3. The order of releases

1. **Release N recognizes the new names and refuses them.** A `config.toml`, `yolo-jail.toml` or
   a pack folder holding `pack.toml` stops the launch with "this file is for a newer yolo". It
   changes nothing else, so it ships long before the rest. The pack case matters: without it an
   older yolo treats a migrated pack as manifest-less and silently stages only its files.
2. **Release N+1 reads only the new format** and refuses an old file, offering the conversion.
3. **Every machine that shares dotfiles upgrades to N before any of them migrates**, because one
   machine's migration rewrites the file every other machine reads.

1. 💬 <a id="OQ-RC6"></a>**OQ-RC6: What happens on a machine still running an older yolo?**

   A colleague migrates with the new yolo, then runs a jail from an older yolo installed
   elsewhere, or your Mac is a release behind. The older yolo looks for `config.jsonc`, finds
   none, and launches a jail with no packs, saying only that it has no coding agent; a migrated
   local pack it stages as if it had no manifest.

   ```text
   # A  migrate keeps the old file beside the new, marked superseded; an older yolo works, and drifts
   config.toml
   config.jsonc        # superseded by config.toml on 2026-11-02; not read by yolo ≥ 0.17

   # B  migrate removes the old file; an older yolo starts empty and says so
   config.toml

   # C  ship a release that knows the new names first; an old-enough yolo refuses, naming the upgrade
   config.toml         →  "config.toml is for a newer yolo than this one. Upgrade with `yolo update`."
   ```

   <!-- vantage: question id=OQ-RC6 leaning="C: one release that recognizes the new names and refuses with upgrade yolo, then the migrating release; no silently empty jail, no drifting copy." -->

   _Leaning:_ C: one release that recognizes the new names and refuses with "upgrade yolo", then the
   migrating release; no silently empty jail, no drifting copy.

   **Answer:**
   > _(empty — fill in when decided)_

## 4. What `yolo config migrate` does

1. **Proves it lost nothing.** Effective config before (old files) and after (the files it would
   write), as canonical JSON in the new vocabulary; it refuses to write unless they are equal, and
   prints any difference key by key.
2. **Rewrites each file where it is, and never merges.** `config.jsonc` → `config.toml`, each
   include → its `.toml` twin with the include entries respelled; through a symlink, so the
   dotfiles copy changes; a file it cannot reach is listed with the command to run where it can;
   each original kept as `<name>.pre-migrate`.
3. **Keeps comments, in order.** A comment is never read as belonging to a key; each is written
   back in document order, before whatever key follows it in the new file, and nothing is dropped.
4. **Drops written defaults and folds repeated facts**, each as its own hunk, only while the
   equality check holds.
5. **Respells the records only yolo reads**: the approval records under
   `~/.local/share/yolo-jail/approvals/` and each workspace's `.yolo/config-boot.json`. Without
   this every migrated workspace meets a full-file approval prompt once, and a launch with no
   terminal is refused until someone answers. A record it cannot read is left, and that workspace
   prompts once, which the output says.
6. **Deletes a zero-byte `config.lua`** and names it; refuses a non-empty one, naming it.
7. **Runs once, on request.** The launch offers it; nothing runs it unasked. On the host for the
   user files; in a jail only for the project's.

1. 💬 <a id="OQ-RC18"></a>**OQ-RC18: Is what the migration does approved?**

   You run `yolo config migrate` on a work machine and see the transcript in [§1](#1-what-youd-see):
   four files found, one named as missing with the command to run elsewhere, a zero-byte
   `config.lua` to delete, one diff summary per file, the equality line, the records respelled,
   and the `y/N`. The seven behaviors above are what produces it; each can be vetoed alone.

   - **A — Approve all seven.**
   - **B — Approve, except:** name the behavior to change (for instance, do not fold facts or drop
     defaults; convert the spelling only, and leave the shrinking to a later `yolo config tidy`).
   - **C — Convert nothing automatically**; print the new files and let the person save them.

   <!-- vantage: question id=OQ-RC18 leaning="A: the equality check is what makes the folds safe, and a conversion that leaves the shrinking for later leaves the misreadings in place for as long." -->

   _Leaning:_ A: the equality check is what makes the folds safe, and a conversion that leaves the
   shrinking for later leaves the misreadings in place for as long.

   **Answer:**
   > _(empty — fill in when decided)_

## 5. Every case

- **Both `config.toml` and `config.jsonc` in one place:** refused, naming both and the command.
  Formats are never merged.
- **An old key in a new file:** refused, naming the replacement already respelled. No aliases.
- **A project file in a jail:** an agent may run the migration on it.
- **Shipped packs:** migrated in the commit that changes the reader.
- **A local pack in the old format:** refused, naming `yolo pack migrate <path>`
  ([`pack-manifests.md`](pack-manifests.md#5-the-migration)).
- **A yolo older than this change:** [OQ-RC6](#OQ-RC6).
- **Files only yolo reads** stay JSON in the new vocabulary; a drift report says `allow.git.push`,
  never `brokered.git.grants`. The jail's inherited copy of the user config becomes TOML.
- **An error in a new file** names `file:line:col` and the key.
- **The repository's own references:** over 400 Go files name the old file names, and only the
  workspace name has a constant; one fixture helper is what stops the next format change from
  touching 400 files (in the seed).
- **A fetched pack in the old format:** the one case left open.

1. 💬 <a id="OQ-RC8"></a>**OQ-RC8: What happens to a pack fetched from someone else's repository that still has an old `pack.json`?**

   A colleague publishes a pack on GitHub and you list it. You upgrade to the release that reads
   only `pack.toml`; the colleague has not migrated. Today the one fetched pack in your list,
   `matt-craft`, has no manifest at all, so it is unaffected; a colleague's pack would be.

   ```console
   # A  yolo keeps an old-manifest reader for fetched packs only, converting in memory; two readers
   $ yolo
   acme/tools: pack.json read in compatibility mode; ask its author to run `yolo pack migrate`.

   # B  refuse, naming the two ways forward; one reader
   $ yolo
   acme/tools (https://github.com/acme/tools) has a pack.json this yolo no longer reads.
     Ask its author to run `yolo pack migrate`, or fork it and run that yourself. Nothing was started.

   # C  B, after one release of A with a warning
   ```

   <!-- vantage: question id=OQ-RC8 leaning="B: one reader; no fetched pack in use today has a manifest, and the refusal names both ways forward." -->

   _Leaning:_ B: one reader; no fetched pack in use today has a manifest, and the refusal names
   both ways forward.

   **Answer:**
   > _(empty — fill in when decided)_

## 6. Principles

1. **The name is the version.** `.jsonc` is old, `.toml` is new; nothing inside a file says
   which yolo wrote it until a same-format shape change needs it.
2. **A conversion proves equality or writes nothing.**
3. **No aliases, ever.** An old spelling is refused naming the new one.
4. **Nothing migrates unasked.** The launch offers; a person answers.
5. **The step before the break is a release that only knows the names.**

## Appendix: evidence

- **No version field exists today**, and old files are recognized only by content digests
  (`internal/config/launchorigins.go`, `drift.go`). Unknown top-level keys are a hard error on the
  host; retired keys (`use_profiles`, `agents` and six others) are refused each with its
  replacement (`internal/config/validate.go`); nothing rewrites a config file.
- **The approval snapshot is canonical JSON** that must not change by one byte, or every
  workspace prompts ([`config-safety.md`](../reference/config-safety.md)); renaming keys changes
  it, which is why step 5 of [§4](#4-what-yolo-config-migrate-does) exists.
- **Prior art on versioning.** ESLint's flat config is a new file name plus a migrate command.
  Docker Compose's `version` key switched semantics between 2 and 3, confused users for years,
  and is now ignored with a warning. Ruff kept reading old keys forever with a warning. Renovate
  migrates in memory and writes back only through an opt-in PR. Cosmos SDK's `confix` is a Go
  tool that migrates TOML config between versions. The lesson each tool drew: change the name for
  a real shape change, make the rewrite explicit and reviewable, and avoid a version key that
  changes semantics.
- **Manifests at yolo ≤ 0.11.1 are decoded strictly**, so any new manifest field refuses the pack;
  a new manifest shape is a new file name however it is done, and an older yolo meeting a folder
  with only `pack.toml` treats it as manifest-less (`matt-craft` is that today).
- **A zero-byte `~/.config/yolo-jail/config.lua` exists on at least one machine**; the Lua
  transform that read it is gone (`internal/config/inherit.go`).
- **Include paths are relative to the including file**, and one starting with `~` or `/` is
  refused (`internal/config/load.go`); the migration respells each entry in place.
