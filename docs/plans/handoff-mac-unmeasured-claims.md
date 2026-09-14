---
title: "Handoff: the claims that shipped on reading, and the Mac that can settle them"
status: in-review
date: 2026-09-13
tags: [macos-user, handoff, seatbelt, declaration-parity, ci, image]
summary: "Seven threads landed on 2026-09-13 whose correctness was read off source rather than observed. All of them are now measured on hardware except the nightly's unexplained exit 125: the three Seatbelt probes ran (nothing inverted — Seatbelt evaluates the target, so DP-L1's mechanism stays a copy), and one launch settled the briefing batch and both notice sets. Two defects were found in the process, both fixed: the workspace reached SeatbeltProfile un-resolved, which made its rules dead and was masking a bypass of the neutral-ground refusal, and the briefing's Packages section offered a resources cap this backend ignores by ruling."
---

# Handoff: the claims that shipped on reading, and the Mac that can settle them

**Audience:** an agent or human at a real Mac. Each thread says whether it needs Apple Silicon,
a password, or only a Mac.

**Role of this doc:** a *sequencer*, like
[`runbooks/mac-agent-guide.md`](runbooks/mac-agent-guide.md). It does not restate any
procedure — [`runbooks/macos-user-manual-checks.md`](runbooks/macos-user-manual-checks.md) is
the spec for what only a Mac can verify and stays the spec. What this doc adds is **what
changed on 2026-09-13 that is now waiting on hardware, in the order that buys the most.**

> [!NOTE]
> **[§1](#1-the-three-seatbelt-probes--do-these-first) IS DONE — all three probes run on hardware
> 2026-09-13, raw output recorded at
> [`../design/declaration-parity.md` §6.1](../design/declaration-parity.md#61-dp-l1-the-mechanism-is-a-copy-and-what-nobody-has-measured).**
> Nothing inverted: Seatbelt evaluates the TARGET, so `DP-L1`'s mechanism stays a copy, and
> `DP-L4`'s read is real (EPERM for the write, EACCES for the same write unsandboxed — the two
> errnos are what prove it is the profile and not the directory's owner). Probe 2 found the latent
> bug **reachable**, and fixing it exposed a second defect it had been masking: a symlinked
> workspace evaded the neutral-ground refusal. Both fixed in `4b25d7b9`.
>
> **[§2](#2-the-briefing-batch-on-the-arm-no-test-executes) and
> [§3](#3-the-new-stderr-notices-in-real-output) are DONE TOO** — one launch, 2026-09-13. The
> macos-user arm of `run.Run` is reached and every briefing expectation holds; both notice sets
> read correctly and a config declaring none of those keys prints none of the lines. That section's ⚠ was
> fixed by `0d0c26da` before the run, and that run found a **fourth** false place one section lower
> (`## Packages & Resource Limits`, offering a `resources` cap this backend ignores by ruling),
> fixed in `8027814e`.
>
> **What is left in this file: the nightly ([§4](#4-the-nightly--five-links-all-now-named)) only** —
> the nightly was re-dispatched 2026-09-13 (run `34774761002`) and its `exit 125` is still
> unexplained. That needs no Mac.

> [!IMPORTANT]
> **The original lead, kept for its reasoning.** *(Superseded above 2026-09-13.)* **Start at
> [§1](#1-the-three-seatbelt-probes--do-these-first). One measurement settles three
> threads**, two of its three probes need no yolo, and one of them can invert an entire section
> of the parity catalog. Nothing else in this file is close.

**What is NOT here, so you do not re-do it:** whether the backend works. It does —
[`handoff-macos-user-open-threads.md`](handoff-macos-user-open-threads.md) records the first
end-to-end hardware run (2026-09-12, macOS 26.5, arm64) and every runbook item passing. This
file is only about claims made *since*, on reading.

---

## 0. What changed on 2026-09-13

Two waves landed. The first fixed four CI/lint defects; the second implemented steps 1–3 and 6
of [`../design/declaration-parity.md`](../design/declaration-parity.md) [§11](../design/declaration-parity.md#11-what-i-would-build-in-order). Between them they
produced **seven claims about macos-user that no machine has executed**, plus one CI result
that is genuinely new.

**The genuinely new result, which needs nothing from you:** the `macos-user backend` workflow
went **green for the first time ever** (run `34769269355`), including the `lsp_servers` subtest
that was red on 2026-09-12. That closes the last open item of
[`handoff-macos-user-open-threads.md`](handoff-macos-user-open-threads.md) [§1](handoff-macos-user-open-threads.md#1-lsp_servers-installs-nothing-on-this-backend--ruled-and-wired-2026-09-13) — its *"leaving
one Mac run of its integration subtest"* has now happened and passed. Six of the ten runbook
items therefore have a passing automated twin on a schedule.

⚠ **The `Nightly macOS Integration` workflow is a different job and is still not a signal.** See
[§4](#4-the-nightly--five-links-all-now-named).

---

## 1. The three Seatbelt probes — do these first

**Needs:** a Mac. **Probes 1 and 2 need no yolo, no `_yolojail` account and no password.**
**Time:** ~20 minutes. **Source:** [`../design/declaration-parity.md`](../design/declaration-parity.md)
[§6.1](../design/declaration-parity.md#61-dp-l1-the-mechanism-is-a-copy-and-what-nobody-has-measured)'s `CAUTION` block, which spells all three out with their commands.

### Why this one first

It is the only measurement in this file that unblocks more than itself:

1. **It is the gate on the largest unbuilt piece of work in the catalog.** [§11](../design/declaration-parity.md#11-what-i-would-build-in-order) step 5 (`DP-L1`,
   five cells) is the materialize mechanism, and it was deliberately held back from the
   2026-09-13 implementation wave for exactly this reason. Probe 1 decides whether the
   mechanism is a copy or can be a symlink.
2. **It retroactively settles a change that already shipped.** `DP-L4` put `/var/yolo-jail` on
   `macosuser.SandboxPath` on 2026-09-13, so the sandbox can resolve `yolo` by name. That the
   Seatbelt profile permits **read + exec** there is read off the SBPL text — the profile is
   `(allow default)` with read denies only for `/Volumes`, `/Users` and `/Library/Keychains`,
   so `/var/yolo-jail` lands on `(allow default)`. **Probe 3 is precisely that read, and nobody
   has ever made it.** `macosuser.DarwinBootstrapArgv` carries no `sandbox-exec`, so the staged
   tree has only ever been read *unsandboxed*.
3. **Probe 2 pins a latent bug** independent of its own verdict —
   `macosuser.BuildRunPlan` passes `workspace` **raw** into `SeatbeltProfile` while
   `YOLO_HOST_DIR` gets `resolvePathAbs(workspace)` a few lines later. Latent only because
   `HomeContaining` normally pushes workspaces onto a non-symlinked shared root.

### RESULT, 2026-09-13 — all three run, nothing inverted, two defects fixed

| Probe | Result | What followed |
| :--- | :--- | :--- |
| 1 — the crux | `Operation not permitted` for an absolute AND a relative link, with three controls behaving | Target evaluation. The three statements stand, the symlink half is dead, `DP-L1` stays a copy. **No retraction.** |
| 2 — canonicalization | deny `(subpath "/tmp")` → `touch /tmp/canary` **succeeded**; deny `(subpath "/private/tmp")` → the same write **denied** | The latent bug is REACHABLE (Go's `os.Getwd` honours `$PWD`, so an ordinary `cd` reaches it). Measured consequence: the workspace is unwritable under the profile yolo built. **Fixed `4b25d7b9`** — plus the bypass below. |
| 3 — the staged tree | read OK; write `Operation not permitted` sandboxed vs `Permission denied` unsandboxed | The free `:ro` is observed, not predicted. `DP-L4`'s read is real. |

⚠ **Probe 2's fix could not be made alone, and that is the part worth carrying forward.**
`HomeContaining` — the neutral-ground refusal ([DP-D15](../design/declaration-parity.md#7-ruled-divergent-and-the-ones-i-would-re-open)) — read the raw path too, so
`/Users/Shared/yolo/homelink` → `/Users/matt/…` passed with `✓ all plan invariants hold`,
measured. The dead profile was the only thing making that fail-closed. **A reader who "just
resolves the path for the profile" converts a confusing launch failure into a live grant into the
invoking user's home.** One resolution, both consumers.

### The stakes on probe 1

Three statements in this tree agree that Seatbelt evaluates the **target** rather than the
link, and **none of them is an observation**: `macos-user-nix-and-features.md`'s *"Any doc that
says otherwise about this backend is wrong; this one is the authority"*, the shipped
`cache_relocations` warning in `macosuser.buildPlan`, and `macos-user-home-tiers.md` [§5.3](../design/macos-user-home-tiers.md#53-what-the-credential-tier-then-needs-precisely)'s
*"resolution happens in the VFS before the policy is consulted"*.

| Probe 1 result | What follows |
| :--- | :--- |
| `Operation not permitted` | The three statements stand, the symlink half is dead, `DP-L1`'s mechanism is a copy, and [§6.1](../design/declaration-parity.md#61-dp-l1-the-mechanism-is-a-copy-and-what-nobody-has-measured) is confirmed as written. |
| **Success** *(did not happen — probe 1 came back denied, both spellings)* | **[§6.1](../design/declaration-parity.md#61-dp-l1-the-mechanism-is-a-copy-and-what-nobody-has-measured) inverts.** A staged symlink becomes legitimate, and *both* `cache_relocations` warnings plus `macos-user-home-tiers.md` [§5.3](../design/macos-user-home-tiers.md#53-what-the-credential-tier-then-needs-precisely)'s VFS claim need retracting. |

Report the raw command output either way, not a verdict — the second row rewrites shipped
warnings, so the evidence has to outlive the conclusion.

---

## 2. The briefing batch, on the arm no test executes

**Needs:** a Mac with `_yolojail` (i.e. after `yolo macos-setup`). **Time:** one launch.

`DP-L2`, `DP-L7`, `DP-L8`, `DP-L9` and the [`OQ-DP2`](../design/declaration-parity.md#decision-ledger) threading all landed on 2026-09-13 and are
verified on Linux through the real `refreshJailBriefings` → written-file path. The gap is
narrow and specific: **no test in this repo executes the macos-user arm of `run.Run`.** That
the arm is reached with `rt == "macos-user"` is read at the call site and not observed.

So: launch macos-user once and read `.yolo/`'s composed briefing. Expect the network paragraph
to say *"this environment shares the host's network stack"*, **no** ports sections, **no**
`/ctx` mounts, **no** resource limits, and a header describing Seatbelt rather than namespaces.

> [!NOTE]
> **MEASURED 2026-09-13 — the arm IS reached, and every expectation above holds.** One launch in
> `/Users/Shared/yolo/mac-hand`, reading the briefing the sandbox actually got
> (`<ws>/.yolo/home/claude/CLAUDE.md`, 7793 bytes, written by that launch):
>
> | Expectation | Observed |
> | :--- | :--- |
> | the network paragraph | *"**Network**: Host networking — this environment shares the host's network stack. `localhost` / `127.0.0.1` resolves directly to the host. No port mapping needed."* |
> | no ports sections | none |
> | no `/ctx` mounts | none — the string does not occur |
> | no resource limits in the block | none |
> | a Seatbelt header | *"# YOLO Environment — jail (native, no container)"* / *"You are confined by a Seatbelt sandbox on the human's REAL machine, not by a container."* |
>
> The macos-user-specific sections are all correct and well-aimed too — the shared-home warning
> names `.claude-shared-credentials` and `.gemini-shared-credentials` one by one, the
> rendered-from-DEFAULTS warning names both settings files, and the no-network-namespace
> paragraph says *"every port you bind is bound on the human's REAL machine … listed in
> `network.ports` or not."*

~~⚠ **While you have that briefing open, read the `## Environment` block**~~ — **FIXED
2026-09-13 (`0d0c26da`), and the measurement above is of the fixed block.** It was false in three
places at once: `/workspace` described as a bind mount (there is none — the workspace is at its
real path), **Home** as `/home/agent` (it is `/Users/_yolojail`), and **OS** as *"NixOS-based
minimal container"* — contradicting the header three lines above it. The wording ruling it was
waiting on came down as **name the absence, keep `/workspace` canonical**: the three built-in
skills carry 25 `/workspace` references as static markdown, so the bullet became the one place a
macos-user agent is told those references mean its own path.

> [!IMPORTANT]
> **A FOURTH FALSE PLACE, one section lower, which that fix did not reach — now also fixed
> (`8027814e`).** `## Packages & Resource Limits` told the agent to request a *"container-limit
> change"* by editing `resources`: no container, and `resources` is read and IGNORED here by
> ruling ([DP-D1](../design/declaration-parity.md#7-ruled-divergent-and-the-ones-i-would-re-open)).
> Worse than a wrong path — an agent that follows it asks its human for a cap nothing delivers,
> the human grants it, and the limit does not exist. DP-D1's own sentence decided the shape
> (*"a cap a user believes in but that does not hold is worse than a documented absence"*), so
> the native arm is `## Packages`, names only `packages`, and states the absence. `/workspace` is
> kept on both arms so the convention above is not forked.

---

## 3. The new stderr notices, in real output

**Needs:** a Mac with `_yolojail`. **Time:** minutes, same launch as [§2](#2-the-briefing-batch-on-the-arm-no-test-executes).

Two sets of per-key warnings now print on the macos-user arm, both new on 2026-09-13 and both
asserted only against Linux fixtures:

- `DP-L10` — one line each for a declared `devices`, `gpu.enabled` or `kvm`. These deliberately
  do **not** reuse the container path's *"not supported on macOS"* string, because that is a
  **platform** claim, true of podman and Apple Container on a Mac and false here: the key is not
  refused by a platform limit, it is read by nothing.
- `DP-L2`'s stderr half — one line each for a non-empty `network.ports` or
  `forward_host_ports`, naming remap entries separately as the not-satisfiable half.

What to check is not that they appear — Linux proves that — but that they read correctly beside
a real launch's other output, and that a config declaring **none** of these keys produces
**none** of these lines. A warning people learn to skip is worse than none
([`OQ-BP-3`](../design/backend-parity.md#open-questions)), and that is the failure mode here.

> [!NOTE]
> **MEASURED 2026-09-13, both halves.** The negative half first, because it is the one
> [`OQ-BP-3`](../design/backend-parity.md#open-questions) cares about: the real launch above
> declares none of these keys and printed **none** of these lines. The positive half came from a
> throwaway workspace declaring all five (a `--dry-run`, so no password) — every notice fired,
> and none of them reuses the container path's *"not supported on macOS"* string:
>
> ```
> Warning: `devices` is not read on macos-user — usb probe device. Device passthrough attaches a
>   host device to a CONTAINER, and this backend starts none; the sandboxed process reaches
>   devices under ordinary macOS permissions instead, so yolo neither attaches nor restricts
>   anything here.
> Warning: `gpu.enabled` is not read on macos-user — GPU passthrough is a CDI device plus
>   NVIDIA/ROCm environment on a container, and this backend starts none. …
> Warning: `kvm` is not read on macos-user — it asks for /dev/kvm inside a container, and there
>   is neither a container nor a /dev/kvm on macOS.
> Warning: `network.ports` is not honored on macos-user — 18080:8080. … 18080:8080 asks for a
>   port REMAP, which needs a second stack to land on and cannot be delivered at all: the
>   process is reachable on the port it binds.
> Warning: `network.forward_host_ports` is not honored on macos-user — 5432:5432. There is no hop
>   to make: the sandbox is already on this machine's stack, so `localhost:<port>` inside it is
>   this machine's port.
> ```
>
> `DP-L2`'s remap half reads exactly as designed — the entry is named twice, once for what is
> true anyway (the port is published on real interfaces regardless) and once as the
> **not-satisfiable** half. And each `DP-L10` line says *read by nothing* rather than
> *platform-refused*, which is the distinction that section required.
>
> ⚠ **Incidental, and worth knowing before writing a probe config:** `forward_host_ports` is
> `network.forward_host_ports`, not top-level — a top-level spelling is refused as an unknown
> key. The port-shape refusal is one of the better messages in the tree: *"expected
> '&lt;host&gt;:&lt;jail&gt;' or '&lt;ip&gt;:&lt;host&gt;:&lt;jail&gt;' (host side FIRST — the reverse of
> network.forward_host_ports)"*.

---

## 4. The nightly — five links, all now named

**Needs:** no Mac — this is CI. Listed here because it is macOS-gated in practice.

`Nightly macOS Integration` has been red since **2026-09-04** (nine consecutive scheduled runs,
last green 2026-09-04). The cause was diagnosed on 2026-09-13 and fixed: the launcher built the
image **unconditionally**, which the
[`../design/darwin-image-provenance.md`](../design/darwin-image-provenance.md) chain never
named, and [`OQ-IP4`](../design/darwin-image-provenance.md#decision-ledger) now makes a stock-tagged image skip the build entirely.

> [!WARNING]
> **The run testing that fix is contaminated and must be re-dispatched.** The commit it ran on
> also carried a defect where a launch disclosure was written to the jail command's **stdout**,
> which two integration tests compare exactly. Whatever that run says about the stock tag is
> unreliable. Re-dispatch on a commit that includes the fix
> (`gh workflow run nightly-macos.yml --repo mschulkind-oss/yolo-jail --ref main`).

> [!NOTE]
> **RESOLVED 2026-09-13, and `exit 125` was two more links rather than one.** The report cap was
> widened exactly as planned, the next red nightly said what 125 was, and clearing it exposed
> another cause underneath. The whole chain, in the order each became visible — each was hidden
> by the one before it, which is the thing worth carrying forward about this failure:
>
> | # | Cause | Symptom it presented as | Fixed |
> | :--- | :--- | :--- | :--- |
> | 1 | `imageIdentity` varied by system, so a darwin host could not vouch for a Linux-built image | every launch demanded a rebuild | [`OQ-IP1`](../design/darwin-image-provenance.md#decision-ledger), 2026-09-12 |
> | 2 | the launcher built the image **unconditionally** — there was no rebuild *decision* to fix | `IMAGE BUILD FAILED` | [`OQ-IP4`](../design/darwin-image-provenance.md#decision-ledger) |
> | 3 | `podman machine init -v` REPLACES the default share set, so `-v /nix:/nix` deleted `/Users`, `/private` and `/var/folders` | `exit 125` → `Error: statfs <path>` | `1e55f321` |
> | 4 | two of four shards exceeded `timeout-minutes: 50` once launches did real work | half the run's evidence silently missing | eight shards, `178fa9f2` |
> | 5 | `-v /nix/store:/nix/store:ro` shadowed the image's own store; `/bin/*` are symlinks BY VALUE into it, and a darwin store holds no Linux closure | `exec: "bash": executable file not found` — 50 times, 41 tests | `8f59c674` |
>
> **Link 3's fix is what made link 5 visible**, and link 5 had been latent since 2026-09-08.
> Nothing was flaky; every one was deterministic and each only surfaced once its predecessor
> stopped failing first.
>
> ⚠ **What is NOT fixed, and is expected:** three tests (`TestExtraPackageLibFarm`,
> `TestExtraPackagesFromMountedStore`, `TestDevPackageLinksRuntimeLib`) declare `packages:`, which
> makes them genuinely NON-STOCK, so they correctly build — and this runner has no Linux builder.
> That is the designed behaviour, not a bug. Whether they should SKIP on a builder-less runner
> rather than fail is an open question nobody has ruled.
>
> **Still open:** the run proving link 5 had not finished when this was written. Confirm with
> `exec: "bash"` → 0, `IMAGE BUILD FAILED` → still 3, and the other 38 green.

---

## 5. What a green run would let us delete

Not work — a consequence worth knowing, because it changes what the next reader trusts.

[`../design/declaration-parity.md`](../design/declaration-parity.md) [§11](../design/declaration-parity.md#11-what-i-would-build-in-order) carries a `CAUTION` stating that every macos-user row in
the catalog is *"ruled against reading, not against measurement"*, and that **two macos-user
launch warnings were retired on 2026-09-12 on the strength of code that has never executed**.
Items [§1](#1-the-three-seatbelt-probes--do-these-first) and
[§2](#2-the-briefing-batch-on-the-arm-no-test-executes) together are most of what that caution
is about. A Mac session that runs both either retires the caution or re-opens a carve-out that
is currently undeclared — and the second outcome is the one worth going looking for.

---

## What to do first, if you want an order

1. [§1](#1-the-three-seatbelt-probes--do-these-first) probes 1 and 2 — no yolo, no account, no
   password. Twenty minutes, and probe 1 can invert a section.
2. `yolo macos-setup` if the account is absent, then [§1](#1-the-three-seatbelt-probes--do-these-first)
   probe 3 — the read `DP-L4` already depends on.
3. One macos-user launch, reading [§2](#2-the-briefing-batch-on-the-arm-no-test-executes) and
   [§3](#3-the-new-stderr-notices-in-real-output) off the same run.
4. Re-dispatch the nightly ([§4](#4-the-nightly--five-links-all-now-named)) — it
   needs no Mac and can run while you do the rest.

**Report raw output, not verdicts.** Three of these threads can retract something already
shipped, and a verdict without its evidence cannot be re-litigated when the next reader
disagrees.
