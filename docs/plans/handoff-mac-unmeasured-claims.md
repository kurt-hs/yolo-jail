---
title: "Handoff: the claims that shipped on reading, and the Mac that can settle them"
status: in-review
date: 2026-09-13
tags: [macos-user, handoff, seatbelt, declaration-parity, ci, image]
summary: "Seven threads landed on 2026-09-13 whose correctness is read off source rather than observed, and one measurement settles three of them at once. The three Seatbelt probes in the parity catalog's DP-L1 section need a Mac and about twenty minutes, two of them need no yolo at all, and one of the three can invert a whole section of that catalog. Everything else here is cheaper and less consequential."
---

# Handoff: the claims that shipped on reading, and the Mac that can settle them

**Audience:** an agent or human at a real Mac. Each thread says whether it needs Apple Silicon,
a password, or only a Mac.

**Role of this doc:** a *sequencer*, like
[`runbooks/mac-agent-guide.md`](runbooks/mac-agent-guide.md). It does not restate any
procedure — [`runbooks/macos-user-manual-checks.md`](runbooks/macos-user-manual-checks.md) is
the spec for what only a Mac can verify and stays the spec. What this doc adds is **what
changed on 2026-09-13 that is now waiting on hardware, in the order that buys the most.**

> [!IMPORTANT]
> **Start at [§1](#1-the-three-seatbelt-probes--do-these-first). One measurement settles three
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
[§4](#4-the-nightly-and-the-exit-125-nobody-has-explained).

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

### The stakes on probe 1

Three statements in this tree agree that Seatbelt evaluates the **target** rather than the
link, and **none of them is an observation**: `macos-user-nix-and-features.md`'s *"Any doc that
says otherwise about this backend is wrong; this one is the authority"*, the shipped
`cache_relocations` warning in `macosuser.buildPlan`, and `macos-user-home-tiers.md` [§5.3](../design/macos-user-home-tiers.md#53-what-the-credential-tier-then-needs-precisely)'s
*"resolution happens in the VFS before the policy is consulted"*.

| Probe 1 result | What follows |
| :--- | :--- |
| `Operation not permitted` | The three statements stand, the symlink half is dead, `DP-L1`'s mechanism is a copy, and [§6.1](../design/declaration-parity.md#61-dp-l1-the-mechanism-is-a-copy-and-what-nobody-has-measured) is confirmed as written. |
| **Success** | **[§6.1](../design/declaration-parity.md#61-dp-l1-the-mechanism-is-a-copy-and-what-nobody-has-measured) inverts.** A staged symlink becomes legitimate, and *both* `cache_relocations` warnings plus `macos-user-home-tiers.md` [§5.3](../design/macos-user-home-tiers.md#53-what-the-credential-tier-then-needs-precisely)'s VFS claim need retracting. |

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

⚠ **While you have that briefing open, read the `## Environment` block** — it is a defect the
catalog does not yet have a row for, found on 2026-09-13 and deliberately not fixed. On
macos-user it is false in three places at once: `/workspace` described as a bind mount (there
is none — the workspace is at its real path), **Home** as `/home/agent` (it is
`/Users/_yolojail`), and **OS** as *"NixOS-based minimal container"* — contradicting the header
three lines above it. **This needs a wording ruling before anyone can fix it**, specifically
for the workspace line, and that ruling is the maintainer's.

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

---

## 4. The nightly, and the exit 125 nobody has explained

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

**`exit code: 125` is a separate, still-unexplained symptom**, deliberately not guessed at. It
is podman's own "could not start", so the container never started, and the harness's 60-line
report cap swallowed podman's words. The report now quotes the last lines when it truncates,
so **the next red nightly should finally say what 125 was.** Expect it to survive the stock-tag
fix; if it does not appear at all, say so, because that is information too.

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
4. Re-dispatch the nightly ([§4](#4-the-nightly-and-the-exit-125-nobody-has-explained)) — it
   needs no Mac and can run while you do the rest.

**Report raw output, not verdicts.** Three of these threads can retract something already
shipped, and a verdict without its evidence cannot be re-litigated when the next reader
disagrees.
