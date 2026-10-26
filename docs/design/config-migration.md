---
title: "Moving to the new config: the file name is the version, and one command converts everything"
date: 2026-10-25
status: in-review
stage: DESIGN
next: "Rule OQ-RC11 (§2, write a format key now or only know it), OQ-RC6 (§3), OQ-RC18 (§4) and OQ-RC8 (§5)"
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

> **In short.** `config.toml` beside no `config.jsonc` is the whole version scheme for this move.
> The next release reads both formats, and keeps the old reader for a few releases; it offers the
> conversion, which refuses to write unless the config it produces is byte-equal to the one it
> read, and one key in the old user file says "later" so the offer stops; a later release drops
> the old reader.

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

A yolo from before the TOML release meeting a migrated config. Nothing can be shipped to it:
today's yolo has no idea a `config.toml` exists, so it finds no `config.jsonc` and starts with no
packs, saying today's line and nothing about the file beside it:

```console
$ yolo
No packs are configured, so this jail has no coding agent.
```

That is why the migration is the user's act, taken once every machine sharing the files runs the
TOML release ([§3](#3-the-order-of-releases)), and why the offer does not nag: the next release
meeting an old file offers once per launch until the person converts, or says "later" with one key
in the old user file, added by hand. Ruled 2026-10-25: *"force the user to add something to their
user level config file to acknowledge that they're using the old version so that it doesn't bug
them every time … They have to add that. It's an annoyance for them if they don't want to
convert. That's fine."*

```console
$ yolo
~/.config/yolo-jail/config.jsonc is in the old format. Convert it now? [y/N] n
Launching with the old file; this yolo reads it until 0.20. `yolo config migrate` converts it once
  every machine that shares it runs 0.17 or newer. To stop this prompt until then, add
  "convert_later": true to ~/.config/yolo-jail/config.jsonc.
```

(`y` shows the diff above, writes, and continues the launch. With no terminal the offer prints as
one line and the launch continues on the old file. `"convert_later": true` in the old user file
silences the offer for every old file this machine reads; no command writes it. The version
numbers are illustrative.)

An old key in a new file, from the same rename table the conversion uses, so nothing is
maintained for this message alone:

```console
$ yolo check
~/.config/yolo-jail/config.toml:14: `brokered` is no longer a key. Its lines are now:
  [allow.git]
  always = ["fetch"]
```

## 2. How a yolo knows which config it has

For this move the answer is settled: the format changes
([OQ-RC2](readable-config.md#decision-ledger), TOML), a format change changes the file name, and
so the name is the version. `.jsonc` is old, `.toml` is new, and nothing inside either says which
yolo wrote it. Reviewed 2026-10-25: *"We're moving to TOML, so that's the answer here. The only
question is, do we have to do something now to prepare for the eventual version 3?"*

The next shape change will be a change *inside* TOML, which keeps the name, so a yolo has to learn
from the file itself that it was written for a newer one. In this move a yolo older than the TOML
release cannot be told anything ([§1](#1-what-youd-see)); the TOML release can be built so that in
the next move it can. Three things it does for that:

1. **Its reader knows a `format` key** and refuses a value above its own, naming the upgrade. A
   file with no `format` is format 2, because format 1 is the other file name. Whether migrated
   files also *write* the key is the question below.
2. **The rename table is data, not code.** One table of old key → new lines drives the conversion,
   the refusal in [§1](#1-what-youd-see), and the next conversion, which adds rows.
3. **Each file name is one constant and one fixture helper**, where today over 400 Go files spell
   them ([§5](#5-every-case)); the next rename touches the constant.

What a format-3 file does to a format-2 yolo, under either option:

```console
$ yolo
~/.config/yolo-jail/config.toml is format 3; this yolo (0.19.0) reads format 2.
Upgrade with `yolo update`, or `just install` from a checkout. Nothing was started.
```

1. 💬 <a id="OQ-RC11"></a>**OQ-RC11: Does the first TOML release write the `format` key, or only know it?**

   You open a migrated `config.toml`. Under A its first line is `[install]`; under B it is
   `format = 2`, a line that does nothing until the next shape change, which may be years away. A
   colleague's yolo from before that change behaves the same under both: it refuses a format-3
   file naming the upgrade.

   ```toml
   # A  the reader knows `format`; migrate writes nothing; no `format` means 2
   [install]
   nix = ["just"]

   # B  every migrated file starts with it, and the next migration rewrites the number
   format = 2

   [install]
   nix = ["just"]
   ```

   <!-- vantage: question id=OQ-RC11 leaning="A: absence meaning 2 is as exact as writing it, since format 1 is the other file name, and a line nobody acts on for years is a line every reader asks about." -->

   _Leaning:_ A: absence meaning 2 is as exact as writing it, since format 1 is the other file
   name, and a line nobody acts on for years is a line every reader asks about.

   **Answer:**
   > _(empty — fill in when decided)_

## 3. The order of releases

1. **The next release reads both formats, and keeps the old reader for a few releases.** Ruled
   2026-10-25: *"we're going to ship toml in the next version"* and *"answering no needs to launch
   with the old file at least for a few versions. we need to maintain both."* There is no
   release before it that only knows the names. It offers the conversion at launch; `n` launches
   on the old file as before, and `"convert_later": true` in the old user file stops the offer
   ([§1](#1-what-youd-see)). Both files in one place are still refused ([§5](#5-every-case)); the
   two readers never merge.
2. **A later release drops the old reader** and refuses an old file naming `yolo config migrate`.
3. **Every machine that shares dotfiles upgrades to the TOML release before any of them
   migrates**, because one machine's migration rewrites the file every other machine reads, and a
   yolo older than that release starts empty on a migrated file. Step 1's window is what lets
   that happen without pressure; the offer says so.

1. 💬 <a id="OQ-RC6"></a>**OQ-RC6: What happens on a machine still running an older yolo?**

   A colleague migrates with the new yolo, then runs a jail from an older yolo installed
   elsewhere, or your Mac is a release behind despite the window in step 1. The older yolo looks
   for `config.jsonc`, finds none, and launches a jail with no packs, saying only that it has no
   coding agent; a migrated local pack it stages as if it had no manifest. No release can reach
   it first: TOML ships next.

   ```text
   # A  migrate keeps the old file beside the new; an older yolo works off it, and the two drift
   config.toml
   config.jsonc        # superseded by config.toml on 2026-11-02; the TOML release reads only config.toml when both exist

   # B  migrate renames the old file; an older yolo starts empty and says so; the rename back is one mv
   config.toml
   config.jsonc.pre-migrate
   ```

   <!-- vantage: question id=OQ-RC6 leaning="B: the window is for upgrading every machine first, the offer says so, and a machine that fell behind has the one-mv way back; A leaves two files that read differently on different machines." -->

   _Leaning:_ B: the window is for upgrading every machine first, the offer says so, and a machine
   that fell behind has the one-`mv` way back; A leaves two files that read differently on
   different machines.

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
- **An old key in a new file:** refused, naming the replacement already respelled, from the
  conversion's own rename table. Reviewed 2026-10-25: *"If we have this mapping anyway for the
  conversion, we might as well use it for nice happy path errors."* No aliases; nothing is
  maintained for the message alone.
- **An old file under a yolo that reads both:** read, with the offer at each launch until
  `"convert_later": true` is in the old user file ([§3](#3-the-order-of-releases) step 1).
- **`"convert_later"` in a TOML file:** refused; it has nothing to say there.
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

   A colleague publishes a pack on GitHub and you list it. The old reader goes
   ([§3](#3-the-order-of-releases) step 2) and the colleague has not migrated. Today the one
   fetched pack in your list, `matt-craft`, has no manifest at all, so it is unaffected; a
   colleague's pack would be.

   ```console
   # A  yolo keeps the old-manifest reader past the window, for fetched packs only, converting in memory
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

1. **The name is the version** for a format change: `.jsonc` is old, `.toml` is new. A change
   inside TOML is told by the `format` key the first TOML reader already knows.
2. **A conversion proves equality or writes nothing.**
3. **No aliases, ever.** An old spelling is refused naming the new one, from the table the
   conversion uses.
4. **Nothing migrates unasked.** The launch offers; a person answers, `n` is a launch, and
   "later" is a key the person writes.
5. **The old reader stays for a few releases, and that window is for upgrading every machine.**
   Nothing reaches a yolo older than the TOML release.

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
