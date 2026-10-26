---
title: "Jail mechanics: native and real-rootless verification"
status: accepted
stage: DECIDED
next: "Run the backend-specific observations below when a native macOS or non-nested rootless Linux host is available"
tags: [verification, macos, podman, mechanics]
summary: "A bounded observation plan for mechanics guidance on native macOS and a real rootless Linux Podman host; it changes no behavior or policy."
---

# Jail mechanics: native and real-rootless verification

**Status:** The observations below remain outstanding until the required hosts are available; the
plan adds no behavior or product-policy question. See the [jail mechanics reference](../reference/jail-mechanics-skill.md)
for implemented behavior and its explicit unmeasured limits.

## Preconditions

- Use the exact reviewed candidate or later approved landing commit, and record its full SHA.
- Run on an actual native macOS host for the `macos-user` observation, and on a Linux host where
  Podman is genuinely rootless for the rootless observation. A nested container is not a
  substitute: it shares host networking and user namespaces and cannot prove these properties.
- Use a disposable workspace and non-interactive fixture content. Do not start an agent, use
  credentials, or make provider/API calls.

## Native macOS observation

1. Launch through the actual `macos-user` backend and inspect the staged `configuring-the-jail`,
   `diagnosing-the-jail`, mechanics index, host map and applicable recipes.
2. Compare every rendered host path and lifecycle statement against that launch's real workspace,
   home and host-file delivery. Confirm that the delivered skills' permissions are enforced by
   the native profile at the paths the sandbox actually reads.
3. Record the operating-system version, backend/mode, candidate SHA, exact commands, observed
   output and any refusal. This is direct runtime evidence; a Darwin-targeted compile or vet is
   not a substitute.

## Real-rootless Linux Podman observation

1. On the host, run `podman info --format '{{.Host.Security.Rootless}}'` and record its output; it
   must be `true`. Do not run this check from inside a nested jail.
2. Launch a disposable container jail and compare the generated host map with the actual running
   jail's mounts and its authenticated applied record. Exercise one successful live context-mount
   apply and a fresh attach; verify the regenerated map follows the effective mount, not an
   unapplied config edit.
3. Where a controlled failed fold can be produced without widening privileges, verify that it
   leaves the authenticated snapshot and both mechanics files unchanged. If that setup is not
   available, report this subcase as not observed rather than as a pass.
4. Record host/kernel and Podman details, the candidate SHA, the rootless-state output, exact
   commands, actual mount/snapshot comparisons, and any skipped subcase. This proves only the
   exercised runtime paths; it does not certify generic host reachability without a separate
   service-reachability observation.

## Landing boundary

- Validate the current checked-in JSONC config contract and its legacy behavior; this plan does not activate the separate readable-config/TOML design.
- Keep regression assertions at the production call sites that construct and publish mechanics facts. A renderer golden or helper-only test does not prove those callers remain wired.
- On an exact candidate, use the repository's matched-bundle path with non-agent fixture commands to validate the generated guidance where runtime observation is part of the claim. Do not start an agent or infer runtime behavior from unit fixtures.
- Run final whole-repository QA serially once on the final combined tree, after component changes are integrated; component-level checks do not substitute for it, and it should not be repeated per document edit.
- Keep native macOS and real-rootless observations separate from static tests and nested-host evidence. State each unavailable platform or behavior plainly; do not generalize beyond the environment actually observed.

## Report

Keep the two observations separate and state the environment that produced each result. Report
failed or unavailable host prerequisites plainly; do not convert a static test, fixture, or nested
Podman run into native macOS or real-rootless evidence.
