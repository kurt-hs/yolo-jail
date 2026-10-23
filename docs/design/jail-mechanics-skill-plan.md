---
title: "jail-mechanics skill: implementation plan (seed)"
status: draft
stage: SKETCH
next: "Complete against the tree once jail-mechanics-skill.md is accepted"
depends-on: [jail-mechanics-skill.md#OQ-JM2, jail-mechanics-skill.md#OQ-JM4]
tags: [plan, skills, briefing, lifecycle]
---

# jail-mechanics skill: implementation plan (seed)

**Status:** 2026-10-23. Seeded during design; not a hand-off, and incomplete until the design is
accepted. Nothing may be built from it while it is a seed.

The design is [`jail-mechanics-skill.md`](jail-mechanics-skill.md), and it wins on behavior. This
file holds what the design work surfaced and the user need not rule on.

## Surfaced during design

- **Where the renderer hooks in.** `run.(*Options).refreshJailBriefings`
  ([`prepare.go`](../../internal/cli/run/prepare.go)) builds `jailcontent.BriefingInput` and calls
  `jailcontent.PrepareSkillsWith` in one function, on every launch and attach. The built-in suite is
  written by `writeBuiltinSkills` ([`write.go`](../../internal/jailcontent/write.go)) from
  `builtinskills.FS`, filling names the packs left free (`composeSkillLayers`,
  [`skills.go`](../../internal/jailcontent/skills.go)). The rendered skill is written after the packs,
  like `writeLSPPlugin`, per [JM-D2](jail-mechanics-skill.md#decision-ledger). Today the input is
  built *after* skills staging, so the order inside `refreshJailBriefings` changes.
- **`TestBuiltinSkillsEmbedMatchesTree`** pins the embed list against the on-disk tree. A rendered
  skill has no on-disk tree under `builtinskills/`; decide whether its templates live there (and the
  test learns templates) or beside the renderer. Blocked on
  [OQ-JM2](jail-mechanics-skill.md#OQ-JM2), which decides whether the other two become templates.
- **The change-timing table today** is `keyOutcome`, `mountsOutcome`, `scopeChangeOutcome` and
  `verifiedScopeOutcome` in [`liveapplyplan.go`](../../internal/cli/run/liveapplyplan.go), package
  `run`. `jailcontent` cannot import `run`; the table needs a package both import (or the renderer
  takes the classified rows as input, built in `run`).
- **The rebuild predicate**: `config.EffectivePackages` ([`derived.go`](../../internal/config/derived.go))
  and the `gpu.vaapi` + `gpu.vendor: "amd"` rule; check where the latter is decided before relying on
  it. On a store-delivered launch (`YOLO_STORE_PACKAGES=1`) no rebuild happens; find the decision's
  function.
- **User-scope-only keys**: the refusals in [`validate.go`](../../internal/config/validate.go); check
  whether one list exists or each key refuses on its own.
- **A second, partial key table** exists in `internal/config/inherit.go` (what reaches a nested
  jail); check that it does not contradict the timing table.
- **Restart wording sites** to fold into one function ([JM-D8](jail-mechanics-skill.md#decision-ledger)):
  `applyOutcome.String`, `stopRemedy` ([`attachskew.go`](../../internal/cli/run/attachskew.go)),
  [`configdrift.go`](../../internal/cli/configdrift.go), `lockedConfigRequest`
  ([`briefing.go`](../../internal/jailcontent/briefing.go)), `internal/broker/brokercmd.go`,
  `internal/prune/miseversions.go`.
- **The persistence map has no host-source column.** `jailcontent.PersistentPath` is `Path` and
  `Class` ([`persistence.go`](../../internal/jailcontent/persistence.go)); `persistenceMapFor`
  ([`persistencemap.go`](../../internal/cli/run/persistencemap.go)) knows the sources and drops them.
  The argv-agreement test that pins the map lives near it; find it by `rg -n persistenceMapFor internal/cli/run/*_test.go`.
- **Effective config at attach.** `.yolo/config-boot.json` is agent-writable
  ([`drift.go`](../../internal/config/drift.go)); the fact sheet should prefer host-held inputs (the
  inspected env, the live mount record, the running pack tree) over that file. Check what the live
  mount record holds before relying on it.
- **Apple Container** copies staged content at launch (`acMaterialize`, `acMaterializeTree` in
  [`helpers.go`](../../internal/cli/run/helpers.go)); confirm skills take the same path as the
  briefing before writing [JM-D7](jail-mechanics-skill.md#decision-ledger)'s behavior.
- **`yolo config apply` re-render** ([JM-D4](jail-mechanics-skill.md#decision-ledger)) runs from
  `liveapply.go`'s command path, which today calls no staging; it needs the running jail's pack view
  that an attach builds.
- **Static texts to edit when the design lands**: `packagesSection` and the Skills section in
  `briefing.go`; both `SKILL.md` files; the agent-workflow paragraphs of `config_ref.txt`
  (search `Please restart the jail`); [`agent-briefings.md`](../reference/agent-briefings.md)'s
  Skills and Gotchas sections; [`config-safety.md`](../reference/config-safety.md)'s attach sentence.
- **Host notch** ([OQ-JM4](jail-mechanics-skill.md#OQ-JM4)): `internal/cli/applyhostskills.go` and
  `internal/hostskills/compose.go` both state that built-ins are not written to a real home; option A
  changes both comments and the host render's layer plan.
