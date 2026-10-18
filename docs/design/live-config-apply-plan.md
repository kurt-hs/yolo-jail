---
title: "Live config apply: implementation sketch"
status: draft
stage: SKETCH
next: "Complete this into the implementation plan against the tree; every ruling is in"
tags: [plan, sketch, config, mounts, github, live-apply]
---

# Live config apply: implementation sketch

**Status:** 2026-10-18. This is incomplete, and it is unstable while
[`live-config-apply.md`](live-config-apply.md)'s questions are open. **Do not build from it.** The
design wins on behavior. This file only parks the material below the design's altitude.

## Reuse, not fork

- **The gate.** Call `config.ApproveConfigAndScope` (`internal/config/snapshot.go:329`) through the
  same wrapper shape as `Options.checkConfigChanges` (`internal/cli/run/preflight.go:416-450`),
  reading through `config.ReadWorkspaceForGate`. Use the same `changePrompter.PromptReport` and
  `declineLines`.
  - The outcome block needs a hook between the diff and the question. Add one parameter for
    pre-question lines; do not copy the prompt.
- **The scope read.** Use `brokeredScopeForCheck` (`internal/cli/check/check.go`) or
  `brokeredScopeCheck` (`internal/cli/run/brokeredscope.go:35`), whichever already takes a
  workspace without a launch plan.
- **The lock.** Use `holdLaunchLock` / `acquireWorkspaceLock` (`internal/cli/run/flock.go:69-99`,
  `:293-318`), keyed on `runtime.FromWorkspace`.
- **The probe.** Use `probeRunningContainer` (`internal/cli/run/lifecycle.go:96-115`), which is
  tri-state.
- **Contract tags.** Freeze them through `entrypoint.ContractTagsEnv`, and read them the way
  `internal/cli/run/contracttags.go` reads them.
- **The briefing re-render.** Use `refreshJailBriefings` (`internal/cli/run/run.go:2978-2990`,
  `prepare.go:40`). Change its config input to the effective config (LA-D7). Note that today it
  is passed the current `cfg` at `run.go:2987`.
- **Verb registration.** Register it in `internal/cli/config.go:236-246`, the help-token list at
  `:348`, and the notchless verbs at `:256-264`.

## Mount injector

- A hidden host subcommand, for example `yolo internal ctx-attach`, with the arguments
  `--pid <container pid> --source <resolved> --dest </ctx/...> [--rw]`.
  - Rootless: run it under `podman unshare`.
  - Rootful: run it directly when yolo runs podman as root.
  - podman machine: run it through `podman machine ssh`, using the Linux binary from the mounted
    bundle.
  - The branch comes from `podman info`, as `internal/image/storespec.go` decides its own.
- Syscalls: `open_tree` 428, `move_mount` 429, `mount_setattr` 442 on x86_64 and arm64 alike.
  `golang.org/x/sys/unix` is vendored and wraps all three. Check that the vendored version has
  `MountSetattr` before relying on it.
- **Go detail.** `runtime.LockOSThread()`, then `unix.Unshare(CLONE_FS)` before
  `setns(CLONE_NEWNS)`. A multi-threaded Go process otherwise gets `EINVAL`. The probe confirmed the
  sequence works.
- **Destination open.** Use `openat2` with `RESOLVE_NO_SYMLINKS | RESOLVE_NO_MAGICLINKS` inside
  the jail namespace. Check that `/ctx`'s filesystem is tmpfs (`statfs` magic) before the
  private-clone `mkdirat`.
- **Removal ([OQ-LA3](live-config-apply.md#decision-ledger)).** `setns`, then `umount2(dest, MNT_DETACH)`, then `rmdir` through
  the private clone.
- **The launch half.** Add `--mount type=tmpfs,destination=/ctx,ro=true` to the podman argv in
  `internal/cli/run/assemble.go`, near the `--read-only` flags at `:380-391`.
  - Check the flake's baked `/ctx/host-*` directories (`flake.nix:1851-1863`). podman copies them
    up into the tmpfs; the probe saw `host-claude` and `host-nvim-config` there.
  - Check `paths.ReservedContextPaths` consumers for any assumption that `/ctx` is image content.
- **The probe program** used on 2026-10-18 was a 90-line Go `main` making raw syscalls. Rewrite
  it as a test fixture rather than keeping it.

## GitHub broker

- In `internal/ghbroker/daemon.go:435`, classify against the scope from `authority.live()` rather
  than `b.scope`. Keep `b.scope.describe()` (`:597`) and `widenAdvice` on the same source.
- Write the capability field (LA-D11) in `BindAuthority` (`internal/ghbroker/authority.go:30`).
  The struct is `internal/brokerscope/file.go:33`; an unknown field is ignored by older readers.
- Rewrite with `brokerlock.WithTransaction(BrokerSourceDir)`. `brokerscope.Write` drops
  `broker_*` fields unless they are copied across, which is a trap.
- macos-user: enumerate the live launch-id files under `broker/github/scope/` for this workspace.
  Liveness is the file's `pid`.

## Effective-config record

- `config.WriteWorkspaceBootBaseline` (`internal/config/drift.go:52-61`) writes the whole snapshot.
  Add a fold-in writer that takes the boot JSON and replaces one top-level key.
  `WorkspaceConfigDrift` (`drift.go:103-139`) needs no change, beyond its labels ("config this
  jail has" rather than "config that started this jail").

## Per-row delivery evidence (from the 2026-10-18 inventory)

- `mounts`: `ctxmounts.go:55-110`, `assemble.go:368-377,712`. Read-write mounts come from
  `config/mounts.go:312-326`. The read-write `at` must be under `/ctx` (`config/mounts.go:203-210`).
- `env_sources`, providers, profiles: channel delivery at `run.go:2852,3054-3071`;
  `agentenvfiles.go:66`.
- `mcp_servers` / `lsp_servers`: `assemble.go:1212,1259,1264-1265,1453`; boot
  `bootsteps.go:381`.
- `security.blocked_tools`: `assemble.go:358-359,1243`; boot `bootsteps.go:250`.
- `resources`: `assemble_parts.go:878`; `backendcaps.go:267-314`.
- `network.forward_host_ports`: `assemble_parts.go:484-503`; `network.go:43`; boot
  `bootsteps.go:523`.
- Loopholes: `loopholesruntime.go:274`, `packloopholes.go:920,983`, argv
  `assemble_parts.go:839`.
- Pack kinds: `internal/packdecl/kinds.go:46-377`; surfaces `entrypoint/surfaceloop.go`.
- Apple Container briefing copy: `assemble.go:1059-1060`; `attachskewbriefing.go:64-66`.

## Tests to write (shape only)

- Gate reuse: a fixture where the apply and a launch see the same diff and write the same record
  bytes.
- **Delete-the-call-site check.** Removing the apply's call into the gate must fail a test.
- Integration (container): inject into a running jail and assert the mount is read-only inside.
  This runs in CI's rootless jobs for M1; a nested jail gives a free green here.
- The broker: widen through the file and see a request admitted; narrow and see it refused; a file
  without the capability field reports *at next restart*.
