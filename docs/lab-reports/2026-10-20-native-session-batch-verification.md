---
title: "Native session batch verification"
date: 2026-10-20
status: open
tags: [lab-report, macos-user, sessions, testing]
summary: "Records post-main combined Linux verification and the platform-specific evidence that remains unmeasured."
---

# Native session batch verification, 2026-10-20

A [lab report](README.md). This records gates and their scope; it is not a release or native-macOS certification.

## Observed

The source snapshot was `f5841084a83fdc688acf8e224d43e5035344350d`, after the serial main-first commits `0935c4ed9954a1f64ad010b13b9e1a586709e954`, `007d278064a6dda8a97f13bcd3c940ea20f188f3`, and `f5841084a83fdc688acf8e224d43e5035344350d`. The first full `just check-ci` exposed two outdated assertions in [`bootsteps_test.go`](../../internal/entrypoint/bootsteps_test.go): the macos-user sequence omitted the now-shared `prepare_session_homes` step, and its AST check recognized only an inline `bootRun` although `RunDarwinBootstrap` prepares the value before running the table. The narrowly updated assertions passed; no production source changed. The repaired full `just check-ci` passed in 224 seconds.

`just build-go` passed. A fresh isolated home used its actual user config path `$HOME/.config/yolo-jail/config.jsonc` with `packs: []`; `yolo check --no-build` passed (16 passed, 4 warnings). The freshly built CLI, found by `PATH` and pointed at this checkout with `YOLO_REPO_ROOT`, launched a rootful nested jail from a throwaway workspace; the bounded bash probe printed `NESTED_SMOKE=booted` and exited 0. The exact slow command `YOLO_NO_PROGRAM_READINESS=1 go test -count=1 -timeout 0 ./integration` passed in 804 seconds.

Prior item56 evidence is from candidate `b23fd889b613e0893795000f36ed268addd051a9`, not this combined source run: after correcting the isolated user-config location from XDG to the actual `$HOME/.config/yolo-jail/config.jsonc`, its preflight exited 0 (20 passed, 4 warnings) and nested smoke exited 0 with `NESTED_SMOKE=booted`. The original misplaced-config preflight is not counted as a pass.

## Limits and evidence

All fresh checks ran on Linux/amd64 with Podman `rootless=false`; nested networking was shared with the host. Rootless delivery/reachability is unverified. Native macOS account census, Seatbelt physical-home access and real Pi foreground/detached lifecycles remain unmeasured. Claude vendor refresh-lock behavior and file-versus-Keychain precedence remain unverified; no real agent, credentials, login, or API call was used. No rollout, install, push, tag, or publication was performed.

Detailed command output and UTC timing are in `$YOLO_DURABLE_DIR/post-main-native-batch-20261020T035507Z/`; recovered b23 setup and smoke metadata are in the existing lane37 nested-smoke record. This report does not claim completion of the broader post-release queue.
