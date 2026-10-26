---
title: "Force-only Podman exec-monitor recovery stays fail-closed"
status: accepted
stage: DECIDED
next: "Complete exact HIGH review and land; rootless-host recurrence remains unmeasured"
tags: [design, podman, conmon, lifecycle, process-identity]
summary: "A narrowly guarded last resort for unresolved explicit force-stop/removal failures; no source fact or process name alone grants signal authority."
---

# Force-only Podman exec-monitor recovery stays fail-closed

**Status:** 2026-10-26. Behavior is ruled and implemented on the candidate branch; exact HIGH review and landing remain outstanding. **UNMEASURED:** no rootless-host recurrence or host recovery was witnessed.

> **In short.** Only an unresolved explicit `yolo stop --force` against a local Podman `5.8.9` container can attempt recovery, and only by signaling one pidfd-pinned exec conmon whose full identity, ownership, STOPPING snapshot, and only-zombie-child condition all revalidate. Every uncertainty declines; confirmed container absence, never a signal or return code, is success.

**Why it matters.** `ExecIDs` is stored-session association rather than liveness, and `Running=false` includes STOPPING. Conflating either with known-gone can strand a container, while killing its main monitor or a live exec can disrupt the container itself.

**The shape.** The stop caller asks one bounded Podman metadata probe; the Linux helper classifies an authentic exec monitor, pins it, rechecks the exact container/session/child identities, and can send only SIGKILL via that pidfd.

**Cost.** Unsupported versions, remote/default connections, missing metadata, inaccessible proc state, PID reuse, missing pidfd, and any child ambiguity receive no recovery. Rootless host recurrence remains unmeasured.

**Start at [the eligibility rule](#eligibility-and-revalidation)** — all required proofs and declines are there.

**Reads with:** [`../research/exec-monitor-recovery.md`](../research/exec-monitor-recovery.md) (version-fenced upstream facts), [`exec-stop-monitor-recovery-plan.md`](exec-stop-monitor-recovery-plan.md) (implementation map and test contract).

---

## Eligibility and revalidation

The recovery helper is Linux-only and applies only to the exact Podman CLI already selected by `yolo stop`. It is called only when the user explicitly requested `--force` and a force-stop or force-removal result remains unresolved. The caller passes explicit user intent separately from runtime argv: an ordinary stop may internally issue `podman rm --force`, but that does not grant authority to signal a host process. Other backends and normal graceful shutdown never enter the helper.

The selected Podman `5.8.9` info schema is the supported source fence. The unchanged `podman info --format json` invocation must return a structurally valid response with exact version `5.8.9`, explicit client-side `host.serviceIsRemote: false`, and an absolute `host.conmon.path`. Do not add `--remote=false`, strip `CONTAINER_HOST` or `CONTAINER_CONNECTION`, override Podman config, or infer client locality from a server's platform. Unknown/missing versions, fields, malformed JSON, or any nonlocal service decline. See the [research record](../research/exec-monitor-recovery.md#runtime-info-provenance).

Typed container inspect must provide one exact full container ID, state `stopping`, and stored ExecIDs containing the candidate UUID. ExecIDs is only an association check, never active-session proof. A candidate's process arguments must have exact `-c <full CID>`, explicit `-e`, and `-u <stored exec UUID>` where the UUID differs from CID. A main monitor (`-u` equal to CID or no `-e`), missing stored membership, `--no-session` not listed, or any live exec is ineligible.

Process identity requires caller-UID ownership; PID greater than one and different from self; strict nonzero `/proc` starttime; a live process executable whose file device/inode matches `host.conmon.path`; and successful `pidfd_open`. A numeric PID, argv text, PPID alone, or a historical ExecIDs entry is insufficient authority.

Before the pidfd signal, the helper repeats typed inspect and requires the same full CID, same exec UUID membership, and exact STOPPING state. It re-reads the monitor executable, UID, PID, PPID, state, and starttime and the observed direct child. Child enumeration must be complete across monitor tasks; it must contain exactly the owned direct zombie child used by the predicate and no live direct child. A foreign-UID child, unreadable or incomplete enumeration, unknown state, additional child, changed PPID/starttime/state/UID, inaccessible metadata, or any change observed at final revalidation declines. Signal only SIGKILL through the pinned pidfd for that exact exec-monitor. Never signal the main monitor, an exec child, a process group, or by numeric PID; never use this path for normal shutdown.

**The final state check is not atomic with the host signal.** Podman exposes no lock that spans typed inspect and `pidfd_send_signal`; container state can change after the last inspect and before the signal. The final read rejects observed changes, and the pidfd pins the process against PID reuse, but the pidfd does not pin container state. A pidfd-open or pidfd-send error such as `ESRCH` or `EPERM` declines recovery. Even a successful signal is not proof of removal; only the caller's answered typed-absence check is success.

## Caller outcomes

A runtime stop or removal return code is not proof of absence. The helper is attempted only after an explicit force request and unresolved runtime outcome. It performs one final typed/proc re-read, but Podman offers no atomic container-state lock across that read and pidfd signal; the state may change in that narrow interval. Any change observed at revalidation declines, and the pidfd prevents PID reuse only—not container-state changes. After an attempted signal, the caller re-probes typed container existence; only an answered absence can return success. A present or unknown container remains an error with the existing runtime recovery guidance. `STOPPING`, `Running=false`, an empty list, monitor exit, or a successful signal are not known-gone.

## Tests and operational limits

Permanent tests drive the actual public stop caller with a fake runtime runner, fake `/proc`, and fake pidfd implementation. Positive behavior must fail if the real recovery invocation is removed from its stop/removal caller. Negative controls also reach caller/runtime-selection gates, not just a helper parser: force=false, non-Podman/remote/missing or malformed metadata, non-5.8.9 schema, no ExecIDs member, main monitor, wrong CID/session/UID/executable/starttime, PID reuse, changed STOPPING snapshot, no pidfd, absent Z child, live/foreign child, and incomplete enumeration. Tests do not scan the host's `/proc`, run containers, or signal real processes.

> [!WARNING]
> **UNMEASURED:** no real rootless Podman host, real conmon process, or positive monitor-recovery recurrence was observed. The saved source report is not runtime evidence. Rootful or nested execution cannot prove rootless-host recurrence. A concrete, separately authorized, durable-path-only verification plan is in the [implementation plan](exec-stop-monitor-recovery-plan.md#rootless-host-verification-plan-not-performed).
