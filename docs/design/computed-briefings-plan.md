---
title: "Computed briefings: implementation plan (seed)"
date: 2026-10-24
status: draft
stage: SKETCH
depends-on: [computed-briefings.md#OQ-CB1]
tags: [briefing, packs, derive, plan]
---

# Computed briefings: implementation plan (seed)

**Status:** 2026-10-24 — seeded during design; not a hand-off, and incomplete until the design is
accepted. Nothing may be built from it while it is a seed.

**Precedence:** [`computed-briefings.md`](computed-briefings.md) wins on behavior. Entries here
are only what the design work surfaced; the planner checks each against the tree at a stamped
commit.

Blocked on [OQ-CB1](computed-briefings.md#OQ-CB1): whether facts come from core or from a
publishing pack shapes the access table's builder.

## Notes surfaced during design

- **Registrations.** `newDeriveSession` in `internal/agentcfg/luahook/derive.go` registers
  `yolo.derive` and `yolo.env`; `yolo.briefing` joins them with a key space of its own.
  `DeriveRegistrations` already lists registrations without a ctx (used by `yolo config ls` and
  `packload.AgentEnv`); lint's pairing check can reuse it. Check how
  `TestEveryShippedPackDeriveStillRuns` enumerates "the three registration spaces" before adding a
  fourth.
- **The producer ctx is its own type**, not new fields on `DeriveCtx`, which is the surface and env
  ctx.
- **Composition call sites.** `refreshJailBriefings` is called from three places in
  `internal/cli/run/run.go`: the macos-user arm, the container launch, and the attach refresh.
  `entrypoint.ComposeHostBriefings` is the host notch's composer. Both pass pack prose through
  `packBriefingProses` / `ComposeBriefingSections`; check where a producer's output can enter as a
  virtual `briefing/<name>.md` entry so ordering is unchanged.
- **Launch facts.** `o.approvedScopes` and `o.approvedGrants` hold the gate's result before the
  briefing composes; the broker start decision is `loopholes.Set.BrokeredToStart` less placement
  refusals (see `writeScopeFiles` in `internal/cli/run/brokeredscope.go`).
- **Attach facts.** `brokerscope.LiveFiles(source, container, workspace)` reads a running jail's
  scope files under the source's lock.
- **Working set.** `internal/brokerstore/working.go`; read under the store's transaction lock.
- **Live apply.** The `changeScope` arm in `internal/cli/run/liveapply.go` replaces scope files;
  the re-composition goes after it, under the launch lock. Check that `yolo config apply` already
  holds that lock at that point.
- **Wildcard expansion and repository identity** use `brokerscope.AnyRepo` and
  `gitproto.Identity`, as the broker's own matching does.
- **Parity pin.** `internal/cli/run/briefingparity_test.go` pins jail and host composition bytes
  against each other; extend it with a producer.
- **Manifest field.** `derive` and `reads` on a `briefing` contribution in
  `internal/packdecl/packdecl.go`; check the unknown-field behavior on the strict and tolerant
  decoders for an older yolo reading a pack that uses them.
