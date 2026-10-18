---
title: "Per-session profiles: implementation sketch"
status: draft
stage: SKETCH
next: "Grow this into the implementation plan; OQ-SP1 and OQ-SP2 are ruled"
tags: [plan, sketch, profiles, attach, sessions, pi]
---

# Per-session profiles: implementation sketch

**Status:** 2026-10-18. Incomplete. **Do not build from it** until it is completed against the tree.
The design wins on behavior; this file only parks material below its altitude.
[OQ-SP1](per-session-profiles.md#11-decision-ledger) and [OQ-SP2](per-session-profiles.md#11-decision-ledger)
are ruled (build it; capture in-session settings).

## Where the pieces attach

- **Snapshot write.** `deliverChannel` (`internal/cli/run/agentenvfiles.go:66`) gains a
  session-scoped destination beside the shared one; the attach arm already has the id before the exec
  (`attachSessionID`, `internal/cli/run/run.go:3113`). The fresh launch's first session mints its id
  in `firstSessionExecCmd` (`internal/cli/run/jailmain.go:262`); the snapshot must be written before
  that exec too. Coordinate the directory layout with
  [attach-daemon-activation §4.2](attach-daemon-activation.md#42-publication-and-concurrent-attaches)
  so the two designs make one snapshot, not two.
- **Hydrate.** `hydrateEnvFromUserEnvFile` (`internal/entrypoint/boot.go:205`) reads the snapshot
  named by `YOLO_SESSION_ID` when present.
- **Launchers.** `agentEnvShellFn` (`internal/entrypoint/agentenv.go:76-87`) sources the snapshot's
  files first; `.bashrc`'s line at `internal/entrypoint/shell.go:248` likewise.
- **Render.** `renderPackSet` (`internal/entrypoint/surfaceloop.go:203`) is where a session pass
  (`bootRun.sessionPass`, `internal/entrypoint/bootsteps.go:85`) would redirect a surface whose path
  lies under a declared directory. The capture sidecars live under `<workspace>/.yolo/prism/`; a
  session copy needs its own baseline there.
- **Declaration.** A new pack contribution kind or a field on the `state` kind; the schema lives in
  `internal/packdecl` (doc comments are the reference). Put it top-level if older yolos must skip it
  cleanly (the `launch_computed` precedent in `internal/packdecl/launchcomputed.go`).
- **Contract tag.** New tag in `internal/cli/run/contracttags.go`; the older-jail line goes beside
  `noteBootedPackSetDiffers` in `attachExisting`.

## Traps to check before relying on them

- Measure, for each shared name, whether stock pi writes in place or by rename: `auth.json`
  (auth-storage), `models-store.json`, `mcp-auth.json`, `run-history.jsonl`. The existing
  shared-credentials hook suggests `auth.json` is written through its link today; confirm.
- `pi update --extensions` (the prelaunch refresh) installs into `npm/` and `git/`: both must be
  linked before the launcher runs it, and the launcher must run it with the variable exported.
- Check whether pi's resource loader reads anything under `~/.agents` besides pi-subagents' agents
  dir, and whether it changes when `PI_CODING_AGENT_DIR` is set.
- The yolo footer extension (`packs/pi/extensions/yolo-footer.js`) shells out to yolo; confirm it
  reads nothing from `~/.pi/agent`.
- `TestRenderFingerprintStable` pins the boot render; a session pass's render target differs, so the
  fingerprint must be taken per destination.

## Tests the build will need

- An attach with a different profile leaves an earlier session's home byte-identical (the deadlock's
  unit form), and the test must fail if the session pass writes the shared `settings.json`.
- A bare attach (no `-p`) renders only its own home.
- A session home links `auth.json`; deleting the link makes a login test fail.
- Integration: two execs into one jail with different `-p`, reading `PI_CODING_AGENT_DIR` and the
  `subagents` block each sees; no agent CLI is started (`--version` probes only).
