---
title: "Linux fork materialization verification"
date: 2026-10-19
status: open
tags: [lab-report, linux, patched-forks, testing]
summary: "Records selective completion checks and a full Linux integration pass for the tested Go tree, with platform and release limits kept explicit."
---

# Linux fork materialization verification, 2026-10-19

A [lab report](README.md). This records verification evidence, not a source fix or a release gate.

## Observed

The recorded source snapshot was `a866b30b2d0c0a83236aeafbe630ee5fdfc5c259`. Main subsequently landed as `a08d51c9785d90ef6ab1da6879f1527a60846c5b`; its diff from that snapshot changes only `CHANGELOG.md` and `.pi/skills/cutting-releases/SKILL.md`. The Go source tested here is therefore unchanged on that main commit. The selective `just done` run completed before `a08`; the full integration run began before and finished after it, with source metadata recording `a866b30b2d0c0a83236aeafbe630ee5fdfc5c259` both before and after. Do not describe the first run as post-main or the integration command as having started on `a08`.

`just done` passed its selective checks for 15 changed paths: Linux and Darwin vet/staticcheck, short tests, formatting, and changed-file reader tests. Then `YOLO_NO_PROGRAM_READINESS=1 go test -timeout 0 ./integration` passed: package time 758.197 seconds, wall time 759 seconds. No `-count=1` was used. Both run records ended clean, with zero staged files.

## Open limits

The host was Linux amd64 and Podman reported `rootless=false`; rootless behavior and native macOS remain unverified. The integration run opted out of program readiness, and did not exercise interactive agents or authenticated API flows. A macOS local-sidecar regression remains an explicitly deferred experimental patch, not a blocker and not a verified fix. No push, tag, release, install, or deploy was performed; ordinary CI must be checked against the exact main SHA before any release claim.

## Evidence

- `just done` — exit 0; selective checks passed.
- `YOLO_NO_PROGRAM_READINESS=1 go test -timeout 0 ./integration` — exit 0; `ok github.com/mschulkind-oss/yolo-jail/integration 758.197s`.
- Source before/after: `a866b30b2d0c0a83236aeafbe630ee5fdfc5c259`; main: `a08d51c9785d90ef6ab1da6879f1527a60846c5b`.
