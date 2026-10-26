---
title: "Podman exec-monitor identity and recovery limits"
status: accepted
tags: [podman, conmon, exec, process-identity]
summary: "Version-fenced source findings for identifying Podman exec conmon processes without mistaking database membership or shutdown state for liveness."
---

# Podman exec-monitor identity and recovery limits

**Checked:** 2026-10-26 from the Podman `v5.8.9` and conmon `v2.2.1` release sources. Release-tag citations below are version-fenced; upstream commit IDs were not resolved. Exact fetched-byte SHA-256 values for the evidence-bearing files are listed under [Source byte hashes](#source-byte-hashes). Installed-runtime equivalence is unknown.

## Exec monitor arguments

Podman's shared conmon argument builder supplies a container ID and a UUID. The exec caller uses the full container ID for `-c`, the exec session UUID for `-u`, and explicit `-e` to select exec mode. The main monitor uses the full container ID for both `-c` and `-u` and has no `-e`. Conmon defines `-c` as the container ID, `-u` as the conmon UUID (not a Unix UID), and `-e` as exec mode. These arguments are an identification pattern, not proof of executable, owner, liveness, or ancestry.

- [Podman `sharedConmonArgs`](https://github.com/containers/podman/blob/v5.8.9/libpod/oci_conmon_common.go) — the producer uses the container ID.
- [Podman `startExec`](https://github.com/containers/podman/blob/v5.8.9/libpod/oci_conmon_exec_common.go) — the exec call supplies the session ID and `-e`.
- [Conmon CLI options](https://github.com/containers/conmon/blob/v2.2.1/src/cli.c) — definitions for `-c`, `-u`, and `-e`.

## ExecIDs and lightweight exec

Podman inspect enumerates keys from the saved exec-session map into `ExecIDs`; it does not filter that map to sessions currently running. Exec creation can register an unstarted session, and stopped sessions can remain in the map. Conversely, lightweight `--no-session` exec creates an in-memory session, invokes the runtime exec path, and does not persistently register it. Therefore ExecIDs membership is only a necessary association check when available: it is neither an active-process list nor proof of liveness, and absence cannot disprove a monitor.

- [Podman inspect producer](https://github.com/containers/podman/blob/v5.8.9/libpod/container_inspect.go) — ExecIDs is built from session-map keys.
- [Podman exec sessions](https://github.com/containers/podman/blob/v5.8.9/libpod/container_exec.go) — persistent and lightweight session paths differ.
- [Podman exec CLI](https://github.com/containers/podman/blob/v5.8.9/cmd/podman/containers/exec.go) — `--no-session` selects the lightweight path.

## Stop state

Podman explicitly persists a `stopping` state before invoking runtime stop work. Inspect's `Running` is true only in the running state, so `Running=false` includes an in-progress stop. Main conmon exit is a separate lifecycle event and can trigger recovery/cleanup of the container process. Neither `stopping` nor `Running=false` proves that a container is gone.

- [Podman state definitions](https://github.com/containers/podman/blob/v5.8.9/libpod/define/containerstate.go) — stopping is an in-progress state.
- [Podman stop and main-monitor lifecycle](https://github.com/containers/podman/blob/v5.8.9/libpod/container_internal.go) — stop-state persistence and conmon-exit handling.
- [Podman exec-session stop](https://github.com/containers/podman/blob/v5.8.9/libpod/oci_conmon_exec_common.go) — session process stopping is not a generic conmon-kill authorization.

## Runtime-info provenance

In Podman `v5.8.9`, `Info.Version` is serialized as top-level lowercase `version`; its nested `Version.Version` field is uppercase because that struct has no JSON tag. `HostInfo.Conmon` is `host.conmon`; `ConmonInfo.Path` and `ConmonInfo.Version` are lowercase `path` and `version`. Runtime info may omit `conmon` when its runtime-info probe errors. `cmd/podman/system/info.go` assigns `Host.ServiceIsRemote` from that client invocation's `registry.IsRemote()` before serialization; it is not copied from the server's HostInfo. The probe must use the exact unmodified selected `podman info --format json` invocation; do not add `--remote=false`, override connection selection, ignore `CONTAINER_HOST`/`CONTAINER_CONNECTION`, or trust argv alone.

Podman `v5.8.9`'s `getConmonVersion` runs `conmon --version`, replaces the first newline with `, `, then removes the final newline. Conmon `v2.2.1` prints `conmon version 2.2.1` followed by `commit: <build commit>`, so the serialized `host.conmon.version` has the form `conmon version 2.2.1, commit: <nonempty build commit>`. The recovery fence accepts that producer format and requires the semantic version to be exactly `2.2.1`; it does not assume or pin an unresolved upstream Git commit SHA.

A version-fenced caller must require exact Podman `5.8.9`, explicit client-side `serviceIsRemote: false`, an absolute `conmon.path`, and the source-defined conmon output format with semantic version exactly `2.2.1`; missing fields, malformed output, or other versions decline. `Conmon.Path` comes from the selected runtime's `RuntimeInfo()`.

The inspected `v5.8.9` inspect producer exposes `Id` (full lowercase 64-hex ID), `State.Status`, and `ExecIDs`. The caller accepts only that typed `{{json .}}` object shape and exact 64-character ID; no abbreviation or same-name volume can stand in for a container ID.

- [Podman info command](https://github.com/containers/podman/blob/v5.8.9/cmd/podman/system/info.go) — client-side remote flag assignment.
- [Podman info schema](https://github.com/containers/podman/blob/v5.8.9/libpod/define/info.go) — JSON casing and conmon fields.
- [Podman info producer](https://github.com/containers/podman/blob/v5.8.9/libpod/info.go) — runtime Conmon.Path source.
- [Podman connection selection](https://github.com/containers/podman/blob/v5.8.9/cmd/podman/root.go) — CLI/env/config precedence.
- [Podman inspect schema](https://github.com/containers/podman/blob/v5.8.9/libpod/define/container_inspect.go) — exact `Id`, `State`, and `ExecIDs` JSON names.

## Destructive recovery boundary

The source facts above do not authorize killing a monitor. A force-only recovery candidate must additionally be owned by the invoking UID, have a positive non-self PID and strict nonzero start time, match the authentic Conmon.Path by executable file identity, and remain pinned with pidfd. The exact full CID, distinct exec UUID and STOPPING state must be re-read immediately before signaling. A same-UID direct zombie child must be observed under that monitor and revalidated after pidfd pinning. Complete child enumeration must find no live child alongside the zombie; any foreign-UID child, unreadable/incomplete enumeration, unknown state, change observed at revalidation, unsupported pidfd, or mismatch declines. The main monitor, live exec, --no-session exec without stored association, remote client, normal graceful shutdown, and numeric-PID fallback are never signal targets.

> [!WARNING]
> A direct zombie-child witness is not established by upstream source inspection. No real rootless-host recovery recurrence has been observed. The final Podman-state read and pidfd signal are not atomic: state can change after the read, and the pidfd pins process identity only. See the [design's explicit TOCTOU residual](../design/exec-stop-monitor-recovery.md#eligibility-and-revalidation). Do not claim the host wedge is recoverable from these source facts.

## Source byte hashes

SHA-256 of the complete fetched files; URLs are exact release tags, and upstream commit SHAs were not resolved. The version fence must be re-reviewed before accepting any different Podman or conmon version.

| Source | SHA-256 |
| :--- | :--- |
| [Podman `libpod/oci_conmon_exec_common.go`](https://github.com/containers/podman/blob/v5.8.9/libpod/oci_conmon_exec_common.go) | `515246e823eaad18116714be1c78ff79fcbb908e4236d272c443ddc40b18288c` |
| [Podman `libpod/oci_conmon_common.go`](https://github.com/containers/podman/blob/v5.8.9/libpod/oci_conmon_common.go) | `13ca3c46e7b913cc355cca441bfc1467bb921b3b3206d8bb642ad8c2a2823092` |
| [Podman `libpod/container_inspect.go`](https://github.com/containers/podman/blob/v5.8.9/libpod/container_inspect.go) | `411bc8ebd87865064b9013a8b4f47eba0e81ee3c95d7b627e71277c1201b54c7` |
| [Podman `libpod/define/container_inspect.go`](https://github.com/containers/podman/blob/v5.8.9/libpod/define/container_inspect.go) | `118bd4b653683c31b58581e4512b19f234e9fb78a4ff4fca88b848e7bdda1452` |
| [Podman `libpod/container_exec.go`](https://github.com/containers/podman/blob/v5.8.9/libpod/container_exec.go) | `ed06a35fc4769ebd4281a28096b0c050530750ea9234cb3ad2b2ecd73b433e14` |
| [Podman `libpod/container_internal.go`](https://github.com/containers/podman/blob/v5.8.9/libpod/container_internal.go) | `d78ed896fdbfb4d7b8f210a2ef5db55a085cde915df4d452f7b93908689be2dd` |
| [Podman `libpod/define/containerstate.go`](https://github.com/containers/podman/blob/v5.8.9/libpod/define/containerstate.go) | `9a2583be3c20b96723f507fcabd7efe82e294fddf57ff418b3148b77adf0be48` |
| [Conmon `src/cli.c`](https://github.com/containers/conmon/blob/v2.2.1/src/cli.c) | `ea7a5d21862efa3780b6f1957f45012617f4f7c9367b473ac6f65ada76ff66b0` |
| [Podman `libpod/define/info.go`](https://github.com/containers/podman/blob/v5.8.9/libpod/define/info.go) | `1db463834c040b352458a7cf8c1d20a2a769d2c346dbc57b18572aaa02d30672` |
| [Podman `libpod/define/version.go`](https://github.com/containers/podman/blob/v5.8.9/libpod/define/version.go) | `fb7ea400ec2f99388037d286603cf02355f12991b916f42dabf2fb5b12ca48d6` |
| [Podman `libpod/info.go`](https://github.com/containers/podman/blob/v5.8.9/libpod/info.go) | `08e9d7d981476139fc3e057d20157172e3733c4586ef33a1b7e7eb055d793515` |
| [Podman `cmd/podman/system/info.go`](https://github.com/containers/podman/blob/v5.8.9/cmd/podman/system/info.go) | `4656814f1883b6da465201487283018085f1e8ad689c8b74c410befa7985c40b` |
| [Podman `cmd/podman/root.go`](https://github.com/containers/podman/blob/v5.8.9/cmd/podman/root.go) | `00335ae1cb0d1c231dcd36d336a931fcbb1b29a9025144e325bf562e2b7fb222` |
| [Podman `cmd/podman/containers/exec.go`](https://github.com/containers/podman/blob/v5.8.9/cmd/podman/containers/exec.go) | `80a3efe142704c9b20272dfbea963404f91baf48a6ab12887fbcf8731d624506` |

## Sources / See also

- [Exec-stop monitor recovery design](../design/exec-stop-monitor-recovery.md) — the bounded project decision that consumes these version-specific facts.
- [Implementation plan](../design/exec-stop-monitor-recovery-plan.md) — current code seams, validation and the explicitly unperformed host plan.

> Local source packets and reports remain outside this repository: `/workspace/.yolo/durable/handoff-20261026/execstop-upstream-source/` and `/workspace/.yolo/durable/execstop-v589-info-source/`.
