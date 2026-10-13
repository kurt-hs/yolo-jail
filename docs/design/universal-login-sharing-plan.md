---
title: "Universal login sharing — implementation sketch"
date: 2026-10-13
status: draft
stage: SKETCH
next: "Incomplete and unstable while [OQ-ULS1](universal-login-sharing.md#OQ-ULS1) is open — do not build from this"
depends-on:
  - universal-login-sharing.md#OQ-ULS1
  - universal-login-sharing.md#OQ-ULS2
  - universal-login-sharing.md#OQ-ULS3
tags: [credentials, auth, host, jails, shared-credentials, sketch]
summary: "Parking lot for the universal login-sharing design: the code path the machine-share hook runs on, the files a host-side reader or linker would touch, and the spike that would settle the pi/Muse dual-parse. Not a plan; incomplete while the design's questions are open."
---

# Universal login sharing — implementation sketch

**Status:** 2026-10-13 — incomplete, and unstable while questions are open. **Do not build from
this.** The design wins on behavior: [`universal-login-sharing.md`](universal-login-sharing.md).

## What is already built (so the plan does not re-derive it)

| Piece | Where | Note |
| :--- | :--- | :--- |
| The file-share rule and its ordering | [`internal/entrypoint/sharedlink.go`](../../internal/entrypoint/sharedlink.go) (`linkThroughShared`, `sharedFileNode`) | "shared side always wins" only after a copy reports success; read the warning before editing |
| The hook dispatch | [`internal/entrypoint/packhooks.go`](../../internal/entrypoint/packhooks.go) (`linkSharedCredential`, `linkSharedDirectory`, `linkIntoSharedDir`) | runs in the entrypoint only |
| The bind source for machine-scope dirs | [`internal/cli/run/shareddirsources.go`](../../internal/cli/run/shareddirsources.go) (`ensureSharedDirSources`, `paths.GlobalHome()`) | host-side, before any container starts |
| The one host view | [`internal/cli/hostclaudeview.go`](../../internal/cli/hostclaudeview.go) (`hostClaudeView`) | the template for [OQ-ULS1](universal-login-sharing.md#OQ-ULS1) option A; the broker's [CL-D27](claude-login-without-interception.md) |
| The closed hook set | `internal/cli/config_ref.txt` (`shared_credentials`, `shared_directory`, `unshare_directory`, `per_jail_history`) | [OQ-ULS3](universal-login-sharing.md#OQ-ULS3) option B needs a new one |

## Checks to run before relying on any of it

- **Does `yolo host apply` run any pack hook?** At `80e08f98b` it does not
  (`rg -n 'linkThroughShared|shared_credentials' internal/cli/` finds only the Claude view and
  tests). Re-check before building a host path that assumes otherwise.
- **Do pi and Muse declare the same machine-scope `at`?** They do not today, which is why the base
  name collision is latent rather than active. Confirm before leaning on
  [§2](universal-login-sharing.md#2-what-exists-today)'s collapse.
- **Does pi's `auth.json` reader tolerate an unknown top-level key?** Read the reader in
  `packs/pi/extensions` / pi's own loader before trusting [OQ-ULS3](universal-login-sharing.md#OQ-ULS3)
  option A.
- **Is the store under `state` ever reset?** `yolo` state-reset paths decide whether
  [OQ-ULS1](universal-login-sharing.md#OQ-ULS1) option B can leave the login there.

## The dual-parse spike (settles [OQ-ULS3](universal-login-sharing.md#OQ-ULS3) option A)

1. Write one file at a scratch path holding Muse's schema plus a flattened pi map.
2. Point a pi process and a Muse CLI at it in an isolated home; observe which one rejects it and with
   what message.
3. Result: either a shape both accept (option A survives), or a named incompatibility (fall to C, or
   promote B).

No agent session may be started for this; run the parsers' own load/validate path only.

## Sequencing (below the design's altitude)

- The **host view** (A) reuses the broker's view pattern; the new work is per-agent redirection and
  the capture-back path when the host takes the login.
- The **canonical-store** route (B) is mostly a store-location decision plus a host-side link step;
  the risk is real-home mutation, not code volume.
- The **translation** (B) is the only piece that needs new core code, and the spike says whether it
  is ever needed.
