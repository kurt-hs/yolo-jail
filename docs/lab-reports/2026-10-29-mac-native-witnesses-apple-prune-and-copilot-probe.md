---
title: "Mac native witnesses: Apple Container prune output and the Copilot version probe"
date: 2026-10-29
tags: [lab-report, macos, apple-container, macos-user, copilot]
summary: "Native Apple Container 1.1.0 JSON parses through the new prune reader; the self-mode Copilot parse error is Copilot's trailing period. Two other new repairs wait on a launcher build."
---

# Mac native witnesses: Apple prune output and the Copilot version probe

**Run 2026-10-29 on the maintainer's Mac** (macOS 26.5, arm64, Apple Container 1.1.0). The source is
`main` at `7e633b446`. The installed launcher is `yolo-jail 0.15.1+705.gd159a57f0331`, which predates
every repair named here. The maintainer asked for no build on this Mac, so this run collects only the
witnesses that need no new launcher. No repair is claimed as natively verified.

**Terms.** A *native witness* is output captured on a real Mac, as opposed to the source-derived
fixtures the repair plans used. *Self mode* is macos-user's run-as-you mode, selected by
`"macos_user_mode": "self"` ([MMP-D12](../design/macos-user-model-plan.md#MMP-D12)).

## 1. What is owed, and what this run covers

| Repair | Plan's open item | This run |
| :--- | :--- | :--- |
| Apple Container image prune (`662d9103b`) | [native JSON output and deletion unmeasured](../design/apple-container-image-prune-plan.md#review-backlog--not-fixed) | Output captured and replayed through the new reader ([§2](#2-apple-container-image-prune)) |
| Capture compatibility context (`662d9103b`) | [raw output of the self-mode parse report missing](../design/native-capture-compatibility-context-plan.md#native-capture-compatibility-context) | Raw output captured; cause found ([§3](#3-self-mode-copilot-version-probe)) |
| Inherited account relaunch (`1c8be03c9`) | [native integration witnesses](../design/inherited-relaunch-plan.md) | Not run: needs a launcher at or after `1c8be03c9` |
| Claude view retirement (`662d9103b`) | [native On→Off sequence](../design/claude-view-retirement-plan.md) | Not run: needs a launcher at or after `662d9103b` |
| [Synthetic comparison runbook](../plans/macos-native-comparison-runbook.md) | Safehouse, ACL and TLS controls | Not run: needs a disposable macOS user and session, which only the maintainer can create |

## 2. Apple Container image prune

### 2a. Native output

`container image list --format json` and `container ls --all --format json` both exited 0. The
image list had 8 rows, and the container list had 1 (`yolo-ac-builder`, running).

- Each image row has the keys `configuration`, `id` and `variants`. `configuration` holds `name`,
  `descriptor` and `creationDate`, and `id` equals the descriptor digest without `sha256:`.
  These are the fields [`appleImageJSON`](../../internal/prune/appleimages.go#L16) reads.
- `name` is unprefixed for yolo's images (`yolo-jail:10c5c729c035f116`) and fully qualified for
  others (`docker.io/curlimages/curl:8.10.1`). Every digest is `sha256:` plus 64 hex characters.
- The container row carries `configuration.image.reference` and
  `configuration.image.descriptor.digest`, which [`appleContainerJSON`](../../internal/prune/appleimages.go#L25)
  reads. `status` is an object (`{"state": "running", "networks": [...], ...}`), not the string the
  plan's fixture shows. The reader does not read `status`, so this does not matter to it.

### 2b. Replay through the new reader

A throwaway test, created in `internal/prune`, run once and deleted, fed the captured JSON to
`readAppleImages`, `appleImagesInUse` and `appleCandidateImages` with an empty protected set:

```text
readAppleImages ok=true n=8
appleImagesInUse ok=true digests=1 refs=map[ghcr.io/mschulkind-oss/yolo-jail-builder:latest:true]
appleCandidateImages ok=true candidates=[yolo-jail:10c5c729c035f116 yolo-jail:3366b7f555d58d5d yolo-jail:37cfb9e1834ecd91 yolo-jail:latest]
```

The reader accepts native 1.1.0 output, and only `yolo-jail` images become candidates. `latest`
is a candidate here only because the replay passed no protected set. In production the caller
passes the protected set. This exercises the parsers, not `pruneAppleOldImages`' guard or a real
deletion.

### 2c. Delete grammar

Deleting a missing image exits 1 with
`Error: failed to delete one or more images: ["yolo-jail:doesnotexist0000"]`. The image count
stayed at 8. No existing image was deleted by this run.

### 2d. The installed launcher still has the old defect

The runner's post-job hook log (`~/actions-runner/_diag/yolo-post-job-cleanup.log`) recorded a real
CI job (`apple-container`, run 40921756461, 13:42). The hook's own image step deleted
`yolo-jail:c2abbbe5483f0960`. The installed `yolo prune` still printed
`FAILED — could not list this runtime's images (container), so nothing was swept.`, which is the
[defect the repair targets](2026-10-29-mac-ci-cleanup-and-native-confirmation.md#why-it-piles-up).
The rest of the prune and the bundle step ran, and 191 GiB stayed free.

## 3. Self-mode Copilot version probe

### 3a. Reproduction

The workspace was a disposable `/Users/Shared/yolo/cv-probe` with `{}` as its config, and
`overrides.jsonc` set self mode. `YOLO_RUNTIME=macos-user YOLO_ALLOW_MISSING_PROGRAMS=1 yolo -- bash ./probe.sh`
materialized Copilot from its capture and printed the same error the
[SecurityServer audit](2026-10-29-mac-securityserver-audit.md#6-also-seen) reported:

```text
yolo: ERROR: cannot parse copilot's declared version probe for this macos-user self launch; use a container backend (podman or container), or repair its declared version probe.
```

The generated launcher had `COMPATIBILITY_CONTEXT_VALID=1`, `COMPATIBILITY_ENABLED=1`,
`COMPATIBILITY_REFUSE_AT_OR_ABOVE=1.0.93` and `PROBE_ARGS=(--version)`. The context was valid,
so this is not the "invalid macos-user launch context" family the audit guessed.

### 3b. Raw output

Inside the jail, `~/.local/bin/copilot --version` exited 0. Stdout alone and stdout plus stderr
were identical:

```text
GitHub Copilot CLI 1.0.103.
Run 'copilot update' to check for updates.
```

### 3c. Cause

The guard in [`programcompatibility.go`](../../internal/entrypoint/programcompatibility.go#L157)
matches `(^|[^[:alnum:].]|[vV])([0-9]+)\.([0-9]+)\.([0-9]+)($|[^[:alnum:].])`. The character after
the version must not be a letter, a digit or `.`, so that a four-part token like `1.0.92.103` is
refused ([`TestShippedCopilotMalformedVersionTokensFailClosed`](../../internal/entrypoint/programcompatibility_test.go#L280)).
Copilot ends its version with a sentence period, so nothing in the output matches. The same
regex in bash 5.3 on the host:

| Input | Result |
| :--- | :--- |
| `GitHub Copilot CLI 1.0.103.` plus the update line | no match |
| `GitHub Copilot CLI 1.0.103` | match, `1.0.103` |
| `1.0.103` | match, `1.0.103` |

The launch fails closed either way, because 1.0.103 is above the 1.0.93 threshold in
[`packs/copilot/compatibility.json`](../../packs/copilot/compatibility.json). Only the message is
wrong. The user sees "cannot parse … repair its declared version probe" instead of the intended
refusal, which names [issue #5085](https://github.com/github/copilot-cli/issues/5085) and the
container backends. The unit fixtures pass bare versions, so none of them carries the trailing
period. No fix was made here.

## 4. Also seen

- `yolo check --no-build` in this repository reports one FAIL:
  `yolo-jail.jsonc:78:15: config.brokered.git.grants[0].kind: your user config does not allow a workspace to hold a 'branch' grant`.
  The grant came in with `02e8d13de` (2026-10-24), and the user config was not changed. The
  disposable workspace's own check passed (77 passed, 3 warnings).
- Pack `matt`'s `board` program failed to install in the self-mode jail. Its fork build
  `c5dafe0445128f70` is a linux/arm64 capture, and this is darwin/arm64.

## 5. State changed on this Mac

- `~/.config/yolo-jail/overrides.jsonc` was created for [§3](#3-self-mode-copilot-version-probe) and removed.
- `/Users/Shared/yolo/cv-probe` and its tmux session `cv-probe` were created and removed.
- Two capture files of JSON output went to the session scratchpad, outside the repository.
- `internal/prune/zz_native_replay_test.go` was created, run once and deleted. `git status` was
  clean afterwards.
- No image, container, keychain or credential was changed.
