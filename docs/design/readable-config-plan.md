---
title: "Readable config: implementation plan (seed)"
date: 2026-10-24
status: draft
stage: SKETCH
depends-on: [readable-config.md#decision-ledger]
tags: [config, packs, migration, editing, plan]
summary: "The parking lot for what the readable-config design surfaced below its altitude: the position-reporting parsers a splice needs, the code that already exists for it, the records a migration must respell, and the fixtures that hardcode today's file names. Not a hand-off; nothing builds from it while it is a sketch."
---

# Readable config: implementation plan (seed)

**Status:** 2026-10-24 — seeded during design; not a hand-off, and incomplete until
[`readable-config.md`](readable-config.md) and the five documents it fronts are accepted. The
design wins on behavior; this file holds mechanism notes only. The rulings that decide which
entries apply are in: [OQ-RC2](readable-config.md#decision-ledger) (TOML),
[OQ-RC9](readable-config.md#decision-ledger) (splice) and [OQ-RC11](readable-config.md#decision-ledger)
(a `format` key the reader knows and nothing writes); every other question is ruled too, so the
next step is to grow this seed into the hand-off.

## What already exists for a splice

- `internal/json5/locate.go`: `Locate`, `LocateSteps` and `NewIndex` return the byte range of a
  value by key path; the package comment says it exists for in-place edits. Only
  `internal/config/sources.go` calls it, to name a line in an error. Verify both facts at the
  implementing commit.
- `internal/entrypoint/tomltrivia.go`: a hand-written TOML comment scanner by position, used by
  the prism's read-modify-write. It attaches comments; it does not report key positions. Check
  whether extending it is cheaper than a second parser.
- `internal/config/workspacefile.go`: temp-file-and-rename write and 0600 mode, the pattern the
  splice reuses. The broker store's lock is the candidate for serializing two commands.
- Parse errors in `internal/json5/json5.go` report a byte offset only; `Sources` builds the index
  lazily to turn an offset into `line:col`. A splice's index is the same index.

## Position-reporting parsers, per format

- **TOML.** BurntSushi (vendored, used by `internal/tomlx`) exposes positions only in
  `ParseError`, so it cannot serve a splice. `github.com/pelletier/go-toml/v2/unstable` has
  `KeepComments`, `Position` (offset, line, column) and `Shape`, and no writer; a splice needs only
  the positions. One new vendored module if chosen; check its size before deciding against a
  scanner of yolo's own. `creachadair/tomledit` round-trips but reformats spacing and says it is
  not production-ready; it is the library-round-trip option the design does not recommend.
- **JSONC.** `json5.Locate` already does it. `tailscale/hujson` is the library alternative; its
  `Patch` keeps bytes but its `Format` is opinionated, so it would also be a round trip.
- **YAML.** Not recommended by the design; `yaml.v3` is frozen upstream and its node tree loses
  layout on re-encode.

## What a migration must touch besides the files

- Approval records under `paths.BrokerSourceDir`'s sibling `approvals/` and each workspace's
  `.yolo/config-boot.json` hold canonical JSON that the renames change; respell key for key or
  every migrated workspace prompts once.
- `inheritCensus` in `internal/config/inherit.go` lists every key the generated in-jail copy may
  carry; each rename is an entry change there, and the `yolo-origins` line's key names change with it.
- The retired-key set in `internal/config/config.go` and its per-key refusals in `validate.go`
  are the pattern for refusing an old key with the new spelling.
- The [jail-mechanics reference](../reference/jail-mechanics-skill.md) describes the shared
  timing classifier. `TestEveryConfigKeyHasAnExplicitTimingRow` compares it with the accepted
  config schema for every backend; keep that schema-total check current on key renames.
- A zero-byte `~/.config/yolo-jail/config.lua` exists on at least one machine; the transform that
  read it is gone.

## File names

- Over 400 Go files under `internal/`, `integration/` and `cmd/` name `yolo-jail.jsonc` or
  `config.jsonc`; only the workspace names have constants (`WorkspaceConfigName`,
  `WorkspaceLocalConfigName` in `internal/config/config.go`), and the user config name is the
  unexported `userConfigSuffix` in `internal/paths/paths.go` plus a spelled-out `config.json`
  fallback. One fixture helper that writes a config in the current format is the change that
  stops the next format change from touching 400 files.
- The user guide and reference mention `.jsonc` in about 50 files; `yolo config-ref` is
  regenerated, not edited.

## Pack-side notes

- `launchFlagClaims` in `internal/packload/packload.go` assigns a bin's flag list; P4 makes it
  append unless `replace = true`.
- Manifests at yolo ≤ 0.11.1 are decoded with `DisallowUnknownFields`, so any new field refuses
  the pack; the new file name sidesteps that; a yolo older than the TOML release stages a migrated pack as
  manifest-less ([`config-migration.md` §3](config-migration.md#3-the-order-of-releases)).
- `pack lint` findings the design asks for: an overlay key the surface's derive also writes
  (`subagents.defaultModel`, `defaultProvider`, `modelScope.*` on `pi/settings`), named with the
  winning layer; a pack copying files one by one into a folder pi loads as a package; a gzip ratio
  0.10 or more under prose of the same size, per contribution.
- `yolo pack migrate` moves `files/extensions` and `files/themes` under `files/pi/` for pi packs
  (Route C of [OQ-PR1](../reference/pack-system.md#oq-pr1), already shipped) and uses `git mv`
  in a checkout.
